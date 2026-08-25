package gemini

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/BerriAI/litellm/go-proxy/internal/config"
)

func TestChatCompletionConvertsGeminiRequestAndResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1beta/models/gemini-test:generateContent" || request.URL.Query().Get("key") != "gemini-key" {
			t.Fatalf("unexpected request %s", request.URL.String())
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		const expected = `{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"systemInstruction":{"parts":[{"text":"follow instructions"}]}}`
		if string(body) != expected {
			t.Fatalf("body = %s", body)
		}
		_, _ = writer.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"hello back"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":2,"totalTokenCount":5}}`))
	}))
	defer upstream.Close()
	response, err := NewClient(upstream.Client()).ChatCompletion(context.Background(), config.Model{Model: "gemini/gemini-test", APIKey: "gemini-key", APIBase: upstream.URL + "/v1beta"}, []byte(`{"model":"gateway","messages":[{"role":"system","content":"follow instructions"},{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	converted, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(converted), `"content":"hello back"`) || !strings.Contains(string(converted), `"total_tokens":5`) {
		t.Fatalf("converted = %s", converted)
	}
}
