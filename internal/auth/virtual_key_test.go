package auth

import (
	"context"
	"errors"
	"testing"
)

func TestValidatorAcceptsStoredKeyForAllowedModel(t *testing.T) {
	validator := NewValidator(stubStore{key: VirtualKey{TokenHash: HashKey("sk-test"), Models: []string{"gateway-model"}}})

	key, err := validator.Validate(context.Background(), "sk-test", "gateway-model")
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if key.TokenHash != HashKey("sk-test") {
		t.Fatalf("TokenHash = %q", key.TokenHash)
	}
}

func TestValidatorRejectsKeyOutsideModelAllowlist(t *testing.T) {
	validator := NewValidator(stubStore{key: VirtualKey{Models: []string{"gateway-model"}}})

	_, err := validator.Validate(context.Background(), "sk-test", "other-model")
	if !errors.Is(err, ErrInvalidVirtualKey) {
		t.Fatalf("Validate() error = %v, want ErrInvalidVirtualKey", err)
	}
}

func TestAllowsModelRequiresEveryScopedAllowlist(t *testing.T) {
	key := VirtualKey{
		Models:             []string{"gateway-model", "team-only"},
		UserModels:         []string{"gateway-model", "team-only"},
		TeamModels:         []string{"gateway-model", "team-only"},
		ProjectModels:      []string{"gateway-model"},
		OrganizationModels: []string{"gateway-model"},
	}
	if !AllowsModel(key, "gateway-model") {
		t.Fatal("shared model was rejected")
	}
	if AllowsModel(key, "team-only") {
		t.Fatal("model omitted by a narrower scope was accepted")
	}
}

type stubStore struct {
	key VirtualKey
}

func (store stubStore) FindVirtualKey(_ context.Context, _ string) (VirtualKey, error) {
	return store.key, nil
}
