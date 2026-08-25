package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/BerriAI/litellm/go-proxy/internal/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TeamStore struct {
	pool *pgxpool.Pool
}

func NewTeamStore(pool *pgxpool.Pool) TeamStore {
	return TeamStore{pool: pool}
}

func (store TeamStore) CreateTeam(ctx context.Context, team auth.ManagedTeam) error {
	if store.pool == nil || team.TeamID == "" {
		return auth.ErrInvalidVirtualKey
	}
	team.Admins = nonNilStringSlice(team.Admins)
	team.Members = nonNilStringSlice(team.Members)
	team.Models = nonNilStringSlice(team.Models)
	_, err := store.pool.Exec(ctx, `
INSERT INTO "LiteLLM_TeamTable" ("team_id", "team_alias", "admins", "members", "models", "blocked")
VALUES ($1, $2, $3, $4, $5, $6)`, team.TeamID, team.TeamAlias, team.Admins, team.Members, team.Models, team.Blocked)
	if err != nil {
		return fmt.Errorf("create team: %w", err)
	}
	return nil
}

func nonNilStringSlice(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func (store TeamStore) GetTeam(ctx context.Context, teamID string) (auth.ManagedTeam, error) {
	if store.pool == nil {
		return auth.ManagedTeam{}, auth.ErrInvalidVirtualKey
	}
	var team auth.ManagedTeam
	err := store.pool.QueryRow(ctx, `
SELECT "team_id", COALESCE("team_alias", ''), COALESCE("admins", ARRAY[]::TEXT[]), COALESCE("members", ARRAY[]::TEXT[]), COALESCE("models", ARRAY[]::TEXT[]), COALESCE("blocked", false)
FROM "LiteLLM_TeamTable" WHERE "team_id" = $1`, teamID).Scan(&team.TeamID, &team.TeamAlias, &team.Admins, &team.Members, &team.Models, &team.Blocked)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ManagedTeam{}, auth.ErrInvalidVirtualKey
	}
	if err != nil {
		return auth.ManagedTeam{}, fmt.Errorf("get team: %w", err)
	}
	return team, nil
}

func (store TeamStore) UpdateTeam(ctx context.Context, teamID string, update auth.ManagedTeamUpdate) (bool, error) {
	if store.pool == nil {
		return false, auth.ErrInvalidVirtualKey
	}
	var alias, admins, members, models, blocked any
	if update.TeamAlias != nil {
		alias = *update.TeamAlias
	}
	if update.Admins != nil {
		admins = nonNilStringSlice(*update.Admins)
	}
	if update.Members != nil {
		members = nonNilStringSlice(*update.Members)
	}
	if update.Models != nil {
		models = nonNilStringSlice(*update.Models)
	}
	if update.Blocked != nil {
		blocked = *update.Blocked
	}
	result, err := store.pool.Exec(ctx, "UPDATE \"LiteLLM_TeamTable\" SET \"team_alias\" = COALESCE($2, \"team_alias\"), \"admins\" = COALESCE($3, \"admins\"), \"members\" = COALESCE($4, \"members\"), \"models\" = COALESCE($5, \"models\"), \"blocked\" = COALESCE($6, \"blocked\"), \"updated_at\" = NOW() WHERE \"team_id\" = $1", teamID, alias, admins, members, models, blocked)
	if err != nil {
		return false, fmt.Errorf("update team: %w", err)
	}
	return result.RowsAffected() > 0, nil
}

func (store TeamStore) ListTeams(ctx context.Context, limit int) ([]auth.ManagedTeam, error) {
	if store.pool == nil {
		return nil, auth.ErrInvalidVirtualKey
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := store.pool.Query(ctx, `
SELECT "team_id", COALESCE("team_alias", ''), COALESCE("admins", ARRAY[]::TEXT[]), COALESCE("members", ARRAY[]::TEXT[]), COALESCE("models", ARRAY[]::TEXT[]), COALESCE("blocked", false)
FROM "LiteLLM_TeamTable" ORDER BY "created_at" DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list teams: %w", err)
	}
	defer rows.Close()
	teams := []auth.ManagedTeam{}
	for rows.Next() {
		var team auth.ManagedTeam
		if err := rows.Scan(&team.TeamID, &team.TeamAlias, &team.Admins, &team.Members, &team.Models, &team.Blocked); err != nil {
			return nil, fmt.Errorf("scan team: %w", err)
		}
		teams = append(teams, team)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate teams: %w", err)
	}
	return teams, nil
}

func (store TeamStore) SetTeamBlocked(ctx context.Context, teamID string, blocked bool) (bool, error) {
	if store.pool == nil {
		return false, auth.ErrInvalidVirtualKey
	}
	result, err := store.pool.Exec(ctx, `UPDATE "LiteLLM_TeamTable" SET "blocked" = $2, "updated_at" = NOW() WHERE "team_id" = $1`, teamID, blocked)
	if err != nil {
		return false, fmt.Errorf("set team blocked: %w", err)
	}
	return result.RowsAffected() > 0, nil
}

func (store TeamStore) DeleteTeam(ctx context.Context, teamID string) (bool, error) {
	if store.pool == nil {
		return false, auth.ErrInvalidVirtualKey
	}
	result, err := store.pool.Exec(ctx, `DELETE FROM "LiteLLM_TeamTable" WHERE "team_id" = $1`, teamID)
	if err != nil {
		return false, fmt.Errorf("delete team: %w", err)
	}
	return result.RowsAffected() > 0, nil
}
