package device_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chibuike-kt/hapto-api/internal/device"
)

func openTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
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
	return pool
}

func TestPostgresStore_CreateAndGetByID(t *testing.T) {
	pool := openTestPool(t)

	ctx := context.Background()
	if err := device.ApplySchema(ctx, pool); err != nil {
		t.Fatalf("apply schema: %v", err)
	}

	store := device.NewPostgresStore(pool)

	d := &device.Device{
		ID:        uuid.NewString(),
		UserID:    uuid.NewString(),
		PublicKey: []byte(uuid.NewString()), // unique per run to satisfy the public_key uniqueness constraint
		Algorithm: device.AlgorithmEd25519,
		Status:    device.StatusActive,
		CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "DELETE FROM signing_devices WHERE id = $1", d.ID); err != nil {
			t.Logf("cleanup: delete device %s: %v", d.ID, err)
		}
	})

	if err := store.Create(ctx, d); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := store.GetByID(ctx, d.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if got.UserID != d.UserID {
		t.Errorf("user_id = %q, want %q", got.UserID, d.UserID)
	}
	if !bytes.Equal(got.PublicKey, d.PublicKey) {
		t.Errorf("public_key = %q, want %q", got.PublicKey, d.PublicKey)
	}
	if got.Algorithm != d.Algorithm {
		t.Errorf("algorithm = %q, want %q", got.Algorithm, d.Algorithm)
	}
	if got.Status != d.Status {
		t.Errorf("status = %q, want %q", got.Status, d.Status)
	}
	if got.RevokedAt != nil {
		t.Errorf("revoked_at = %v, want nil", got.RevokedAt)
	}
}

func TestPostgresStore_GetByID_NotFound(t *testing.T) {
	pool := openTestPool(t)

	ctx := context.Background()
	if err := device.ApplySchema(ctx, pool); err != nil {
		t.Fatalf("apply schema: %v", err)
	}

	store := device.NewPostgresStore(pool)

	_, err := store.GetByID(ctx, uuid.NewString())
	if !errors.Is(err, device.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
