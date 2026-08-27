package auth_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chibuike-kt/hapto-api/internal/auth"
	"github.com/chibuike-kt/hapto-api/internal/migrate"
)

func openTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://hapto:hapto@localhost:5432/hapto?sslmode=disable" //nolint:gosec // local-only dev/test credential, matches docker-compose.yml
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("postgres not available: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("postgres not available: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := migrate.Up(dsn); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	return pool
}

func newTestUser(t *testing.T, pool *pgxpool.Pool, store *auth.PostgresStore) *auth.User {
	t.Helper()
	ctx := context.Background()

	u := &auth.User{ //nolint:gosec // fixture hash, not a real credential
		ID:           uuid.NewString(),
		Email:        uuid.NewString() + "@example.test",
		PasswordHash: "$argon2id$v=19$m=1,t=1,p=1$c29tZXNhbHQ$c29tZWhhc2g",
		Status:       auth.UserStatusActive,
		CreatedAt:    time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, "DELETE FROM password_reset_tokens WHERE user_id = $1", u.ID)
		_, _ = pool.Exec(bg, "DELETE FROM auth_totp WHERE user_id = $1", u.ID)
		_, _ = pool.Exec(bg, "DELETE FROM users WHERE id = $1", u.ID)
	})
	return u
}

func TestPostgresStore_CreateAndGetUser(t *testing.T) {
	pool := openTestPool(t)
	store := auth.NewPostgresStore(pool)
	u := newTestUser(t, pool, store)

	ctx := context.Background()

	byEmail, err := store.GetUserByEmail(ctx, u.Email)
	if err != nil {
		t.Fatalf("get by email: %v", err)
	}
	if byEmail.ID != u.ID {
		t.Fatalf("got user %s, want %s", byEmail.ID, u.ID)
	}

	byID, err := store.GetUserByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if byID.Email != u.Email {
		t.Fatalf("got email %s, want %s", byID.Email, u.Email)
	}
}

func TestPostgresStore_CreateUser_DuplicateEmailRejected(t *testing.T) {
	pool := openTestPool(t)
	store := auth.NewPostgresStore(pool)
	u := newTestUser(t, pool, store)

	dup := &auth.User{
		ID:           uuid.NewString(),
		Email:        u.Email,
		PasswordHash: u.PasswordHash,
		Status:       auth.UserStatusActive,
		CreatedAt:    time.Now().UTC(),
	}
	err := store.CreateUser(context.Background(), dup)
	if !errors.Is(err, auth.ErrEmailTaken) {
		t.Fatalf("expected ErrEmailTaken, got %v", err)
	}
}

func TestPostgresStore_TOTPUpsertAndEnable(t *testing.T) {
	pool := openTestPool(t)
	store := auth.NewPostgresStore(pool)
	u := newTestUser(t, pool, store)
	ctx := context.Background()

	if err := store.UpsertTOTPSecret(ctx, u.ID, []byte("encrypted-secret-v1")); err != nil {
		t.Fatalf("upsert totp: %v", err)
	}

	row, err := store.GetTOTP(ctx, u.ID)
	if err != nil {
		t.Fatalf("get totp: %v", err)
	}
	if row.Enabled() {
		t.Fatal("expected totp not yet enabled")
	}

	if err := store.EnableTOTP(ctx, u.ID, time.Now().UTC()); err != nil {
		t.Fatalf("enable totp: %v", err)
	}
	row, err = store.GetTOTP(ctx, u.ID)
	if err != nil {
		t.Fatalf("get totp: %v", err)
	}
	if !row.Enabled() {
		t.Fatal("expected totp to be enabled")
	}

	// Re-enrolling (secret rotation) must reset enabled_at.
	if err := store.UpsertTOTPSecret(ctx, u.ID, []byte("encrypted-secret-v2")); err != nil {
		t.Fatalf("re-upsert totp: %v", err)
	}
	row, err = store.GetTOTP(ctx, u.ID)
	if err != nil {
		t.Fatalf("get totp: %v", err)
	}
	if row.Enabled() {
		t.Fatal("expected re-enrollment to reset enabled_at")
	}
	if string(row.EncryptedSecret) != "encrypted-secret-v2" {
		t.Fatalf("expected updated secret, got %q", row.EncryptedSecret)
	}
}

func TestPostgresStore_ApplyPasswordReset_SingleUse(t *testing.T) {
	pool := openTestPool(t)
	store := auth.NewPostgresStore(pool)
	u := newTestUser(t, pool, store)
	ctx := context.Background()

	resetToken := &auth.PasswordResetToken{
		ID:        uuid.NewString(),
		UserID:    u.ID,
		TokenHash: uuid.NewString(),
		ExpiresAt: time.Now().UTC().Add(30 * time.Minute),
		CreatedAt: time.Now().UTC(),
	}
	if err := store.CreatePasswordResetToken(ctx, resetToken); err != nil {
		t.Fatalf("create reset token: %v", err)
	}

	fetched, err := store.GetPasswordResetTokenByHash(ctx, resetToken.TokenHash)
	if err != nil {
		t.Fatalf("get reset token: %v", err)
	}
	if fetched.UsedAt != nil {
		t.Fatal("expected token to start unused")
	}

	if err := store.ApplyPasswordReset(ctx, resetToken.ID, u.ID, "new-hash", time.Now().UTC()); err != nil {
		t.Fatalf("apply password reset: %v", err)
	}

	updated, err := store.GetUserByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if updated.PasswordHash != "new-hash" {
		t.Fatalf("expected password hash to be updated, got %q", updated.PasswordHash)
	}

	// Second application of the same token must fail (single-use).
	err = store.ApplyPasswordReset(ctx, resetToken.ID, u.ID, "another-hash", time.Now().UTC())
	if err == nil {
		t.Fatal("expected re-applying an already-used reset token to fail")
	}
}
