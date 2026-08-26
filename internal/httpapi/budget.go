package httpapi

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/BerriAI/litellm/go-proxy/internal/auth"
	"github.com/BerriAI/litellm/go-proxy/internal/store/redis"
	"github.com/BerriAI/litellm/go-proxy/internal/usage"
)

var ErrBudgetExceeded = errors.New("budget exceeded")

type BudgetExceededError struct {
	CurrentCost float64
	MaxBudget   float64
}

func (err *BudgetExceededError) Error() string {
	return fmt.Sprintf("Budget has been exceeded! Current cost: %v, Max budget: %v", err.CurrentCost, err.MaxBudget)
}

func (err *BudgetExceededError) Unwrap() error { return ErrBudgetExceeded }

type SpendCounter interface {
	Get(ctx context.Context, key string) ([]byte, error)
	IncrByFloat(ctx context.Context, key string, amount float64) (float64, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
}

type SpendSince interface {
	SpendSince(ctx context.Context, keyHash string, since time.Time) (float64, error)
}

type BudgetRateLimiter interface {
	Allow(ctx context.Context, key string, limit int64, window time.Duration) (bool, error)
}

type BudgetEnforcer struct {
	counter SpendCounter
	limiter BudgetRateLimiter
	budgets auth.BudgetManager
	spend   SpendSince
}

func NewBudgetEnforcer(counter SpendCounter, limiter BudgetRateLimiter, budgets auth.BudgetManager, spend SpendSince) *BudgetEnforcer {
	return &BudgetEnforcer{counter: counter, limiter: limiter, budgets: budgets, spend: spend}
}

func (enforcer *BudgetEnforcer) Authorize(ctx context.Context, keyHash string, budgetID string, model string) error {
	if enforcer == nil || keyHash == "" || budgetID == "" || enforcer.budgets == nil {
		return nil
	}
	budget, err := enforcer.budgets.GetBudget(ctx, budgetID)
	if err != nil || !validBudgetLimit(budget.MaxBudget) {
		return nil
	}
	spend, err := enforcer.currentSpend(ctx, keyHash, budget.BudgetDuration)
	if err != nil {
		return nil
	}
	if spend >= *budget.MaxBudget {
		return &BudgetExceededError{CurrentCost: spend, MaxBudget: *budget.MaxBudget}
	}
	if enforcer.limiter != nil && validPositiveInt64(budget.RPMLimit) {
		allowed, err := enforcer.limiter.Allow(ctx, "litellm:budget:rpm:"+keyHash+":"+model, *budget.RPMLimit, time.Minute)
		if err != nil {
			return nil
		}
		if !allowed {
			return ErrBudgetExceeded
		}
	}
	return nil
}

func (enforcer *BudgetEnforcer) currentSpend(ctx context.Context, keyHash string, duration string) (float64, error) {
	key := spendCounterKey(keyHash, duration)
	raw, err := enforcer.counter.Get(ctx, key)
	if err != nil && !errors.Is(err, redis.ErrCacheMiss) {
		return 0, err
	}
	if err == nil && len(raw) > 0 {
		if value, convErr := strconv.ParseFloat(string(raw), 64); convErr == nil {
			return value, nil
		}
	}
	if duration != "" && enforcer.spend != nil {
		window, parseErr := usage.BudgetDurationSeconds(duration)
		if parseErr == nil {
			seed, seedErr := enforcer.spend.SpendSince(ctx, keyHash, time.Now().UTC().Add(-window))
			if seedErr == nil && seed > 0 {
				_ = enforcer.counter.Set(ctx, key, []byte(strconv.FormatFloat(seed, 'f', -1, 64)), window)
				return seed, nil
			}
		}
	}
	return 0, nil
}

func (enforcer *BudgetEnforcer) RecordSpend(ctx context.Context, keyHash string, budgetID string, spend float64) {
	if enforcer == nil || keyHash == "" || budgetID == "" || spend <= 0 || enforcer.budgets == nil {
		return
	}
	budget, err := enforcer.budgets.GetBudget(ctx, budgetID)
	if err != nil {
		return
	}
	_, _ = enforcer.counter.IncrByFloat(ctx, spendCounterKey(keyHash, budget.BudgetDuration), spend)
}

func spendCounterKey(keyHash string, duration string) string {
	if duration == "" {
		return "spend:key:" + keyHash
	}
	return "spend:key:" + keyHash + ":window:" + duration
}

func validBudgetLimit(value *float64) bool {
	return value != nil && *value >= 0
}

func validPositiveInt64(value *int64) bool {
	return value != nil && *value > 0
}
