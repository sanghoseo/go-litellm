package gemini

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
	requestBody, err := generateContentRequest(body)
	if err != nil {
		return providers.Response{}, err
	}
	stream := isStreaming(body)
	targetURL, err := generateContentURL(deployment.APIBase, deployment.Model, deployment.APIKey, stream)
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
	if stream {
		return providers.Response{StatusCode: response.StatusCode, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: openAIStream(response.Body, deployment.Model)}, nil
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

func (client Client) CreateEmbedding(ctx context.Context, deployment config.Model, body []byte) (providers.Response, error) {
	inputs, err := embeddingInputs(body)
	if err != nil {
		return providers.Response{}, err
	}
	targetURL, err := embeddingURL(deployment.APIBase, deployment.Model, deployment.APIKey)
	if err != nil {
		return providers.Response{}, err
	}
	data := make([]proxytpes.Embedding, 0, len(inputs))
	for index, input := range inputs {
		requestBody, err := json.Marshal(map[string]any{"content": map[string]any{"parts": []map[string]string{{"text": input}}}})
		if err != nil {
			return providers.Response{}, fmt.Errorf("encode Gemini embedding request: %w", err)
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(requestBody))
		if err != nil {
			return providers.Response{}, fmt.Errorf("create Gemini embedding request: %w", err)
		}
		request.Header.Set("Content-Type", "application/json")
		if requestID := observability.RequestID(ctx); requestID != "" {
			request.Header.Set("X-Request-Id", requestID)
		}
		response, err := client.httpClient.Do(request)
		if err != nil {
			return providers.Response{}, fmt.Errorf("send Gemini embedding request: %w", err)
		}
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			return providers.Response{StatusCode: response.StatusCode, Header: response.Header, Body: response.Body}, nil
		}
		var upstream struct {
			Embedding struct {
				Values []float64 `json:"values"`
			} `json:"embedding"`
		}
		decodeErr := json.NewDecoder(response.Body).Decode(&upstream)
		_ = response.Body.Close()
		if decodeErr != nil {
			return providers.Response{}, fmt.Errorf("decode Gemini embedding response: %w", decodeErr)
		}
		vector, err := json.Marshal(upstream.Embedding.Values)
		if err != nil {
			return providers.Response{}, fmt.Errorf("encode Gemini embedding response: %w", err)
		}
		data = append(data, proxytpes.Embedding{Object: "embedding", Embedding: vector, Index: index})
	}
	model := strings.TrimPrefix(deployment.Model, "gemini/")
	converted, err := json.Marshal(proxytpes.EmbeddingResponse{Object: "list", Data: data, Model: model, Usage: &proxytpes.Usage{}})
	if err != nil {
		return providers.Response{}, fmt.Errorf("encode OpenAI embedding response: %w", err)
	}
	return providers.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(converted))}, nil
}

func generateContentURL(apiBase, configuredModel, apiKey string, stream bool) (string, error) {
	method := "generateContent"
	if stream {
		method = "streamGenerateContent"
	}
	return modelURL(apiBase, configuredModel, apiKey, method)
}

func isStreaming(body []byte) bool {
	var request struct {
		Stream bool `json:"stream"`
	}
	return json.Unmarshal(body, &request) == nil && request.Stream
}

func openAIStream(source io.ReadCloser, configuredModel string) io.ReadCloser {
	reader, writer := io.Pipe()
	go func() {
		defer source.Close()
		defer writer.Close()
		scanner := bufio.NewScanner(source)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		started := false
		created := time.Now().Unix()
		model := strings.TrimPrefix(configuredModel, "gemini/")
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var response struct {
				Candidates []struct {
					Content struct {
						Parts []struct {
							Text string `json:"text"`
						} `json:"parts"`
					} `json:"content"`
					FinishReason string `json:"finishReason"`
				} `json:"candidates"`
			}
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &response) != nil || len(response.Candidates) == 0 {
				continue
			}
			candidate := response.Candidates[0]
			if !started {
				writeStreamChunk(writer, model, created, proxytpes.Delta{Role: "assistant"}, nil)
				started = true
			}
			for _, part := range candidate.Content.Parts {
				if part.Text != "" {
					writeStreamChunk(writer, model, created, proxytpes.Delta{Content: part.Text}, nil)
				}
			}
			if candidate.FinishReason != "" {
				finishReason := strings.ToLower(candidate.FinishReason)
				writeStreamChunk(writer, model, created, proxytpes.Delta{}, &finishReason)
				_, _ = io.WriteString(writer, "data: [DONE]\n\n")
				return
			}
		}
	}()
	return reader
}

func writeStreamChunk(writer *io.PipeWriter, model string, created int64, delta proxytpes.Delta, finishReason *string) {
	chunk, err := json.Marshal(proxytpes.ChatCompletionChunk{ID: "chatcmpl-gemini", Object: "chat.completion.chunk", Created: created, Model: model, Choices: []proxytpes.StreamingChoice{{Index: 0, Delta: delta, FinishReason: finishReason}}})
	if err == nil {
		_, _ = writer.Write(append([]byte("data: "), append(chunk, []byte("\n\n")...)...))
	}
}

func embeddingURL(apiBase, configuredModel, apiKey string) (string, error) {
	return modelURL(apiBase, configuredModel, apiKey, "embedContent")
}

func modelURL(apiBase, configuredModel, apiKey, method string) (string, error) {
	if apiBase == "" {
		apiBase = "https://generativelanguage.googleapis.com/v1beta"
	}
	parsed, err := url.Parse(apiBase)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid Gemini api_base %q", apiBase)
	}
	model := strings.TrimPrefix(configuredModel, "gemini/")
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/models/" + url.PathEscape(model) + ":" + method
	if apiKey != "" {
		query := parsed.Query()
		query.Set("key", apiKey)
		parsed.RawQuery = query.Encode()
	}
	return parsed.String(), nil
}

func embeddingInputs(body []byte) ([]string, error) {
	var request struct {
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, fmt.Errorf("decode embedding request: %w", err)
	}
	var inputs []string
	if err := json.Unmarshal(request.Input, &inputs); err == nil {
		return inputs, nil
	}
	var input string
	if err := json.Unmarshal(request.Input, &input); err != nil {
		return nil, fmt.Errorf("Gemini embedding input must be a string or list of strings")
	}
	return []string{input}, nil
}

func generateContentRequest(body []byte) ([]byte, error) {
	var request struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
		Temperature *float64        `json:"temperature"`
		Tools       json.RawMessage `json:"tools"`
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
	if len(request.Tools) > 0 {
		tools, err := geminiTools(request.Tools)
		if err != nil {
			return nil, err
		}
		payload["tools"] = []any{map[string]any{"functionDeclarations": tools}}
	}
	return json.Marshal(payload)
}

func geminiTools(rawTools json.RawMessage) ([]map[string]any, error) {
	var tools []struct {
		Type     string `json:"type"`
		Function struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Parameters  json.RawMessage `json:"parameters"`
		} `json:"function"`
	}
	if err := json.Unmarshal(rawTools, &tools); err != nil {
		return nil, fmt.Errorf("decode OpenAI tools: %w", err)
	}
	declarations := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		if tool.Type != "function" || tool.Function.Name == "" {
			continue
		}
		declaration := map[string]any{"name": tool.Function.Name}
		if tool.Function.Description != "" {
			declaration["description"] = tool.Function.Description
		}
		if len(tool.Function.Parameters) > 0 {
			declaration["parameters"] = tool.Function.Parameters
		}
		declarations = append(declarations, declaration)
	}
	return declarations, nil
}

func chatCompletionResponse(body io.Reader, configuredModel string) ([]byte, error) {
	var response struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text         string `json:"text"`
					FunctionCall struct {
						Name string          `json:"name"`
						Args json.RawMessage `json:"args"`
					} `json:"functionCall"`
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
	toolCalls := make([]map[string]any, 0)
	for _, part := range parts {
		text = append(text, part.Text)
		if part.FunctionCall.Name != "" {
			arguments := part.FunctionCall.Args
			if len(arguments) == 0 {
				arguments = json.RawMessage(`{}`)
			}
			toolCalls = append(toolCalls, map[string]any{"id": "call_" + part.FunctionCall.Name, "type": "function", "function": map[string]any{"name": part.FunctionCall.Name, "arguments": string(arguments)}})
		}
	}
	content, _ := json.Marshal(strings.Join(text, ""))
	finishReason := strings.ToLower(response.Candidates[0].FinishReason)
	if finishReason == "stop" {
		finishReason = "stop"
	}
	message := proxytpes.Message{Role: "assistant", Content: content}
	if len(toolCalls) > 0 {
		encoded, err := json.Marshal(toolCalls)
		if err != nil {
			return nil, fmt.Errorf("encode Gemini tool calls: %w", err)
		}
		message.ToolCalls = encoded
		if len(text) == 0 {
			message.Content = nil
		}
		finishReason = "tool_calls"
	}
	model := strings.TrimPrefix(configuredModel, "gemini/")
	return json.Marshal(proxytpes.ChatCompletionResponse{ID: "chatcmpl-gemini", Object: "chat.completion", Created: time.Now().Unix(), Model: model, Choices: []proxytpes.ChatChoice{{Index: 0, Message: message, FinishReason: &finishReason}}, Usage: &proxytpes.Usage{PromptTokens: response.Usage.Prompt, CompletionTokens: response.Usage.Completion, TotalTokens: response.Usage.Total}})
}
