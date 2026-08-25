package litellm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	proxytpes "github.com/BerriAI/litellm/go-proxy/pkg/types"
)

func TestCompletionUsesOpenAIProxyContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" || request.Header.Get("Authorization") != "Bearer client-key" {
			t.Fatalf("request path=%s authorization=%s", request.URL.Path, request.Header.Get("Authorization"))
		}
		var payload proxytpes.ChatCompletionRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil || payload.Model != "gateway-model" {
			t.Fatalf("payload=%#v err=%v", payload, err)
		}
		_, _ = writer.Write([]byte(`{"id":"chatcmpl-1","object":"chat.completion","created":1,"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`))
	}))
	defer server.Close()
	response, err := (Client{BaseURL: server.URL + "/v1", APIKey: "client-key", HTTPClient: server.Client()}).Completion(context.Background(), proxytpes.ChatCompletionRequest{Model: "gateway-model"})
	if err != nil || response.ID != "chatcmpl-1" || response.Usage.TotalTokens != 3 {
		t.Fatalf("response=%#v err=%v", response, err)
	}
}

func TestCompletionReturnsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = writer.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer server.Close()
	_, err := (Client{BaseURL: server.URL, HTTPClient: server.Client()}).Completion(context.Background(), proxytpes.ChatCompletionRequest{Model: "gateway-model"})
	apiError, ok := err.(*APIError)
	if !ok || apiError.StatusCode != http.StatusUnauthorized || apiError.Message != "bad key" {
		t.Fatalf("error=%#v", err)
	}
}
