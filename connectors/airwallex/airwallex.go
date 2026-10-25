// Package airwallex is the Airwallex connector (FR-BNK-18, FR-INT-25; p1-bank-import-reconcile task 8.4, design D11): a
// wallet's cash balances and their history, read-only with a scoped key, with its manifest beside it. The adapter is
// internal/adapters/airwallex; its webhooks reach internal/bankfeed, which only queues an early sync.
package airwallex

import (
	_ "embed"
	"errors"
	"net/http"
	"time"

	adapter "github.com/cognizhi/fyniq/internal/adapters/airwallex"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/ports"
)

//go:embed manifest.json
var manifest []byte

// Binding is the Airwallex connector's manifest and its adapter.
func Binding() (connector.Binding, error) {
	m, err := connector.Parse(manifest)
	if err != nil {
		return connector.Binding{}, err
	}
	return connector.Binding{Manifest: m,
		BankData: func(c connector.Config) (ports.BankData, error) {
			key, err := c.Secret("api_key")
			if err != nil {
				return nil, err
			}
			if c.Clock == nil {
				return nil, errors.New("airwallex: the connector runs without a clock")
			}
			base := adapter.ProductionBase
			if c.String("environment", "production") == "sandbox" {
				base = adapter.SandboxBase
			}
			days := 90
			if v, ok := c.Values["history_days"].(float64); ok && v >= 1 && v <= 365 {
				days = int(v)
			}
			return &adapter.Client{Base: base, ClientID: c.String("client_id", ""), APIKey: key, History: time.Duration(days) * 24 * time.Hour,
				HTTP: c.HTTP, Now: c.Clock.Now}, nil
		},
		VerifyWebhook: func(c connector.Config, h http.Header, body []byte, now time.Time) error {
			secret, err := c.Secret("webhook_secret")
			if err != nil {
				return err
			}
			return adapter.Verify(secret, h, body, now)
		}}, nil
}
