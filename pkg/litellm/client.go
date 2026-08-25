package litellm

import (
	"bufio"
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

type Stream struct {
	Chunks <-chan proxytpes.ChatCompletionChunk
	Errors <-chan error
}

type TextStream struct {
	Chunks <-chan proxytpes.TextCompletionChunk
	Errors <-chan error
}

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

func (client Client) TextCompletion(ctx context.Context, request proxytpes.TextCompletionRequest) (proxytpes.TextCompletionResponse, error) {
	response := proxytpes.TextCompletionResponse{}
	if err := client.post(ctx, "completions", request, &response); err != nil {
		return proxytpes.TextCompletionResponse{}, err
	}
	return response, nil
}

func (client Client) Moderation(ctx context.Context, request proxytpes.ModerationRequest) (proxytpes.ModerationResponse, error) {
	response := proxytpes.ModerationResponse{}
	if err := client.post(ctx, "moderations", request, &response); err != nil {
		return proxytpes.ModerationResponse{}, err
	}
	return response, nil
}

func (client Client) Rerank(ctx context.Context, request proxytpes.RerankRequest) (proxytpes.RerankResponse, error) {
	response := proxytpes.RerankResponse{}
	if err := client.post(ctx, "rerank", request, &response); err != nil {
		return proxytpes.RerankResponse{}, err
	}
	return response, nil
}

func (client Client) ImageGeneration(ctx context.Context, request proxytpes.ImageGenerationRequest) (proxytpes.ImageGenerationResponse, error) {
	response := proxytpes.ImageGenerationResponse{}
	if err := client.post(ctx, "images/generations", request, &response); err != nil {
		return proxytpes.ImageGenerationResponse{}, err
	}
	return response, nil
}

func (client Client) Speech(ctx context.Context, request proxytpes.SpeechRequest) ([]byte, error) {
	return client.postBytes(ctx, "audio/speech", request)
}

func (client Client) ListFiles(ctx context.Context) (proxytpes.FileListResponse, error) {
	response := proxytpes.FileListResponse{}
	if err := client.get(ctx, "files", &response); err != nil {
		return proxytpes.FileListResponse{}, err
	}
	return response, nil
}

func (client Client) RetrieveFile(ctx context.Context, fileID string) (proxytpes.FileObject, error) {
	response := proxytpes.FileObject{}
	if err := client.get(ctx, "files/"+url.PathEscape(fileID), &response); err != nil {
		return proxytpes.FileObject{}, err
	}
	return response, nil
}

func (client Client) DeleteFile(ctx context.Context, fileID string) (proxytpes.FileDeleteResponse, error) {
	response := proxytpes.FileDeleteResponse{}
	if err := client.requestJSON(ctx, http.MethodDelete, "files/"+url.PathEscape(fileID), nil, &response); err != nil {
		return proxytpes.FileDeleteResponse{}, err
	}
	return response, nil
}

func (client Client) TextCompletionStream(ctx context.Context, request proxytpes.TextCompletionRequest) (TextStream, error) {
	request.Stream = true
	encoded, err := json.Marshal(request)
	if err != nil {
		return TextStream{}, fmt.Errorf("encode request: %w", err)
	}
	endpointURL, err := client.endpointURL("completions")
	if err != nil {
		return TextStream{}, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL, bytes.NewReader(encoded))
	if err != nil {
		return TextStream{}, fmt.Errorf("create request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "text/event-stream")
	if client.APIKey != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+client.APIKey)
	}
	httpClient := client.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	response, err := httpClient.Do(httpRequest)
	if err != nil {
		return TextStream{}, fmt.Errorf("send request: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		return TextStream{}, &APIError{StatusCode: response.StatusCode, Message: string(body)}
	}
	chunks := make(chan proxytpes.TextCompletionChunk)
	errors := make(chan error, 1)
	go readTextStream(response.Body, chunks, errors)
	return TextStream{Chunks: chunks, Errors: errors}, nil
}

func (client Client) Embedding(ctx context.Context, request proxytpes.EmbeddingRequest) (proxytpes.EmbeddingResponse, error) {
	response := proxytpes.EmbeddingResponse{}
	if err := client.post(ctx, "embeddings", request, &response); err != nil {
		return proxytpes.EmbeddingResponse{}, err
	}
	return response, nil
}

func (client Client) Response(ctx context.Context, request proxytpes.ResponsesRequest) (proxytpes.ResponsesResponse, error) {
	response := proxytpes.ResponsesResponse{}
	if err := client.post(ctx, "responses", request, &response); err != nil {
		return proxytpes.ResponsesResponse{}, err
	}
	return response, nil
}

func (client Client) CompletionStream(ctx context.Context, request proxytpes.ChatCompletionRequest) (Stream, error) {
	request.Stream = true
	encoded, err := json.Marshal(request)
	if err != nil {
		return Stream{}, fmt.Errorf("encode request: %w", err)
	}
	endpointURL, err := client.endpointURL("chat/completions")
	if err != nil {
		return Stream{}, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL, bytes.NewReader(encoded))
	if err != nil {
		return Stream{}, fmt.Errorf("create request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "text/event-stream")
	if client.APIKey != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+client.APIKey)
	}
	httpClient := client.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	response, err := httpClient.Do(httpRequest)
	if err != nil {
		return Stream{}, fmt.Errorf("send request: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		return Stream{}, &APIError{StatusCode: response.StatusCode, Message: string(body)}
	}
	chunks := make(chan proxytpes.ChatCompletionChunk)
	errors := make(chan error, 1)
	go readStream(response.Body, chunks, errors)
	return Stream{Chunks: chunks, Errors: errors}, nil
}

func readStream(body io.ReadCloser, chunks chan<- proxytpes.ChatCompletionChunk, errors chan<- error) {
	defer body.Close()
	defer close(chunks)
	defer close(errors)
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			return
		}
		chunk := proxytpes.ChatCompletionChunk{}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			errors <- fmt.Errorf("decode stream chunk: %w", err)
			return
		}
		chunks <- chunk
	}
	if err := scanner.Err(); err != nil {
		errors <- fmt.Errorf("read stream: %w", err)
	}
}

func readTextStream(body io.ReadCloser, chunks chan<- proxytpes.TextCompletionChunk, errors chan<- error) {
	defer body.Close()
	defer close(chunks)
	defer close(errors)
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			return
		}
		chunk := proxytpes.TextCompletionChunk{}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			errors <- fmt.Errorf("decode stream chunk: %w", err)
			return
		}
		chunks <- chunk
	}
	if err := scanner.Err(); err != nil {
		errors <- fmt.Errorf("read stream: %w", err)
	}
}

func (client Client) post(ctx context.Context, endpoint string, payload any, output any) error {
	return client.requestJSON(ctx, http.MethodPost, endpoint, payload, output)
}

func (client Client) get(ctx context.Context, endpoint string, output any) error {
	return client.requestJSON(ctx, http.MethodGet, endpoint, nil, output)
}

func (client Client) requestJSON(ctx context.Context, method string, endpoint string, payload any, output any) error {
	var requestBody io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		requestBody = bytes.NewReader(encoded)
	}
	endpointURL, err := client.endpointURL(endpoint)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, method, endpointURL, requestBody)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
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
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		var errorResponse struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(responseBody, &errorResponse)
		message := errorResponse.Error.Message
		if message == "" {
			message = string(responseBody)
		}
		return &APIError{StatusCode: response.StatusCode, Message: message}
	}
	if err := json.Unmarshal(responseBody, output); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (client Client) postBytes(ctx context.Context, endpoint string, payload any) ([]byte, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	endpointURL, err := client.endpointURL(endpoint)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL, bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
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
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, &APIError{StatusCode: response.StatusCode, Message: string(body)}
	}
	return body, nil
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
