package device

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	haptov1 "github.com/chibuike-kt/hapto-api/gen/hapto/v1"
)

var ErrInvalidPublicKey = errors.New("invalid public key")
var ErrUnsupportedAlgorithm = errors.New("unsupported algorithm")
var ErrForbidden = errors.New("caller does not own this device")

// Validator confirms a public key is well-formed for its algorithm. In
// production this is backed by hapto-crypto over gRPC; device registration
// must never store a key it hasn't validated first.
type Validator interface {
	ValidatePublicKey(ctx context.Context, publicKey []byte, algorithm haptov1.SignatureAlgorithm) (valid bool, reason string, err error)
}

type Service struct {
	store     Store
	validator Validator
}

func NewService(store Store, validator Validator) *Service {
	return &Service{store: store, validator: validator}
}

type RegisterInput struct {
	UserID    string
	PublicKey []byte
	Algorithm Algorithm
}

func (s *Service) Register(ctx context.Context, in RegisterInput) (*Device, error) {
	protoAlg, err := toProtoAlgorithm(in.Algorithm)
	if err != nil {
		return nil, err
	}

	valid, reason, err := s.validator.ValidatePublicKey(ctx, in.PublicKey, protoAlg)
	if err != nil {
		return nil, fmt.Errorf("validate public key: %w", err)
	}
	if !valid {
		return nil, fmt.Errorf("%w: %s", ErrInvalidPublicKey, reason)
	}

	d := &Device{
		ID:        uuid.NewString(),
		UserID:    in.UserID,
		PublicKey: in.PublicKey,
		Algorithm: in.Algorithm,
		Status:    StatusActive,
		CreatedAt: time.Now().UTC(),
	}

	if err := s.store.Create(ctx, d); err != nil {
		return nil, fmt.Errorf("create device: %w", err)
	}

	return d, nil
}

// Revoke marks a device revoked. Only the device's owner may revoke it.
// Revoking an already-revoked device is an error, not a silent no-op, so
// callers can tell the difference between "done" and "already done".
func (s *Service) Revoke(ctx context.Context, id, callerUserID string) error {
	d, err := s.store.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if d.UserID != callerUserID {
		return ErrForbidden
	}
	if d.IsRevoked() {
		return ErrAlreadyRevoked
	}

	if err := s.store.Revoke(ctx, id, time.Now().UTC()); err != nil {
		return fmt.Errorf("revoke device: %w", err)
	}
	return nil
}

// GetTrustedDevice looks up a device and confirms it hasn't been revoked.
// No signature-verification path exists yet, but when one does, it must
// call this instead of GetByID directly — a revoked device fails even with
// a valid signature, per hapto's invariants.
func (s *Service) GetTrustedDevice(ctx context.Context, id string) (*Device, error) {
	d, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if d.IsRevoked() {
		return nil, ErrDeviceRevoked
	}
	return d, nil
}

func toProtoAlgorithm(a Algorithm) (haptov1.SignatureAlgorithm, error) {
	switch a {
	case AlgorithmEd25519:
		return haptov1.SignatureAlgorithm_SIGNATURE_ALGORITHM_ED25519, nil
	default:
		return haptov1.SignatureAlgorithm_SIGNATURE_ALGORITHM_UNSPECIFIED, fmt.Errorf("%w: %q", ErrUnsupportedAlgorithm, a)
	}
}
