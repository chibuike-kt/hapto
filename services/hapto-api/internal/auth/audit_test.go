package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/chibuike-kt/hapto-api/internal/audit"
	"github.com/chibuike-kt/hapto-api/internal/auth"
)

func TestLogin_Success_LogsLoginSucceeded(t *testing.T) {
	h := newHarness()
	u := h.signup(t, "audit-success@example.com")

	if _, err := h.service.Login(context.Background(), "audit-success@example.com", testPassword); err != nil {
		t.Fatalf("login: %v", err)
	}

	entry := h.auditLog.findEntry(audit.ActionLoginSucceeded)
	if entry == nil {
		t.Fatal("expected a login.succeeded audit entry")
	}
	if entry.ActorUserID == nil || *entry.ActorUserID != u.ID {
		t.Fatalf("actor_user_id = %v, want %s", entry.ActorUserID, u.ID)
	}
	if entry.TargetType != audit.TargetTypeUser || entry.TargetID == nil || *entry.TargetID != u.ID {
		t.Fatalf("unexpected target: type=%s id=%v", entry.TargetType, entry.TargetID)
	}
}

func TestLogin_WrongPassword_LogsLoginFailed(t *testing.T) {
	h := newHarness()
	u := h.signup(t, "audit-wrong@example.com")

	_, err := h.service.Login(context.Background(), "audit-wrong@example.com", "totally wrong password")
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("login: %v", err)
	}

	entry := h.auditLog.findEntry(audit.ActionLoginFailed)
	if entry == nil {
		t.Fatal("expected a login.failed audit entry")
	}
	if entry.ActorUserID == nil || *entry.ActorUserID != u.ID {
		t.Fatalf("actor_user_id = %v, want %s", entry.ActorUserID, u.ID)
	}
	if entry.Metadata["reason"] != "wrong_password" {
		t.Fatalf("metadata reason = %v, want wrong_password", entry.Metadata["reason"])
	}
}

func TestLogin_UnknownEmail_LogsLoginFailedWithNoActor(t *testing.T) {
	h := newHarness()

	_, err := h.service.Login(context.Background(), "nobody@example.com", testPassword)
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("login: %v", err)
	}

	entry := h.auditLog.findEntry(audit.ActionLoginFailed)
	if entry == nil {
		t.Fatal("expected a login.failed audit entry")
	}
	if entry.ActorUserID != nil {
		t.Fatalf("expected no actor for an unknown email, got %v", *entry.ActorUserID)
	}
	if entry.Metadata["reason"] != "unknown_email" {
		t.Fatalf("metadata reason = %v, want unknown_email", entry.Metadata["reason"])
	}
}

func TestLogin_FifthFailure_LogsLoginLockedOut(t *testing.T) {
	h := newHarness()
	u := h.signup(t, "audit-lockout@example.com")

	for range 4 {
		_, _ = h.service.Login(context.Background(), "audit-lockout@example.com", "wrong")
	}
	if h.auditLog.findEntry(audit.ActionLoginLockedOut) != nil {
		t.Fatal("expected no lockout event before the 5th failure")
	}

	_, err := h.service.Login(context.Background(), "audit-lockout@example.com", "wrong")
	if err == nil {
		t.Fatal("expected the 5th attempt to fail")
	}

	entry := h.auditLog.findEntry(audit.ActionLoginLockedOut)
	if entry == nil {
		t.Fatal("expected a login.locked_out audit entry on the 5th failure")
	}
	if entry.ActorUserID == nil || *entry.ActorUserID != u.ID {
		t.Fatalf("actor_user_id = %v, want %s", entry.ActorUserID, u.ID)
	}
}

func TestLogin_SuccessAfterFailures_LogsLockoutCleared(t *testing.T) {
	h := newHarness()
	u := h.signup(t, "audit-cleared@example.com")

	if _, err := h.service.Login(context.Background(), "audit-cleared@example.com", "wrong"); err == nil {
		t.Fatal("expected the wrong-password attempt to fail")
	}

	if _, err := h.service.Login(context.Background(), "audit-cleared@example.com", testPassword); err != nil {
		t.Fatalf("login: %v", err)
	}

	entry := h.auditLog.findEntry(audit.ActionLockoutCleared)
	if entry == nil {
		t.Fatal("expected a login.lockout_cleared audit entry")
	}
	if entry.ActorUserID == nil || *entry.ActorUserID != u.ID {
		t.Fatalf("actor_user_id = %v, want %s", entry.ActorUserID, u.ID)
	}
}

func TestLogin_SuccessWithNoPriorFailures_DoesNotLogLockoutCleared(t *testing.T) {
	h := newHarness()
	h.signup(t, "audit-clean@example.com")

	if _, err := h.service.Login(context.Background(), "audit-clean@example.com", testPassword); err != nil {
		t.Fatalf("login: %v", err)
	}

	if h.auditLog.findEntry(audit.ActionLockoutCleared) != nil {
		t.Fatal("expected no lockout_cleared event when there was nothing to clear")
	}
}

func TestConfirmTOTP_Success_LogsTOTPEnrolled(t *testing.T) {
	h := newHarness()
	u := h.signup(t, "audit-totp@example.com")
	h.enrollAndConfirmTOTP(t, u.ID)

	entry := h.auditLog.findEntry(audit.ActionTOTPEnrolled)
	if entry == nil {
		t.Fatal("expected a totp.enrolled audit entry")
	}
	if entry.ActorUserID == nil || *entry.ActorUserID != u.ID {
		t.Fatalf("actor_user_id = %v, want %s", entry.ActorUserID, u.ID)
	}
}

func TestForgotPassword_KnownEmail_LogsPasswordResetRequested(t *testing.T) {
	h := newHarness()
	u := h.signup(t, "audit-forgot@example.com")

	if err := h.service.ForgotPassword(context.Background(), "audit-forgot@example.com"); err != nil {
		t.Fatalf("forgot password: %v", err)
	}

	entry := h.auditLog.findEntry(audit.ActionPasswordResetRequested)
	if entry == nil {
		t.Fatal("expected a password.reset_requested audit entry")
	}
	if entry.ActorUserID == nil || *entry.ActorUserID != u.ID {
		t.Fatalf("actor_user_id = %v, want %s", entry.ActorUserID, u.ID)
	}
	if entry.Metadata["found"] != true {
		t.Fatalf("metadata found = %v, want true", entry.Metadata["found"])
	}
}

func TestForgotPassword_UnknownEmail_LogsPasswordResetRequestedWithNoActor(t *testing.T) {
	h := newHarness()

	if err := h.service.ForgotPassword(context.Background(), "nobody@example.com"); err != nil {
		t.Fatalf("forgot password: %v", err)
	}

	entry := h.auditLog.findEntry(audit.ActionPasswordResetRequested)
	if entry == nil {
		t.Fatal("expected a password.reset_requested audit entry even for an unknown email")
	}
	if entry.ActorUserID != nil {
		t.Fatalf("expected no actor for an unknown email, got %v", *entry.ActorUserID)
	}
	if entry.Metadata["found"] != false {
		t.Fatalf("metadata found = %v, want false", entry.Metadata["found"])
	}
}

func TestResetPassword_Success_LogsPasswordResetCompleted(t *testing.T) {
	h := newHarness()
	u := h.signup(t, "audit-reset@example.com")
	if err := h.service.ForgotPassword(context.Background(), "audit-reset@example.com"); err != nil {
		t.Fatalf("forgot password: %v", err)
	}
	token := extractResetToken(t, h.mailer.sentLink[0])

	if err := h.service.ResetPassword(context.Background(), token, "a brand new password"); err != nil {
		t.Fatalf("reset password: %v", err)
	}

	entry := h.auditLog.findEntry(audit.ActionPasswordResetCompleted)
	if entry == nil {
		t.Fatal("expected a password.reset_completed audit entry")
	}
	if entry.ActorUserID == nil || *entry.ActorUserID != u.ID {
		t.Fatalf("actor_user_id = %v, want %s", entry.ActorUserID, u.ID)
	}
}

// --- audit failures must never block the primary action --------------------

func TestLogin_AuditFailureDoesNotBlockLogin(t *testing.T) {
	h := newHarness()
	h.signup(t, "audit-fail@example.com")
	h.auditLog.failErr = errors.New("audit backend unavailable")

	result, err := h.service.Login(context.Background(), "audit-fail@example.com", testPassword)
	if err != nil {
		t.Fatalf("expected login to succeed despite the audit logger failing, got: %v", err)
	}
	if result.Status != auth.LoginStatusOK {
		t.Fatalf("expected status ok, got %q", result.Status)
	}
	if h.auditLog.findEntry(audit.ActionLoginSucceeded) == nil {
		t.Fatal("expected the audit logger to still have been called")
	}
}

func TestResetPassword_AuditFailureDoesNotBlockReset(t *testing.T) {
	h := newHarness()
	h.signup(t, "audit-fail-reset@example.com")
	if err := h.service.ForgotPassword(context.Background(), "audit-fail-reset@example.com"); err != nil {
		t.Fatalf("forgot password: %v", err)
	}
	token := extractResetToken(t, h.mailer.sentLink[0])

	h.auditLog.failErr = errors.New("audit backend unavailable")

	if err := h.service.ResetPassword(context.Background(), token, "a brand new password"); err != nil {
		t.Fatalf("expected reset to succeed despite the audit logger failing, got: %v", err)
	}

	result, err := h.service.Login(context.Background(), "audit-fail-reset@example.com", "a brand new password")
	if err != nil || result.Status != auth.LoginStatusOK {
		t.Fatalf("expected the new password to work, got result=%v err=%v", result, err)
	}
}

func TestConfirmTOTP_AuditFailureDoesNotBlockEnrollment(t *testing.T) {
	h := newHarness()
	u := h.signup(t, "audit-fail-totp@example.com")

	secret, _, err := h.service.EnrollTOTP(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}

	h.auditLog.failErr = errors.New("audit backend unavailable")

	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}
	if err := h.service.ConfirmTOTP(context.Background(), u.ID, code); err != nil {
		t.Fatalf("expected confirm to succeed despite the audit logger failing, got: %v", err)
	}
}
