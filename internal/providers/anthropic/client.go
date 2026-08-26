package anthropic

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
	requestBody, err := messagesRequest(body, deployment.Model)
	if err != nil {
		return providers.Response{}, err
	}
	targetURL, err := messagesURL(deployment.APIBase)
	if err != nil {
		return providers.Response{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(requestBody))
	if err != nil {
		return providers.Response{}, fmt.Errorf("create Anthropic request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("anthropic-version", "2023-06-01")
	if requestID := observability.RequestID(ctx); requestID != "" {
		request.Header.Set("X-Request-Id", requestID)
	}
	if deployment.APIKey != "" {
		request.Header.Set("x-api-key", deployment.APIKey)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providers.Response{}, fmt.Errorf("send Anthropic request: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return providers.Response{StatusCode: response.StatusCode, Header: response.Header, Body: response.Body}, nil
	}
	if isStreaming(requestBody) {
		return providers.Response{StatusCode: response.StatusCode, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: openAIStream(response.Body)}, nil
	}
	defer response.Body.Close()
	converted, err := chatCompletionResponse(response.Body)
	if err != nil {
		return providers.Response{}, err
	}
	return providers.Response{StatusCode: response.StatusCode, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(converted))}, nil
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
		var id, model string
		created := time.Now().Unix()
		scanner := bufio.NewScanner(source)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			var event struct {
				Type    string `json:"type"`
				Index   int    `json:"index"`
				Message struct {
					ID    string `json:"id"`
					Model string `json:"model"`
				} `json:"message"`
				Delta struct {
					Type       string `json:"type"`
					Text       string `json:"text"`
					StopReason string `json:"stop_reason"`
				} `json:"delta"`
			}
			if json.Unmarshal([]byte(data), &event) != nil {
				continue
			}
			switch event.Type {
			case "message_start":
				id, model = event.Message.ID, event.Message.Model
				writeChunk(writer, id, model, created, event.Index, proxytpes.Delta{Role: "assistant"}, nil)
			case "content_block_delta":
				if event.Delta.Type == "text_delta" {
					writeChunk(writer, id, model, created, event.Index, proxytpes.Delta{Content: event.Delta.Text}, nil)
				}
			case "message_delta":
				finishReason := event.Delta.StopReason
				if finishReason == "end_turn" {
					finishReason = "stop"
				}
				writeChunk(writer, id, model, created, 0, proxytpes.Delta{}, &finishReason)
			case "message_stop":
				_, _ = io.WriteString(writer, "data: [DONE]\n\n")
			}
		}
	}()
	return reader
}

func writeChunk(writer *io.PipeWriter, id string, model string, created int64, index int, delta proxytpes.Delta, finishReason *string) {
	chunk, err := json.Marshal(proxytpes.ChatCompletionChunk{ID: id, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []proxytpes.StreamingChoice{{Index: index, Delta: delta, FinishReason: finishReason}}})
	if err == nil {
		_, _ = writer.Write(append([]byte("data: "), append(chunk, []byte("\n\n")...)...))
	}
}

func (client Client) CreateResponse(context.Context, config.Model, []byte) (providers.Response, error) {
	return providers.Response{}, providers.ErrProviderNotConfigured
}

func (client Client) CreateEmbedding(context.Context, config.Model, []byte) (providers.Response, error) {
	return providers.Response{}, providers.ErrProviderNotConfigured
}

func messagesURL(apiBase string) (string, error) {
	if apiBase == "" {
		apiBase = "https://api.anthropic.com/v1"
	}
	parsed, err := url.Parse(apiBase)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid Anthropic api_base %q", apiBase)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/messages"
	return parsed.String(), nil
}

func messagesRequest(body []byte, configuredModel string) ([]byte, error) {
	payload := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode chat completion request: %w", err)
	}
	model := strings.TrimPrefix(configuredModel, "anthropic/")
	encodedModel, _ := json.Marshal(model)
	payload["model"] = encodedModel
	if _, found := payload["max_tokens"]; !found {
		payload["max_tokens"] = json.RawMessage("4096")
	}
	var messages []map[string]json.RawMessage
	if rawMessages, found := payload["messages"]; found {
		if err := json.Unmarshal(rawMessages, &messages); err != nil {
			return nil, fmt.Errorf("decode messages: %w", err)
		}
	}
	filtered := make([]map[string]json.RawMessage, 0, len(messages))
	for _, message := range messages {
		var role string
		_ = json.Unmarshal(message["role"], &role)
		if role == "system" {
			if _, alreadySet := payload["system"]; !alreadySet {
				payload["system"] = message["content"]
			}
			continue
		}
		filtered = append(filtered, message)
	}
	payload["messages"], _ = json.Marshal(filtered)
	if err := translateTools(payload); err != nil {
		return nil, err
	}
	delete(payload, "stream_options")
	return json.Marshal(payload)
}

func translateTools(payload map[string]json.RawMessage) error {
	rawTools, found := payload["tools"]
	if !found {
		return nil
	}
	var openAITools []struct {
		Type     string `json:"type"`
		Function struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Parameters  json.RawMessage `json:"parameters"`
		} `json:"function"`
	}
	if err := json.Unmarshal(rawTools, &openAITools); err != nil {
		return fmt.Errorf("decode OpenAI tools: %w", err)
	}
	type anthropicTool struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		InputSchema json.RawMessage `json:"input_schema"`
	}
	tools := make([]anthropicTool, 0, len(openAITools))
	for _, tool := range openAITools {
		if tool.Type != "function" || tool.Function.Name == "" {
			continue
		}
		inputSchema := tool.Function.Parameters
		if len(inputSchema) == 0 {
			inputSchema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		tools = append(tools, anthropicTool{Name: tool.Function.Name, Description: tool.Function.Description, InputSchema: inputSchema})
	}
	encodedTools, err := json.Marshal(tools)
	if err != nil {
		return fmt.Errorf("encode Anthropic tools: %w", err)
	}
	payload["tools"] = encodedTools
	return nil
}

func chatCompletionResponse(body io.Reader) ([]byte, error) {
	var response struct {
		ID         string `json:"id"`
		Model      string `json:"model"`
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(body).Decode(&response); err != nil {
		return nil, fmt.Errorf("decode Anthropic response: %w", err)
	}
	parts := make([]string, 0, len(response.Content))
	toolCalls := make([]map[string]any, 0)
	for _, content := range response.Content {
		if content.Type == "text" {
			parts = append(parts, content.Text)
		}
		if content.Type == "tool_use" {
			arguments := string(content.Input)
			if arguments == "" {
				arguments = "{}"
			}
			toolCalls = append(toolCalls, map[string]any{"id": content.ID, "type": "function", "function": map[string]string{"name": content.Name, "arguments": arguments}})
		}
	}
	content, _ := json.Marshal(strings.Join(parts, ""))
	message := proxytpes.Message{Role: "assistant", Content: content}
	if len(toolCalls) > 0 {
		encodedCalls, err := json.Marshal(toolCalls)
		if err != nil {
			return nil, fmt.Errorf("encode Anthropic tool calls: %w", err)
		}
		message.ToolCalls = encodedCalls
		if len(parts) == 0 {
			message.Content = nil
		}
	}
	finishReason := response.StopReason
	if finishReason == "end_turn" {
		finishReason = "stop"
	}
	if finishReason == "tool_use" {
		finishReason = "tool_calls"
	}
	return json.Marshal(proxytpes.ChatCompletionResponse{
		ID: response.ID, Object: "chat.completion", Created: time.Now().Unix(), Model: response.Model,
		Choices: []proxytpes.ChatChoice{{Index: 0, Message: message, FinishReason: &finishReason}},
		Usage:   &proxytpes.Usage{PromptTokens: response.Usage.InputTokens, CompletionTokens: response.Usage.OutputTokens, TotalTokens: response.Usage.InputTokens + response.Usage.OutputTokens},
	})
}
