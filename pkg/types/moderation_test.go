package types

import (
	"encoding/json"
	"testing"
)

func TestModerationRequestMarshalsInput(t *testing.T) {
	encoded, err := json.Marshal(ModerationRequest{Model: "omni-moderation", Input: []string{"one", "two"}})
	if err != nil || string(encoded) != `{"model":"omni-moderation","input":["one","two"]}` {
		t.Fatalf("encoded=%s err=%v", encoded, err)
	}
}
