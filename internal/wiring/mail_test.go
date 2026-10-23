package wiring_test

import (
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/outbox"
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

// The records each sending domain's providers asked for come as JSON, only for sending domains, each a TXT or CNAME
// with a name and a value; the domains and records are read without any provider, for the nightly check.
// req: FR-INT-21
func TestTheSendingDomainsRecordsAreConfiguration(t *testing.T) {
	c, err := wiring.ReadMail(env(map[string]string{"FYNIQ_MAIL_DOMAINS": "mail.example.com",
		"FYNIQ_MAIL_DNS": `{"mail.example.com":[{"type":"TXT","name":"k._domainkey.mail.example.com","value":"v=DKIM1"},` +
			`{"type":"CNAME","name":"bounce.mail.example.com","value":"cluster.zeptomail.com"}]}`}))
	if err != nil || len(c.Providers) != 0 || len(c.DNS["mail.example.com"]) != 2 || c.DNS["mail.example.com"][1].Type != "CNAME" {
		t.Fatalf("%+v, %v", c, err)
	}
	for name, v := range map[string]string{
		"not JSON":               `{`,
		"another domain":         `{"other.example.com":[{"type":"TXT","name":"a","value":"b"}]}`,
		"an MX record":           `{"mail.example.com":[{"type":"MX","name":"a","value":"b"}]}`,
		"a record with no value": `{"mail.example.com":[{"type":"TXT","name":"a"}]}`,
	} {
		if _, err := wiring.ReadMail(env(map[string]string{"FYNIQ_MAIL_DOMAINS": "mail.example.com", "FYNIQ_MAIL_DNS": v})); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// Every platform schedule has its job, so the tick never starts one the worker cannot run.
// req: FR-INT-21
func TestEveryPlatformScheduleHasItsJob(t *testing.T) {
	jobs := (&wiring.Kit{}).PlatformJobs(nil)
	for _, s := range outbox.PlatformSchedules {
		if _, ok := jobs[s.Job]; !ok {
			t.Errorf("%s has no job", s.Job)
		}
	}
}
