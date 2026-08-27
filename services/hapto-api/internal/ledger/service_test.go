package ledger_test

import (
	"context"
	"errors"
	"testing"

	"github.com/chibuike-kt/hapto-api/internal/ledger"
)

type fakeStore struct {
	wallets   map[string]*ledger.Wallet
	written   []ledger.TransactionInput
	netSum    int64
	netSumErr error
}

func newFakeStore() *fakeStore {
	return &fakeStore{wallets: map[string]*ledger.Wallet{}}
}

func (f *fakeStore) CreateWallet(_ context.Context, w *ledger.Wallet) error {
	f.wallets[w.ID] = w
	return nil
}

func (f *fakeStore) GetWallet(_ context.Context, id string) (*ledger.Wallet, error) {
	w, ok := f.wallets[id]
	if !ok {
		return nil, ledger.ErrWalletNotFound
	}
	return w, nil
}

func (f *fakeStore) GetWalletByUserID(_ context.Context, userID, currency string) (*ledger.Wallet, error) {
	for _, w := range f.wallets {
		if w.UserID == userID && w.Currency == currency {
			return w, nil
		}
	}
	return nil, ledger.ErrWalletNotFound
}

func (f *fakeStore) WriteTransaction(_ context.Context, in ledger.TransactionInput) (*ledger.Transaction, error) {
	f.written = append(f.written, in)
	entries := make([]ledger.Entry, len(in.Entries))
	for i, e := range in.Entries {
		entries[i] = ledger.Entry{WalletID: e.WalletID, Amount: e.Amount, Direction: e.Direction}
	}
	return &ledger.Transaction{ID: "fake-tx", Entries: entries}, nil
}

func (f *fakeStore) Balance(_ context.Context, walletID string) (int64, error) {
	var sum int64
	for _, in := range f.written {
		for _, e := range in.Entries {
			if e.WalletID != walletID {
				continue
			}
			if e.Direction == ledger.DirectionCredit {
				sum += e.Amount
			} else {
				sum -= e.Amount
			}
		}
	}
	return sum, nil
}

func (f *fakeStore) NetSum(_ context.Context) (int64, error) {
	return f.netSum, f.netSumErr
}

func TestService_RecordTransaction_BalancedSucceeds(t *testing.T) {
	store := newFakeStore()
	svc := ledger.NewService(store)

	tx, err := svc.RecordTransaction(context.Background(), ledger.TransactionInput{
		IdempotencyKey: "key-1",
		Entries: []ledger.EntryInput{
			{WalletID: "wallet-a", Amount: 500, Direction: ledger.DirectionDebit},
			{WalletID: "wallet-b", Amount: 500, Direction: ledger.DirectionCredit},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tx.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(tx.Entries))
	}
	if len(store.written) != 1 {
		t.Fatalf("expected the store to receive 1 write, got %d", len(store.written))
	}
}

func TestService_RecordTransaction_RejectsUnbalanced(t *testing.T) {
	store := newFakeStore()
	svc := ledger.NewService(store)

	_, err := svc.RecordTransaction(context.Background(), ledger.TransactionInput{
		IdempotencyKey: "key-1",
		Entries: []ledger.EntryInput{
			{WalletID: "wallet-a", Amount: 500, Direction: ledger.DirectionDebit},
			{WalletID: "wallet-b", Amount: 400, Direction: ledger.DirectionCredit},
		},
	})
	if !errors.Is(err, ledger.ErrUnbalancedTransaction) {
		t.Fatalf("expected ErrUnbalancedTransaction, got %v", err)
	}
	if len(store.written) != 0 {
		t.Fatal("expected validation to reject the write before it reached the store")
	}
}

func TestService_RecordTransaction_RejectsTooFewEntries(t *testing.T) {
	store := newFakeStore()
	svc := ledger.NewService(store)

	_, err := svc.RecordTransaction(context.Background(), ledger.TransactionInput{
		IdempotencyKey: "key-1",
		Entries: []ledger.EntryInput{
			{WalletID: "wallet-a", Amount: 500, Direction: ledger.DirectionDebit},
		},
	})
	if !errors.Is(err, ledger.ErrTooFewEntries) {
		t.Fatalf("expected ErrTooFewEntries, got %v", err)
	}
	if len(store.written) != 0 {
		t.Fatal("expected validation to reject the write before it reached the store")
	}
}

func TestService_RecordTransaction_RejectsNonPositiveAmount(t *testing.T) {
	store := newFakeStore()
	svc := ledger.NewService(store)

	_, err := svc.RecordTransaction(context.Background(), ledger.TransactionInput{
		IdempotencyKey: "key-1",
		Entries: []ledger.EntryInput{
			{WalletID: "wallet-a", Amount: 0, Direction: ledger.DirectionDebit},
			{WalletID: "wallet-b", Amount: 0, Direction: ledger.DirectionCredit},
		},
	})
	if !errors.Is(err, ledger.ErrInvalidAmount) {
		t.Fatalf("expected ErrInvalidAmount, got %v", err)
	}
}

func TestService_RecordTransaction_RejectsInvalidDirection(t *testing.T) {
	store := newFakeStore()
	svc := ledger.NewService(store)

	_, err := svc.RecordTransaction(context.Background(), ledger.TransactionInput{
		IdempotencyKey: "key-1",
		Entries: []ledger.EntryInput{
			{WalletID: "wallet-a", Amount: 500, Direction: "sideways"},
			{WalletID: "wallet-b", Amount: 500, Direction: ledger.DirectionCredit},
		},
	})
	if !errors.Is(err, ledger.ErrInvalidDirection) {
		t.Fatalf("expected ErrInvalidDirection, got %v", err)
	}
}

func TestService_Reconcile_BalancedIsNil(t *testing.T) {
	store := newFakeStore()
	store.netSum = 0
	svc := ledger.NewService(store)

	if err := svc.Reconcile(context.Background()); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestService_Reconcile_DetectsImbalance(t *testing.T) {
	store := newFakeStore()
	store.netSum = 42
	svc := ledger.NewService(store)

	err := svc.Reconcile(context.Background())
	var imbalance *ledger.ImbalanceError
	if !errors.As(err, &imbalance) {
		t.Fatalf("expected *ImbalanceError, got %v", err)
	}
	if imbalance.NetAmount != 42 {
		t.Fatalf("net amount = %d, want 42", imbalance.NetAmount)
	}
}
