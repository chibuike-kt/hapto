package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

// Log is the only way audit_logs is ever written. There is no update or
// delete method on this type — corrections belong in a new entry, never an
// edit to history.
func (s *PostgresStore) Log(ctx context.Context, entry Entry) error {
	metadata := entry.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	var targetType *string
	if entry.TargetType != "" {
		targetType = &entry.TargetType
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO audit_logs (id, actor_user_id, action, target_type, target_id, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, uuid.NewString(), entry.ActorUserID, entry.Action, targetType, entry.TargetID, metadataJSON, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("insert audit log: %w", err)
	}
	return nil
}
