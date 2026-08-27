// Package ratelimit throttles how often an action can be attempted, keyed
// by an arbitrary caller-chosen string (an IP, an email, or both). It's
// deliberately separate from account lockout: this slows down attempt
// frequency, it never blocks an account outright.
package ratelimit

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type Limiter struct {
	rdb *redis.Client
	// base is the backoff after the first throttled attempt; it doubles
	// per consecutive attempt up to max.
	base time.Duration
	max  time.Duration
}

func NewLimiter(rdb *redis.Client, base, max time.Duration) *Limiter {
	return &Limiter{rdb: rdb, base: base, max: max}
}

func limiterKey(key string) string {
	return fmt.Sprintf("ratelimit:%s", key)
}

// Allow records an attempt for key and reports whether it's allowed right
// now. A denied attempt still isn't counted again until the backoff clears,
// so retrying early doesn't push the backoff out further.
func (l *Limiter) Allow(ctx context.Context, key string) (allowed bool, retryAfter time.Duration, err error) {
	k := limiterKey(key)

	data, err := l.rdb.HGetAll(ctx, k).Result()
	if err != nil {
		return false, 0, err
	}

	now := time.Now()
	attempts := 0
	var nextAllowed time.Time
	if len(data) > 0 {
		attempts, _ = strconv.Atoi(data["attempts"])
		if ms, err := strconv.ParseInt(data["next_allowed_ms"], 10, 64); err == nil {
			nextAllowed = time.UnixMilli(ms)
		}
	}

	if now.Before(nextAllowed) {
		return false, time.Until(nextAllowed), nil
	}

	attempts++
	backoff := min(l.base*time.Duration(1<<min(attempts-1, 20)), l.max)

	pipe := l.rdb.TxPipeline()
	pipe.HSet(ctx, k, "attempts", attempts, "next_allowed_ms", now.Add(backoff).UnixMilli())
	pipe.Expire(ctx, k, l.max*2)
	if _, err := pipe.Exec(ctx); err != nil {
		return false, 0, err
	}

	return true, 0, nil
}

// Reset clears the backoff state for key, used by tests that need a clean
// slate between cases sharing a Redis instance.
func (l *Limiter) Reset(ctx context.Context, key string) error {
	return l.rdb.Del(ctx, limiterKey(key)).Err()
}
