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
	record := auth.ManagedVirtualKey{TokenHash: auth.HashKey("sk-integration-test"), KeyAlias: "integration", Models: []string{"gateway-model"}, ExpiresAt: &expires}
	if err := store.CreateVirtualKey(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetVirtualKey(context.Background(), record.TokenHash)
	if err != nil || loaded.KeyAlias != record.KeyAlias || len(loaded.Models) != 1 || loaded.Models[0] != "gateway-model" {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
	if _, err := store.FindVirtualKey(context.Background(), record.TokenHash); err != nil {
		t.Fatal(err)
	}
	deleted, err := store.DeleteVirtualKey(context.Background(), record.TokenHash)
	if err != nil || !deleted {
		t.Fatalf("deleted=%t err=%v", deleted, err)
	}
}
