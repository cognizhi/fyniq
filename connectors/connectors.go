// Package connectors is this release's connector catalogue (PRD-001 FR-INT-25): every first-party connector, each in
// its own directory with its manifest.json (contracts/connectors/manifest.schema.json) and the binding that builds
// its adapters. Adding a provider adds a directory here and a line below, never a change to the core (FR-INT-20).
package connectors

import (
	"github.com/cognizhi/fyniq/connectors/resend"
	"github.com/cognizhi/fyniq/connectors/smtp"
	"github.com/cognizhi/fyniq/connectors/wise"
	"github.com/cognizhi/fyniq/connectors/zeptomail"
	"github.com/cognizhi/fyniq/internal/connector"
)

// bindings are the connectors compiled in.
var bindings = []func() (connector.Binding, error){smtp.Binding, zeptomail.Binding, resend.Binding, wise.Binding}

// Catalogue is the release's catalogue; an error means a manifest or binding is malformed, and the service must not
// start.
func Catalogue() (*connector.Catalogue, error) {
	bs := make([]connector.Binding, 0, len(bindings))
	for _, b := range bindings {
		x, err := b()
		if err != nil {
			return nil, err
		}
		bs = append(bs, x)
	}
	return connector.NewCatalogue(bs...)
}
