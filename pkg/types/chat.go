package types

import "encoding/json"

type ChatCompletionRequest struct {
	Model               string          `json:"model"`
	Messages            []Message       `json:"messages"`
	Stream              bool            `json:"stream,omitempty"`
	Temperature         *float64        `json:"temperature,omitempty"`
	MaxTokens           *int            `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int            `json:"max_completion_tokens,omitempty"`
	Tools               json.RawMessage `json:"tools,omitempty"`
	ToolChoice          json.RawMessage `json:"tool_choice,omitempty"`
}

type Message struct {
	Role             string          `json:"role"`
	Content          json.RawMessage `json:"content,omitempty"`
	Name             string          `json:"name,omitempty"`
	ToolCallID       string          `json:"tool_call_id,omitempty"`
	ToolCalls        json.RawMessage `json:"tool_calls,omitempty"`
	FunctionCall     json.RawMessage `json:"function_call,omitempty"`
	ReasoningContent string          `json:"reasoning_content,omitempty"`
}

type FunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type ChatChoice struct {
	Index        int             `json:"index"`
	Message      Message         `json:"message"`
	FinishReason *string         `json:"finish_reason"`
	Logprobs     json.RawMessage `json:"logprobs,omitempty"`
}

type ChatCompletionResponse struct {
	ID                string       `json:"id"`
	Object            string       `json:"object"`
	Created           int64        `json:"created"`
	Model             string       `json:"model,omitempty"`
	Choices           []ChatChoice `json:"choices"`
	Usage             *Usage       `json:"usage,omitempty"`
	SystemFingerprint string       `json:"system_fingerprint,omitempty"`
}

type ChatCompletionChunk struct {
	ID                string            `json:"id"`
	Object            string            `json:"object"`
	Created           int64             `json:"created"`
	Model             string            `json:"model,omitempty"`
	Choices           []StreamingChoice `json:"choices"`
	Usage             *Usage            `json:"usage,omitempty"`
	SystemFingerprint string            `json:"system_fingerprint,omitempty"`
}

type StreamingChoice struct {
	Index        int             `json:"index"`
	Delta        Delta           `json:"delta"`
	FinishReason *string         `json:"finish_reason"`
	Logprobs     json.RawMessage `json:"logprobs,omitempty"`
}

type Delta struct {
	Role             string          `json:"role,omitempty"`
	Content          string          `json:"content,omitempty"`
	ToolCalls        json.RawMessage `json:"tool_calls,omitempty"`
	FunctionCall     *FunctionCall   `json:"function_call,omitempty"`
	ReasoningContent string          `json:"reasoning_content,omitempty"`
}
