package litellm

import (
	"context"
	"testing"
)

func TestInternalCallContext(t *testing.T) {
	ctx := context.Background()
	if IsInternalCall(ctx) {
		t.Fatal("background context must not be an internal call")
	}
	if !IsInternalCall(WithInternalCall(ctx)) {
		t.Fatal("internal call context marker was not retained")
	}
}
