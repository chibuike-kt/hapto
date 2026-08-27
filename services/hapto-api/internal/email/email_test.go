package email

import (
	"strings"
	"testing"
)

func TestPasswordResetEmail_ContainsLink(t *testing.T) {
	link := "https://app.example.test/reset-password?token=abc123"
	subject, html := passwordResetEmail(link)

	if subject == "" {
		t.Fatal("expected a non-empty subject")
	}
	if !strings.Contains(html, link) {
		t.Fatalf("expected email body to contain the reset link %q, got %q", link, html)
	}
}
