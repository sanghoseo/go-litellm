package postgres

import (
	"context"
	"fmt"

	"github.com/BerriAI/litellm/go-proxy/internal/usage"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SpendLogStore struct{ pool *pgxpool.Pool }

func NewSpendLogStore(pool *pgxpool.Pool) SpendLogStore { return SpendLogStore{pool: pool} }

func (store SpendLogStore) Insert(ctx context.Context, record usage.Record) error {
	if store.pool == nil {
		return fmt.Errorf("PostgreSQL pool is required")
	}
	_, err := store.pool.Exec(ctx, `
INSERT INTO "LiteLLM_SpendLogs" (
"request_id", "call_type", "api_key", "total_tokens", "prompt_tokens", "completion_tokens",
"startTime", "endTime", "request_duration_ms", "model", "custom_llm_provider", "api_base", "status")
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
ON CONFLICT ("request_id") DO NOTHING`,
		record.RequestID, record.CallType, record.APIKeyHash, record.Usage.TotalTokens, record.Usage.PromptTokens, record.Usage.CompletionTokens,
		record.StartedAt, record.CompletedAt, int(record.CompletedAt.Sub(record.StartedAt).Milliseconds()), record.Model, record.Provider, record.APIBase, record.Status)
	if err != nil {
		return fmt.Errorf("insert spend log: %w", err)
	}
	return nil
}
