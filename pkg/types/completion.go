package types

import "encoding/json"

// TextCompletionRequest is the OpenAI-compatible /v1/completions payload.
type TextCompletionRequest struct {
	Model       string   `json:"model"`
	Prompt      any      `json:"prompt"`
	MaxTokens   int      `json:"max_tokens,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
	Stop        any      `json:"stop,omitempty"`
	Stream      bool     `json:"stream,omitempty"`
	User        string   `json:"user,omitempty"`
}

type TextCompletionChoice struct {
	Text         string           `json:"text"`
	Index        int              `json:"index"`
	FinishReason string           `json:"finish_reason"`
	Logprobs     *json.RawMessage `json:"logprobs,omitempty"`
}

type TextCompletionResponse struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Created int64                  `json:"created"`
	Model   string                 `json:"model"`
	Choices []TextCompletionChoice `json:"choices"`
	Usage   Usage                  `json:"usage"`
}
