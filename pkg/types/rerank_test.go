package types

import (
	"encoding/json"
	"testing"
)

func TestRerankRequestMarshalsOpenAICompatibleFields(t *testing.T) {
	encoded, err := json.Marshal(RerankRequest{Model: "rerank", Query: "q", Documents: []string{"doc"}, TopN: 1})
	if err != nil || string(encoded) != `{"model":"rerank","query":"q","documents":["doc"],"top_n":1}` {
		t.Fatalf("encoded=%s err=%v", encoded, err)
	}
}
