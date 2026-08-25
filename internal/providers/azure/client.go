package azure

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

func (client Client) request(ctx context.Context, deployment config.Model, body []byte, operation string) (providers.Response, error) {
	targetURL, err := endpointURL(deployment.APIBase, deployment.Model, operation)
	if err != nil {
		return providers.Response{}, err
	}
	requestBody, err := withDeploymentModel(body, deployment.Model)
	if err != nil {
		return providers.Response{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(requestBody))
	if err != nil {
		return providers.Response{}, fmt.Errorf("create Azure request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	if requestID := observability.RequestID(ctx); requestID != "" {
		request.Header.Set("X-Request-Id", requestID)
	}
	if deployment.APIKey != "" {
		request.Header.Set("api-key", deployment.APIKey)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providers.Response{}, fmt.Errorf("send Azure request: %w", err)
	}
	return providers.Response{StatusCode: response.StatusCode, Header: response.Header, Body: response.Body}, nil
}

func endpointURL(apiBase string, model string, operation string) (string, error) {
	if apiBase == "" {
		return "", fmt.Errorf("Azure api_base is required")
	}
	parsed, err := url.Parse(apiBase)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid Azure api_base %q", apiBase)
	}
	_, deployment, found := strings.Cut(model, "/")
	if !found || deployment == "" {
		deployment = model
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/openai/deployments/" + url.PathEscape(deployment) + "/" + operation
	if parsed.Query().Get("api-version") == "" {
		return "", fmt.Errorf("Azure api_base must include api-version query parameter")
	}
	return parsed.String(), nil
}

func withDeploymentModel(body []byte, model string) ([]byte, error) {
	payload := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode Azure request: %w", err)
	}
	_, deployment, found := strings.Cut(model, "/")
	if !found || deployment == "" {
		deployment = model
	}
	encodedModel, err := json.Marshal(deployment)
	if err != nil {
		return nil, fmt.Errorf("encode Azure deployment: %w", err)
	}
	payload["model"] = encodedModel
	encodedPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode Azure request: %w", err)
	}
	return encodedPayload, nil
}
