package usage

import (
	"strings"
	"unicode"

	proxytpes "github.com/BerriAI/litellm/go-proxy/pkg/types"
)

type Cost struct {
	InputCost     float64
	OutputCost    float64
	BatchInput    float64
	BatchOutput   float64
	CacheReadCost float64
	MaxInput      float64
	MaxOutput     float64
}

func (cost Cost) Priced() bool {
	return cost.InputCost != 0 || cost.OutputCost != 0
}

func (cost Cost) Total(usage proxytpes.Usage) float64 {
	if !cost.Priced() {
		return 0
	}
	input := float64(usage.PromptTokens)
	cached := float64(0)
	if usage.PromptTokensDetails != nil {
		cached = float64(usage.PromptTokensDetails.CachedTokens)
	}
	if cost.CacheReadCost > 0 && cached > 0 && cached <= input {
		return (input-cached)*cost.InputCost + cached*cost.CacheReadCost + float64(usage.CompletionTokens)*cost.OutputCost
	}
	return input*cost.InputCost + float64(usage.CompletionTokens)*cost.OutputCost
}

type CostCalculator struct{}

func NewCostCalculator() CostCalculator { return CostCalculator{} }

func (CostCalculator) Lookup(model string) Cost {
	for _, candidate := range costKeyCandidates(model) {
		if cost, ok := lookupModelCost(candidate); ok {
			return Cost{
				InputCost:     cost[inputCostIndex],
				OutputCost:    cost[outputCostIndex],
				BatchInput:    cost[batchInputIndex],
				BatchOutput:   cost[batchOutputIndex],
				CacheReadCost: cost[cacheReadIndex],
				MaxInput:      cost[maxInputIndex],
				MaxOutput:     cost[maxOutputIndex],
			}
		}
	}
	return Cost{}
}

func costKeyCandidates(model string) []string {
	candidates := []string{}
	if model == "" {
		return candidates
	}
	base := model
	if slash := strings.LastIndex(model, "/"); slash >= 0 {
		base = model[slash+1:]
	}
	seen := map[string]struct{}{}
	appendUnique := func(candidate string) {
		if candidate == "" {
			return
		}
		if _, ok := seen[candidate]; ok {
			return
		}
		seen[candidate] = struct{}{}
		candidates = append(candidates, candidate)
	}
	appendUnique(model)
	appendUnique(base)
	appendUnique(strings.ToLower(model))
	appendUnique(strings.ToLower(base))
	appendUnique(stripVersionSuffix(base))
	appendUnique(strings.ToLower(stripVersionSuffix(base)))
	return candidates
}

func stripVersionSuffix(model string) string {
	for _, marker := range []string{"-latest", "-preview", "-alpha", "-beta"} {
		if strings.HasSuffix(model, marker) {
			return strings.TrimSuffix(model, marker)
		}
	}
	if cut := strings.LastIndex(model, "-"); cut > 0 {
		suffix := model[cut+1:]
		if isVersionSuffix(suffix) {
			return model[:cut]
		}
	}
	return model
}

func isVersionSuffix(suffix string) bool {
	if suffix == "" {
		return false
	}
	digits := 0
	for _, char := range suffix {
		if unicode.IsDigit(char) {
			digits++
			continue
		}
		if char != '-' && char != '.' {
			return false
		}
	}
	return digits >= 4
}
