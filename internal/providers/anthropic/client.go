package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/BerriAI/litellm/go-proxy/internal/config"
	"github.com/BerriAI/litellm/go-proxy/internal/providers"
	proxytpes "github.com/BerriAI/litellm/go-proxy/pkg/types"
)

type Client struct{ httpClient *http.Client }

func NewClient(httpClient *http.Client) Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return Client{httpClient: httpClient}
}

func (client Client) ChatCompletion(ctx context.Context, deployment config.Model, body []byte) (providers.Response, error) {
	requestBody, err := messagesRequest(body, deployment.Model)
	if err != nil {
		return providers.Response{}, err
	}
	targetURL, err := messagesURL(deployment.APIBase)
	if err != nil {
		return providers.Response{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(requestBody))
	if err != nil {
		return providers.Response{}, fmt.Errorf("create Anthropic request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("anthropic-version", "2023-06-01")
	if deployment.APIKey != "" {
		request.Header.Set("x-api-key", deployment.APIKey)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providers.Response{}, fmt.Errorf("send Anthropic request: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return providers.Response{StatusCode: response.StatusCode, Header: response.Header, Body: response.Body}, nil
	}
	defer response.Body.Close()
	converted, err := chatCompletionResponse(response.Body)
	if err != nil {
		return providers.Response{}, err
	}
	return providers.Response{StatusCode: response.StatusCode, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(converted))}, nil
}

func (client Client) CreateResponse(context.Context, config.Model, []byte) (providers.Response, error) {
	return providers.Response{}, providers.ErrProviderNotConfigured
}

func (client Client) CreateEmbedding(context.Context, config.Model, []byte) (providers.Response, error) {
	return providers.Response{}, providers.ErrProviderNotConfigured
}

func messagesURL(apiBase string) (string, error) {
	if apiBase == "" {
		apiBase = "https://api.anthropic.com/v1"
	}
	parsed, err := url.Parse(apiBase)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid Anthropic api_base %q", apiBase)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/messages"
	return parsed.String(), nil
}

func messagesRequest(body []byte, configuredModel string) ([]byte, error) {
	payload := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode chat completion request: %w", err)
	}
	model := strings.TrimPrefix(configuredModel, "anthropic/")
	encodedModel, _ := json.Marshal(model)
	payload["model"] = encodedModel
	if _, found := payload["max_tokens"]; !found {
		payload["max_tokens"] = json.RawMessage("4096")
	}
	var messages []map[string]json.RawMessage
	if rawMessages, found := payload["messages"]; found {
		if err := json.Unmarshal(rawMessages, &messages); err != nil {
			return nil, fmt.Errorf("decode messages: %w", err)
		}
	}
	filtered := make([]map[string]json.RawMessage, 0, len(messages))
	for _, message := range messages {
		var role string
		_ = json.Unmarshal(message["role"], &role)
		if role == "system" {
			if _, alreadySet := payload["system"]; !alreadySet {
				payload["system"] = message["content"]
			}
			continue
		}
		filtered = append(filtered, message)
	}
	payload["messages"], _ = json.Marshal(filtered)
	delete(payload, "stream_options")
	return json.Marshal(payload)
}

func chatCompletionResponse(body io.Reader) ([]byte, error) {
	var response struct {
		ID         string `json:"id"`
		Model      string `json:"model"`
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(body).Decode(&response); err != nil {
		return nil, fmt.Errorf("decode Anthropic response: %w", err)
	}
	parts := make([]string, 0, len(response.Content))
	for _, content := range response.Content {
		if content.Type == "text" {
			parts = append(parts, content.Text)
		}
	}
	content, _ := json.Marshal(strings.Join(parts, ""))
	finishReason := response.StopReason
	if finishReason == "end_turn" {
		finishReason = "stop"
	}
	return json.Marshal(proxytpes.ChatCompletionResponse{
		ID: response.ID, Object: "chat.completion", Created: time.Now().Unix(), Model: response.Model,
		Choices: []proxytpes.ChatChoice{{Index: 0, Message: proxytpes.Message{Role: "assistant", Content: content}, FinishReason: &finishReason}},
		Usage:   &proxytpes.Usage{PromptTokens: response.Usage.InputTokens, CompletionTokens: response.Usage.OutputTokens, TotalTokens: response.Usage.InputTokens + response.Usage.OutputTokens},
	})
}
