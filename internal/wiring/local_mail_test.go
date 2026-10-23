//go:build local

package wiring_test

import (
	"testing"

	"github.com/cognizhi/fyniq/internal/wiring"
)

// Local mode substitutes Mailpit for every email provider (ADR-T4, PRD-001 §8.24): it catches what is sent over SMTP,
// and stands in for receiving through its webhook and API, under a local-only secret.
// req: communication/email-and-notifications Launch adapters and local substitutes
func TestLocalModeSubstitutesMailpitForTheEmailProviders(t *testing.T) {
	c := wiring.LocalMail(env(nil))
	if len(c.Providers) != 1 || c.Providers[0].Name() != "smtp" || c.Inbound == nil || c.Inbound.Name() != "mailpit" ||
		c.MailpitWebhook != "fyniq:local-only" || c.CaptureDomain != "in.fyniq.local" {
		t.Fatalf("%+v", c)
	}
}
