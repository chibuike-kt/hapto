// Package device owns device registration and the device registry: the
// record of which public keys are allowed to sign on behalf of a user, and
// whether they're still trusted.
package device

import (
	"context"
	"errors"
	"time"
)

// ErrDeviceRevoked signals that a device has been revoked and must not be
// trusted, even if it presents an otherwise-valid signature. Every future
// signature-verification path must go through GetTrustedDevice rather than
// GetByID directly, so this check can never be forgotten.
var ErrDeviceRevoked = errors.New("device is revoked")

type Algorithm string

const (
	AlgorithmEd25519 Algorithm = "ed25519"
)

type Status string

const (
	StatusActive  Status = "active"
	StatusRevoked Status = "revoked"
)

type Device struct {
	ID        string
	UserID    string
	PublicKey []byte
	Algorithm Algorithm
	Status    Status
	CreatedAt time.Time
	RevokedAt *time.Time
}

// IsRevoked reports whether this device has been revoked. This is the one
// place that decides trust from a Device value — callers should use it
// rather than checking RevokedAt directly.
func (d *Device) IsRevoked() bool {
	return d != nil && d.RevokedAt != nil
}

// Store persists devices. Reads must be able to answer whether a device is
// still trusted, per hapto's revocation invariant: a revoked device fails
// even with a valid signature.
type Store interface {
	Create(ctx context.Context, d *Device) error
	GetByID(ctx context.Context, id string) (*Device, error)

	// Revoke sets revoked_at for a device that isn't already revoked. The
	// row is never deleted, per hapto's audit-trail principle. Callers are
	// expected to have already confirmed the device exists and is owned by
	// the caller; Revoke itself only distinguishes "not currently revoked"
	// from "already revoked" via ErrAlreadyRevoked.
	Revoke(ctx context.Context, id string, revokedAt time.Time) error
}

// ErrAlreadyRevoked indicates the device was already revoked; the caller's
// revoke request made no change.
var ErrAlreadyRevoked = errors.New("device already revoked")
