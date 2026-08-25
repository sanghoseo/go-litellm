package gemini

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
	requestBody, err := generateContentRequest(body)
	if err != nil {
		return providers.Response{}, err
	}
	targetURL, err := generateContentURL(deployment.APIBase, deployment.Model, deployment.APIKey)
	if err != nil {
		return providers.Response{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(requestBody))
	if err != nil {
		return providers.Response{}, fmt.Errorf("create Gemini request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if requestID := observability.RequestID(ctx); requestID != "" {
		request.Header.Set("X-Request-Id", requestID)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providers.Response{}, fmt.Errorf("send Gemini request: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return providers.Response{StatusCode: response.StatusCode, Header: response.Header, Body: response.Body}, nil
	}
	defer response.Body.Close()
	converted, err := chatCompletionResponse(response.Body, deployment.Model)
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

func generateContentURL(apiBase, configuredModel, apiKey string) (string, error) {
	if apiBase == "" {
		apiBase = "https://generativelanguage.googleapis.com/v1beta"
	}
	parsed, err := url.Parse(apiBase)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid Gemini api_base %q", apiBase)
	}
	model := strings.TrimPrefix(configuredModel, "gemini/")
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/models/" + url.PathEscape(model) + ":generateContent"
	if apiKey != "" {
		query := parsed.Query()
		query.Set("key", apiKey)
		parsed.RawQuery = query.Encode()
	}
	return parsed.String(), nil
}

func generateContentRequest(body []byte) ([]byte, error) {
	var request struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
		Temperature *float64 `json:"temperature"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, fmt.Errorf("decode chat completion request: %w", err)
	}
	type part struct {
		Text string `json:"text"`
	}
	type content struct {
		Role  string `json:"role,omitempty"`
		Parts []part `json:"parts"`
	}
	contents := make([]content, 0, len(request.Messages))
	var system *content
	for _, message := range request.Messages {
		var text string
		if json.Unmarshal(message.Content, &text) != nil {
			continue
		}
		item := content{Role: message.Role, Parts: []part{{Text: text}}}
		if message.Role == "system" {
			system = &item
			continue
		}
		if message.Role == "assistant" {
			item.Role = "model"
		}
		contents = append(contents, item)
	}
	payload := map[string]any{"contents": contents}
	if system != nil {
		payload["systemInstruction"] = map[string]any{"parts": system.Parts}
	}
	if request.Temperature != nil {
		payload["generationConfig"] = map[string]any{"temperature": *request.Temperature}
	}
	return json.Marshal(payload)
}

func chatCompletionResponse(body io.Reader, configuredModel string) ([]byte, error) {
	var response struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		Usage struct {
			Prompt     int `json:"promptTokenCount"`
			Completion int `json:"candidatesTokenCount"`
			Total      int `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.NewDecoder(body).Decode(&response); err != nil {
		return nil, fmt.Errorf("decode Gemini response: %w", err)
	}
	if len(response.Candidates) == 0 {
		return nil, fmt.Errorf("Gemini response did not contain candidates")
	}
	parts := response.Candidates[0].Content.Parts
	text := make([]string, 0, len(parts))
	for _, part := range parts {
		text = append(text, part.Text)
	}
	content, _ := json.Marshal(strings.Join(text, ""))
	finishReason := strings.ToLower(response.Candidates[0].FinishReason)
	if finishReason == "stop" {
		finishReason = "stop"
	}
	model := strings.TrimPrefix(configuredModel, "gemini/")
	return json.Marshal(proxytpes.ChatCompletionResponse{ID: "chatcmpl-gemini", Object: "chat.completion", Created: time.Now().Unix(), Model: model, Choices: []proxytpes.ChatChoice{{Index: 0, Message: proxytpes.Message{Role: "assistant", Content: content}, FinishReason: &finishReason}}, Usage: &proxytpes.Usage{PromptTokens: response.Usage.Prompt, CompletionTokens: response.Usage.Completion, TotalTokens: response.Usage.Total}})
}
