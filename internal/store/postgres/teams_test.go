package postgres

import (
	"context"
	"os"
	"testing"

	"github.com/BerriAI/litellm/go-proxy/internal/auth"
	"github.com/BerriAI/litellm/go-proxy/internal/localdev"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTeamStoreLifecycle(t *testing.T) {
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
	store := NewTeamStore(pool)
	team := auth.ManagedTeam{TeamID: "team-integration", TeamAlias: "Integration", Models: []string{"gateway-model"}}
	if err := store.CreateTeam(context.Background(), team); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetTeam(context.Background(), team.TeamID)
	if err != nil || loaded.TeamAlias != team.TeamAlias || len(loaded.Models) != 1 || loaded.Admins == nil || loaded.Members == nil {
		t.Fatalf("team=%#v err=%v", loaded, err)
	}
	alias := "Updated"
	members := []string{"member"}
	updated, err := store.UpdateTeam(context.Background(), team.TeamID, auth.ManagedTeamUpdate{TeamAlias: &alias, Members: &members})
	if err != nil || !updated {
		t.Fatalf("updated=%t err=%v", updated, err)
	}
	loaded, err = store.GetTeam(context.Background(), team.TeamID)
	if err != nil || loaded.TeamAlias != alias || len(loaded.Members) != 1 || loaded.Members[0] != members[0] {
		t.Fatalf("updated team=%#v err=%v", loaded, err)
	}
	teams, err := store.ListTeams(context.Background(), 10)
	if err != nil || len(teams) != 1 || teams[0].TeamID != team.TeamID {
		t.Fatalf("teams=%#v err=%v", teams, err)
	}
	blocked, err := store.SetTeamBlocked(context.Background(), team.TeamID, true)
	if err != nil || !blocked {
		t.Fatalf("blocked=%t err=%v", blocked, err)
	}
	loaded, err = store.GetTeam(context.Background(), team.TeamID)
	if err != nil || !loaded.Blocked {
		t.Fatalf("team=%#v err=%v", loaded, err)
	}
	deleted, err := store.DeleteTeam(context.Background(), team.TeamID)
	if err != nil || !deleted {
		t.Fatalf("deleted=%t err=%v", deleted, err)
	}
}
