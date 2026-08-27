package auth_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/chibuike-kt/hapto-api/internal/auth"
)

func TestHashPassword_VerifyRoundTrip(t *testing.T) {
	hash, err := auth.HashPassword("pepper", "correct horse battery staple")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	ok, err := auth.VerifyPassword("pepper", "correct horse battery staple", hash)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !ok {
		t.Fatal("expected correct password to verify")
	}
}

func TestVerifyPassword_WrongPasswordFails(t *testing.T) {
	hash, err := auth.HashPassword("pepper", "correct horse battery staple")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	ok, err := auth.VerifyPassword("pepper", "wrong password entirely", hash)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if ok {
		t.Fatal("expected wrong password not to verify")
	}
}

func TestVerifyPassword_WrongPepperFails(t *testing.T) {
	hash, err := auth.HashPassword("pepper-a", "correct horse battery staple")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	ok, err := auth.VerifyPassword("pepper-b", "correct horse battery staple", hash)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if ok {
		t.Fatal("expected mismatched pepper not to verify")
	}
}

func TestHashPassword_SaltsDiffer(t *testing.T) {
	h1, err := auth.HashPassword("pepper", "correct horse battery staple")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	h2, err := auth.HashPassword("pepper", "correct horse battery staple")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if h1 == h2 {
		t.Fatal("expected two hashes of the same password to differ by salt")
	}
}

func TestValidatePasswordLength(t *testing.T) {
	tests := []struct {
		name    string
		pw      string
		wantErr bool
	}{
		{"too short", strings.Repeat("a", 11), true},
		{"exactly minimum", strings.Repeat("a", 12), false},
		{"well above minimum", strings.Repeat("a", 40), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := auth.ValidatePasswordLength(tt.pw)
			if tt.wantErr && !errors.Is(err, auth.ErrPasswordTooShort) {
				t.Fatalf("expected ErrPasswordTooShort, got %v", err)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
