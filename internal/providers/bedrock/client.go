package bedrock

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/BerriAI/litellm/go-proxy/internal/config"
	"github.com/BerriAI/litellm/go-proxy/internal/providers"
	proxytpes "github.com/BerriAI/litellm/go-proxy/pkg/types"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	bedrockruntime "github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	bedrocktypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

type converseClient interface {
	Converse(context.Context, *bedrockruntime.ConverseInput, ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error)
}

type converseStreamClient interface {
	ConverseStream(context.Context, *bedrockruntime.ConverseStreamInput, ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseStreamOutput, error)
}

type streamReader interface {
	Events() <-chan bedrocktypes.ConverseStreamOutput
	Close() error
	Err() error
}

type runtimeFactory func(context.Context, string) (converseClient, error)

type Client struct{ factory runtimeFactory }

func NewClient() Client                                  { return Client{factory: defaultRuntime} }
func NewClientWithFactory(factory runtimeFactory) Client { return Client{factory: factory} }

func (client Client) ChatCompletion(ctx context.Context, deployment config.Model, body []byte) (providers.Response, error) {
	input, err := converseInput(deployment.Model, body)
	if err != nil {
		return providers.Response{}, err
	}
	runtime, err := client.factory(ctx, deployment.AWSRegion)
	if err != nil {
		return providers.Response{}, err
	}
	if isStreaming(body) {
		streamRuntime, ok := runtime.(converseStreamClient)
		if !ok {
			return providers.Response{}, fmt.Errorf("Bedrock runtime does not support ConverseStream")
		}
		streamInput := &bedrockruntime.ConverseStreamInput{ModelId: input.ModelId, Messages: input.Messages, System: input.System, InferenceConfig: input.InferenceConfig}
		streamOutput, err := streamRuntime.ConverseStream(ctx, streamInput)
		if err != nil {
			return providers.Response{}, fmt.Errorf("invoke Bedrock ConverseStream: %w", err)
		}
		stream := streamOutput.GetStream()
		if stream == nil {
			return providers.Response{}, fmt.Errorf("Bedrock ConverseStream returned no event stream")
		}
		return providers.Response{StatusCode: 200, Header: map[string][]string{"Content-Type": {"text/event-stream"}}, Body: openAIStream(stream, deployment.Model)}, nil
	}
	output, err := runtime.Converse(ctx, input)
	if err != nil {
		return providers.Response{}, fmt.Errorf("invoke Bedrock Converse: %w", err)
	}
	converted, err := openAIResponse(output, deployment.Model)
	if err != nil {
		return providers.Response{}, err
	}
	return providers.Response{StatusCode: 200, Header: map[string][]string{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(converted))}, nil
}

func isStreaming(body []byte) bool {
	var request struct {
		Stream bool `json:"stream"`
	}
	return json.Unmarshal(body, &request) == nil && request.Stream
}

func openAIStream(stream streamReader, configuredModel string) io.ReadCloser {
	reader, writer := io.Pipe()
	go func() {
		defer writer.Close()
		defer stream.Close()
		created := time.Now().Unix()
		model := strings.TrimPrefix(configuredModel, "bedrock/")
		for event := range stream.Events() {
			switch value := event.(type) {
			case *bedrocktypes.ConverseStreamOutputMemberMessageStart:
				writeStreamChunk(writer, model, created, proxytpes.Delta{Role: string(value.Value.Role)}, nil)
			case *bedrocktypes.ConverseStreamOutputMemberContentBlockDelta:
				if text, ok := value.Value.Delta.(*bedrocktypes.ContentBlockDeltaMemberText); ok {
					writeStreamChunk(writer, model, created, proxytpes.Delta{Content: text.Value}, nil)
				}
			case *bedrocktypes.ConverseStreamOutputMemberMessageStop:
				finishReason := strings.ToLower(string(value.Value.StopReason))
				if finishReason == "end_turn" {
					finishReason = "stop"
				}
				writeStreamChunk(writer, model, created, proxytpes.Delta{}, &finishReason)
				_, _ = io.WriteString(writer, "data: [DONE]\n\n")
				return
			}
		}
	}()
	return reader
}

func writeStreamChunk(writer *io.PipeWriter, model string, created int64, delta proxytpes.Delta, finishReason *string) {
	chunk, err := json.Marshal(proxytpes.ChatCompletionChunk{ID: "chatcmpl-bedrock", Object: "chat.completion.chunk", Created: created, Model: model, Choices: []proxytpes.StreamingChoice{{Index: 0, Delta: delta, FinishReason: finishReason}}})
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

func defaultRuntime(ctx context.Context, region string) (converseClient, error) {
	options := []func(*awsconfig.LoadOptions) error{}
	if region != "" {
		options = append(options, awsconfig.WithRegion(region))
	}
	awsConfig, err := awsconfig.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}
	return bedrockruntime.NewFromConfig(awsConfig), nil
}

func converseInput(configuredModel string, body []byte) (*bedrockruntime.ConverseInput, error) {
	var request struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, fmt.Errorf("decode chat completion request: %w", err)
	}
	modelID := strings.TrimPrefix(configuredModel, "bedrock/")
	input := &bedrockruntime.ConverseInput{ModelId: &modelID}
	for _, message := range request.Messages {
		var text string
		if json.Unmarshal(message.Content, &text) != nil {
			continue
		}
		if message.Role == "system" {
			input.System = append(input.System, &bedrocktypes.SystemContentBlockMemberText{Value: text})
			continue
		}
		role := bedrocktypes.ConversationRoleUser
		if message.Role == "assistant" {
			role = bedrocktypes.ConversationRoleAssistant
		}
		input.Messages = append(input.Messages, bedrocktypes.Message{Role: role, Content: []bedrocktypes.ContentBlock{&bedrocktypes.ContentBlockMemberText{Value: text}}})
	}
	return input, nil
}

func openAIResponse(output *bedrockruntime.ConverseOutput, configuredModel string) ([]byte, error) {
	message, ok := output.Output.(*bedrocktypes.ConverseOutputMemberMessage)
	if !ok {
		return nil, fmt.Errorf("unexpected Bedrock output %T", output.Output)
	}
	parts := []string{}
	for _, block := range message.Value.Content {
		if text, ok := block.(*bedrocktypes.ContentBlockMemberText); ok {
			parts = append(parts, text.Value)
		}
	}
	content, _ := json.Marshal(strings.Join(parts, ""))
	finish := string(output.StopReason)
	if finish == "end_turn" {
		finish = "stop"
	}
	usage := proxytpes.Usage{}
	if output.Usage != nil {
		usage = proxytpes.Usage{PromptTokens: dereference(output.Usage.InputTokens), CompletionTokens: dereference(output.Usage.OutputTokens), TotalTokens: dereference(output.Usage.TotalTokens)}
	}
	return json.Marshal(proxytpes.ChatCompletionResponse{ID: "chatcmpl-bedrock", Object: "chat.completion", Created: time.Now().Unix(), Model: strings.TrimPrefix(configuredModel, "bedrock/"), Choices: []proxytpes.ChatChoice{{Index: 0, Message: proxytpes.Message{Role: "assistant", Content: content}, FinishReason: &finish}}, Usage: &usage})
}

func dereference(value *int32) int {
	if value == nil {
		return 0
	}
	return int(*value)
}
