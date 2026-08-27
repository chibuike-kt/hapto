package auth_test

import (
	"bytes"
	"crypto/rand"
	"testing"

	"github.com/chibuike-kt/hapto-api/internal/auth"
)

func randomKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

func TestTOTPSecret_EncryptDecryptRoundTrip(t *testing.T) {
	key := randomKey(t)
	plaintext := []byte("JBSWY3DPEHPK3PXP")

	ciphertext, err := auth.EncryptTOTPSecret(key, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if bytes.Equal(ciphertext, plaintext) {
		t.Fatal("ciphertext must not equal plaintext")
	}

	decrypted, err := auth.DecryptTOTPSecret(key, ciphertext)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("decrypted = %q, want %q", decrypted, plaintext)
	}
}

func TestTOTPSecret_DecryptWithWrongKeyFails(t *testing.T) {
	ciphertext, err := auth.EncryptTOTPSecret(randomKey(t), []byte("secret"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	if _, err := auth.DecryptTOTPSecret(randomKey(t), ciphertext); err == nil {
		t.Fatal("expected decryption with the wrong key to fail")
	}
}

func TestTOTPSecret_RejectsNonAES256Key(t *testing.T) {
	if _, err := auth.EncryptTOTPSecret([]byte("too-short"), []byte("secret")); err == nil {
		t.Fatal("expected a non-32-byte key to be rejected")
	}
}
