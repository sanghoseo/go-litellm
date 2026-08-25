package types

import (
	"encoding/json"
	"testing"
)

func TestSpeechRequestMarshalsOpenAIFields(t *testing.T) {
	encoded, err := json.Marshal(SpeechRequest{Model: "tts-model", Input: "hello", Voice: "alloy", ResponseFormat: "mp3"})
	if err != nil || string(encoded) != `{"model":"tts-model","input":"hello","voice":"alloy","response_format":"mp3"}` {
		t.Fatalf("encoded=%s err=%v", encoded, err)
	}
}
