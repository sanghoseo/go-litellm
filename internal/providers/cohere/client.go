package cohere

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
	observability.ApplyRequestMetadata(ctx, request.Header)
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
	if isStreaming(requestBody) {
		return providers.Response{StatusCode: response.StatusCode, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: openAIStream(response.Body)}, nil
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

func (client Client) CreateEmbedding(ctx context.Context, deployment config.Model, body []byte) (providers.Response, error) {
	requestBody, err := embeddingRequest(body, deployment.Model)
	if err != nil {
		return providers.Response{}, err
	}
	response, err := client.request(ctx, deployment, "embed", requestBody)
	if err != nil || response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return response, err
	}
	defer response.Body.Close()
	converted, err := embeddingResponse(response.Body, strings.TrimPrefix(deployment.Model, "cohere/"))
	if err != nil {
		return providers.Response{}, err
	}
	return providers.Response{StatusCode: response.StatusCode, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(converted))}, nil
}

func (client Client) Rerank(ctx context.Context, deployment config.Model, body []byte) (providers.Response, error) {
	requestBody, err := withModel(body, deployment.Model)
	if err != nil {
		return providers.Response{}, err
	}
	return client.request(ctx, deployment, "rerank", requestBody)
}

func chatURL(apiBase string) (string, error) {
	return endpointURL(apiBase, "chat")
}

func endpointURL(apiBase string, endpoint string) (string, error) {
	if apiBase == "" {
		apiBase = "https://api.cohere.com/v2"
	}
	parsed, err := url.Parse(apiBase)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid Cohere api_base %q", apiBase)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/" + endpoint
	return parsed.String(), nil
}

func (client Client) request(ctx context.Context, deployment config.Model, endpoint string, body []byte) (providers.Response, error) {
	targetURL, err := endpointURL(deployment.APIBase, endpoint)
	if err != nil {
		return providers.Response{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return providers.Response{}, fmt.Errorf("create Cohere request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if deployment.APIKey != "" {
		request.Header.Set("Authorization", "Bearer "+deployment.APIKey)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providers.Response{}, fmt.Errorf("send Cohere request: %w", err)
	}
	return providers.Response{StatusCode: response.StatusCode, Header: response.Header, Body: response.Body}, nil
}

func chatRequest(body []byte, configuredModel string) ([]byte, error) {
	payload := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode chat completion request: %w", err)
	}
	if err := setModel(payload, configuredModel); err != nil {
		return nil, err
	}
	delete(payload, "stream_options")
	return json.Marshal(payload)
}

func isStreaming(body []byte) bool {
	var payload struct {
		Stream bool `json:"stream"`
	}
	return json.Unmarshal(body, &payload) == nil && payload.Stream
}

func openAIStream(source io.ReadCloser) io.ReadCloser {
	reader, writer := io.Pipe()
	go func() {
		defer source.Close()
		defer writer.Close()
		scanner := bufio.NewScanner(source)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		var eventType string
		var id, model string
		created := time.Now().Unix()
		started := false
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event: ") {
				eventType = strings.TrimPrefix(line, "event: ")
				continue
			}
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var event struct {
				Type  string `json:"type"`
				ID    string `json:"id"`
				Model string `json:"model"`
				Delta struct {
					Message struct {
						Content any `json:"content"`
					} `json:"message"`
					FinishReason string `json:"finish_reason"`
				} `json:"delta"`
				Data struct {
					ID    string `json:"id"`
					Model string `json:"model"`
					Delta struct {
						Message struct {
							Content any `json:"content"`
						} `json:"message"`
						FinishReason string `json:"finish_reason"`
					} `json:"delta"`
				} `json:"data"`
			}
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event) != nil {
				continue
			}
			if event.Type == "" {
				event.Type = eventType
			}
			if event.ID != "" {
				id = event.ID
			}
			if event.Data.ID != "" {
				id = event.Data.ID
			}
			if event.Model != "" {
				model = event.Model
			}
			if event.Data.Model != "" {
				model = event.Data.Model
			}
			if event.Type == "message-start" && !started {
				writeChunk(writer, id, model, created, proxytpes.Delta{Role: "assistant"}, nil)
				started = true
			}
			if event.Type == "content-delta" {
				content := event.Delta.Message.Content
				if content == nil {
					content = event.Data.Delta.Message.Content
				}
				if text := contentText(content); text != "" {
					if !started {
						writeChunk(writer, id, model, created, proxytpes.Delta{Role: "assistant"}, nil)
						started = true
					}
					writeChunk(writer, id, model, created, proxytpes.Delta{Content: text}, nil)
				}
			}
			if event.Type == "message-end" {
				finishReason := event.Delta.FinishReason
				if finishReason == "" {
					finishReason = event.Data.Delta.FinishReason
				}
				if finishReason == "" || strings.EqualFold(finishReason, "complete") {
					finishReason = "stop"
				}
				writeChunk(writer, id, model, created, proxytpes.Delta{}, &finishReason)
				_, _ = io.WriteString(writer, "data: [DONE]\n\n")
			}
		}
	}()
	return reader
}

func contentText(content any) string {
	switch value := content.(type) {
	case string:
		return value
	case map[string]any:
		text, _ := value["text"].(string)
		return text
	}
	return ""
}

func writeChunk(writer *io.PipeWriter, id string, model string, created int64, delta proxytpes.Delta, finishReason *string) {
	chunk, err := json.Marshal(proxytpes.ChatCompletionChunk{ID: id, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []proxytpes.StreamingChoice{{Index: 0, Delta: delta, FinishReason: finishReason}}})
	if err == nil {
		_, _ = writer.Write(append([]byte("data: "), append(chunk, []byte("\n\n")...)...))
	}
}

func embeddingRequest(body []byte, configuredModel string) ([]byte, error) {
	var request struct {
		Input          json.RawMessage `json:"input"`
		EncodingFormat string          `json:"encoding_format"`
		Dimensions     *int            `json:"dimensions"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, fmt.Errorf("decode embedding request: %w", err)
	}
	var texts []string
	if err := json.Unmarshal(request.Input, &texts); err != nil {
		var text string
		if singleErr := json.Unmarshal(request.Input, &text); singleErr != nil {
			return nil, fmt.Errorf("Cohere embedding input must be a string or list of strings")
		}
		texts = []string{text}
	}
	payload := map[string]any{"model": strings.TrimPrefix(configuredModel, "cohere/"), "texts": texts, "input_type": "search_document"}
	if request.EncodingFormat != "" {
		payload["embedding_types"] = []string{request.EncodingFormat}
	}
	if request.Dimensions != nil {
		payload["output_dimension"] = *request.Dimensions
	}
	return json.Marshal(payload)
}

func withModel(body []byte, configuredModel string) ([]byte, error) {
	payload := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode Cohere request: %w", err)
	}
	if err := setModel(payload, configuredModel); err != nil {
		return nil, err
	}
	return json.Marshal(payload)
}

func setModel(payload map[string]json.RawMessage, configuredModel string) error {
	encodedModel, err := json.Marshal(strings.TrimPrefix(configuredModel, "cohere/"))
	if err != nil {
		return fmt.Errorf("encode configured model: %w", err)
	}
	payload["model"] = encodedModel
	return nil
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

func embeddingResponse(body io.Reader, model string) ([]byte, error) {
	var response struct {
		Embeddings map[string][][]float64 `json:"embeddings"`
		Meta       struct {
			BilledUnits struct {
				InputTokens int `json:"input_tokens"`
			} `json:"billed_units"`
		} `json:"meta"`
	}
	if err := json.NewDecoder(body).Decode(&response); err != nil {
		return nil, fmt.Errorf("decode Cohere embedding response: %w", err)
	}
	vectors := response.Embeddings["float"]
	if len(vectors) == 0 {
		for _, candidate := range response.Embeddings {
			vectors = candidate
			break
		}
	}
	data := make([]proxytpes.Embedding, 0, len(vectors))
	for index, vector := range vectors {
		encoded, err := json.Marshal(vector)
		if err != nil {
			return nil, fmt.Errorf("encode Cohere embedding vector: %w", err)
		}
		data = append(data, proxytpes.Embedding{Object: "embedding", Index: index, Embedding: encoded})
	}
	return json.Marshal(proxytpes.EmbeddingResponse{Object: "list", Data: data, Model: model, Usage: &proxytpes.Usage{PromptTokens: response.Meta.BilledUnits.InputTokens, TotalTokens: response.Meta.BilledUnits.InputTokens}})
}
