// Package device owns device registration and the device registry: the
// record of which public keys are allowed to sign on behalf of a user, and
// whether they're still trusted.
package device

import (
	"context"
	"time"
)

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

// Store persists devices. Reads must be able to answer whether a device is
// still trusted, per hapto's revocation invariant: a revoked device fails
// even with a valid signature.
type Store interface {
	Create(ctx context.Context, d *Device) error
	GetByID(ctx context.Context, id string) (*Device, error)
}
