package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/BerriAI/litellm/go-proxy/internal/auth"
	"github.com/BerriAI/litellm/go-proxy/internal/config"
	"github.com/BerriAI/litellm/go-proxy/internal/providers"
)

type VirtualKeyValidator interface {
	Validate(context.Context, string, string) (auth.VirtualKey, error)
}

type Server struct {
	config        config.Config
	chatCompleter providers.ChatCompleter
	responseMaker providers.ResponseCreator
	embedder      providers.Embedder
	keyValidator  VirtualKeyValidator
}

func NewServer(proxyConfig config.Config, completers ...providers.ChatCompleter) Server {
	var chatCompleter providers.ChatCompleter
	if len(completers) > 0 {
		chatCompleter = completers[0]
	}
	server := Server{config: proxyConfig, chatCompleter: chatCompleter}
	server.setOptionalCompleters(chatCompleter)
	return server
}

func NewServerWithVirtualKeyValidator(proxyConfig config.Config, completer providers.ChatCompleter, validator VirtualKeyValidator) Server {
	server := Server{config: proxyConfig, chatCompleter: completer, keyValidator: validator}
	server.setOptionalCompleters(completer)
	return server
}

func (server *Server) setOptionalCompleters(completer providers.ChatCompleter) {
	if responseMaker, ok := completer.(providers.ResponseCreator); ok {
		server.responseMaker = responseMaker
	}
	if embedder, ok := completer.(providers.Embedder); ok {
		server.embedder = embedder
	}
}

func (server Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/liveliness", server.health)
	mux.HandleFunc("GET /health/readiness", server.health)
	mux.HandleFunc("GET /v1/models", server.models)
	mux.HandleFunc("POST /v1/chat/completions", server.chatCompletions)
	mux.HandleFunc("POST /v1/responses", server.responses)
	mux.HandleFunc("POST /v1/embeddings", server.embeddings)
	return mux
}

func (server Server) responses(writer http.ResponseWriter, request *http.Request) {
	if server.responseMaker == nil {
		server.providerUnavailable(writer, "No responses provider is configured")
		return
	}
	server.forwardModelRequest(writer, request, server.responseMaker.CreateResponse)
}

func (server Server) embeddings(writer http.ResponseWriter, request *http.Request) {
	if server.embedder == nil {
		server.providerUnavailable(writer, "No embedding provider is configured")
		return
	}
	server.forwardModelRequest(writer, request, server.embedder.CreateEmbedding)
}

type modelRequestCompleter func(context.Context, config.Model, []byte) (providers.Response, error)

func (server Server) forwardModelRequest(writer http.ResponseWriter, request *http.Request, completer modelRequestCompleter) {
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, 10<<20))
	if err != nil {
		writeJSON(writer, http.StatusRequestEntityTooLarge, openAIError{Message: "Request body exceeds 10 MiB", Type: "invalid_request_error", Code: "request_too_large"})
		return
	}
	modelName, err := requestedModel(body)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, openAIError{Message: err.Error(), Type: "invalid_request_error", Code: "invalid_request"})
		return
	}
	deployment, found := server.deploymentFor(modelName)
	if !found {
		writeJSON(writer, http.StatusNotFound, openAIError{Message: fmt.Sprintf("The model %q does not exist", modelName), Type: "invalid_request_error", Code: "model_not_found"})
		return
	}
	if _, authorized := server.authorize(request, modelName); !authorized {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	upstream, err := completer(request.Context(), deployment, body)
	if err != nil {
		writeJSON(writer, http.StatusBadGateway, openAIError{Message: "Upstream provider request failed", Type: "api_error", Code: "upstream_error"})
		return
	}
	defer upstream.Body.Close()
	copyResponseHeaders(writer.Header(), upstream.Header)
	writer.WriteHeader(upstream.StatusCode)
	_ = copyResponse(writer, upstream.Body)
}

func (server Server) providerUnavailable(writer http.ResponseWriter, message string) {
	writeJSON(writer, http.StatusServiceUnavailable, openAIError{Message: message, Type: "server_error", Code: "provider_unavailable"})
}

func (server Server) chatCompletions(writer http.ResponseWriter, request *http.Request) {
	if server.chatCompleter == nil {
		writeJSON(writer, http.StatusServiceUnavailable, openAIError{Message: "No chat completion provider is configured", Type: "server_error", Code: "provider_unavailable"})
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, 10<<20))
	if err != nil {
		writeJSON(writer, http.StatusRequestEntityTooLarge, openAIError{Message: "Request body exceeds 10 MiB", Type: "invalid_request_error", Code: "request_too_large"})
		return
	}
	modelName, err := requestedModel(body)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, openAIError{Message: err.Error(), Type: "invalid_request_error", Code: "invalid_request"})
		return
	}
	deployment, found := server.deploymentFor(modelName)
	if !found {
		writeJSON(writer, http.StatusNotFound, openAIError{Message: fmt.Sprintf("The model %q does not exist", modelName), Type: "invalid_request_error", Code: "model_not_found"})
		return
	}
	if _, authorized := server.authorize(request, modelName); !authorized {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}

	upstream, err := server.chatCompleter.ChatCompletion(request.Context(), deployment, body)
	if err != nil {
		writeJSON(writer, http.StatusBadGateway, openAIError{Message: "Upstream provider request failed", Type: "api_error", Code: "upstream_error"})
		return
	}
	defer upstream.Body.Close()

	copyResponseHeaders(writer.Header(), upstream.Header)
	writer.WriteHeader(upstream.StatusCode)
	if err := copyResponse(writer, upstream.Body); err != nil {
		return
	}
}

func requestedModel(body []byte) (string, error) {
	payload := struct {
		Model string `json:"model"`
	}{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", errors.New("Request body must be valid JSON")
	}
	if payload.Model == "" {
		return "", errors.New("Missing required parameter: 'model'")
	}
	return payload.Model, nil
}

func (server Server) deploymentFor(modelName string) (config.Model, bool) {
	for _, configuredModel := range server.config.Models {
		if configuredModel.Name == modelName {
			return configuredModel, true
		}
	}
	return config.Model{}, false
}

func copyResponseHeaders(destination http.Header, source http.Header) {
	for _, name := range []string{"Content-Type", "Cache-Control", "X-Request-Id"} {
		if value := source.Get(name); value != "" {
			destination.Set(name, value)
		}
	}
}

func copyResponse(writer http.ResponseWriter, body io.Reader) error {
	buffer := make([]byte, 32*1024)
	flusher, canFlush := writer.(http.Flusher)
	for {
		count, readErr := body.Read(buffer)
		if count > 0 {
			if _, writeErr := writer.Write(buffer[:count]); writeErr != nil {
				return writeErr
			}
			if canFlush {
				flusher.Flush()
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func (server Server) health(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (server Server) models(writer http.ResponseWriter, request *http.Request) {
	virtualKey, authorized := server.authorize(request, "")
	if !authorized {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}

	models := make([]modelResponse, 0, len(server.config.Models))
	for _, configuredModel := range server.config.Models {
		if len(virtualKey.Models) > 0 && !contains(virtualKey.Models, configuredModel.Name) {
			continue
		}
		models = append(models, modelResponse{
			ID:      configuredModel.Name,
			Object:  "model",
			Created: 0,
			OwnedBy: providerName(configuredModel.Model),
		})
	}

	writeJSON(writer, http.StatusOK, modelListResponse{Object: "list", Data: models})
}

func (server Server) authorize(request *http.Request, model string) (auth.VirtualKey, bool) {
	provided, found := bearerToken(request)
	if server.config.MasterKey != "" && found && len(provided) == len(server.config.MasterKey) && subtle.ConstantTimeCompare([]byte(provided), []byte(server.config.MasterKey)) == 1 {
		return auth.VirtualKey{}, true
	}
	if server.keyValidator != nil && found {
		virtualKey, err := server.keyValidator.Validate(request.Context(), provided, model)
		if err == nil {
			return virtualKey, true
		}
	}
	return auth.VirtualKey{}, server.config.MasterKey == "" && server.keyValidator == nil
}

func bearerToken(request *http.Request) (string, bool) {
	authorization := request.Header.Get("Authorization")
	if !strings.HasPrefix(authorization, "Bearer ") {
		return "", false
	}
	provided := strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer "))
	return provided, provided != ""
}

func contains(models []string, expected string) bool {
	for _, model := range models {
		if model == expected {
			return true
		}
	}
	return false
}

func providerName(model string) string {
	provider, _, found := strings.Cut(model, "/")
	if !found || provider == "" {
		return "litellm"
	}
	return provider
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

type modelListResponse struct {
	Object string          `json:"object"`
	Data   []modelResponse `json:"data"`
}

type modelResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

type openAIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}
