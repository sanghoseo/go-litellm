package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

func EnsureCoreSchema(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("PostgreSQL pool is required")
	}
	_, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS "LiteLLM_VerificationToken" ("token" TEXT PRIMARY KEY, "expires" TIMESTAMP(3), "models" TEXT[] DEFAULT ARRAY[]::TEXT[], "blocked" BOOLEAN)`)
	if err != nil {
		return fmt.Errorf("create LiteLLM_VerificationToken: %w", err)
	}
	return nil
}
