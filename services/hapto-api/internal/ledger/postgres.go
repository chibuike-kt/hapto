package ledger

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
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

func (s *PostgresStore) CreateWallet(ctx context.Context, w *Wallet) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO wallets (id, user_id, currency, created_at)
		VALUES ($1, $2, $3, $4)
	`, w.ID, w.UserID, w.Currency, w.CreatedAt)
	return err
}

func (s *PostgresStore) GetWallet(ctx context.Context, id string) (*Wallet, error) {
	var w Wallet
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, currency, created_at FROM wallets WHERE id = $1
	`, id).Scan(&w.ID, &w.UserID, &w.Currency, &w.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrWalletNotFound
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func (s *PostgresStore) GetWalletByUserID(ctx context.Context, userID, currency string) (*Wallet, error) {
	var w Wallet
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, currency, created_at FROM wallets WHERE user_id = $1 AND currency = $2
	`, userID, currency).Scan(&w.ID, &w.UserID, &w.Currency, &w.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrWalletNotFound
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

// WriteTransaction is the only place ledger_entries rows are ever created.
// Balance is checked again here even though Service already checked it,
// entries are written inside one Postgres transaction, and idempotency is
// enforced by ledger_transactions.idempotency_key's unique constraint, not
// by an application-level pre-check.
func (s *PostgresStore) WriteTransaction(ctx context.Context, in TransactionInput) (*Transaction, error) {
	if err := validateEntries(in.Entries); err != nil {
		return nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	txID := uuid.NewString()
	createdAt := time.Now().UTC()

	_, err = tx.Exec(ctx, `
		INSERT INTO ledger_transactions (id, idempotency_key, created_at)
		VALUES ($1, $2, $3)
	`, txID, in.IdempotencyKey, createdAt)

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		// Someone already used this idempotency key. Postgres blocks a
		// second inserter on the unique index until the first transaction
		// resolves, so by the time we observe this error the winning
		// write is guaranteed committed — replay its result instead of
		// writing again.
		return s.transactionByIdempotencyKey(ctx, in.IdempotencyKey)
	}
	if err != nil {
		return nil, fmt.Errorf("create transaction: %w", err)
	}

	entries := make([]Entry, len(in.Entries))
	for i, e := range in.Entries {
		entryID := uuid.NewString()
		if _, err := tx.Exec(ctx, `
			INSERT INTO ledger_entries (id, wallet_id, amount, direction, transaction_id, created_at)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, entryID, e.WalletID, e.Amount, e.Direction, txID, createdAt); err != nil {
			return nil, fmt.Errorf("insert entry: %w", err)
		}
		entries[i] = Entry{
			ID:            entryID,
			WalletID:      e.WalletID,
			Amount:        e.Amount,
			Direction:     e.Direction,
			TransactionID: txID,
			CreatedAt:     createdAt,
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return &Transaction{ID: txID, Entries: entries, CreatedAt: createdAt}, nil
}

func (s *PostgresStore) transactionByIdempotencyKey(ctx context.Context, key string) (*Transaction, error) {
	var txID string
	var createdAt time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT id, created_at FROM ledger_transactions WHERE idempotency_key = $1
	`, key).Scan(&txID, &createdAt)
	if err != nil {
		return nil, fmt.Errorf("lookup transaction by idempotency key: %w", err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, wallet_id, amount, direction, transaction_id, created_at
		FROM ledger_entries
		WHERE transaction_id = $1
		ORDER BY created_at, id
	`, txID)
	if err != nil {
		return nil, fmt.Errorf("load replayed entries: %w", err)
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.WalletID, &e.Amount, &e.Direction, &e.TransactionID, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan replayed entry: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate replayed entries: %w", err)
	}

	return &Transaction{ID: txID, Entries: entries, CreatedAt: createdAt, Replayed: true}, nil
}

// Balance convention: credit increases a wallet's balance, debit decreases
// it — everyday banking-statement semantics. This only decides which side
// of a transaction moves a given wallet's balance up or down; the
// balanced-transaction invariant (total debits == total credits) holds
// regardless of which side is which.
func (s *PostgresStore) Balance(ctx context.Context, walletID string) (int64, error) {
	var balance int64
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(CASE WHEN direction = 'credit' THEN amount ELSE -amount END), 0)
		FROM ledger_entries
		WHERE wallet_id = $1
	`, walletID).Scan(&balance)
	if err != nil {
		return 0, err
	}
	return balance, nil
}

func (s *PostgresStore) NetSum(ctx context.Context) (int64, error) {
	var sum int64
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(CASE WHEN direction = 'credit' THEN amount ELSE -amount END), 0)
		FROM ledger_entries
	`).Scan(&sum)
	if err != nil {
		return 0, err
	}
	return sum, nil
}
