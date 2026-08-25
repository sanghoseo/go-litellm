package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/BerriAI/litellm/go-proxy/internal/config"
	"github.com/BerriAI/litellm/go-proxy/internal/observability"
)

func TestChatCompletionUsesDeploymentCredentialsAndModel(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %q, want /v1/chat/completions", request.URL.Path)
		}
		if authorization := request.Header.Get("Authorization"); authorization != "Bearer upstream-key" {
			t.Fatalf("authorization = %q, want upstream credential", authorization)
		}
		if request.Header.Get("X-Request-Id") != "request-123" {
			t.Fatalf("request id = %q", request.Header.Get("X-Request-Id"))
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if string(body) != "{\"messages\":[],\"model\":\"gpt-5\"}" {
			t.Fatalf("body = %s", body)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"id":"chatcmpl-test"}`))
	}))
	defer upstream.Close()

	client := NewClient(upstream.Client())
	response, err := client.ChatCompletion(observability.WithRequestID(context.Background(), "request-123"), config.Model{
		Name:    "gateway-model",
		Model:   "openai/gpt-5",
		APIKey:  "upstream-key",
		APIBase: upstream.URL + "/v1",
	}, []byte(`{"model":"gateway-model","messages":[]}`))
	if err != nil {
		t.Fatalf("ChatCompletion() error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}
}

func TestChatCompletionStripsOpenAICompatibleProviderPrefix(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if string(body) != `{"messages":[],"model":"llama-3.3-70b-versatile"}` {
			t.Fatalf("body = %s", body)
		}
		_, _ = writer.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	response, err := NewClient(upstream.Client()).ChatCompletion(context.Background(), config.Model{
		Model:   "groq/llama-3.3-70b-versatile",
		APIBase: upstream.URL,
	}, []byte(`{"model":"gateway-model","messages":[]}`))
	if err != nil {
		t.Fatalf("ChatCompletion() error = %v", err)
	}
	defer response.Body.Close()
}

func TestGenerateImageUsesOpenAIImagesEndpoint(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/images/generations" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		_, _ = writer.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	response, err := NewClient(upstream.Client()).GenerateImage(context.Background(), config.Model{Model: "openai/gpt-image-1", APIBase: upstream.URL + "/v1"}, []byte(`{"model":"gateway-model","prompt":"a lighthouse"}`))
	if err != nil {
		t.Fatalf("GenerateImage() error = %v", err)
	}
	defer response.Body.Close()
}
