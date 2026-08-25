package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/BerriAI/litellm/go-proxy/internal/auth"
	"github.com/BerriAI/litellm/go-proxy/internal/config"
	"github.com/BerriAI/litellm/go-proxy/internal/providers"
	"github.com/BerriAI/litellm/go-proxy/internal/usage"
)

func TestModelsRequiresMasterKey(t *testing.T) {
	server := NewServer(config.Config{MasterKey: "master-key", Models: []config.Model{{Name: "gpt-test", Model: "openai/gpt-test"}}})
	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestVirtualKeyManagementRequiresMasterKeyAndStoresOnlyHash(t *testing.T) {
	manager := &memoryKeyManager{records: map[string]auth.ManagedVirtualKey{}}
	server := NewServer(config.Config{MasterKey: "master-key"}).WithVirtualKeyManager(manager)
	generate := httptest.NewRequest(http.MethodPost, "/key/generate", strings.NewReader(`{"key":"sk-test-key","key_alias":"integration","models":["gateway-model"],"organization_id":"org-test","budget_id":"budget-test","rpm_limit":12}`))
	generate.Header.Set("Authorization", "Bearer master-key")
	generated := httptest.NewRecorder()
	server.Handler().ServeHTTP(generated, generate)
	if generated.Code != http.StatusOK || !strings.Contains(generated.Body.String(), `"key":"sk-test-key"`) {
		t.Fatalf("generate status=%d body=%s", generated.Code, generated.Body.String())
	}
	if _, found := manager.records["sk-test-key"]; found {
		t.Fatal("manager stored raw key")
	}
	if manager.records[auth.HashKey("sk-test-key")].OrganizationID != "org-test" {
		t.Fatalf("organization scope was not persisted: %#v", manager.records)
	}
	if manager.records[auth.HashKey("sk-test-key")].BudgetID != "budget-test" {
		t.Fatalf("budget scope was not persisted: %#v", manager.records)
	}

	info := httptest.NewRequest(http.MethodGet, "/key/info?key=sk-test-key", nil)
	info.Header.Set("Authorization", "Bearer master-key")
	infoResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(infoResponse, info)
	if infoResponse.Code != http.StatusOK || !strings.Contains(infoResponse.Body.String(), `"key_alias":"integration"`) {
		t.Fatalf("info status=%d body=%s", infoResponse.Code, infoResponse.Body.String())
	}
	blockRequest := httptest.NewRequest(http.MethodPost, "/key/block", strings.NewReader(`{"key":"sk-test-key"}`))
	blockRequest.Header.Set("Authorization", "Bearer master-key")
	blocked := httptest.NewRecorder()
	server.Handler().ServeHTTP(blocked, blockRequest)
	if blocked.Code != http.StatusOK || !manager.records[auth.HashKey("sk-test-key")].Blocked {
		t.Fatalf("block status=%d record=%#v", blocked.Code, manager.records[auth.HashKey("sk-test-key")])
	}
	unblockRequest := httptest.NewRequest(http.MethodPost, "/key/unblock", strings.NewReader(`{"key":"sk-test-key"}`))
	unblockRequest.Header.Set("Authorization", "Bearer master-key")
	unblocked := httptest.NewRecorder()
	server.Handler().ServeHTTP(unblocked, unblockRequest)
	if unblocked.Code != http.StatusOK || manager.records[auth.HashKey("sk-test-key")].Blocked {
		t.Fatalf("unblock status=%d record=%#v", unblocked.Code, manager.records[auth.HashKey("sk-test-key")])
	}
	updateRequest := httptest.NewRequest(http.MethodPost, "/key/update", strings.NewReader(`{"key":"sk-test-key","key_alias":"updated","models":["other-model"],"rpm_limit":8}`))
	updateRequest.Header.Set("Authorization", "Bearer master-key")
	updated := httptest.NewRecorder()
	server.Handler().ServeHTTP(updated, updateRequest)
	record := manager.records[auth.HashKey("sk-test-key")]
	if updated.Code != http.StatusOK || record.KeyAlias != "updated" || len(record.Models) != 1 || record.Models[0] != "other-model" || record.RPMLimit == nil || *record.RPMLimit != 8 {
		t.Fatalf("update status=%d record=%#v", updated.Code, record)
	}
	listRequest := httptest.NewRequest(http.MethodGet, "/key/list", nil)
	listRequest.Header.Set("Authorization", "Bearer master-key")
	listed := httptest.NewRecorder()
	server.Handler().ServeHTTP(listed, listRequest)
	if listed.Code != http.StatusOK || strings.Contains(listed.Body.String(), "sk-test-key") || !strings.Contains(listed.Body.String(), `"key_alias":"updated"`) {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
	}
	regenerateRequest := httptest.NewRequest(http.MethodPost, "/key/regenerate", strings.NewReader(`{"key":"sk-test-key"}`))
	regenerateRequest.Header.Set("Authorization", "Bearer master-key")
	regenerated := httptest.NewRecorder()
	server.Handler().ServeHTTP(regenerated, regenerateRequest)
	if regenerated.Code != http.StatusOK || !strings.Contains(regenerated.Body.String(), `"key":"sk-`) || len(manager.records) != 1 {
		t.Fatalf("regenerate status=%d body=%s records=%v", regenerated.Code, regenerated.Body.String(), manager.records)
	}

	deleteRequest := httptest.NewRequest(http.MethodPost, "/key/delete", strings.NewReader(`{"key":"sk-test-key"}`))
	deleteRequest.Header.Set("Authorization", "Bearer master-key")
	deleted := httptest.NewRecorder()
	server.Handler().ServeHTTP(deleted, deleteRequest)
	if deleted.Code != http.StatusOK || !strings.Contains(deleted.Body.String(), `"deleted":false`) || len(manager.records) != 1 {
		t.Fatalf("delete status=%d records=%v", deleted.Code, manager.records)
	}
}

func TestTeamManagementLifecycle(t *testing.T) {
	manager := &memoryTeamManager{teams: map[string]auth.ManagedTeam{}}
	server := NewServer(config.Config{MasterKey: "master-key"}).WithTeamManager(manager)
	create := httptest.NewRequest(http.MethodPost, "/team/new", strings.NewReader(`{"team_id":"team-test","team_alias":"Engineering","admins":["admin"],"members":["member"],"models":["gateway-model"]}`))
	create.Header.Set("Authorization", "Bearer master-key")
	created := httptest.NewRecorder()
	server.Handler().ServeHTTP(created, create)
	if created.Code != http.StatusOK || manager.teams["team-test"].TeamAlias != "Engineering" {
		t.Fatalf("create status=%d teams=%#v", created.Code, manager.teams)
	}
	if strings.Contains(created.Body.String(), `"admins":null`) || strings.Contains(created.Body.String(), `"members":null`) {
		t.Fatalf("create body must normalize arrays: %s", created.Body.String())
	}
	info := httptest.NewRequest(http.MethodGet, "/team/info?team_id=team-test", nil)
	info.Header.Set("Authorization", "Bearer master-key")
	infoResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(infoResponse, info)
	if infoResponse.Code != http.StatusOK || !strings.Contains(infoResponse.Body.String(), `"team_alias":"Engineering"`) {
		t.Fatalf("info status=%d body=%s", infoResponse.Code, infoResponse.Body.String())
	}
	update := httptest.NewRequest(http.MethodPost, "/team/update", strings.NewReader(`{"team_id":"team-test","team_alias":"Updated"}`))
	update.Header.Set("Authorization", "Bearer master-key")
	updated := httptest.NewRecorder()
	server.Handler().ServeHTTP(updated, update)
	if updated.Code != http.StatusOK || manager.teams["team-test"].TeamAlias != "Updated" {
		t.Fatalf("update status=%d team=%#v", updated.Code, manager.teams["team-test"])
	}
	block := httptest.NewRequest(http.MethodPost, "/team/block", strings.NewReader(`{"team_id":"team-test"}`))
	block.Header.Set("Authorization", "Bearer master-key")
	blocked := httptest.NewRecorder()
	server.Handler().ServeHTTP(blocked, block)
	if blocked.Code != http.StatusOK || !manager.teams["team-test"].Blocked {
		t.Fatalf("block status=%d team=%#v", blocked.Code, manager.teams["team-test"])
	}
	deleteRequest := httptest.NewRequest(http.MethodPost, "/team/delete", strings.NewReader(`{"team_id":"team-test"}`))
	deleteRequest.Header.Set("Authorization", "Bearer master-key")
	deleted := httptest.NewRecorder()
	server.Handler().ServeHTTP(deleted, deleteRequest)
	if deleted.Code != http.StatusOK || len(manager.teams) != 0 {
		t.Fatalf("delete status=%d teams=%#v", deleted.Code, manager.teams)
	}
}

func TestUserManagementLifecycle(t *testing.T) {
	manager := &memoryUserManager{users: map[string]auth.ManagedUser{}}
	server := NewServer(config.Config{MasterKey: "master-key"}).WithUserManager(manager)
	create := httptest.NewRequest(http.MethodPost, "/user/new", strings.NewReader("{\"user_id\":\"user-test\",\"user_alias\":\"Developer\",\"team_id\":\"team-test\",\"user_email\":\"developer@example.com\",\"models\":[\"gateway-model\"]}"))
	create.Header.Set("Authorization", "Bearer master-key")
	created := httptest.NewRecorder()
	server.Handler().ServeHTTP(created, create)
	if created.Code != http.StatusOK || manager.users["user-test"].UserEmail != "developer@example.com" {
		t.Fatalf("create status=%d users=%#v", created.Code, manager.users)
	}
	info := httptest.NewRequest(http.MethodGet, "/user/info?user_id=user-test", nil)
	info.Header.Set("Authorization", "Bearer master-key")
	infoResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(infoResponse, info)
	if infoResponse.Code != http.StatusOK || !strings.Contains(infoResponse.Body.String(), "\"team_id\":\"team-test\"") {
		t.Fatalf("info status=%d body=%s", infoResponse.Code, infoResponse.Body.String())
	}
	update := httptest.NewRequest(http.MethodPost, "/user/update", strings.NewReader("{\"user_id\":\"user-test\",\"user_alias\":\"Updated\"}"))
	update.Header.Set("Authorization", "Bearer master-key")
	updated := httptest.NewRecorder()
	server.Handler().ServeHTTP(updated, update)
	if updated.Code != http.StatusOK || manager.users["user-test"].UserAlias != "Updated" {
		t.Fatalf("update status=%d user=%#v", updated.Code, manager.users["user-test"])
	}
	block := httptest.NewRequest(http.MethodPost, "/user/block", strings.NewReader("{\"user_id\":\"user-test\"}"))
	block.Header.Set("Authorization", "Bearer master-key")
	blocked := httptest.NewRecorder()
	server.Handler().ServeHTTP(blocked, block)
	if blocked.Code != http.StatusOK || !manager.users["user-test"].Blocked {
		t.Fatalf("block status=%d user=%#v", blocked.Code, manager.users["user-test"])
	}
	deleteRequest := httptest.NewRequest(http.MethodPost, "/user/delete", strings.NewReader("{\"user_id\":\"user-test\"}"))
	deleteRequest.Header.Set("Authorization", "Bearer master-key")
	deleted := httptest.NewRecorder()
	server.Handler().ServeHTTP(deleted, deleteRequest)
	if deleted.Code != http.StatusOK || len(manager.users) != 0 {
		t.Fatalf("delete status=%d users=%#v", deleted.Code, manager.users)
	}
}

func TestProjectManagementLifecycle(t *testing.T) {
	manager := &memoryProjectManager{projects: map[string]auth.ManagedProject{}}
	server := NewServer(config.Config{MasterKey: "master-key"}).WithProjectManager(manager)
	create := httptest.NewRequest(http.MethodPost, "/project/new", strings.NewReader("{\"project_id\":\"project-test\",\"project_alias\":\"Platform\",\"team_id\":\"team-test\",\"models\":[\"gateway-model\"]}"))
	create.Header.Set("Authorization", "Bearer master-key")
	created := httptest.NewRecorder()
	server.Handler().ServeHTTP(created, create)
	if created.Code != http.StatusOK || manager.projects["project-test"].ProjectAlias != "Platform" {
		t.Fatalf("create status=%d projects=%#v", created.Code, manager.projects)
	}
	update := httptest.NewRequest(http.MethodPost, "/project/update", strings.NewReader("{\"project_id\":\"project-test\",\"project_alias\":\"Updated\"}"))
	update.Header.Set("Authorization", "Bearer master-key")
	updated := httptest.NewRecorder()
	server.Handler().ServeHTTP(updated, update)
	if updated.Code != http.StatusOK || manager.projects["project-test"].ProjectAlias != "Updated" {
		t.Fatalf("update status=%d project=%#v", updated.Code, manager.projects["project-test"])
	}
	block := httptest.NewRequest(http.MethodPost, "/project/block", strings.NewReader("{\"project_id\":\"project-test\"}"))
	block.Header.Set("Authorization", "Bearer master-key")
	blocked := httptest.NewRecorder()
	server.Handler().ServeHTTP(blocked, block)
	if blocked.Code != http.StatusOK || !manager.projects["project-test"].Blocked {
		t.Fatalf("block status=%d project=%#v", blocked.Code, manager.projects["project-test"])
	}
	deleteRequest := httptest.NewRequest(http.MethodPost, "/project/delete", strings.NewReader("{\"project_id\":\"project-test\"}"))
	deleteRequest.Header.Set("Authorization", "Bearer master-key")
	deleted := httptest.NewRecorder()
	server.Handler().ServeHTTP(deleted, deleteRequest)
	if deleted.Code != http.StatusOK || len(manager.projects) != 0 {
		t.Fatalf("delete status=%d projects=%#v", deleted.Code, manager.projects)
	}
}

func TestOrganizationManagementLifecycle(t *testing.T) {
	manager := &memoryOrganizationManager{organizations: map[string]auth.ManagedOrganization{}}
	server := NewServer(config.Config{MasterKey: "master-key"}).WithOrganizationManager(manager)
	create := httptest.NewRequest(http.MethodPost, "/organization/new", strings.NewReader(`{"organization_id":"org-test","organization_alias":"Platform","budget_id":"budget-test","models":["gateway-model"]}`))
	create.Header.Set("Authorization", "Bearer master-key")
	created := httptest.NewRecorder()
	server.Handler().ServeHTTP(created, create)
	if created.Code != http.StatusOK || manager.organizations["org-test"].OrganizationAlias != "Platform" {
		t.Fatalf("create status=%d organizations=%#v", created.Code, manager.organizations)
	}
	info := httptest.NewRequest(http.MethodGet, "/organization/info?organization_id=org-test", nil)
	info.Header.Set("Authorization", "Bearer master-key")
	infoResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(infoResponse, info)
	if infoResponse.Code != http.StatusOK || !strings.Contains(infoResponse.Body.String(), `"budget_id":"budget-test"`) {
		t.Fatalf("info status=%d body=%s", infoResponse.Code, infoResponse.Body.String())
	}
	update := httptest.NewRequest(http.MethodPost, "/organization/update", strings.NewReader(`{"organization_id":"org-test","organization_alias":"Updated","blocked":true}`))
	update.Header.Set("Authorization", "Bearer master-key")
	updated := httptest.NewRecorder()
	server.Handler().ServeHTTP(updated, update)
	if updated.Code != http.StatusOK || manager.organizations["org-test"].OrganizationAlias != "Updated" || !manager.organizations["org-test"].Blocked {
		t.Fatalf("update status=%d organization=%#v", updated.Code, manager.organizations["org-test"])
	}
	list := httptest.NewRequest(http.MethodGet, "/organization/list", nil)
	list.Header.Set("Authorization", "Bearer master-key")
	listed := httptest.NewRecorder()
	server.Handler().ServeHTTP(listed, list)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"organization_id":"org-test"`) {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
	}
	deleteRequest := httptest.NewRequest(http.MethodPost, "/organization/delete", strings.NewReader(`{"organization_id":"org-test"}`))
	deleteRequest.Header.Set("Authorization", "Bearer master-key")
	deleted := httptest.NewRecorder()
	server.Handler().ServeHTTP(deleted, deleteRequest)
	if deleted.Code != http.StatusOK || len(manager.organizations) != 0 {
		t.Fatalf("delete status=%d organizations=%#v", deleted.Code, manager.organizations)
	}
}

func TestBudgetManagementLifecycle(t *testing.T) {
	manager := &memoryBudgetManager{budgets: map[string]auth.ManagedBudget{}}
	server := NewServer(config.Config{MasterKey: "master-key"}).WithBudgetManager(manager)
	create := httptest.NewRequest(http.MethodPost, "/budget/new", strings.NewReader(`{"budget_id":"budget-test","max_budget":12.5,"rpm_limit":4,"budget_duration":"1d"}`))
	create.Header.Set("Authorization", "Bearer master-key")
	created := httptest.NewRecorder()
	server.Handler().ServeHTTP(created, create)
	if created.Code != http.StatusOK || manager.budgets["budget-test"].MaxBudget == nil || *manager.budgets["budget-test"].MaxBudget != 12.5 {
		t.Fatalf("create status=%d budgets=%#v", created.Code, manager.budgets)
	}
	info := httptest.NewRequest(http.MethodPost, "/budget/info", strings.NewReader(`{"budgets":["budget-test"]}`))
	info.Header.Set("Authorization", "Bearer master-key")
	infoResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(infoResponse, info)
	if infoResponse.Code != http.StatusOK || !strings.Contains(infoResponse.Body.String(), `"budget_id":"budget-test"`) {
		t.Fatalf("info status=%d body=%s", infoResponse.Code, infoResponse.Body.String())
	}
	update := httptest.NewRequest(http.MethodPost, "/budget/update", strings.NewReader(`{"budget_id":"budget-test","soft_budget":10}`))
	update.Header.Set("Authorization", "Bearer master-key")
	updated := httptest.NewRecorder()
	server.Handler().ServeHTTP(updated, update)
	if updated.Code != http.StatusOK || manager.budgets["budget-test"].SoftBudget == nil || *manager.budgets["budget-test"].SoftBudget != 10 {
		t.Fatalf("update status=%d budget=%#v", updated.Code, manager.budgets["budget-test"])
	}
	list := httptest.NewRequest(http.MethodGet, "/budget/list", nil)
	list.Header.Set("Authorization", "Bearer master-key")
	listed := httptest.NewRecorder()
	server.Handler().ServeHTTP(listed, list)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"budget_id":"budget-test"`) {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
	}
	deleteRequest := httptest.NewRequest(http.MethodPost, "/budget/delete", strings.NewReader(`{"id":"budget-test"}`))
	deleteRequest.Header.Set("Authorization", "Bearer master-key")
	deleted := httptest.NewRecorder()
	server.Handler().ServeHTTP(deleted, deleteRequest)
	if deleted.Code != http.StatusOK || len(manager.budgets) != 0 {
		t.Fatalf("delete status=%d budgets=%#v", deleted.Code, manager.budgets)
	}
}

func TestChatCompletionsAcceptsAllowedVirtualKey(t *testing.T) {
	server := NewServerWithVirtualKeyValidator(
		config.Config{Models: []config.Model{
			{Name: "gateway-model", Model: "openai/gpt-5"},
			{Name: "other-model", Model: "openai/gpt-5-mini"},
		}},
		stubChatCompleter{},
		stubVirtualKeyValidator{},
	)
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gateway-model","messages":[]}`))
	request.Header.Set("Authorization", "Bearer sk-virtual-key")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
}

func TestChatCompletionsRejectsVirtualKeyForOtherModel(t *testing.T) {
	server := NewServerWithVirtualKeyValidator(
		config.Config{Models: []config.Model{
			{Name: "gateway-model", Model: "openai/gpt-5"},
			{Name: "other-model", Model: "openai/gpt-5-mini"},
		}},
		stubChatCompleter{},
		stubVirtualKeyValidator{},
	)
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"other-model","messages":[]}`))
	request.Header.Set("Authorization", "Bearer sk-virtual-key")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestModelsReturnsConfiguredModels(t *testing.T) {
	server := NewServer(config.Config{MasterKey: "master-key", Models: []config.Model{{Name: "gpt-test", Model: "openai/gpt-test"}}})
	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	request.Header.Set("Authorization", "Bearer master-key")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if body := response.Body.String(); body != "{\"object\":\"list\",\"data\":[{\"id\":\"gpt-test\",\"object\":\"model\",\"created\":0,\"owned_by\":\"openai\"}]}\n" {
		t.Fatalf("body = %s", body)
	}
}

func TestModelsListsEachModelGroupOnce(t *testing.T) {
	server := NewServer(config.Config{MasterKey: "master-key", Models: []config.Model{{Name: "shared", Model: "openai/a"}, {Name: "shared", Model: "openai/b"}}})
	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	request.Header.Set("Authorization", "Bearer master-key")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || strings.Count(response.Body.String(), `"id":"shared"`) != 1 {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestModelReturnsConfiguredModelAndRejectsUnknownModel(t *testing.T) {
	server := NewServer(config.Config{MasterKey: "master-key", Models: []config.Model{{Name: "gpt-test", Model: "openai/gpt-test"}}})
	request := httptest.NewRequest(http.MethodGet, "/v1/models/gpt-test", nil)
	request.Header.Set("Authorization", "Bearer master-key")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"id":"gpt-test"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	unknownRequest := httptest.NewRequest(http.MethodGet, "/v1/models/unknown", nil)
	unknownRequest.Header.Set("Authorization", "Bearer master-key")
	unknownResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(unknownResponse, unknownRequest)
	if unknownResponse.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", unknownResponse.Code, unknownResponse.Body.String())
	}
}

func TestModelsAppliesAllVirtualKeyModelScopes(t *testing.T) {
	server := NewServerWithVirtualKeyValidator(config.Config{Models: []config.Model{{Name: "shared", Model: "openai/shared"}, {Name: "key-only", Model: "openai/key-only"}}}, nil, scopedModelsValidator{})
	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	request.Header.Set("Authorization", "Bearer sk-test")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"id":"shared"`) || strings.Contains(response.Body.String(), `"id":"key-only"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestSpendLogsRequiresAdminAndReturnsLogs(t *testing.T) {
	server := NewServer(config.Config{MasterKey: "master-key"}).WithSpendLogReader(memorySpendLogReader{logs: []usage.Log{{RequestID: "request-test", TotalTokens: 5}}})
	request := httptest.NewRequest(http.MethodGet, "/spend/logs", nil)
	request.Header.Set("Authorization", "Bearer master-key")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"request_id":"request-test"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestHealthDoesNotRequireAuthentication(t *testing.T) {
	server := NewServer(config.Config{MasterKey: "master-key"})
	request := httptest.NewRequest(http.MethodGet, "/health/liveliness", nil)
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
}

func TestLegacyHealthRouteDoesNotRequireAuthentication(t *testing.T) {
	server := NewServer(config.Config{MasterKey: "master-key"})
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Body.String() != "{\"status\":\"ok\"}\n" {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestHandlerPreservesRequestID(t *testing.T) {
	server := NewServer(config.Config{})
	request := httptest.NewRequest(http.MethodGet, "/health/liveliness", nil)
	request.Header.Set("X-Request-Id", "request-123")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Header().Get("X-Request-Id") != "request-123" {
		t.Fatalf("request id = %q", response.Header().Get("X-Request-Id"))
	}
}

func TestReadinessReportsUnavailableDependency(t *testing.T) {
	server := NewServerWithRuntime(config.Config{}, nil, nil, nil, nil, failingReadinessCheck{})
	request := httptest.NewRequest(http.MethodGet, "/health/readiness", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}

func TestChatCompletionsForwardsConfiguredDeployment(t *testing.T) {
	server := NewServer(
		config.Config{MasterKey: "master-key", Models: []config.Model{{Name: "gateway-model", Model: "openai/gpt-5"}}},
		stubChatCompleter{},
	)
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gateway-model","messages":[]}`))
	request.Header.Set("Authorization", "Bearer master-key")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if body := response.Body.String(); body != `{"id":"chatcmpl-test"}` {
		t.Fatalf("body = %s", body)
	}
}

func TestCompletionsForwardsConfiguredDeployment(t *testing.T) {
	server := NewServer(
		config.Config{MasterKey: "master-key", Models: []config.Model{{Name: "gateway-model", Model: "openai/gpt-5"}}},
		mediaProvider{},
	)
	request := httptest.NewRequest(http.MethodPost, "/v1/completions", strings.NewReader(`{"model":"gateway-model","prompt":"hello"}`))
	request.Header.Set("Authorization", "Bearer master-key")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Body.String() != `{"choices":[{"text":"hello"}]}` {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestChatCompletionsRecordsUsage(t *testing.T) {
	recorder := &recordingUsageRecorder{}
	server := NewServerWithDependencies(
		config.Config{MasterKey: "master-key", Models: []config.Model{{Name: "gateway-model", Model: "openai/gpt-5"}}},
		usageChatCompleter{}, nil, recorder,
	)
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gateway-model","messages":[]}`))
	request.Header.Set("Authorization", "Bearer master-key")
	server.Handler().ServeHTTP(httptest.NewRecorder(), request)
	if len(recorder.records) != 1 || recorder.records[0].Usage.TotalTokens != 5 {
		t.Fatalf("records = %#v", recorder.records)
	}
}

func TestEmbeddingsRecordsUsage(t *testing.T) {
	recorder := &recordingUsageRecorder{}
	server := NewServerWithDependencies(
		config.Config{MasterKey: "master-key", Models: []config.Model{{Name: "embedding-model", Model: "openai/text-embedding"}}},
		usageEmbeddingCompleter{}, nil, recorder,
	)
	request := httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(`{"model":"embedding-model","input":"hello"}`))
	request.Header.Set("Authorization", "Bearer master-key")
	server.Handler().ServeHTTP(httptest.NewRecorder(), request)
	if len(recorder.records) != 1 || recorder.records[0].CallType != "embedding" || recorder.records[0].Usage.TotalTokens != 2 {
		t.Fatalf("records=%#v", recorder.records)
	}
}

func TestChatCompletionsReturnsCachedResponse(t *testing.T) {
	cache := &memoryResponseCache{values: map[string][]byte{}}
	server := NewServerWithRuntime(config.Config{MasterKey: "master-key", Models: []config.Model{{Name: "gateway-model", Model: "openai/gpt-5"}}}, usageChatCompleter{}, nil, nil, nil).WithResponseCache(cache)
	for requestNumber := 0; requestNumber < 2; requestNumber++ {
		request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gateway-model","messages":[]}`))
		request.Header.Set("Authorization", "Bearer master-key")
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("request %d status=%d", requestNumber, response.Code)
		}
		if requestNumber == 1 && response.Header().Get("X-LiteLLM-Cache") != "hit" {
			t.Fatalf("cache header = %q", response.Header().Get("X-LiteLLM-Cache"))
		}
	}
}

func TestChatCompletionsAppliesVirtualKeyRateLimit(t *testing.T) {
	limit := int64(1)
	server := NewServerWithRuntime(
		config.Config{Models: []config.Model{{Name: "gateway-model", Model: "openai/gpt-5"}}}, stubChatCompleter{}, limitedVirtualKeyValidator{limit: limit}, nil, denyingLimiter{},
	)
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gateway-model","messages":[]}`))
	request.Header.Set("Authorization", "Bearer sk-virtual-key")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusTooManyRequests)
	}
}

func TestChatCompletionsRoundRobinsConfiguredDeployments(t *testing.T) {
	server := NewServer(
		config.Config{MasterKey: "master-key", Models: []config.Model{
			{Name: "gateway-model", Model: "openai/gpt-5", APIBase: "https://one.example"},
			{Name: "gateway-model", Model: "openai/gpt-5", APIBase: "https://two.example"},
		}},
		deploymentCapturingCompleter{bases: make(chan string, 2)},
	)
	for range 2 {
		request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gateway-model","messages":[]}`))
		request.Header.Set("Authorization", "Bearer master-key")
		server.Handler().ServeHTTP(httptest.NewRecorder(), request)
	}
	completer := server.chatCompleter.(deploymentCapturingCompleter)
	if first, second := <-completer.bases, <-completer.bases; first != "https://one.example" || second != "https://two.example" {
		t.Fatalf("deployment bases = %q, %q, want round robin", first, second)
	}
}

func TestChatCompletionsFallsBackAfterProviderError(t *testing.T) {
	server := NewServer(config.Config{MasterKey: "master-key", Models: []config.Model{
		{Name: "gateway-model", Model: "openai/gpt-5", APIBase: "https://one.example"},
		{Name: "gateway-model", Model: "openai/gpt-5", APIBase: "https://two.example"},
	}}, fallbackChatCompleter{})
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gateway-model","messages":[]}`))
	request.Header.Set("Authorization", "Bearer master-key")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != `{"deployment":"two"}` {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestChatCompletionsFallsBackAfterProviderServerFailure(t *testing.T) {
	server := NewServer(config.Config{MasterKey: "master-key", Models: []config.Model{
		{Name: "gateway-model", Model: "openai/gpt-5", APIBase: "https://one.example"},
		{Name: "gateway-model", Model: "openai/gpt-5", APIBase: "https://two.example"},
	}}, fallbackServerFailureCompleter{})
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gateway-model","messages":[]}`))
	request.Header.Set("Authorization", "Bearer master-key")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != `{"deployment":"two"}` {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestEmbeddingsFallsBackAfterProviderError(t *testing.T) {
	server := NewServer(config.Config{MasterKey: "master-key", Models: []config.Model{
		{Name: "embedding-model", Model: "openai/text-embedding", APIBase: "https://one.example"},
		{Name: "embedding-model", Model: "openai/text-embedding", APIBase: "https://two.example"},
	}}, fallbackProvider{})
	request := httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(`{"model":"embedding-model","input":"hello"}`))
	request.Header.Set("Authorization", "Bearer master-key")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != `{"deployment":"two"}` {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestImagesForwardsOpenAICompatibleRequest(t *testing.T) {
	server := NewServer(
		config.Config{MasterKey: "master-key", Models: []config.Model{{Name: "image-model", Model: "openai/gpt-image-1"}}},
		mediaProvider{},
	)
	request := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"model":"image-model","prompt":"a lighthouse"}`))
	request.Header.Set("Authorization", "Bearer master-key")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Body.String() != `{"data":[{"url":"https://image.example/test"}]}` {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestSpeechForwardsOpenAICompatibleRequest(t *testing.T) {
	server := NewServer(
		config.Config{MasterKey: "master-key", Models: []config.Model{{Name: "speech-model", Model: "openai/gpt-4o-mini-tts"}}},
		mediaProvider{},
	)
	request := httptest.NewRequest(http.MethodPost, "/v1/audio/speech", strings.NewReader(`{"model":"speech-model","input":"hello","voice":"alloy"}`))
	request.Header.Set("Authorization", "Bearer master-key")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Body.String() != "audio-bytes" {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestModerationsForwardsOpenAICompatibleRequest(t *testing.T) {
	server := NewServer(
		config.Config{MasterKey: "master-key", Models: []config.Model{{Name: "moderation-model", Model: "openai/omni-moderation-latest"}}},
		mediaProvider{},
	)
	request := httptest.NewRequest(http.MethodPost, "/v1/moderations", strings.NewReader(`{"model":"moderation-model","input":"hello"}`))
	request.Header.Set("Authorization", "Bearer master-key")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Body.String() != `{"results":[{"flagged":false}]}` {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestRerankForwardsOpenAICompatibleRequest(t *testing.T) {
	server := NewServer(
		config.Config{MasterKey: "master-key", Models: []config.Model{{Name: "rerank-model", Model: "openai/rerank-test"}}},
		mediaProvider{},
	)
	request := httptest.NewRequest(http.MethodPost, "/v1/rerank", strings.NewReader(`{"model":"rerank-model","query":"hello","documents":["one"]}`))
	request.Header.Set("Authorization", "Bearer master-key")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Body.String() != `{"results":[{"index":0,"relevance_score":1}]}` {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestFilesAndBatchesUseConfiguredDefaultDeployment(t *testing.T) {
	provider := &resourceProvider{}
	server := NewServer(
		config.Config{MasterKey: "master-key", ResourceModel: "resource-model", Models: []config.Model{
			{Name: "chat-model", Model: "openai/gpt-5"},
			{Name: "resource-model", Model: "openai/gpt-4o-mini"},
		}},
		provider,
	)
	for _, testCase := range []struct {
		method   string
		path     string
		endpoint string
	}{
		{http.MethodPost, "/v1/files", "files"},
		{http.MethodGet, "/v1/files/file-123/content", "files/file-123/content"},
		{http.MethodPost, "/v1/batches/batch-123/cancel", "batches/batch-123/cancel"},
		{http.MethodPost, "/v1/fine_tuning/jobs", "fine_tuning/jobs"},
		{http.MethodGet, "/v1/fine_tuning/jobs/job-123", "fine_tuning/jobs/job-123"},
		{http.MethodPost, "/v1/fine_tuning/jobs/job-123/cancel", "fine_tuning/jobs/job-123/cancel"},
		{http.MethodGet, "/v1/fine_tuning/jobs/job-123/events", "fine_tuning/jobs/job-123/events"},
		{http.MethodGet, "/v1/fine_tuning/jobs/job-123/checkpoints", "fine_tuning/jobs/job-123/checkpoints"},
		{http.MethodPost, "/v1/containers", "containers"},
		{http.MethodGet, "/v1/containers/container-123", "containers/container-123"},
		{http.MethodDelete, "/v1/containers/container-123", "containers/container-123"},
		{http.MethodPost, "/v1/containers/container-123/files", "containers/container-123/files"},
		{http.MethodGet, "/v1/containers/container-123/files/file-123", "containers/container-123/files/file-123"},
		{http.MethodDelete, "/v1/containers/container-123/files/file-123", "containers/container-123/files/file-123"},
		{http.MethodGet, "/v1/containers/container-123/files/file-123/content", "containers/container-123/files/file-123/content"},
		{http.MethodPost, "/v1/assistants", "assistants"},
		{http.MethodGet, "/v1/assistants/asst-123", "assistants/asst-123"},
		{http.MethodDelete, "/v1/assistants/asst-123", "assistants/asst-123"},
		{http.MethodPost, "/v1/threads", "threads"},
		{http.MethodPost, "/v1/threads/runs", "threads/runs"},
		{http.MethodGet, "/v1/threads/thread-123", "threads/thread-123"},
		{http.MethodPost, "/v1/threads/thread-123/messages", "threads/thread-123/messages"},
		{http.MethodGet, "/v1/threads/thread-123/messages/message-123", "threads/thread-123/messages/message-123"},
		{http.MethodPost, "/v1/threads/thread-123/runs", "threads/thread-123/runs"},
		{http.MethodPost, "/v1/threads/thread-123/runs/run-123/cancel", "threads/thread-123/runs/run-123/cancel"},
		{http.MethodPost, "/v1/threads/thread-123/runs/run-123/submit_tool_outputs", "threads/thread-123/runs/run-123/submit_tool_outputs"},
		{http.MethodGet, "/v1/threads/thread-123/runs/run-123/steps", "threads/thread-123/runs/run-123/steps"},
		{http.MethodGet, "/v1/threads/thread-123/runs/run-123/steps/step-123", "threads/thread-123/runs/run-123/steps/step-123"},
		{http.MethodPost, "/v1/audio/transcriptions", "audio/transcriptions"},
		{http.MethodPost, "/v1/audio/translations", "audio/translations"},
		{http.MethodPost, "/v1/images/edits", "images/edits"},
		{http.MethodPost, "/v1/vector_stores", "vector_stores"},
		{http.MethodPost, "/v1/vector_stores/vs-123/search", "vector_stores/vs-123/search"},
		{http.MethodPost, "/v1/vector_stores/vs-123/files", "vector_stores/vs-123/files"},
		{http.MethodGet, "/v1/vector_stores/vs-123/files/file-123/content", "vector_stores/vs-123/files/file-123/content"},
		{http.MethodPost, "/v1/vector_stores/vs-123/file_batches/batch-123/cancel", "vector_stores/vs-123/file_batches/batch-123/cancel"},
	} {
		request := httptest.NewRequest(testCase.method, testCase.path, strings.NewReader(`{"test":true}`))
		request.Header.Set("Authorization", "Bearer master-key")
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusOK || provider.endpoint != testCase.endpoint || provider.deployment.Name != "resource-model" {
			t.Fatalf("%s %s: status=%d endpoint=%q deployment=%q", testCase.method, testCase.path, response.Code, provider.endpoint, provider.deployment.Name)
		}
	}
}

type stubChatCompleter struct{}

type memoryKeyManager struct {
	records map[string]auth.ManagedVirtualKey
}

type memoryTeamManager struct {
	teams map[string]auth.ManagedTeam
}

type memoryUserManager struct {
	users map[string]auth.ManagedUser
}

type memoryProjectManager struct {
	projects map[string]auth.ManagedProject
}

type memoryOrganizationManager struct {
	organizations map[string]auth.ManagedOrganization
}

type memoryBudgetManager struct{ budgets map[string]auth.ManagedBudget }

func (manager *memoryBudgetManager) CreateBudget(_ context.Context, budget auth.ManagedBudget) error {
	manager.budgets[budget.BudgetID] = budget
	return nil
}
func (manager *memoryBudgetManager) GetBudget(_ context.Context, budgetID string) (auth.ManagedBudget, error) {
	budget, found := manager.budgets[budgetID]
	if !found {
		return auth.ManagedBudget{}, auth.ErrInvalidVirtualKey
	}
	return budget, nil
}
func (manager *memoryBudgetManager) ListBudgets(_ context.Context, _ int) ([]auth.ManagedBudget, error) {
	budgets := make([]auth.ManagedBudget, 0, len(manager.budgets))
	for _, budget := range manager.budgets {
		budgets = append(budgets, budget)
	}
	return budgets, nil
}
func (manager *memoryBudgetManager) UpdateBudget(_ context.Context, budgetID string, update auth.ManagedBudgetUpdate) (bool, error) {
	budget, found := manager.budgets[budgetID]
	if !found {
		return false, nil
	}
	if update.MaxBudget != nil {
		budget.MaxBudget = update.MaxBudget
	}
	if update.SoftBudget != nil {
		budget.SoftBudget = update.SoftBudget
	}
	if update.MaxParallelRequests != nil {
		budget.MaxParallelRequests = update.MaxParallelRequests
	}
	if update.TPMLimit != nil {
		budget.TPMLimit = update.TPMLimit
	}
	if update.RPMLimit != nil {
		budget.RPMLimit = update.RPMLimit
	}
	if update.BudgetDuration != nil {
		budget.BudgetDuration = *update.BudgetDuration
	}
	if update.BudgetResetAt != nil {
		budget.BudgetResetAt = update.BudgetResetAt
	}
	manager.budgets[budgetID] = budget
	return true, nil
}
func (manager *memoryBudgetManager) DeleteBudget(_ context.Context, budgetID string) (bool, error) {
	if _, found := manager.budgets[budgetID]; !found {
		return false, nil
	}
	delete(manager.budgets, budgetID)
	return true, nil
}

func (manager *memoryOrganizationManager) CreateOrganization(_ context.Context, organization auth.ManagedOrganization) error {
	manager.organizations[organization.OrganizationID] = organization
	return nil
}

func (manager *memoryOrganizationManager) GetOrganization(_ context.Context, organizationID string) (auth.ManagedOrganization, error) {
	organization, found := manager.organizations[organizationID]
	if !found {
		return auth.ManagedOrganization{}, auth.ErrInvalidVirtualKey
	}
	return organization, nil
}

func (manager *memoryOrganizationManager) ListOrganizations(_ context.Context, _ int) ([]auth.ManagedOrganization, error) {
	organizations := make([]auth.ManagedOrganization, 0, len(manager.organizations))
	for _, organization := range manager.organizations {
		organizations = append(organizations, organization)
	}
	return organizations, nil
}

func (manager *memoryOrganizationManager) UpdateOrganization(_ context.Context, organizationID string, update auth.ManagedOrganizationUpdate) (bool, error) {
	organization, found := manager.organizations[organizationID]
	if !found {
		return false, nil
	}
	if update.OrganizationAlias != nil {
		organization.OrganizationAlias = *update.OrganizationAlias
	}
	if update.BudgetID != nil {
		organization.BudgetID = *update.BudgetID
	}
	if update.Models != nil {
		organization.Models = *update.Models
	}
	if update.Blocked != nil {
		organization.Blocked = *update.Blocked
	}
	manager.organizations[organizationID] = organization
	return true, nil
}

func (manager *memoryOrganizationManager) DeleteOrganization(_ context.Context, organizationID string) (bool, error) {
	if _, found := manager.organizations[organizationID]; !found {
		return false, nil
	}
	delete(manager.organizations, organizationID)
	return true, nil
}

func (manager *memoryProjectManager) CreateProject(_ context.Context, project auth.ManagedProject) error {
	manager.projects[project.ProjectID] = project
	return nil
}
func (manager *memoryProjectManager) GetProject(_ context.Context, projectID string) (auth.ManagedProject, error) {
	project, found := manager.projects[projectID]
	if !found {
		return auth.ManagedProject{}, auth.ErrInvalidVirtualKey
	}
	return project, nil
}
func (manager *memoryProjectManager) UpdateProject(_ context.Context, projectID string, update auth.ManagedProjectUpdate) (bool, error) {
	project, found := manager.projects[projectID]
	if !found {
		return false, nil
	}
	if update.ProjectAlias != nil {
		project.ProjectAlias = *update.ProjectAlias
	}
	if update.Description != nil {
		project.Description = *update.Description
	}
	if update.TeamID != nil {
		project.TeamID = *update.TeamID
	}
	if update.BudgetID != nil {
		project.BudgetID = *update.BudgetID
	}
	if update.Models != nil {
		project.Models = *update.Models
	}
	if update.Blocked != nil {
		project.Blocked = *update.Blocked
	}
	manager.projects[projectID] = project
	return true, nil
}
func (manager *memoryProjectManager) ListProjects(_ context.Context, _ int) ([]auth.ManagedProject, error) {
	projects := make([]auth.ManagedProject, 0, len(manager.projects))
	for _, project := range manager.projects {
		projects = append(projects, project)
	}
	return projects, nil
}
func (manager *memoryProjectManager) SetProjectBlocked(_ context.Context, projectID string, blocked bool) (bool, error) {
	project, found := manager.projects[projectID]
	if !found {
		return false, nil
	}
	project.Blocked = blocked
	manager.projects[projectID] = project
	return true, nil
}
func (manager *memoryProjectManager) DeleteProject(_ context.Context, projectID string) (bool, error) {
	if _, found := manager.projects[projectID]; !found {
		return false, nil
	}
	delete(manager.projects, projectID)
	return true, nil
}

func (manager *memoryUserManager) CreateUser(_ context.Context, user auth.ManagedUser) error {
	manager.users[user.UserID] = user
	return nil
}
func (manager *memoryUserManager) GetUser(_ context.Context, userID string) (auth.ManagedUser, error) {
	user, found := manager.users[userID]
	if !found {
		return auth.ManagedUser{}, auth.ErrInvalidVirtualKey
	}
	return user, nil
}
func (manager *memoryUserManager) UpdateUser(_ context.Context, userID string, update auth.ManagedUserUpdate) (bool, error) {
	user, found := manager.users[userID]
	if !found {
		return false, nil
	}
	if update.UserAlias != nil {
		user.UserAlias = *update.UserAlias
	}
	if update.TeamID != nil {
		user.TeamID = *update.TeamID
	}
	if update.UserEmail != nil {
		user.UserEmail = *update.UserEmail
	}
	if update.Models != nil {
		user.Models = *update.Models
	}
	if update.Blocked != nil {
		user.Blocked = *update.Blocked
	}
	manager.users[userID] = user
	return true, nil
}
func (manager *memoryUserManager) ListUsers(_ context.Context, _ int) ([]auth.ManagedUser, error) {
	users := make([]auth.ManagedUser, 0, len(manager.users))
	for _, user := range manager.users {
		users = append(users, user)
	}
	return users, nil
}
func (manager *memoryUserManager) SetUserBlocked(_ context.Context, userID string, blocked bool) (bool, error) {
	user, found := manager.users[userID]
	if !found {
		return false, nil
	}
	user.Blocked = blocked
	manager.users[userID] = user
	return true, nil
}
func (manager *memoryUserManager) DeleteUser(_ context.Context, userID string) (bool, error) {
	if _, found := manager.users[userID]; !found {
		return false, nil
	}
	delete(manager.users, userID)
	return true, nil
}

func (manager *memoryTeamManager) CreateTeam(_ context.Context, team auth.ManagedTeam) error {
	manager.teams[team.TeamID] = team
	return nil
}
func (manager *memoryTeamManager) GetTeam(_ context.Context, teamID string) (auth.ManagedTeam, error) {
	team, found := manager.teams[teamID]
	if !found {
		return auth.ManagedTeam{}, auth.ErrInvalidVirtualKey
	}
	return team, nil
}
func (manager *memoryTeamManager) UpdateTeam(_ context.Context, teamID string, update auth.ManagedTeamUpdate) (bool, error) {
	team, found := manager.teams[teamID]
	if !found {
		return false, nil
	}
	if update.TeamAlias != nil {
		team.TeamAlias = *update.TeamAlias
	}
	if update.Admins != nil {
		team.Admins = *update.Admins
	}
	if update.Members != nil {
		team.Members = *update.Members
	}
	if update.Models != nil {
		team.Models = *update.Models
	}
	if update.Blocked != nil {
		team.Blocked = *update.Blocked
	}
	manager.teams[teamID] = team
	return true, nil
}
func (manager *memoryTeamManager) ListTeams(_ context.Context, _ int) ([]auth.ManagedTeam, error) {
	teams := make([]auth.ManagedTeam, 0, len(manager.teams))
	for _, team := range manager.teams {
		teams = append(teams, team)
	}
	return teams, nil
}
func (manager *memoryTeamManager) SetTeamBlocked(_ context.Context, teamID string, blocked bool) (bool, error) {
	team, found := manager.teams[teamID]
	if !found {
		return false, nil
	}
	team.Blocked = blocked
	manager.teams[teamID] = team
	return true, nil
}
func (manager *memoryTeamManager) DeleteTeam(_ context.Context, teamID string) (bool, error) {
	if _, found := manager.teams[teamID]; !found {
		return false, nil
	}
	delete(manager.teams, teamID)
	return true, nil
}

func (manager *memoryKeyManager) CreateVirtualKey(_ context.Context, record auth.ManagedVirtualKey) error {
	manager.records[record.TokenHash] = record
	return nil
}
func (manager *memoryKeyManager) GetVirtualKey(_ context.Context, tokenHash string) (auth.ManagedVirtualKey, error) {
	record, found := manager.records[tokenHash]
	if !found {
		return auth.ManagedVirtualKey{}, auth.ErrInvalidVirtualKey
	}
	return record, nil
}
func (manager *memoryKeyManager) DeleteVirtualKey(_ context.Context, tokenHash string) (bool, error) {
	if _, found := manager.records[tokenHash]; !found {
		return false, nil
	}
	delete(manager.records, tokenHash)
	return true, nil
}
func (manager *memoryKeyManager) SetVirtualKeyBlocked(_ context.Context, tokenHash string, blocked bool) (bool, error) {
	record, found := manager.records[tokenHash]
	if !found {
		return false, nil
	}
	record.Blocked = blocked
	manager.records[tokenHash] = record
	return true, nil
}
func (manager *memoryKeyManager) UpdateVirtualKey(_ context.Context, tokenHash string, update auth.ManagedVirtualKeyUpdate) (bool, error) {
	record, found := manager.records[tokenHash]
	if !found {
		return false, nil
	}
	if update.KeyAlias != nil {
		record.KeyAlias = *update.KeyAlias
	}
	if update.Models != nil {
		record.Models = *update.Models
	}
	if update.ExpiresAt != nil {
		record.ExpiresAt = update.ExpiresAt
	}
	if update.RPMLimit != nil {
		record.RPMLimit = update.RPMLimit
	}
	manager.records[tokenHash] = record
	return true, nil
}
func (manager *memoryKeyManager) ListVirtualKeys(_ context.Context, _ int) ([]auth.ManagedVirtualKey, error) {
	keys := make([]auth.ManagedVirtualKey, 0, len(manager.records))
	for _, key := range manager.records {
		keys = append(keys, key)
	}
	return keys, nil
}
func (manager *memoryKeyManager) RegenerateVirtualKey(_ context.Context, oldTokenHash, newTokenHash string) (auth.ManagedVirtualKey, error) {
	record, found := manager.records[oldTokenHash]
	if !found {
		return auth.ManagedVirtualKey{}, auth.ErrInvalidVirtualKey
	}
	delete(manager.records, oldTokenHash)
	record.TokenHash = newTokenHash
	manager.records[newTokenHash] = record
	return record, nil
}

type deploymentCapturingCompleter struct {
	bases chan string
}

type fallbackChatCompleter struct{}

type fallbackServerFailureCompleter struct{}

func (fallbackChatCompleter) ChatCompletion(_ context.Context, deployment config.Model, _ []byte) (providers.Response, error) {
	if deployment.APIBase == "https://one.example" {
		return providers.Response{}, errors.New("first deployment failed")
	}
	return providers.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"deployment":"two"}`))}, nil
}

func (fallbackServerFailureCompleter) ChatCompletion(_ context.Context, deployment config.Model, _ []byte) (providers.Response, error) {
	if deployment.APIBase == "https://one.example" {
		return providers.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader(`{"error":"unavailable"}`))}, nil
	}
	return providers.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"deployment":"two"}`))}, nil
}

type fallbackProvider struct{}

func (fallbackProvider) ChatCompletion(context.Context, config.Model, []byte) (providers.Response, error) {
	return providers.Response{}, errors.New("not used")
}
func (fallbackProvider) CreateResponse(context.Context, config.Model, []byte) (providers.Response, error) {
	return providers.Response{}, errors.New("not used")
}
func (fallbackProvider) CreateEmbedding(_ context.Context, deployment config.Model, _ []byte) (providers.Response, error) {
	if deployment.APIBase == "https://one.example" {
		return providers.Response{}, errors.New("first deployment failed")
	}
	return providers.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"deployment":"two"}`))}, nil
}

type mediaProvider struct{}

func (mediaProvider) ChatCompletion(context.Context, config.Model, []byte) (providers.Response, error) {
	return providers.Response{}, errors.New("not used")
}
func (mediaProvider) TextCompletion(context.Context, config.Model, []byte) (providers.Response, error) {
	return providers.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"choices":[{"text":"hello"}]}`))}, nil
}
func (mediaProvider) CreateResponse(context.Context, config.Model, []byte) (providers.Response, error) {
	return providers.Response{}, errors.New("not used")
}
func (mediaProvider) CreateEmbedding(context.Context, config.Model, []byte) (providers.Response, error) {
	return providers.Response{}, errors.New("not used")
}
func (mediaProvider) GenerateImage(context.Context, config.Model, []byte) (providers.Response, error) {
	return providers.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"data":[{"url":"https://image.example/test"}]}`))}, nil
}
func (mediaProvider) CreateSpeech(context.Context, config.Model, []byte) (providers.Response, error) {
	return providers.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("audio-bytes"))}, nil
}
func (mediaProvider) Moderate(context.Context, config.Model, []byte) (providers.Response, error) {
	return providers.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"results":[{"flagged":false}]}`))}, nil
}
func (mediaProvider) Rerank(context.Context, config.Model, []byte) (providers.Response, error) {
	return providers.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"results":[{"index":0,"relevance_score":1}]}`))}, nil
}

type resourceProvider struct {
	endpoint   string
	deployment config.Model
}

func (*resourceProvider) ChatCompletion(context.Context, config.Model, []byte) (providers.Response, error) {
	return providers.Response{}, errors.New("not used")
}
func (*resourceProvider) CreateResponse(context.Context, config.Model, []byte) (providers.Response, error) {
	return providers.Response{}, errors.New("not used")
}
func (*resourceProvider) CreateEmbedding(context.Context, config.Model, []byte) (providers.Response, error) {
	return providers.Response{}, errors.New("not used")
}
func (provider *resourceProvider) Passthrough(_ context.Context, deployment config.Model, _ string, endpoint, _ string, _ []byte) (providers.Response, error) {
	provider.endpoint = endpoint
	provider.deployment = deployment
	return providers.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true}`))}, nil
}

type usageChatCompleter struct{}

func (usageChatCompleter) ChatCompletion(context.Context, config.Model, []byte) (providers.Response, error) {
	return providers.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`))}, nil
}

type usageEmbeddingCompleter struct{}

func (usageEmbeddingCompleter) ChatCompletion(context.Context, config.Model, []byte) (providers.Response, error) {
	return providers.Response{}, errors.New("not used")
}
func (usageEmbeddingCompleter) CreateResponse(context.Context, config.Model, []byte) (providers.Response, error) {
	return providers.Response{}, errors.New("not used")
}
func (usageEmbeddingCompleter) CreateEmbedding(context.Context, config.Model, []byte) (providers.Response, error) {
	return providers.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"usage":{"prompt_tokens":2,"total_tokens":2}}`))}, nil
}

type recordingUsageRecorder struct{ records []usage.Record }

func (recorder *recordingUsageRecorder) Insert(_ context.Context, record usage.Record) error {
	recorder.records = append(recorder.records, record)
	return nil
}

type memorySpendLogReader struct{ logs []usage.Log }

func (reader memorySpendLogReader) List(context.Context, int) ([]usage.Log, error) {
	return reader.logs, nil
}

type limitedVirtualKeyValidator struct{ limit int64 }

func (validator limitedVirtualKeyValidator) Validate(_ context.Context, _ string, _ string) (auth.VirtualKey, error) {
	return auth.VirtualKey{TokenHash: "hash", RPMLimit: &validator.limit}, nil
}

type denyingLimiter struct{}

func (denyingLimiter) Allow(context.Context, string, int64, time.Duration) (bool, error) {
	return false, nil
}

type failingReadinessCheck struct{}

func (failingReadinessCheck) Ping(context.Context) error { return errors.New("unavailable") }

type memoryResponseCache struct{ values map[string][]byte }

func (cache *memoryResponseCache) Get(_ context.Context, key string) ([]byte, error) {
	value, found := cache.values[key]
	if !found {
		return nil, errors.New("cache miss")
	}
	return value, nil
}
func (cache *memoryResponseCache) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	cache.values[key] = append([]byte(nil), value...)
	return nil
}

func (completer deploymentCapturingCompleter) ChatCompletion(_ context.Context, deployment config.Model, _ []byte) (providers.Response, error) {
	completer.bases <- deployment.APIBase
	return providers.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
}

type stubVirtualKeyValidator struct{}

type scopedModelsValidator struct{}

func (scopedModelsValidator) Validate(_ context.Context, _ string, model string) (auth.VirtualKey, error) {
	key := auth.VirtualKey{Models: []string{"shared", "key-only"}, UserModels: []string{"shared", "key-only"}, TeamModels: []string{"shared", "key-only"}, ProjectModels: []string{"shared"}, OrganizationModels: []string{"shared"}}
	if model != "" && !auth.AllowsModel(key, model) {
		return auth.VirtualKey{}, auth.ErrInvalidVirtualKey
	}
	return key, nil
}

func (stubVirtualKeyValidator) Validate(_ context.Context, rawKey string, model string) (auth.VirtualKey, error) {
	if rawKey != "sk-virtual-key" || (model != "" && model != "gateway-model") {
		return auth.VirtualKey{}, auth.ErrInvalidVirtualKey
	}
	return auth.VirtualKey{Models: []string{"gateway-model"}}, nil
}

func (stubChatCompleter) ChatCompletion(_ context.Context, deployment config.Model, _ []byte) (providers.Response, error) {
	if deployment.Name != "gateway-model" {
		return providers.Response{}, nil
	}
	return providers.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"chatcmpl-test"}`)),
	}, nil
}
