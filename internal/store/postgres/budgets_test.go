package postgres

import (
	"context"
	"os"
	"testing"

	"github.com/BerriAI/litellm/go-proxy/internal/auth"
	"github.com/BerriAI/litellm/go-proxy/internal/localdev"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBudgetStoreLifecycle(t *testing.T) {
	if os.Getenv("LITELLM_RUN_EMBEDDED_TESTS") != "1" {
		t.Skip("set LITELLM_RUN_EMBEDDED_TESTS=1 to run embedded PostgreSQL integration tests")
	}
	dependencies, err := localdev.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer dependencies.Close()
	pool, err := pgxpool.New(context.Background(), dependencies.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := EnsureCoreSchema(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	store := NewBudgetStore(pool)
	maxBudget := 12.5
	budget := auth.ManagedBudget{BudgetID: "budget-integration", MaxBudget: &maxBudget, BudgetDuration: "1d"}
	if err := store.CreateBudget(context.Background(), budget); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetBudget(context.Background(), budget.BudgetID)
	if err != nil || loaded.MaxBudget == nil || *loaded.MaxBudget != maxBudget {
		t.Fatalf("budget=%#v err=%v", loaded, err)
	}
	softBudget := 10.0
	updated, err := store.UpdateBudget(context.Background(), budget.BudgetID, auth.ManagedBudgetUpdate{SoftBudget: &softBudget})
	if err != nil || !updated {
		t.Fatalf("updated=%t err=%v", updated, err)
	}
	budgets, err := store.ListBudgets(context.Background(), 10)
	if err != nil || len(budgets) != 1 || budgets[0].SoftBudget == nil || *budgets[0].SoftBudget != softBudget {
		t.Fatalf("budgets=%#v err=%v", budgets, err)
	}
	deleted, err := store.DeleteBudget(context.Background(), budget.BudgetID)
	if err != nil || !deleted {
		t.Fatalf("deleted=%t err=%v", deleted, err)
	}
}
