package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// LockoutTracker locks an account after too many failed login attempts,
// independent of which IP they came from and independent of the per-IP/
// per-email rate limiting in internal/ratelimit — that throttles attempt
// frequency, this locks the account outright.
type LockoutTracker struct {
	rdb          *redis.Client
	maxFailures  int64
	window       time.Duration
	lockDuration time.Duration
}

func NewLockoutTracker(rdb *redis.Client) *LockoutTracker {
	return &LockoutTracker{
		rdb:          rdb,
		maxFailures:  5,
		window:       15 * time.Minute,
		lockDuration: 15 * time.Minute,
	}
}

func failuresKey(userID string) string {
	return fmt.Sprintf("auth:lockout:failures:%s", userID)
}

func lockKey(userID string) string {
	return fmt.Sprintf("auth:lockout:locked:%s", userID)
}

// IsLocked reports whether the account is currently locked, and if so, for
// how much longer.
func (t *LockoutTracker) IsLocked(ctx context.Context, userID string) (bool, time.Duration, error) {
	ttl, err := t.rdb.TTL(ctx, lockKey(userID)).Result()
	if err != nil {
		return false, 0, err
	}
	if ttl > 0 {
		return true, ttl, nil
	}
	return false, 0, nil
}

// RecordFailure counts one failed authentication attempt against the
// account. Once maxFailures accumulate inside the window, the account locks
// for lockDuration. Reports whether this call is the one that just
// triggered the lock, so callers can record a distinct audit event for it.
func (t *LockoutTracker) RecordFailure(ctx context.Context, userID string) (locked bool, err error) {
	key := failuresKey(userID)

	n, err := t.rdb.Incr(ctx, key).Result()
	if err != nil {
		return false, err
	}
	if n == 1 {
		if err := t.rdb.Expire(ctx, key, t.window).Err(); err != nil {
			return false, err
		}
	}

	if n >= t.maxFailures {
		if err := t.rdb.Set(ctx, lockKey(userID), "1", t.lockDuration).Err(); err != nil {
			return false, err
		}
		if err := t.rdb.Del(ctx, key).Err(); err != nil {
			return false, err
		}
		return true, nil
	}

	return false, nil
}

// Reset clears both the failure counter and any active lock, called on
// successful authentication. Reports whether there was anything to clear,
// so callers can skip recording a "lockout cleared" event on the common
// path where no failures had ever been recorded.
func (t *LockoutTracker) Reset(ctx context.Context, userID string) (cleared bool, err error) {
	n, err := t.rdb.Del(ctx, failuresKey(userID), lockKey(userID)).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
