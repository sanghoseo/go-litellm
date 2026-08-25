package types

import (
	"encoding/json"
	"testing"
)

func TestEmbeddingRequestAcceptsStringAndTokenArrayInput(t *testing.T) {
	for _, input := range []string{`"hello"`, `[1,2,3]`} {
		request := EmbeddingRequest{Model: "text-embedding-3-small", Input: json.RawMessage(input)}
		encoded, err := json.Marshal(request)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		if !json.Valid(encoded) {
			t.Fatalf("invalid JSON: %s", encoded)
		}
	}
}
