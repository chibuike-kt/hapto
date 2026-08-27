package session_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/chibuike-kt/hapto-api/internal/session"
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

func TestStore_CreateAndGet(t *testing.T) {
	rdb := openTestRedis(t)
	store := session.NewStore(rdb, time.Minute, time.Hour)
	ctx := context.Background()
	userID := uuid.NewString()

	token, err := store.Create(ctx, userID)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = store.Delete(context.Background(), token) })

	sess, err := store.Get(ctx, token)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if sess.UserID != userID {
		t.Fatalf("got user %q, want %q", sess.UserID, userID)
	}
}

func TestStore_GetSlidesIdleExpiry(t *testing.T) {
	rdb := openTestRedis(t)
	store := session.NewStore(rdb, 2*time.Second, time.Hour)
	ctx := context.Background()

	token, err := store.Create(ctx, uuid.NewString())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = store.Delete(context.Background(), token) })

	// Touch the session just before idle expiry would otherwise hit.
	time.Sleep(1200 * time.Millisecond)
	if _, err := store.Get(ctx, token); err != nil {
		t.Fatalf("get before expiry: %v", err)
	}

	time.Sleep(1200 * time.Millisecond)
	if _, err := store.Get(ctx, token); err != nil {
		t.Fatalf("expected the touch to have slid the expiry forward, got: %v", err)
	}
}

func TestStore_HardMaxTTLExpiresRegardlessOfActivity(t *testing.T) {
	rdb := openTestRedis(t)
	store := session.NewStore(rdb, 10*time.Second, time.Second)
	ctx := context.Background()

	token, err := store.Create(ctx, uuid.NewString())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = store.Delete(context.Background(), token) })

	time.Sleep(1200 * time.Millisecond)

	if _, err := store.Get(ctx, token); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("expected hard TTL to expire the session despite idle TTL headroom, got: %v", err)
	}
}

func TestStore_DeleteAllForUser(t *testing.T) {
	rdb := openTestRedis(t)
	store := session.NewStore(rdb, time.Minute, time.Hour)
	ctx := context.Background()
	userID := uuid.NewString()

	tokenA, err := store.Create(ctx, userID)
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	tokenB, err := store.Create(ctx, userID)
	if err != nil {
		t.Fatalf("create b: %v", err)
	}

	if err := store.DeleteAllForUser(ctx, userID); err != nil {
		t.Fatalf("delete all for user: %v", err)
	}

	if _, err := store.Get(ctx, tokenA); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("expected session a to be gone, got: %v", err)
	}
	if _, err := store.Get(ctx, tokenB); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("expected session b to be gone, got: %v", err)
	}
}

func TestRequireSession_MiddlewareRejectsMissingAndInvalidTokens(t *testing.T) {
	rdb := openTestRedis(t)
	store := session.NewStore(rdb, time.Minute, time.Hour)

	handlerCalled := false
	protected := store.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	rec := httptest.NewRecorder()
	protected.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing token: got status %d, want 401", rec.Code)
	}
	if handlerCalled {
		t.Fatal("handler must not run without a token")
	}

	req = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-token")
	rec = httptest.NewRecorder()
	protected.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("invalid token: got status %d, want 401", rec.Code)
	}
}

func TestRequireSession_MiddlewareAcceptsValidToken(t *testing.T) {
	rdb := openTestRedis(t)
	store := session.NewStore(rdb, time.Minute, time.Hour)
	userID := uuid.NewString()

	token, err := store.Create(context.Background(), userID)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = store.Delete(context.Background(), token) })

	var gotUserID string
	protected := store.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, ok := session.FromContext(r.Context())
		if !ok {
			t.Fatal("expected session in context")
		}
		gotUserID = sess.UserID
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	protected.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200", rec.Code)
	}
	if gotUserID != userID {
		t.Fatalf("got user %q, want %q", gotUserID, userID)
	}
}

func TestRequireStepUp_RejectsWithoutRecentStepUp(t *testing.T) {
	rdb := openTestRedis(t)
	store := session.NewStore(rdb, time.Minute, time.Hour)

	token, err := store.Create(context.Background(), uuid.NewString())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = store.Delete(context.Background(), token) })

	handlerCalled := false
	protected := store.RequireSession(session.RequireStepUp(5 * time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/sensitive", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	protected.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("got status %d, want 403", rec.Code)
	}
	if handlerCalled {
		t.Fatal("handler must not run without a recent step-up")
	}
}

func TestRequireStepUp_AllowsAfterMarkStepUp(t *testing.T) {
	rdb := openTestRedis(t)
	store := session.NewStore(rdb, time.Minute, time.Hour)

	token, err := store.Create(context.Background(), uuid.NewString())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = store.Delete(context.Background(), token) })

	if err := store.MarkStepUp(context.Background(), token); err != nil {
		t.Fatalf("mark step up: %v", err)
	}

	handlerCalled := false
	protected := store.RequireSession(session.RequireStepUp(5 * time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/sensitive", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	protected.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200", rec.Code)
	}
	if !handlerCalled {
		t.Fatal("expected handler to run after a recent step-up")
	}
}
