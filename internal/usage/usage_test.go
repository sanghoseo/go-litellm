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

func TestUsageFromSSEStreamResponse(t *testing.T) {
	body := []byte(`data: {"choices":[{"delta":{"content":"h"}}]}

data: {"choices":[{"delta":{"content":"i"}}]}

data: {"choices":[],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}

data: [DONE]
`)
	usage, err := UsageFromOpenAIResponse(body)
	if err != nil {
		t.Fatalf("UsageFromOpenAIResponse() error = %v", err)
	}
	if usage.TotalTokens != 5 || usage.PromptTokens != 2 || usage.CompletionTokens != 3 {
		t.Fatalf("usage = %#v, want total 5 from stream usage chunk", usage)
	}
}

func TestUsageFromSSEStreamWithoutUsageIsEmpty(t *testing.T) {
	body := []byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n")
	usage, err := UsageFromOpenAIResponse(body)
	if err != nil {
		t.Fatalf("UsageFromOpenAIResponse() error = %v", err)
	}
	if usage.TotalTokens != 0 {
		t.Fatalf("usage = %#v, want empty when stream has no usage", usage)
	}
}
