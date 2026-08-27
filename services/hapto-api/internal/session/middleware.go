package session

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

type ctxKey int

const sessionCtxKey ctxKey = iota

// FromContext retrieves the session a preceding RequireSession call
// validated and attached to the request context.
func FromContext(ctx context.Context) (*Session, bool) {
	sess, ok := ctx.Value(sessionCtxKey).(*Session)
	return sess, ok
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(h, prefix))
}

// RequireSession validates the bearer token on every request, rejecting
// missing, unknown, or expired sessions before the wrapped handler runs.
func (s *Store) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			writeAuthError(w, http.StatusUnauthorized, "missing session token")
			return
		}

		sess, err := s.Get(r.Context(), token)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				writeAuthError(w, http.StatusUnauthorized, "invalid or expired session")
				return
			}
			writeAuthError(w, http.StatusInternalServerError, "session lookup failed")
			return
		}

		ctx := context.WithValue(r.Context(), sessionCtxKey, sess)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireStepUp gates a handler behind a recent step-up (re-auth)
// verification on top of an already-valid session. No endpoint uses this
// yet — it's the mechanism future sensitive actions (changing a payout
// destination, revoking a device) will plug into. Must run after
// RequireSession.
func RequireStepUp(maxAge time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sess, ok := FromContext(r.Context())
			if !ok {
				writeAuthError(w, http.StatusUnauthorized, "missing session token")
				return
			}
			if sess.StepUpAt == nil || time.Since(*sess.StepUpAt) > maxAge {
				writeAuthError(w, http.StatusForbidden, "step_up_required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeAuthError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Error string `json:"error"`
	}{Error: msg})
}
