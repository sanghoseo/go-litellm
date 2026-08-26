package usage

import (
	"testing"
	"time"
)

func TestBudgetDurationSeconds(t *testing.T) {
	cases := map[string]time.Duration{
		"1s":     time.Second,
		"30s":    30 * time.Second,
		"5m":     5 * time.Minute,
		"2h":     2 * time.Hour,
		"1d":     24 * time.Hour,
		"1w":     7 * 24 * time.Hour,
		"1mo":    30 * 24 * time.Hour,
		"30d":    30 * 24 * time.Hour,
		"hourly": time.Hour,
		"daily":  24 * time.Hour,
	}
	for input, want := range cases {
		got, err := BudgetDurationSeconds(input)
		if err != nil {
			t.Fatalf("BudgetDurationSeconds(%q) error = %v", input, err)
		}
		if got != want {
			t.Fatalf("BudgetDurationSeconds(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestBudgetDurationSecondsRejectsInvalid(t *testing.T) {
	for _, input := range []string{"", "abc", "5x", "-1d", "1.5d"} {
		if _, err := BudgetDurationSeconds(input); err == nil {
			t.Fatalf("BudgetDurationSeconds(%q) expected error", input)
		}
	}
}
