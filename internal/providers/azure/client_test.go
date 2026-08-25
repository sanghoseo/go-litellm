package azure

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/BerriAI/litellm/go-proxy/internal/config"
)

func TestChatCompletionUsesAzureDeploymentPathAndAPIKey(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/openai/deployments/gpt-deployment/chat/completions" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		if request.URL.Query().Get("api-version") != "2025-01-01-preview" {
			t.Fatalf("api-version = %q", request.URL.Query().Get("api-version"))
		}
		if request.Header.Get("api-key") != "azure-key" {
			t.Fatalf("api-key = %q", request.Header.Get("api-key"))
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if string(body) != `{"messages":[],"model":"gpt-deployment"}` {
			t.Fatalf("body = %s", body)
		}
		_, _ = writer.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	client := NewClient(upstream.Client())
	response, err := client.ChatCompletion(context.Background(), config.Model{Model: "azure/gpt-deployment", APIKey: "azure-key", APIBase: upstream.URL + "?api-version=2025-01-01-preview"}, []byte(`{"model":"gateway-model","messages":[]}`))
	if err != nil {
		t.Fatalf("ChatCompletion() error = %v", err)
	}
	response.Body.Close()
}
