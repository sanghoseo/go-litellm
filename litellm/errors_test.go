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
