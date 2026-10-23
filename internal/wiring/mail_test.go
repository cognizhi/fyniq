package wiring_test

import (
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/wiring"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

// The deployment names its providers in order, each with its key from the environment, the platform's sender and
// the sending domains; a provider without its key, an unknown or repeated one, or no sender stops the service
// starting rather than sending nothing (FR-INT-21, PRD-001 rev 0.102).
// req: FR-INT-21
func TestTheDeploymentNamesItsEmailProvidersInOrder(t *testing.T) {
	c, err := wiring.ReadMail(env(map[string]string{"FYNIQ_EMAIL_PROVIDERS": "zeptomail, resend", "FYNIQ_ZEPTOMAIL_KEY": "z", "FYNIQ_RESEND_KEY": "r",
		"FYNIQ_MAIL_FROM": "Fyniq <no-reply@Mail.Example.com>", "FYNIQ_MAIL_DOMAINS": "mail.example.com, Example.org,example.org"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Providers) != 2 || c.Providers[0].Name() != "zeptomail" || c.Providers[1].Name() != "resend" ||
		strings.Join(c.Domains, ",") != "mail.example.com,example.org" || c.From != "Fyniq <no-reply@Mail.Example.com>" {
		t.Fatalf("%+v", c)
	}
	// An SMTP relay alone, as before, when only it is set; its sender's domain is the sending domain.
	c, err = wiring.ReadMail(env(map[string]string{"FYNIQ_SMTP_ADDR": "smtp.example.com:587", "FYNIQ_MAIL_FROM": "no-reply@Example.com"}))
	if err != nil || len(c.Providers) != 1 || c.Providers[0].Name() != "smtp" || strings.Join(c.Domains, ",") != "example.com" {
		t.Fatalf("%+v, %v", c, err)
	}
	// Nothing set: no provider, so no email is sent.
	if c, err := wiring.ReadMail(env(nil)); err != nil || len(c.Providers) != 0 {
		t.Fatalf("%+v, %v", c, err)
	}
	for name, m := range map[string]map[string]string{
		"a provider without its key": {"FYNIQ_EMAIL_PROVIDERS": "zeptomail", "FYNIQ_MAIL_FROM": "a@example.com"},
		"an unknown provider":        {"FYNIQ_EMAIL_PROVIDERS": "postmark", "FYNIQ_MAIL_FROM": "a@example.com"},
		"a provider twice":           {"FYNIQ_EMAIL_PROVIDERS": "resend,resend", "FYNIQ_RESEND_KEY": "r", "FYNIQ_MAIL_FROM": "a@example.com"},
		"no platform sender":         {"FYNIQ_EMAIL_PROVIDERS": "resend", "FYNIQ_RESEND_KEY": "r"},
		"smtp without its relay":     {"FYNIQ_EMAIL_PROVIDERS": "smtp", "FYNIQ_MAIL_FROM": "a@example.com"},
	} {
		if _, err := wiring.ReadMail(env(m)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
