// Package wise is the Wise connector (FR-BNK-18, FR-INT-25; p1-bank-import-reconcile task 8.3, design D11): a business
// profile's balances and statements, read-only, with its manifest beside it. The adapter is internal/adapters/wise; its
// webhooks reach internal/bankfeed, which only queues an early sync.
package wise

import (
	_ "embed"
	"errors"
	"net/http"
	"time"

	adapter "github.com/cognizhi/fyniq/internal/adapters/wise"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/ports"
)

//go:embed manifest.json
var manifest []byte

// Binding is the Wise connector's manifest and its adapter.
func Binding() (connector.Binding, error) {
	m, err := connector.Parse(manifest)
	if err != nil {
		return connector.Binding{}, err
	}
	return connector.Binding{Manifest: m,
		BankData: func(c connector.Config) (ports.BankData, error) {
			token, err := c.Secret("api_token")
			if err != nil {
				return nil, err
			}
			base := adapter.ProductionBase
			if c.String("environment", "production") == "sandbox" {
				base = adapter.SandboxBase
			}
			days := 90
			if v, ok := c.Values["history_days"].(float64); ok && v >= 1 && v <= 469 {
				days = int(v)
			}
			if c.Clock == nil {
				return nil, errors.New("wise: the connector runs without a clock")
			}
			return &adapter.Client{Base: base, Token: token, Profile: c.String("profile_id", ""), History: time.Duration(days) * 24 * time.Hour,
				HTTP: c.HTTP, Now: c.Clock.Now}, nil
		},
		VerifyWebhook: func(c connector.Config, h http.Header, body []byte, _ time.Time) error {
			return adapter.Verify(c.String("environment", "production") == "sandbox", h, body)
		}}, nil
}
