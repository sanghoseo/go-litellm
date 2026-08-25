package openrouter

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/BerriAI/litellm/go-proxy/internal/config"
)

func TestChatCompletionUsesOpenRouterModelAndCredentials(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/chat/completions" || request.Header.Get("Authorization") != "Bearer router-key" {
			t.Fatalf("request path=%s auth=%s", request.URL.Path, request.Header.Get("Authorization"))
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != `{"messages":[],"model":"anthropic/claude-test"}` {
			t.Fatalf("body=%s", body)
		}
		_, _ = writer.Write([]byte(`{}`))
	}))
	defer upstream.Close()
	response, err := NewClient(upstream.Client()).ChatCompletion(context.Background(), config.Model{Model: "openrouter/anthropic/claude-test", APIKey: "router-key", APIBase: upstream.URL + "/api/v1"}, []byte(`{"model":"gateway","messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
}
