package postgres

import (
	"context"
	"os"
	"testing"

	"github.com/BerriAI/litellm/go-proxy/internal/auth"
	"github.com/BerriAI/litellm/go-proxy/internal/localdev"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOrganizationStoreLifecycle(t *testing.T) {
	if os.Getenv("LITELLM_RUN_EMBEDDED_TESTS") != "1" {
		t.Skip("set LITELLM_RUN_EMBEDDED_TESTS=1 to run embedded PostgreSQL integration tests")
	}
	dependencies, err := localdev.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer dependencies.Close()
	pool, err := pgxpool.New(context.Background(), dependencies.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := EnsureCoreSchema(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	store := NewOrganizationStore(pool)
	organization := auth.ManagedOrganization{OrganizationID: "org-integration", OrganizationAlias: "Integration", BudgetID: "budget-test", Models: []string{"gateway-model"}}
	if err := store.CreateOrganization(context.Background(), organization); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetOrganization(context.Background(), organization.OrganizationID)
	if err != nil || loaded.OrganizationAlias != organization.OrganizationAlias || len(loaded.Models) != 1 {
		t.Fatalf("organization=%#v err=%v", loaded, err)
	}
	alias := "Updated"
	updated, err := store.UpdateOrganization(context.Background(), organization.OrganizationID, auth.ManagedOrganizationUpdate{OrganizationAlias: &alias})
	if err != nil || !updated {
		t.Fatalf("updated=%t err=%v", updated, err)
	}
	organizations, err := store.ListOrganizations(context.Background(), 10)
	if err != nil || len(organizations) != 1 || organizations[0].OrganizationAlias != alias {
		t.Fatalf("organizations=%#v err=%v", organizations, err)
	}
	deleted, err := store.DeleteOrganization(context.Background(), organization.OrganizationID)
	if err != nil || !deleted {
		t.Fatalf("deleted=%t err=%v", deleted, err)
	}
}
