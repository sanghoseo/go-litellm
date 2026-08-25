package litellm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	proxytpes "github.com/BerriAI/litellm/go-proxy/pkg/types"
)

type Client struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

type APIError struct {
	StatusCode int
	Message    string
}

func (errorValue *APIError) Error() string {
	return fmt.Sprintf("LiteLLM API error (%d): %s", errorValue.StatusCode, errorValue.Message)
}

func (client Client) Completion(ctx context.Context, request proxytpes.ChatCompletionRequest) (proxytpes.ChatCompletionResponse, error) {
	response := proxytpes.ChatCompletionResponse{}
	if err := client.post(ctx, "chat/completions", request, &response); err != nil {
		return proxytpes.ChatCompletionResponse{}, err
	}
	return response, nil
}

func (client Client) Embedding(ctx context.Context, request proxytpes.EmbeddingRequest) (proxytpes.EmbeddingResponse, error) {
	response := proxytpes.EmbeddingResponse{}
	if err := client.post(ctx, "embeddings", request, &response); err != nil {
		return proxytpes.EmbeddingResponse{}, err
	}
	return response, nil
}

func (client Client) post(ctx context.Context, endpoint string, payload any, output any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}
	endpointURL, err := client.endpointURL(endpoint)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if client.APIKey != "" {
		request.Header.Set("Authorization", "Bearer "+client.APIKey)
	}
	httpClient := client.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		var errorResponse struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(body, &errorResponse)
		message := errorResponse.Error.Message
		if message == "" {
			message = string(body)
		}
		return &APIError{StatusCode: response.StatusCode, Message: message}
	}
	if err := json.Unmarshal(body, output); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (client Client) endpointURL(endpoint string) (string, error) {
	base := client.BaseURL
	if base == "" {
		base = "http://localhost:4000/v1"
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid LiteLLM base URL %q", client.BaseURL)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/" + endpoint
	return parsed.String(), nil
}
