package types

import (
	"encoding/json"
	"testing"
)

func TestTextCompletionRequestMarshalsOpenAIFields(t *testing.T) {
	request := TextCompletionRequest{Model: "gpt-test", Prompt: []string{"one", "two"}, MaxTokens: 8}
	encoded, err := json.Marshal(request)
	if err != nil || string(encoded) != `{"model":"gpt-test","prompt":["one","two"],"max_tokens":8}` {
		t.Fatalf("encoded=%s err=%v", encoded, err)
	}
}
