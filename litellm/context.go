package litellm

import "context"

type internalCallContextKey struct{}

func WithInternalCall(ctx context.Context) context.Context {
	return context.WithValue(ctx, internalCallContextKey{}, true)
}

func IsInternalCall(ctx context.Context) bool {
	value, _ := ctx.Value(internalCallContextKey{}).(bool)
	return value
}
