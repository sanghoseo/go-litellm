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
	TokenHash          string
	Models             []string
	UserID             string
	UserModels         []string
	TeamID             string
	TeamModels         []string
	ProjectID          string
	ProjectModels      []string
	OrganizationID     string
	OrganizationModels []string
	BudgetID           string
	ExpiresAt          *time.Time
	Blocked            bool
	RPMLimit           *int64
}

type ManagedVirtualKey struct {
	TokenHash      string
	KeyAlias       string
	Models         []string
	UserID         string
	TeamID         string
	TeamModels     []string
	ProjectID      string
	OrganizationID string
	BudgetID       string
	ExpiresAt      *time.Time
	Blocked        bool
	RPMLimit       *int64
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

type ManagedTeamUpdate struct {
	TeamAlias *string
	Admins    *[]string
	Members   *[]string
	Models    *[]string
	Blocked   *bool
}

type TeamManager interface {
	CreateTeam(context.Context, ManagedTeam) error
	GetTeam(context.Context, string) (ManagedTeam, error)
	UpdateTeam(context.Context, string, ManagedTeamUpdate) (bool, error)
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

type ManagedUserUpdate struct {
	UserAlias *string
	TeamID    *string
	UserEmail *string
	Models    *[]string
	Blocked   *bool
}

type UserManager interface {
	CreateUser(context.Context, ManagedUser) error
	GetUser(context.Context, string) (ManagedUser, error)
	UpdateUser(context.Context, string, ManagedUserUpdate) (bool, error)
	ListUsers(context.Context, int) ([]ManagedUser, error)
	SetUserBlocked(context.Context, string, bool) (bool, error)
	DeleteUser(context.Context, string) (bool, error)
}

type ManagedProject struct {
	ProjectID    string
	ProjectAlias string
	Description  string
	TeamID       string
	BudgetID     string
	Models       []string
	Blocked      bool
}

type ManagedOrganization struct {
	OrganizationID    string
	OrganizationAlias string
	BudgetID          string
	Models            []string
	Blocked           bool
}

type ManagedBudget struct {
	BudgetID            string     `json:"budget_id"`
	MaxBudget           *float64   `json:"max_budget,omitempty"`
	SoftBudget          *float64   `json:"soft_budget,omitempty"`
	MaxParallelRequests *int       `json:"max_parallel_requests,omitempty"`
	TPMLimit            *int64     `json:"tpm_limit,omitempty"`
	RPMLimit            *int64     `json:"rpm_limit,omitempty"`
	BudgetDuration      string     `json:"budget_duration,omitempty"`
	BudgetResetAt       *time.Time `json:"budget_reset_at,omitempty"`
}

type ManagedBudgetUpdate struct {
	MaxBudget           *float64
	SoftBudget          *float64
	MaxParallelRequests *int
	TPMLimit            *int64
	RPMLimit            *int64
	BudgetDuration      *string
	BudgetResetAt       *time.Time
}

type BudgetManager interface {
	CreateBudget(context.Context, ManagedBudget) error
	GetBudget(context.Context, string) (ManagedBudget, error)
	ListBudgets(context.Context, int) ([]ManagedBudget, error)
	UpdateBudget(context.Context, string, ManagedBudgetUpdate) (bool, error)
	DeleteBudget(context.Context, string) (bool, error)
}

type ManagedOrganizationUpdate struct {
	OrganizationAlias *string
	BudgetID          *string
	Models            *[]string
	Blocked           *bool
}

type OrganizationManager interface {
	CreateOrganization(context.Context, ManagedOrganization) error
	GetOrganization(context.Context, string) (ManagedOrganization, error)
	ListOrganizations(context.Context, int) ([]ManagedOrganization, error)
	UpdateOrganization(context.Context, string, ManagedOrganizationUpdate) (bool, error)
	DeleteOrganization(context.Context, string) (bool, error)
}

type ManagedProjectUpdate struct {
	ProjectAlias *string
	Description  *string
	TeamID       *string
	BudgetID     *string
	Models       *[]string
	Blocked      *bool
}

type ProjectManager interface {
	CreateProject(context.Context, ManagedProject) error
	GetProject(context.Context, string) (ManagedProject, error)
	UpdateProject(context.Context, string, ManagedProjectUpdate) (bool, error)
	ListProjects(context.Context, int) ([]ManagedProject, error)
	SetProjectBlocked(context.Context, string, bool) (bool, error)
	DeleteProject(context.Context, string) (bool, error)
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
	if model != "" && !AllowsModel(virtualKey, model) {
		return VirtualKey{}, ErrInvalidVirtualKey
	}
	return virtualKey, nil
}

func AllowsModel(key VirtualKey, model string) bool {
	return (len(key.Models) == 0 || contains(key.Models, model)) &&
		(len(key.UserModels) == 0 || contains(key.UserModels, model)) &&
		(len(key.TeamModels) == 0 || contains(key.TeamModels, model)) &&
		(len(key.ProjectModels) == 0 || contains(key.ProjectModels, model)) &&
		(len(key.OrganizationModels) == 0 || contains(key.OrganizationModels, model))
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
