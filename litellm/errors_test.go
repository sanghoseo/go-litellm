package litellm

import "testing"

func TestRateLimitValuesValidate(t *testing.T) {
	if !LiteLLMRateLimit.Valid() || !RateLimitTokens.Valid() {
		t.Fatal("known rate limit values must validate")
	}
	if RateLimitErrorCategory("unknown").Valid() || RateLimitType("unknown").Valid() {
		t.Fatal("unknown rate limit values must not validate")
	}
}

func TestTypedAPIErrorsPreserveHTTPMetadata(t *testing.T) {
	auth := NewAuthenticationError("bad key", "openai", "gpt-test")
	if auth.StatusCode != 401 || auth.Provider != "openai" || auth.Model != "gpt-test" {
		t.Fatalf("authentication error = %#v", auth)
	}
	rateLimit := NewRateLimitError("slow down", "openai", "gpt-test", LiteLLMRateLimit, RateLimitRequests)
	if rateLimit.StatusCode != 429 || rateLimit.Category != LiteLLMRateLimit || rateLimit.LimitType != RateLimitRequests {
		t.Fatalf("rate-limit error = %#v", rateLimit)
	}
	if NewServiceUnavailableError("unavailable", "openai", "gpt-test").StatusCode != 503 {
		t.Fatal("service unavailable error must use HTTP 503")
	}
}
