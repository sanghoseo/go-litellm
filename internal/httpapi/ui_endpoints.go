package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BerriAI/litellm/go-proxy/internal/usage"
)

//go:embed logo.jpg
var defaultLogo []byte

const (
	uiAdminUserID = "default_user_id"
	uiAdminRole   = "proxy_admin"
	uiSessionTTL  = 24 * time.Hour
)

type uiLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type uiLoginResponse struct {
	RedirectURL string `json:"redirect_url"`
	Token       string `json:"token"`
}

// uiLogin implements POST /v2/login, the UI login helper. The dashboard
// exchange flow stores the returned JWT in a document.cookie-read "token"
// cookie, so the cookie must not be HttpOnly. The JWT "key" claim carries a
// credential the UI then sends as the API bearer token; for the built-in UI
// admin that credential is the proxy master key.
func (server Server) uiLogin(writer http.ResponseWriter, request *http.Request) {
	var in uiLoginRequest
	if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1<<20)).Decode(&in); err != nil {
		writeJSON(writer, http.StatusBadRequest, openAIError{Message: "Request body contains invalid JSON", Type: "invalid_request_error", Code: "invalid_json"})
		return
	}
	uiUsername := os.Getenv("UI_USERNAME")
	if uiUsername == "" {
		uiUsername = "admin"
	}
	uiPassword := os.Getenv("UI_PASSWORD")
	if uiPassword == "" {
		uiPassword = server.config.MasterKey
	}
	if uiPassword == "" {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "set Proxy master key to use UI. Set LITELLM_MASTER_KEY in .env or general_settings.master_key in config.yaml.", Type: "auth_error", Code: "invalid_credentials"})
		return
	}
	usernameOK := subtle.ConstantTimeCompare([]byte(in.Username), []byte(uiUsername)) == 1
	passwordOK := subtle.ConstantTimeCompare([]byte(in.Password), []byte(uiPassword)) == 1
	if !usernameOK || !passwordOK {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Invalid credentials used to access UI.", Type: "auth_error", Code: "invalid_credentials"})
		return
	}
	now := time.Now()
	claims := map[string]any{
		"user_id":          uiAdminUserID,
		"key":              server.config.MasterKey,
		"user_email":       nil,
		"user_role":        uiAdminRole,
		"login_method":     "username_password",
		"premium_user":     false,
		"auth_header_name": "Authorization",
		"disabled_non_admin_personal_key_creation": false,
		"server_root_path":                         "",
		"exp":                                      now.Add(uiSessionTTL).Unix(),
	}
	token, err := encodeHS256(claims, server.config.MasterKey)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, openAIError{Message: "Could not encode UI session token", Type: "server_error", Code: "ui_session_failed"})
		return
	}
	scheme := "http"
	if forwarded := request.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		scheme = forwarded
	}
	redirectURL := fmt.Sprintf("%s://%s/ui?login=success", scheme, request.Host)
	http.SetCookie(writer, &http.Cookie{Name: "token", Value: token, Path: "/"})
	writeJSON(writer, http.StatusOK, uiLoginResponse{RedirectURL: redirectURL, Token: token})
}

func encodeHS256(claims map[string]any, secret string) (string, error) {
	header, err := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encoding := base64.RawURLEncoding
	signingInput := encoding.EncodeToString(header) + "." + encoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	return signingInput + "." + encoding.EncodeToString(mac.Sum(nil)), nil
}

type v2UserInfoResponse struct {
	UserID         string          `json:"user_id"`
	UserEmail      *string         `json:"user_email"`
	UserAlias      *string         `json:"user_alias"`
	UserRole       string          `json:"user_role"`
	Spend          float64         `json:"spend"`
	MaxBudget      *float64        `json:"max_budget"`
	Models         []string        `json:"models"`
	BudgetDuration *string         `json:"budget_duration"`
	BudgetResetAt  *time.Time      `json:"budget_reset_at"`
	Metadata       json.RawMessage `json:"metadata"`
	CreatedAt      *time.Time      `json:"created_at"`
	UpdatedAt      *time.Time      `json:"updated_at"`
	SSOUserID      *string         `json:"sso_user_id"`
	Teams          []string        `json:"teams"`
}

// v2UserInfo implements GET /v2/user/info, the lightweight dashboard user
// lookup. Without a user_id it returns the caller's own identity; the built-in
// UI admin maps to the proxy admin identity.
func (server Server) v2UserInfo(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	userID := request.URL.Query().Get("user_id")
	// The built-in UI admin always resolves to the proxy admin identity: the
	// Python proxy guarantees a "default_user_id" row on login, the Go proxy
	// has no such row and answering 404 here crashes the dashboard.
	if userID == "" || userID == uiAdminUserID {
		writeJSON(writer, http.StatusOK, v2UserInfoResponse{
			UserID:   uiAdminUserID,
			UserRole: uiAdminRole,
			Models:   []string{},
			Teams:    []string{},
			Metadata: nil,
		})
		return
	}
	if server.userManager == nil {
		writeJSON(writer, http.StatusNotFound, openAIError{Message: "User not found", Type: "invalid_request_error", Code: "user_not_found"})
		return
	}
	user, err := server.userManager.GetUser(request.Context(), userID)
	if err != nil {
		writeJSON(writer, http.StatusNotFound, openAIError{Message: "User not found", Type: "invalid_request_error", Code: "user_not_found"})
		return
	}
	teams := []string{}
	if user.TeamID != "" {
		teams = append(teams, user.TeamID)
	}
	response := v2UserInfoResponse{
		UserID:   user.UserID,
		UserRole: "internal_user",
		Models:   nonNilStrings(user.Models),
		Teams:    teams,
	}
	if user.UserEmail != "" {
		response.UserEmail = &user.UserEmail
	}
	if user.UserAlias != "" {
		response.UserAlias = &user.UserAlias
	}
	writeJSON(writer, http.StatusOK, response)
}

type teamListItem struct {
	TeamID           string    `json:"team_id"`
	TeamAlias        string    `json:"team_alias"`
	Models           []string  `json:"models"`
	MaxBudget        *float64  `json:"max_budget"`
	BudgetDuration   *string   `json:"budget_duration"`
	TPMLimit         *int64    `json:"tpm_limit"`
	RPMLimit         *int64    `json:"rpm_limit"`
	OrganizationID   string    `json:"organization_id"`
	CreatedAt        time.Time `json:"created_at"`
	Keys             []any     `json:"keys"`
	MembersWithRoles []any     `json:"members_with_roles"`
	Spend            float64   `json:"spend"`
}

type teamListPageResponse struct {
	Page       int            `json:"page"`
	PageSize   int            `json:"page_size"`
	Total      int            `json:"total"`
	TotalPages int            `json:"total_pages"`
	Teams      []teamListItem `json:"teams"`
}

// v2TeamList implements GET /v2/team/list, the paginated dashboard team list.
func (server Server) v2TeamList(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	page := 1
	pageSize := 10
	if raw := request.URL.Query().Get("page"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			page = parsed
		}
	}
	if raw := request.URL.Query().Get("page_size"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 100 {
			pageSize = parsed
		}
	}
	teams := []teamListItem{}
	if server.teamManager != nil {
		managed, err := server.teamManager.ListTeams(request.Context(), pageSize)
		if err != nil {
			writeJSON(writer, http.StatusInternalServerError, openAIError{Message: "Could not list teams", Type: "server_error", Code: "team_list_failed"})
			return
		}
		for _, team := range managed {
			item := teamListItem{
				TeamID:           team.TeamID,
				TeamAlias:        team.TeamAlias,
				Models:           nonNilStrings(team.Models),
				OrganizationID:   "",
				CreatedAt:        time.Time{},
				Keys:             []any{},
				MembersWithRoles: []any{},
			}
			teams = append(teams, item)
		}
	}
	totalPages := 0
	if len(teams) > 0 {
		totalPages = (len(teams) + pageSize - 1) / pageSize
	}
	writeJSON(writer, http.StatusOK, teamListPageResponse{Page: page, PageSize: pageSize, Total: len(teams), TotalPages: totalPages, Teams: teams})
}

type v2KeyInfoRequest struct {
	Keys []string `json:"keys"`
}

// v2KeyInfo implements POST /v2/key/info. The dashboard calls it to refresh
// the active key row; unknown keys (including the master key) yield an empty
// list instead of an error so the UI does not treat the session as invalid.
func (server Server) v2KeyInfo(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	var in v2KeyInfoRequest
	if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1<<20)).Decode(&in); err != nil {
		writeJSON(writer, http.StatusBadRequest, openAIError{Message: "Request body contains invalid JSON", Type: "invalid_request_error", Code: "invalid_json"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"keys": []any{}})
}

// availableRoles implements GET /user/available_roles with the role set the
// dashboard exposes for key and user creation.
func (server Server) availableRoles(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]map[string]string{
		"proxy_admin":          {"title": "Admin", "description": "Full access to the proxy"},
		"internal_user":        {"title": "Internal User", "description": "API access with assigned model groups"},
		"internal_user_viewer": {"title": "Internal User (view only)", "description": "Read-only API access"},
	})
}

func (server Server) uiSettings(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	server.writeUISettings(writer)
}

// writeUISettings serves the empty UI settings bag; the Go proxy persists no
// dashboard settings yet.
func (server Server) writeUISettings(writer http.ResponseWriter) {
	writeJSON(writer, http.StatusOK, map[string]any{"values": map[string]any{}})
}

// uiSettingsPublic implements GET /get/ui_settings. The dashboard fetches it
// without an authorization header (useUISettings), mirroring the Python
// endpoint which is unauthenticated.
func (server Server) uiSettingsPublic(writer http.ResponseWriter, request *http.Request) {
	server.writeUISettings(writer)
}

// userBanner implements GET /get/user_banner with the default disabled banner.
func (server Server) userBanner(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"enabled": false, "message": "", "severity": "info", "revision": ""})
}

// uiThemeSettings implements GET /get/ui_theme_settings. The theme loader
// fetches it without an authorization header, so it stays public.
func (server Server) uiThemeSettings(writer http.ResponseWriter, request *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{"values": map[string]any{}})
}

func (server Server) apiPlugins(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	writeJSON(writer, http.StatusOK, []any{})
}

// readinessDetails implements GET /health/readiness/details, the auth-gated
// readiness payload the navbar renders.
func (server Server) readinessDetails(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	status := "healthy"
	for _, check := range server.readinessChecks {
		ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
		err := check.Ping(ctx)
		cancel()
		if err != nil {
			status = "unhealthy"
			break
		}
	}
	writeJSON(writer, http.StatusOK, map[string]any{"status": status, "litellm_version": "go-proxy"})
}

// license implements GET /health/license; the Go proxy ships without a
// commercial license.
func (server Server) license(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"has_license":      false,
		"license_type":     nil,
		"expiration_date":  nil,
		"allowed_features": []any{},
		"limits":           map[string]any{"max_users": nil, "max_teams": nil},
	})
}

// blogPosts implements GET /public/litellm_blog_posts. It is public because
// the blog query fires before authentication completes.
func (server Server) blogPosts(writer http.ResponseWriter, request *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{"posts": []any{}})
}

// tagList implements GET /tag/list; the Go proxy does not manage tags yet.
func (server Server) tagList(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{})
}

// agents implements GET /v1/agents; the Go proxy does not host A2A agents.
func (server Server) agents(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	writeJSON(writer, http.StatusOK, []any{})
}

// guardrailsList implements GET /v2/guardrails/list and GET /guardrails/list;
// the Go proxy does not register guardrails.
func (server Server) guardrailsList(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"guardrails": []any{}})
}

// getImage implements GET /get_image, serving the navbar logo. A local file
// from UI_LOGO_PATH (or an http(s) URL, which the browser follows) overrides
// the bundled default, mirroring the Python proxy behaviour.
func (server Server) getImage(writer http.ResponseWriter, request *http.Request) {
	if logoPath := os.Getenv("UI_LOGO_PATH"); logoPath != "" {
		if strings.HasPrefix(logoPath, "http://") || strings.HasPrefix(logoPath, "https://") {
			http.Redirect(writer, request, logoPath, http.StatusFound)
			return
		}
		if data, err := os.ReadFile(logoPath); err == nil && len(data) > 0 {
			writer.Header().Set("Content-Type", "image/jpeg")
			_, _ = writer.Write(data)
			return
		}
	}
	writer.Header().Set("Content-Type", "image/jpeg")
	_, _ = writer.Write(defaultLogo)
}

// customerList implements GET /customer/list; the Go proxy does not track
// end users (customers), so the dashboard dropdown stays empty.
func (server Server) customerList(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	writeJSON(writer, http.StatusOK, []any{})
}

// gatewayDailyActivity implements GET /gateway/daily/activity with zeroed
// counters; the Go proxy does not record per-route gateway request stats yet.
func (server Server) gatewayDailyActivity(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"total_successful_requests": 0,
		"total_failed_requests":     0,
		"by_date":                   []any{},
		"by_route":                  []any{},
	})
}

// userDailyActivityAggregated implements GET /user/daily/activity/aggregated
// with empty results; the usage page renders its empty state from this shape.
func (server Server) userDailyActivityAggregated(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"results": []any{},
		"metadata": map[string]any{
			"total_spend":                        0.0,
			"total_flat_cost":                    0.0,
			"total_prompt_tokens":                0,
			"total_completion_tokens":            0,
			"total_tokens":                       0,
			"total_api_requests":                 0,
			"total_successful_requests":          0,
			"total_failed_requests":              0,
			"total_cache_read_input_tokens":      0,
			"total_cache_creation_input_tokens":  0,
			"total_compression_saved_tokens":     0,
			"total_compression_savings_spend":    0.0,
			"total_prompt_caching_savings_spend": 0.0,
			"total_autorouter_savings_spend":     0.0,
			"page":                               1,
			"total_pages":                        1,
			"has_more":                           false,
		},
	})
}

func parseUILogTime(value string, endOfDay bool) (time.Time, bool) {
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"} {
		parsed, err := time.ParseInLocation(layout, value, time.UTC)
		if err != nil {
			continue
		}
		if layout == "2006-01-02" && endOfDay {
			return parsed.Add(24*time.Hour - time.Nanosecond), true
		}
		return parsed, true
	}
	return time.Time{}, false
}

// uiSpendLogs implements GET /spend/logs/ui with the paginated shape the
// dashboard Request Logs table consumes: {data, total, page, page_size,
// total_pages}. Records come from the spend log store; dimensions the Go
// proxy does not track yet (user, team, end user, cost) stay empty.
func (server Server) uiSpendLogs(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) || server.spendLogReader == nil {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	query := request.URL.Query()
	page := 1
	pageSize := 50
	if raw := query.Get("page"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			writeJSON(writer, http.StatusBadRequest, openAIError{Message: "Invalid page value", Type: "invalid_request_error", Code: "invalid_query_param"})
			return
		}
		page = parsed
	}
	if raw := query.Get("page_size"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 1000 {
			writeJSON(writer, http.StatusBadRequest, openAIError{Message: "Invalid page_size value", Type: "invalid_request_error", Code: "invalid_query_param"})
			return
		}
		pageSize = parsed
	}
	var startTime, endTime time.Time
	if raw := query.Get("start_date"); raw != "" {
		parsed, ok := parseUILogTime(raw, false)
		if !ok {
			writeJSON(writer, http.StatusBadRequest, openAIError{Message: "Invalid start_date value", Type: "invalid_request_error", Code: "invalid_query_param"})
			return
		}
		startTime = parsed
	}
	if raw := query.Get("end_date"); raw != "" {
		parsed, ok := parseUILogTime(raw, true)
		if !ok {
			writeJSON(writer, http.StatusBadRequest, openAIError{Message: "Invalid end_date value", Type: "invalid_request_error", Code: "invalid_query_param"})
			return
		}
		endTime = parsed
	}
	modelFilter := query.Get("model")
	apiKeyFilter := query.Get("api_key")
	requestIDFilter := query.Get("request_id")
	statusFilter := query.Get("status_filter")
	if query.Get("user_id") != "" || query.Get("team_id") != "" || query.Get("end_user") != "" || query.Get("session_id") != "" {
		// The Go proxy does not record these dimensions yet, so any request
		// filtering on them cannot match a stored log.
		writeJSON(writer, http.StatusOK, uiSpendLogsResponse(nil, 0, page, pageSize))
		return
	}
	logs, err := server.spendLogReader.List(request.Context(), 1000)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, openAIError{Message: "Could not list spend logs", Type: "server_error", Code: "spend_log_list_failed"})
		return
	}
	matches := make([]usage.Log, 0, len(logs))
	for _, log := range logs {
		if !startTime.IsZero() && log.StartedAt.Before(startTime) {
			continue
		}
		if !endTime.IsZero() && log.StartedAt.After(endTime) {
			continue
		}
		if modelFilter != "" && log.Model != modelFilter {
			continue
		}
		if apiKeyFilter != "" && log.APIKeyHash != apiKeyFilter {
			continue
		}
		if requestIDFilter != "" && log.RequestID != requestIDFilter {
			continue
		}
		if statusFilter == "success" && log.Status != "success" {
			continue
		}
		if (statusFilter == "error" || statusFilter == "failed") && log.Status == "success" {
			continue
		}
		matches = append(matches, log)
	}
	sort.Slice(matches, func(i, j int) bool {
		less := false
		switch query.Get("sort_by") {
		case "total_tokens":
			less = matches[i].TotalTokens < matches[j].TotalTokens
		case "endTime":
			less = matches[i].CompletedAt.Before(matches[j].CompletedAt)
		case "model":
			less = matches[i].Model < matches[j].Model
		default:
			less = matches[i].StartedAt.Before(matches[j].StartedAt)
		}
		if query.Get("sort_order") == "asc" {
			return less
		}
		return !less
	})
	total := len(matches)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	entries := make([]map[string]any, 0, end-start)
	for _, log := range matches[start:end] {
		entries = append(entries, uiSpendLogEntry(log))
	}
	writeJSON(writer, http.StatusOK, uiSpendLogsResponse(entries, total, page, pageSize))
}

func uiSpendLogsResponse(entries []map[string]any, total, page, pageSize int) map[string]any {
	totalPages := 0
	if total > 0 && pageSize > 0 {
		totalPages = (total + pageSize - 1) / pageSize
	}
	return map[string]any{
		"data":            entries,
		"total":           total,
		"page":            page,
		"page_size":       pageSize,
		"total_pages":     totalPages,
		"total_is_capped": false,
	}
}

func uiSpendLogEntry(log usage.Log) map[string]any {
	durationMS := 0
	if !log.CompletedAt.IsZero() && !log.StartedAt.IsZero() {
		durationMS = int(log.CompletedAt.Sub(log.StartedAt).Milliseconds())
	}
	return map[string]any{
		"request_id":          log.RequestID,
		"api_key":             log.APIKeyHash,
		"team_id":             "",
		"model":               log.Model,
		"model_id":            log.Model,
		"model_group":         "",
		"api_base":            "",
		"call_type":           log.CallType,
		"spend":               0.0,
		"total_tokens":        log.TotalTokens,
		"prompt_tokens":       0,
		"completion_tokens":   log.TotalTokens,
		"startTime":           log.StartedAt.UTC().Format(time.RFC3339),
		"endTime":             log.CompletedAt.UTC().Format(time.RFC3339),
		"user":                "",
		"end_user":            "",
		"custom_llm_provider": log.Provider,
		"metadata":            map[string]any{},
		"cache_hit":           "",
		"request_tags":        map[string]any{},
		"messages":            "",
		"response":            "",
		"session_id":          "",
		"status":              log.Status,
		"request_duration_ms": durationMS,
	}
}

// modelInfoV2 implements GET /v2/model/info with the paginated shape the
// dashboard models list consumes: {data, total_count, current_page,
// total_pages, size}.
func (server Server) modelInfoV2(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	query := request.URL.Query()
	page := 1
	size := 50
	if raw := query.Get("page"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			writeJSON(writer, http.StatusBadRequest, openAIError{Message: "Invalid page value", Type: "invalid_request_error", Code: "invalid_query_param"})
			return
		}
		page = parsed
	}
	if raw := query.Get("size"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 1000 {
			writeJSON(writer, http.StatusBadRequest, openAIError{Message: "Invalid size value", Type: "invalid_request_error", Code: "invalid_query_param"})
			return
		}
		size = parsed
	}
	search := strings.ToLower(strings.TrimSpace(query.Get("search")))
	entries := make([]map[string]any, 0, len(server.config.Models))
	for _, model := range server.config.Models {
		if search != "" && !strings.Contains(strings.ToLower(model.Name), search) && !strings.Contains(strings.ToLower(model.Model), search) {
			continue
		}
		entries = append(entries, map[string]any{
			"model_name":         model.Name,
			"litellm_model_name": model.Model,
			"mode":               "chat",
			"max_tokens":         0,
			"model_info": map[string]any{
				"base_model":            model.Model,
				"input_cost_per_token":  0.0,
				"output_cost_per_token": 0.0,
			},
		})
	}
	totalCount := len(entries)
	totalPages := 0
	if totalCount > 0 {
		totalPages = (totalCount + size - 1) / size
	}
	start := (page - 1) * size
	if start > totalCount {
		start = totalCount
	}
	end := start + size
	if end > totalCount {
		end = totalCount
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"data":         entries[start:end],
		"total_count":  totalCount,
		"current_page": page,
		"total_pages":  totalPages,
		"size":         size,
	})
}

// availableUsers implements GET /user/available_users; without a commercial
// license the limits are null and only the used counts are reported.
func (server Server) availableUsers(writer http.ResponseWriter, request *http.Request) {
	if !server.authorizeAdmin(request) {
		writeJSON(writer, http.StatusUnauthorized, openAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"})
		return
	}
	usersUsed := 0
	teamsUsed := 0
	if server.userManager != nil {
		if users, err := server.userManager.ListUsers(request.Context(), 1000); err == nil {
			usersUsed = len(users)
		}
	}
	if server.teamManager != nil {
		if teams, err := server.teamManager.ListTeams(request.Context(), 1000); err == nil {
			teamsUsed = len(teams)
		}
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"total_users":           nil,
		"total_users_used":      usersUsed,
		"total_users_remaining": nil,
		"total_teams":           nil,
		"total_teams_used":      teamsUsed,
		"total_teams_remaining": nil,
	})
}
