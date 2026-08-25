package providers

import (
	"context"
	"errors"
	"strings"

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
	return client.ChatCompletion(ctx, deployment, body)
}

func (registry Registry) CreateResponse(ctx context.Context, deployment config.Model, body []byte) (Response, error) {
	client, err := registry.clientFor(deployment)
	if err != nil {
		return Response{}, err
	}
	return client.CreateResponse(ctx, deployment, body)
}

func (registry Registry) CreateEmbedding(ctx context.Context, deployment config.Model, body []byte) (Response, error) {
	client, err := registry.clientFor(deployment)
	if err != nil {
		return Response{}, err
	}
	return client.CreateEmbedding(ctx, deployment, body)
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
