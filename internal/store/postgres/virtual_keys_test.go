package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/BerriAI/litellm/go-proxy/internal/auth"
	"github.com/BerriAI/litellm/go-proxy/internal/localdev"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestVirtualKeyStoreLifecycle(t *testing.T) {
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
	expires := time.Now().UTC().Add(time.Hour)
	store := NewVirtualKeyStore(pool)
	teamStore := NewTeamStore(pool)
	if err := teamStore.CreateTeam(context.Background(), auth.ManagedTeam{TeamID: "team-key-integration"}); err != nil {
		t.Fatal(err)
	}
	projectStore := NewProjectStore(pool)
	if err := projectStore.CreateProject(context.Background(), auth.ManagedProject{ProjectID: "project-key-integration", TeamID: "team-key-integration"}); err != nil {
		t.Fatal(err)
	}
	record := auth.ManagedVirtualKey{TokenHash: auth.HashKey("sk-integration-test"), KeyAlias: "integration", Models: []string{"gateway-model"}, TeamID: "team-key-integration", ProjectID: "project-key-integration", ExpiresAt: &expires}
	if err := store.CreateVirtualKey(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetVirtualKey(context.Background(), record.TokenHash)
	if err != nil || loaded.KeyAlias != record.KeyAlias || loaded.TeamID != record.TeamID || loaded.ProjectID != record.ProjectID || len(loaded.Models) != 1 || loaded.Models[0] != "gateway-model" {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
	alias := "updated"
	models := []string{"other-model"}
	rpmLimit := int64(10)
	updated, err := store.UpdateVirtualKey(context.Background(), record.TokenHash, auth.ManagedVirtualKeyUpdate{KeyAlias: &alias, Models: &models, RPMLimit: &rpmLimit})
	if err != nil || !updated {
		t.Fatalf("updated=%t err=%v", updated, err)
	}
	loaded, err = store.GetVirtualKey(context.Background(), record.TokenHash)
	if err != nil || loaded.KeyAlias != alias || len(loaded.Models) != 1 || loaded.Models[0] != models[0] || loaded.RPMLimit == nil || *loaded.RPMLimit != rpmLimit {
		t.Fatalf("updated record=%#v err=%v", loaded, err)
	}
	keys, err := store.ListVirtualKeys(context.Background(), 10)
	if err != nil || len(keys) != 1 || keys[0].TokenHash != record.TokenHash {
		t.Fatalf("keys=%#v err=%v", keys, err)
	}
	regeneratedHash := auth.HashKey("sk-regenerated-integration-test")
	regenerated, err := store.RegenerateVirtualKey(context.Background(), record.TokenHash, regeneratedHash)
	if err != nil || regenerated.TokenHash != regeneratedHash || regenerated.ProjectID != record.ProjectID {
		t.Fatalf("regenerated=%#v err=%v", regenerated, err)
	}
	if _, err := store.GetVirtualKey(context.Background(), record.TokenHash); err == nil {
		t.Fatal("old key hash still exists")
	}
	record.TokenHash = regeneratedHash
	if _, err := store.FindVirtualKey(context.Background(), record.TokenHash); err != nil {
		t.Fatal(err)
	}
	updated, err = store.SetVirtualKeyBlocked(context.Background(), record.TokenHash, true)
	if err != nil || !updated {
		t.Fatalf("updated=%t err=%v", updated, err)
	}
	if _, err := auth.NewValidator(store).Validate(context.Background(), "sk-integration-test", "gateway-model"); err == nil {
		t.Fatal("blocked key was accepted")
	}
	if _, err := store.SetVirtualKeyBlocked(context.Background(), record.TokenHash, false); err != nil {
		t.Fatal(err)
	}
	if _, err := teamStore.SetTeamBlocked(context.Background(), record.TeamID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.NewValidator(store).Validate(context.Background(), "sk-integration-test", "gateway-model"); err == nil {
		t.Fatal("key attached to blocked team was accepted")
	}
	if _, err := teamStore.SetTeamBlocked(context.Background(), record.TeamID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := projectStore.SetProjectBlocked(context.Background(), record.ProjectID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.NewValidator(store).Validate(context.Background(), "sk-integration-test", "gateway-model"); err == nil {
		t.Fatal("key attached to blocked project was accepted")
	}
	deleted, err := store.DeleteVirtualKey(context.Background(), record.TokenHash)
	if err != nil || !deleted {
		t.Fatalf("deleted=%t err=%v", deleted, err)
	}
}
