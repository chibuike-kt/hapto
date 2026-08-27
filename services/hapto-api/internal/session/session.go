// Package session issues and validates opaque session tokens backed by
// Redis. A session has a sliding idle expiry (extended on every successful
// validation) and a hard max TTL measured from creation that no amount of
// activity extends past.
package session

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

var ErrNotFound = errors.New("session not found or expired")

type Session struct {
	ID        string     `json:"-"`
	UserID    string     `json:"user_id"`
	CreatedAt time.Time  `json:"created_at"`
	LastSeen  time.Time  `json:"last_seen"`
	StepUpAt  *time.Time `json:"step_up_at,omitempty"`
}

type Store struct {
	rdb     *redis.Client
	idleTTL time.Duration
	maxTTL  time.Duration
}

func NewStore(rdb *redis.Client, idleTTL, maxTTL time.Duration) *Store {
	return &Store{rdb: rdb, idleTTL: idleTTL, maxTTL: maxTTL}
}

func sessionKey(id string) string {
	return fmt.Sprintf("session:%s", id)
}

func userSessionsKey(userID string) string {
	return fmt.Sprintf("session:user:%s", userID)
}

func (s *Store) Create(ctx context.Context, userID string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	id := base64.RawURLEncoding.EncodeToString(raw)

	now := time.Now().UTC()
	sess := Session{UserID: userID, CreatedAt: now, LastSeen: now}
	body, err := json.Marshal(sess)
	if err != nil {
		return "", fmt.Errorf("marshal session: %w", err)
	}

	pipe := s.rdb.TxPipeline()
	pipe.Set(ctx, sessionKey(id), body, s.idleTTL)
	pipe.SAdd(ctx, userSessionsKey(userID), id)
	pipe.Expire(ctx, userSessionsKey(userID), s.maxTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return "", fmt.Errorf("store session: %w", err)
	}

	return id, nil
}

// Get validates a session token, enforcing the hard max TTL, and slides the
// idle expiry forward on success.
func (s *Store) Get(ctx context.Context, id string) (*Session, error) {
	body, err := s.rdb.Get(ctx, sessionKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	var sess Session
	if err := json.Unmarshal(body, &sess); err != nil {
		return nil, fmt.Errorf("unmarshal session: %w", err)
	}
	sess.ID = id

	if time.Since(sess.CreatedAt) > s.maxTTL {
		_ = s.deleteSession(ctx, id, sess.UserID)
		return nil, ErrNotFound
	}

	sess.LastSeen = time.Now().UTC()
	body, err = json.Marshal(sess)
	if err != nil {
		return nil, fmt.Errorf("marshal session: %w", err)
	}
	if err := s.rdb.Set(ctx, sessionKey(id), body, s.idleTTL).Err(); err != nil {
		return nil, fmt.Errorf("slide session expiry: %w", err)
	}

	return &sess, nil
}

func (s *Store) Delete(ctx context.Context, id string) error {
	body, err := s.rdb.Get(ctx, sessionKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil
	}
	if err != nil {
		return err
	}

	var sess Session
	if err := json.Unmarshal(body, &sess); err != nil {
		return fmt.Errorf("unmarshal session: %w", err)
	}

	return s.deleteSession(ctx, id, sess.UserID)
}

func (s *Store) deleteSession(ctx context.Context, id, userID string) error {
	pipe := s.rdb.TxPipeline()
	pipe.Del(ctx, sessionKey(id))
	pipe.SRem(ctx, userSessionsKey(userID), id)
	_, err := pipe.Exec(ctx)
	return err
}

// DeleteAllForUser invalidates every session belonging to a user. Used on
// password reset, per hapto's invariant that a reset must kill every
// existing session.
func (s *Store) DeleteAllForUser(ctx context.Context, userID string) error {
	ids, err := s.rdb.SMembers(ctx, userSessionsKey(userID)).Result()
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}

	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = sessionKey(id)
	}

	pipe := s.rdb.TxPipeline()
	pipe.Del(ctx, keys...)
	pipe.Del(ctx, userSessionsKey(userID))
	_, err = pipe.Exec(ctx)
	return err
}

// MarkStepUp records that the session just passed a step-up (re-auth)
// check. Nothing calls this yet — it's the write side of the mechanism
// RequireStepUp reads, ready for the first sensitive endpoint that needs it.
func (s *Store) MarkStepUp(ctx context.Context, id string) error {
	sess, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	sess.StepUpAt = &now

	body, err := json.Marshal(sess)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	return s.rdb.Set(ctx, sessionKey(id), body, s.idleTTL).Err()
}
