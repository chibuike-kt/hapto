package intent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	haptov1 "github.com/chibuike-kt/hapto-api/gen/hapto/v1"
	"github.com/chibuike-kt/hapto-api/internal/audit"
	"github.com/chibuike-kt/hapto-api/internal/device"
	"github.com/chibuike-kt/hapto-api/internal/ledger"
)

const nonceSizeBytes = 32

type Service struct {
	store    Store
	ledger   Ledger
	devices  Devices
	crypto   Crypto
	auditLog audit.Logger
	ttl      time.Duration
}

func NewService(store Store, ledgerSvc Ledger, devices Devices, crypto Crypto, auditLog audit.Logger, ttl time.Duration) *Service {
	return &Service{
		store:    store,
		ledger:   ledgerSvc,
		devices:  devices,
		crypto:   crypto,
		auditLog: auditLog,
		ttl:      ttl,
	}
}

// logAudit records a security event. Audit logging must never block or
// fail the action it describes — see internal/auth's identical helper for
// the full rationale, which applies here unchanged.
func (s *Service) logAudit(ctx context.Context, entry audit.Entry) {
	if s.auditLog == nil {
		return
	}
	if err := s.auditLog.Log(ctx, entry); err != nil {
		log.Printf("audit log failed for action %s: %v", entry.Action, err)
	}
}

type CreateInput struct {
	MerchantUserID string
	Amount         int64
	Currency       string
	IdempotencyKey string
}

// Create opens a new payment intent. It always ends in PENDING (with a
// generated nonce and expiry) or returns an error — CREATED never persists
// past this call under normal operation.
func (s *Service) Create(ctx context.Context, in CreateInput) (*Intent, error) {
	if in.Amount <= 0 {
		return nil, ErrInvalidAmount
	}
	if in.Currency == "" {
		return nil, ErrInvalidCurrency
	}
	if in.IdempotencyKey == "" {
		return nil, ErrMissingIdempotencyKey
	}

	now := time.Now().UTC()
	created := &Intent{
		ID:             uuid.NewString(),
		MerchantUserID: in.MerchantUserID,
		Amount:         in.Amount,
		Currency:       in.Currency,
		Status:         StatusCreated,
		IdempotencyKey: in.IdempotencyKey,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	current, replayed, err := s.store.Create(ctx, created)
	if err != nil {
		return nil, err // ErrIdempotencyConflict, or an infra error
	}
	if replayed {
		return current, nil
	}

	nonce, err := s.crypto.GenerateNonce(ctx, nonceSizeBytes)
	if err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	expiresAt := now.Add(s.ttl)
	if err := s.store.TransitionToPending(ctx, current.ID, nonce, expiresAt); err != nil {
		return nil, fmt.Errorf("transition to pending: %w", err)
	}
	current.Nonce = nonce
	current.Status = StatusPending
	current.ExpiresAt = &expiresAt

	s.logAudit(ctx, audit.Entry{
		ActorUserID: new(in.MerchantUserID),
		Action:      audit.ActionPaymentIntentCreated,
		TargetType:  audit.TargetTypePaymentIntent,
		TargetID:    new(current.ID),
		Metadata:    map[string]any{"amount": in.Amount, "currency": in.Currency},
	})

	return current, nil
}

func (s *Service) GetByID(ctx context.Context, id string) (*Intent, error) {
	return s.store.GetByID(ctx, id)
}

type AuthorizeInput struct {
	CustomerSigningDeviceID string
	Signature               []byte
	SignedPayload           []byte
}

// Authorize verifies the customer's signature and, on success, records the
// authorization and settles the intent.
//
// The PENDING-and-not-expired check happens only inside the atomic
// transition in RecordAuthorization, never as a prior read-based gate here
// — an initial read of the intent is still needed to get its nonce (for
// the signature check), but that read's Status is never used to decide
// anything. Otherwise two concurrent requests could both pass a "still
// PENDING" check before either writes, exactly the race this design avoids.
func (s *Service) Authorize(ctx context.Context, id string, in AuthorizeInput) (*Intent, error) {
	current, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	dev, err := s.devices.GetTrustedDevice(ctx, in.CustomerSigningDeviceID)
	if err != nil {
		return nil, err // device.ErrNotFound or device.ErrDeviceRevoked
	}

	if len(current.Nonce) == 0 || !bytes.HasPrefix(in.SignedPayload, current.Nonce) {
		return nil, ErrNonceMismatch
	}

	protoAlg, err := protoAlgorithm(dev.Algorithm)
	if err != nil {
		return nil, err
	}

	valid, reason, err := s.crypto.VerifySignature(ctx, dev.PublicKey, in.SignedPayload, in.Signature, protoAlg)
	if err != nil {
		return nil, fmt.Errorf("verify signature: %w", err)
	}
	if !valid {
		return nil, fmt.Errorf("%w: %s", ErrInvalidSignature, reason)
	}

	payloadHash := sha256.Sum256(in.SignedPayload)
	authRecord := &Authorization{
		ID:                      uuid.NewString(),
		PaymentIntentID:         id,
		CustomerSigningDeviceID: in.CustomerSigningDeviceID,
		Signature:               in.Signature,
		SignedPayloadHash:       payloadHash[:],
		CreatedAt:               time.Now().UTC(),
	}

	if err := s.store.RecordAuthorization(ctx, id, authRecord); err != nil {
		return nil, err // ErrAuthorizationReplayed or ErrInvalidTransition
	}

	s.logAudit(ctx, audit.Entry{
		ActorUserID: new(dev.UserID),
		Action:      audit.ActionPaymentIntentAuthorized,
		TargetType:  audit.TargetTypePaymentIntent,
		TargetID:    new(id),
	})

	s.settle(ctx, id, dev.UserID, current.MerchantUserID, current.Amount, current.Currency)

	return s.store.GetByID(ctx, id)
}

// settle moves an authorized intent through PROCESSING to COMPLETED or
// FAILED. It's internal — triggered right after a successful Authorize,
// never called directly by a handler. This is a single Postgres
// transaction inside ledger.RecordTransaction: it either fully commits or
// it doesn't, there's no saga/compensating-transaction machinery because
// there's no external system to coordinate against here.
func (s *Service) settle(ctx context.Context, id, customerUserID, merchantUserID string, amount int64, currency string) {
	if err := s.store.TransitionToProcessing(ctx, id); err != nil {
		log.Printf("settle %s: transition to processing: %v", id, err) //nolint:gosec // id is a path-derived UUID used only in a parameterized query, not a log-forging vector worth blocking on
		return
	}

	customerWallet, err := s.ledger.GetWalletByUserID(ctx, customerUserID, currency)
	if err != nil {
		s.failSettlement(ctx, id, merchantUserID, fmt.Errorf("get customer wallet: %w", err))
		return
	}
	merchantWallet, err := s.ledger.GetWalletByUserID(ctx, merchantUserID, currency)
	if err != nil {
		s.failSettlement(ctx, id, merchantUserID, fmt.Errorf("get merchant wallet: %w", err))
		return
	}

	_, err = s.ledger.RecordTransaction(ctx, ledger.TransactionInput{
		IdempotencyKey: id,
		Entries: []ledger.EntryInput{
			{WalletID: customerWallet.ID, Amount: amount, Direction: ledger.DirectionDebit},
			{WalletID: merchantWallet.ID, Amount: amount, Direction: ledger.DirectionCredit},
		},
	})
	if err != nil {
		s.failSettlement(ctx, id, merchantUserID, fmt.Errorf("record settlement transaction: %w", err))
		return
	}

	if err := s.store.TransitionToCompleted(ctx, id); err != nil {
		log.Printf("settle %s: transition to completed: %v", id, err) //nolint:gosec // id is a path-derived UUID, see the identical note above
		return
	}

	s.logAudit(ctx, audit.Entry{
		ActorUserID: new(merchantUserID),
		Action:      audit.ActionPaymentIntentCompleted,
		TargetType:  audit.TargetTypePaymentIntent,
		TargetID:    new(id),
	})
}

func (s *Service) failSettlement(ctx context.Context, id, merchantUserID string, cause error) {
	if err := s.store.TransitionToFailed(ctx, id); err != nil {
		log.Printf("settle %s: transition to failed: %v (original cause: %v)", id, err, cause) //nolint:gosec // id is a path-derived UUID, see the identical note above
		return
	}
	log.Printf("settle %s: failed: %v", id, cause) //nolint:gosec // id is a path-derived UUID, see the identical note above
	s.logAudit(ctx, audit.Entry{
		ActorUserID: new(merchantUserID),
		Action:      audit.ActionPaymentIntentFailed,
		TargetType:  audit.TargetTypePaymentIntent,
		TargetID:    new(id),
		Metadata:    map[string]any{"reason": cause.Error()},
	})
}

// SweepExpired moves every past-due PENDING intent to EXPIRED. Meant to be
// called on a timer; safe to call concurrently with itself or with any
// Authorize call, since it's the same atomic conditional UPDATE pattern.
func (s *Service) SweepExpired(ctx context.Context) (int, error) {
	expired, err := s.store.SweepExpired(ctx)
	if err != nil {
		return 0, fmt.Errorf("sweep expired: %w", err)
	}
	for _, e := range expired {
		s.logAudit(ctx, audit.Entry{
			ActorUserID: new(e.MerchantUserID),
			Action:      audit.ActionPaymentIntentExpired,
			TargetType:  audit.TargetTypePaymentIntent,
			TargetID:    new(e.ID),
		})
	}
	return len(expired), nil
}

func protoAlgorithm(a device.Algorithm) (haptov1.SignatureAlgorithm, error) {
	switch a {
	case device.AlgorithmEd25519:
		return haptov1.SignatureAlgorithm_SIGNATURE_ALGORITHM_ED25519, nil
	default:
		return haptov1.SignatureAlgorithm_SIGNATURE_ALGORITHM_UNSPECIFIED, fmt.Errorf("unsupported algorithm: %q", a)
	}
}
