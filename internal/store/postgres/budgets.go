package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/BerriAI/litellm/go-proxy/internal/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type BudgetStore struct{ pool *pgxpool.Pool }

func NewBudgetStore(pool *pgxpool.Pool) BudgetStore { return BudgetStore{pool: pool} }

func (store BudgetStore) CreateBudget(ctx context.Context, budget auth.ManagedBudget) error {
	if store.pool == nil || budget.BudgetID == "" {
		return auth.ErrInvalidVirtualKey
	}
	_, err := store.pool.Exec(ctx, `INSERT INTO "LiteLLM_BudgetTable" ("budget_id", "max_budget", "soft_budget", "max_parallel_requests", "tpm_limit", "rpm_limit", "budget_duration", "budget_reset_at") VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, budget.BudgetID, budget.MaxBudget, budget.SoftBudget, budget.MaxParallelRequests, budget.TPMLimit, budget.RPMLimit, budget.BudgetDuration, budget.BudgetResetAt)
	if err != nil {
		return fmt.Errorf("create budget: %w", err)
	}
	return nil
}

func (store BudgetStore) GetBudget(ctx context.Context, budgetID string) (auth.ManagedBudget, error) {
	if store.pool == nil {
		return auth.ManagedBudget{}, auth.ErrInvalidVirtualKey
	}
	var budget auth.ManagedBudget
	err := store.pool.QueryRow(ctx, `SELECT "budget_id", "max_budget", "soft_budget", "max_parallel_requests", "tpm_limit", "rpm_limit", COALESCE("budget_duration", ''), "budget_reset_at" FROM "LiteLLM_BudgetTable" WHERE "budget_id" = $1`, budgetID).Scan(&budget.BudgetID, &budget.MaxBudget, &budget.SoftBudget, &budget.MaxParallelRequests, &budget.TPMLimit, &budget.RPMLimit, &budget.BudgetDuration, &budget.BudgetResetAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ManagedBudget{}, auth.ErrInvalidVirtualKey
	}
	if err != nil {
		return auth.ManagedBudget{}, fmt.Errorf("get budget: %w", err)
	}
	return budget, nil
}

func (store BudgetStore) ListBudgets(ctx context.Context, limit int) ([]auth.ManagedBudget, error) {
	if store.pool == nil {
		return nil, auth.ErrInvalidVirtualKey
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := store.pool.Query(ctx, `SELECT "budget_id", "max_budget", "soft_budget", "max_parallel_requests", "tpm_limit", "rpm_limit", COALESCE("budget_duration", ''), "budget_reset_at" FROM "LiteLLM_BudgetTable" ORDER BY "created_at" DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list budgets: %w", err)
	}
	defer rows.Close()
	budgets := []auth.ManagedBudget{}
	for rows.Next() {
		var budget auth.ManagedBudget
		if err := rows.Scan(&budget.BudgetID, &budget.MaxBudget, &budget.SoftBudget, &budget.MaxParallelRequests, &budget.TPMLimit, &budget.RPMLimit, &budget.BudgetDuration, &budget.BudgetResetAt); err != nil {
			return nil, fmt.Errorf("scan budget: %w", err)
		}
		budgets = append(budgets, budget)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate budgets: %w", err)
	}
	return budgets, nil
}

func (store BudgetStore) UpdateBudget(ctx context.Context, budgetID string, update auth.ManagedBudgetUpdate) (bool, error) {
	if store.pool == nil {
		return false, auth.ErrInvalidVirtualKey
	}
	result, err := store.pool.Exec(ctx, `UPDATE "LiteLLM_BudgetTable" SET "max_budget" = COALESCE($2, "max_budget"), "soft_budget" = COALESCE($3, "soft_budget"), "max_parallel_requests" = COALESCE($4, "max_parallel_requests"), "tpm_limit" = COALESCE($5, "tpm_limit"), "rpm_limit" = COALESCE($6, "rpm_limit"), "budget_duration" = COALESCE($7, "budget_duration"), "budget_reset_at" = COALESCE($8, "budget_reset_at"), "updated_at" = NOW() WHERE "budget_id" = $1`, budgetID, update.MaxBudget, update.SoftBudget, update.MaxParallelRequests, update.TPMLimit, update.RPMLimit, update.BudgetDuration, update.BudgetResetAt)
	if err != nil {
		return false, fmt.Errorf("update budget: %w", err)
	}
	return result.RowsAffected() > 0, nil
}

func (store BudgetStore) DeleteBudget(ctx context.Context, budgetID string) (bool, error) {
	if store.pool == nil {
		return false, auth.ErrInvalidVirtualKey
	}
	result, err := store.pool.Exec(ctx, `DELETE FROM "LiteLLM_BudgetTable" WHERE "budget_id" = $1`, budgetID)
	if err != nil {
		return false, fmt.Errorf("delete budget: %w", err)
	}
	return result.RowsAffected() > 0, nil
}
