package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/BerriAI/litellm/go-proxy/internal/config"
	"github.com/BerriAI/litellm/go-proxy/internal/observability"
	"github.com/BerriAI/litellm/go-proxy/internal/providers"
)

type Client struct{ httpClient *http.Client }

func NewClient(httpClient *http.Client) Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return Client{httpClient: httpClient}
}

func (client Client) ChatCompletion(ctx context.Context, deployment config.Model, body []byte) (providers.Response, error) {
	return client.request(ctx, deployment, body, "chat/completions")
}

func (client Client) CreateResponse(ctx context.Context, deployment config.Model, body []byte) (providers.Response, error) {
	return client.request(ctx, deployment, body, "responses")
}

func (client Client) CreateEmbedding(ctx context.Context, deployment config.Model, body []byte) (providers.Response, error) {
	return client.request(ctx, deployment, body, "embeddings")
}

func (client Client) request(ctx context.Context, deployment config.Model, body []byte, endpoint string) (providers.Response, error) {
	targetURL, err := endpointURL(deployment.APIBase, endpoint)
	if err != nil {
		return providers.Response{}, err
	}
	requestBody, err := withProviderModel(body, deployment.Model)
	if err != nil {
		return providers.Response{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(requestBody))
	if err != nil {
		return providers.Response{}, fmt.Errorf("create OpenRouter request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	if deployment.APIKey != "" {
		request.Header.Set("Authorization", "Bearer "+deployment.APIKey)
	}
	if requestID := observability.RequestID(ctx); requestID != "" {
		request.Header.Set("X-Request-Id", requestID)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providers.Response{}, fmt.Errorf("send OpenRouter request: %w", err)
	}
	return providers.Response{StatusCode: response.StatusCode, Header: response.Header, Body: response.Body}, nil
}

func endpointURL(apiBase, endpoint string) (string, error) {
	if apiBase == "" {
		apiBase = "https://openrouter.ai/api/v1"
	}
	parsed, err := url.Parse(apiBase)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid OpenRouter api_base %q", apiBase)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/" + endpoint
	return parsed.String(), nil
}

func withProviderModel(body []byte, configuredModel string) ([]byte, error) {
	payload := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode OpenRouter request: %w", err)
	}
	model := strings.TrimPrefix(configuredModel, "openrouter/")
	encodedModel, _ := json.Marshal(model)
	payload["model"] = encodedModel
	return json.Marshal(payload)
}
