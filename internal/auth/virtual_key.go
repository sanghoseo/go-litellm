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

type ManagedVirtualKeyUpdate struct {
	KeyAlias  *string
	Models    *[]string
	ExpiresAt *time.Time
	RPMLimit  *int64
}

type VirtualKeyManager interface {
	CreateVirtualKey(context.Context, ManagedVirtualKey) error
	GetVirtualKey(context.Context, string) (ManagedVirtualKey, error)
	DeleteVirtualKey(context.Context, string) (bool, error)
	SetVirtualKeyBlocked(context.Context, string, bool) (bool, error)
	UpdateVirtualKey(context.Context, string, ManagedVirtualKeyUpdate) (bool, error)
	ListVirtualKeys(context.Context, int) ([]ManagedVirtualKey, error)
	RegenerateVirtualKey(context.Context, string, string) (ManagedVirtualKey, error)
}

type ManagedTeam struct {
	TeamID    string
	TeamAlias string
	Admins    []string
	Members   []string
	Models    []string
	Blocked   bool
}

type TeamManager interface {
	CreateTeam(context.Context, ManagedTeam) error
	GetTeam(context.Context, string) (ManagedTeam, error)
	ListTeams(context.Context, int) ([]ManagedTeam, error)
	SetTeamBlocked(context.Context, string, bool) (bool, error)
	DeleteTeam(context.Context, string) (bool, error)
}

type ManagedUser struct {
	UserID    string
	UserAlias string
	TeamID    string
	UserEmail string
	Models    []string
	Blocked   bool
}

type UserManager interface {
	CreateUser(context.Context, ManagedUser) error
	GetUser(context.Context, string) (ManagedUser, error)
	ListUsers(context.Context, int) ([]ManagedUser, error)
	SetUserBlocked(context.Context, string, bool) (bool, error)
	DeleteUser(context.Context, string) (bool, error)
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
