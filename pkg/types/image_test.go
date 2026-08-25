package types

import (
	"encoding/json"
	"testing"
)

func TestImageGenerationRequestMarshalsOpenAIFields(t *testing.T) {
	encoded, err := json.Marshal(ImageGenerationRequest{Model: "image-model", Prompt: "a lighthouse", Size: "1024x1024"})
	if err != nil || string(encoded) != `{"model":"image-model","prompt":"a lighthouse","size":"1024x1024"}` {
		t.Fatalf("encoded=%s err=%v", encoded, err)
	}
}
