package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/chibuike-kt/hapto-api/internal/session"
)

// RateLimiter throttles attempt frequency, independent of account lockout.
// Satisfied by internal/ratelimit.Limiter.
type RateLimiter interface {
	Allow(ctx context.Context, key string) (allowed bool, retryAfter time.Duration, err error)
}

type Handler struct {
	service *Service
	limiter RateLimiter
}

func NewHandler(service *Service, limiter RateLimiter) *Handler {
	return &Handler{service: service, limiter: limiter}
}

func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		first, _, _ := strings.Cut(fwd, ",")
		return first
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// checkRateLimit enforces both the per-IP and per-email throttle for a
// login-shaped endpoint. It writes the response itself and returns false
// when the request should stop here.
func (h *Handler) checkRateLimit(w http.ResponseWriter, r *http.Request, scope, email string) bool {
	ctx := r.Context()

	for _, key := range []string{scope + ":ip:" + clientIP(r), scope + ":email:" + normalizeEmail(email)} {
		allowed, retryAfter, err := h.limiter.Allow(ctx, key)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "rate limit check failed")
			return false
		}
		if !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
			writeJSON(w, http.StatusTooManyRequests, map[string]any{
				"error":               "rate_limited",
				"retry_after_seconds": int(retryAfter.Seconds()) + 1,
			})
			return false
		}
	}
	return true
}

type signupRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) Signup(w http.ResponseWriter, r *http.Request) {
	var req signupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, err := h.service.Signup(r.Context(), req.Email, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, ErrPasswordTooShort):
			writeError(w, http.StatusUnprocessableEntity, err.Error())
		case errors.Is(err, ErrEmailTaken):
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "failed to create account")
		}
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         user.ID,
		"email":      user.Email,
		"status":     user.Status,
		"created_at": user.CreatedAt.Format(time.RFC3339),
	})
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if !h.checkRateLimit(w, r, "login", req.Email) {
		return
	}

	result, err := h.service.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		writeLoginError(w, err)
		return
	}

	writeLoginResult(w, result)
}

type verifyTOTPRequest struct {
	SessionID string `json:"session_id"`
	Code      string `json:"code"`
}

func (h *Handler) VerifyTOTPLogin(w http.ResponseWriter, r *http.Request) {
	var req verifyTOTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	result, err := h.service.VerifyTOTPLogin(r.Context(), req.SessionID, req.Code)
	if err != nil {
		writeLoginError(w, err)
		return
	}

	writeLoginResult(w, result)
}

func writeLoginResult(w http.ResponseWriter, result *LoginResult) {
	switch result.Status {
	case LoginStatusTOTPRequired:
		writeJSON(w, http.StatusOK, map[string]any{
			"status":     result.Status,
			"session_id": result.PendingLoginID,
		})
	default:
		writeJSON(w, http.StatusOK, map[string]any{
			"status":        result.Status,
			"session_token": result.SessionToken,
		})
	}
}

func writeLoginError(w http.ResponseWriter, err error) {
	var locked *LockedError
	switch {
	case errors.As(err, &locked):
		w.Header().Set("Retry-After", strconv.Itoa(int(locked.RetryAfter.Seconds())+1))
		writeJSON(w, http.StatusLocked, map[string]any{
			"error":               "account_locked",
			"retry_after_seconds": int(locked.RetryAfter.Seconds()) + 1,
		})
	case errors.Is(err, ErrInvalidCredentials), errors.Is(err, ErrInvalidTOTPCode):
		writeError(w, http.StatusUnauthorized, "invalid_credentials")
	case errors.Is(err, ErrPendingLoginNotFound), errors.Is(err, ErrTOTPNotEnrolled):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "login failed")
	}
}

func (h *Handler) EnrollTOTP(w http.ResponseWriter, r *http.Request) {
	sess, ok := session.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing session")
		return
	}

	secret, otpauthURL, err := h.service.EnrollTOTP(r.Context(), sess.UserID)
	if err != nil {
		if errors.Is(err, ErrTOTPAlreadyEnabled) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to enroll totp")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"secret":      secret,
		"otpauth_url": otpauthURL,
	})
}

type confirmTOTPRequest struct {
	Code string `json:"code"`
}

func (h *Handler) ConfirmTOTP(w http.ResponseWriter, r *http.Request) {
	sess, ok := session.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing session")
		return
	}

	var req confirmTOTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.service.ConfirmTOTP(r.Context(), sess.UserID, req.Code); err != nil {
		switch {
		case errors.Is(err, ErrInvalidTOTPCode):
			writeError(w, http.StatusUnauthorized, err.Error())
		case errors.Is(err, ErrTOTPAlreadyEnabled), errors.Is(err, ErrTOTPNotEnrolled):
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "failed to confirm totp")
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "enabled"})
}

type forgotPasswordRequest struct {
	Email string `json:"email"`
}

// ForgotPassword always answers with the same generic response, whether or
// not the email exists and regardless of any internal failure, so the
// response itself can never be used to enumerate accounts.
func (h *Handler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req forgotPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if !h.checkRateLimit(w, r, "password_forgot", req.Email) {
		return
	}

	if err := h.service.ForgotPassword(r.Context(), req.Email); err != nil {
		// Logged, not surfaced: see the doc comment above.
		writeJSON(w, http.StatusOK, genericForgotResponse)
		return
	}

	writeJSON(w, http.StatusOK, genericForgotResponse)
}

var genericForgotResponse = map[string]any{
	"message": "if an account with that email exists, a password reset link has been sent",
}

type resetPasswordRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

func (h *Handler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req resetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.service.ResetPassword(r.Context(), req.Token, req.NewPassword); err != nil {
		switch {
		case errors.Is(err, ErrPasswordTooShort):
			writeError(w, http.StatusUnprocessableEntity, err.Error())
		case errors.Is(err, ErrInvalidResetToken):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "failed to reset password")
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
