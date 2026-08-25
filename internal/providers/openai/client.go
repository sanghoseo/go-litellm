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
	targetURL, err := chatCompletionsURL(deployment.APIBase)
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
	if deployment.APIKey != "" {
		request.Header.Set("Authorization", "Bearer "+deployment.APIKey)
	}

	response, err := client.httpClient.Do(request)
	if err != nil {
		return providers.Response{}, fmt.Errorf("send upstream request: %w", err)
	}

	return providers.Response{StatusCode: response.StatusCode, Header: response.Header, Body: response.Body}, nil
}

func chatCompletionsURL(apiBase string) (string, error) {
	base := apiBase
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid OpenAI api_base %q", apiBase)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/chat/completions"
	return parsed.String(), nil
}

func withProviderModel(body []byte, configuredModel string) ([]byte, error) {
	payload := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode chat completion request: %w", err)
	}

	providerModel := configuredModel
	if provider, model, found := strings.Cut(configuredModel, "/"); found && provider == "openai" {
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
