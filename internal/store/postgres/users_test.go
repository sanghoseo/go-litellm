package postgres

import (
	"context"
	"os"
	"testing"

	"github.com/BerriAI/litellm/go-proxy/internal/auth"
	"github.com/BerriAI/litellm/go-proxy/internal/localdev"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestUserStoreLifecycle(t *testing.T) {
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
	store := NewUserStore(pool)
	user := auth.ManagedUser{UserID: "user-integration", UserAlias: "Integration", TeamID: "team-integration", UserEmail: "user@example.com", Models: []string{"gateway-model"}}
	if err := store.CreateUser(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetUser(context.Background(), user.UserID)
	if err != nil || loaded.UserAlias != user.UserAlias || loaded.TeamID != user.TeamID || loaded.UserEmail != user.UserEmail || len(loaded.Models) != 1 {
		t.Fatalf("user=%#v err=%v", loaded, err)
	}
	users, err := store.ListUsers(context.Background(), 10)
	if err != nil || len(users) != 1 || users[0].UserID != user.UserID {
		t.Fatalf("users=%#v err=%v", users, err)
	}
	blocked, err := store.SetUserBlocked(context.Background(), user.UserID, true)
	if err != nil || !blocked {
		t.Fatalf("blocked=%t err=%v", blocked, err)
	}
	loaded, err = store.GetUser(context.Background(), user.UserID)
	if err != nil || !loaded.Blocked {
		t.Fatalf("user=%#v err=%v", loaded, err)
	}
	deleted, err := store.DeleteUser(context.Background(), user.UserID)
	if err != nil || !deleted {
		t.Fatalf("deleted=%t err=%v", deleted, err)
	}
}
