package intent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const uniqueViolation = "23505"

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

// Create is the only INSERT this package does against payment_intents; every
// later change is an UPDATE ... WHERE status = <expected>. On an
// idempotency-key collision it looks up and returns the original instead
// of erroring — unless the new request's body doesn't match, which is a
// genuine conflict, not a replay.
func (s *PostgresStore) Create(ctx context.Context, in *Intent) (*Intent, bool, error) {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO payment_intents (id, merchant_user_id, amount, currency, status, idempotency_key, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, in.ID, in.MerchantUserID, in.Amount, in.Currency, in.Status, in.IdempotencyKey, in.CreatedAt, in.UpdatedAt)

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		existing, getErr := s.getByIdempotencyKey(ctx, in.IdempotencyKey)
		if getErr != nil {
			return nil, false, fmt.Errorf("lookup existing intent by idempotency key: %w", getErr)
		}
		if existing.MerchantUserID != in.MerchantUserID || existing.Amount != in.Amount || existing.Currency != in.Currency {
			return nil, false, ErrIdempotencyConflict
		}
		return existing, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	return in, false, nil
}

func (s *PostgresStore) getByIdempotencyKey(ctx context.Context, key string) (*Intent, error) {
	return s.scanIntent(s.pool.QueryRow(ctx, `
		SELECT id, merchant_user_id, amount, currency, status, nonce, idempotency_key, expires_at, created_at, updated_at
		FROM payment_intents
		WHERE idempotency_key = $1
	`, key))
}

func (s *PostgresStore) GetByID(ctx context.Context, id string) (*Intent, error) {
	return s.scanIntent(s.pool.QueryRow(ctx, `
		SELECT id, merchant_user_id, amount, currency, status, nonce, idempotency_key, expires_at, created_at, updated_at
		FROM payment_intents
		WHERE id = $1
	`, id))
}

func (s *PostgresStore) scanIntent(row pgx.Row) (*Intent, error) {
	var in Intent
	err := row.Scan(&in.ID, &in.MerchantUserID, &in.Amount, &in.Currency, &in.Status, &in.Nonce,
		&in.IdempotencyKey, &in.ExpiresAt, &in.CreatedAt, &in.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &in, nil
}

func (s *PostgresStore) TransitionToPending(ctx context.Context, id string, nonce []byte, expiresAt time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE payment_intents
		SET status = $2, nonce = $3, expires_at = $4, updated_at = now()
		WHERE id = $1 AND status = $5
	`, id, StatusPending, nonce, expiresAt, StatusCreated)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrInvalidTransition
	}
	return nil
}

// RecordAuthorization inserts the authorization row and transitions
// PENDING -> CUSTOMER_AUTHORIZED in a single transaction. The unique
// constraint on payment_authorizations.payment_intent_id is what actually
// enforces "at most one authorization per intent"; the status transition's
// WHERE clause is what enforces "only from PENDING, and never expired" —
// both checked here, at commit time, never via a prior read.
func (s *PostgresStore) RecordAuthorization(ctx context.Context, intentID string, auth *Authorization) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `
		INSERT INTO payment_authorizations (id, payment_intent_id, customer_signing_device_id, signature, signed_payload_hash, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, auth.ID, intentID, auth.CustomerSigningDeviceID, auth.Signature, auth.SignedPayloadHash, auth.CreatedAt)

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return ErrAuthorizationReplayed
	}
	if err != nil {
		return fmt.Errorf("insert authorization: %w", err)
	}

	tag, err := tx.Exec(ctx, `
		UPDATE payment_intents
		SET status = $2, updated_at = now()
		WHERE id = $1 AND status = $3 AND expires_at > now()
	`, intentID, StatusCustomerAuthorized, StatusPending)
	if err != nil {
		return fmt.Errorf("transition to customer_authorized: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrInvalidTransition
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetAuthorizationByIntentID(ctx context.Context, intentID string) (*Authorization, error) {
	var a Authorization
	err := s.pool.QueryRow(ctx, `
		SELECT id, payment_intent_id, customer_signing_device_id, signature, signed_payload_hash, created_at
		FROM payment_authorizations
		WHERE payment_intent_id = $1
	`, intentID).Scan(&a.ID, &a.PaymentIntentID, &a.CustomerSigningDeviceID, &a.Signature, &a.SignedPayloadHash, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *PostgresStore) TransitionToProcessing(ctx context.Context, id string) error {
	return s.transition(ctx, id, StatusProcessing, StatusCustomerAuthorized)
}

func (s *PostgresStore) TransitionToCompleted(ctx context.Context, id string) error {
	return s.transition(ctx, id, StatusCompleted, StatusProcessing)
}

func (s *PostgresStore) TransitionToFailed(ctx context.Context, id string) error {
	return s.transition(ctx, id, StatusFailed, StatusProcessing)
}

func (s *PostgresStore) transition(ctx context.Context, id string, to, from Status) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE payment_intents SET status = $2, updated_at = now() WHERE id = $1 AND status = $3
	`, id, to, from)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrInvalidTransition
	}
	return nil
}

// SweepExpired is one atomic UPDATE ... RETURNING covering every past-due
// PENDING intent at once — no per-row read-then-write loop.
func (s *PostgresStore) SweepExpired(ctx context.Context) ([]ExpiredIntent, error) {
	rows, err := s.pool.Query(ctx, `
		UPDATE payment_intents
		SET status = $1, updated_at = now()
		WHERE status = $2 AND expires_at <= now()
		RETURNING id, merchant_user_id
	`, StatusExpired, StatusPending)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var expired []ExpiredIntent
	for rows.Next() {
		var e ExpiredIntent
		if err := rows.Scan(&e.ID, &e.MerchantUserID); err != nil {
			return nil, fmt.Errorf("scan expired intent: %w", err)
		}
		expired = append(expired, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expired intents: %w", err)
	}
	return expired, nil
}
