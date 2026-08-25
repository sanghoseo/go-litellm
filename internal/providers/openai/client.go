package openai

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

type Client struct {
	httpClient *http.Client
}

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

func (client Client) GenerateImage(ctx context.Context, deployment config.Model, body []byte) (providers.Response, error) {
	return client.request(ctx, deployment, body, "images/generations")
}

func (client Client) CreateSpeech(ctx context.Context, deployment config.Model, body []byte) (providers.Response, error) {
	return client.request(ctx, deployment, body, "audio/speech")
}

func (client Client) Passthrough(ctx context.Context, deployment config.Model, method, endpoint, contentType string, body []byte) (providers.Response, error) {
	targetURL, err := endpointURL(deployment.APIBase, endpoint)
	if err != nil {
		return providers.Response{}, err
	}
	request, err := http.NewRequestWithContext(ctx, method, targetURL, bytes.NewReader(body))
	if err != nil {
		return providers.Response{}, fmt.Errorf("create upstream passthrough request: %w", err)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if deployment.APIKey != "" {
		request.Header.Set("Authorization", "Bearer "+deployment.APIKey)
	}
	if requestID := observability.RequestID(ctx); requestID != "" {
		request.Header.Set("X-Request-Id", requestID)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providers.Response{}, fmt.Errorf("send upstream passthrough request: %w", err)
	}
	return providers.Response{StatusCode: response.StatusCode, Header: response.Header, Body: response.Body}, nil
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
		return providers.Response{}, fmt.Errorf("create upstream request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	if requestID := observability.RequestID(ctx); requestID != "" {
		request.Header.Set("X-Request-Id", requestID)
	}
	if deployment.APIKey != "" {
		request.Header.Set("Authorization", "Bearer "+deployment.APIKey)
	}

	response, err := client.httpClient.Do(request)
	if err != nil {
		return providers.Response{}, fmt.Errorf("send upstream request: %w", err)
	}

	return providers.Response{StatusCode: response.StatusCode, Header: response.Header, Body: response.Body}, nil
}

func endpointURL(apiBase string, endpoint string) (string, error) {
	base := apiBase
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid OpenAI api_base %q", apiBase)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/" + endpoint
	return parsed.String(), nil
}

func withProviderModel(body []byte, configuredModel string) ([]byte, error) {
	payload := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode chat completion request: %w", err)
	}

	providerModel := configuredModel
	if _, model, found := strings.Cut(configuredModel, "/"); found {
		providerModel = model
	}
	encodedModel, err := json.Marshal(providerModel)
	if err != nil {
		return nil, fmt.Errorf("encode configured model: %w", err)
	}
	payload["model"] = encodedModel

	encodedPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode chat completion request: %w", err)
	}
	return encodedPayload, nil
}
