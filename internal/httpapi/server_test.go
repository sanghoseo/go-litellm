package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/BerriAI/litellm/go-proxy/internal/auth"
	"github.com/BerriAI/litellm/go-proxy/internal/config"
	"github.com/BerriAI/litellm/go-proxy/internal/providers"
	"github.com/BerriAI/litellm/go-proxy/internal/usage"
)

func TestModelsRequiresMasterKey(t *testing.T) {
	server := NewServer(config.Config{MasterKey: "master-key", Models: []config.Model{{Name: "gpt-test", Model: "openai/gpt-test"}}})
	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestChatCompletionsAcceptsAllowedVirtualKey(t *testing.T) {
	server := NewServerWithVirtualKeyValidator(
		config.Config{Models: []config.Model{
			{Name: "gateway-model", Model: "openai/gpt-5"},
			{Name: "other-model", Model: "openai/gpt-5-mini"},
		}},
		stubChatCompleter{},
		stubVirtualKeyValidator{},
	)
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gateway-model","messages":[]}`))
	request.Header.Set("Authorization", "Bearer sk-virtual-key")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
}

func TestChatCompletionsRejectsVirtualKeyForOtherModel(t *testing.T) {
	server := NewServerWithVirtualKeyValidator(
		config.Config{Models: []config.Model{
			{Name: "gateway-model", Model: "openai/gpt-5"},
			{Name: "other-model", Model: "openai/gpt-5-mini"},
		}},
		stubChatCompleter{},
		stubVirtualKeyValidator{},
	)
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"other-model","messages":[]}`))
	request.Header.Set("Authorization", "Bearer sk-virtual-key")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestModelsReturnsConfiguredModels(t *testing.T) {
	server := NewServer(config.Config{MasterKey: "master-key", Models: []config.Model{{Name: "gpt-test", Model: "openai/gpt-test"}}})
	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	request.Header.Set("Authorization", "Bearer master-key")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if body := response.Body.String(); body != "{\"object\":\"list\",\"data\":[{\"id\":\"gpt-test\",\"object\":\"model\",\"created\":0,\"owned_by\":\"openai\"}]}\n" {
		t.Fatalf("body = %s", body)
	}
}

func TestHealthDoesNotRequireAuthentication(t *testing.T) {
	server := NewServer(config.Config{MasterKey: "master-key"})
	request := httptest.NewRequest(http.MethodGet, "/health/liveliness", nil)
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
}

func TestHandlerPreservesRequestID(t *testing.T) {
	server := NewServer(config.Config{})
	request := httptest.NewRequest(http.MethodGet, "/health/liveliness", nil)
	request.Header.Set("X-Request-Id", "request-123")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Header().Get("X-Request-Id") != "request-123" {
		t.Fatalf("request id = %q", response.Header().Get("X-Request-Id"))
	}
}

func TestReadinessReportsUnavailableDependency(t *testing.T) {
	server := NewServerWithRuntime(config.Config{}, nil, nil, nil, nil, failingReadinessCheck{})
	request := httptest.NewRequest(http.MethodGet, "/health/readiness", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}

func TestChatCompletionsForwardsConfiguredDeployment(t *testing.T) {
	server := NewServer(
		config.Config{MasterKey: "master-key", Models: []config.Model{{Name: "gateway-model", Model: "openai/gpt-5"}}},
		stubChatCompleter{},
	)
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gateway-model","messages":[]}`))
	request.Header.Set("Authorization", "Bearer master-key")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if body := response.Body.String(); body != `{"id":"chatcmpl-test"}` {
		t.Fatalf("body = %s", body)
	}
}

func TestChatCompletionsRecordsUsage(t *testing.T) {
	recorder := &recordingUsageRecorder{}
	server := NewServerWithDependencies(
		config.Config{MasterKey: "master-key", Models: []config.Model{{Name: "gateway-model", Model: "openai/gpt-5"}}},
		usageChatCompleter{}, nil, recorder,
	)
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gateway-model","messages":[]}`))
	request.Header.Set("Authorization", "Bearer master-key")
	server.Handler().ServeHTTP(httptest.NewRecorder(), request)
	if len(recorder.records) != 1 || recorder.records[0].Usage.TotalTokens != 5 {
		t.Fatalf("records = %#v", recorder.records)
	}
}

func TestChatCompletionsReturnsCachedResponse(t *testing.T) {
	cache := &memoryResponseCache{values: map[string][]byte{}}
	server := NewServerWithRuntime(config.Config{MasterKey: "master-key", Models: []config.Model{{Name: "gateway-model", Model: "openai/gpt-5"}}}, usageChatCompleter{}, nil, nil, nil).WithResponseCache(cache)
	for requestNumber := 0; requestNumber < 2; requestNumber++ {
		request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gateway-model","messages":[]}`))
		request.Header.Set("Authorization", "Bearer master-key")
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("request %d status=%d", requestNumber, response.Code)
		}
		if requestNumber == 1 && response.Header().Get("X-LiteLLM-Cache") != "hit" {
			t.Fatalf("cache header = %q", response.Header().Get("X-LiteLLM-Cache"))
		}
	}
}

func TestChatCompletionsAppliesVirtualKeyRateLimit(t *testing.T) {
	limit := int64(1)
	server := NewServerWithRuntime(
		config.Config{Models: []config.Model{{Name: "gateway-model", Model: "openai/gpt-5"}}}, stubChatCompleter{}, limitedVirtualKeyValidator{limit: limit}, nil, denyingLimiter{},
	)
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gateway-model","messages":[]}`))
	request.Header.Set("Authorization", "Bearer sk-virtual-key")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusTooManyRequests)
	}
}

func TestChatCompletionsRoundRobinsConfiguredDeployments(t *testing.T) {
	server := NewServer(
		config.Config{MasterKey: "master-key", Models: []config.Model{
			{Name: "gateway-model", Model: "openai/gpt-5", APIBase: "https://one.example"},
			{Name: "gateway-model", Model: "openai/gpt-5", APIBase: "https://two.example"},
		}},
		deploymentCapturingCompleter{bases: make(chan string, 2)},
	)
	for range 2 {
		request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gateway-model","messages":[]}`))
		request.Header.Set("Authorization", "Bearer master-key")
		server.Handler().ServeHTTP(httptest.NewRecorder(), request)
	}
	completer := server.chatCompleter.(deploymentCapturingCompleter)
	if first, second := <-completer.bases, <-completer.bases; first != "https://one.example" || second != "https://two.example" {
		t.Fatalf("deployment bases = %q, %q, want round robin", first, second)
	}
}

type stubChatCompleter struct{}

type deploymentCapturingCompleter struct {
	bases chan string
}

type usageChatCompleter struct{}

func (usageChatCompleter) ChatCompletion(context.Context, config.Model, []byte) (providers.Response, error) {
	return providers.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`))}, nil
}

type recordingUsageRecorder struct{ records []usage.Record }

func (recorder *recordingUsageRecorder) Insert(_ context.Context, record usage.Record) error {
	recorder.records = append(recorder.records, record)
	return nil
}

type limitedVirtualKeyValidator struct{ limit int64 }

func (validator limitedVirtualKeyValidator) Validate(_ context.Context, _ string, _ string) (auth.VirtualKey, error) {
	return auth.VirtualKey{TokenHash: "hash", RPMLimit: &validator.limit}, nil
}

type denyingLimiter struct{}

func (denyingLimiter) Allow(context.Context, string, int64, time.Duration) (bool, error) {
	return false, nil
}

type failingReadinessCheck struct{}

func (failingReadinessCheck) Ping(context.Context) error { return errors.New("unavailable") }

type memoryResponseCache struct{ values map[string][]byte }

func (cache *memoryResponseCache) Get(_ context.Context, key string) ([]byte, error) {
	value, found := cache.values[key]
	if !found {
		return nil, errors.New("cache miss")
	}
	return value, nil
}
func (cache *memoryResponseCache) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	cache.values[key] = append([]byte(nil), value...)
	return nil
}

func (completer deploymentCapturingCompleter) ChatCompletion(_ context.Context, deployment config.Model, _ []byte) (providers.Response, error) {
	completer.bases <- deployment.APIBase
	return providers.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
}

type stubVirtualKeyValidator struct{}

func (stubVirtualKeyValidator) Validate(_ context.Context, rawKey string, model string) (auth.VirtualKey, error) {
	if rawKey != "sk-virtual-key" || (model != "" && model != "gateway-model") {
		return auth.VirtualKey{}, auth.ErrInvalidVirtualKey
	}
	return auth.VirtualKey{Models: []string{"gateway-model"}}, nil
}

func (stubChatCompleter) ChatCompletion(_ context.Context, deployment config.Model, _ []byte) (providers.Response, error) {
	if deployment.Name != "gateway-model" {
		return providers.Response{}, nil
	}
	return providers.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"chatcmpl-test"}`)),
	}, nil
}
