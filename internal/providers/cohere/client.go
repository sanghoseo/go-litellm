package cohere

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
	"github.com/BerriAI/litellm/go-proxy/internal/observability"
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
	requestBody, err := chatRequest(body, deployment.Model)
	if err != nil {
		return providers.Response{}, err
	}
	targetURL, err := chatURL(deployment.APIBase)
	if err != nil {
		return providers.Response{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(requestBody))
	if err != nil {
		return providers.Response{}, fmt.Errorf("create Cohere request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	if requestID := observability.RequestID(ctx); requestID != "" {
		request.Header.Set("X-Request-Id", requestID)
	}
	if deployment.APIKey != "" {
		request.Header.Set("Authorization", "Bearer "+deployment.APIKey)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providers.Response{}, fmt.Errorf("send Cohere request: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return providers.Response{StatusCode: response.StatusCode, Header: response.Header, Body: response.Body}, nil
	}
	defer response.Body.Close()
	converted, err := chatResponse(response.Body)
	if err != nil {
		return providers.Response{}, err
	}
	return providers.Response{StatusCode: response.StatusCode, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(converted))}, nil
}

func (Client) CreateResponse(context.Context, config.Model, []byte) (providers.Response, error) {
	return providers.Response{}, providers.ErrProviderNotConfigured
}

func (Client) CreateEmbedding(context.Context, config.Model, []byte) (providers.Response, error) {
	return providers.Response{}, providers.ErrProviderNotConfigured
}

func chatURL(apiBase string) (string, error) {
	if apiBase == "" {
		apiBase = "https://api.cohere.com/v2"
	}
	parsed, err := url.Parse(apiBase)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid Cohere api_base %q", apiBase)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/chat"
	return parsed.String(), nil
}

func chatRequest(body []byte, configuredModel string) ([]byte, error) {
	payload := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode chat completion request: %w", err)
	}
	if rawStream, found := payload["stream"]; found && string(rawStream) == "true" {
		return nil, fmt.Errorf("Cohere streaming chat is not supported")
	}
	model := strings.TrimPrefix(configuredModel, "cohere/")
	encodedModel, err := json.Marshal(model)
	if err != nil {
		return nil, fmt.Errorf("encode configured model: %w", err)
	}
	payload["model"] = encodedModel
	delete(payload, "stream_options")
	return json.Marshal(payload)
}

func chatResponse(body io.Reader) ([]byte, error) {
	var response struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Message struct {
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
		Usage        struct {
			Tokens struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(body).Decode(&response); err != nil {
		return nil, fmt.Errorf("decode Cohere response: %w", err)
	}
	parts := make([]string, 0, len(response.Message.Content))
	for _, content := range response.Message.Content {
		if content.Type == "text" {
			parts = append(parts, content.Text)
		}
	}
	content, err := json.Marshal(strings.Join(parts, ""))
	if err != nil {
		return nil, fmt.Errorf("encode Cohere response content: %w", err)
	}
	finishReason := strings.ToLower(response.FinishReason)
	if finishReason == "complete" {
		finishReason = "stop"
	}
	role := response.Message.Role
	if role == "" {
		role = "assistant"
	}
	return json.Marshal(proxytpes.ChatCompletionResponse{
		ID: response.ID, Object: "chat.completion", Created: time.Now().Unix(), Model: response.Model,
		Choices: []proxytpes.ChatChoice{{Index: 0, Message: proxytpes.Message{Role: role, Content: content}, FinishReason: &finishReason}},
		Usage:   &proxytpes.Usage{PromptTokens: response.Usage.Tokens.InputTokens, CompletionTokens: response.Usage.Tokens.OutputTokens, TotalTokens: response.Usage.Tokens.InputTokens + response.Usage.Tokens.OutputTokens},
	})
}
