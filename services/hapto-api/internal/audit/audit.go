// Package audit is hapto's security/action history: who did what, to what,
// and when. It is deliberately separate from internal/ledger — the ledger
// is money, this is history, and the two are never conflated in one table
// or one code path. Like ledger_entries, audit_logs is append-only: there
// is no update or delete path anywhere in this package.
package audit

import "context"

// Entry describes one security-relevant event to record. ActorUserID and
// TargetID are both nullable: many events (a failed login against an email
// that isn't registered, for instance) have no authenticated actor and no
// resolvable target yet.
type Entry struct {
	ActorUserID *string
	Action      string
	TargetType  string
	TargetID    *string
	Metadata    map[string]any
}

// Logger records a security-relevant event. A logging failure must never
// block or roll back the action it describes. Every call site is expected
// to log the error and continue — see each call site's comment for why.
type Logger interface {
	Log(ctx context.Context, entry Entry) error
}

// Actions recorded by internal/auth and internal/device. Keeping the full
// vocabulary here (rather than scattering string literals across two
// packages) makes it possible to see hapto's entire audit surface in one
// place.
const (
	ActionLoginSucceeded         = "login.succeeded"
	ActionLoginFailed            = "login.failed"
	ActionLoginLockedOut         = "login.locked_out"
	ActionLockoutCleared         = "login.lockout_cleared"
	ActionDeviceRegistered       = "device.registered"
	ActionDeviceRevoked          = "device.revoked"
	ActionPasswordResetRequested = "password.reset_requested"
	ActionPasswordResetCompleted = "password.reset_completed"
	ActionTOTPEnrolled           = "totp.enrolled"

	ActionPaymentIntentCreated    = "payment_intent.created"
	ActionPaymentIntentAuthorized = "payment_intent.authorized"
	ActionPaymentIntentCompleted  = "payment_intent.completed"
	ActionPaymentIntentFailed     = "payment_intent.failed"
	ActionPaymentIntentExpired    = "payment_intent.expired"
)

const (
	TargetTypeUser          = "user"
	TargetTypeDevice        = "device"
	TargetTypePaymentIntent = "payment_intent"
)
