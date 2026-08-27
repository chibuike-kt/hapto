package auth_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/chibuike-kt/hapto-api/internal/auth"
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

func TestLockoutTracker_LocksAfterMaxFailuresAndClearsOnReset(t *testing.T) {
	rdb := openTestRedis(t)
	tracker := auth.NewLockoutTracker(rdb)
	userID := uuid.NewString()
	ctx := context.Background()
	t.Cleanup(func() { _ = tracker.Reset(context.Background(), userID) })

	for i := range 4 {
		if err := tracker.RecordFailure(ctx, userID); err != nil {
			t.Fatalf("record failure %d: %v", i, err)
		}
		locked, _, err := tracker.IsLocked(ctx, userID)
		if err != nil {
			t.Fatalf("is locked: %v", err)
		}
		if locked {
			t.Fatalf("expected account not locked after %d failures", i+1)
		}
	}

	if err := tracker.RecordFailure(ctx, userID); err != nil {
		t.Fatalf("record 5th failure: %v", err)
	}

	locked, retryAfter, err := tracker.IsLocked(ctx, userID)
	if err != nil {
		t.Fatalf("is locked: %v", err)
	}
	if !locked {
		t.Fatal("expected account to be locked after 5 failures")
	}
	if retryAfter <= 0 || retryAfter > 15*time.Minute {
		t.Fatalf("unexpected retry-after: %v", retryAfter)
	}

	if err := tracker.Reset(ctx, userID); err != nil {
		t.Fatalf("reset: %v", err)
	}
	locked, _, err = tracker.IsLocked(ctx, userID)
	if err != nil {
		t.Fatalf("is locked: %v", err)
	}
	if locked {
		t.Fatal("expected lock to be cleared after reset")
	}
}

func TestLockoutTracker_UnknownAccountNotLocked(t *testing.T) {
	rdb := openTestRedis(t)
	tracker := auth.NewLockoutTracker(rdb)

	locked, _, err := tracker.IsLocked(context.Background(), uuid.NewString())
	if err != nil {
		t.Fatalf("is locked: %v", err)
	}
	if locked {
		t.Fatal("expected an account with no recorded failures not to be locked")
	}
}
