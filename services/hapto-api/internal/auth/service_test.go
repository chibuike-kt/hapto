package auth_test

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/chibuike-kt/hapto-api/internal/auth"
)

// --- fakes ---------------------------------------------------------------

type fakeStore struct {
	usersByID    map[string]*auth.User
	usersByEmail map[string]*auth.User
	totp         map[string]*auth.TOTP
	resetTokens  map[string]*auth.PasswordResetToken // keyed by token hash
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		usersByID:    map[string]*auth.User{},
		usersByEmail: map[string]*auth.User{},
		totp:         map[string]*auth.TOTP{},
		resetTokens:  map[string]*auth.PasswordResetToken{},
	}
}

func (f *fakeStore) CreateUser(_ context.Context, u *auth.User) error {
	if _, exists := f.usersByEmail[u.Email]; exists {
		return auth.ErrEmailTaken
	}
	cp := *u
	f.usersByID[u.ID] = &cp
	f.usersByEmail[u.Email] = &cp
	return nil
}

func (f *fakeStore) GetUserByEmail(_ context.Context, email string) (*auth.User, error) {
	u, ok := f.usersByEmail[email]
	if !ok {
		return nil, auth.ErrNotFound
	}
	cp := *u
	return &cp, nil
}

func (f *fakeStore) GetUserByID(_ context.Context, id string) (*auth.User, error) {
	u, ok := f.usersByID[id]
	if !ok {
		return nil, auth.ErrNotFound
	}
	cp := *u
	return &cp, nil
}

func (f *fakeStore) UpsertTOTPSecret(_ context.Context, userID string, encryptedSecret []byte) error {
	f.totp[userID] = &auth.TOTP{UserID: userID, EncryptedSecret: encryptedSecret}
	return nil
}

func (f *fakeStore) EnableTOTP(_ context.Context, userID string, enabledAt time.Time) error {
	t, ok := f.totp[userID]
	if !ok {
		return auth.ErrNotFound
	}
	t.EnabledAt = &enabledAt
	return nil
}

func (f *fakeStore) GetTOTP(_ context.Context, userID string) (*auth.TOTP, error) {
	t, ok := f.totp[userID]
	if !ok {
		return nil, auth.ErrNotFound
	}
	cp := *t
	return &cp, nil
}

func (f *fakeStore) CreatePasswordResetToken(_ context.Context, t *auth.PasswordResetToken) error {
	cp := *t
	f.resetTokens[t.TokenHash] = &cp
	return nil
}

func (f *fakeStore) GetPasswordResetTokenByHash(_ context.Context, hash string) (*auth.PasswordResetToken, error) {
	t, ok := f.resetTokens[hash]
	if !ok {
		return nil, auth.ErrNotFound
	}
	cp := *t
	return &cp, nil
}

func (f *fakeStore) ApplyPasswordReset(_ context.Context, tokenID, userID, newPasswordHash string, usedAt time.Time) error {
	u, ok := f.usersByID[userID]
	if !ok {
		return auth.ErrNotFound
	}

	var token *auth.PasswordResetToken
	for _, t := range f.resetTokens {
		if t.ID == tokenID {
			token = t
			break
		}
	}
	if token == nil {
		return auth.ErrNotFound
	}
	if token.UsedAt != nil {
		return fmt.Errorf("reset token %s already used", tokenID)
	}

	u.PasswordHash = newPasswordHash
	token.UsedAt = &usedAt
	return nil
}

type fakeSessions struct {
	created        map[string]string // token -> userID
	deletedForUser []string
	seq            int
}

func newFakeSessions() *fakeSessions {
	return &fakeSessions{created: map[string]string{}}
}

func (f *fakeSessions) Create(_ context.Context, userID string) (string, error) {
	f.seq++
	token := fmt.Sprintf("session-%d", f.seq)
	f.created[token] = userID
	return token, nil
}

func (f *fakeSessions) DeleteAllForUser(_ context.Context, userID string) error {
	f.deletedForUser = append(f.deletedForUser, userID)
	for tok, uid := range f.created {
		if uid == userID {
			delete(f.created, tok)
		}
	}
	return nil
}

type fakeMailer struct {
	sentTo   []string
	sentLink []string
}

func (f *fakeMailer) SendPasswordReset(_ context.Context, to, link string) error {
	f.sentTo = append(f.sentTo, to)
	f.sentLink = append(f.sentLink, link)
	return nil
}

type fakeLockout struct {
	failures map[string]int
	locked   map[string]time.Duration
}

func newFakeLockout() *fakeLockout {
	return &fakeLockout{failures: map[string]int{}, locked: map[string]time.Duration{}}
}

func (f *fakeLockout) IsLocked(_ context.Context, userID string) (bool, time.Duration, error) {
	d, ok := f.locked[userID]
	return ok, d, nil
}

func (f *fakeLockout) RecordFailure(_ context.Context, userID string) error {
	f.failures[userID]++
	if f.failures[userID] >= 5 {
		f.locked[userID] = 15 * time.Minute
	}
	return nil
}

func (f *fakeLockout) Reset(_ context.Context, userID string) error {
	delete(f.failures, userID)
	delete(f.locked, userID)
	return nil
}

type fakePending struct {
	byID map[string]string
	seq  int
}

func newFakePending() *fakePending {
	return &fakePending{byID: map[string]string{}}
}

func (f *fakePending) Create(_ context.Context, userID string) (string, error) {
	f.seq++
	id := fmt.Sprintf("pending-%d", f.seq)
	f.byID[id] = userID
	return id, nil
}

func (f *fakePending) Get(_ context.Context, id string) (string, error) {
	uid, ok := f.byID[id]
	if !ok {
		return "", auth.ErrPendingLoginNotFound
	}
	return uid, nil
}

func (f *fakePending) Delete(_ context.Context, id string) error {
	delete(f.byID, id)
	return nil
}

// --- test harness ----------------------------------------------------------

type harness struct {
	store    *fakeStore
	sessions *fakeSessions
	lockout  *fakeLockout
	pending  *fakePending
	mailer   *fakeMailer
	service  *auth.Service
}

func newHarness() *harness {
	h := &harness{
		store:    newFakeStore(),
		sessions: newFakeSessions(),
		lockout:  newFakeLockout(),
		pending:  newFakePending(),
		mailer:   &fakeMailer{},
	}
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	h.service = auth.NewService(h.store, h.sessions, h.lockout, h.pending, h.mailer, auth.ServiceConfig{
		Pepper:       "test-pepper",
		TOTPKey:      key,
		TOTPIssuer:   "hapto-test",
		ResetBaseURL: "https://app.example.test",
	})
	return h
}

const testPassword = "correct horse battery staple"

func (h *harness) signup(t *testing.T, email string) *auth.User {
	t.Helper()
	u, err := h.service.Signup(context.Background(), email, testPassword)
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	return u
}

// enrollAndConfirmTOTP enrolls TOTP for a user and returns the secret, so
// the test can generate valid codes.
func (h *harness) enrollAndConfirmTOTP(t *testing.T, userID string) string {
	t.Helper()
	secret, _, err := h.service.EnrollTOTP(context.Background(), userID)
	if err != nil {
		t.Fatalf("enroll totp: %v", err)
	}
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}
	if err := h.service.ConfirmTOTP(context.Background(), userID, code); err != nil {
		t.Fatalf("confirm totp: %v", err)
	}
	return secret
}

// --- signup ------------------------------------------------------------

func TestSignup_Success(t *testing.T) {
	h := newHarness()
	u := h.signup(t, "Alice@Example.com")

	if u.Email != "alice@example.com" {
		t.Fatalf("expected normalized email, got %q", u.Email)
	}
	if u.Status != auth.UserStatusActive {
		t.Fatalf("expected active status, got %q", u.Status)
	}
	if u.PasswordHash == testPassword {
		t.Fatal("password must not be stored in plaintext")
	}

	ok, err := auth.VerifyPassword("test-pepper", testPassword, u.PasswordHash)
	if err != nil || !ok {
		t.Fatalf("stored hash does not verify: ok=%v err=%v", ok, err)
	}
}

func TestSignup_PasswordTooShort(t *testing.T) {
	h := newHarness()
	_, err := h.service.Signup(context.Background(), "bob@example.com", "short")
	if !errors.Is(err, auth.ErrPasswordTooShort) {
		t.Fatalf("expected ErrPasswordTooShort, got %v", err)
	}
	if len(h.store.usersByEmail) != 0 {
		t.Fatal("expected no user to be created")
	}
}

func TestSignup_EmailTaken(t *testing.T) {
	h := newHarness()
	h.signup(t, "carol@example.com")

	_, err := h.service.Signup(context.Background(), "carol@example.com", testPassword)
	if !errors.Is(err, auth.ErrEmailTaken) {
		t.Fatalf("expected ErrEmailTaken, got %v", err)
	}
}

// --- login ---------------------------------------------------------------

func TestLogin_Success_NoTOTP(t *testing.T) {
	h := newHarness()
	u := h.signup(t, "dave@example.com")

	result, err := h.service.Login(context.Background(), "dave@example.com", testPassword)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if result.Status != auth.LoginStatusOK {
		t.Fatalf("expected status ok, got %q", result.Status)
	}
	if h.sessions.created[result.SessionToken] != u.ID {
		t.Fatal("expected a session to be created for the user")
	}
}

func TestLogin_UnknownEmail(t *testing.T) {
	h := newHarness()
	_, err := h.service.Login(context.Background(), "nobody@example.com", testPassword)
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
	if len(h.lockout.failures) != 0 {
		t.Fatal("expected no lockout tracking for an unknown account")
	}
}

func TestLogin_WrongPassword_RecordsFailure(t *testing.T) {
	h := newHarness()
	u := h.signup(t, "erin@example.com")

	_, err := h.service.Login(context.Background(), "erin@example.com", "totally wrong password")
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
	if h.lockout.failures[u.ID] != 1 {
		t.Fatalf("expected 1 recorded failure, got %d", h.lockout.failures[u.ID])
	}
}

func TestLogin_AccountLocked(t *testing.T) {
	h := newHarness()
	u := h.signup(t, "frank@example.com")
	h.lockout.locked[u.ID] = 7 * time.Minute

	_, err := h.service.Login(context.Background(), "frank@example.com", testPassword)

	var locked *auth.LockedError
	if !errors.As(err, &locked) {
		t.Fatalf("expected *LockedError, got %v", err)
	}
	if !errors.Is(err, auth.ErrAccountLocked) {
		t.Fatal("expected errors.Is(err, ErrAccountLocked) to hold")
	}
	if locked.RetryAfter != 7*time.Minute {
		t.Fatalf("retry after = %v, want 7m", locked.RetryAfter)
	}
	if len(h.sessions.created) != 0 {
		t.Fatal("expected no session to be created for a locked account")
	}
}

func TestLogin_TOTPRequired(t *testing.T) {
	h := newHarness()
	u := h.signup(t, "grace@example.com")
	h.enrollAndConfirmTOTP(t, u.ID)

	result, err := h.service.Login(context.Background(), "grace@example.com", testPassword)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if result.Status != auth.LoginStatusTOTPRequired {
		t.Fatalf("expected totp_required, got %q", result.Status)
	}
	if result.PendingLoginID == "" {
		t.Fatal("expected a pending login id")
	}
	if len(h.sessions.created) != 0 {
		t.Fatal("expected no full session before the second factor is verified")
	}
}

// --- totp login verification --------------------------------------------

func TestVerifyTOTPLogin_Success(t *testing.T) {
	h := newHarness()
	u := h.signup(t, "heidi@example.com")
	secret := h.enrollAndConfirmTOTP(t, u.ID)

	login, err := h.service.Login(context.Background(), "heidi@example.com", testPassword)
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}

	result, err := h.service.VerifyTOTPLogin(context.Background(), login.PendingLoginID, code)
	if err != nil {
		t.Fatalf("verify totp login: %v", err)
	}
	if result.Status != auth.LoginStatusOK {
		t.Fatalf("expected status ok, got %q", result.Status)
	}
	if h.sessions.created[result.SessionToken] != u.ID {
		t.Fatal("expected a session to be created for the user")
	}
	if _, err := h.pending.Get(context.Background(), login.PendingLoginID); !errors.Is(err, auth.ErrPendingLoginNotFound) {
		t.Fatal("expected the pending login to be consumed")
	}
}

func TestVerifyTOTPLogin_WrongCodeRecordsFailureAndKeepsPending(t *testing.T) {
	h := newHarness()
	u := h.signup(t, "ivan@example.com")
	h.enrollAndConfirmTOTP(t, u.ID)

	login, err := h.service.Login(context.Background(), "ivan@example.com", testPassword)
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	_, err = h.service.VerifyTOTPLogin(context.Background(), login.PendingLoginID, "000000")
	if !errors.Is(err, auth.ErrInvalidTOTPCode) {
		t.Fatalf("expected ErrInvalidTOTPCode, got %v", err)
	}
	if h.lockout.failures[u.ID] != 1 {
		t.Fatalf("expected 1 recorded failure, got %d", h.lockout.failures[u.ID])
	}
	if _, err := h.pending.Get(context.Background(), login.PendingLoginID); err != nil {
		t.Fatal("expected the pending login to survive a wrong code so the user can retry")
	}
}

func TestVerifyTOTPLogin_UnknownPendingID(t *testing.T) {
	h := newHarness()
	_, err := h.service.VerifyTOTPLogin(context.Background(), "does-not-exist", "123456")
	if !errors.Is(err, auth.ErrPendingLoginNotFound) {
		t.Fatalf("expected ErrPendingLoginNotFound, got %v", err)
	}
}

// --- totp enrollment -----------------------------------------------------

func TestEnrollTOTP_ThenConfirm_Enables(t *testing.T) {
	h := newHarness()
	u := h.signup(t, "judy@example.com")
	h.enrollAndConfirmTOTP(t, u.ID)

	row := h.store.totp[u.ID]
	if row == nil || !row.Enabled() {
		t.Fatal("expected totp to be enabled after confirmation")
	}
}

func TestEnrollTOTP_AlreadyEnabled(t *testing.T) {
	h := newHarness()
	u := h.signup(t, "mallory@example.com")
	h.enrollAndConfirmTOTP(t, u.ID)

	_, _, err := h.service.EnrollTOTP(context.Background(), u.ID)
	if !errors.Is(err, auth.ErrTOTPAlreadyEnabled) {
		t.Fatalf("expected ErrTOTPAlreadyEnabled, got %v", err)
	}
}

func TestConfirmTOTP_WrongCode(t *testing.T) {
	h := newHarness()
	u := h.signup(t, "oscar@example.com")
	if _, _, err := h.service.EnrollTOTP(context.Background(), u.ID); err != nil {
		t.Fatalf("enroll: %v", err)
	}

	err := h.service.ConfirmTOTP(context.Background(), u.ID, "000000")
	if !errors.Is(err, auth.ErrInvalidTOTPCode) {
		t.Fatalf("expected ErrInvalidTOTPCode, got %v", err)
	}
	if h.store.totp[u.ID].Enabled() {
		t.Fatal("expected totp to remain unconfirmed")
	}
}

// --- password reset --------------------------------------------------------

func TestForgotPassword_UnknownEmailSendsNothing(t *testing.T) {
	h := newHarness()
	if err := h.service.ForgotPassword(context.Background(), "nobody@example.com"); err != nil {
		t.Fatalf("forgot password: %v", err)
	}
	if len(h.mailer.sentTo) != 0 {
		t.Fatal("expected no email for an unknown address")
	}
	if len(h.store.resetTokens) != 0 {
		t.Fatal("expected no reset token to be created for an unknown address")
	}
}

// extractResetToken pulls the raw token out of the link the fake mailer
// captured, mirroring what a real reset-password page would read from the
// query string.
func extractResetToken(t *testing.T, link string) string {
	t.Helper()
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse link: %v", err)
	}
	token := u.Query().Get("token")
	if token == "" {
		t.Fatalf("no token in link %q", link)
	}
	return token
}

func TestForgotPassword_ExistingEmailSendsResetLink(t *testing.T) {
	h := newHarness()
	h.signup(t, "peggy@example.com")

	if err := h.service.ForgotPassword(context.Background(), "Peggy@Example.com"); err != nil {
		t.Fatalf("forgot password: %v", err)
	}
	if len(h.mailer.sentTo) != 1 || h.mailer.sentTo[0] != "peggy@example.com" {
		t.Fatalf("expected an email to peggy@example.com, got %v", h.mailer.sentTo)
	}
	if !strings.HasPrefix(h.mailer.sentLink[0], "https://app.example.test/reset-password?token=") {
		t.Fatalf("unexpected reset link: %q", h.mailer.sentLink[0])
	}
}

func TestResetPassword_Success_AllowsLoginWithNewPassword(t *testing.T) {
	h := newHarness()
	h.signup(t, "trent@example.com")
	if err := h.service.ForgotPassword(context.Background(), "trent@example.com"); err != nil {
		t.Fatalf("forgot password: %v", err)
	}
	token := extractResetToken(t, h.mailer.sentLink[0])

	const newPassword = "a brand new password"
	if err := h.service.ResetPassword(context.Background(), token, newPassword); err != nil {
		t.Fatalf("reset password: %v", err)
	}

	if _, err := h.service.Login(context.Background(), "trent@example.com", testPassword); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("expected old password to be rejected, got %v", err)
	}
	result, err := h.service.Login(context.Background(), "trent@example.com", newPassword)
	if err != nil || result.Status != auth.LoginStatusOK {
		t.Fatalf("expected new password to work, got result=%v err=%v", result, err)
	}
}

func TestResetPassword_InvalidatesExistingSessions(t *testing.T) {
	h := newHarness()
	u := h.signup(t, "sybil@example.com")
	login, err := h.service.Login(context.Background(), "sybil@example.com", testPassword)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if len(h.sessions.created) != 1 {
		t.Fatalf("expected 1 session to exist before reset, got %d", len(h.sessions.created))
	}

	if err := h.service.ForgotPassword(context.Background(), "sybil@example.com"); err != nil {
		t.Fatalf("forgot password: %v", err)
	}
	token := extractResetToken(t, h.mailer.sentLink[0])

	if err := h.service.ResetPassword(context.Background(), token, "a brand new password"); err != nil {
		t.Fatalf("reset password: %v", err)
	}

	if len(h.sessions.deletedForUser) != 1 || h.sessions.deletedForUser[0] != u.ID {
		t.Fatalf("expected DeleteAllForUser to be called for %s, got %v", u.ID, h.sessions.deletedForUser)
	}
	if _, stillExists := h.sessions.created[login.SessionToken]; stillExists {
		t.Fatal("expected the pre-reset session to be gone")
	}
}

func TestResetPassword_TokenIsSingleUse(t *testing.T) {
	h := newHarness()
	h.signup(t, "victor@example.com")
	if err := h.service.ForgotPassword(context.Background(), "victor@example.com"); err != nil {
		t.Fatalf("forgot password: %v", err)
	}
	token := extractResetToken(t, h.mailer.sentLink[0])

	if err := h.service.ResetPassword(context.Background(), token, "first new password!"); err != nil {
		t.Fatalf("first reset: %v", err)
	}

	err := h.service.ResetPassword(context.Background(), token, "second new password!")
	if err == nil {
		t.Fatal("expected reusing a spent reset token to fail")
	}
}

func TestResetPassword_ExpiredTokenRejected(t *testing.T) {
	h := newHarness()
	h.signup(t, "walter@example.com")
	if err := h.service.ForgotPassword(context.Background(), "walter@example.com"); err != nil {
		t.Fatalf("forgot password: %v", err)
	}
	token := extractResetToken(t, h.mailer.sentLink[0])

	for _, rec := range h.store.resetTokens {
		rec.ExpiresAt = time.Now().Add(-time.Minute)
	}

	err := h.service.ResetPassword(context.Background(), token, "a brand new password")
	if !errors.Is(err, auth.ErrInvalidResetToken) {
		t.Fatalf("expected ErrInvalidResetToken, got %v", err)
	}
}

func TestResetPassword_UnknownTokenRejected(t *testing.T) {
	h := newHarness()
	err := h.service.ResetPassword(context.Background(), "not-a-real-token", "a brand new password")
	if !errors.Is(err, auth.ErrInvalidResetToken) {
		t.Fatalf("expected ErrInvalidResetToken, got %v", err)
	}
}

func TestResetPassword_NewPasswordTooShort(t *testing.T) {
	h := newHarness()
	h.signup(t, "yara@example.com")
	if err := h.service.ForgotPassword(context.Background(), "yara@example.com"); err != nil {
		t.Fatalf("forgot password: %v", err)
	}
	token := extractResetToken(t, h.mailer.sentLink[0])

	err := h.service.ResetPassword(context.Background(), token, "short")
	if !errors.Is(err, auth.ErrPasswordTooShort) {
		t.Fatalf("expected ErrPasswordTooShort, got %v", err)
	}
}
