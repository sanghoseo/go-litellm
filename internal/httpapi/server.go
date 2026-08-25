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
	"math"
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
	config              config.Config
	chatCompleter       providers.ChatCompleter
	textCompleter       providers.TextCompleter
	responseMaker       providers.ResponseCreator
	embedder            providers.Embedder
	imageGenerator      providers.ImageGenerator
	speechCreator       providers.SpeechCreator
	moderator           providers.Moderator
	reranker            providers.Reranker
	passthrough         providers.PassthroughClient
	keyValidator        VirtualKeyValidator
	router              *routing.Router
	usageRecorder       usage.Recorder
	spendLogReader      usage.LogReader
	requestLimiter      RequestLimiter
	readinessChecks     []ReadinessCheck
	responseCache       ResponseCache
	metrics             *observability.Metrics
	keyManager          auth.VirtualKeyManager
	teamManager         auth.TeamManager
	userManager         auth.UserManager
	projectManager      auth.ProjectManager
	organizationManager auth.OrganizationManager
	budgetManager       auth.BudgetManager
}

func (server Server) WithResponseCache(cache ResponseCache) Server {
	server.responseCache = cache
	return server
}

func (server Server) WithSpendLogReader(reader usage.LogReader) Server {
	server.spendLogReader = reader
	return server
}

func (server Server) WithVirtualKeyManager(manager auth.VirtualKeyManager) Server {
	server.keyManager = manager
	return server
}

func (server Server) WithTeamManager(manager auth.TeamManager) Server {
	server.teamManager = manager
	return server
}

func (server Server) WithUserManager(manager auth.UserManager) Server {
	server.userManager = manager
	return server
}

func (server Server) WithProjectManager(manager auth.ProjectManager) Server {
	server.projectManager = manager
	return server
}

func (server Server) WithOrganizationManager(manager auth.OrganizationManager) Server {
	server.organizationManager = manager
	return server
}

func (server Server) WithBudgetManager(manager auth.BudgetManager) Server {
	server.budgetManager = manager
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
	if textCompleter, ok := completer.(providers.TextCompleter); ok {
		server.textCompleter = textCompleter
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
	if moderator, ok := completer.(providers.Moderator); ok {
		server.moderator = moderator
	}
	if reranker, ok := completer.(providers.Reranker); ok {
		server.reranker = reranker
	}
	if passthrough, ok := completer.(providers.PassthroughClient); ok {
		server.passthrough = passthrough
	}
}

func (server Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", server.health)
	mux.HandleFunc("GET /health/liveliness", server.health)
	mux.HandleFunc("GET /health/readiness", server.readiness)
	mux.HandleFunc("GET /v1/models", server.models)
	mux.HandleFunc("GET /v1/models/{modelID}", server.model)
	mux.HandleFunc("GET /spend/logs", server.spendLogs)
	mux.HandleFunc("POST /key/generate", server.generateKey)
	mux.HandleFunc("GET /key/info", server.keyInfo)
	mux.HandleFunc("POST /key/delete", server.deleteKey)
	mux.HandleFunc("POST /key/block", server.blockKey)
	mux.HandleFunc("POST /key/unblock", server.unblockKey)
	mux.HandleFunc("POST /key/update", server.updateKey)
	mux.HandleFunc("GET /key/list", server.listKeys)
	mux.HandleFunc("POST /key/regenerate", server.regenerateKey)
	mux.HandleFunc("POST /organization/new", server.createOrganization)
	mux.HandleFunc("GET /organization/info", server.organizationInfo)
	mux.HandleFunc("GET /organization/list", server.listOrganizations)
	mux.HandleFunc("POST /organization/update", server.updateOrganization)
	mux.HandleFunc("POST /organization/delete", server.deleteOrganization)
	mux.HandleFunc("POST /budget/new", server.createBudget)
	mux.HandleFunc("POST /budget/info", server.budgetInfo)
	mux.HandleFunc("GET /budget/list", server.listBudgets)
	mux.HandleFunc("POST /budget/update", server.updateBudget)
	mux.HandleFunc("POST /budget/delete", server.deleteBudget)
	mux.HandleFunc("POST /team/new", server.createTeam)
	mux.HandleFunc("GET /team/info", server.teamInfo)
	mux.HandleFunc("GET /team/list", server.listTeams)
	mux.HandleFunc("POST /team/update", server.updateTeam)
	mux.HandleFunc("POST /team/block", server.blockTeam)
	mux.HandleFunc("POST /team/unblock", server.unblockTeam)
	mux.HandleFunc("POST /team/delete", server.deleteTeam)
	mux.HandleFunc("POST /user/new", server.createUser)
	mux.HandleFunc("GET /user/info", server.userInfo)
	mux.HandleFunc("GET /user/list", server.listUsers)
	mux.HandleFunc("POST /user/update", server.updateUser)
	mux.HandleFunc("POST /user/block", server.blockUser)
	mux.HandleFunc("POST /user/unblock", server.unblockUser)
	mux.HandleFunc("POST /user/delete", server.deleteUser)
	mux.HandleFunc("POST /project/new", server.createProject)
	mux.HandleFunc("GET /project/info", server.projectInfo)
	mux.HandleFunc("GET /project/list", server.listProjects)
	mux.HandleFunc("POST /project/update", server.updateProject)
	mux.HandleFunc("POST /project/block", server.blockProject)
	mux.HandleFunc("POST /project/unblock", server.unblockProject)
	mux.HandleFunc("POST /project/delete", server.deleteProject)
	mux.HandleFunc("POST /v1/chat/completions", server.chatCompletions)
	mux.HandleFunc("POST /v1/completions", server.completions)
	mux.HandleFunc("POST /v1/responses", server.responses)
	mux.HandleFunc("POST /v1/embeddings", server.embeddings)
	mux.HandleFunc("POST /v1/moderations", server.moderations)
	mux.HandleFunc("POST /v1/rerank", server.rerank)
	mux.HandleFunc("POST /v1/images/generations", server.images)
	mux.HandleFunc("POST /v1/images/edits", server.imageEdits)
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
	mux.HandleFunc("GET /v1/fine_tuning/jobs", server.fineTuningJobs)
	mux.HandleFunc("POST /v1/fine_tuning/jobs", server.fineTuningJobs)
	mux.HandleFunc("GET /v1/fine_tuning/jobs/{jobID}", server.fineTuningJob)
	mux.HandleFunc("POST /v1/fine_tuning/jobs/{jobID}/cancel", server.cancelFineTuningJob)
	mux.HandleFunc("GET /v1/fine_tuning/jobs/{jobID}/events", server.fineTuningJobEvents)
	mux.HandleFunc("GET /v1/fine_tuning/jobs/{jobID}/checkpoints", server.fineTuningJobCheckpoints)
	mux.HandleFunc("GET /v1/containers", server.containers)
	mux.HandleFunc("POST /v1/containers", server.containers)
	mux.HandleFunc("GET /v1/containers/{containerID}", server.container)
	mux.HandleFunc("DELETE /v1/containers/{containerID}", server.container)
	mux.HandleFunc("GET /v1/containers/{containerID}/files", server.containerFiles)
	mux.HandleFunc("POST /v1/containers/{containerID}/files", server.containerFiles)
	mux.HandleFunc("GET /v1/containers/{containerID}/files/{fileID}", server.containerFile)
	mux.HandleFunc("DELETE /v1/containers/{containerID}/files/{fileID}", server.containerFile)
	mux.HandleFunc("GET /v1/containers/{containerID}/files/{fileID}/content", server.containerFileContent)
	mux.HandleFunc("GET /v1/assistants", server.assistants)
	mux.HandleFunc("POST /v1/assistants", server.assistants)
	mux.HandleFunc("GET /v1/assistants/{assistantID}", server.assistant)
	mux.HandleFunc("POST /v1/assistants/{assistantID}", server.assistant)
	mux.HandleFunc("DELETE /v1/assistants/{assistantID}", server.assistant)
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
	server.forwardModelRequest(writer, request, "responses", server.responseMaker.CreateResponse)
}

func (server Server) embeddings(writer http.ResponseWriter, request *http.Request) {
	if server.embedder == nil {
		server.providerUnavailable(writer, "No embedding provider is configured")
		return
	}
	server.forwardModelRequest(writer, request, "embedding", server.embedder.CreateEmbedding)
}

func (server Server) moderations(writer http.ResponseWriter, request *http.Request) {
	if server.moderator == nil {
		server.providerUnavailable(writer, "No moderation provider is configured")
		return
	}
	server.forwardModelRequest(writer, request, "moderation", server.moderator.Moderate)
}

func (server Server) rerank(writer http.ResponseWriter, request *http.Request) {
	if server.reranker == nil {
		server.providerUnavailable(writer, "No rerank provider is configured")
		return
	}
	server.forwardModelRequest(writer, request, "rerank", server.reranker.Rerank)
}

func (server Server) images(writer http.ResponseWriter, request *http.Request) {
	if server.imageGenerator == nil {
		server.providerUnavailable(writer, "No image generation provider is configured")
		return
	}
	server.forwardModelRequest(writer, request, "image_generation", server.imageGenerator.GenerateImage)
}

func (server Server) imageEdits(writer http.ResponseWriter, request *http.Request) {
	server.forwardPassthrough(writer, request, "images/edits")
}

func (server Server) speech(writer http.ResponseWriter, request *http.Request) {
	if server.speechCreator == nil {
		server.providerUnavailable(writer, "No speech provider is configured")
		return
	}
	server.forwardModelRequest(writer, request, "speech", server.speechCreator.CreateSpeech)
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

func (server Server) fineTuningJobs(writer http.ResponseWriter, request *http.Request) {
	server.forwardPassthrough(writer, request, "fine_tuning/jobs")
}
func (server Server) fineTuningJob(writer http.ResponseWriter, request *http.Request) {
	server.forwardPassthrough(writer, request, "fine_tuning/jobs/"+request.PathValue("jobID"))
}
func (server Server) cancelFineTuningJob(writer http.ResponseWriter, request *http.Request) {
	server.forwardPassthrough(writer, request, "fine_tuning/jobs/"+request.PathValue("jobID")+"/cancel")
}
func (server Server) fineTuningJobEvents(writer http.ResponseWriter, request *http.Request) {
	server.forwardPassthrough(writer, request, "fine_tuning/jobs/"+request.PathValue("jobID")+"/events")
}
func (server Server) fineTuningJobCheckpoints(writer http.ResponseWriter, request *http.Request) {
	server.forwardPassthrough(writer, request, "fine_tuning/jobs/"+request.PathValue("jobID")+"/checkpoints")
}

func (server Server) containers(writer http.ResponseWriter, request *http.Request) {
	server.forwardPassthrough(writer, request, "containers")
}
func (server Server) container(writer http.ResponseWriter, request *http.Request) {
	server.forwardPassthrough(writer, request, "containers/"+request.PathValue("containerID"))
}
func (server Server) containerFiles(writer http.ResponseWriter, request *http.Request) {
	server.forwardPassthrough(writer, request, "containers/"+request.PathValue("containerID")+"/files")
}
func (server Server) containerFile(writer http.ResponseWriter, request *http.Request) {
	server.forwardPassthrough(writer, request, "containers/"+request.PathValue("containerID")+"/files/"+request.PathValue("fileID"))
}
func (server Server) containerFileContent(writer http.ResponseWriter, request *http.Request) {
	server.forwardPassthrough(writer, request, "containers/"+request.PathValue("containerID")+"/files/"+request.PathValue("fileID")+"/content")
}
func (server Server) assistants(writer http.ResponseWriter, request *http.Request) {
	server.forwardPassthrough(writer, request, "assistants")
}
func (server Server) assistant(writer http.ResponseWriter, request *http.Request) {
	server.forwardPassthrough(writer, request, "assistants/"+request.PathValue("assistantID"))
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
	Key            string     `json:"key"`
	KeyAlias       string     `json:"key_alias"`
	Models         []string   `json:"models"`
	UserID         string     `json:"user_id"`
	TeamID         string     `json:"team_id"`
	ProjectID      string     `json:"project_id"`
	OrganizationID string     `json:"organization_id"`
	BudgetID       string     `json:"budget_id"`
	Expires        *time.Time `json:"expires"`
	RPMLimit       *int64     `json:"rpm_limit"`
}

type keyResponse struct {
	Key            string     `json:"key,omitempty"`
	KeyAlias       string     `json:"key_alias,omitempty"`
	Models         []string   `json:"models"`
	UserID         string     `json:"user_id,omitempty"`
	TeamID         string     `json:"team_id,omitempty"`
	ProjectID      string     `json:"project_id,omitempty"`
	OrganizationID string     `json:"organization_id,omitempty"`
	BudgetID       string     `json:"budget_id,omitempty"`
	Expires        *time.Time `json:"expires,omitempty"`
	Blocked        bool       `json:"blocked"`
	RPMLimit       *int64     `json:"rpm_limit,omitempty"`
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
	record := auth.ManagedVirtualKey{TokenHash: auth.HashKey(rawKey), KeyAlias: input.KeyAlias, Models: input.Models, UserID: input.UserID, TeamID: input.TeamID, ProjectID: input.ProjectID, OrganizationID: input.OrganizationID, BudgetID: input.BudgetID, ExpiresAt: input.Expires, RPMLimit: input.RPMLimit}
	if err := server.keyManager.CreateVirtualKey(request.Context(), record); err != nil {
		writeJSON(writer, http.StatusInternalServerError, openAIError{Message: "Could not create key", Type: "server_error", Code: "key_creation_failed"})
		return
	}
	writeJSON(writer, http.StatusOK, keyResponse{Key: rawKey, KeyAlias: record.KeyAlias, Models: record.Models, UserID: record.UserID, TeamID: record.TeamID, ProjectID: record.ProjectID, OrganizationID: record.OrganizationID, BudgetID: record.BudgetID, Expires: record.ExpiresAt, Blocked: record.Blocked, RPMLimit: record.RPMLimit})
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
	writeJSON(writer, http.StatusOK, keyResponse{KeyAlias: record.KeyAlias, Models: record.Models, UserID: record.UserID, TeamID: record.TeamID, ProjectID: record.ProjectID, OrganizationID: record.OrganizationID, BudgetID: record.BudgetID, Expires: record.ExpiresAt, Blocked: record.Blocked, RPMLimit: record.RPMLimit})
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
		response = append(response, keyResponse{KeyAlias: key.KeyAlias, Models: key.Models, UserID: key.UserID, TeamID: key.TeamID, ProjectID: key.ProjectID, OrganizationID: key.OrganizationID, BudgetID: key.BudgetID, Expires: key.ExpiresAt, Blocked: key.Blocked, RPMLimit: key.RPMLimit})
	}
	writeJSON(writer, http.StatusOK, map[string]any{"data": response})
}

func (server Server) regenerateKey(writer http.ResponseWriter, request *http.Request) {
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
	value, err := litellm.UUID4()
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, openAIError{Message: "Could not generate key", Type: "server_error", Code: "key_generation_failed"})
		return
	}
	rawKey := "sk-" + value
	record, err := server.keyManager.RegenerateVirtualKey(request.Context(), auth.HashKey(input.Key), auth.HashKey(rawKey))
	if err != nil {
		writeJSON(writer, http.StatusNotFound, openAIError{Message: "Key not found", Type: "invalid_request_error", Code: "key_not_found"})
		return
	}
	writeJSON(writer, http.StatusOK, keyResponse{Key: rawKey, KeyAlias: record.KeyAlias, Models: record.Models, UserID: record.UserID, TeamID: record.TeamID, ProjectID: record.ProjectID, OrganizationID: record.OrganizationID, BudgetID: record.BudgetID, Expires: record.ExpiresAt, Blocked: record.Blocked, RPMLimit: record.RPMLimit})
}

type teamRequest struct {
	TeamID    string   `json:"team_id"`
	TeamAlias string   `json:"team_alias"`
	Admins    []string `json:"admins"`
	Members   []string `json:"members"`
	Models    []string `json:"models"`
}

type teamResponse struct {
	TeamID    string   `json:"team_id"`
	TeamAlias string   `json:"team_alias,omitempty"`
	Admins    []string `json:"admins"`
	Members   []string `json:"members"`
	Models    []string `json:"models"`
	Blocked   bool     `json:"blocked"`
}

type teamUpdateRequest struct {
	TeamID    string    `json:"team_id"`
	TeamAlias *string   `json:"team_alias"`
	Admins    *[]string `json:"admins"`
	Members   *[]string `json:"members"`
	Models    *[]string `json:"models"`
	Blocked   *bool     `json:"blocked"`
}

func (server Server) createTeam(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) || server.teamManager == nil {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	var input teamRequest
	if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1<<20)).Decode(&input); err != nil {
		writeJSON(writer, http.StatusBadRequest, openAIError{Message: "Request body must be valid JSON", Type: "invalid_request_error", Code: "invalid_request"})
		return
	}
	if input.TeamID == "" {
		id, err := litellm.UUID4()
		if err != nil {
			writeJSON(writer, http.StatusInternalServerError, openAIError{Message: "Could not generate team id", Type: "server_error", Code: "team_creation_failed"})
			return
		}
		input.TeamID = "team-" + id
	}
	team := auth.ManagedTeam{TeamID: input.TeamID, TeamAlias: input.TeamAlias, Admins: input.Admins, Members: input.Members, Models: input.Models}
	if err := server.teamManager.CreateTeam(request.Context(), team); err != nil {
		writeJSON(writer, http.StatusInternalServerError, openAIError{Message: "Could not create team", Type: "server_error", Code: "team_creation_failed"})
		return
	}
	writeJSON(writer, http.StatusOK, teamResponseFrom(team))
}

func (server Server) teamInfo(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) || server.teamManager == nil {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	teamID := request.URL.Query().Get("team_id")
	if teamID == "" {
		writeJSON(writer, http.StatusBadRequest, openAIError{Message: "Missing required parameter: 'team_id'", Type: "invalid_request_error", Code: "invalid_request"})
		return
	}
	team, err := server.teamManager.GetTeam(request.Context(), teamID)
	if err != nil {
		writeJSON(writer, http.StatusNotFound, openAIError{Message: "Team not found", Type: "invalid_request_error", Code: "team_not_found"})
		return
	}
	writeJSON(writer, http.StatusOK, teamResponseFrom(team))
}

func (server Server) listTeams(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) || server.teamManager == nil {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	teams, err := server.teamManager.ListTeams(request.Context(), 100)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, openAIError{Message: "Could not list teams", Type: "server_error", Code: "team_list_failed"})
		return
	}
	response := make([]teamResponse, 0, len(teams))
	for _, team := range teams {
		response = append(response, teamResponseFrom(team))
	}
	writeJSON(writer, http.StatusOK, map[string]any{"data": response})
}

func (server Server) updateTeam(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) || server.teamManager == nil {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	var input teamUpdateRequest
	if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1<<20)).Decode(&input); err != nil || input.TeamID == "" {
		writeJSON(writer, http.StatusBadRequest, openAIError{Message: "Missing required parameter: 'team_id'", Type: "invalid_request_error", Code: "invalid_request"})
		return
	}
	updated, err := server.teamManager.UpdateTeam(request.Context(), input.TeamID, auth.ManagedTeamUpdate{TeamAlias: input.TeamAlias, Admins: input.Admins, Members: input.Members, Models: input.Models, Blocked: input.Blocked})
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, openAIError{Message: "Could not update team", Type: "server_error", Code: "team_update_failed"})
		return
	}
	if !updated {
		writeJSON(writer, http.StatusNotFound, openAIError{Message: "Team not found", Type: "invalid_request_error", Code: "team_not_found"})
		return
	}
	team, err := server.teamManager.GetTeam(request.Context(), input.TeamID)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, openAIError{Message: "Could not read updated team", Type: "server_error", Code: "team_update_failed"})
		return
	}
	writeJSON(writer, http.StatusOK, teamResponseFrom(team))
}

func (server Server) blockTeam(writer http.ResponseWriter, request *http.Request) {
	server.setTeamBlocked(writer, request, true)
}
func (server Server) unblockTeam(writer http.ResponseWriter, request *http.Request) {
	server.setTeamBlocked(writer, request, false)
}

func (server Server) setTeamBlocked(writer http.ResponseWriter, request *http.Request, blocked bool) {
	if !server.authorizeAdmin(request) || server.teamManager == nil {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	var input struct {
		TeamID string `json:"team_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1<<20)).Decode(&input); err != nil || input.TeamID == "" {
		writeJSON(writer, http.StatusBadRequest, openAIError{Message: "Missing required parameter: 'team_id'", Type: "invalid_request_error", Code: "invalid_request"})
		return
	}
	updated, err := server.teamManager.SetTeamBlocked(request.Context(), input.TeamID, blocked)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, openAIError{Message: "Could not update team", Type: "server_error", Code: "team_update_failed"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]bool{"updated": updated, "blocked": blocked})
}

func (server Server) deleteTeam(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) || server.teamManager == nil {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	var input struct {
		TeamID string `json:"team_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1<<20)).Decode(&input); err != nil || input.TeamID == "" {
		writeJSON(writer, http.StatusBadRequest, openAIError{Message: "Missing required parameter: 'team_id'", Type: "invalid_request_error", Code: "invalid_request"})
		return
	}
	deleted, err := server.teamManager.DeleteTeam(request.Context(), input.TeamID)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, openAIError{Message: "Could not delete team", Type: "server_error", Code: "team_deletion_failed"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]bool{"deleted": deleted})
}

func teamResponseFrom(team auth.ManagedTeam) teamResponse {
	return teamResponse{TeamID: team.TeamID, TeamAlias: team.TeamAlias, Admins: nonNilStrings(team.Admins), Members: nonNilStrings(team.Members), Models: nonNilStrings(team.Models), Blocked: team.Blocked}
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

type userRequest struct {
	UserID    string   `json:"user_id"`
	UserAlias string   `json:"user_alias"`
	TeamID    string   `json:"team_id"`
	UserEmail string   `json:"user_email"`
	Models    []string `json:"models"`
}

type userResponse struct {
	UserID    string   `json:"user_id"`
	UserAlias string   `json:"user_alias,omitempty"`
	TeamID    string   `json:"team_id,omitempty"`
	UserEmail string   `json:"user_email,omitempty"`
	Models    []string `json:"models"`
	Blocked   bool     `json:"blocked"`
}

type userUpdateRequest struct {
	UserID    string    `json:"user_id"`
	UserAlias *string   `json:"user_alias"`
	TeamID    *string   `json:"team_id"`
	UserEmail *string   `json:"user_email"`
	Models    *[]string `json:"models"`
	Blocked   *bool     `json:"blocked"`
}

func (server Server) createUser(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) || server.userManager == nil {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	var input userRequest
	if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1<<20)).Decode(&input); err != nil {
		writeJSON(writer, http.StatusBadRequest, openAIError{Message: "Request body must be valid JSON", Type: "invalid_request_error", Code: "invalid_request"})
		return
	}
	if input.UserID == "" {
		id, err := litellm.UUID4()
		if err != nil {
			writeJSON(writer, http.StatusInternalServerError, openAIError{Message: "Could not generate user id", Type: "server_error", Code: "user_creation_failed"})
			return
		}
		input.UserID = "user-" + id
	}
	user := auth.ManagedUser{UserID: input.UserID, UserAlias: input.UserAlias, TeamID: input.TeamID, UserEmail: input.UserEmail, Models: input.Models}
	if err := server.userManager.CreateUser(request.Context(), user); err != nil {
		writeJSON(writer, http.StatusInternalServerError, openAIError{Message: "Could not create user", Type: "server_error", Code: "user_creation_failed"})
		return
	}
	writeJSON(writer, http.StatusOK, userResponseFrom(user))
}

func (server Server) userInfo(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) || server.userManager == nil {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	userID := request.URL.Query().Get("user_id")
	if userID == "" {
		writeJSON(writer, http.StatusBadRequest, openAIError{Message: "Missing required parameter: 'user_id'", Type: "invalid_request_error", Code: "invalid_request"})
		return
	}
	user, err := server.userManager.GetUser(request.Context(), userID)
	if err != nil {
		writeJSON(writer, http.StatusNotFound, openAIError{Message: "User not found", Type: "invalid_request_error", Code: "user_not_found"})
		return
	}
	writeJSON(writer, http.StatusOK, userResponseFrom(user))
}

func (server Server) listUsers(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) || server.userManager == nil {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	users, err := server.userManager.ListUsers(request.Context(), 100)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, openAIError{Message: "Could not list users", Type: "server_error", Code: "user_list_failed"})
		return
	}
	response := make([]userResponse, 0, len(users))
	for _, user := range users {
		response = append(response, userResponseFrom(user))
	}
	writeJSON(writer, http.StatusOK, map[string]any{"data": response})
}

func (server Server) updateUser(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) || server.userManager == nil {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	var input userUpdateRequest
	if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1<<20)).Decode(&input); err != nil || input.UserID == "" {
		writeJSON(writer, http.StatusBadRequest, openAIError{Message: "Missing required parameter: 'user_id'", Type: "invalid_request_error", Code: "invalid_request"})
		return
	}
	updated, err := server.userManager.UpdateUser(request.Context(), input.UserID, auth.ManagedUserUpdate{UserAlias: input.UserAlias, TeamID: input.TeamID, UserEmail: input.UserEmail, Models: input.Models, Blocked: input.Blocked})
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, openAIError{Message: "Could not update user", Type: "server_error", Code: "user_update_failed"})
		return
	}
	if !updated {
		writeJSON(writer, http.StatusNotFound, openAIError{Message: "User not found", Type: "invalid_request_error", Code: "user_not_found"})
		return
	}
	user, err := server.userManager.GetUser(request.Context(), input.UserID)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, openAIError{Message: "Could not read updated user", Type: "server_error", Code: "user_update_failed"})
		return
	}
	writeJSON(writer, http.StatusOK, userResponseFrom(user))
}

func (server Server) blockUser(writer http.ResponseWriter, request *http.Request) {
	server.setUserBlocked(writer, request, true)
}
func (server Server) unblockUser(writer http.ResponseWriter, request *http.Request) {
	server.setUserBlocked(writer, request, false)
}

func (server Server) setUserBlocked(writer http.ResponseWriter, request *http.Request, blocked bool) {
	if !server.authorizeAdmin(request) || server.userManager == nil {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	var input struct {
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1<<20)).Decode(&input); err != nil || input.UserID == "" {
		writeJSON(writer, http.StatusBadRequest, openAIError{Message: "Missing required parameter: 'user_id'", Type: "invalid_request_error", Code: "invalid_request"})
		return
	}
	updated, err := server.userManager.SetUserBlocked(request.Context(), input.UserID, blocked)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, openAIError{Message: "Could not update user", Type: "server_error", Code: "user_update_failed"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]bool{"updated": updated, "blocked": blocked})
}

func (server Server) deleteUser(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) || server.userManager == nil {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	var input struct {
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1<<20)).Decode(&input); err != nil || input.UserID == "" {
		writeJSON(writer, http.StatusBadRequest, openAIError{Message: "Missing required parameter: 'user_id'", Type: "invalid_request_error", Code: "invalid_request"})
		return
	}
	deleted, err := server.userManager.DeleteUser(request.Context(), input.UserID)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, openAIError{Message: "Could not delete user", Type: "server_error", Code: "user_deletion_failed"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]bool{"deleted": deleted})
}

func userResponseFrom(user auth.ManagedUser) userResponse {
	return userResponse{UserID: user.UserID, UserAlias: user.UserAlias, TeamID: user.TeamID, UserEmail: user.UserEmail, Models: nonNilStrings(user.Models), Blocked: user.Blocked}
}

type projectRequest struct {
	ProjectID    string   `json:"project_id"`
	ProjectAlias string   `json:"project_alias"`
	Description  string   `json:"description"`
	TeamID       string   `json:"team_id"`
	BudgetID     string   `json:"budget_id"`
	Models       []string `json:"models"`
}

type projectResponse struct {
	ProjectID    string   `json:"project_id"`
	ProjectAlias string   `json:"project_alias,omitempty"`
	Description  string   `json:"description,omitempty"`
	TeamID       string   `json:"team_id,omitempty"`
	BudgetID     string   `json:"budget_id,omitempty"`
	Models       []string `json:"models"`
	Blocked      bool     `json:"blocked"`
}

type projectUpdateRequest struct {
	ProjectID    string    `json:"project_id"`
	ProjectAlias *string   `json:"project_alias"`
	Description  *string   `json:"description"`
	TeamID       *string   `json:"team_id"`
	BudgetID     *string   `json:"budget_id"`
	Models       *[]string `json:"models"`
	Blocked      *bool     `json:"blocked"`
}

func (server Server) createProject(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) || server.projectManager == nil {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	var input projectRequest
	if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1<<20)).Decode(&input); err != nil {
		writeJSON(writer, http.StatusBadRequest, openAIError{Message: "Request body must be valid JSON", Type: "invalid_request_error", Code: "invalid_request"})
		return
	}
	if input.TeamID == "" {
		writeJSON(writer, http.StatusBadRequest, openAIError{Message: "Missing required parameter: 'team_id'", Type: "invalid_request_error", Code: "invalid_request"})
		return
	}
	if input.ProjectID == "" {
		id, err := litellm.UUID4()
		if err != nil {
			writeJSON(writer, http.StatusInternalServerError, openAIError{Message: "Could not generate project id", Type: "server_error", Code: "project_creation_failed"})
			return
		}
		input.ProjectID = "project-" + id
	}
	project := auth.ManagedProject{ProjectID: input.ProjectID, ProjectAlias: input.ProjectAlias, Description: input.Description, TeamID: input.TeamID, BudgetID: input.BudgetID, Models: input.Models}
	if err := server.projectManager.CreateProject(request.Context(), project); err != nil {
		writeJSON(writer, http.StatusInternalServerError, openAIError{Message: "Could not create project", Type: "server_error", Code: "project_creation_failed"})
		return
	}
	writeJSON(writer, http.StatusOK, projectResponseFrom(project))
}

func (server Server) projectInfo(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) || server.projectManager == nil {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	projectID := request.URL.Query().Get("project_id")
	if projectID == "" {
		writeJSON(writer, http.StatusBadRequest, openAIError{Message: "Missing required parameter: 'project_id'", Type: "invalid_request_error", Code: "invalid_request"})
		return
	}
	project, err := server.projectManager.GetProject(request.Context(), projectID)
	if err != nil {
		writeJSON(writer, http.StatusNotFound, openAIError{Message: "Project not found", Type: "invalid_request_error", Code: "project_not_found"})
		return
	}
	writeJSON(writer, http.StatusOK, projectResponseFrom(project))
}

func (server Server) listProjects(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) || server.projectManager == nil {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	projects, err := server.projectManager.ListProjects(request.Context(), 100)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, openAIError{Message: "Could not list projects", Type: "server_error", Code: "project_list_failed"})
		return
	}
	response := make([]projectResponse, 0, len(projects))
	for _, project := range projects {
		response = append(response, projectResponseFrom(project))
	}
	writeJSON(writer, http.StatusOK, map[string]any{"data": response})
}

func (server Server) updateProject(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) || server.projectManager == nil {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	var input projectUpdateRequest
	if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1<<20)).Decode(&input); err != nil || input.ProjectID == "" {
		writeJSON(writer, http.StatusBadRequest, openAIError{Message: "Missing required parameter: 'project_id'", Type: "invalid_request_error", Code: "invalid_request"})
		return
	}
	updated, err := server.projectManager.UpdateProject(request.Context(), input.ProjectID, auth.ManagedProjectUpdate{ProjectAlias: input.ProjectAlias, Description: input.Description, TeamID: input.TeamID, BudgetID: input.BudgetID, Models: input.Models, Blocked: input.Blocked})
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, openAIError{Message: "Could not update project", Type: "server_error", Code: "project_update_failed"})
		return
	}
	if !updated {
		writeJSON(writer, http.StatusNotFound, openAIError{Message: "Project not found", Type: "invalid_request_error", Code: "project_not_found"})
		return
	}
	project, err := server.projectManager.GetProject(request.Context(), input.ProjectID)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, openAIError{Message: "Could not read updated project", Type: "server_error", Code: "project_update_failed"})
		return
	}
	writeJSON(writer, http.StatusOK, projectResponseFrom(project))
}

func (server Server) blockProject(writer http.ResponseWriter, request *http.Request) {
	server.setProjectBlocked(writer, request, true)
}
func (server Server) unblockProject(writer http.ResponseWriter, request *http.Request) {
	server.setProjectBlocked(writer, request, false)
}

func (server Server) setProjectBlocked(writer http.ResponseWriter, request *http.Request, blocked bool) {
	if !server.authorizeAdmin(request) || server.projectManager == nil {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	var input struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1<<20)).Decode(&input); err != nil || input.ProjectID == "" {
		writeJSON(writer, http.StatusBadRequest, openAIError{Message: "Missing required parameter: 'project_id'", Type: "invalid_request_error", Code: "invalid_request"})
		return
	}
	updated, err := server.projectManager.SetProjectBlocked(request.Context(), input.ProjectID, blocked)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, openAIError{Message: "Could not update project", Type: "server_error", Code: "project_update_failed"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]bool{"updated": updated, "blocked": blocked})
}

func (server Server) deleteProject(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) || server.projectManager == nil {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	var input struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1<<20)).Decode(&input); err != nil || input.ProjectID == "" {
		writeJSON(writer, http.StatusBadRequest, openAIError{Message: "Missing required parameter: 'project_id'", Type: "invalid_request_error", Code: "invalid_request"})
		return
	}
	deleted, err := server.projectManager.DeleteProject(request.Context(), input.ProjectID)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, openAIError{Message: "Could not delete project", Type: "server_error", Code: "project_deletion_failed"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]bool{"deleted": deleted})
}

func projectResponseFrom(project auth.ManagedProject) projectResponse {
	return projectResponse{ProjectID: project.ProjectID, ProjectAlias: project.ProjectAlias, Description: project.Description, TeamID: project.TeamID, BudgetID: project.BudgetID, Models: nonNilStrings(project.Models), Blocked: project.Blocked}
}

type organizationRequest struct {
	OrganizationID    string   `json:"organization_id"`
	OrganizationAlias string   `json:"organization_alias"`
	BudgetID          string   `json:"budget_id"`
	Models            []string `json:"models"`
	Blocked           bool     `json:"blocked"`
}
type organizationUpdateRequest struct {
	OrganizationID    string    `json:"organization_id"`
	OrganizationAlias *string   `json:"organization_alias"`
	BudgetID          *string   `json:"budget_id"`
	Models            *[]string `json:"models"`
	Blocked           *bool     `json:"blocked"`
}
type organizationResponse struct {
	OrganizationID    string   `json:"organization_id"`
	OrganizationAlias string   `json:"organization_alias,omitempty"`
	BudgetID          string   `json:"budget_id,omitempty"`
	Models            []string `json:"models"`
	Blocked           bool     `json:"blocked"`
}

func (server Server) createOrganization(w http.ResponseWriter, r *http.Request) {
	if !server.authorizeAdmin(r) || server.organizationManager == nil {
		writeJSON(w, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	var in organizationRequest
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in) != nil {
		writeJSON(w, 400, openAIError{Message: "Request body must be valid JSON", Type: "invalid_request_error", Code: "invalid_request"})
		return
	}
	if in.OrganizationID == "" {
		id, e := litellm.UUID4()
		if e != nil {
			writeJSON(w, 500, openAIError{Message: "Could not generate organization id", Type: "server_error", Code: "organization_creation_failed"})
			return
		}
		in.OrganizationID = "org-" + id
	}
	o := auth.ManagedOrganization{OrganizationID: in.OrganizationID, OrganizationAlias: in.OrganizationAlias, BudgetID: in.BudgetID, Models: in.Models, Blocked: in.Blocked}
	if e := server.organizationManager.CreateOrganization(r.Context(), o); e != nil {
		writeJSON(w, 500, openAIError{Message: "Could not create organization", Type: "server_error", Code: "organization_creation_failed"})
		return
	}
	writeJSON(w, 200, organizationResponseFrom(o))
}
func (server Server) organizationInfo(w http.ResponseWriter, r *http.Request) {
	if !server.authorizeAdmin(r) || server.organizationManager == nil {
		writeJSON(w, 401, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	id := r.URL.Query().Get("organization_id")
	if id == "" {
		writeJSON(w, 400, openAIError{Message: "Missing required parameter: 'organization_id'", Type: "invalid_request_error", Code: "invalid_request"})
		return
	}
	o, e := server.organizationManager.GetOrganization(r.Context(), id)
	if e != nil {
		writeJSON(w, 404, openAIError{Message: "Organization not found", Type: "invalid_request_error", Code: "organization_not_found"})
		return
	}
	writeJSON(w, 200, organizationResponseFrom(o))
}
func (server Server) listOrganizations(w http.ResponseWriter, r *http.Request) {
	if !server.authorizeAdmin(r) || server.organizationManager == nil {
		writeJSON(w, 401, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	os, e := server.organizationManager.ListOrganizations(r.Context(), 100)
	if e != nil {
		writeJSON(w, 500, openAIError{Message: "Could not list organizations", Type: "server_error", Code: "organization_list_failed"})
		return
	}
	out := make([]organizationResponse, 0, len(os))
	for _, o := range os {
		out = append(out, organizationResponseFrom(o))
	}
	writeJSON(w, 200, map[string]any{"data": out})
}
func organizationResponseFrom(o auth.ManagedOrganization) organizationResponse {
	return organizationResponse{OrganizationID: o.OrganizationID, OrganizationAlias: o.OrganizationAlias, BudgetID: o.BudgetID, Models: nonNilStrings(o.Models), Blocked: o.Blocked}
}
func (server Server) updateOrganization(w http.ResponseWriter, r *http.Request) {
	if !server.authorizeAdmin(r) || server.organizationManager == nil {
		writeJSON(w, 401, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	var in organizationUpdateRequest
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in) != nil || in.OrganizationID == "" {
		writeJSON(w, 400, openAIError{Message: "Missing required parameter: 'organization_id'", Type: "invalid_request_error", Code: "invalid_request"})
		return
	}
	ok, e := server.organizationManager.UpdateOrganization(r.Context(), in.OrganizationID, auth.ManagedOrganizationUpdate{OrganizationAlias: in.OrganizationAlias, BudgetID: in.BudgetID, Models: in.Models, Blocked: in.Blocked})
	if e != nil {
		writeJSON(w, 500, openAIError{Message: "Could not update organization", Type: "server_error", Code: "organization_update_failed"})
		return
	}
	if !ok {
		writeJSON(w, 404, openAIError{Message: "Organization not found", Type: "invalid_request_error", Code: "organization_not_found"})
		return
	}
	o, e := server.organizationManager.GetOrganization(r.Context(), in.OrganizationID)
	if e != nil {
		writeJSON(w, 500, openAIError{Message: "Could not load organization", Type: "server_error", Code: "organization_load_failed"})
		return
	}
	writeJSON(w, 200, organizationResponseFrom(o))
}
func (server Server) deleteOrganization(w http.ResponseWriter, r *http.Request) {
	if !server.authorizeAdmin(r) || server.organizationManager == nil {
		writeJSON(w, 401, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	var in struct {
		OrganizationID string `json:"organization_id"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in) != nil || in.OrganizationID == "" {
		writeJSON(w, 400, openAIError{Message: "Missing required parameter: 'organization_id'", Type: "invalid_request_error", Code: "invalid_request"})
		return
	}
	ok, e := server.organizationManager.DeleteOrganization(r.Context(), in.OrganizationID)
	if e != nil {
		writeJSON(w, 500, openAIError{Message: "Could not delete organization", Type: "server_error", Code: "organization_deletion_failed"})
		return
	}
	writeJSON(w, 200, map[string]bool{"deleted": ok})
}

type modelRequestCompleter func(context.Context, config.Model, []byte) (providers.Response, error)

func (server Server) forwardModelRequest(writer http.ResponseWriter, request *http.Request, callType string, completer modelRequestCompleter) {
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
	startedAt := time.Now().UTC()
	upstream, err := server.completeModelWithFallback(request.Context(), modelName, deployment, body, completer)
	if err != nil {
		writeJSON(writer, http.StatusBadGateway, openAIError{Message: "Upstream provider request failed", Type: "api_error", Code: "upstream_error"})
		return
	}
	defer upstream.Body.Close()
	copyResponseHeaders(writer.Header(), upstream.Header)
	writer.WriteHeader(upstream.StatusCode)
	responseBody := bytes.Buffer{}
	_ = copyResponse(writer, io.TeeReader(upstream.Body, &responseBody))
	server.recordUsage(request.Context(), virtualKey.TokenHash, deployment, responseBody.Bytes(), startedAt, upstream.StatusCode, callType)
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
	server.recordUsage(request.Context(), virtualKey.TokenHash, deployment, responseBody.Bytes(), startedAt, upstream.StatusCode, "chat_completion")
}

func (server Server) completions(writer http.ResponseWriter, request *http.Request) {
	if server.textCompleter == nil {
		server.providerUnavailable(writer, "No text completion provider is configured")
		return
	}
	server.forwardModelRequest(writer, request, "text_completion", server.textCompleter.TextCompletion)
}

func (server Server) completeModelWithFallback(ctx context.Context, modelName string, deployment config.Model, body []byte, completer modelRequestCompleter) (providers.Response, error) {
	failed := []config.Model{}
	for {
		response, err := completer(ctx, deployment, body)
		if err == nil && response.StatusCode < http.StatusInternalServerError {
			return response, nil
		}
		if err == nil {
			err = fmt.Errorf("upstream returned status %d", response.StatusCode)
		}
		if response.Body != nil {
			_ = response.Body.Close()
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

func (server Server) recordUsage(ctx context.Context, keyHash string, deployment config.Model, body []byte, startedAt time.Time, statusCode int, callType string) {
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
	_ = server.usageRecorder.Insert(ctx, usage.Record{RequestID: requestID, CallType: callType, APIKeyHash: keyHash, Model: deployment.Name, Provider: provider, APIBase: deployment.APIBase, StartedAt: startedAt, CompletedAt: time.Now().UTC(), Usage: responseUsage, Status: http.StatusText(statusCode)})
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
	if server.config.ResourceModel != "" {
		return server.deploymentFor(server.config.ResourceModel)
	}
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
		if !auth.AllowsModel(virtualKey, configuredModel.Name) {
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

func (server Server) model(writer http.ResponseWriter, request *http.Request) {
	modelID := request.PathValue("modelID")
	virtualKey, authorized := server.authorize(request, modelID)
	if !authorized {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	if !auth.AllowsModel(virtualKey, modelID) {
		writeJSON(writer, http.StatusNotFound, openAIError{Message: "The model '" + modelID + "' does not exist", Type: "invalid_request_error", Code: "model_not_found"})
		return
	}
	deployment, err := server.router.Select(modelID)
	if err != nil {
		writeJSON(writer, http.StatusNotFound, openAIError{Message: "The model '" + modelID + "' does not exist", Type: "invalid_request_error", Code: "model_not_found"})
		return
	}
	writeJSON(writer, http.StatusOK, modelResponse{ID: modelID, Object: "model", Created: 0, OwnedBy: providerName(deployment.Model)})
}

func (server Server) spendLogs(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) || server.spendLogReader == nil {
		writeJSON(writer, 401, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	logs, err := server.spendLogReader.List(request.Context(), 100)
	if err != nil {
		writeJSON(writer, 500, openAIError{Message: "Could not list spend logs", Type: "server_error", Code: "spend_log_list_failed"})
		return
	}
	writeJSON(writer, 200, logs)
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

type budgetRequest struct {
	BudgetID            string     `json:"budget_id"`
	MaxBudget           *float64   `json:"max_budget"`
	SoftBudget          *float64   `json:"soft_budget"`
	MaxParallelRequests *int       `json:"max_parallel_requests"`
	TPMLimit            *int64     `json:"tpm_limit"`
	RPMLimit            *int64     `json:"rpm_limit"`
	BudgetDuration      string     `json:"budget_duration"`
	BudgetResetAt       *time.Time `json:"budget_reset_at"`
}

type budgetUpdateRequest struct {
	BudgetID            string     `json:"budget_id"`
	MaxBudget           *float64   `json:"max_budget"`
	SoftBudget          *float64   `json:"soft_budget"`
	MaxParallelRequests *int       `json:"max_parallel_requests"`
	TPMLimit            *int64     `json:"tpm_limit"`
	RPMLimit            *int64     `json:"rpm_limit"`
	BudgetDuration      *string    `json:"budget_duration"`
	BudgetResetAt       *time.Time `json:"budget_reset_at"`
}

type budgetInfoRequest struct {
	Budgets []string `json:"budgets"`
}

func validBudgetValue(value *float64) bool {
	return value == nil || (!math.IsNaN(*value) && !math.IsInf(*value, 0) && *value >= 0)
}

func (server Server) createBudget(w http.ResponseWriter, r *http.Request) {
	if !server.authorizeAdmin(r) || server.budgetManager == nil {
		writeJSON(w, 401, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	var in budgetRequest
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in) != nil || !validBudgetValue(in.MaxBudget) || !validBudgetValue(in.SoftBudget) {
		writeJSON(w, 400, openAIError{Message: "Request body contains an invalid budget", Type: "invalid_request_error", Code: "invalid_request"})
		return
	}
	if in.BudgetID == "" {
		id, e := litellm.UUID4()
		if e != nil {
			writeJSON(w, 500, openAIError{Message: "Could not generate budget ID", Type: "server_error", Code: "budget_creation_failed"})
			return
		}
		in.BudgetID = "budget-" + id
	}
	budget := auth.ManagedBudget{BudgetID: in.BudgetID, MaxBudget: in.MaxBudget, SoftBudget: in.SoftBudget, MaxParallelRequests: in.MaxParallelRequests, TPMLimit: in.TPMLimit, RPMLimit: in.RPMLimit, BudgetDuration: in.BudgetDuration, BudgetResetAt: in.BudgetResetAt}
	if e := server.budgetManager.CreateBudget(r.Context(), budget); e != nil {
		writeJSON(w, 500, openAIError{Message: "Could not create budget", Type: "server_error", Code: "budget_creation_failed"})
		return
	}
	writeJSON(w, 200, budget)
}

func (server Server) budgetInfo(w http.ResponseWriter, r *http.Request) {
	if !server.authorizeAdmin(r) || server.budgetManager == nil {
		writeJSON(w, 401, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	var in budgetInfoRequest
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in) != nil || len(in.Budgets) == 0 {
		writeJSON(w, 400, openAIError{Message: "Specify budget IDs to query", Type: "invalid_request_error", Code: "invalid_request"})
		return
	}
	budgets := make([]auth.ManagedBudget, 0, len(in.Budgets))
	for _, id := range in.Budgets {
		budget, e := server.budgetManager.GetBudget(r.Context(), id)
		if e == nil {
			budgets = append(budgets, budget)
		}
	}
	writeJSON(w, 200, budgets)
}

func (server Server) listBudgets(w http.ResponseWriter, r *http.Request) {
	if !server.authorizeAdmin(r) || server.budgetManager == nil {
		writeJSON(w, 401, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	budgets, e := server.budgetManager.ListBudgets(r.Context(), 100)
	if e != nil {
		writeJSON(w, 500, openAIError{Message: "Could not list budgets", Type: "server_error", Code: "budget_list_failed"})
		return
	}
	writeJSON(w, 200, budgets)
}

func (server Server) updateBudget(w http.ResponseWriter, r *http.Request) {
	if !server.authorizeAdmin(r) || server.budgetManager == nil {
		writeJSON(w, 401, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	var in budgetUpdateRequest
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in) != nil || in.BudgetID == "" || !validBudgetValue(in.MaxBudget) || !validBudgetValue(in.SoftBudget) {
		writeJSON(w, 400, openAIError{Message: "Request body contains an invalid budget", Type: "invalid_request_error", Code: "invalid_request"})
		return
	}
	ok, e := server.budgetManager.UpdateBudget(r.Context(), in.BudgetID, auth.ManagedBudgetUpdate{MaxBudget: in.MaxBudget, SoftBudget: in.SoftBudget, MaxParallelRequests: in.MaxParallelRequests, TPMLimit: in.TPMLimit, RPMLimit: in.RPMLimit, BudgetDuration: in.BudgetDuration, BudgetResetAt: in.BudgetResetAt})
	if e != nil {
		writeJSON(w, 500, openAIError{Message: "Could not update budget", Type: "server_error", Code: "budget_update_failed"})
		return
	}
	if !ok {
		writeJSON(w, 404, openAIError{Message: "Budget not found", Type: "invalid_request_error", Code: "budget_not_found"})
		return
	}
	budget, e := server.budgetManager.GetBudget(r.Context(), in.BudgetID)
	if e != nil {
		writeJSON(w, 500, openAIError{Message: "Could not load budget", Type: "server_error", Code: "budget_load_failed"})
		return
	}
	writeJSON(w, 200, budget)
}

func (server Server) deleteBudget(w http.ResponseWriter, r *http.Request) {
	if !server.authorizeAdmin(r) || server.budgetManager == nil {
		writeJSON(w, 401, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	var in struct {
		ID string `json:"id"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in) != nil || in.ID == "" {
		writeJSON(w, 400, openAIError{Message: "Missing required parameter: 'id'", Type: "invalid_request_error", Code: "invalid_request"})
		return
	}
	deleted, e := server.budgetManager.DeleteBudget(r.Context(), in.ID)
	if e != nil {
		writeJSON(w, 500, openAIError{Message: "Could not delete budget", Type: "server_error", Code: "budget_deletion_failed"})
		return
	}
	writeJSON(w, 200, map[string]bool{"deleted": deleted})
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
