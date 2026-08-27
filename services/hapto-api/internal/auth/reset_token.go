package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

const passwordResetTokenTTLMinutes = 30

// generateResetToken returns the raw token to email the user and the hash
// to persist. The raw token is never stored: only its hash is, so a
// database read can never hand back something usable to reset a password.
func generateResetToken() (raw, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generate reset token: %w", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, hashResetToken(raw), nil
}

func hashResetToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
