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

func TestCreateEmbeddingConvertsGeminiResponse(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		if request.URL.Path != "/v1beta/models/text-embedding-004:embedContent" || request.URL.Query().Get("key") != "gemini-key" {
			t.Fatalf("unexpected Gemini embedding request: path=%q query=%q", request.URL.Path, request.URL.RawQuery)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if string(body) != `{"content":{"parts":[{"text":"hello"}]}}` && string(body) != `{"content":{"parts":[{"text":"world"}]}}` {
			t.Fatalf("body = %s", body)
		}
		_, _ = writer.Write([]byte(`{"embedding":{"values":[0.1,0.2]}}`))
	}))
	defer upstream.Close()

	response, err := NewClient(upstream.Client()).CreateEmbedding(context.Background(), config.Model{Model: "gemini/text-embedding-004", APIKey: "gemini-key", APIBase: upstream.URL + "/v1beta"}, []byte(`{"model":"gateway","input":["hello","world"]}`))
	if err != nil {
		t.Fatalf("CreateEmbedding() error = %v", err)
	}
	defer response.Body.Close()
	converted, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read converted body: %v", err)
	}
	const expected = `{"object":"list","data":[{"object":"embedding","embedding":[0.1,0.2],"index":0},{"object":"embedding","embedding":[0.1,0.2],"index":1}],"model":"text-embedding-004","usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`
	if string(converted) != expected || calls != 2 {
		t.Fatalf("converted = %s, calls = %d", converted, calls)
	}
}

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

func TestChatCompletionConvertsGeminiStream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1beta/models/gemini-test:streamGenerateContent" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello\"}]}}]}\n\n")
		_, _ = io.WriteString(writer, "data: {\"candidates\":[{\"finishReason\":\"STOP\"}]}\n\n")
	}))
	defer upstream.Close()

	response, err := NewClient(upstream.Client()).ChatCompletion(context.Background(), config.Model{Model: "gemini/gemini-test", APIBase: upstream.URL + "/v1beta"}, []byte(`{"model":"gateway","stream":true,"messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	stream, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stream), `"role":"assistant"`) || !strings.Contains(string(stream), `"content":"hello"`) || !strings.Contains(string(stream), `"finish_reason":"stop"`) || !strings.HasSuffix(string(stream), "data: [DONE]\n\n") {
		t.Fatalf("stream = %s", stream)
	}
}
