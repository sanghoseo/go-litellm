package types

import (
	"encoding/json"
	"testing"
)

func TestChatCompletionResponseJSONContract(t *testing.T) {
	finishReason := "stop"
	response := ChatCompletionResponse{
		ID:      "chatcmpl-1",
		Object:  "chat.completion",
		Created: 1,
		Model:   "gpt-test",
		Choices: []ChatChoice{{Index: 0, Message: Message{Role: "assistant", Content: json.RawMessage(`"hello"`)}, FinishReason: &finishReason}},
		Usage:   &Usage{PromptTokens: 2, CompletionTokens: 1, TotalTokens: 3},
	}

	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	const expected = `{"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"gpt-test","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`
	if string(encoded) != expected {
		t.Fatalf("JSON = %s, want %s", encoded, expected)
	}
}
