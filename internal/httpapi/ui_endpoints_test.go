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
