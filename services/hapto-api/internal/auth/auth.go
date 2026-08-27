// Package auth owns email+password authentication: signup, login, TOTP as a
// second factor, password reset, and account lockout. Sessions themselves
// (issuing, validating, sliding expiry) live in internal/session; auth only
// depends on that package through the narrow Sessions interface below.
package auth

import (
	"context"
	"errors"
	"time"
)

const MinPasswordLength = 12

var (
	ErrInvalidCredentials   = errors.New("invalid credentials")
	ErrAccountLocked        = errors.New("account locked")
	ErrEmailTaken           = errors.New("email already registered")
	ErrPasswordTooShort     = errors.New("password too short")
	ErrTOTPAlreadyEnabled   = errors.New("totp already enabled")
	ErrTOTPNotEnrolled      = errors.New("totp not enrolled")
	ErrInvalidTOTPCode      = errors.New("invalid totp code")
	ErrPendingLoginNotFound = errors.New("login not pending or expired")
	ErrInvalidResetToken    = errors.New("invalid or expired reset token")
)

type UserStatus string

const UserStatusActive UserStatus = "active"

type User struct {
	ID           string
	Email        string
	PasswordHash string
	Status       UserStatus
	CreatedAt    time.Time
}

type TOTP struct {
	UserID          string
	EncryptedSecret []byte
	EnabledAt       *time.Time
}

func (t *TOTP) Enabled() bool {
	return t != nil && t.EnabledAt != nil
}

type PasswordResetToken struct {
	ID        string
	UserID    string
	TokenHash string
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}

// Store persists users, their TOTP enrollment, and password reset tokens.
type Store interface {
	CreateUser(ctx context.Context, u *User) error
	GetUserByEmail(ctx context.Context, email string) (*User, error)
	GetUserByID(ctx context.Context, id string) (*User, error)

	UpsertTOTPSecret(ctx context.Context, userID string, encryptedSecret []byte) error
	EnableTOTP(ctx context.Context, userID string, enabledAt time.Time) error
	GetTOTP(ctx context.Context, userID string) (*TOTP, error)

	CreatePasswordResetToken(ctx context.Context, t *PasswordResetToken) error
	GetPasswordResetTokenByHash(ctx context.Context, tokenHash string) (*PasswordResetToken, error)

	// ApplyPasswordReset updates the user's password and marks the reset
	// token used in a single transaction, so a mid-flight failure can never
	// leave a token marked used without the password actually changing (or
	// vice versa).
	ApplyPasswordReset(ctx context.Context, tokenID, userID, newPasswordHash string, usedAt time.Time) error
}

var ErrNotFound = errors.New("not found")

// Sessions is the subset of internal/session's Store that auth needs: issue
// a session on successful login, and blow away every session a user has on
// password reset.
type Sessions interface {
	Create(ctx context.Context, userID string) (token string, err error)
	DeleteAllForUser(ctx context.Context, userID string) error
}

// Mailer sends the one transactional email auth needs.
type Mailer interface {
	SendPasswordReset(ctx context.Context, to, resetLink string) error
}

// Lockout tracks failed login attempts against an account. Satisfied by
// *LockoutTracker in production; fakeable in unit tests.
type Lockout interface {
	IsLocked(ctx context.Context, userID string) (locked bool, retryAfter time.Duration, err error)
	RecordFailure(ctx context.Context, userID string) error
	Reset(ctx context.Context, userID string) error
}

// PendingLogins holds the state between a password check that requires a
// TOTP second factor and the verify-totp call that completes it. Satisfied
// by *PendingLoginStore in production; fakeable in unit tests.
type PendingLogins interface {
	Create(ctx context.Context, userID string) (id string, err error)
	Get(ctx context.Context, id string) (userID string, err error)
	Delete(ctx context.Context, id string) error
}
