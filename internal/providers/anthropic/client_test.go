package anthropic

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestChatCompletionConvertsAnthropicStream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-test\"}}\n\n")
		_, _ = io.WriteString(writer, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n")
		_, _ = io.WriteString(writer, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n")
		_, _ = io.WriteString(writer, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer upstream.Close()

	response, err := NewClient(upstream.Client()).ChatCompletion(context.Background(), config.Model{Model: "anthropic/claude-test", APIBase: upstream.URL}, []byte(`{"model":"gateway","stream":true,"messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatalf("ChatCompletion() error = %v", err)
	}
	defer response.Body.Close()
	stream, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	if !strings.Contains(string(stream), `"content":"hello"`) || !strings.HasSuffix(string(stream), "data: [DONE]\n\n") {
		t.Fatalf("converted stream = %s", stream)
	}
}

func TestChatCompletionConvertsTools(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), `"input_schema":{"type":"object","properties":{"city":{"type":"string"}}}`) || strings.Contains(string(body), `"function"`) {
			t.Fatalf("tools were not converted: %s", body)
		}
		_, _ = writer.Write([]byte(`{"id":"msg_1","model":"claude-test","stop_reason":"tool_use","content":[{"type":"tool_use","id":"toolu_1","name":"weather","input":{"city":"Seoul"}}],"usage":{"input_tokens":3,"output_tokens":2}}`))
	}))
	defer upstream.Close()

	response, err := NewClient(upstream.Client()).ChatCompletion(context.Background(), config.Model{Model: "anthropic/claude-test", APIBase: upstream.URL + "/v1"}, []byte(`{"model":"gateway","messages":[{"role":"user","content":"weather"}],"tools":[{"type":"function","function":{"name":"weather","description":"Get weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	converted, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(converted), `"finish_reason":"tool_calls"`) || !strings.Contains(string(converted), `"name":"weather"`) || !strings.Contains(string(converted), `"arguments":"{\"city\":\"Seoul\"}"`) {
		t.Fatalf("converted = %s", converted)
	}
}

func TestMessagesRequestConvertsToolRoundTrip(t *testing.T) {
	converted, err := messagesRequest([]byte(`{"messages":[{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"weather","arguments":"{\"city\":\"Seoul\"}"}}]},{"role":"tool","tool_call_id":"call_1","content":"sunny"}]}`), "anthropic/claude-test")
	if err != nil {
		t.Fatalf("messagesRequest() error = %v", err)
	}
	if !strings.Contains(string(converted), `"type":"tool_use"`) || !strings.Contains(string(converted), `"input":{"city":"Seoul"}`) || !strings.Contains(string(converted), `"type":"tool_result"`) || !strings.Contains(string(converted), `"tool_use_id":"call_1"`) || strings.Contains(string(converted), `"tool_calls"`) {
		t.Fatalf("converted request = %s", converted)
	}
}
