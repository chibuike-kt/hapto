package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// PendingLoginStore holds the short-lived state between a password check
// that requires a TOTP second factor and the verify-totp call that
// completes it. It never holds a full session.
type PendingLoginStore struct {
	rdb *redis.Client
	ttl time.Duration
}

func NewPendingLoginStore(rdb *redis.Client) *PendingLoginStore {
	return &PendingLoginStore{rdb: rdb, ttl: 5 * time.Minute}
}

func pendingLoginKey(id string) string {
	return fmt.Sprintf("auth:pending-login:%s", id)
}

func (s *PendingLoginStore) Create(ctx context.Context, userID string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate pending login id: %w", err)
	}
	id := base64.RawURLEncoding.EncodeToString(raw)

	if err := s.rdb.Set(ctx, pendingLoginKey(id), userID, s.ttl).Err(); err != nil {
		return "", err
	}
	return id, nil
}

func (s *PendingLoginStore) Get(ctx context.Context, id string) (string, error) {
	userID, err := s.rdb.Get(ctx, pendingLoginKey(id)).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrPendingLoginNotFound
	}
	if err != nil {
		return "", err
	}
	return userID, nil
}

func (s *PendingLoginStore) Delete(ctx context.Context, id string) error {
	return s.rdb.Del(ctx, pendingLoginKey(id)).Err()
}
