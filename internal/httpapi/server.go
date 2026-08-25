package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/BerriAI/litellm/go-proxy/internal/config"
)

type Server struct {
	config config.Config
}

func NewServer(proxyConfig config.Config) Server {
	return Server{config: proxyConfig}
}

func (server Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/liveliness", server.health)
	mux.HandleFunc("GET /health/readiness", server.health)
	mux.HandleFunc("GET /v1/models", server.models)
	return mux
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
