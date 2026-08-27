package device_test

import (
	"context"
	"errors"
	"testing"

	"github.com/chibuike-kt/hapto-api/internal/device"
)

func registerTestDevice(t *testing.T, store *fakeStore, userID string) *device.Device {
	t.Helper()
	svc := device.NewService(store, &fakeValidator{valid: true}, &fakeAuditLogger{})

	d, err := svc.Register(context.Background(), device.RegisterInput{
		UserID:    userID,
		PublicKey: []byte("pubkey"),
		Algorithm: device.AlgorithmEd25519,
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	return d
}

func TestService_Revoke_Success(t *testing.T) {
	store := &fakeStore{}
	d := registerTestDevice(t, store, "user-1")
	svc := device.NewService(store, &fakeValidator{valid: true}, &fakeAuditLogger{})

	if err := svc.Revoke(context.Background(), d.ID, "user-1"); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	if !store.created.IsRevoked() {
		t.Fatal("expected device to be revoked")
	}
	if store.created.Status != device.StatusRevoked {
		t.Fatalf("expected status %q, got %q", device.StatusRevoked, store.created.Status)
	}
}

func TestService_Revoke_AlreadyRevokedFails(t *testing.T) {
	store := &fakeStore{}
	d := registerTestDevice(t, store, "user-1")
	svc := device.NewService(store, &fakeValidator{valid: true}, &fakeAuditLogger{})

	if err := svc.Revoke(context.Background(), d.ID, "user-1"); err != nil {
		t.Fatalf("first revoke: %v", err)
	}

	err := svc.Revoke(context.Background(), d.ID, "user-1")
	if !errors.Is(err, device.ErrAlreadyRevoked) {
		t.Fatalf("expected ErrAlreadyRevoked, got %v", err)
	}
}

func TestService_Revoke_DifferentOwnerForbidden(t *testing.T) {
	store := &fakeStore{}
	d := registerTestDevice(t, store, "user-1")
	svc := device.NewService(store, &fakeValidator{valid: true}, &fakeAuditLogger{})

	err := svc.Revoke(context.Background(), d.ID, "user-2")
	if !errors.Is(err, device.ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
	if store.created.IsRevoked() {
		t.Fatal("expected device not to be revoked by a non-owner")
	}
}

func TestService_Revoke_UnknownDeviceNotFound(t *testing.T) {
	store := &fakeStore{}
	svc := device.NewService(store, &fakeValidator{valid: true}, &fakeAuditLogger{})

	err := svc.Revoke(context.Background(), "does-not-exist", "user-1")
	if !errors.Is(err, device.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// --- the trust guard future signature verification must use ---------------

func TestService_GetTrustedDevice_ActiveDeviceIsTrusted(t *testing.T) {
	store := &fakeStore{}
	d := registerTestDevice(t, store, "user-1")
	svc := device.NewService(store, &fakeValidator{valid: true}, &fakeAuditLogger{})

	got, err := svc.GetTrustedDevice(context.Background(), d.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != d.ID {
		t.Fatalf("got device %s, want %s", got.ID, d.ID)
	}
}

func TestService_GetTrustedDevice_RevokedDeviceFailsTrust(t *testing.T) {
	store := &fakeStore{}
	d := registerTestDevice(t, store, "user-1")
	svc := device.NewService(store, &fakeValidator{valid: true}, &fakeAuditLogger{})

	if err := svc.Revoke(context.Background(), d.ID, "user-1"); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	_, err := svc.GetTrustedDevice(context.Background(), d.ID)
	if !errors.Is(err, device.ErrDeviceRevoked) {
		t.Fatalf("expected ErrDeviceRevoked, got %v", err)
	}
}

func TestService_GetTrustedDevice_UnknownDeviceNotFound(t *testing.T) {
	store := &fakeStore{}
	svc := device.NewService(store, &fakeValidator{valid: true}, &fakeAuditLogger{})

	_, err := svc.GetTrustedDevice(context.Background(), "does-not-exist")
	if !errors.Is(err, device.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
