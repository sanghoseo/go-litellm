package usage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	proxytpes "github.com/BerriAI/litellm/go-proxy/pkg/types"
)

type Recorder interface {
	Insert(context.Context, Record) error
}

type Log struct {
	RequestID   string    `json:"request_id"`
	CallType    string    `json:"call_type"`
	APIKeyHash  string    `json:"api_key"`
	TotalTokens int       `json:"total_tokens"`
	Model       string    `json:"model"`
	Provider    string    `json:"custom_llm_provider"`
	StartedAt   time.Time `json:"start_time"`
	CompletedAt time.Time `json:"end_time"`
	Status      string    `json:"status"`
}

type LogReader interface {
	List(context.Context, int) ([]Log, error)
}

type Record struct {
	RequestID   string
	CallType    string
	APIKeyHash  string
	Model       string
	Provider    string
	APIBase     string
	StartedAt   time.Time
	CompletedAt time.Time
	Usage       proxytpes.Usage
	Status      string
	Cost        float64
}

func UsageFromOpenAIResponse(body []byte) (proxytpes.Usage, error) {
	if isSSEBody(body) {
		return usageFromSSEBody(body), nil
	}
	payload := struct {
		Usage *proxytpes.Usage `json:"usage"`
	}{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return proxytpes.Usage{}, fmt.Errorf("decode OpenAI usage: %w", err)
	}
	if payload.Usage == nil {
		return proxytpes.Usage{}, nil
	}
	return *payload.Usage, nil
}

func isSSEBody(body []byte) bool {
	trimmed := bytes.TrimLeft(body, " \t\r\n")
	return bytes.HasPrefix(trimmed, []byte("data:")) || bytes.HasPrefix(trimmed, []byte("event:"))
}

func usageFromSSEBody(body []byte) proxytpes.Usage {
	var usage proxytpes.Usage
	found := false
	for _, line := range bytes.Split(body, []byte("\n")) {
		line = bytes.TrimRight(line, " \t\r")
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
			continue
		}
		chunk := struct {
			Usage *proxytpes.Usage `json:"usage"`
		}{}
		if err := json.Unmarshal(payload, &chunk); err != nil {
			continue
		}
		if chunk.Usage != nil {
			usage = *chunk.Usage
			found = true
		}
	}
	if !found {
		return proxytpes.Usage{}
	}
	return usage
}
