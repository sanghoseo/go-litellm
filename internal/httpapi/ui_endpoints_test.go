package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/BerriAI/litellm/go-proxy/internal/auth"
	"github.com/BerriAI/litellm/go-proxy/internal/config"
	"github.com/BerriAI/litellm/go-proxy/internal/usage"
)

func decodeJWTClaims(t *testing.T, token string) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token %q is not a 3-part JWT", token)
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode JWT payload: %v", err)
	}
	claims := map[string]any{}
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatalf("unmarshal JWT payload: %v", err)
	}
	return claims
}

func TestV2LoginSuccessIssuesUISession(t *testing.T) {
	server := NewServer(config.Config{MasterKey: "master-key"})
	request := httptest.NewRequest(http.MethodPost, "/v2/login", strings.NewReader(`{"username":"admin","password":"master-key"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	var body struct {
		RedirectURL string `json:"redirect_url"`
		Token       string `json:"token"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Token == "" {
		t.Fatalf("unexpected body %s", response.Body.String())
	}
	if body.RedirectURL != "http://example.com/ui?login=success" {
		t.Fatalf("redirect_url = %q", body.RedirectURL)
	}
	cookie := response.Result().Header.Get("Set-Cookie")
	if !strings.Contains(cookie, "token="+body.Token) {
		t.Fatalf("Set-Cookie %q does not carry the session token", cookie)
	}
	claims := decodeJWTClaims(t, body.Token)
	if claims["key"] != "master-key" {
		t.Fatalf("claims.key = %v", claims["key"])
	}
	if claims["user_role"] != "proxy_admin" {
		t.Fatalf("claims.user_role = %v", claims["user_role"])
	}
	if claims["user_id"] != "default_user_id" {
		t.Fatalf("claims.user_id = %v", claims["user_id"])
	}
	if claims["login_method"] != "username_password" {
		t.Fatalf("claims.login_method = %v", claims["login_method"])
	}
	exp, ok := claims["exp"].(float64)
	if !ok || exp < float64(time.Now().Unix()) {
		t.Fatalf("claims.exp = %v, want future timestamp", claims["exp"])
	}
}

func TestV2LoginRejectsInvalidCredentials(t *testing.T) {
	server := NewServer(config.Config{MasterKey: "master-key"})
	request := httptest.NewRequest(http.MethodPost, "/v2/login", strings.NewReader(`{"username":"admin","password":"wrong"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized || !hasErrorEnvelope(response.Body.String()) {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
}

func TestV2LoginSupportsCustomUICredentials(t *testing.T) {
	t.Setenv("UI_USERNAME", "root")
	t.Setenv("UI_PASSWORD", "ui-password")
	server := NewServer(config.Config{MasterKey: "master-key"})

	custom := httptest.NewRequest(http.MethodPost, "/v2/login", strings.NewReader(`{"username":"root","password":"ui-password"}`))
	custom.Header.Set("Content-Type", "application/json")
	customResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(customResponse, custom)
	if customResponse.Code != http.StatusOK {
		t.Fatalf("custom credentials status = %d body = %s", customResponse.Code, customResponse.Body.String())
	}

	legacy := httptest.NewRequest(http.MethodPost, "/v2/login", strings.NewReader(`{"username":"admin","password":"master-key"}`))
	legacy.Header.Set("Content-Type", "application/json")
	legacyResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(legacyResponse, legacy)
	if legacyResponse.Code != http.StatusUnauthorized {
		t.Fatalf("master credentials after override status = %d, want 401", legacyResponse.Code)
	}
}

func TestV2LoginFailsWithoutMasterKeyOrPassword(t *testing.T) {
	t.Setenv("UI_PASSWORD", "")
	server := NewServer(config.Config{})
	request := httptest.NewRequest(http.MethodPost, "/v2/login", strings.NewReader(`{"username":"admin","password":"whatever"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized || !hasErrorEnvelope(response.Body.String()) {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
}

func TestV2UserInfoReturnsCallerIdentity(t *testing.T) {
	server := NewServer(config.Config{MasterKey: "master-key"})

	unauthorized := httptest.NewRequest(http.MethodGet, "/v2/user/info", nil)
	unauthorizedResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", unauthorizedResponse.Code)
	}

	request := httptest.NewRequest(http.MethodGet, "/v2/user/info", nil)
	request.Header.Set("Authorization", "Bearer master-key")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	var body struct {
		UserID   string   `json:"user_id"`
		UserRole string   `json:"user_role"`
		Spend    float64  `json:"spend"`
		Models   []string `json:"models"`
		Teams    []string `json:"teams"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v body = %s", err, response.Body.String())
	}
	if body.UserID != "default_user_id" || body.UserRole != "proxy_admin" {
		t.Fatalf("identity = %+v", body)
	}
	if body.Models == nil || body.Teams == nil {
		t.Fatalf("models/teams must be non-nil arrays: %+v", body)
	}
}

func TestV2TeamListReturnsPageShape(t *testing.T) {
	emptyServer := NewServer(config.Config{MasterKey: "master-key"})
	emptyRequest := httptest.NewRequest(http.MethodGet, "/v2/team/list", nil)
	emptyRequest.Header.Set("Authorization", "Bearer master-key")
	emptyResponse := httptest.NewRecorder()
	emptyServer.Handler().ServeHTTP(emptyResponse, emptyRequest)

	if emptyResponse.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", emptyResponse.Code, emptyResponse.Body.String())
	}
	var emptyBody struct {
		Page     int `json:"page"`
		PageSize int `json:"page_size"`
		Total    int `json:"total"`
		Teams    any `json:"teams"`
	}
	if err := json.Unmarshal(emptyResponse.Body.Bytes(), &emptyBody); err != nil || emptyBody.Page != 1 || emptyBody.Total != 0 {
		t.Fatalf("empty body = %s", emptyResponse.Body.String())
	}
	teams, ok := emptyBody.Teams.([]any)
	if !ok || len(teams) != 0 {
		t.Fatalf("teams = %#v, want empty array", emptyBody.Teams)
	}

	manager := &memoryTeamManager{teams: map[string]auth.ManagedTeam{
		"team-test": {TeamID: "team-test", TeamAlias: "Engineering", Models: []string{"gpt-test"}, Members: []string{"user-test"}},
	}}
	seededServer := NewServer(config.Config{MasterKey: "master-key"}).WithTeamManager(manager)
	seededRequest := httptest.NewRequest(http.MethodGet, "/v2/team/list", nil)
	seededRequest.Header.Set("Authorization", "Bearer master-key")
	seededResponse := httptest.NewRecorder()
	seededServer.Handler().ServeHTTP(seededResponse, seededRequest)

	var seededBody struct {
		Total int `json:"total"`
		Teams []struct {
			TeamID string `json:"team_id"`
		} `json:"teams"`
	}
	if err := json.Unmarshal(seededResponse.Body.Bytes(), &seededBody); err != nil || seededBody.Total != 1 || len(seededBody.Teams) != 1 || seededBody.Teams[0].TeamID != "team-test" {
		t.Fatalf("seeded body = %s", seededResponse.Body.String())
	}
}

func TestV2KeyInfoReturnsKeyList(t *testing.T) {
	server := NewServer(config.Config{MasterKey: "master-key"})
	request := httptest.NewRequest(http.MethodPost, "/v2/key/info", strings.NewReader(`{"keys":["sk-unknown"]}`))
	request.Header.Set("Authorization", "Bearer master-key")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	var body struct {
		Keys any `json:"keys"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v body = %s", err, response.Body.String())
	}
	keys, ok := body.Keys.([]any)
	if !ok || len(keys) != 0 {
		t.Fatalf("keys = %#v, want empty array", body.Keys)
	}

	unauthorized := httptest.NewRequest(http.MethodPost, "/v2/key/info", strings.NewReader(`{"keys":["sk-unknown"]}`))
	unauthorizedResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", unauthorizedResponse.Code)
	}
}

func TestUserAvailableRolesIncludesProxyAdmin(t *testing.T) {
	server := NewServer(config.Config{MasterKey: "master-key"})
	request := httptest.NewRequest(http.MethodGet, "/user/available_roles", nil)
	request.Header.Set("Authorization", "Bearer master-key")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	body := map[string]map[string]string{}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v body = %s", err, response.Body.String())
	}
	if _, ok := body["proxy_admin"]; !ok {
		t.Fatalf("roles = %s, want proxy_admin entry", response.Body.String())
	}
}

func assertUISettingsValues(t *testing.T, response *httptest.ResponseRecorder, path string) {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("%s status = %d body = %s", path, response.Code, response.Body.String())
	}
	var body struct {
		Values any `json:"values"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Values == nil {
		t.Fatalf("%s body = %s, want values object", path, response.Body.String())
	}
}

func TestGetUISettingsIsPublic(t *testing.T) {
	server := NewServer(config.Config{MasterKey: "master-key"})
	request := httptest.NewRequest(http.MethodGet, "/get/ui_settings", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	assertUISettingsValues(t, response, "/get/ui_settings")
}

func TestSsoUISettingsRequiresAuth(t *testing.T) {
	server := NewServer(config.Config{MasterKey: "master-key"})

	request := httptest.NewRequest(http.MethodGet, "/sso/get/ui_settings", nil)
	request.Header.Set("Authorization", "Bearer master-key")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	assertUISettingsValues(t, response, "/sso/get/ui_settings")

	unauthorized := httptest.NewRequest(http.MethodGet, "/sso/get/ui_settings", nil)
	unauthorizedResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", unauthorizedResponse.Code)
	}
}

func TestUserBannerDisabledByDefault(t *testing.T) {
	server := NewServer(config.Config{MasterKey: "master-key"})
	request := httptest.NewRequest(http.MethodGet, "/get/user_banner", nil)
	request.Header.Set("Authorization", "Bearer master-key")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	var body struct {
		Enabled  bool   `json:"enabled"`
		Message  string `json:"message"`
		Severity string `json:"severity"`
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Enabled || body.Severity != "info" {
		t.Fatalf("body = %s", response.Body.String())
	}
}

func TestUIThemeSettingsIsPublic(t *testing.T) {
	server := NewServer(config.Config{MasterKey: "master-key"})
	request := httptest.NewRequest(http.MethodGet, "/get/ui_theme_settings", nil)
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("public theme settings status = %d, want 200", response.Code)
	}
	var body struct {
		Values any `json:"values"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Values == nil {
		t.Fatalf("body = %s, want values object", response.Body.String())
	}
}

func TestApiPluginsReturnsEmptyArray(t *testing.T) {
	server := NewServer(config.Config{MasterKey: "master-key"})
	request := httptest.NewRequest(http.MethodGet, "/api/plugins", nil)
	request.Header.Set("Authorization", "Bearer master-key")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != "[]" {
		t.Fatalf("status = %d body = %q, want []", response.Code, response.Body.String())
	}
}

func TestHealthReadinessDetailsRequiresAuth(t *testing.T) {
	server := NewServer(config.Config{MasterKey: "master-key"})

	unauthorized := httptest.NewRequest(http.MethodGet, "/health/readiness/details", nil)
	unauthorizedResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", unauthorizedResponse.Code)
	}

	request := httptest.NewRequest(http.MethodGet, "/health/readiness/details", nil)
	request.Header.Set("Authorization", "Bearer master-key")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Status == "" {
		t.Fatalf("body = %s, want non-empty status", response.Body.String())
	}
}

func TestHealthLicenseReportsNoLicense(t *testing.T) {
	server := NewServer(config.Config{MasterKey: "master-key"})
	request := httptest.NewRequest(http.MethodGet, "/health/license", nil)
	request.Header.Set("Authorization", "Bearer master-key")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	var body struct {
		HasLicense     bool `json:"has_license"`
		AllowedFeature any  `json:"allowed_features"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.HasLicense {
		t.Fatalf("body = %s, want has_license=false", response.Body.String())
	}
	features, ok := body.AllowedFeature.([]any)
	if !ok || len(features) != 0 {
		t.Fatalf("allowed_features = %#v, want empty array", body.AllowedFeature)
	}
}

func TestBlogPostsPublicReturnsEmptyPosts(t *testing.T) {
	server := NewServer(config.Config{MasterKey: "master-key"})
	request := httptest.NewRequest(http.MethodGet, "/public/litellm_blog_posts", nil)
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("public blog posts status = %d, want 200", response.Code)
	}
	var body struct {
		Posts []any `json:"posts"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Posts == nil || len(body.Posts) != 0 {
		t.Fatalf("body = %s, want empty posts array", response.Body.String())
	}
}

func TestModelsAliasRouteMatchesV1Models(t *testing.T) {
	server := NewServer(config.Config{MasterKey: "master-key", Models: []config.Model{{Name: "gpt-test", Model: "openai/gpt-test"}}})
	request := httptest.NewRequest(http.MethodGet, "/models", nil)
	request.Header.Set("Authorization", "Bearer master-key")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	var body struct {
		Object string `json:"object"`
		Data   []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Object != "list" || len(body.Data) != 1 || body.Data[0].ID != "gpt-test" {
		t.Fatalf("body = %s", response.Body.String())
	}
}

func TestTagListReturnsEmptyObject(t *testing.T) {
	server := NewServer(config.Config{MasterKey: "master-key"})
	request := httptest.NewRequest(http.MethodGet, "/tag/list", nil)
	request.Header.Set("Authorization", "Bearer master-key")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != "{}" {
		t.Fatalf("status = %d body = %q, want {}", response.Code, response.Body.String())
	}
}

func TestAgentsListReturnsEmptyArray(t *testing.T) {
	server := NewServer(config.Config{MasterKey: "master-key"})

	unauthorized := httptest.NewRequest(http.MethodGet, "/v1/agents", nil)
	unauthorizedResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", unauthorizedResponse.Code)
	}

	request := httptest.NewRequest(http.MethodGet, "/v1/agents", nil)
	request.Header.Set("Authorization", "Bearer master-key")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != "[]" {
		t.Fatalf("status = %d body = %q, want []", response.Code, response.Body.String())
	}
}

func TestGuardrailsListEndpointsReturnEmptyGuardrails(t *testing.T) {
	server := NewServer(config.Config{MasterKey: "master-key"})
	for _, path := range []string{"/v2/guardrails/list", "/guardrails/list"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer master-key")
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status = %d body = %s", path, response.Code, response.Body.String())
		}
		var body struct {
			Guardrails []any `json:"guardrails"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Guardrails == nil || len(body.Guardrails) != 0 {
			t.Fatalf("%s body = %s, want empty guardrails array", path, response.Body.String())
		}
	}
}

func TestV2UserInfoLooksUpUserFromManager(t *testing.T) {
	manager := &memoryUserManager{users: map[string]auth.ManagedUser{}}
	server := NewServer(config.Config{MasterKey: "master-key"}).WithUserManager(manager)

	request := httptest.NewRequest(http.MethodGet, "/v2/user/info?user_id=user-test", nil)
	request.Header.Set("Authorization", "Bearer master-key")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing user status = %d body = %s, want 404", response.Code, response.Body.String())
	}

	manager.users["user-test"] = auth.ManagedUser{UserID: "user-test", UserAlias: "Dev", UserEmail: "dev@example.com", Models: []string{"gpt-test"}, TeamID: "team-test"}
	second := httptest.NewRequest(http.MethodGet, "/v2/user/info?user_id=user-test", nil)
	second.Header.Set("Authorization", "Bearer master-key")
	secondResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(secondResponse, second)

	if secondResponse.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", secondResponse.Code, secondResponse.Body.String())
	}
	var body struct {
		UserID    string   `json:"user_id"`
		UserEmail string   `json:"user_email"`
		UserRole  string   `json:"user_role"`
		Models    []string `json:"models"`
		Teams     []string `json:"teams"`
	}
	if err := json.Unmarshal(secondResponse.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v body = %s", err, secondResponse.Body.String())
	}
	if body.UserID != "user-test" || body.UserEmail != "dev@example.com" || body.Models[0] != "gpt-test" || len(body.Teams) != 1 || body.Teams[0] != "team-test" {
		t.Fatalf("body = %+v", body)
	}
}

func TestV2UserInfoServesBuiltInAdminWithoutDatabaseRow(t *testing.T) {
	server := NewServer(config.Config{MasterKey: "master-key"}).WithUserManager(&memoryUserManager{users: map[string]auth.ManagedUser{}})
	request := httptest.NewRequest(http.MethodGet, "/v2/user/info?user_id=default_user_id", nil)
	request.Header.Set("Authorization", "Bearer master-key")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s, want 200", response.Code, response.Body.String())
	}
	var body struct {
		UserID   string `json:"user_id"`
		UserRole string `json:"user_role"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.UserID != "default_user_id" || body.UserRole != "proxy_admin" {
		t.Fatalf("body = %s", response.Body.String())
	}
}

func TestOrganizationListReturnsBareArray(t *testing.T) {
	manager := &memoryOrganizationManager{organizations: map[string]auth.ManagedOrganization{
		"org-test": {OrganizationID: "org-test", OrganizationAlias: "Platform"},
	}}
	server := NewServer(config.Config{MasterKey: "master-key"}).WithOrganizationManager(manager)
	request := httptest.NewRequest(http.MethodGet, "/organization/list", nil)
	request.Header.Set("Authorization", "Bearer master-key")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	body := strings.TrimSpace(response.Body.String())
	if !strings.HasPrefix(body, "[") || !strings.HasSuffix(body, "]") {
		t.Fatalf("body = %s, want bare JSON array", body)
	}
	var orgs []struct {
		OrganizationID string `json:"organization_id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &orgs); err != nil || len(orgs) != 1 || orgs[0].OrganizationID != "org-test" {
		t.Fatalf("body = %s", response.Body.String())
	}
}

func TestTeamListV1ReturnsBareArray(t *testing.T) {
	manager := &memoryTeamManager{teams: map[string]auth.ManagedTeam{
		"team-test": {TeamID: "team-test", TeamAlias: "Engineering"},
	}}
	server := NewServer(config.Config{MasterKey: "master-key"}).WithTeamManager(manager)
	request := httptest.NewRequest(http.MethodGet, "/team/list", nil)
	request.Header.Set("Authorization", "Bearer master-key")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	body := strings.TrimSpace(response.Body.String())
	if !strings.HasPrefix(body, "[") || !strings.HasSuffix(body, "]") {
		t.Fatalf("body = %s, want bare JSON array", body)
	}
	var teams []struct {
		TeamID string `json:"team_id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &teams); err != nil || len(teams) != 1 || teams[0].TeamID != "team-test" {
		t.Fatalf("body = %s", response.Body.String())
	}
}

func TestUserListReturnsPaginatedShape(t *testing.T) {
	manager := &memoryUserManager{users: map[string]auth.ManagedUser{}}
	for _, id := range []string{"user-a", "user-b", "user-c"} {
		manager.users[id] = auth.ManagedUser{UserID: id, UserEmail: id + "@example.com"}
	}
	server := NewServer(config.Config{MasterKey: "master-key"}).WithUserManager(manager)

	page1 := httptest.NewRequest(http.MethodGet, "/user/list?page=1&page_size=2", nil)
	page1.Header.Set("Authorization", "Bearer master-key")
	page1Response := httptest.NewRecorder()
	server.Handler().ServeHTTP(page1Response, page1)

	if page1Response.Code != http.StatusOK {
		t.Fatalf("page1 status = %d body = %s", page1Response.Code, page1Response.Body.String())
	}
	var first struct {
		Page       int `json:"page"`
		PageSize   int `json:"page_size"`
		Total      int `json:"total"`
		TotalPages int `json:"total_pages"`
		Users      []struct {
			UserID string `json:"user_id"`
		} `json:"users"`
	}
	if err := json.Unmarshal(page1Response.Body.Bytes(), &first); err != nil {
		t.Fatalf("unmarshal: %v body = %s", err, page1Response.Body.String())
	}
	if first.Page != 1 || first.PageSize != 2 || first.Total != 3 || first.TotalPages != 2 || len(first.Users) != 2 {
		t.Fatalf("page1 = %+v", first)
	}

	page2 := httptest.NewRequest(http.MethodGet, "/user/list?page=2&page_size=2", nil)
	page2.Header.Set("Authorization", "Bearer master-key")
	page2Response := httptest.NewRecorder()
	server.Handler().ServeHTTP(page2Response, page2)

	var second struct {
		Page  int `json:"page"`
		Users []struct {
			UserID string `json:"user_id"`
		} `json:"users"`
	}
	if err := json.Unmarshal(page2Response.Body.Bytes(), &second); err != nil || second.Page != 2 || len(second.Users) != 1 {
		t.Fatalf("page2 = %s", page2Response.Body.String())
	}

	unauthorized := httptest.NewRequest(http.MethodGet, "/user/list", nil)
	unauthorizedResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", unauthorizedResponse.Code)
	}
}

func TestCustomerListReturnsEmptyArray(t *testing.T) {
	server := NewServer(config.Config{MasterKey: "master-key"})

	request := httptest.NewRequest(http.MethodGet, "/customer/list", nil)
	request.Header.Set("Authorization", "Bearer master-key")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != "[]" {
		t.Fatalf("status = %d body = %q, want []", response.Code, response.Body.String())
	}

	unauthorized := httptest.NewRequest(http.MethodGet, "/customer/list", nil)
	unauthorizedResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", unauthorizedResponse.Code)
	}
}

func TestGatewayDailyActivityReturnsZeroedShape(t *testing.T) {
	server := NewServer(config.Config{MasterKey: "master-key"})
	request := httptest.NewRequest(http.MethodGet, "/gateway/daily/activity", nil)
	request.Header.Set("Authorization", "Bearer master-key")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	var body struct {
		TotalSuccessfulRequests int   `json:"total_successful_requests"`
		TotalFailedRequests     int   `json:"total_failed_requests"`
		ByDate                  []any `json:"by_date"`
		ByRoute                 []any `json:"by_route"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v body = %s", err, response.Body.String())
	}
	if body.ByDate == nil || body.ByRoute == nil || len(body.ByDate) != 0 || len(body.ByRoute) != 0 {
		t.Fatalf("body = %s, want empty by_date/by_route arrays", response.Body.String())
	}
}

func TestUserDailyActivityAggregatedReturnsEmptyResults(t *testing.T) {
	server := NewServer(config.Config{MasterKey: "master-key"})
	request := httptest.NewRequest(http.MethodGet, "/user/daily/activity/aggregated?start_date=2026-08-01&end_date=2026-08-31", nil)
	request.Header.Set("Authorization", "Bearer master-key")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	var body struct {
		Results  []any `json:"results"`
		Metadata struct {
			TotalSpend  float64 `json:"total_spend"`
			Page        int     `json:"page"`
			TotalPages  int     `json:"total_pages"`
			HasMore     bool    `json:"has_more"`
			TotalTokens int     `json:"total_tokens"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v body = %s", err, response.Body.String())
	}
	if body.Results == nil || len(body.Results) != 0 || body.Metadata.Page != 1 || body.Metadata.TotalPages != 1 || body.Metadata.HasMore {
		t.Fatalf("body = %s", response.Body.String())
	}
}

func TestAvailableUsersReturnsUnlicensedShape(t *testing.T) {
	server := NewServer(config.Config{MasterKey: "master-key"})
	request := httptest.NewRequest(http.MethodGet, "/user/available_users", nil)
	request.Header.Set("Authorization", "Bearer master-key")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	var body struct {
		TotalUsers          any `json:"total_users"`
		TotalUsersUsed      int `json:"total_users_used"`
		TotalUsersRemaining any `json:"total_users_remaining"`
		TotalTeams          any `json:"total_teams"`
		TotalTeamsUsed      int `json:"total_teams_used"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v body = %s", err, response.Body.String())
	}
	if body.TotalUsers != nil || body.TotalTeams != nil || body.TotalUsersUsed != 0 || body.TotalTeamsUsed != 0 {
		t.Fatalf("body = %s, want null limits and zeroed used counts", response.Body.String())
	}
}

func TestUiSpendLogsEndpointContract(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	logs := []usage.Log{
		{RequestID: "req-old", Model: "gpt-4o-mini", CallType: "completion", TotalTokens: 10, Status: "success", StartedAt: now.Add(-2 * time.Hour), CompletedAt: now.Add(-2*time.Hour + time.Second)},
		{RequestID: "req-mid", Model: "gpt-4o-mini", CallType: "completion", TotalTokens: 20, Status: "success", StartedAt: now.Add(-1 * time.Hour), CompletedAt: now.Add(-1*time.Hour + time.Second)},
		{RequestID: "req-new", Model: "claude", CallType: "completion", TotalTokens: 30, Status: "success", StartedAt: now, CompletedAt: now.Add(time.Second)},
	}
	server := NewServer(config.Config{MasterKey: "master-key"}).WithSpendLogReader(memorySpendLogReader{logs: logs})

	request := httptest.NewRequest(http.MethodGet, "/spend/logs/ui?start_date=2026-08-31%2000:00:00&end_date=2026-08-31%2023:59:59&page=1&page_size=2", nil)
	request.Header.Set("Authorization", "Bearer master-key")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	var body struct {
		Data []struct {
			RequestID   string `json:"request_id"`
			Model       string `json:"model"`
			TotalTokens int    `json:"total_tokens"`
			StartTime   string `json:"startTime"`
			EndTime     string `json:"endTime"`
			CallType    string `json:"call_type"`
		} `json:"data"`
		Total      int `json:"total"`
		Page       int `json:"page"`
		PageSize   int `json:"page_size"`
		TotalPages int `json:"total_pages"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v body = %s", err, response.Body.String())
	}
	if body.Total != 3 || body.Page != 1 || body.PageSize != 2 || body.TotalPages != 2 || len(body.Data) != 2 {
		t.Fatalf("body = %s", response.Body.String())
	}
	if body.Data[0].RequestID != "req-new" || body.Data[1].RequestID != "req-mid" {
		t.Fatalf("expected startTime desc order, got %s then %s", body.Data[0].RequestID, body.Data[1].RequestID)
	}
	if body.Data[0].StartTime == "" || body.Data[0].EndTime == "" {
		t.Fatalf("missing startTime/endTime: %s", response.Body.String())
	}

	// page 2 has the remaining entry
	page2 := httptest.NewRequest(http.MethodGet, "/spend/logs/ui?start_date=2026-08-31%2000:00:00&end_date=2026-08-31%2023:59:59&page=2&page_size=2", nil)
	page2.Header.Set("Authorization", "Bearer master-key")
	page2Response := httptest.NewRecorder()
	server.Handler().ServeHTTP(page2Response, page2)
	var second struct {
		Data []struct {
			RequestID string `json:"request_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(page2Response.Body.Bytes(), &second); err != nil || len(second.Data) != 1 || second.Data[0].RequestID != "req-old" {
		t.Fatalf("page2 = %s", page2Response.Body.String())
	}

	// time range filter excludes older logs
	filtered := httptest.NewRequest(http.MethodGet, "/spend/logs/ui?start_date=2026-08-31%2011:00:00&end_date=2026-08-31%2023:59:59&page=1&page_size=50", nil)
	filtered.Header.Set("Authorization", "Bearer master-key")
	filteredResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(filteredResponse, filtered)
	var filterBody struct {
		Total int `json:"total"`
	}
	if err := json.Unmarshal(filteredResponse.Body.Bytes(), &filterBody); err != nil || filterBody.Total != 2 {
		t.Fatalf("time-filtered total = %s", filteredResponse.Body.String())
	}

	// model filter
	byModel := httptest.NewRequest(http.MethodGet, "/spend/logs/ui?start_date=2026-08-31%2000:00:00&end_date=2026-08-31%2023:59:59&page=1&page_size=50&model=claude", nil)
	byModel.Header.Set("Authorization", "Bearer master-key")
	byModelResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(byModelResponse, byModel)
	var modelBody struct {
		Data []struct {
			Model string `json:"model"`
		} `json:"data"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(byModelResponse.Body.Bytes(), &modelBody); err != nil || modelBody.Total != 1 || modelBody.Data[0].Model != "claude" {
		t.Fatalf("model-filtered = %s", byModelResponse.Body.String())
	}

	unauthorized := httptest.NewRequest(http.MethodGet, "/spend/logs/ui", nil)
	unauthorizedResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", unauthorizedResponse.Code)
	}
}

func TestModelInfoV2EndpointContract(t *testing.T) {
	server := NewServer(config.Config{
		MasterKey: "master-key",
		Models: []config.Model{
			{Name: "gpt-4o-mini", Model: "openai/gpt-4o-mini", APIBase: "http://127.0.0.1:14999/v1"},
			{Name: "claude", Model: "anthropic/claude"},
		},
	})
	request := httptest.NewRequest(http.MethodGet, "/v2/model/info?include_team_models=true&page=1&size=50", nil)
	request.Header.Set("Authorization", "Bearer master-key")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	var body struct {
		Data []struct {
			ModelName        string `json:"model_name"`
			LiteLLMModelName string `json:"litellm_model_name"`
		} `json:"data"`
		TotalCount  int `json:"total_count"`
		CurrentPage int `json:"current_page"`
		TotalPages  int `json:"total_pages"`
		Size        int `json:"size"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v body = %s", err, response.Body.String())
	}
	if body.TotalCount != 2 || body.CurrentPage != 1 || body.TotalPages != 1 || body.Size != 50 || len(body.Data) != 2 {
		t.Fatalf("body = %s", response.Body.String())
	}
	if body.Data[0].ModelName != "gpt-4o-mini" || body.Data[0].LiteLLMModelName != "openai/gpt-4o-mini" {
		t.Fatalf("data = %s", response.Body.String())
	}

	unauthorized := httptest.NewRequest(http.MethodGet, "/v2/model/info", nil)
	unauthorizedResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", unauthorizedResponse.Code)
	}
}
