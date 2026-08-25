package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/BerriAI/litellm/go-proxy/internal/auth"
	"github.com/BerriAI/litellm/go-proxy/internal/config"
	"github.com/BerriAI/litellm/go-proxy/internal/providers"
	"github.com/BerriAI/litellm/go-proxy/internal/routing"
	"github.com/BerriAI/litellm/go-proxy/internal/usage"
	"github.com/BerriAI/litellm/go-proxy/litellm"
)

type VirtualKeyValidator interface {
	Validate(context.Context, string, string) (auth.VirtualKey, error)
}

type RequestLimiter interface {
	Allow(context.Context, string, int64, time.Duration) (bool, error)
}

type ReadinessCheck interface {
	Ping(context.Context) error
}

type ResponseCache interface {
	Get(context.Context, string) ([]byte, error)
	Set(context.Context, string, []byte, time.Duration) error
}

type Server struct {
	config          config.Config
	chatCompleter   providers.ChatCompleter
	responseMaker   providers.ResponseCreator
	embedder        providers.Embedder
	keyValidator    VirtualKeyValidator
	router          *routing.Router
	usageRecorder   usage.Recorder
	requestLimiter  RequestLimiter
	readinessChecks []ReadinessCheck
	responseCache   ResponseCache
}

func (server Server) WithResponseCache(cache ResponseCache) Server {
	server.responseCache = cache
	return server
}

func NewServer(proxyConfig config.Config, completers ...providers.ChatCompleter) Server {
	var chatCompleter providers.ChatCompleter
	if len(completers) > 0 {
		chatCompleter = completers[0]
	}
	server := Server{config: proxyConfig, chatCompleter: chatCompleter, router: routing.NewWithAliases(proxyConfig.Models, proxyConfig.ModelAliases)}
	server.setOptionalCompleters(chatCompleter)
	return server
}

func NewServerWithVirtualKeyValidator(proxyConfig config.Config, completer providers.ChatCompleter, validator VirtualKeyValidator) Server {
	return NewServerWithDependencies(proxyConfig, completer, validator, nil)
}

func NewServerWithDependencies(proxyConfig config.Config, completer providers.ChatCompleter, validator VirtualKeyValidator, recorder usage.Recorder) Server {
	return NewServerWithRuntime(proxyConfig, completer, validator, recorder, nil)
}

func NewServerWithRuntime(proxyConfig config.Config, completer providers.ChatCompleter, validator VirtualKeyValidator, recorder usage.Recorder, limiter RequestLimiter, readinessChecks ...ReadinessCheck) Server {
	server := Server{config: proxyConfig, chatCompleter: completer, keyValidator: validator, usageRecorder: recorder, requestLimiter: limiter, readinessChecks: readinessChecks, router: routing.NewWithAliases(proxyConfig.Models, proxyConfig.ModelAliases)}
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
	mux.HandleFunc("GET /health/readiness", server.readiness)
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
	virtualKey, authorized := server.authorize(request, modelName)
	if !authorized {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	if !server.allowedByRateLimit(request.Context(), virtualKey, modelName) {
		writeJSON(writer, http.StatusTooManyRequests, openAIError{Message: "Rate limit exceeded", Type: "rate_limit_error", Code: "rate_limit_exceeded"})
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

func (server Server) allowedByRateLimit(ctx context.Context, virtualKey auth.VirtualKey, modelName string) bool {
	if server.requestLimiter == nil || virtualKey.TokenHash == "" || virtualKey.RPMLimit == nil {
		return true
	}
	allowed, err := server.requestLimiter.Allow(ctx, "litellm:rpm:"+virtualKey.TokenHash+":"+modelName, *virtualKey.RPMLimit, time.Minute)
	return err == nil && allowed
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
	virtualKey, authorized := server.authorize(request, modelName)
	if !authorized {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	if !server.allowedByRateLimit(request.Context(), virtualKey, modelName) {
		writeJSON(writer, http.StatusTooManyRequests, openAIError{Message: "Rate limit exceeded", Type: "rate_limit_error", Code: "rate_limit_exceeded"})
		return
	}
	cacheKey := server.cacheKey(body, virtualKey.TokenHash)
	if cacheKey != "" {
		if cached, err := server.responseCache.Get(request.Context(), cacheKey); err == nil {
			writer.Header().Set("Content-Type", "application/json")
			writer.Header().Set("X-LiteLLM-Cache", "hit")
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write(cached)
			return
		}
	}

	startedAt := time.Now().UTC()
	upstream, err := server.chatCompleter.ChatCompletion(request.Context(), deployment, body)
	if err != nil {
		writeJSON(writer, http.StatusBadGateway, openAIError{Message: "Upstream provider request failed", Type: "api_error", Code: "upstream_error"})
		return
	}
	defer upstream.Body.Close()

	copyResponseHeaders(writer.Header(), upstream.Header)
	writer.WriteHeader(upstream.StatusCode)
	responseBody := bytes.Buffer{}
	_ = copyResponse(writer, io.TeeReader(upstream.Body, &responseBody))
	if cacheKey != "" && upstream.StatusCode >= http.StatusOK && upstream.StatusCode < http.StatusMultipleChoices {
		_ = server.responseCache.Set(request.Context(), cacheKey, responseBody.Bytes(), time.Minute)
	}
	server.recordUsage(request.Context(), virtualKey.TokenHash, deployment, responseBody.Bytes(), startedAt, upstream.StatusCode)
}

func (server Server) cacheKey(body []byte, keyHash string) string {
	if server.responseCache == nil || isStreamingRequest(body) {
		return ""
	}
	hash := sha256.Sum256(append(append([]byte(nil), body...), []byte("\x00"+keyHash)...))
	return fmt.Sprintf("litellm:response:%x", hash)
}

func isStreamingRequest(body []byte) bool {
	var payload struct {
		Stream bool `json:"stream"`
	}
	return json.Unmarshal(body, &payload) == nil && payload.Stream
}

func (server Server) recordUsage(ctx context.Context, keyHash string, deployment config.Model, body []byte, startedAt time.Time, statusCode int) {
	if server.usageRecorder == nil {
		return
	}
	requestID, err := litellm.UUID4()
	if err != nil {
		return
	}
	responseUsage, err := usage.UsageFromOpenAIResponse(body)
	if err != nil {
		return
	}
	provider, _, _ := strings.Cut(deployment.Model, "/")
	_ = server.usageRecorder.Insert(ctx, usage.Record{RequestID: requestID, CallType: "chat_completion", APIKeyHash: keyHash, Model: deployment.Name, Provider: provider, APIBase: deployment.APIBase, StartedAt: startedAt, CompletedAt: time.Now().UTC(), Usage: responseUsage, Status: http.StatusText(statusCode)})
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
	if server.router == nil {
		return config.Model{}, false
	}
	model, err := server.router.Select(modelName)
	return model, err == nil
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

func (server Server) readiness(writer http.ResponseWriter, request *http.Request) {
	for _, check := range server.readinessChecks {
		ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
		err := check.Ping(ctx)
		cancel()
		if err != nil {
			writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
			return
		}
	}
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
