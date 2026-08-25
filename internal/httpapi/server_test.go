package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/BerriAI/litellm/go-proxy/internal/config"
	"github.com/BerriAI/litellm/go-proxy/internal/providers"
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

type stubChatCompleter struct{}

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
