package ratelimit_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/chibuike-kt/hapto-api/internal/ratelimit"
)

func openTestRedis(t *testing.T) *redis.Client {
	t.Helper()

	url := os.Getenv("REDIS_URL")
	if url == "" {
		url = "redis://localhost:6379/0"
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatalf("parse redis url: %v", err)
	}

	rdb := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("redis not available: %v", err)
	}
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

func TestLimiter_FirstAttemptAllowedSecondThrottled(t *testing.T) {
	rdb := openTestRedis(t)
	limiter := ratelimit.NewLimiter(rdb, time.Second, time.Minute)
	key := "test:" + uuid.NewString()
	ctx := context.Background()
	t.Cleanup(func() { _ = limiter.Reset(context.Background(), key) })

	allowed, _, err := limiter.Allow(ctx, key)
	if err != nil {
		t.Fatalf("first allow: %v", err)
	}
	if !allowed {
		t.Fatal("expected the first attempt to be allowed")
	}

	allowed, retryAfter, err := limiter.Allow(ctx, key)
	if err != nil {
		t.Fatalf("second allow: %v", err)
	}
	if allowed {
		t.Fatal("expected the immediate second attempt to be throttled")
	}
	if retryAfter <= 0 {
		t.Fatalf("expected a positive retry-after, got %v", retryAfter)
	}
}

func TestLimiter_BackoffGrowsWithRepeatedAttempts(t *testing.T) {
	rdb := openTestRedis(t)
	limiter := ratelimit.NewLimiter(rdb, 100*time.Millisecond, time.Minute)
	key := "test:" + uuid.NewString()
	ctx := context.Background()
	t.Cleanup(func() { _ = limiter.Reset(context.Background(), key) })

	if _, _, err := limiter.Allow(ctx, key); err != nil {
		t.Fatalf("attempt 1: %v", err)
	}

	time.Sleep(120 * time.Millisecond)
	if _, _, err := limiter.Allow(ctx, key); err != nil {
		t.Fatalf("attempt 2: %v", err)
	}

	// Attempt 3 arrives immediately after attempt 2's shorter backoff
	// already cleared once; the backoff has now doubled, so it should be
	// throttled again with a longer wait.
	allowed, retryAfter, err := limiter.Allow(ctx, key)
	if err != nil {
		t.Fatalf("attempt 3: %v", err)
	}
	if allowed {
		t.Fatal("expected attempt 3 to be throttled by the doubled backoff")
	}
	if retryAfter < 150*time.Millisecond {
		t.Fatalf("expected backoff to have grown past the base, got %v", retryAfter)
	}
}

func TestLimiter_ResetClearsBackoff(t *testing.T) {
	rdb := openTestRedis(t)
	limiter := ratelimit.NewLimiter(rdb, time.Second, time.Minute)
	key := "test:" + uuid.NewString()
	ctx := context.Background()

	if _, _, err := limiter.Allow(ctx, key); err != nil {
		t.Fatalf("first allow: %v", err)
	}
	if allowed, _, _ := limiter.Allow(ctx, key); allowed {
		t.Fatal("expected the second attempt to be throttled before reset")
	}

	if err := limiter.Reset(ctx, key); err != nil {
		t.Fatalf("reset: %v", err)
	}

	allowed, _, err := limiter.Allow(ctx, key)
	if err != nil {
		t.Fatalf("allow after reset: %v", err)
	}
	if !allowed {
		t.Fatal("expected a fresh attempt to be allowed after reset")
	}
}
