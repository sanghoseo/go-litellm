package usage

import "testing"

func TestUsageFromOpenAIResponse(t *testing.T) {
	usage, err := UsageFromOpenAIResponse([]byte(`{"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`))
	if err != nil {
		t.Fatalf("UsageFromOpenAIResponse() error = %v", err)
	}
	if usage.TotalTokens != 5 || usage.PromptTokens != 2 || usage.CompletionTokens != 3 {
		t.Fatalf("usage = %#v", usage)
	}
}
