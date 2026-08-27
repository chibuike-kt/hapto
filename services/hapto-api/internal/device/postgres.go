package device

import (
	"context"
	_ "embed"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schemaSQL string

// ApplySchema creates the devices table if it doesn't already exist. There's
// exactly one table here, so a migration tool would be more machinery than
// the schema warrants.
func ApplySchema(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, schemaSQL)
	return err
}

var ErrNotFound = errors.New("device not found")

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) Create(ctx context.Context, d *Device) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO devices (id, user_id, public_key, algorithm, status, created_at, revoked_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, d.ID, d.UserID, d.PublicKey, d.Algorithm, d.Status, d.CreatedAt, d.RevokedAt)
	return err
}

func (s *PostgresStore) GetByID(ctx context.Context, id string) (*Device, error) {
	var d Device
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, public_key, algorithm, status, created_at, revoked_at
		FROM devices
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
