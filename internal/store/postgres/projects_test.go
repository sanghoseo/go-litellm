package postgres

import (
	"context"
	"os"
	"testing"

	"github.com/BerriAI/litellm/go-proxy/internal/auth"
	"github.com/BerriAI/litellm/go-proxy/internal/localdev"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestProjectStoreLifecycle(t *testing.T) {
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
	store := NewProjectStore(pool)
	project := auth.ManagedProject{ProjectID: "project-integration", ProjectAlias: "Integration", TeamID: "team-integration", Models: []string{"gateway-model"}}
	if err := store.CreateProject(context.Background(), project); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetProject(context.Background(), project.ProjectID)
	if err != nil || loaded.ProjectAlias != project.ProjectAlias || len(loaded.Models) != 1 {
		t.Fatalf("project=%#v err=%v", loaded, err)
	}
	projects, err := store.ListProjects(context.Background(), 10)
	if err != nil || len(projects) != 1 || projects[0].ProjectID != project.ProjectID {
		t.Fatalf("projects=%#v err=%v", projects, err)
	}
	blocked, err := store.SetProjectBlocked(context.Background(), project.ProjectID, true)
	if err != nil || !blocked {
		t.Fatalf("blocked=%t err=%v", blocked, err)
	}
	loaded, err = store.GetProject(context.Background(), project.ProjectID)
	if err != nil || !loaded.Blocked {
		t.Fatalf("project=%#v err=%v", loaded, err)
	}
	deleted, err := store.DeleteProject(context.Background(), project.ProjectID)
	if err != nil || !deleted {
		t.Fatalf("deleted=%t err=%v", deleted, err)
	}
}
