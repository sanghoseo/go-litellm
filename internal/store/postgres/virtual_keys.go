package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/BerriAI/litellm/go-proxy/internal/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type VirtualKeyStore struct {
	pool *pgxpool.Pool
}

func NewVirtualKeyStore(pool *pgxpool.Pool) VirtualKeyStore {
	return VirtualKeyStore{pool: pool}
}

func (store VirtualKeyStore) FindVirtualKey(ctx context.Context, tokenHash string) (auth.VirtualKey, error) {
	if store.pool == nil {
		return auth.VirtualKey{}, auth.ErrInvalidVirtualKey
	}

	key := auth.VirtualKey{}
	err := store.pool.QueryRow(ctx, `
SELECT k."token", COALESCE(k."models", ARRAY[]::TEXT[]), COALESCE(k."user_id", ''), COALESCE(u."models", ARRAY[]::TEXT[]), COALESCE(k."team_id", ''), COALESCE(t."models", ARRAY[]::TEXT[]), COALESCE(k."project_id", ''), COALESCE(p."models", ARRAY[]::TEXT[]), COALESCE(k."organization_id", ''), COALESCE(o."models", ARRAY[]::TEXT[]), COALESCE(k."budget_id", ''), k."expires", COALESCE(k."blocked", false) OR COALESCE(u."blocked", false) OR COALESCE(t."blocked", false) OR COALESCE(p."blocked", false) OR COALESCE(o."blocked", false), k."rpm_limit"
FROM "LiteLLM_VerificationToken" k
LEFT JOIN "LiteLLM_UserTable" u ON u."user_id" = k."user_id"
LEFT JOIN "LiteLLM_TeamTable" t ON t."team_id" = k."team_id"
LEFT JOIN "LiteLLM_ProjectTable" p ON p."project_id" = k."project_id"
LEFT JOIN "LiteLLM_OrganizationTable" o ON o."organization_id" = k."organization_id"
WHERE k."token" = $1`, tokenHash).Scan(&key.TokenHash, &key.Models, &key.UserID, &key.UserModels, &key.TeamID, &key.TeamModels, &key.ProjectID, &key.ProjectModels, &key.OrganizationID, &key.OrganizationModels, &key.BudgetID, &key.ExpiresAt, &key.Blocked, &key.RPMLimit)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.VirtualKey{}, auth.ErrInvalidVirtualKey
	}
	if err != nil {
		return auth.VirtualKey{}, fmt.Errorf("query virtual key: %w", err)
	}
	if key.ExpiresAt != nil && !key.ExpiresAt.After(time.Now()) {
		return auth.VirtualKey{}, auth.ErrInvalidVirtualKey
	}
	return key, nil
}

func (store VirtualKeyStore) CreateVirtualKey(ctx context.Context, record auth.ManagedVirtualKey) error {
	if store.pool == nil || record.TokenHash == "" {
		return auth.ErrInvalidVirtualKey
	}
	models := record.Models
	if models == nil {
		models = []string{}
	}
	_, err := store.pool.Exec(ctx, `
INSERT INTO "LiteLLM_VerificationToken" ("token", "key_alias", "models", "user_id", "team_id", "project_id", "organization_id", "budget_id", "expires", "blocked", "rpm_limit")
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`, record.TokenHash, record.KeyAlias, models, record.UserID, record.TeamID, record.ProjectID, record.OrganizationID, record.BudgetID, record.ExpiresAt, record.Blocked, record.RPMLimit)
	if err != nil {
		return fmt.Errorf("create virtual key: %w", err)
	}
	return nil
}

func (store VirtualKeyStore) GetVirtualKey(ctx context.Context, tokenHash string) (auth.ManagedVirtualKey, error) {
	if store.pool == nil {
		return auth.ManagedVirtualKey{}, auth.ErrInvalidVirtualKey
	}
	var record auth.ManagedVirtualKey
	err := store.pool.QueryRow(ctx, `
SELECT "token", COALESCE("key_alias", ''), COALESCE("models", ARRAY[]::TEXT[]), COALESCE("user_id", ''), COALESCE("team_id", ''), COALESCE("project_id", ''), COALESCE("organization_id", ''), COALESCE("budget_id", ''), "expires", COALESCE("blocked", false), "rpm_limit"
FROM "LiteLLM_VerificationToken" WHERE "token" = $1`, tokenHash).Scan(&record.TokenHash, &record.KeyAlias, &record.Models, &record.UserID, &record.TeamID, &record.ProjectID, &record.OrganizationID, &record.BudgetID, &record.ExpiresAt, &record.Blocked, &record.RPMLimit)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ManagedVirtualKey{}, auth.ErrInvalidVirtualKey
	}
	if err != nil {
		return auth.ManagedVirtualKey{}, fmt.Errorf("get virtual key: %w", err)
	}
	return record, nil
}

func (store VirtualKeyStore) DeleteVirtualKey(ctx context.Context, tokenHash string) (bool, error) {
	if store.pool == nil {
		return false, auth.ErrInvalidVirtualKey
	}
	result, err := store.pool.Exec(ctx, `DELETE FROM "LiteLLM_VerificationToken" WHERE "token" = $1`, tokenHash)
	if err != nil {
		return false, fmt.Errorf("delete virtual key: %w", err)
	}
	return result.RowsAffected() > 0, nil
}

func (store VirtualKeyStore) SetVirtualKeyBlocked(ctx context.Context, tokenHash string, blocked bool) (bool, error) {
	if store.pool == nil {
		return false, auth.ErrInvalidVirtualKey
	}
	result, err := store.pool.Exec(ctx, `UPDATE "LiteLLM_VerificationToken" SET "blocked" = $2, "updated_at" = NOW() WHERE "token" = $1`, tokenHash, blocked)
	if err != nil {
		return false, fmt.Errorf("set virtual key blocked: %w", err)
	}
	return result.RowsAffected() > 0, nil
}

func (store VirtualKeyStore) UpdateVirtualKey(ctx context.Context, tokenHash string, update auth.ManagedVirtualKeyUpdate) (bool, error) {
	if store.pool == nil {
		return false, auth.ErrInvalidVirtualKey
	}
	var alias, models, expires, rpmLimit any
	if update.KeyAlias != nil {
		alias = *update.KeyAlias
	}
	if update.Models != nil {
		models = *update.Models
	}
	if update.ExpiresAt != nil {
		expires = *update.ExpiresAt
	}
	if update.RPMLimit != nil {
		rpmLimit = *update.RPMLimit
	}
	result, err := store.pool.Exec(ctx, `
UPDATE "LiteLLM_VerificationToken"
SET "key_alias" = COALESCE($2, "key_alias"), "models" = COALESCE($3, "models"),
    "expires" = COALESCE($4, "expires"), "rpm_limit" = COALESCE($5, "rpm_limit"), "updated_at" = NOW()
WHERE "token" = $1`, tokenHash, alias, models, expires, rpmLimit)
	if err != nil {
		return false, fmt.Errorf("update virtual key: %w", err)
	}
	return result.RowsAffected() > 0, nil
}

func (store VirtualKeyStore) ListVirtualKeys(ctx context.Context, limit int) ([]auth.ManagedVirtualKey, error) {
	if store.pool == nil {
		return nil, auth.ErrInvalidVirtualKey
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := store.pool.Query(ctx, `
SELECT "token", COALESCE("key_alias", ''), COALESCE("models", ARRAY[]::TEXT[]), COALESCE("user_id", ''), COALESCE("team_id", ''), COALESCE("project_id", ''), COALESCE("organization_id", ''), COALESCE("budget_id", ''), "expires", COALESCE("blocked", false), "rpm_limit"
FROM "LiteLLM_VerificationToken" ORDER BY "created_at" DESC NULLS LAST LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list virtual keys: %w", err)
	}
	defer rows.Close()
	keys := []auth.ManagedVirtualKey{}
	for rows.Next() {
		var key auth.ManagedVirtualKey
		if err := rows.Scan(&key.TokenHash, &key.KeyAlias, &key.Models, &key.UserID, &key.TeamID, &key.ProjectID, &key.OrganizationID, &key.BudgetID, &key.ExpiresAt, &key.Blocked, &key.RPMLimit); err != nil {
			return nil, fmt.Errorf("scan virtual key: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate virtual keys: %w", err)
	}
	return keys, nil
}

func (store VirtualKeyStore) RegenerateVirtualKey(ctx context.Context, oldTokenHash, newTokenHash string) (auth.ManagedVirtualKey, error) {
	if store.pool == nil || oldTokenHash == "" || newTokenHash == "" {
		return auth.ManagedVirtualKey{}, auth.ErrInvalidVirtualKey
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return auth.ManagedVirtualKey{}, fmt.Errorf("begin virtual key regeneration: %w", err)
	}
	defer tx.Rollback(ctx)
	var record auth.ManagedVirtualKey
	err = tx.QueryRow(ctx, `
SELECT "token", COALESCE("key_alias", ''), COALESCE("models", ARRAY[]::TEXT[]), COALESCE("user_id", ''), COALESCE("team_id", ''), COALESCE("project_id", ''), COALESCE("organization_id", ''), COALESCE("budget_id", ''), "expires", COALESCE("blocked", false), "rpm_limit"
FROM "LiteLLM_VerificationToken" WHERE "token" = $1 FOR UPDATE`, oldTokenHash).Scan(&record.TokenHash, &record.KeyAlias, &record.Models, &record.UserID, &record.TeamID, &record.ProjectID, &record.OrganizationID, &record.BudgetID, &record.ExpiresAt, &record.Blocked, &record.RPMLimit)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ManagedVirtualKey{}, auth.ErrInvalidVirtualKey
	}
	if err != nil {
		return auth.ManagedVirtualKey{}, fmt.Errorf("load virtual key for regeneration: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM "LiteLLM_VerificationToken" WHERE "token" = $1`, oldTokenHash); err != nil {
		return auth.ManagedVirtualKey{}, fmt.Errorf("delete old virtual key: %w", err)
	}
	record.TokenHash = newTokenHash
	if _, err := tx.Exec(ctx, `
INSERT INTO "LiteLLM_VerificationToken" ("token", "key_alias", "models", "user_id", "team_id", "project_id", "organization_id", "budget_id", "expires", "blocked", "rpm_limit")
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`, record.TokenHash, record.KeyAlias, record.Models, record.UserID, record.TeamID, record.ProjectID, record.OrganizationID, record.BudgetID, record.ExpiresAt, record.Blocked, record.RPMLimit); err != nil {
		return auth.ManagedVirtualKey{}, fmt.Errorf("create regenerated virtual key: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return auth.ManagedVirtualKey{}, fmt.Errorf("commit virtual key regeneration: %w", err)
	}
	return record, nil
}
