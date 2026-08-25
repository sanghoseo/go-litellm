package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/BerriAI/litellm/go-proxy/internal/config"
	"github.com/BerriAI/litellm/go-proxy/internal/providers"
)

type Server struct {
	config        config.Config
	chatCompleter providers.ChatCompleter
}

func NewServer(proxyConfig config.Config, completers ...providers.ChatCompleter) Server {
	var chatCompleter providers.ChatCompleter
	if len(completers) > 0 {
		chatCompleter = completers[0]
	}
	return Server{config: proxyConfig, chatCompleter: chatCompleter}
}

func (server Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/liveliness", server.health)
	mux.HandleFunc("GET /health/readiness", server.health)
	mux.HandleFunc("GET /v1/models", server.models)
	mux.HandleFunc("POST /v1/chat/completions", server.chatCompletions)
	return mux
}

func (server Server) chatCompletions(writer http.ResponseWriter, request *http.Request) {
	if !server.authorized(request) {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
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
	if !server.authorized(request) {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}

	models := make([]modelResponse, 0, len(server.config.Models))
	for _, configuredModel := range server.config.Models {
		models = append(models, modelResponse{
			ID:      configuredModel.Name,
			Object:  "model",
			Created: 0,
			OwnedBy: providerName(configuredModel.Model),
		})
	}

	writeJSON(writer, http.StatusOK, modelListResponse{Object: "list", Data: models})
}

func (server Server) authorized(request *http.Request) bool {
	if server.config.MasterKey == "" {
		return true
	}

	provided := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
	if len(provided) != len(server.config.MasterKey) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(server.config.MasterKey)) == 1
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
