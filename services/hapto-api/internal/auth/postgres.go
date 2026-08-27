package auth

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schemaSQL string

// ApplySchema creates the auth tables (users, auth_totp,
// password_reset_tokens) if they don't already exist.
func ApplySchema(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, schemaSQL)
	return err
}

const uniqueViolation = "23505"

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) CreateUser(ctx context.Context, u *User) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO users (id, email, password_hash, status, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, u.ID, u.Email, u.PasswordHash, u.Status, u.CreatedAt)

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return ErrEmailTaken
	}
	return err
}

func (s *PostgresStore) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	return s.scanUser(s.pool.QueryRow(ctx, `
		SELECT id, email, password_hash, status, created_at
		FROM users
		WHERE email = $1
	`, email))
}

func (s *PostgresStore) GetUserByID(ctx context.Context, id string) (*User, error) {
	return s.scanUser(s.pool.QueryRow(ctx, `
		SELECT id, email, password_hash, status, created_at
		FROM users
		WHERE id = $1
	`, id))
}

func (s *PostgresStore) scanUser(row pgx.Row) (*User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Status, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *PostgresStore) UpsertTOTPSecret(ctx context.Context, userID string, encryptedSecret []byte) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO auth_totp (user_id, secret, enabled_at)
		VALUES ($1, $2, NULL)
		ON CONFLICT (user_id) DO UPDATE SET secret = EXCLUDED.secret, enabled_at = NULL
	`, userID, encryptedSecret)
	return err
}

func (s *PostgresStore) EnableTOTP(ctx context.Context, userID string, enabledAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE auth_totp SET enabled_at = $2 WHERE user_id = $1
	`, userID, enabledAt)
	return err
}

func (s *PostgresStore) GetTOTP(ctx context.Context, userID string) (*TOTP, error) {
	var t TOTP
	err := s.pool.QueryRow(ctx, `
		SELECT user_id, secret, enabled_at FROM auth_totp WHERE user_id = $1
	`, userID).Scan(&t.UserID, &t.EncryptedSecret, &t.EnabledAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *PostgresStore) CreatePasswordResetToken(ctx context.Context, t *PasswordResetToken) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO password_reset_tokens (id, user_id, token_hash, expires_at, used_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, t.ID, t.UserID, t.TokenHash, t.ExpiresAt, t.UsedAt, t.CreatedAt)
	return err
}

func (s *PostgresStore) GetPasswordResetTokenByHash(ctx context.Context, tokenHash string) (*PasswordResetToken, error) {
	var t PasswordResetToken
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, token_hash, expires_at, used_at, created_at
		FROM password_reset_tokens
		WHERE token_hash = $1
	`, tokenHash).Scan(&t.ID, &t.UserID, &t.TokenHash, &t.ExpiresAt, &t.UsedAt, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *PostgresStore) ApplyPasswordReset(ctx context.Context, tokenID, userID, newPasswordHash string, usedAt time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1`, userID, newPasswordHash); err != nil {
		return fmt.Errorf("update password: %w", err)
	}

	tag, err := tx.Exec(ctx, `
		UPDATE password_reset_tokens SET used_at = $2 WHERE id = $1 AND used_at IS NULL
	`, tokenID, usedAt)
	if err != nil {
		return fmt.Errorf("mark token used: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("reset token %s already used", tokenID)
	}

	return tx.Commit(ctx)
}
