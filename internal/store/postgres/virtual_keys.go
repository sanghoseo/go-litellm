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
SELECT "token", COALESCE("models", ARRAY[]::TEXT[]), "expires", COALESCE("blocked", false), "rpm_limit"
FROM "LiteLLM_VerificationToken"
WHERE "token" = $1`, tokenHash).Scan(&key.TokenHash, &key.Models, &key.ExpiresAt, &key.Blocked, &key.RPMLimit)
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
