package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

func EnsureCoreSchema(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("PostgreSQL pool is required")
	}
	for _, name := range coreSchemaOrder {
		statement := coreSchemaStatements[name]
		if _, err := pool.Exec(ctx, statement); err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}
	}
	return nil
}

// ReplicaIdentityFullEnabled reports whether migrations should configure every
// LiteLLM table for PostgreSQL logical replication. It intentionally accepts
// the same common truthy values as the legacy proxy extras package.
func ReplicaIdentityFullEnabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "t", "y", "yes":
		return true
	default:
		return false
	}
}

// ApplyReplicaIdentityFull configures all LiteLLM tables in the active schema
// for logical replication consumers. Callers should treat a failure as an
// operational warning: normal proxy traffic does not require this setting.
func ApplyReplicaIdentityFull(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("PostgreSQL pool is required")
	}
	_, err := pool.Exec(ctx, `DO $$
DECLARE
    target regclass;
BEGIN
    SET LOCAL lock_timeout = '5s';
    FOR target IN
        SELECT c.oid::regclass
        FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE c.relkind = 'r'
          AND c.relreplident <> 'f'
          AND n.nspname = ANY (current_schemas(false))
          AND c.relname LIKE 'LiteLLM\_%'
    LOOP
        BEGIN
            EXECUTE format('ALTER TABLE %s REPLICA IDENTITY FULL', target);
        EXCEPTION WHEN lock_not_available THEN
            RAISE WARNING 'REPLICA IDENTITY FULL skipped for %: table busy, retrying next run', target;
        END;
    END LOOP;
END
$$`)
	if err != nil {
		return fmt.Errorf("apply replica identity full: %w", err)
	}
	return nil
}

var coreSchemaStatements = map[string]string{
	"budget table": `CREATE TABLE IF NOT EXISTS "LiteLLM_BudgetTable" (
"budget_id" TEXT PRIMARY KEY, "max_budget" DOUBLE PRECISION, "soft_budget" DOUBLE PRECISION,
"max_parallel_requests" INTEGER, "tpm_limit" BIGINT, "rpm_limit" BIGINT, "model_max_budget" JSONB,
"budget_duration" TEXT, "budget_reset_at" TIMESTAMP(3), "allowed_models" TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
"created_at" TIMESTAMP(3) NOT NULL DEFAULT NOW(), "created_by" TEXT NOT NULL DEFAULT '',
"updated_at" TIMESTAMP(3) NOT NULL DEFAULT NOW(), "updated_by" TEXT NOT NULL DEFAULT '')`,
	"user table": `CREATE TABLE IF NOT EXISTS "LiteLLM_UserTable" (
"user_id" TEXT PRIMARY KEY, "user_alias" TEXT, "team_id" TEXT, "organization_id" TEXT, "user_email" TEXT,
"user_role" TEXT, "password" TEXT, "teams" TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[], "models" TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
"metadata" JSONB NOT NULL DEFAULT '{}'::JSONB, "spend" DOUBLE PRECISION NOT NULL DEFAULT 0, "max_budget" DOUBLE PRECISION,
"max_parallel_requests" INTEGER, "tpm_limit" BIGINT, "rpm_limit" BIGINT, "blocked" BOOLEAN NOT NULL DEFAULT FALSE,
"created_at" TIMESTAMP(3) DEFAULT NOW(), "updated_at" TIMESTAMP(3) DEFAULT NOW())`,
	"organization table": `CREATE TABLE IF NOT EXISTS "LiteLLM_OrganizationTable" (
"organization_id" TEXT PRIMARY KEY, "organization_alias" TEXT NOT NULL DEFAULT '', "budget_id" TEXT,
"models" TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[], "metadata" JSONB NOT NULL DEFAULT '{}'::JSONB,
"spend" DOUBLE PRECISION NOT NULL DEFAULT 0, "blocked" BOOLEAN NOT NULL DEFAULT FALSE,
"created_at" TIMESTAMP(3) NOT NULL DEFAULT NOW(), "created_by" TEXT NOT NULL DEFAULT '',
"updated_at" TIMESTAMP(3) NOT NULL DEFAULT NOW(), "updated_by" TEXT NOT NULL DEFAULT '' )`,
	"team table": `CREATE TABLE IF NOT EXISTS "LiteLLM_TeamTable" (
"team_id" TEXT PRIMARY KEY, "team_alias" TEXT, "organization_id" TEXT, "admins" TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
"members" TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[], "models" TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
"metadata" JSONB NOT NULL DEFAULT '{}'::JSONB, "spend" DOUBLE PRECISION NOT NULL DEFAULT 0, "max_budget" DOUBLE PRECISION,
"max_parallel_requests" INTEGER, "tpm_limit" BIGINT, "rpm_limit" BIGINT, "blocked" BOOLEAN NOT NULL DEFAULT FALSE,
"created_at" TIMESTAMP(3) NOT NULL DEFAULT NOW(), "updated_at" TIMESTAMP(3) NOT NULL DEFAULT NOW())`,
	"project table": `CREATE TABLE IF NOT EXISTS "LiteLLM_ProjectTable" (
"project_id" TEXT PRIMARY KEY, "project_alias" TEXT, "description" TEXT, "team_id" TEXT, "budget_id" TEXT,
"models" TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[], "metadata" JSONB NOT NULL DEFAULT '{}'::JSONB,
"spend" DOUBLE PRECISION NOT NULL DEFAULT 0, "blocked" BOOLEAN NOT NULL DEFAULT FALSE,
"created_at" TIMESTAMP(3) NOT NULL DEFAULT NOW(), "created_by" TEXT NOT NULL DEFAULT '',
"updated_at" TIMESTAMP(3) NOT NULL DEFAULT NOW(), "updated_by" TEXT NOT NULL DEFAULT '')`,
	"verification token table": `CREATE TABLE IF NOT EXISTS "LiteLLM_VerificationToken" (
"token" TEXT PRIMARY KEY, "key_name" TEXT, "key_alias" TEXT, "spend" DOUBLE PRECISION NOT NULL DEFAULT 0,
"expires" TIMESTAMP(3), "models" TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[], "aliases" JSONB NOT NULL DEFAULT '{}'::JSONB,
"config" JSONB NOT NULL DEFAULT '{}'::JSONB, "user_id" TEXT, "team_id" TEXT, "project_id" TEXT,
"permissions" JSONB NOT NULL DEFAULT '{}'::JSONB, "metadata" JSONB NOT NULL DEFAULT '{}'::JSONB,
"max_parallel_requests" INTEGER, "blocked" BOOLEAN DEFAULT FALSE, "tpm_limit" BIGINT, "rpm_limit" BIGINT,
"max_budget" DOUBLE PRECISION, "budget_duration" TEXT, "budget_reset_at" TIMESTAMP(3), "budget_id" TEXT,
"organization_id" TEXT, "created_at" TIMESTAMP(3) DEFAULT NOW(), "updated_at" TIMESTAMP(3) DEFAULT NOW(), "last_active" TIMESTAMP(3))`,
	"spend logs table": `CREATE TABLE IF NOT EXISTS "LiteLLM_SpendLogs" (
"request_id" TEXT PRIMARY KEY, "call_type" TEXT NOT NULL, "api_key" TEXT NOT NULL DEFAULT '', "spend" DOUBLE PRECISION NOT NULL DEFAULT 0,
"total_tokens" INTEGER NOT NULL DEFAULT 0, "prompt_tokens" INTEGER NOT NULL DEFAULT 0, "completion_tokens" INTEGER NOT NULL DEFAULT 0,
"startTime" TIMESTAMP(3) NOT NULL, "endTime" TIMESTAMP(3) NOT NULL, "request_duration_ms" INTEGER,
"model" TEXT NOT NULL DEFAULT '', "model_group" TEXT, "custom_llm_provider" TEXT, "api_base" TEXT,
"user" TEXT, "metadata" JSONB NOT NULL DEFAULT '{}'::JSONB, "team_id" TEXT, "organization_id" TEXT, "end_user" TEXT,
"status" TEXT, "created_at" TIMESTAMP(3) NOT NULL DEFAULT NOW(), "updated_at" TIMESTAMP(3) NOT NULL DEFAULT NOW())`,
	"config table": `CREATE TABLE IF NOT EXISTS "LiteLLM_Config" (
"param_name" TEXT PRIMARY KEY, "param_value" JSONB, "last_run_at" TIMESTAMP(3), "reload_revision" BIGINT NOT NULL DEFAULT 0)`,
	"verification token indexes": `CREATE INDEX IF NOT EXISTS "LiteLLM_VerificationToken_team_id_idx" ON "LiteLLM_VerificationToken" ("team_id")`,
	"spend log index":            `CREATE INDEX IF NOT EXISTS "LiteLLM_SpendLogs_startTime_idx" ON "LiteLLM_SpendLogs" ("startTime")`,
}

var coreSchemaOrder = []string{
	"budget table",
	"user table",
	"organization table",
	"team table",
	"project table",
	"verification token table",
	"spend logs table",
	"config table",
	"verification token indexes",
	"spend log index",
}
