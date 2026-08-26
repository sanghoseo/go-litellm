package usage

import (
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
