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

// APIError is the common error payload returned by providers and the proxy.
// It intentionally carries the same provider/model/retry metadata used by the
// Python LiteLLM exception hierarchy while remaining idiomatic for errors.As.
type APIError struct {
	StatusCode int
	Message    string
	Provider   string
	Model      string
	Category   RateLimitErrorCategory
	LimitType  RateLimitType
}

func (errorValue APIError) Error() string {
	return fmt.Sprintf("litellm.APIError: %s", errorValue.Message)
}

type AuthenticationError struct{ APIError }
type BadRequestError struct{ APIError }
type NotFoundError struct{ APIError }
type PermissionDeniedError struct{ APIError }
type UnprocessableEntityError struct{ APIError }
type RateLimitError struct{ APIError }
type ServiceUnavailableError struct{ APIError }
type BadGatewayError struct{ APIError }
type InternalServerError struct{ APIError }
type APIConnectionError struct{ APIError }
type TimeoutError struct{ APIError }

func NewAuthenticationError(message, provider, model string) AuthenticationError {
	return AuthenticationError{APIError: APIError{StatusCode: 401, Message: message, Provider: provider, Model: model}}
}

func NewBadRequestError(message, provider, model string) BadRequestError {
	return BadRequestError{APIError: APIError{StatusCode: 400, Message: message, Provider: provider, Model: model}}
}

func NewNotFoundError(message, provider, model string) NotFoundError {
	return NotFoundError{APIError: APIError{StatusCode: 404, Message: message, Provider: provider, Model: model}}
}

func NewRateLimitError(message, provider, model string, category RateLimitErrorCategory, limitType RateLimitType) RateLimitError {
	return RateLimitError{APIError: APIError{StatusCode: 429, Message: message, Provider: provider, Model: model, Category: category, LimitType: limitType}}
}

func NewServiceUnavailableError(message, provider, model string) ServiceUnavailableError {
	return ServiceUnavailableError{APIError: APIError{StatusCode: 503, Message: message, Provider: provider, Model: model}}
}
