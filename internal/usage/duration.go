package usage

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var durationPattern = regexp.MustCompile(`^(\d+)(mo|[smhdw]?)$`)

var budgetDurationAliases = map[string]string{
	"hourly":  "1h",
	"daily":   "24h",
	"weekly":  "7d",
	"monthly": "30d",
}

const monthSeconds = 30 * 24 * time.Hour

func BudgetDurationSeconds(duration string) (time.Duration, error) {
	value := strings.ToLower(strings.TrimSpace(duration))
	if alias, ok := budgetDurationAliases[value]; ok {
		value = alias
	}
	match := durationPattern.FindStringSubmatch(value)
	if match == nil {
		return 0, fmt.Errorf("invalid budget duration %q", duration)
	}
	amount, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, fmt.Errorf("invalid budget duration %q", duration)
	}
	switch match[2] {
	case "s", "":
		return time.Duration(amount) * time.Second, nil
	case "m":
		return time.Duration(amount) * time.Minute, nil
	case "h":
		return time.Duration(amount) * time.Hour, nil
	case "d":
		return time.Duration(amount) * 24 * time.Hour, nil
	case "w":
		return time.Duration(amount) * 7 * 24 * time.Hour, nil
	case "mo":
		return time.Duration(amount) * monthSeconds, nil
	}
	return 0, fmt.Errorf("invalid budget duration %q", duration)
}
