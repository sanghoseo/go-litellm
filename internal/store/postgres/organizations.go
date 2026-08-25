package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/BerriAI/litellm/go-proxy/internal/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type OrganizationStore struct{ pool *pgxpool.Pool }

func NewOrganizationStore(pool *pgxpool.Pool) OrganizationStore { return OrganizationStore{pool: pool} }

func (store OrganizationStore) CreateOrganization(ctx context.Context, organization auth.ManagedOrganization) error {
	if store.pool == nil || organization.OrganizationID == "" {
		return auth.ErrInvalidVirtualKey
	}
	_, err := store.pool.Exec(ctx, "INSERT INTO \"LiteLLM_OrganizationTable\" (\"organization_id\", \"organization_alias\", \"budget_id\", \"models\", \"blocked\") VALUES ($1, $2, $3, $4, $5)", organization.OrganizationID, organization.OrganizationAlias, organization.BudgetID, nonNilStringSlice(organization.Models), organization.Blocked)
	if err != nil {
		return fmt.Errorf("create organization: %w", err)
	}
	return nil
}

func (store OrganizationStore) GetOrganization(ctx context.Context, organizationID string) (auth.ManagedOrganization, error) {
	if store.pool == nil {
		return auth.ManagedOrganization{}, auth.ErrInvalidVirtualKey
	}
	var organization auth.ManagedOrganization
	err := store.pool.QueryRow(ctx, "SELECT \"organization_id\", COALESCE(\"organization_alias\", ''), COALESCE(\"budget_id\", ''), COALESCE(\"models\", ARRAY[]::TEXT[]), COALESCE(\"blocked\", false) FROM \"LiteLLM_OrganizationTable\" WHERE \"organization_id\" = $1", organizationID).Scan(&organization.OrganizationID, &organization.OrganizationAlias, &organization.BudgetID, &organization.Models, &organization.Blocked)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ManagedOrganization{}, auth.ErrInvalidVirtualKey
	}
	if err != nil {
		return auth.ManagedOrganization{}, fmt.Errorf("get organization: %w", err)
	}
	return organization, nil
}

func (store OrganizationStore) ListOrganizations(ctx context.Context, limit int) ([]auth.ManagedOrganization, error) {
	if store.pool == nil {
		return nil, auth.ErrInvalidVirtualKey
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := store.pool.Query(ctx, "SELECT \"organization_id\", COALESCE(\"organization_alias\", ''), COALESCE(\"budget_id\", ''), COALESCE(\"models\", ARRAY[]::TEXT[]), COALESCE(\"blocked\", false) FROM \"LiteLLM_OrganizationTable\" ORDER BY \"created_at\" DESC LIMIT $1", limit)
	if err != nil {
		return nil, fmt.Errorf("list organizations: %w", err)
	}
	defer rows.Close()
	organizations := []auth.ManagedOrganization{}
	for rows.Next() {
		var organization auth.ManagedOrganization
		if err := rows.Scan(&organization.OrganizationID, &organization.OrganizationAlias, &organization.BudgetID, &organization.Models, &organization.Blocked); err != nil {
			return nil, fmt.Errorf("scan organization: %w", err)
		}
		organizations = append(organizations, organization)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate organizations: %w", err)
	}
	return organizations, nil
}

func (store OrganizationStore) UpdateOrganization(ctx context.Context, organizationID string, update auth.ManagedOrganizationUpdate) (bool, error) {
	if store.pool == nil {
		return false, auth.ErrInvalidVirtualKey
	}
	var alias, budgetID, models, blocked any
	if update.OrganizationAlias != nil {
		alias = *update.OrganizationAlias
	}
	if update.BudgetID != nil {
		budgetID = *update.BudgetID
	}
	if update.Models != nil {
		models = nonNilStringSlice(*update.Models)
	}
	if update.Blocked != nil {
		blocked = *update.Blocked
	}
	result, err := store.pool.Exec(ctx, "UPDATE \"LiteLLM_OrganizationTable\" SET \"organization_alias\" = COALESCE($2, \"organization_alias\"), \"budget_id\" = COALESCE($3, \"budget_id\"), \"models\" = COALESCE($4, \"models\"), \"blocked\" = COALESCE($5, \"blocked\"), \"updated_at\" = NOW() WHERE \"organization_id\" = $1", organizationID, alias, budgetID, models, blocked)
	if err != nil {
		return false, fmt.Errorf("update organization: %w", err)
	}
	return result.RowsAffected() > 0, nil
}

func (store OrganizationStore) DeleteOrganization(ctx context.Context, organizationID string) (bool, error) {
	if store.pool == nil {
		return false, auth.ErrInvalidVirtualKey
	}
	result, err := store.pool.Exec(ctx, "DELETE FROM \"LiteLLM_OrganizationTable\" WHERE \"organization_id\" = $1", organizationID)
	if err != nil {
		return false, fmt.Errorf("delete organization: %w", err)
	}
	return result.RowsAffected() > 0, nil
}
