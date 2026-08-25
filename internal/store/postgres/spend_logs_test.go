package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/BerriAI/litellm/go-proxy/internal/localdev"
	"github.com/BerriAI/litellm/go-proxy/internal/usage"
	proxytpes "github.com/BerriAI/litellm/go-proxy/pkg/types"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSpendLogStoreInsertIsIdempotent(t *testing.T) {
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
	now := time.Now().UTC()
	record := usage.Record{RequestID: "request-1", CallType: "completion", Model: "gpt-test", StartedAt: now, CompletedAt: now.Add(time.Millisecond), Usage: proxytpes.Usage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5}, Status: "success"}
	store := NewSpendLogStore(pool)
	if err := store.Insert(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if err := store.Insert(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	var count, tokens int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*), MAX("total_tokens") FROM "LiteLLM_SpendLogs"`).Scan(&count, &tokens); err != nil {
		t.Fatal(err)
	}
	if count != 1 || tokens != 5 {
		t.Fatalf("count=%d tokens=%d", count, tokens)
	}
	logs, err := store.List(context.Background(), 10)
	if err != nil || len(logs) != 1 || logs[0].RequestID != "request-1" {
		t.Fatalf("logs=%#v err=%v", logs, err)
	}
}
