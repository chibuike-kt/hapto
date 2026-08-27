package intent_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chibuike-kt/hapto-api/internal/intent"
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

// newPendingIntent creates a fresh intent and drives it straight to PENDING
// via the store's own Create + TransitionToPending, exactly like
// Service.Create would, and registers cleanup.
func newPendingIntent(t *testing.T, pool *pgxpool.Pool, store *intent.PostgresStore, ttl time.Duration) *intent.Intent {
	t.Helper()
	ctx := context.Background()

	now := time.Now().UTC()
	in := &intent.Intent{
		ID:             uuid.NewString(),
		MerchantUserID: uuid.NewString(),
		Amount:         1000,
		Currency:       "USD",
		Status:         intent.StatusCreated,
		IdempotencyKey: uuid.NewString(),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	created, replayed, err := store.Create(ctx, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if replayed {
		t.Fatal("expected a fresh create, not a replay")
	}
	t.Cleanup(func() {
		bg := context.Background()
		if _, err := pool.Exec(bg, "DELETE FROM payment_authorizations WHERE payment_intent_id = $1", created.ID); err != nil {
			t.Logf("cleanup: delete authorization: %v", err)
		}
		if _, err := pool.Exec(bg, "DELETE FROM payment_intents WHERE id = $1", created.ID); err != nil {
			t.Logf("cleanup: delete intent: %v", err)
		}
	})

	nonce := []byte("test-nonce-0123456789012345678901")
	expiresAt := now.Add(ttl)
	if err := store.TransitionToPending(ctx, created.ID, nonce, expiresAt); err != nil {
		t.Fatalf("transition to pending: %v", err)
	}
	created.Nonce = nonce
	created.Status = intent.StatusPending
	created.ExpiresAt = &expiresAt
	return created
}

func newAuthorization(intentID string) *intent.Authorization {
	return &intent.Authorization{
		ID:                      uuid.NewString(),
		PaymentIntentID:         intentID,
		CustomerSigningDeviceID: uuid.NewString(),
		Signature:               []byte("signature-bytes"),
		SignedPayloadHash:       []byte("hash-bytes"),
		CreatedAt:               time.Now().UTC(),
	}
}

func TestPostgresStore_Create_IdempotencyKeyReuse_ReturnsOriginal(t *testing.T) {
	pool := openTestPool(t)
	store := intent.NewPostgresStore(pool)
	ctx := context.Background()

	key := uuid.NewString()
	merchantID := uuid.NewString()
	now := time.Now().UTC()
	first := &intent.Intent{
		ID: uuid.NewString(), MerchantUserID: merchantID, Amount: 500, Currency: "USD",
		Status: intent.StatusCreated, IdempotencyKey: key, CreatedAt: now, UpdatedAt: now,
	}
	created, replayed, err := store.Create(ctx, first)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	if replayed {
		t.Fatal("expected first create not to be a replay")
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "DELETE FROM payment_intents WHERE id = $1", created.ID); err != nil {
			t.Logf("cleanup: %v", err)
		}
	})

	second := &intent.Intent{
		ID: uuid.NewString(), MerchantUserID: merchantID, Amount: 500, Currency: "USD",
		Status: intent.StatusCreated, IdempotencyKey: key, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	got, replayed, err := store.Create(ctx, second)
	if err != nil {
		t.Fatalf("second create: %v", err)
	}
	if !replayed {
		t.Fatal("expected the second create to be a replay")
	}
	if got.ID != created.ID {
		t.Fatalf("replayed id = %s, want %s (a duplicate row must not be created)", got.ID, created.ID)
	}
}

func TestPostgresStore_Create_IdempotencyKeyReuse_DifferentBodyConflicts(t *testing.T) {
	pool := openTestPool(t)
	store := intent.NewPostgresStore(pool)
	ctx := context.Background()

	key := uuid.NewString()
	now := time.Now().UTC()
	first := &intent.Intent{
		ID: uuid.NewString(), MerchantUserID: uuid.NewString(), Amount: 500, Currency: "USD",
		Status: intent.StatusCreated, IdempotencyKey: key, CreatedAt: now, UpdatedAt: now,
	}
	created, _, err := store.Create(ctx, first)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "DELETE FROM payment_intents WHERE id = $1", created.ID); err != nil {
			t.Logf("cleanup: %v", err)
		}
	})

	conflicting := &intent.Intent{
		ID: uuid.NewString(), MerchantUserID: uuid.NewString(), Amount: 999, Currency: "USD",
		Status: intent.StatusCreated, IdempotencyKey: key, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	_, _, err = store.Create(ctx, conflicting)
	if !errors.Is(err, intent.ErrIdempotencyConflict) {
		t.Fatalf("expected ErrIdempotencyConflict, got %v", err)
	}
}

func TestPostgresStore_TransitionToPending_WrongStateFails(t *testing.T) {
	pool := openTestPool(t)
	store := intent.NewPostgresStore(pool)
	in := newPendingIntent(t, pool, store, 5*time.Minute) // already PENDING

	err := store.TransitionToPending(context.Background(), in.ID, []byte("nonce"), time.Now().Add(time.Minute))
	if !errors.Is(err, intent.ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestPostgresStore_RecordAuthorization_Success(t *testing.T) {
	pool := openTestPool(t)
	store := intent.NewPostgresStore(pool)
	in := newPendingIntent(t, pool, store, 5*time.Minute)

	if err := store.RecordAuthorization(context.Background(), in.ID, newAuthorization(in.ID)); err != nil {
		t.Fatalf("record authorization: %v", err)
	}

	got, err := store.GetByID(context.Background(), in.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if got.Status != intent.StatusCustomerAuthorized {
		t.Fatalf("status = %s, want %s", got.Status, intent.StatusCustomerAuthorized)
	}
}

func TestPostgresStore_RecordAuthorization_ReplayedFails(t *testing.T) {
	pool := openTestPool(t)
	store := intent.NewPostgresStore(pool)
	in := newPendingIntent(t, pool, store, 5*time.Minute)

	if err := store.RecordAuthorization(context.Background(), in.ID, newAuthorization(in.ID)); err != nil {
		t.Fatalf("first authorization: %v", err)
	}

	err := store.RecordAuthorization(context.Background(), in.ID, newAuthorization(in.ID))
	if !errors.Is(err, intent.ErrAuthorizationReplayed) {
		t.Fatalf("expected ErrAuthorizationReplayed, got %v", err)
	}
}

func TestPostgresStore_RecordAuthorization_ExpiredFails(t *testing.T) {
	pool := openTestPool(t)
	store := intent.NewPostgresStore(pool)
	// Negative TTL: already expired the moment it's created.
	in := newPendingIntent(t, pool, store, -time.Minute)

	err := store.RecordAuthorization(context.Background(), in.ID, newAuthorization(in.ID))
	if !errors.Is(err, intent.ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition, got %v", err)
	}

	// The failed attempt must not have left a dangling authorization row —
	// the insert and the transition are one transaction.
	var count int
	if err := pool.QueryRow(context.Background(),
		"SELECT count(*) FROM payment_authorizations WHERE payment_intent_id = $1", in.ID,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no authorization row after a rolled-back transition, got %d", count)
	}
}

func TestPostgresStore_RecordAuthorization_NotPendingFails(t *testing.T) {
	pool := openTestPool(t)
	store := intent.NewPostgresStore(pool)
	in := newPendingIntent(t, pool, store, 5*time.Minute)

	if err := store.RecordAuthorization(context.Background(), in.ID, newAuthorization(in.ID)); err != nil {
		t.Fatalf("first authorization: %v", err)
	}
	if err := store.TransitionToProcessing(context.Background(), in.ID); err != nil {
		t.Fatalf("transition to processing: %v", err)
	}
	if err := store.TransitionToCompleted(context.Background(), in.ID); err != nil {
		t.Fatalf("transition to completed: %v", err)
	}

	// Now COMPLETED: a fresh authorization attempt must fail even though
	// this is a different authorization row than the one already used
	// (the unique constraint alone wouldn't catch a *new* device trying to
	// authorize an already-settled intent — the status condition does).
	err := store.RecordAuthorization(context.Background(), in.ID, &intent.Authorization{
		ID: uuid.NewString(), PaymentIntentID: in.ID, CustomerSigningDeviceID: uuid.NewString(),
		Signature: []byte("sig2"), SignedPayloadHash: []byte("hash2"), CreatedAt: time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("expected authorization against a COMPLETED intent to fail")
	}
}

func TestPostgresStore_ConcurrentRecordAuthorization_OnlyOneSucceeds(t *testing.T) {
	pool := openTestPool(t)
	store := intent.NewPostgresStore(pool)
	in := newPendingIntent(t, pool, store, 5*time.Minute)

	const n = 5
	var wg sync.WaitGroup
	results := make([]error, n)

	for i := range n {
		wg.Go(func() {
			results[i] = store.RecordAuthorization(context.Background(), in.ID, newAuthorization(in.ID))
		})
	}
	wg.Wait()

	successes := 0
	for _, err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("expected exactly 1 of %d concurrent authorize attempts to succeed, got %d", n, successes)
	}

	got, err := store.GetByID(context.Background(), in.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if got.Status != intent.StatusCustomerAuthorized {
		t.Fatalf("status = %s, want %s", got.Status, intent.StatusCustomerAuthorized)
	}

	var count int
	if err := pool.QueryRow(context.Background(),
		"SELECT count(*) FROM payment_authorizations WHERE payment_intent_id = $1", in.ID,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 authorization row, got %d", count)
	}
}

func TestPostgresStore_SweepExpired_MovesOnlyPastDuePending(t *testing.T) {
	pool := openTestPool(t)
	store := intent.NewPostgresStore(pool)

	pastDue := newPendingIntent(t, pool, store, -time.Minute)
	stillLive := newPendingIntent(t, pool, store, 5*time.Minute)

	expired, err := store.SweepExpired(context.Background())
	if err != nil {
		t.Fatalf("sweep expired: %v", err)
	}

	var sweptPastDue bool
	for _, e := range expired {
		if e.ID == pastDue.ID {
			sweptPastDue = true
		}
		if e.ID == stillLive.ID {
			t.Fatal("sweep must not touch an intent that hasn't expired yet")
		}
	}
	if !sweptPastDue {
		t.Fatal("expected the past-due intent to be included in the sweep")
	}

	got, err := store.GetByID(context.Background(), pastDue.ID)
	if err != nil {
		t.Fatalf("get past-due: %v", err)
	}
	if got.Status != intent.StatusExpired {
		t.Fatalf("past-due status = %s, want %s", got.Status, intent.StatusExpired)
	}

	stillGot, err := store.GetByID(context.Background(), stillLive.ID)
	if err != nil {
		t.Fatalf("get still-live: %v", err)
	}
	if stillGot.Status != intent.StatusPending {
		t.Fatalf("still-live status = %s, want %s (must be untouched)", stillGot.Status, intent.StatusPending)
	}
}
