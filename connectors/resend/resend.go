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
			s := adapter.New(c.String("url", adapter.DefaultURL), key)
			if c.HTTP != nil {
				s.HTTP = c.HTTP
			}
			return s, nil
		},
		InboundEmail: func(c connector.Config) (ports.InboundMail, error) {
			key, err := c.Secret("api_key")
			if err != nil {
				return nil, err
			}
			r := adapter.NewReceiving(c.String("api_base", adapter.DefaultBase), key)
			if c.HTTP != nil {
				r.HTTP = c.HTTP
			}
			return r, nil
		}}, nil
}
