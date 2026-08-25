package providers

import (
	"context"
	"errors"
	"testing"

	"github.com/BerriAI/litellm/go-proxy/internal/config"
)

func TestRegistrySelectsProviderFromDeploymentModel(t *testing.T) {
	client := testClient{}
	registry := NewRegistry(map[string]Client{"openai": client})
	if _, err := registry.ChatCompletion(context.Background(), config.Model{Model: "openai/gpt-test"}, nil); err != nil {
		t.Fatalf("ChatCompletion() error = %v", err)
	}
}

func TestRegistryRejectsUnportedProvider(t *testing.T) {
	registry := NewRegistry(nil)
	_, err := registry.ChatCompletion(context.Background(), config.Model{Model: "anthropic/claude-test"}, nil)
	if !errors.Is(err, ErrProviderNotConfigured) {
		t.Fatalf("error = %v, want ErrProviderNotConfigured", err)
	}
}

type testClient struct{}

func (testClient) ChatCompletion(context.Context, config.Model, []byte) (Response, error) {
	return Response{}, nil
}
func (testClient) CreateResponse(context.Context, config.Model, []byte) (Response, error) {
	return Response{}, nil
}
func (testClient) CreateEmbedding(context.Context, config.Model, []byte) (Response, error) {
	return Response{}, nil
}
