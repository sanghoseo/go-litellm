package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

var ErrInvalidVirtualKey = errors.New("invalid virtual key")

type VirtualKey struct {
	TokenHash string
	Models    []string
	ExpiresAt *time.Time
	Blocked   bool
	RPMLimit  *int64
}

type ManagedVirtualKey struct {
	TokenHash string
	KeyAlias  string
	Models    []string
	ExpiresAt *time.Time
	Blocked   bool
	RPMLimit  *int64
}

type VirtualKeyManager interface {
	CreateVirtualKey(context.Context, ManagedVirtualKey) error
	GetVirtualKey(context.Context, string) (ManagedVirtualKey, error)
	DeleteVirtualKey(context.Context, string) (bool, error)
	SetVirtualKeyBlocked(context.Context, string, bool) (bool, error)
}

type VirtualKeyStore interface {
	FindVirtualKey(context.Context, string) (VirtualKey, error)
}

type Validator struct {
	store VirtualKeyStore
}

func NewValidator(store VirtualKeyStore) Validator {
	return Validator{store: store}
}

func (validator Validator) Validate(ctx context.Context, rawKey string, model string) (VirtualKey, error) {
	if rawKey == "" || validator.store == nil {
		return VirtualKey{}, ErrInvalidVirtualKey
	}

	virtualKey, err := validator.store.FindVirtualKey(ctx, HashKey(rawKey))
	if err != nil {
		return VirtualKey{}, fmt.Errorf("find virtual key: %w", err)
	}
	if virtualKey.Blocked || (virtualKey.ExpiresAt != nil && !virtualKey.ExpiresAt.After(time.Now())) {
		return VirtualKey{}, ErrInvalidVirtualKey
	}
	if model != "" && len(virtualKey.Models) > 0 && !contains(virtualKey.Models, model) {
		return VirtualKey{}, ErrInvalidVirtualKey
	}
	return virtualKey, nil
}

func HashKey(rawKey string) string {
	hash := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(hash[:])
}

func contains(models []string, expected string) bool {
	for _, model := range models {
		if model == expected {
			return true
		}
	}
	return false
}
