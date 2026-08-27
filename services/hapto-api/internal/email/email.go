// Package email wraps Resend for the one transactional email hapto sends:
// a link, for password reset.
package email

import (
	"context"
	"fmt"

	"github.com/resend/resend-go/v2"
)

type Client struct {
	resend *resend.Client
	from   string
}

func NewClient(apiKey, from string) *Client {
	return &Client{resend: resend.NewClient(apiKey), from: from}
}

// SendPasswordReset emails a single-use password reset link.
func (c *Client) SendPasswordReset(ctx context.Context, to, resetLink string) error {
	subject, html := passwordResetEmail(resetLink)

	_, err := c.resend.Emails.SendWithContext(ctx, &resend.SendEmailRequest{
		From:    c.from,
		To:      []string{to},
		Subject: subject,
		Html:    html,
	})
	return err
}

func passwordResetEmail(link string) (subject, html string) {
	subject = "Reset your hapto password"
	html = fmt.Sprintf(
		`<p>We received a request to reset your hapto password.</p>`+
			`<p><a href="%s">Reset your password</a></p>`+
			`<p>This link expires in 30 minutes. If you didn't request this, you can ignore this email.</p>`,
		link,
	)
	return subject, html
}
