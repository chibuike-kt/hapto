// Package ledger is hapto's double-entry ledger: the financial source of
// truth for the whole system. A wallet's balance is never stored, only
// derived by summing its entries, and ledger_entries is insert-only —
// corrections are new entries, never an UPDATE or DELETE against an
// existing row.
package ledger

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	ErrWalletNotFound        = errors.New("wallet not found")
	ErrTooFewEntries         = errors.New("a transaction requires at least two entries")
	ErrInvalidAmount         = errors.New("entry amount must be a positive integer")
	ErrInvalidDirection      = errors.New("entry direction must be debit or credit")
	ErrUnbalancedTransaction = errors.New("transaction entries do not balance: debits must equal credits")
)

type Direction string

const (
	DirectionDebit  Direction = "debit"
	DirectionCredit Direction = "credit"
)

type Wallet struct {
	ID        string
	UserID    string
	Currency  string
	CreatedAt time.Time
}

// Entry is one row of a balanced transaction. Amount is always a positive
// magnitude in integer minor units; Direction decides its sign relative to
// a wallet's balance.
type Entry struct {
	ID            string
	WalletID      string
	Amount        int64
	Direction     Direction
	TransactionID string
	CreatedAt     time.Time
}

type EntryInput struct {
	WalletID  string
	Amount    int64
	Direction Direction
}

// Transaction is the result of writing a balanced set of entries.
// Replayed is true when a caller reused an idempotency key and this is the
// original result being returned again, not a fresh write.
type Transaction struct {
	ID        string
	Entries   []Entry
	CreatedAt time.Time
	Replayed  bool
}

type TransactionInput struct {
	IdempotencyKey string
	Entries        []EntryInput
}

// Store persists wallets and ledger entries. It never exposes an update or
// delete path for entries, and every write is atomic and idempotent at the
// database level, not merely checked in application logic first.
type Store interface {
	CreateWallet(ctx context.Context, w *Wallet) error
	GetWallet(ctx context.Context, id string) (*Wallet, error)

	// GetWalletByUserID looks up a user's wallet in a given currency.
	// Wallet provisioning is a separate concern from this package: there is
	// no implicit create-on-first-use here, a missing wallet is
	// ErrWalletNotFound.
	GetWalletByUserID(ctx context.Context, userID, currency string) (*Wallet, error)

	// WriteTransaction persists every entry in in.Entries atomically in a
	// single Postgres transaction, keyed by in.IdempotencyKey. Reusing a
	// key returns the original transaction (Replayed set) instead of
	// writing again; the uniqueness is enforced by a database constraint.
	WriteTransaction(ctx context.Context, in TransactionInput) (*Transaction, error)

	// Balance sums a wallet's entries. There is no stored balance anywhere
	// in the system; this is the only source of truth for what a wallet
	// holds.
	Balance(ctx context.Context, walletID string) (int64, error)

	// NetSum sums every entry across the entire system, signed by
	// direction. A correctly functioning ledger always nets to zero.
	NetSum(ctx context.Context) (int64, error)
}

// validateEntries enforces the balanced-transaction invariant. Both Service
// and Store call this independently — for a financial ledger, that
// invariant is non-negotiable enough to check at every entry point, not
// just once.
func validateEntries(entries []EntryInput) error {
	if len(entries) < 2 {
		return ErrTooFewEntries
	}

	var debit, credit int64
	for _, e := range entries {
		if e.Amount <= 0 {
			return ErrInvalidAmount
		}
		switch e.Direction {
		case DirectionDebit:
			debit += e.Amount
		case DirectionCredit:
			credit += e.Amount
		default:
			return ErrInvalidDirection
		}
	}

	if debit != credit {
		return ErrUnbalancedTransaction
	}
	return nil
}

// ImbalanceError reports that the whole-system ledger doesn't net to zero
// — a critical integrity failure. Never swallow this into a log line.
type ImbalanceError struct {
	NetAmount int64
}

func (e *ImbalanceError) Error() string {
	return fmt.Sprintf("ledger integrity check failed: entries net to %d minor units, expected 0", e.NetAmount)
}
