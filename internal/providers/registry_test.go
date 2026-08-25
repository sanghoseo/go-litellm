package providers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/BerriAI/litellm/go-proxy/internal/config"
)

func TestRegistrySelectsProviderFromDeploymentModel(t *testing.T) {
	client := testClient{}
	registry := NewRegistry(map[string]Client{"openai": client})
	if _, err := registry.ChatCompletion(context.Background(), config.Model{Model: "openai/gpt-test"}, nil); err != nil {
		t.Fatalf("ChatCompletion() error = %v", err)
	}
}

func TestRegistryRetriesServerFailures(t *testing.T) {
	client := &retryClient{}
	registry := NewRegistry(map[string]Client{"openai": client})
	response, err := registry.ChatCompletion(context.Background(), config.Model{Model: "openai/gpt-test", NumRetries: 1}, nil)
	if err != nil || response.StatusCode != http.StatusOK || client.calls != 2 {
		t.Fatalf("response=%#v err=%v calls=%d", response, err, client.calls)
	}
}

func TestRegistryPassesDeploymentTimeoutToProvider(t *testing.T) {
	client := timeoutClient{}
	registry := NewRegistry(map[string]Client{"openai": client})
	_, err := registry.ChatCompletion(context.Background(), config.Model{Model: "openai/gpt-test", Timeout: time.Second}, nil)
	if err != nil {
		t.Fatal(err)
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

type retryClient struct{ calls int }

func (client *retryClient) ChatCompletion(context.Context, config.Model, []byte) (Response, error) {
	client.calls++
	status := http.StatusInternalServerError
	if client.calls == 2 {
		status = http.StatusOK
	}
	return Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("{}"))}, nil
}
func (*retryClient) CreateResponse(context.Context, config.Model, []byte) (Response, error) {
	return Response{}, nil
}
func (*retryClient) CreateEmbedding(context.Context, config.Model, []byte) (Response, error) {
	return Response{}, nil
}

type timeoutClient struct{}

func (timeoutClient) ChatCompletion(ctx context.Context, _ config.Model, _ []byte) (Response, error) {
	if _, found := ctx.Deadline(); !found {
		return Response{}, errors.New("missing deadline")
	}
	return Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}"))}, nil
}
func (timeoutClient) CreateResponse(context.Context, config.Model, []byte) (Response, error) {
	return Response{}, nil
}
func (timeoutClient) CreateEmbedding(context.Context, config.Model, []byte) (Response, error) {
	return Response{}, nil
}
