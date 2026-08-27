package device_test

import (
	"context"
	"errors"
	"testing"

	"github.com/chibuike-kt/hapto-api/internal/audit"
	"github.com/chibuike-kt/hapto-api/internal/device"
)

func TestService_Register_LogsDeviceRegistered(t *testing.T) {
	store := &fakeStore{}
	auditLog := &fakeAuditLogger{}
	svc := device.NewService(store, &fakeValidator{valid: true}, auditLog)

	d, err := svc.Register(context.Background(), device.RegisterInput{
		UserID:    "user-1",
		PublicKey: []byte("pubkey"),
		Algorithm: device.AlgorithmEd25519,
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	entry := auditLog.findEntry(audit.ActionDeviceRegistered)
	if entry == nil {
		t.Fatal("expected a device.registered audit entry")
	}
	if entry.ActorUserID == nil || *entry.ActorUserID != "user-1" {
		t.Fatalf("actor_user_id = %v, want user-1", entry.ActorUserID)
	}
	if entry.TargetType != audit.TargetTypeDevice || entry.TargetID == nil || *entry.TargetID != d.ID {
		t.Fatalf("unexpected target: type=%s id=%v", entry.TargetType, entry.TargetID)
	}
	if entry.Metadata["algorithm"] != string(device.AlgorithmEd25519) {
		t.Fatalf("metadata algorithm = %v, want %s", entry.Metadata["algorithm"], device.AlgorithmEd25519)
	}
}

func TestService_Register_RejectedKey_DoesNotLog(t *testing.T) {
	store := &fakeStore{}
	auditLog := &fakeAuditLogger{}
	svc := device.NewService(store, &fakeValidator{valid: false, reason: "malformed key"}, auditLog)

	_, err := svc.Register(context.Background(), device.RegisterInput{
		UserID:    "user-1",
		PublicKey: []byte("bad"),
		Algorithm: device.AlgorithmEd25519,
	})
	if !errors.Is(err, device.ErrInvalidPublicKey) {
		t.Fatalf("expected ErrInvalidPublicKey, got %v", err)
	}
	if len(auditLog.entries) != 0 {
		t.Fatalf("expected no audit entries for a rejected registration, got %d", len(auditLog.entries))
	}
}

func TestService_Revoke_LogsDeviceRevoked(t *testing.T) {
	store := &fakeStore{}
	auditLog := &fakeAuditLogger{}
	regSvc := device.NewService(store, &fakeValidator{valid: true}, &fakeAuditLogger{})
	d, err := regSvc.Register(context.Background(), device.RegisterInput{
		UserID:    "user-1",
		PublicKey: []byte("pubkey"),
		Algorithm: device.AlgorithmEd25519,
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	svc := device.NewService(store, &fakeValidator{valid: true}, auditLog)
	if err := svc.Revoke(context.Background(), d.ID, "user-1"); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	entry := auditLog.findEntry(audit.ActionDeviceRevoked)
	if entry == nil {
		t.Fatal("expected a device.revoked audit entry")
	}
	if entry.ActorUserID == nil || *entry.ActorUserID != "user-1" {
		t.Fatalf("actor_user_id = %v, want user-1", entry.ActorUserID)
	}
	if entry.TargetType != audit.TargetTypeDevice || entry.TargetID == nil || *entry.TargetID != d.ID {
		t.Fatalf("unexpected target: type=%s id=%v", entry.TargetType, entry.TargetID)
	}
}

func TestService_Revoke_ForbiddenDoesNotLog(t *testing.T) {
	store := &fakeStore{}
	regSvc := device.NewService(store, &fakeValidator{valid: true}, &fakeAuditLogger{})
	d, err := regSvc.Register(context.Background(), device.RegisterInput{
		UserID:    "user-1",
		PublicKey: []byte("pubkey"),
		Algorithm: device.AlgorithmEd25519,
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	auditLog := &fakeAuditLogger{}
	svc := device.NewService(store, &fakeValidator{valid: true}, auditLog)
	if err := svc.Revoke(context.Background(), d.ID, "user-2"); !errors.Is(err, device.ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
	if len(auditLog.entries) != 0 {
		t.Fatalf("expected no audit entries for a forbidden revoke, got %d", len(auditLog.entries))
	}
}

// --- audit failures must never block the primary action --------------------

func TestService_Register_AuditFailureDoesNotBlockRegistration(t *testing.T) {
	store := &fakeStore{}
	auditLog := &fakeAuditLogger{failErr: errors.New("audit backend unavailable")}
	svc := device.NewService(store, &fakeValidator{valid: true}, auditLog)

	d, err := svc.Register(context.Background(), device.RegisterInput{
		UserID:    "user-1",
		PublicKey: []byte("pubkey"),
		Algorithm: device.AlgorithmEd25519,
	})
	if err != nil {
		t.Fatalf("expected registration to succeed despite the audit logger failing, got: %v", err)
	}
	if store.created == nil || store.created.ID != d.ID {
		t.Fatal("expected the device to still be persisted")
	}
	if auditLog.findEntry(audit.ActionDeviceRegistered) == nil {
		t.Fatal("expected the audit logger to still have been called")
	}
}

func TestService_Revoke_AuditFailureDoesNotBlockRevoke(t *testing.T) {
	store := &fakeStore{}
	regSvc := device.NewService(store, &fakeValidator{valid: true}, &fakeAuditLogger{})
	d, err := regSvc.Register(context.Background(), device.RegisterInput{
		UserID:    "user-1",
		PublicKey: []byte("pubkey"),
		Algorithm: device.AlgorithmEd25519,
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	auditLog := &fakeAuditLogger{failErr: errors.New("audit backend unavailable")}
	svc := device.NewService(store, &fakeValidator{valid: true}, auditLog)
	if err := svc.Revoke(context.Background(), d.ID, "user-1"); err != nil {
		t.Fatalf("expected revoke to succeed despite the audit logger failing, got: %v", err)
	}
	if !store.created.IsRevoked() {
		t.Fatal("expected the device to still be revoked")
	}
}
