package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"
)

type LoginStatus string

const (
	LoginStatusOK           LoginStatus = "ok"
	LoginStatusTOTPRequired LoginStatus = "totp_required"
)

type LoginResult struct {
	Status         LoginStatus
	SessionToken   string
	PendingLoginID string
}

// LockedError carries how much longer an account is locked for. It still
// satisfies errors.Is(err, ErrAccountLocked) so callers that only care about
// the category don't need to know about this type.
type LockedError struct {
	RetryAfter time.Duration
}

func (e *LockedError) Error() string { return "account locked" }
func (e *LockedError) Is(target error) bool {
	return target == ErrAccountLocked //nolint:errorlint
}

type ServiceConfig struct {
	Pepper       string
	TOTPKey      []byte
	TOTPIssuer   string
	ResetBaseURL string
}

type Service struct {
	store    Store
	sessions Sessions
	lockout  Lockout
	pending  PendingLogins
	mailer   Mailer

	pepper       string
	totpKey      []byte
	totpIssuer   string
	resetBaseURL string
}

func NewService(store Store, sessions Sessions, lockout Lockout, pending PendingLogins, mailer Mailer, cfg ServiceConfig) *Service {
	issuer := cfg.TOTPIssuer
	if issuer == "" {
		issuer = "hapto"
	}
	return &Service{
		store:        store,
		sessions:     sessions,
		lockout:      lockout,
		pending:      pending,
		mailer:       mailer,
		pepper:       cfg.Pepper,
		totpKey:      cfg.TOTPKey,
		totpIssuer:   issuer,
		resetBaseURL: cfg.ResetBaseURL,
	}
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func (s *Service) Signup(ctx context.Context, email, password string) (*User, error) {
	email = normalizeEmail(email)

	if err := ValidatePasswordLength(password); err != nil {
		return nil, err
	}

	hash, err := HashPassword(s.pepper, password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	u := &User{
		ID:           uuid.NewString(),
		Email:        email,
		PasswordHash: hash,
		Status:       UserStatusActive,
		CreatedAt:    time.Now().UTC(),
	}

	if err := s.store.CreateUser(ctx, u); err != nil {
		return nil, err // ErrEmailTaken or infra error, both pass through as-is
	}

	return u, nil
}

func (s *Service) Login(ctx context.Context, email, password string) (*LoginResult, error) {
	email = normalizeEmail(email)

	user, err := s.store.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("get user: %w", err)
	}

	locked, retryAfter, err := s.lockout.IsLocked(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("check lockout: %w", err)
	}
	if locked {
		return nil, &LockedError{RetryAfter: retryAfter}
	}

	ok, err := VerifyPassword(s.pepper, password, user.PasswordHash)
	if err != nil {
		return nil, fmt.Errorf("verify password: %w", err)
	}
	if !ok {
		if err := s.lockout.RecordFailure(ctx, user.ID); err != nil {
			return nil, fmt.Errorf("record failure: %w", err)
		}
		return nil, ErrInvalidCredentials
	}

	totpRow, err := s.store.GetTOTP(ctx, user.ID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("get totp: %w", err)
	}

	if totpRow.Enabled() {
		pendingID, err := s.pending.Create(ctx, user.ID)
		if err != nil {
			return nil, fmt.Errorf("create pending login: %w", err)
		}
		return &LoginResult{Status: LoginStatusTOTPRequired, PendingLoginID: pendingID}, nil
	}

	if err := s.lockout.Reset(ctx, user.ID); err != nil {
		return nil, fmt.Errorf("reset lockout: %w", err)
	}

	token, err := s.sessions.Create(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	return &LoginResult{Status: LoginStatusOK, SessionToken: token}, nil
}

func (s *Service) VerifyTOTPLogin(ctx context.Context, pendingLoginID, code string) (*LoginResult, error) {
	userID, err := s.pending.Get(ctx, pendingLoginID)
	if err != nil {
		return nil, err
	}

	locked, retryAfter, err := s.lockout.IsLocked(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("check lockout: %w", err)
	}
	if locked {
		return nil, &LockedError{RetryAfter: retryAfter}
	}

	totpRow, err := s.store.GetTOTP(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrTOTPNotEnrolled
		}
		return nil, fmt.Errorf("get totp: %w", err)
	}
	if !totpRow.Enabled() {
		return nil, ErrTOTPNotEnrolled
	}

	secret, err := DecryptTOTPSecret(s.totpKey, totpRow.EncryptedSecret)
	if err != nil {
		return nil, fmt.Errorf("decrypt totp secret: %w", err)
	}

	if !totp.Validate(code, string(secret)) {
		if err := s.lockout.RecordFailure(ctx, userID); err != nil {
			return nil, fmt.Errorf("record failure: %w", err)
		}
		return nil, ErrInvalidTOTPCode
	}

	if err := s.pending.Delete(ctx, pendingLoginID); err != nil {
		return nil, fmt.Errorf("delete pending login: %w", err)
	}
	if err := s.lockout.Reset(ctx, userID); err != nil {
		return nil, fmt.Errorf("reset lockout: %w", err)
	}

	token, err := s.sessions.Create(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	return &LoginResult{Status: LoginStatusOK, SessionToken: token}, nil
}

// EnrollTOTP generates a new secret and returns it alongside the otpauth://
// URL a client renders as a QR code. The secret is stored encrypted but not
// yet enabled — ConfirmTOTP flips that once the user proves possession.
func (s *Service) EnrollTOTP(ctx context.Context, userID string) (secret, otpauthURL string, err error) {
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return "", "", fmt.Errorf("get user: %w", err)
	}

	existing, err := s.store.GetTOTP(ctx, userID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return "", "", fmt.Errorf("get totp: %w", err)
	}
	if existing.Enabled() {
		return "", "", ErrTOTPAlreadyEnabled
	}

	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      s.totpIssuer,
		AccountName: user.Email,
	})
	if err != nil {
		return "", "", fmt.Errorf("generate totp key: %w", err)
	}

	encrypted, err := EncryptTOTPSecret(s.totpKey, []byte(key.Secret()))
	if err != nil {
		return "", "", fmt.Errorf("encrypt totp secret: %w", err)
	}

	if err := s.store.UpsertTOTPSecret(ctx, userID, encrypted); err != nil {
		return "", "", fmt.Errorf("store totp secret: %w", err)
	}

	return key.Secret(), key.URL(), nil
}

func (s *Service) ConfirmTOTP(ctx context.Context, userID, code string) error {
	row, err := s.store.GetTOTP(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrTOTPNotEnrolled
		}
		return fmt.Errorf("get totp: %w", err)
	}
	if row.Enabled() {
		return ErrTOTPAlreadyEnabled
	}

	secret, err := DecryptTOTPSecret(s.totpKey, row.EncryptedSecret)
	if err != nil {
		return fmt.Errorf("decrypt totp secret: %w", err)
	}

	if !totp.Validate(code, string(secret)) {
		return ErrInvalidTOTPCode
	}

	return s.store.EnableTOTP(ctx, userID, time.Now().UTC())
}

// ForgotPassword never reports whether the email exists: on a miss it
// simply does nothing and returns nil, the same as on success.
func (s *Service) ForgotPassword(ctx context.Context, email string) error {
	email = normalizeEmail(email)

	user, err := s.store.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return fmt.Errorf("get user: %w", err)
	}

	raw, hash, err := generateResetToken()
	if err != nil {
		return fmt.Errorf("generate reset token: %w", err)
	}

	resetToken := &PasswordResetToken{
		ID:        uuid.NewString(),
		UserID:    user.ID,
		TokenHash: hash,
		ExpiresAt: time.Now().UTC().Add(passwordResetTokenTTLMinutes * time.Minute),
		CreatedAt: time.Now().UTC(),
	}
	if err := s.store.CreatePasswordResetToken(ctx, resetToken); err != nil {
		return fmt.Errorf("store reset token: %w", err)
	}

	link := fmt.Sprintf("%s/reset-password?token=%s", strings.TrimRight(s.resetBaseURL, "/"), raw)
	if err := s.mailer.SendPasswordReset(ctx, user.Email, link); err != nil {
		return fmt.Errorf("send reset email: %w", err)
	}

	return nil
}

func (s *Service) ResetPassword(ctx context.Context, rawToken, newPassword string) error {
	if err := ValidatePasswordLength(newPassword); err != nil {
		return err
	}

	record, err := s.store.GetPasswordResetTokenByHash(ctx, hashResetToken(rawToken))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrInvalidResetToken
		}
		return fmt.Errorf("get reset token: %w", err)
	}
	if record.UsedAt != nil || time.Now().UTC().After(record.ExpiresAt) {
		return ErrInvalidResetToken
	}

	newHash, err := HashPassword(s.pepper, newPassword)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	if err := s.store.ApplyPasswordReset(ctx, record.ID, record.UserID, newHash, time.Now().UTC()); err != nil {
		return fmt.Errorf("apply password reset: %w", err)
	}

	if err := s.sessions.DeleteAllForUser(ctx, record.UserID); err != nil {
		return fmt.Errorf("invalidate sessions: %w", err)
	}

	return nil
}
