package observability

import (
	"context"
	"net/http"
)

type traceparentKey struct{}

func WithTraceparent(ctx context.Context, traceparent string) context.Context {
	return context.WithValue(ctx, traceparentKey{}, traceparent)
}

func Traceparent(ctx context.Context) string {
	traceparent, _ := ctx.Value(traceparentKey{}).(string)
	return traceparent
}

func ApplyTraceparent(ctx context.Context, header http.Header) {
	if traceparent := Traceparent(ctx); traceparent != "" {
		header.Set("traceparent", traceparent)
	}
}

func ApplyRequestMetadata(ctx context.Context, header http.Header) {
	if requestID := RequestID(ctx); requestID != "" {
		header.Set("X-Request-Id", requestID)
	}
	ApplyTraceparent(ctx, header)
}
