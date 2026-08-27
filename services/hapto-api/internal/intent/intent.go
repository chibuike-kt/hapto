// Package intent implements hapto's payment intent lifecycle: the backend
// half of the BLE payment flow. It owns every state transition — a client
// never sets status directly, only triggers the specific verified action
// (create, authorize) that causes the backend to move it.
//
// Every transition is a single atomic UPDATE ... WHERE status = <expected>,
// matching internal/device.Revoke's pattern. There is no read-then-write
// path in this package: a read may inform what to verify (the intent's
// nonce, say), but the transition itself is never gated on a prior SELECT
// — only on the UPDATE's own WHERE clause, so two concurrent requests can
// never both succeed.
package intent

import (
	"context"
	"errors"
	"time"

	haptov1 "github.com/chibuike-kt/hapto-api/gen/hapto/v1"
	"github.com/chibuike-kt/hapto-api/internal/device"
	"github.com/chibuike-kt/hapto-api/internal/ledger"
)

type Status string

const (
	StatusCreated            Status = "CREATED"
	StatusPending            Status = "PENDING"
	StatusCustomerAuthorized Status = "CUSTOMER_AUTHORIZED"
	StatusProcessing         Status = "PROCESSING"
	StatusCompleted          Status = "COMPLETED"
	StatusExpired            Status = "EXPIRED"
	StatusFailed             Status = "FAILED"
	StatusReversed           Status = "REVERSED"
)

var (
	ErrNotFound              = errors.New("payment intent not found")
	ErrInvalidAmount         = errors.New("amount must be a positive integer")
	ErrInvalidCurrency       = errors.New("currency is required")
	ErrMissingIdempotencyKey = errors.New("idempotency key is required")
	ErrIdempotencyConflict   = errors.New("idempotency key reused with a different request")
	ErrInvalidTransition     = errors.New("payment intent is not in a state that allows this action")
	ErrInvalidSignature      = errors.New("invalid signature")
	ErrNonceMismatch         = errors.New("signed payload does not incorporate the intent's nonce")
	ErrAuthorizationReplayed = errors.New("this intent has already been authorized")
)

type Intent struct {
	ID             string
	MerchantUserID string
	Amount         int64
	Currency       string
	Status         Status
	Nonce          []byte
	IdempotencyKey string
	ExpiresAt      *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Authorization is the customer's signed proof that they approved an
// intent, recorded once per intent — payment_intent_id is unique at the
// database level, which is the actual replay guard: a given intent has
// exactly one nonce for its lifetime, so "this (intent, nonce) pair has
// already produced an authorization" and "this intent already has an
// authorization row" are the same fact.
type Authorization struct {
	ID                      string
	PaymentIntentID         string
	CustomerSigningDeviceID string
	Signature               []byte
	SignedPayloadHash       []byte
	CreatedAt               time.Time
}

// ExpiredIntent is the minimal shape SweepExpired reports for each intent
// it moves to EXPIRED, enough to log an audit entry per intent.
type ExpiredIntent struct {
	ID             string
	MerchantUserID string
}

// Store persists payment intents and their authorizations. Every method
// that changes status does so via a single atomic conditional UPDATE; none
// of them accept the intent's current state as an assumption from the
// caller.
type Store interface {
	// Create inserts a new intent in CREATED status. On an idempotency-key
	// collision it returns the existing intent with replayed=true instead
	// of erroring, unless the new request's body doesn't match the
	// original, in which case it returns ErrIdempotencyConflict.
	Create(ctx context.Context, in *Intent) (existing *Intent, replayed bool, err error)

	GetByID(ctx context.Context, id string) (*Intent, error)

	// TransitionToPending sets the generated nonce and expiry and moves
	// CREATED -> PENDING. ErrInvalidTransition if the row wasn't CREATED.
	TransitionToPending(ctx context.Context, id string, nonce []byte, expiresAt time.Time) error

	// RecordAuthorization inserts the authorization row and transitions
	// PENDING -> CUSTOMER_AUTHORIZED in one database transaction: either
	// both happen or neither does. Requires expires_at > now() as part of
	// the same atomic condition — an expired intent can never be
	// authorized, even if the sweep hasn't caught up to it yet.
	// ErrAuthorizationReplayed if this intent already has an authorization
	// (a concurrent request may have just won); ErrInvalidTransition if the
	// intent wasn't PENDING or was expired.
	RecordAuthorization(ctx context.Context, intentID string, auth *Authorization) error

	GetAuthorizationByIntentID(ctx context.Context, intentID string) (*Authorization, error)

	TransitionToProcessing(ctx context.Context, id string) error
	TransitionToCompleted(ctx context.Context, id string) error
	TransitionToFailed(ctx context.Context, id string) error

	// SweepExpired moves every past-due PENDING intent to EXPIRED in one
	// atomic UPDATE ... RETURNING, and reports which ones it moved.
	SweepExpired(ctx context.Context) ([]ExpiredIntent, error)
}

// Ledger is the subset of internal/ledger's Service that intent needs.
type Ledger interface {
	RecordTransaction(ctx context.Context, in ledger.TransactionInput) (*ledger.Transaction, error)
	GetWalletByUserID(ctx context.Context, userID, currency string) (*ledger.Wallet, error)
}

// Devices is the subset of internal/device's Service that intent needs.
// Authorize must go through GetTrustedDevice, never GetByID, so a revoked
// device is rejected even with a mathematically valid signature.
type Devices interface {
	GetTrustedDevice(ctx context.Context, id string) (*device.Device, error)
}

// Crypto is the subset of internal/cryptoclient's Client that intent needs.
type Crypto interface {
	GenerateNonce(ctx context.Context, sizeBytes uint32) ([]byte, error)
	VerifySignature(ctx context.Context, publicKey, message, signature []byte, algorithm haptov1.SignatureAlgorithm) (valid bool, reason string, err error)
}
