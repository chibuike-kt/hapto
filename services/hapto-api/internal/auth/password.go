package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	argonSaltLen = 16
	argonKeyLen  = 32
)

type argonParams struct {
	memory  uint32
	time    uint32
	threads uint8
}

var defaultArgonParams = argonParams{memory: 64 * 1024, time: 3, threads: 4}

// HashPassword hashes a password with Argon2id, mixing in the server-wide
// pepper alongside the per-password salt Argon2 generates internally.
func HashPassword(pepper, password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	p := defaultArgonParams
	hash := argon2.IDKey(pepperedPassword(pepper, password), salt, p.time, p.memory, uint8(p.threads), argonKeyLen)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.memory, p.time, p.threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

// VerifyPassword checks a password against an encoded hash produced by
// HashPassword. It returns (false, nil) for a simple mismatch and a non-nil
// error only if the encoded hash itself is malformed.
func VerifyPassword(pepper, password, encoded string) (bool, error) {
	p, salt, hash, err := decodeArgonHash(encoded)
	if err != nil {
		return false, err
	}
	// A decoded hash from our own format is always argonKeyLen bytes; this
	// guards the int -> uint32 conversion below rather than trusting that.
	if len(hash) == 0 || len(hash) > 1<<20 {
		return false, fmt.Errorf("unexpected hash length %d", len(hash))
	}

	candidate := argon2.IDKey(pepperedPassword(pepper, password), salt, p.time, p.memory, p.threads, uint32(len(hash))) //nolint:gosec // bounds checked above
	return subtle.ConstantTimeCompare(hash, candidate) == 1, nil
}

func pepperedPassword(pepper, password string) []byte {
	return []byte(pepper + password)
}

func decodeArgonHash(encoded string) (argonParams, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return argonParams{}, nil, nil, fmt.Errorf("unrecognized password hash format")
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return argonParams{}, nil, nil, fmt.Errorf("parse hash version: %w", err)
	}
	if version != argon2.Version {
		return argonParams{}, nil, nil, fmt.Errorf("unsupported argon2 version %d", version)
	}

	var p argonParams
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.time, &p.threads); err != nil {
		return argonParams{}, nil, nil, fmt.Errorf("parse hash params: %w", err)
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return argonParams{}, nil, nil, fmt.Errorf("decode salt: %w", err)
	}

	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return argonParams{}, nil, nil, fmt.Errorf("decode hash: %w", err)
	}

	return p, salt, hash, nil
}

// ValidatePasswordLength enforces the one password rule hapto has: a
// minimum length, checked by rune count rather than byte length.
func ValidatePasswordLength(password string) error {
	if utf8.RuneCountInString(password) < MinPasswordLength {
		return ErrPasswordTooShort
	}
	return nil
}
