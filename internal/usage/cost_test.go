package usage

import (
	"math"
	"testing"

	proxytpes "github.com/BerriAI/litellm/go-proxy/pkg/types"
)

func TestCostLookupResolvesProviderPrefixedModel(t *testing.T) {
	cost := NewCostCalculator().Lookup("openai/gpt-4o-mini")
	if !cost.Priced() {
		t.Fatalf("expected priced model, got %+v", cost)
	}
	if math.Abs(cost.InputCost-1.5e-07) > 1e-15 || math.Abs(cost.OutputCost-6e-07) > 1e-15 {
		t.Fatalf("unexpected prices: %+v", cost)
	}
}

func TestCostLookupResolvesBareModel(t *testing.T) {
	cost := NewCostCalculator().Lookup("gpt-4o-mini")
	if !cost.Priced() {
		t.Fatalf("expected priced model, got %+v", cost)
	}
}

func TestCostLookupIsCaseInsensitive(t *testing.T) {
	prefixed := NewCostCalculator().Lookup("openai/gpt-4o-mini")
	upper := NewCostCalculator().Lookup("gpt-4o-MINI")
	if upper.InputCost != prefixed.InputCost || upper.OutputCost != prefixed.OutputCost {
		t.Fatalf("case-insensitive mismatch: %+v vs %+v", upper, prefixed)
	}
}

func TestCostLookupUnknownModelIsUnpriced(t *testing.T) {
	cost := NewCostCalculator().Lookup("openai/definitely-not-a-real-model-12345")
	if cost.Priced() {
		t.Fatalf("expected unpriced model, got %+v", cost)
	}
}

func TestCostTotal(t *testing.T) {
	cost := Cost{InputCost: 1.5e-07, OutputCost: 6e-07}
	total := cost.Total(proxytpes.Usage{PromptTokens: 1000, CompletionTokens: 500, TotalTokens: 1500})
	expected := 1000*1.5e-07 + 500*6e-07
	if math.Abs(total-expected) > 1e-18 {
		t.Fatalf("total = %v, want %v", total, expected)
	}
}

func TestCostTotalAppliesCacheReadPrice(t *testing.T) {
	cost := Cost{InputCost: 1.5e-07, OutputCost: 6e-07, CacheReadCost: 7.5e-08}
	total := cost.Total(proxytpes.Usage{
		PromptTokens:        1000,
		CompletionTokens:    500,
		TotalTokens:         1500,
		PromptTokensDetails: &proxytpes.PromptTokensDetails{CachedTokens: 400},
	})
	expected := 600*1.5e-07 + 400*7.5e-08 + 500*6e-07
	if math.Abs(total-expected) > 1e-18 {
		t.Fatalf("total = %v, want %v", total, expected)
	}
}

func TestCostTotalUnpricedIsZero(t *testing.T) {
	if (Cost{}).Total(proxytpes.Usage{PromptTokens: 100, CompletionTokens: 100}) != 0 {
		t.Fatal("unpriced cost must total zero")
	}
}
