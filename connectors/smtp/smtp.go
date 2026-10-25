// Package smtp is the SMTP connector (FR-INT-21, FR-INT-25): outbound email through a server the deployment chooses,
// over STARTTLS, with its manifest beside it. The adapter is internal/adapters/smtp.
package smtp

import (
	_ "embed"
	"fmt"

	adapter "github.com/cognizhi/fyniq/internal/adapters/smtp"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/ports"
)

//go:embed manifest.json
var manifest []byte

// Binding is the SMTP connector's manifest and its adapter. Only the local flavour speaks to a server without TLS,
// as it does to Mailpit (DEV-07).
func Binding() (connector.Binding, error) {
	m, err := connector.Parse(manifest)
	if err != nil {
		return connector.Binding{}, err
	}
	return connector.Binding{Manifest: m, OutboundEmail: func(c connector.Config) (ports.Email, error) {
		addr := c.String("address", "")
		if addr == "" {
			return nil, fmt.Errorf("the SMTP server is not set: set its host and port in Settings › Integrations")
		}
		return adapter.New(addr, c.String("domain", c.SenderDomain), connector.LocalFlavour), nil
	}}, nil
}
