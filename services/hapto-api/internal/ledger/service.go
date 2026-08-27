package ledger

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) CreateWallet(ctx context.Context, userID, currency string) (*Wallet, error) {
	w := &Wallet{
		ID:        uuid.NewString(),
		UserID:    userID,
		Currency:  currency,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.store.CreateWallet(ctx, w); err != nil {
		return nil, fmt.Errorf("create wallet: %w", err)
	}
	return w, nil
}

// RecordTransaction validates that entries balance before ever reaching the
// database, then writes them atomically and idempotently.
func (s *Service) RecordTransaction(ctx context.Context, in TransactionInput) (*Transaction, error) {
	if err := validateEntries(in.Entries); err != nil {
		return nil, err
	}

	tx, err := s.store.WriteTransaction(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("write transaction: %w", err)
	}
	return tx, nil
}

func (s *Service) Balance(ctx context.Context, walletID string) (int64, error) {
	return s.store.Balance(ctx, walletID)
}

func (s *Service) GetWalletByUserID(ctx context.Context, userID, currency string) (*Wallet, error) {
	return s.store.GetWalletByUserID(ctx, userID, currency)
}

// Reconcile confirms the sum of every entry in the system nets to zero.
// Callable on demand for now; wire it into a scheduled job later. A non-nil
// result is a page, not a log line.
func (s *Service) Reconcile(ctx context.Context) error {
	sum, err := s.store.NetSum(ctx)
	if err != nil {
		return fmt.Errorf("reconcile: %w", err)
	}
	if sum != 0 {
		return &ImbalanceError{NetAmount: sum}
	}
	return nil
}
