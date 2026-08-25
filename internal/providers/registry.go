package providers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/BerriAI/litellm/go-proxy/internal/config"
)

var ErrProviderNotConfigured = errors.New("provider is not configured")

type Client interface {
	ChatCompleter
	ResponseCreator
	Embedder
}

type Registry struct {
	clients map[string]Client
}

func NewRegistry(clients map[string]Client) Registry {
	copyOfClients := make(map[string]Client, len(clients))
	for name, client := range clients {
		copyOfClients[strings.ToLower(name)] = client
	}
	return Registry{clients: copyOfClients}
}

func (registry Registry) ChatCompletion(ctx context.Context, deployment config.Model, body []byte) (Response, error) {
	client, err := registry.clientFor(deployment)
	if err != nil {
		return Response{}, err
	}
	return retry(ctx, deployment, func(callContext context.Context) (Response, error) {
		return client.ChatCompletion(callContext, deployment, body)
	})
}

func (registry Registry) CreateResponse(ctx context.Context, deployment config.Model, body []byte) (Response, error) {
	client, err := registry.clientFor(deployment)
	if err != nil {
		return Response{}, err
	}
	return retry(ctx, deployment, func(callContext context.Context) (Response, error) {
		return client.CreateResponse(callContext, deployment, body)
	})
}

func (registry Registry) CreateEmbedding(ctx context.Context, deployment config.Model, body []byte) (Response, error) {
	client, err := registry.clientFor(deployment)
	if err != nil {
		return Response{}, err
	}
	return retry(ctx, deployment, func(callContext context.Context) (Response, error) {
		return client.CreateEmbedding(callContext, deployment, body)
	})
}

func (registry Registry) GenerateImage(ctx context.Context, deployment config.Model, body []byte) (Response, error) {
	client, err := registry.clientFor(deployment)
	if err != nil {
		return Response{}, err
	}
	imageGenerator, ok := client.(ImageGenerator)
	if !ok {
		return Response{}, ErrProviderNotConfigured
	}
	return retry(ctx, deployment, func(callContext context.Context) (Response, error) {
		return imageGenerator.GenerateImage(callContext, deployment, body)
	})
}

func (registry Registry) CreateSpeech(ctx context.Context, deployment config.Model, body []byte) (Response, error) {
	client, err := registry.clientFor(deployment)
	if err != nil {
		return Response{}, err
	}
	speechCreator, ok := client.(SpeechCreator)
	if !ok {
		return Response{}, ErrProviderNotConfigured
	}
	return retry(ctx, deployment, func(callContext context.Context) (Response, error) {
		return speechCreator.CreateSpeech(callContext, deployment, body)
	})
}

func (registry Registry) Passthrough(ctx context.Context, deployment config.Model, method, endpoint, contentType string, body []byte) (Response, error) {
	client, err := registry.clientFor(deployment)
	if err != nil {
		return Response{}, err
	}
	passthroughClient, ok := client.(PassthroughClient)
	if !ok {
		return Response{}, ErrProviderNotConfigured
	}
	return retry(ctx, deployment, func(callContext context.Context) (Response, error) {
		return passthroughClient.Passthrough(callContext, deployment, method, endpoint, contentType, body)
	})
}

func (registry Registry) Moderate(ctx context.Context, deployment config.Model, body []byte) (Response, error) {
	client, err := registry.clientFor(deployment)
	if err != nil {
		return Response{}, err
	}
	moderator, ok := client.(Moderator)
	if !ok {
		return Response{}, ErrProviderNotConfigured
	}
	return retry(ctx, deployment, func(callContext context.Context) (Response, error) {
		return moderator.Moderate(callContext, deployment, body)
	})
}

func (registry Registry) TextCompletion(ctx context.Context, deployment config.Model, body []byte) (Response, error) {
	client, err := registry.clientFor(deployment)
	if err != nil {
		return Response{}, err
	}
	textCompleter, ok := client.(TextCompleter)
	if !ok {
		return Response{}, ErrProviderNotConfigured
	}
	return retry(ctx, deployment, func(callContext context.Context) (Response, error) {
		return textCompleter.TextCompletion(callContext, deployment, body)
	})
}

func retry(ctx context.Context, deployment config.Model, call func(context.Context) (Response, error)) (Response, error) {
	attempts := deployment.NumRetries + 1
	for attempt := 0; attempt < attempts; attempt++ {
		callContext, cancel := withTimeout(ctx, deployment.Timeout)
		response, err := call(callContext)
		if response.Body != nil {
			response.Body = &cancelReadCloser{ReadCloser: response.Body, cancel: cancel}
		} else {
			cancel()
		}
		if err == nil && response.StatusCode < http.StatusInternalServerError {
			return response, nil
		}
		if attempt+1 == attempts {
			return response, err
		}
		if response.Body != nil {
			_ = response.Body.Close()
		}
		select {
		case <-ctx.Done():
			return Response{}, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 100 * time.Millisecond):
		}
	}
	return Response{}, nil
}

func withTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

type cancelReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (reader *cancelReadCloser) Close() error {
	err := reader.ReadCloser.Close()
	reader.cancel()
	return err
}

func (registry Registry) clientFor(deployment config.Model) (Client, error) {
	provider, _, found := strings.Cut(deployment.Model, "/")
	if !found {
		provider = "openai"
	}
	client, found := registry.clients[strings.ToLower(provider)]
	if !found || client == nil {
		return nil, ErrProviderNotConfigured
	}
	return client, nil
}
