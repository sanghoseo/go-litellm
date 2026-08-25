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
