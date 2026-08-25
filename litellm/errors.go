package litellm

import "fmt"

type RateLimitErrorCategory string

const (
	VendorRateLimit       RateLimitErrorCategory = "vendor_rate_limit"
	VendorBatchRateLimit  RateLimitErrorCategory = "vendor_batch_rate_limit"
	LiteLLMRateLimit      RateLimitErrorCategory = "litellm_rate_limit"
	LiteLLMBatchRateLimit RateLimitErrorCategory = "litellm_batch_rate_limit"
)

type RateLimitType string

const (
	RateLimitRequests           RateLimitType = "requests"
	RateLimitTokens             RateLimitType = "tokens"
	RateLimitConcurrentRequests RateLimitType = "concurrent_requests"
	RateLimitBudget             RateLimitType = "budget"
	RateLimitMaxIterations      RateLimitType = "max_iterations"
)

func (category RateLimitErrorCategory) Valid() bool {
	switch category {
	case VendorRateLimit, VendorBatchRateLimit, LiteLLMRateLimit, LiteLLMBatchRateLimit:
		return true
	default:
		return false
	}
}

func (limitType RateLimitType) Valid() bool {
	switch limitType {
	case RateLimitRequests, RateLimitTokens, RateLimitConcurrentRequests, RateLimitBudget, RateLimitMaxIterations:
		return true
	default:
		return false
	}
}

type Error struct {
	StatusCode int
	Message    string
	Provider   string
	Model      string
	Category   RateLimitErrorCategory
	LimitType  RateLimitType
}

func (errorValue Error) Error() string {
	return fmt.Sprintf("litellm.Error: %s", errorValue.Message)
}
