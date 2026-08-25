package types

import "encoding/json"

type EmbeddingRequest struct {
	Model          string          `json:"model"`
	Input          json.RawMessage `json:"input"`
	EncodingFormat string          `json:"encoding_format,omitempty"`
	Dimensions     *int            `json:"dimensions,omitempty"`
	User           string          `json:"user,omitempty"`
}

type Embedding struct {
	Object    string          `json:"object"`
	Embedding json.RawMessage `json:"embedding"`
	Index     int             `json:"index"`
}

type EmbeddingResponse struct {
	Object string      `json:"object"`
	Data   []Embedding `json:"data"`
	Model  string      `json:"model,omitempty"`
	Usage  *Usage      `json:"usage,omitempty"`
}
