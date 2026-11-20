// Package hitpay is the HitPay connector (FR-PSP-01, FR-INT-25; p1-payment-providers design D1 to D3): a HitPay business's
// charges, fees and payouts as the statement of its money accounts, and its objects and signed webhooks for the
// payment-provider capability, read-only, with its manifest beside it. The adapter is internal/adapters/hitpay; its
// webhooks reach internal/pspevents.
package hitpay

import (
	_ "embed"
	"errors"
	"net/http"
	"time"

	adapter "github.com/cognizhi/fyniq/internal/adapters/hitpay"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/ports"
)

//go:embed manifest.json
var manifest []byte

// Binding is the HitPay connector's manifest and its adapters.
func Binding() (connector.Binding, error) {
	m, err := connector.Parse(manifest)
	if err != nil {
		return connector.Binding{}, err
	}
	client := func(c connector.Config) (*adapter.Client, error) {
		key, err := c.Secret("api_key")
		if err != nil {
			return nil, err
		}
		base := adapter.ProductionBase
		if c.String("environment", "production") == "sandbox" {
			base = adapter.SandboxBase
		}
		days := 90
		if v, ok := c.Values["history_days"].(float64); ok && v >= 1 && v <= 730 {
			days = int(v)
		}
		if c.Clock == nil {
			return nil, errors.New("hitpay: the connector runs without a clock")
		}
		return &adapter.Client{Base: base, Key: key, HTTP: c.HTTP, Now: c.Clock.Now, History: time.Duration(days) * 24 * time.Hour}, nil
	}
	return connector.Binding{Manifest: m,
		BankData:        func(c connector.Config) (ports.BankData, error) { return client(c) },
		PaymentProvider: func(c connector.Config) (ports.PaymentProvider, error) { return client(c) },
		VerifyWebhook: func(c connector.Config, h http.Header, body []byte, _ time.Time) error {
			salt, err := c.Secret("webhook_salt")
			if err != nil {
				return err
			}
			return adapter.Verify(salt, h, body)
		},
		PaymentLinks: func(c connector.Config) (ports.PaymentLinks, error) {
			key, err := c.Secret("api_key")
			if err != nil {
				return nil, err
			}
			base := adapter.ProductionBase
			if c.String("environment", "production") == "sandbox" {
				base = adapter.SandboxBase
			}
			return &adapter.Linker{Base: base, Key: key, HTTP: c.HTTP}, nil
		},
		ParseEvent: adapter.ParseEvent}, nil
}
