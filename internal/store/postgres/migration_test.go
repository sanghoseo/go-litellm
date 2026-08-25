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
	for _, table := range []string{"LiteLLM_VerificationToken", "LiteLLM_UserTable", "LiteLLM_TeamTable", "LiteLLM_SpendLogs"} {
		var found bool
		if err := pool.QueryRow(context.Background(), "SELECT to_regclass($1) IS NOT NULL", `public."`+table+`"`).Scan(&found); err != nil || !found {
			t.Fatalf("table %s was not created: found=%t err=%v", table, found, err)
		}
	}
}
