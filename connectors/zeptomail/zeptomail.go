// Package zeptomail is the Zoho ZeptoMail connector (FR-INT-21, FR-INT-25): outbound email through ZeptoMail, with
// its manifest beside it. The adapter is internal/adapters/zeptomail; its webhooks are read by internal/mailhooks.
package zeptomail

import (
	_ "embed"

	adapter "github.com/cognizhi/fyniq/internal/adapters/zeptomail"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/ports"
)

//go:embed manifest.json
var manifest []byte

// Binding is the ZeptoMail connector's manifest and its adapter.
func Binding() (connector.Binding, error) {
	m, err := connector.Parse(manifest)
	if err != nil {
		return connector.Binding{}, err
	}
	return connector.Binding{Manifest: m, OutboundEmail: func(c connector.Config) (ports.Email, error) {
		key, err := c.Secret("send_token")
		if err != nil {
			return nil, err
		}
		return adapter.New(c.String("url", adapter.DefaultURL), key, c.HTTP), nil // the connector's client, through the guard
	}}, nil
}
