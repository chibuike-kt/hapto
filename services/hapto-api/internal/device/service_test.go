package device_test

import (
	"context"
	"errors"
	"testing"
	"time"

	haptov1 "github.com/chibuike-kt/hapto-api/gen/hapto/v1"
	"github.com/chibuike-kt/hapto-api/internal/audit"
	"github.com/chibuike-kt/hapto-api/internal/device"
)

type fakeAuditLogger struct {
	entries []audit.Entry
	failErr error
}

func (f *fakeAuditLogger) Log(_ context.Context, entry audit.Entry) error {
	f.entries = append(f.entries, entry)
	return f.failErr
}

// findEntry returns the last logged entry for an action, or nil.
func (f *fakeAuditLogger) findEntry(action string) *audit.Entry {
	for i := len(f.entries) - 1; i >= 0; i-- {
		if f.entries[i].Action == action {
			return &f.entries[i]
		}
	}
	return nil
}

type fakeStore struct {
	created *device.Device
}

func (f *fakeStore) Create(_ context.Context, d *device.Device) error {
	f.created = d
	return nil
}

func (f *fakeStore) GetByID(_ context.Context, id string) (*device.Device, error) {
	if f.created != nil && f.created.ID == id {
		return f.created, nil
	}
	return nil, device.ErrNotFound
}

func (f *fakeStore) Revoke(_ context.Context, id string, revokedAt time.Time) error {
	if f.created == nil || f.created.ID != id {
		return device.ErrNotFound
	}
	if f.created.IsRevoked() {
		return device.ErrAlreadyRevoked
	}
	f.created.RevokedAt = &revokedAt
	f.created.Status = device.StatusRevoked
	return nil
}

type fakeValidator struct {
	valid  bool
	reason string
}

func (f *fakeValidator) ValidatePublicKey(_ context.Context, _ []byte, _ haptov1.SignatureAlgorithm) (bool, string, error) {
	return f.valid, f.reason, nil
}

func TestService_Register_ValidKeyIsStored(t *testing.T) {
	store := &fakeStore{}
	svc := device.NewService(store, &fakeValidator{valid: true}, &fakeAuditLogger{})

	d, err := svc.Register(context.Background(), device.RegisterInput{
		UserID:    "user-1",
		PublicKey: []byte("pubkey"),
		Algorithm: device.AlgorithmEd25519,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Status != device.StatusActive {
		t.Fatalf("expected status %q, got %q", device.StatusActive, d.Status)
	}
	if store.created == nil {
		t.Fatal("expected device to be persisted")
	}
}

func TestService_Register_RejectsInvalidKey(t *testing.T) {
	store := &fakeStore{}
	svc := device.NewService(store, &fakeValidator{valid: false, reason: "malformed key"}, &fakeAuditLogger{})

	_, err := svc.Register(context.Background(), device.RegisterInput{
		UserID:    "user-1",
		PublicKey: []byte("bad"),
		Algorithm: device.AlgorithmEd25519,
	})
	if !errors.Is(err, device.ErrInvalidPublicKey) {
		t.Fatalf("expected ErrInvalidPublicKey, got %v", err)
	}
	if store.created != nil {
		t.Fatal("expected device not to be persisted")
	}
}

func TestService_Register_RejectsUnsupportedAlgorithm(t *testing.T) {
	store := &fakeStore{}
	svc := device.NewService(store, &fakeValidator{valid: true}, &fakeAuditLogger{})

	_, err := svc.Register(context.Background(), device.RegisterInput{
		UserID:    "user-1",
		PublicKey: []byte("pubkey"),
		Algorithm: device.Algorithm("rsa"),
	})
	if !errors.Is(err, device.ErrUnsupportedAlgorithm) {
		t.Fatalf("expected ErrUnsupportedAlgorithm, got %v", err)
	}
	if store.created != nil {
		t.Fatal("expected device not to be persisted")
	}
}
