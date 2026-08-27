package audit_test

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chibuike-kt/hapto-api/internal/audit"
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

// newTestUser inserts a minimal user row so tests can satisfy
// audit_logs.actor_user_id's foreign key, and returns its ID.
func newTestUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	id := uuid.NewString()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO users (id, email, password_hash, status, created_at)
		VALUES ($1, $2, 'unused', 'active', now())
	`, id, id+"@example.test")
	if err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", id); err != nil {
			t.Logf("cleanup: delete user %s: %v", id, err)
		}
	})
	return id
}

type auditRow struct {
	ActorUserID *string
	Action      string
	TargetType  *string
	TargetID    *string
	Metadata    map[string]any
	CreatedAt   time.Time
}

func fetchAuditRow(t *testing.T, pool *pgxpool.Pool, action string, targetID *string) auditRow {
	t.Helper()

	var row auditRow
	var metadataJSON []byte
	err := pool.QueryRow(context.Background(), `
		SELECT actor_user_id, action, target_type, target_id, metadata, created_at
		FROM audit_logs
		WHERE action = $1 AND target_id IS NOT DISTINCT FROM $2
		ORDER BY created_at DESC
		LIMIT 1
	`, action, targetID).Scan(&row.ActorUserID, &row.Action, &row.TargetType, &row.TargetID, &metadataJSON, &row.CreatedAt)
	if err != nil {
		t.Fatalf("fetch audit row for action %q: %v", action, err)
	}
	if err := json.Unmarshal(metadataJSON, &row.Metadata); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	return row
}

func TestPostgresStore_Log_PersistsEntry(t *testing.T) {
	pool := openTestPool(t)
	store := audit.NewPostgresStore(pool)

	actorID := newTestUser(t, pool)
	targetID := uuid.NewString()
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "DELETE FROM audit_logs WHERE target_id = $1", targetID); err != nil {
			t.Logf("cleanup: delete audit log for target %s: %v", targetID, err)
		}
	})

	err := store.Log(context.Background(), audit.Entry{
		ActorUserID: new(actorID),
		Action:      audit.ActionDeviceRegistered,
		TargetType:  audit.TargetTypeDevice,
		TargetID:    new(targetID),
		Metadata:    map[string]any{"algorithm": "ed25519"},
	})
	if err != nil {
		t.Fatalf("log: %v", err)
	}

	row := fetchAuditRow(t, pool, audit.ActionDeviceRegistered, &targetID)
	if row.ActorUserID == nil || *row.ActorUserID != actorID {
		t.Fatalf("actor_user_id = %v, want %s", row.ActorUserID, actorID)
	}
	if row.TargetType == nil || *row.TargetType != audit.TargetTypeDevice {
		t.Fatalf("target_type = %v, want %s", row.TargetType, audit.TargetTypeDevice)
	}
	if row.Metadata["algorithm"] != "ed25519" {
		t.Fatalf("metadata algorithm = %v, want ed25519", row.Metadata["algorithm"])
	}
}

func TestPostgresStore_Log_NullableFieldsPersistAsNull(t *testing.T) {
	pool := openTestPool(t)
	store := audit.NewPostgresStore(pool)

	// A failed login against an email that isn't registered has no
	// authenticated actor and no resolvable target yet.
	email := uuid.NewString() + "@example.test"
	err := store.Log(context.Background(), audit.Entry{
		Action:   audit.ActionLoginFailed,
		Metadata: map[string]any{"email": email, "reason": "unknown_email"},
	})
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(),
			"DELETE FROM audit_logs WHERE action = $1 AND metadata->>'email' = $2", audit.ActionLoginFailed, email,
		); err != nil {
			t.Logf("cleanup: delete audit log for email %s: %v", email, err)
		}
	})

	var actorUserID, targetType, targetID *string
	err = pool.QueryRow(context.Background(), `
		SELECT actor_user_id, target_type, target_id FROM audit_logs
		WHERE action = $1 AND metadata->>'email' = $2
	`, audit.ActionLoginFailed, email).Scan(&actorUserID, &targetType, &targetID)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if actorUserID != nil {
		t.Fatalf("actor_user_id = %v, want nil", *actorUserID)
	}
	if targetType != nil {
		t.Fatalf("target_type = %v, want nil", *targetType)
	}
	if targetID != nil {
		t.Fatalf("target_id = %v, want nil", *targetID)
	}
}

// TestPostgresStore_ExposesNoMutationMethod guards the "no update or delete
// path anywhere in the code" invariant at the type level: if anyone later
// adds an UpdateEntry or DeleteEntry method to PostgresStore, this fails.
func TestPostgresStore_ExposesNoMutationMethod(t *testing.T) {
	typ := reflect.TypeFor[*audit.PostgresStore]()
	for m := range typ.Methods() {
		lower := strings.ToLower(m.Name)
		if strings.Contains(lower, "update") || strings.Contains(lower, "delete") {
			t.Fatalf("audit.PostgresStore must not expose a mutation method, found %q", m.Name)
		}
	}
}
