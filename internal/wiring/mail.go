package wiring

import (
	"cmp"
	"fmt"
	netmail "net/mail"
	"slices"
	"strings"

	"github.com/cognizhi/fyniq/internal/adapters/resend"
	"github.com/cognizhi/fyniq/internal/adapters/smtp"
	"github.com/cognizhi/fyniq/internal/adapters/zeptomail"
	"github.com/cognizhi/fyniq/internal/ports"
)

// MailConfig is the deployment's outbound email (FR-INT-21; the owner's decision of 23 Oct 2026, PRD-001 rev 0.102):
// the providers in order, the first primary and the next its failover; the platform's sender; and the domains an
// organisation may choose its senders on. Each provider's key is a secret the deployment puts in the environment
// from Secret Manager; it never passes through the API.
type MailConfig struct {
	Providers []ports.Email
	From      string
	Domains   []string
	// The webhooks' secrets, which only the API holds: Resend's endpoint signing secret and ZeptoMail's Agent
	// authentication key. Empty: that provider's webhooks have no receiver.
	ResendWebhookSecret, ZeptoMailWebhookKey string
}

// ReadMail reads the mail configuration from env:
//   - FYNIQ_EMAIL_PROVIDERS: the providers in order, from smtp, zeptomail and resend; smtp alone when it is unset
//     and FYNIQ_SMTP_ADDR is set; none when neither is.
//   - smtp: FYNIQ_SMTP_ADDR, and FYNIQ_MAIL_DOMAIN for the Message-ID (the sender's domain when unset); STARTTLS only.
//   - zeptomail: FYNIQ_ZEPTOMAIL_KEY, and FYNIQ_ZEPTOMAIL_URL for an account outside the default data centre.
//   - resend: FYNIQ_RESEND_KEY, and FYNIQ_RESEND_URL.
//   - FYNIQ_MAIL_FROM: the platform's sender, needed with any provider.
//   - FYNIQ_MAIL_DOMAINS: the sending domains, comma-separated; the platform sender's domain when unset.
//   - FYNIQ_RESEND_WEBHOOK_SECRET and FYNIQ_ZEPTOMAIL_WEBHOOK_KEY: the webhooks' secrets, on the API.
func ReadMail(env func(string) string) (MailConfig, error) {
	c := MailConfig{From: env("FYNIQ_MAIL_FROM"), ResendWebhookSecret: env("FYNIQ_RESEND_WEBHOOK_SECRET"),
		ZeptoMailWebhookKey: env("FYNIQ_ZEPTOMAIL_WEBHOOK_KEY")}
	names := env("FYNIQ_EMAIL_PROVIDERS")
	if names == "" && env("FYNIQ_SMTP_ADDR") != "" {
		names = "smtp"
	}
	if names == "" {
		return c, nil
	}
	from, err := netmail.ParseAddress(c.From)
	if err != nil {
		return MailConfig{}, fmt.Errorf("FYNIQ_MAIL_FROM %q is not an address: email needs the platform's sender", c.From)
	}
	fromDomain := strings.ToLower(from.Address[strings.LastIndex(from.Address, "@")+1:])
	var seen []string
	for _, name := range strings.Split(names, ",") {
		name = strings.TrimSpace(name)
		if slices.Contains(seen, name) {
			return MailConfig{}, fmt.Errorf("FYNIQ_EMAIL_PROVIDERS names %s twice", name)
		}
		seen = append(seen, name)
		switch name {
		case "smtp":
			if env("FYNIQ_SMTP_ADDR") == "" {
				return MailConfig{}, fmt.Errorf("FYNIQ_EMAIL_PROVIDERS names smtp, but FYNIQ_SMTP_ADDR is not set")
			}
			c.Providers = append(c.Providers, smtp.New(env("FYNIQ_SMTP_ADDR"), cmp.Or(env("FYNIQ_MAIL_DOMAIN"), fromDomain), false))
		case "zeptomail":
			if env("FYNIQ_ZEPTOMAIL_KEY") == "" {
				return MailConfig{}, fmt.Errorf("FYNIQ_EMAIL_PROVIDERS names zeptomail, but FYNIQ_ZEPTOMAIL_KEY is not set")
			}
			c.Providers = append(c.Providers, zeptomail.New(cmp.Or(env("FYNIQ_ZEPTOMAIL_URL"), zeptomail.DefaultURL), env("FYNIQ_ZEPTOMAIL_KEY")))
		case "resend":
			if env("FYNIQ_RESEND_KEY") == "" {
				return MailConfig{}, fmt.Errorf("FYNIQ_EMAIL_PROVIDERS names resend, but FYNIQ_RESEND_KEY is not set")
			}
			c.Providers = append(c.Providers, resend.New(cmp.Or(env("FYNIQ_RESEND_URL"), resend.DefaultURL), env("FYNIQ_RESEND_KEY")))
		default:
			return MailConfig{}, fmt.Errorf("FYNIQ_EMAIL_PROVIDERS names %q: the providers are smtp, zeptomail and resend", name)
		}
	}
	c.Domains = []string{fromDomain}
	if v := env("FYNIQ_MAIL_DOMAINS"); v != "" {
		c.Domains = nil
		for _, d := range strings.Split(v, ",") {
			if d = strings.ToLower(strings.TrimSpace(d)); d != "" && !slices.Contains(c.Domains, d) {
				c.Domains = append(c.Domains, d)
			}
		}
	}
	return c, nil
}
