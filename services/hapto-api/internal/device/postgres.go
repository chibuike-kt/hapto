package device

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("device not found")

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) Create(ctx context.Context, d *Device) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO signing_devices (id, user_id, public_key, algorithm, status, created_at, revoked_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, d.ID, d.UserID, d.PublicKey, d.Algorithm, d.Status, d.CreatedAt, d.RevokedAt)
	return err
}

func (s *PostgresStore) GetByID(ctx context.Context, id string) (*Device, error) {
	var d Device
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, public_key, algorithm, status, created_at, revoked_at
		FROM signing_devices
		WHERE id = $1
	`, id).Scan(&d.ID, &d.UserID, &d.PublicKey, &d.Algorithm, &d.Status, &d.CreatedAt, &d.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *PostgresStore) Revoke(ctx context.Context, id string, revokedAt time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE signing_devices SET revoked_at = $2, status = $3
		WHERE id = $1 AND revoked_at IS NULL
	`, id, revokedAt, StatusRevoked)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAlreadyRevoked
	}
	return nil
}
