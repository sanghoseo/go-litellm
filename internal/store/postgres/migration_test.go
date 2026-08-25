package postgres

import (
	"context"
	"os"
	"testing"

	"github.com/BerriAI/litellm/go-proxy/internal/localdev"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestEnsureCoreSchema(t *testing.T) {
	if os.Getenv("LITELLM_RUN_EMBEDDED_TESTS") != "1" {
		t.Skip("set LITELLM_RUN_EMBEDDED_TESTS=1 to run embedded PostgreSQL integration tests")
	}
	dependencies, err := localdev.Start()
	if err != nil {
		t.Fatalf("start local dependencies: %v", err)
	}
	defer dependencies.Close()
	pool, err := pgxpool.New(context.Background(), dependencies.DatabaseURL)
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	defer pool.Close()
	if err := EnsureCoreSchema(context.Background(), pool); err != nil {
		t.Fatalf("EnsureCoreSchema() error = %v", err)
	}
	for _, table := range []string{"LiteLLM_VerificationToken", "LiteLLM_UserTable", "LiteLLM_OrganizationTable", "LiteLLM_TeamTable", "LiteLLM_SpendLogs"} {
		var found bool
		if err := pool.QueryRow(context.Background(), "SELECT to_regclass($1) IS NOT NULL", `public."`+table+`"`).Scan(&found); err != nil || !found {
			t.Fatalf("table %s was not created: found=%t err=%v", table, found, err)
		}
	}
}

func TestReplicaIdentityFullEnabled(t *testing.T) {
	for _, value := range []string{"true", "1", "T", "y", "YES"} {
		if !ReplicaIdentityFullEnabled(value) {
			t.Errorf("ReplicaIdentityFullEnabled(%q) = false, want true", value)
		}
	}
	for _, value := range []string{"", "false", "0", "on", "enabled"} {
		if ReplicaIdentityFullEnabled(value) {
			t.Errorf("ReplicaIdentityFullEnabled(%q) = true, want false", value)
		}
	}
}

func TestApplyReplicaIdentityFull(t *testing.T) {
	if os.Getenv("LITELLM_RUN_EMBEDDED_TESTS") != "1" {
		t.Skip("set LITELLM_RUN_EMBEDDED_TESTS=1 to run embedded PostgreSQL integration tests")
	}
	dependencies, err := localdev.Start()
	if err != nil {
		t.Fatalf("start local dependencies: %v", err)
	}
	defer dependencies.Close()
	pool, err := pgxpool.New(context.Background(), dependencies.DatabaseURL)
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	defer pool.Close()
	if err := EnsureCoreSchema(context.Background(), pool); err != nil {
		t.Fatalf("EnsureCoreSchema() error = %v", err)
	}
	if err := ApplyReplicaIdentityFull(context.Background(), pool); err != nil {
		t.Fatalf("ApplyReplicaIdentityFull() error = %v", err)
	}
	var identity string
	if err := pool.QueryRow(context.Background(), `SELECT relreplident::text FROM pg_class WHERE oid = '"LiteLLM_TeamTable"'::regclass`).Scan(&identity); err != nil {
		t.Fatalf("query replica identity: %v", err)
	}
	if identity != "f" {
		t.Fatalf("replica identity = %q, want f", identity)
	}
}
