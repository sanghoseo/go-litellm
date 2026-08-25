package redis

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func TestCacheRateLimitAndLock(t *testing.T) {
	server := miniredis.RunT(t)
	client, err := New(server.Addr())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer client.Close()
	ctx := context.Background()

	if err := client.Set(ctx, "cache:key", []byte("value"), time.Minute); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	value, err := client.Get(ctx, "cache:key")
	if err != nil || string(value) != "value" {
		t.Fatalf("Get() = %q, %v", value, err)
	}
	if _, err := client.Get(ctx, "cache:missing"); !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("missing cache error = %v", err)
	}
	for request := 0; request < 3; request++ {
		allowed, err := client.Allow(ctx, "limit:key", 2, time.Minute)
		if err != nil || allowed != (request < 2) {
			t.Fatalf("Allow(%d) = %t, %v", request, allowed, err)
		}
	}
	acquired, err := client.AcquireLock(ctx, "lock:key", "owner", time.Minute)
	if err != nil || !acquired {
		t.Fatalf("AcquireLock() = %t, %v", acquired, err)
	}
	released, err := client.ReleaseLock(ctx, "lock:key", "owner")
	if err != nil || !released {
		t.Fatalf("ReleaseLock() = %t, %v", released, err)
	}
}
