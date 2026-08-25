package types

import "encoding/json"

type ResponsesRequest struct {
	Model        string          `json:"model"`
	Input        json.RawMessage `json:"input"`
	Instructions string          `json:"instructions,omitempty"`
	Stream       bool            `json:"stream,omitempty"`
	Tools        json.RawMessage `json:"tools,omitempty"`
}

type ResponsesResponse struct {
	ID        string          `json:"id"`
	Object    string          `json:"object"`
	CreatedAt int64           `json:"created_at,omitempty"`
	Model     string          `json:"model,omitempty"`
	Output    json.RawMessage `json:"output,omitempty"`
	Usage     *Usage          `json:"usage,omitempty"`
}
