// Package slack is the Slack connector (PRD-001 FR-NTF-04, FR-INT-25): messages posted to the channels an organisation's routing rules name, with its manifest beside it.
// The adapter is internal/adapters/slack; the routing is internal/chatroute.
package slack

import (
	_ "embed"

	adapter "github.com/cognizhi/fyniq/internal/adapters/slack"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/ports"
)

//go:embed manifest.json
var manifest []byte

// Binding is the Slack connector's manifest and its adapter.
func Binding() (connector.Binding, error) {
	m, err := connector.Parse(manifest)
	if err != nil {
		return connector.Binding{}, err
	}
	return connector.Binding{Manifest: m,
		Messaging: func(c connector.Config) (ports.Messaging, error) {
			token, err := c.Secret("bot_token")
			if err != nil {
				return nil, err
			}
			return &adapter.Poster{Base: c.String("base", adapter.DefaultBase), Token: token, HTTP: c.HTTP}, nil // the connector's client, through the guard
		}}, nil
}
