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
	"github.com/BerriAI/litellm/go-proxy/internal/observability"
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
	imageGenerator  providers.ImageGenerator
	speechCreator   providers.SpeechCreator
	passthrough     providers.PassthroughClient
	keyValidator    VirtualKeyValidator
	router          *routing.Router
	usageRecorder   usage.Recorder
	requestLimiter  RequestLimiter
	readinessChecks []ReadinessCheck
	responseCache   ResponseCache
	metrics         *observability.Metrics
	keyManager      auth.VirtualKeyManager
}

func (server Server) WithResponseCache(cache ResponseCache) Server {
	server.responseCache = cache
	return server
}

func (server Server) WithVirtualKeyManager(manager auth.VirtualKeyManager) Server {
	server.keyManager = manager
	return server
}

func NewServer(proxyConfig config.Config, completers ...providers.ChatCompleter) Server {
	var chatCompleter providers.ChatCompleter
	if len(completers) > 0 {
		chatCompleter = completers[0]
	}
	server := Server{config: proxyConfig, chatCompleter: chatCompleter, metrics: observability.NewMetrics(), router: routing.NewWithAliases(proxyConfig.Models, proxyConfig.ModelAliases)}
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
	server := Server{config: proxyConfig, chatCompleter: completer, keyValidator: validator, usageRecorder: recorder, requestLimiter: limiter, readinessChecks: readinessChecks, metrics: observability.NewMetrics(), router: routing.NewWithAliases(proxyConfig.Models, proxyConfig.ModelAliases)}
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
	if imageGenerator, ok := completer.(providers.ImageGenerator); ok {
		server.imageGenerator = imageGenerator
	}
	if speechCreator, ok := completer.(providers.SpeechCreator); ok {
		server.speechCreator = speechCreator
	}
	if passthrough, ok := completer.(providers.PassthroughClient); ok {
		server.passthrough = passthrough
	}
}

func (server Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/liveliness", server.health)
	mux.HandleFunc("GET /health/readiness", server.readiness)
	mux.HandleFunc("GET /v1/models", server.models)
	mux.HandleFunc("POST /key/generate", server.generateKey)
	mux.HandleFunc("GET /key/info", server.keyInfo)
	mux.HandleFunc("POST /key/delete", server.deleteKey)
	mux.HandleFunc("POST /key/block", server.blockKey)
	mux.HandleFunc("POST /key/unblock", server.unblockKey)
	mux.HandleFunc("POST /key/update", server.updateKey)
	mux.HandleFunc("GET /key/list", server.listKeys)
	mux.HandleFunc("POST /v1/chat/completions", server.chatCompletions)
	mux.HandleFunc("POST /v1/responses", server.responses)
	mux.HandleFunc("POST /v1/embeddings", server.embeddings)
	mux.HandleFunc("POST /v1/images/generations", server.images)
	mux.HandleFunc("POST /v1/audio/speech", server.speech)
	mux.HandleFunc("POST /v1/audio/transcriptions", server.transcription)
	mux.HandleFunc("POST /v1/audio/translations", server.translation)
	mux.HandleFunc("GET /v1/files", server.files)
	mux.HandleFunc("POST /v1/files", server.files)
	mux.HandleFunc("GET /v1/files/{fileID}", server.file)
	mux.HandleFunc("DELETE /v1/files/{fileID}", server.file)
	mux.HandleFunc("GET /v1/files/{fileID}/content", server.fileContent)
	mux.HandleFunc("POST /v1/batches", server.batches)
	mux.HandleFunc("GET /v1/batches", server.batches)
	mux.HandleFunc("GET /v1/batches/{batchID}", server.batch)
	mux.HandleFunc("POST /v1/batches/{batchID}/cancel", server.cancelBatch)
	mux.HandleFunc("GET /v1/vector_stores", server.vectorStores)
	mux.HandleFunc("POST /v1/vector_stores", server.vectorStores)
	mux.HandleFunc("GET /v1/vector_stores/{vectorStoreID}", server.vectorStore)
	mux.HandleFunc("POST /v1/vector_stores/{vectorStoreID}", server.vectorStore)
	mux.HandleFunc("DELETE /v1/vector_stores/{vectorStoreID}", server.vectorStore)
	mux.HandleFunc("POST /v1/vector_stores/{vectorStoreID}/search", server.vectorStoreSearch)
	mux.HandleFunc("GET /v1/vector_stores/{vectorStoreID}/files", server.vectorStoreFiles)
	mux.HandleFunc("POST /v1/vector_stores/{vectorStoreID}/files", server.vectorStoreFiles)
	mux.HandleFunc("GET /v1/vector_stores/{vectorStoreID}/files/{fileID}", server.vectorStoreFile)
	mux.HandleFunc("POST /v1/vector_stores/{vectorStoreID}/files/{fileID}", server.vectorStoreFile)
	mux.HandleFunc("DELETE /v1/vector_stores/{vectorStoreID}/files/{fileID}", server.vectorStoreFile)
	mux.HandleFunc("GET /v1/vector_stores/{vectorStoreID}/files/{fileID}/content", server.vectorStoreFileContent)
	mux.HandleFunc("GET /v1/vector_stores/{vectorStoreID}/file_batches", server.vectorStoreFileBatches)
	mux.HandleFunc("POST /v1/vector_stores/{vectorStoreID}/file_batches", server.vectorStoreFileBatches)
	mux.HandleFunc("GET /v1/vector_stores/{vectorStoreID}/file_batches/{fileBatchID}", server.vectorStoreFileBatch)
	mux.HandleFunc("POST /v1/vector_stores/{vectorStoreID}/file_batches/{fileBatchID}/cancel", server.cancelVectorStoreFileBatch)
	handler := server.withRequestID(mux)
	if server.metrics == nil {
		return handler
	}
	mux.Handle("GET /metrics", server.metrics.Handler())
	return server.metrics.Wrap(handler)
}

func (server Server) withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestID := request.Header.Get("X-Request-Id")
		if requestID == "" {
			requestID, _ = litellm.UUID4()
		}
		writer.Header().Set("X-Request-Id", requestID)
		next.ServeHTTP(writer, request.WithContext(observability.WithRequestID(request.Context(), requestID)))
	})
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

func (server Server) images(writer http.ResponseWriter, request *http.Request) {
	if server.imageGenerator == nil {
		server.providerUnavailable(writer, "No image generation provider is configured")
		return
	}
	server.forwardModelRequest(writer, request, server.imageGenerator.GenerateImage)
}

func (server Server) speech(writer http.ResponseWriter, request *http.Request) {
	if server.speechCreator == nil {
		server.providerUnavailable(writer, "No speech provider is configured")
		return
	}
	server.forwardModelRequest(writer, request, server.speechCreator.CreateSpeech)
}

func (server Server) transcription(writer http.ResponseWriter, request *http.Request) {
	server.forwardPassthrough(writer, request, "audio/transcriptions")
}

func (server Server) translation(writer http.ResponseWriter, request *http.Request) {
	server.forwardPassthrough(writer, request, "audio/translations")
}

func (server Server) files(writer http.ResponseWriter, request *http.Request) {
	server.forwardPassthrough(writer, request, "files")
}

func (server Server) file(writer http.ResponseWriter, request *http.Request) {
	server.forwardPassthrough(writer, request, "files/"+request.PathValue("fileID"))
}

func (server Server) fileContent(writer http.ResponseWriter, request *http.Request) {
	server.forwardPassthrough(writer, request, "files/"+request.PathValue("fileID")+"/content")
}

func (server Server) batches(writer http.ResponseWriter, request *http.Request) {
	server.forwardPassthrough(writer, request, "batches")
}

func (server Server) batch(writer http.ResponseWriter, request *http.Request) {
	server.forwardPassthrough(writer, request, "batches/"+request.PathValue("batchID"))
}

func (server Server) cancelBatch(writer http.ResponseWriter, request *http.Request) {
	server.forwardPassthrough(writer, request, "batches/"+request.PathValue("batchID")+"/cancel")
}

func (server Server) vectorStores(writer http.ResponseWriter, request *http.Request) {
	server.forwardPassthrough(writer, request, "vector_stores")
}

func (server Server) vectorStore(writer http.ResponseWriter, request *http.Request) {
	server.forwardPassthrough(writer, request, "vector_stores/"+request.PathValue("vectorStoreID"))
}

func (server Server) vectorStoreSearch(writer http.ResponseWriter, request *http.Request) {
	server.forwardPassthrough(writer, request, "vector_stores/"+request.PathValue("vectorStoreID")+"/search")
}

func (server Server) vectorStoreFiles(writer http.ResponseWriter, request *http.Request) {
	server.forwardPassthrough(writer, request, "vector_stores/"+request.PathValue("vectorStoreID")+"/files")
}

func (server Server) vectorStoreFile(writer http.ResponseWriter, request *http.Request) {
	server.forwardPassthrough(writer, request, "vector_stores/"+request.PathValue("vectorStoreID")+"/files/"+request.PathValue("fileID"))
}

func (server Server) vectorStoreFileContent(writer http.ResponseWriter, request *http.Request) {
	server.forwardPassthrough(writer, request, "vector_stores/"+request.PathValue("vectorStoreID")+"/files/"+request.PathValue("fileID")+"/content")
}

func (server Server) vectorStoreFileBatches(writer http.ResponseWriter, request *http.Request) {
	server.forwardPassthrough(writer, request, "vector_stores/"+request.PathValue("vectorStoreID")+"/file_batches")
}

func (server Server) vectorStoreFileBatch(writer http.ResponseWriter, request *http.Request) {
	server.forwardPassthrough(writer, request, "vector_stores/"+request.PathValue("vectorStoreID")+"/file_batches/"+request.PathValue("fileBatchID"))
}

func (server Server) cancelVectorStoreFileBatch(writer http.ResponseWriter, request *http.Request) {
	server.forwardPassthrough(writer, request, "vector_stores/"+request.PathValue("vectorStoreID")+"/file_batches/"+request.PathValue("fileBatchID")+"/cancel")
}

func (server Server) forwardPassthrough(writer http.ResponseWriter, request *http.Request, endpoint string) {
	if server.passthrough == nil {
		server.providerUnavailable(writer, "No OpenAI-compatible resource provider is configured")
		return
	}
	deployment, found := server.defaultDeployment()
	if !found {
		server.providerUnavailable(writer, "No deployment is configured")
		return
	}
	virtualKey, authorized := server.authorize(request, deployment.Name)
	if !authorized {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	if !server.allowedByRateLimit(request.Context(), virtualKey, deployment.Name) {
		writeJSON(writer, http.StatusTooManyRequests, openAIError{Message: "Rate limit exceeded", Type: "rate_limit_error", Code: "rate_limit_exceeded"})
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, 100<<20))
	if err != nil {
		writeJSON(writer, http.StatusRequestEntityTooLarge, openAIError{Message: "Request body exceeds 100 MiB", Type: "invalid_request_error", Code: "request_too_large"})
		return
	}
	upstream, err := server.passthrough.Passthrough(request.Context(), deployment, request.Method, endpoint, request.Header.Get("Content-Type"), body)
	if err != nil {
		writeJSON(writer, http.StatusBadGateway, openAIError{Message: "Upstream provider request failed", Type: "api_error", Code: "upstream_error"})
		return
	}
	defer upstream.Body.Close()
	copyResponseHeaders(writer.Header(), upstream.Header)
	writer.WriteHeader(upstream.StatusCode)
	_ = copyResponse(writer, upstream.Body)
}

type keyGenerateRequest struct {
	Key      string     `json:"key"`
	KeyAlias string     `json:"key_alias"`
	Models   []string   `json:"models"`
	Expires  *time.Time `json:"expires"`
	RPMLimit *int64     `json:"rpm_limit"`
}

type keyResponse struct {
	Key      string     `json:"key,omitempty"`
	KeyAlias string     `json:"key_alias,omitempty"`
	Models   []string   `json:"models"`
	Expires  *time.Time `json:"expires,omitempty"`
	Blocked  bool       `json:"blocked"`
	RPMLimit *int64     `json:"rpm_limit,omitempty"`
}

type keyUpdateRequest struct {
	Key      string     `json:"key"`
	KeyAlias *string    `json:"key_alias"`
	Models   *[]string  `json:"models"`
	Expires  *time.Time `json:"expires"`
	RPMLimit *int64     `json:"rpm_limit"`
}

func (server Server) generateKey(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) || server.keyManager == nil {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	var input keyGenerateRequest
	if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1<<20)).Decode(&input); err != nil {
		writeJSON(writer, http.StatusBadRequest, openAIError{Message: "Request body must be valid JSON", Type: "invalid_request_error", Code: "invalid_request"})
		return
	}
	if input.Expires != nil && !input.Expires.After(time.Now()) {
		writeJSON(writer, http.StatusBadRequest, openAIError{Message: "expires must be in the future", Type: "invalid_request_error", Code: "invalid_request"})
		return
	}
	rawKey := input.Key
	if rawKey == "" {
		value, err := litellm.UUID4()
		if err != nil {
			writeJSON(writer, http.StatusInternalServerError, openAIError{Message: "Could not generate key", Type: "server_error", Code: "key_generation_failed"})
			return
		}
		rawKey = "sk-" + value
	}
	record := auth.ManagedVirtualKey{TokenHash: auth.HashKey(rawKey), KeyAlias: input.KeyAlias, Models: input.Models, ExpiresAt: input.Expires, RPMLimit: input.RPMLimit}
	if err := server.keyManager.CreateVirtualKey(request.Context(), record); err != nil {
		writeJSON(writer, http.StatusInternalServerError, openAIError{Message: "Could not create key", Type: "server_error", Code: "key_creation_failed"})
		return
	}
	writeJSON(writer, http.StatusOK, keyResponse{Key: rawKey, KeyAlias: record.KeyAlias, Models: record.Models, Expires: record.ExpiresAt, Blocked: record.Blocked, RPMLimit: record.RPMLimit})
}

func (server Server) keyInfo(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) || server.keyManager == nil {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	rawKey := request.URL.Query().Get("key")
	if rawKey == "" {
		writeJSON(writer, http.StatusBadRequest, openAIError{Message: "Missing required parameter: 'key'", Type: "invalid_request_error", Code: "invalid_request"})
		return
	}
	record, err := server.keyManager.GetVirtualKey(request.Context(), auth.HashKey(rawKey))
	if err != nil {
		writeJSON(writer, http.StatusNotFound, openAIError{Message: "Key not found", Type: "invalid_request_error", Code: "key_not_found"})
		return
	}
	writeJSON(writer, http.StatusOK, keyResponse{KeyAlias: record.KeyAlias, Models: record.Models, Expires: record.ExpiresAt, Blocked: record.Blocked, RPMLimit: record.RPMLimit})
}

func (server Server) deleteKey(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) || server.keyManager == nil {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	var input struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1<<20)).Decode(&input); err != nil || input.Key == "" {
		writeJSON(writer, http.StatusBadRequest, openAIError{Message: "Missing required parameter: 'key'", Type: "invalid_request_error", Code: "invalid_request"})
		return
	}
	deleted, err := server.keyManager.DeleteVirtualKey(request.Context(), auth.HashKey(input.Key))
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, openAIError{Message: "Could not delete key", Type: "server_error", Code: "key_deletion_failed"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]bool{"deleted": deleted})
}

func (server Server) blockKey(writer http.ResponseWriter, request *http.Request) {
	server.setKeyBlocked(writer, request, true)
}

func (server Server) unblockKey(writer http.ResponseWriter, request *http.Request) {
	server.setKeyBlocked(writer, request, false)
}

func (server Server) setKeyBlocked(writer http.ResponseWriter, request *http.Request, blocked bool) {
	if !server.authorizeAdmin(request) || server.keyManager == nil {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	var input struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1<<20)).Decode(&input); err != nil || input.Key == "" {
		writeJSON(writer, http.StatusBadRequest, openAIError{Message: "Missing required parameter: 'key'", Type: "invalid_request_error", Code: "invalid_request"})
		return
	}
	updated, err := server.keyManager.SetVirtualKeyBlocked(request.Context(), auth.HashKey(input.Key), blocked)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, openAIError{Message: "Could not update key", Type: "server_error", Code: "key_update_failed"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]bool{"updated": updated, "blocked": blocked})
}

func (server Server) updateKey(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) || server.keyManager == nil {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	var input keyUpdateRequest
	if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1<<20)).Decode(&input); err != nil || input.Key == "" {
		writeJSON(writer, http.StatusBadRequest, openAIError{Message: "Missing required parameter: 'key'", Type: "invalid_request_error", Code: "invalid_request"})
		return
	}
	if input.KeyAlias == nil && input.Models == nil && input.Expires == nil && input.RPMLimit == nil {
		writeJSON(writer, http.StatusBadRequest, openAIError{Message: "No key attributes to update", Type: "invalid_request_error", Code: "invalid_request"})
		return
	}
	if input.Expires != nil && !input.Expires.After(time.Now()) {
		writeJSON(writer, http.StatusBadRequest, openAIError{Message: "expires must be in the future", Type: "invalid_request_error", Code: "invalid_request"})
		return
	}
	updated, err := server.keyManager.UpdateVirtualKey(request.Context(), auth.HashKey(input.Key), auth.ManagedVirtualKeyUpdate{KeyAlias: input.KeyAlias, Models: input.Models, ExpiresAt: input.Expires, RPMLimit: input.RPMLimit})
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, openAIError{Message: "Could not update key", Type: "server_error", Code: "key_update_failed"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]bool{"updated": updated})
}

func (server Server) listKeys(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) || server.keyManager == nil {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	keys, err := server.keyManager.ListVirtualKeys(request.Context(), 100)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, openAIError{Message: "Could not list keys", Type: "server_error", Code: "key_list_failed"})
		return
	}
	response := make([]keyResponse, 0, len(keys))
	for _, key := range keys {
		response = append(response, keyResponse{KeyAlias: key.KeyAlias, Models: key.Models, Expires: key.ExpiresAt, Blocked: key.Blocked, RPMLimit: key.RPMLimit})
	}
	writeJSON(writer, http.StatusOK, map[string]any{"data": response})
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
	upstream, err := server.completeModelWithFallback(request.Context(), modelName, deployment, body, completer)
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
	upstream, err := server.completeModelWithFallback(request.Context(), modelName, deployment, body, server.chatCompleter.ChatCompletion)
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

func (server Server) completeModelWithFallback(ctx context.Context, modelName string, deployment config.Model, body []byte, completer modelRequestCompleter) (providers.Response, error) {
	failed := []config.Model{}
	for {
		response, err := completer(ctx, deployment, body)
		if err == nil {
			return response, nil
		}
		failed = append(failed, deployment)
		if server.router == nil {
			return providers.Response{}, err
		}
		next, fallbackErr := server.router.Fallback(modelName, failed)
		if fallbackErr != nil {
			return providers.Response{}, err
		}
		deployment = next
	}
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

func (server Server) defaultDeployment() (config.Model, bool) {
	if len(server.config.Models) == 0 {
		return config.Model{}, false
	}
	return server.config.Models[0], true
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

func (server Server) authorizeAdmin(request *http.Request) bool {
	provided, found := bearerToken(request)
	return server.config.MasterKey != "" && found && len(provided) == len(server.config.MasterKey) && subtle.ConstantTimeCompare([]byte(provided), []byte(server.config.MasterKey)) == 1
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
