package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/chibuike-kt/hapto-api/internal/auth"
)

func TestPendingLoginStore_CreateGetDelete(t *testing.T) {
	rdb := openTestRedis(t)
	store := auth.NewPendingLoginStore(rdb)
	ctx := context.Background()
	userID := uuid.NewString()

	id, err := store.Create(ctx, userID)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := store.Get(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != userID {
		t.Fatalf("got user %q, want %q", got, userID)
	}

	if err := store.Delete(ctx, id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.Get(ctx, id); !errors.Is(err, auth.ErrPendingLoginNotFound) {
		t.Fatalf("expected ErrPendingLoginNotFound after delete, got %v", err)
	}
}

func TestPendingLoginStore_UnknownIDNotFound(t *testing.T) {
	rdb := openTestRedis(t)
	store := auth.NewPendingLoginStore(rdb)

	_, err := store.Get(context.Background(), "does-not-exist")
	if !errors.Is(err, auth.ErrPendingLoginNotFound) {
		t.Fatalf("expected ErrPendingLoginNotFound, got %v", err)
	}
}
