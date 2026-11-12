// Package resend is the Resend connector (FR-INT-21, FR-INT-22, FR-INT-25): outbound email through Resend, and the
// mail Resend receives for the email-in addresses, with its manifest beside it. The adapters are
// internal/adapters/resend; its webhooks are read by internal/mailhooks.
package resend

import (
	_ "embed"

	adapter "github.com/cognizhi/fyniq/internal/adapters/resend"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/ports"
)

//go:embed manifest.json
var manifest []byte

// Binding is the Resend connector's manifest and its adapters.
func Binding() (connector.Binding, error) {
	m, err := connector.Parse(manifest)
	if err != nil {
		return connector.Binding{}, err
	}
	return connector.Binding{Manifest: m,
		OutboundEmail: func(c connector.Config) (ports.Email, error) {
			key, err := c.Secret("api_key")
			if err != nil {
				return nil, err
			}
			return adapter.New(c.String("url", adapter.DefaultURL), key, c.HTTP), nil // the connector's client, through the guard
		},
		InboundEmail: func(c connector.Config) (ports.InboundMail, error) {
			key, err := c.Secret("api_key")
			if err != nil {
				return nil, err
			}
			// Attachments too go only to the manifest's destinations. [verify the host of Resend's download_url and add it
			// to the manifest if it is not api.resend.com]
			return adapter.NewReceiving(c.String("api_base", adapter.DefaultBase), key, c.HTTP, c.HTTP), nil
		}}, nil
}
