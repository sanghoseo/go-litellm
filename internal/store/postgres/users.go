package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/BerriAI/litellm/go-proxy/internal/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UserStore struct{ pool *pgxpool.Pool }

func NewUserStore(pool *pgxpool.Pool) UserStore { return UserStore{pool: pool} }

func (store UserStore) CreateUser(ctx context.Context, user auth.ManagedUser) error {
	if store.pool == nil || user.UserID == "" {
		return auth.ErrInvalidVirtualKey
	}
	_, err := store.pool.Exec(ctx, "INSERT INTO \"LiteLLM_UserTable\" (\"user_id\", \"user_alias\", \"team_id\", \"user_email\", \"models\", \"blocked\") VALUES ($1, $2, $3, $4, $5, $6)", user.UserID, user.UserAlias, user.TeamID, user.UserEmail, nonNilStringSlice(user.Models), user.Blocked)
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

func (store UserStore) GetUser(ctx context.Context, userID string) (auth.ManagedUser, error) {
	if store.pool == nil {
		return auth.ManagedUser{}, auth.ErrInvalidVirtualKey
	}
	var user auth.ManagedUser
	err := store.pool.QueryRow(ctx, "SELECT \"user_id\", COALESCE(\"user_alias\", ''), COALESCE(\"team_id\", ''), COALESCE(\"user_email\", ''), COALESCE(\"models\", ARRAY[]::TEXT[]), COALESCE(\"blocked\", false) FROM \"LiteLLM_UserTable\" WHERE \"user_id\" = $1", userID).Scan(&user.UserID, &user.UserAlias, &user.TeamID, &user.UserEmail, &user.Models, &user.Blocked)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ManagedUser{}, auth.ErrInvalidVirtualKey
	}
	if err != nil {
		return auth.ManagedUser{}, fmt.Errorf("get user: %w", err)
	}
	return user, nil
}

func (store UserStore) ListUsers(ctx context.Context, limit int) ([]auth.ManagedUser, error) {
	if store.pool == nil {
		return nil, auth.ErrInvalidVirtualKey
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := store.pool.Query(ctx, "SELECT \"user_id\", COALESCE(\"user_alias\", ''), COALESCE(\"team_id\", ''), COALESCE(\"user_email\", ''), COALESCE(\"models\", ARRAY[]::TEXT[]), COALESCE(\"blocked\", false) FROM \"LiteLLM_UserTable\" ORDER BY \"created_at\" DESC LIMIT $1", limit)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	users := []auth.ManagedUser{}
	for rows.Next() {
		var user auth.ManagedUser
		if err := rows.Scan(&user.UserID, &user.UserAlias, &user.TeamID, &user.UserEmail, &user.Models, &user.Blocked); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate users: %w", err)
	}
	return users, nil
}

func (store UserStore) SetUserBlocked(ctx context.Context, userID string, blocked bool) (bool, error) {
	if store.pool == nil {
		return false, auth.ErrInvalidVirtualKey
	}
	result, err := store.pool.Exec(ctx, "UPDATE \"LiteLLM_UserTable\" SET \"blocked\" = $2, \"updated_at\" = NOW() WHERE \"user_id\" = $1", userID, blocked)
	if err != nil {
		return false, fmt.Errorf("set user blocked: %w", err)
	}
	return result.RowsAffected() > 0, nil
}

func (store UserStore) DeleteUser(ctx context.Context, userID string) (bool, error) {
	if store.pool == nil {
		return false, auth.ErrInvalidVirtualKey
	}
	result, err := store.pool.Exec(ctx, "DELETE FROM \"LiteLLM_UserTable\" WHERE \"user_id\" = $1", userID)
	if err != nil {
		return false, fmt.Errorf("delete user: %w", err)
	}
	return result.RowsAffected() > 0, nil
}
