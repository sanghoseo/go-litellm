package cohere

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestChatRequestRejectsStreaming(t *testing.T) {
	if _, err := chatRequest([]byte(`{"stream":true}`), "cohere/command-r"); err == nil {
		t.Fatal("chatRequest() error = nil, want streaming error")
	}
}
