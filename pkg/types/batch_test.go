package types

import (
	"encoding/json"
	"testing"
)

func TestBatchCreateRequestMarshalsOpenAIFields(t *testing.T) {
	encoded, err := json.Marshal(BatchCreateRequest{InputFileID: "file-1", Endpoint: "/v1/chat/completions", CompletionWindow: "24h"})
	if err != nil || string(encoded) != `{"input_file_id":"file-1","endpoint":"/v1/chat/completions","completion_window":"24h"}` {
		t.Fatalf("encoded=%s err=%v", encoded, err)
	}
}
