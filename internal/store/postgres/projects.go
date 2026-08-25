package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/BerriAI/litellm/go-proxy/internal/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ProjectStore struct{ pool *pgxpool.Pool }

func NewProjectStore(pool *pgxpool.Pool) ProjectStore { return ProjectStore{pool: pool} }

func (store ProjectStore) CreateProject(ctx context.Context, project auth.ManagedProject) error {
	if store.pool == nil || project.ProjectID == "" || project.TeamID == "" {
		return auth.ErrInvalidVirtualKey
	}
	_, err := store.pool.Exec(ctx, "INSERT INTO \"LiteLLM_ProjectTable\" (\"project_id\", \"project_alias\", \"description\", \"team_id\", \"budget_id\", \"models\", \"blocked\") VALUES ($1, $2, $3, $4, $5, $6, $7)", project.ProjectID, project.ProjectAlias, project.Description, project.TeamID, project.BudgetID, nonNilStringSlice(project.Models), project.Blocked)
	if err != nil {
		return fmt.Errorf("create project: %w", err)
	}
	return nil
}

func (store ProjectStore) GetProject(ctx context.Context, projectID string) (auth.ManagedProject, error) {
	if store.pool == nil {
		return auth.ManagedProject{}, auth.ErrInvalidVirtualKey
	}
	var project auth.ManagedProject
	err := store.pool.QueryRow(ctx, "SELECT \"project_id\", COALESCE(\"project_alias\", ''), COALESCE(\"description\", ''), COALESCE(\"team_id\", ''), COALESCE(\"budget_id\", ''), COALESCE(\"models\", ARRAY[]::TEXT[]), COALESCE(\"blocked\", false) FROM \"LiteLLM_ProjectTable\" WHERE \"project_id\" = $1", projectID).Scan(&project.ProjectID, &project.ProjectAlias, &project.Description, &project.TeamID, &project.BudgetID, &project.Models, &project.Blocked)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ManagedProject{}, auth.ErrInvalidVirtualKey
	}
	if err != nil {
		return auth.ManagedProject{}, fmt.Errorf("get project: %w", err)
	}
	return project, nil
}

func (store ProjectStore) ListProjects(ctx context.Context, limit int) ([]auth.ManagedProject, error) {
	if store.pool == nil {
		return nil, auth.ErrInvalidVirtualKey
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := store.pool.Query(ctx, "SELECT \"project_id\", COALESCE(\"project_alias\", ''), COALESCE(\"description\", ''), COALESCE(\"team_id\", ''), COALESCE(\"budget_id\", ''), COALESCE(\"models\", ARRAY[]::TEXT[]), COALESCE(\"blocked\", false) FROM \"LiteLLM_ProjectTable\" ORDER BY \"created_at\" DESC LIMIT $1", limit)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()
	projects := []auth.ManagedProject{}
	for rows.Next() {
		var project auth.ManagedProject
		if err := rows.Scan(&project.ProjectID, &project.ProjectAlias, &project.Description, &project.TeamID, &project.BudgetID, &project.Models, &project.Blocked); err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		projects = append(projects, project)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate projects: %w", err)
	}
	return projects, nil
}

func (store ProjectStore) SetProjectBlocked(ctx context.Context, projectID string, blocked bool) (bool, error) {
	if store.pool == nil {
		return false, auth.ErrInvalidVirtualKey
	}
	result, err := store.pool.Exec(ctx, "UPDATE \"LiteLLM_ProjectTable\" SET \"blocked\" = $2, \"updated_at\" = NOW() WHERE \"project_id\" = $1", projectID, blocked)
	if err != nil {
		return false, fmt.Errorf("set project blocked: %w", err)
	}
	return result.RowsAffected() > 0, nil
}

func (store ProjectStore) DeleteProject(ctx context.Context, projectID string) (bool, error) {
	if store.pool == nil {
		return false, auth.ErrInvalidVirtualKey
	}
	result, err := store.pool.Exec(ctx, "DELETE FROM \"LiteLLM_ProjectTable\" WHERE \"project_id\" = $1", projectID)
	if err != nil {
		return false, fmt.Errorf("delete project: %w", err)
	}
	return result.RowsAffected() > 0, nil
}
