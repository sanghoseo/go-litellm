package anthropic

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/BerriAI/litellm/go-proxy/internal/config"
)

func TestChatCompletionConvertsMessagesAndResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/messages" || request.Header.Get("x-api-key") != "anthropic-key" {
			t.Fatalf("unexpected Anthropic request: path=%q key=%q", request.URL.Path, request.Header.Get("x-api-key"))
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		const expected = `{"max_tokens":4096,"messages":[{"content":"hello","role":"user"}],"model":"claude-test","system":"follow instructions"}`
		if string(body) != expected {
			t.Fatalf("body = %s, want %s", body, expected)
		}
		_, _ = writer.Write([]byte(`{"id":"msg_1","model":"claude-test","stop_reason":"end_turn","content":[{"type":"text","text":"hello back"}],"usage":{"input_tokens":3,"output_tokens":2}}`))
	}))
	defer upstream.Close()

	response, err := NewClient(upstream.Client()).ChatCompletion(context.Background(), config.Model{Model: "anthropic/claude-test", APIKey: "anthropic-key", APIBase: upstream.URL + "/v1"}, []byte(`{"model":"gateway","messages":[{"role":"system","content":"follow instructions"},{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatalf("ChatCompletion() error = %v", err)
	}
	defer response.Body.Close()
	converted, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read converted body: %v", err)
	}
	const expectedResponsePrefix = `{"id":"msg_1","object":"chat.completion","created":`
	if len(converted) < len(expectedResponsePrefix) || string(converted[:len(expectedResponsePrefix)]) != expectedResponsePrefix {
		t.Fatalf("converted response = %s", converted)
	}
}
