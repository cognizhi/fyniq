package connector_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/ports"
)

type nopEmail struct{}

func (nopEmail) Name() string { return "nop" }
func (nopEmail) Send(context.Context, ports.OutboundEmail) (ports.EmailReceipt, error) {
	return ports.EmailReceipt{}, nil
}
func (nopEmail) Status(context.Context, ports.OutboundEmail, time.Time) (ports.EmailReceipt, error) {
	return ports.EmailReceipt{}, nil
}

func binding(t *testing.T, id string, email bool) connector.Binding {
	t.Helper()
	m, err := connector.Parse(edit(t, func(m map[string]any) { m["id"] = id }))
	if err != nil {
		t.Fatal(err)
	}
	b := connector.Binding{Manifest: m}
	if email {
		b.OutboundEmail = func(connector.Config) (ports.Email, error) { return nopEmail{}, nil }
	}
	return b
}

// The catalogue holds each connector once, and each binding builds exactly the capabilities its manifest declares.
// req: FR-INT-25
func TestTheCatalogueChecksItsBindings(t *testing.T) {
	c, err := connector.NewCatalogue(binding(t, "beta", true), binding(t, "alpha", true))
	if err != nil {
		t.Fatal(err)
	}
	all := c.All()
	if len(all) != 2 || all[0].Manifest.ID != "alpha" || all[1].Manifest.ID != "beta" {
		t.Fatalf("order: %v", all)
	}
	if got := c.Implementing(connector.OutboundEmail); len(got) != 2 {
		t.Fatalf("implementing outbound email: %d", len(got))
	}
	if got := c.Implementing(connector.BankData); len(got) != 0 {
		t.Fatalf("implementing bank data: %d", len(got))
	}
	if _, ok := c.Get("alpha"); !ok {
		t.Fatal("alpha is not found")
	}
	for name, bs := range map[string][]connector.Binding{
		"twice":           {binding(t, "alpha", true), binding(t, "alpha", true)},
		"no builder":      {binding(t, "alpha", false)},
		"unparsed":        {{Manifest: connector.Manifest{ID: "raw"}}},
		"builds too much": {withInbound(binding(t, "alpha", true))},
	} {
		if _, err := connector.NewCatalogue(bs...); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func withInbound(b connector.Binding) connector.Binding {
	b.InboundEmail = func(connector.Config) (ports.InboundMail, error) { return nil, nil }
	return b
}

// A missing secret says which to set, and a configuration value falls back to its default only when unset.
// req: FR-INT-20
func TestConfigReadsValuesAndSecrets(t *testing.T) {
	c := connector.Config{Values: map[string]any{"url": "https://a.example", "empty": ""}, Secrets: map[string]string{"key": "k"}}
	if c.String("url", "d") != "https://a.example" || c.String("empty", "d") != "d" || c.String("none", "d") != "d" {
		t.Fatal("values")
	}
	if v, err := c.Secret("key"); err != nil || v != "k" {
		t.Fatalf("%q %v", v, err)
	}
	if _, err := c.Secret("token"); err == nil || !strings.Contains(err.Error(), "token") {
		t.Fatalf("missing: %v", err)
	}
}
