package cohere

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/BerriAI/litellm/go-proxy/internal/config"
)

func TestChatCompletionConvertsCohereResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v2/chat" || request.Header.Get("Authorization") != "Bearer cohere-key" {
			t.Fatalf("unexpected Cohere request: path=%q authorization=%q", request.URL.Path, request.Header.Get("Authorization"))
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		const expected = `{"messages":[{"role":"user","content":"hello"}],"model":"command-r"}`
		if string(body) != expected {
			t.Fatalf("body = %s, want %s", body, expected)
		}
		_, _ = writer.Write([]byte(`{"id":"chat_1","model":"command-r","message":{"role":"assistant","content":[{"type":"text","text":"hello back"}]},"finish_reason":"COMPLETE","usage":{"tokens":{"input_tokens":3,"output_tokens":2}}}`))
	}))
	defer upstream.Close()

	response, err := NewClient(upstream.Client()).ChatCompletion(context.Background(), config.Model{Model: "cohere/command-r", APIKey: "cohere-key", APIBase: upstream.URL + "/v2"}, []byte(`{"model":"gateway","messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatalf("ChatCompletion() error = %v", err)
	}
	defer response.Body.Close()
	converted, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read converted body: %v", err)
	}
	var result struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(converted, &result); err != nil {
		t.Fatalf("decode converted response: %v", err)
	}
	if result.Model != "command-r" || len(result.Choices) != 1 || result.Choices[0].Message.Content != "hello back" || result.Choices[0].FinishReason != "stop" || result.Usage.PromptTokens != 3 || result.Usage.CompletionTokens != 2 {
		t.Fatalf("unexpected converted response: %s", converted)
	}
}

func TestChatCompletionConvertsCohereStream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "event: message-start\ndata: {\"id\":\"chat_1\",\"model\":\"command-r\"}\n\n")
		_, _ = io.WriteString(writer, "event: content-delta\ndata: {\"delta\":{\"message\":{\"content\":{\"text\":\"hello\"}}}}\n\n")
		_, _ = io.WriteString(writer, "event: message-end\ndata: {\"delta\":{\"finish_reason\":\"COMPLETE\"}}\n\n")
	}))
	defer upstream.Close()

	response, err := NewClient(upstream.Client()).ChatCompletion(context.Background(), config.Model{Model: "cohere/command-r", APIBase: upstream.URL + "/v2"}, []byte(`{"model":"gateway","stream":true,"messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatalf("ChatCompletion() error = %v", err)
	}
	defer response.Body.Close()
	stream, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	if !strings.Contains(string(stream), `"role":"assistant"`) || !strings.Contains(string(stream), `"content":"hello"`) || !strings.Contains(string(stream), `"finish_reason":"stop"`) || !strings.HasSuffix(string(stream), "data: [DONE]\n\n") {
		t.Fatalf("converted stream = %s", stream)
	}
}

func TestCreateEmbeddingConvertsCohereResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v2/embed" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		const expected = `{"input_type":"search_document","model":"embed-v4","texts":["one","two"]}`
		if string(body) != expected {
			t.Fatalf("body = %s, want %s", body, expected)
		}
		_, _ = writer.Write([]byte(`{"embeddings":{"float":[[0.1,0.2],[0.3,0.4]]},"meta":{"billed_units":{"input_tokens":5}}}`))
	}))
	defer upstream.Close()

	response, err := NewClient(upstream.Client()).CreateEmbedding(context.Background(), config.Model{Model: "cohere/embed-v4", APIBase: upstream.URL + "/v2"}, []byte(`{"model":"gateway","input":["one","two"]}`))
	if err != nil {
		t.Fatalf("CreateEmbedding() error = %v", err)
	}
	defer response.Body.Close()
	converted, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read converted body: %v", err)
	}
	const expectedResponse = `{"object":"list","data":[{"object":"embedding","embedding":[0.1,0.2],"index":0},{"object":"embedding","embedding":[0.3,0.4],"index":1}],"model":"embed-v4","usage":{"prompt_tokens":5,"completion_tokens":0,"total_tokens":5}}`
	if string(converted) != expectedResponse {
		t.Fatalf("converted = %s, want %s", converted, expectedResponse)
	}
}

func TestRerankUsesConfiguredModel(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v2/rerank" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		body, _ := io.ReadAll(request.Body)
		const expected = `{"documents":["one"],"model":"rerank-v3","query":"hello"}`
		if string(body) != expected {
			t.Fatalf("body = %s, want %s", body, expected)
		}
		_, _ = writer.Write([]byte(`{"results":[{"index":0,"relevance_score":0.99}]}`))
	}))
	defer upstream.Close()

	response, err := NewClient(upstream.Client()).Rerank(context.Background(), config.Model{Model: "cohere/rerank-v3", APIBase: upstream.URL + "/v2"}, []byte(`{"model":"gateway","query":"hello","documents":["one"]}`))
	if err != nil {
		t.Fatalf("Rerank() error = %v", err)
	}
	defer response.Body.Close()
	converted, _ := io.ReadAll(response.Body)
	if string(converted) != `{"results":[{"index":0,"relevance_score":0.99}]}` {
		t.Fatalf("response = %s", converted)
	}
}
