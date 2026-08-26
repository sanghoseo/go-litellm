package bedrock

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/BerriAI/litellm/go-proxy/internal/config"
	bedrockruntime "github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	bedrocktypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

func TestOpenAIStreamConvertsBedrockEvents(t *testing.T) {
	events := make(chan bedrocktypes.ConverseStreamOutput, 3)
	events <- &bedrocktypes.ConverseStreamOutputMemberMessageStart{Value: bedrocktypes.MessageStartEvent{Role: bedrocktypes.ConversationRoleAssistant}}
	events <- &bedrocktypes.ConverseStreamOutputMemberContentBlockDelta{Value: bedrocktypes.ContentBlockDeltaEvent{Delta: &bedrocktypes.ContentBlockDeltaMemberText{Value: "hello"}}}
	events <- &bedrocktypes.ConverseStreamOutputMemberMessageStop{Value: bedrocktypes.MessageStopEvent{StopReason: bedrocktypes.StopReasonEndTurn}}
	close(events)

	stream, err := io.ReadAll(openAIStream(mockStream{events: events}, "bedrock/anthropic.test"))
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	if !strings.Contains(string(stream), `"role":"assistant"`) || !strings.Contains(string(stream), `"content":"hello"`) || !strings.Contains(string(stream), `"finish_reason":"stop"`) || !strings.HasSuffix(string(stream), "data: [DONE]\n\n") {
		t.Fatalf("stream = %s", stream)
	}
}

type mockStream struct {
	events <-chan bedrocktypes.ConverseStreamOutput
}

func (stream mockStream) Events() <-chan bedrocktypes.ConverseStreamOutput { return stream.events }
func (mockStream) Close() error                                            { return nil }
func (mockStream) Err() error                                              { return nil }

func TestChatCompletionConvertsBedrockConverse(t *testing.T) {
	client := NewClientWithFactory(func(_ context.Context, region string) (converseClient, error) {
		if region != "us-east-1" {
			t.Fatalf("region = %q", region)
		}
		return fakeConverse{}, nil
	})
	response, err := client.ChatCompletion(context.Background(), config.Model{Model: "bedrock/model-id", AWSRegion: "us-east-1"}, []byte(`{"messages":[{"role":"system","content":"follow instructions"},{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	converted, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(converted), `"content":"hello back"`) || !strings.Contains(string(converted), `"total_tokens":5`) {
		t.Fatalf("response = %s", converted)
	}
}

type fakeConverse struct{}

func (fakeConverse) Converse(_ context.Context, input *bedrockruntime.ConverseInput, _ ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
	if input.ModelId == nil || *input.ModelId != "model-id" || len(input.System) != 1 || len(input.Messages) != 1 {
		return nil, io.ErrUnexpectedEOF
	}
	inputTokens, outputTokens, totalTokens := int32(3), int32(2), int32(5)
	return &bedrockruntime.ConverseOutput{Output: &bedrocktypes.ConverseOutputMemberMessage{Value: bedrocktypes.Message{Role: bedrocktypes.ConversationRoleAssistant, Content: []bedrocktypes.ContentBlock{&bedrocktypes.ContentBlockMemberText{Value: "hello back"}}}}, StopReason: bedrocktypes.StopReason("end_turn"), Usage: &bedrocktypes.TokenUsage{InputTokens: &inputTokens, OutputTokens: &outputTokens, TotalTokens: &totalTokens}}, nil
}
