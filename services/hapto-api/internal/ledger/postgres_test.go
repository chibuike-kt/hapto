package ledger_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chibuike-kt/hapto-api/internal/ledger"
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

// testEnv tracks wallets and transactions a test creates so cleanup can
// remove exactly what it wrote, in FK-safe order, regardless of test
// outcome.
type testEnv struct {
	pool      *pgxpool.Pool
	store     *ledger.PostgresStore
	mu        sync.Mutex
	walletIDs []string
	txIDs     []string
}

func newTestEnv(t *testing.T) *testEnv {
	pool := openTestPool(t)
	env := &testEnv{pool: pool, store: ledger.NewPostgresStore(pool)}

	t.Cleanup(func() {
		ctx := context.Background()
		if len(env.txIDs) > 0 {
			if _, err := pool.Exec(ctx, "DELETE FROM ledger_entries WHERE transaction_id = ANY($1)", env.txIDs); err != nil {
				t.Logf("cleanup: delete entries: %v", err)
			}
			if _, err := pool.Exec(ctx, "DELETE FROM ledger_transactions WHERE id = ANY($1)", env.txIDs); err != nil {
				t.Logf("cleanup: delete transactions: %v", err)
			}
		}
		if len(env.walletIDs) > 0 {
			if _, err := pool.Exec(ctx, "DELETE FROM wallets WHERE id = ANY($1)", env.walletIDs); err != nil {
				t.Logf("cleanup: delete wallets: %v", err)
			}
		}
	})

	return env
}

func (e *testEnv) newWallet(t *testing.T) *ledger.Wallet {
	t.Helper()
	w := &ledger.Wallet{
		ID:        uuid.NewString(),
		UserID:    uuid.NewString(),
		Currency:  "USD",
		CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := e.store.CreateWallet(context.Background(), w); err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	e.mu.Lock()
	e.walletIDs = append(e.walletIDs, w.ID)
	e.mu.Unlock()
	return w
}

// record writes a transaction and tracks its ID for cleanup. Safe to call
// only from the test's main goroutine; concurrent writers should track
// their own results and merge them back after joining.
func (e *testEnv) record(t *testing.T, in ledger.TransactionInput) *ledger.Transaction {
	t.Helper()
	tx, err := e.store.WriteTransaction(context.Background(), in)
	if err != nil {
		t.Fatalf("write transaction: %v", err)
	}
	e.txIDs = append(e.txIDs, tx.ID)
	return tx
}

func TestPostgresStore_WriteTransaction_BalancedSucceeds(t *testing.T) {
	env := newTestEnv(t)
	a := env.newWallet(t)
	b := env.newWallet(t)

	tx := env.record(t, ledger.TransactionInput{
		IdempotencyKey: uuid.NewString(),
		Entries: []ledger.EntryInput{
			{WalletID: a.ID, Amount: 500, Direction: ledger.DirectionDebit},
			{WalletID: b.ID, Amount: 500, Direction: ledger.DirectionCredit},
		},
	})
	if len(tx.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(tx.Entries))
	}
	if tx.Replayed {
		t.Fatal("expected a fresh write, not a replay")
	}

	balA, err := env.store.Balance(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("balance a: %v", err)
	}
	if balA != -500 {
		t.Fatalf("balance a = %d, want -500", balA)
	}

	balB, err := env.store.Balance(context.Background(), b.ID)
	if err != nil {
		t.Fatalf("balance b: %v", err)
	}
	if balB != 500 {
		t.Fatalf("balance b = %d, want 500", balB)
	}
}

func TestPostgresStore_WriteTransaction_UnbalancedFails(t *testing.T) {
	env := newTestEnv(t)
	a := env.newWallet(t)
	b := env.newWallet(t)

	key := uuid.NewString()
	_, err := env.store.WriteTransaction(context.Background(), ledger.TransactionInput{
		IdempotencyKey: key,
		Entries: []ledger.EntryInput{
			{WalletID: a.ID, Amount: 500, Direction: ledger.DirectionDebit},
			{WalletID: b.ID, Amount: 400, Direction: ledger.DirectionCredit},
		},
	})
	if !errors.Is(err, ledger.ErrUnbalancedTransaction) {
		t.Fatalf("expected ErrUnbalancedTransaction, got %v", err)
	}

	var count int
	if err := env.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM ledger_transactions WHERE idempotency_key = $1", key,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no transaction row to be written, got %d", count)
	}
}

func TestPostgresStore_WriteTransaction_IdempotencyKeyReuseDoesNotDoubleWrite(t *testing.T) {
	env := newTestEnv(t)
	a := env.newWallet(t)
	b := env.newWallet(t)

	in := ledger.TransactionInput{
		IdempotencyKey: uuid.NewString(),
		Entries: []ledger.EntryInput{
			{WalletID: a.ID, Amount: 250, Direction: ledger.DirectionDebit},
			{WalletID: b.ID, Amount: 250, Direction: ledger.DirectionCredit},
		},
	}

	first := env.record(t, in)
	if first.Replayed {
		t.Fatal("expected the first write not to be a replay")
	}

	second := env.record(t, in)
	if !second.Replayed {
		t.Fatal("expected the second write to be a replay")
	}
	if second.ID != first.ID {
		t.Fatalf("replayed transaction id = %s, want %s", second.ID, first.ID)
	}

	var count int
	if err := env.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM ledger_entries WHERE transaction_id = $1", first.ID,
	).Scan(&count); err != nil {
		t.Fatalf("count entries: %v", err)
	}
	if count != 2 {
		t.Fatalf("entry count = %d, want 2 (no double-write)", count)
	}
}

func TestPostgresStore_Balance_AggregatesMultipleTransactions(t *testing.T) {
	env := newTestEnv(t)
	a := env.newWallet(t)
	b := env.newWallet(t)

	env.record(t, ledger.TransactionInput{
		IdempotencyKey: uuid.NewString(),
		Entries: []ledger.EntryInput{
			{WalletID: a.ID, Amount: 1000, Direction: ledger.DirectionCredit},
			{WalletID: b.ID, Amount: 1000, Direction: ledger.DirectionDebit},
		},
	})
	env.record(t, ledger.TransactionInput{
		IdempotencyKey: uuid.NewString(),
		Entries: []ledger.EntryInput{
			{WalletID: a.ID, Amount: 300, Direction: ledger.DirectionDebit},
			{WalletID: b.ID, Amount: 300, Direction: ledger.DirectionCredit},
		},
	})

	balA, err := env.store.Balance(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("balance a: %v", err)
	}
	if balA != 700 {
		t.Fatalf("balance a = %d, want 700", balA)
	}

	balB, err := env.store.Balance(context.Background(), b.ID)
	if err != nil {
		t.Fatalf("balance b: %v", err)
	}
	if balB != -700 {
		t.Fatalf("balance b = %d, want -700", balB)
	}
}

func TestPostgresStore_ConcurrentWrites_NoRaceInBalance(t *testing.T) {
	env := newTestEnv(t)
	wallet := env.newWallet(t)
	counterparty := env.newWallet(t)

	const n = 20
	const amount = int64(100)

	var wg sync.WaitGroup
	errCh := make(chan error, n)
	txCh := make(chan string, n)

	for range n {
		wg.Go(func() {
			result, err := env.store.WriteTransaction(context.Background(), ledger.TransactionInput{
				IdempotencyKey: uuid.NewString(),
				Entries: []ledger.EntryInput{
					{WalletID: wallet.ID, Amount: amount, Direction: ledger.DirectionCredit},
					{WalletID: counterparty.ID, Amount: amount, Direction: ledger.DirectionDebit},
				},
			})
			if err != nil {
				errCh <- err
				return
			}
			txCh <- result.ID
		})
	}
	wg.Wait()
	close(errCh)
	close(txCh)

	for err := range errCh {
		t.Errorf("concurrent write failed: %v", err)
	}
	for id := range txCh {
		env.txIDs = append(env.txIDs, id)
	}

	balance, err := env.store.Balance(context.Background(), wallet.ID)
	if err != nil {
		t.Fatalf("balance: %v", err)
	}
	if balance != int64(n)*amount {
		t.Fatalf("balance = %d, want %d", balance, int64(n)*amount)
	}

	counterpartyBalance, err := env.store.Balance(context.Background(), counterparty.ID)
	if err != nil {
		t.Fatalf("counterparty balance: %v", err)
	}
	if counterpartyBalance != -int64(n)*amount {
		t.Fatalf("counterparty balance = %d, want %d", counterpartyBalance, -int64(n)*amount)
	}
}

func TestService_Reconcile_DetectsArtificialImbalance(t *testing.T) {
	env := newTestEnv(t)
	svc := ledger.NewService(env.store)
	wallet := env.newWallet(t)

	before, err := env.store.NetSum(context.Background())
	if err != nil {
		t.Fatalf("net sum: %v", err)
	}

	// Simulate an integrity bug or manual tampering: insert a single entry
	// with no balancing counterpart, bypassing all validation.
	txID := uuid.NewString()
	entryID := uuid.NewString()
	now := time.Now().UTC()
	const injectedAmount = int64(777)

	if _, err := env.pool.Exec(context.Background(), `
		INSERT INTO ledger_transactions (id, idempotency_key, created_at) VALUES ($1, $2, $3)
	`, txID, uuid.NewString(), now); err != nil {
		t.Fatalf("insert artificial transaction: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		if _, err := env.pool.Exec(ctx, "DELETE FROM ledger_entries WHERE id = $1", entryID); err != nil {
			t.Logf("cleanup: delete artificial entry: %v", err)
		}
		if _, err := env.pool.Exec(ctx, "DELETE FROM ledger_transactions WHERE id = $1", txID); err != nil {
			t.Logf("cleanup: delete artificial transaction: %v", err)
		}
	})

	if _, err := env.pool.Exec(context.Background(), `
		INSERT INTO ledger_entries (id, wallet_id, amount, direction, transaction_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, entryID, wallet.ID, injectedAmount, ledger.DirectionCredit, txID, now); err != nil {
		t.Fatalf("insert artificial entry: %v", err)
	}

	err = svc.Reconcile(context.Background())
	var imbalance *ledger.ImbalanceError
	if !errors.As(err, &imbalance) {
		t.Fatalf("expected *ImbalanceError, got %v", err)
	}
	if imbalance.NetAmount != before+injectedAmount {
		t.Fatalf("net amount = %d, want %d", imbalance.NetAmount, before+injectedAmount)
	}
}
