// Package gemini is the Gemini on Agent Platform connector (PRD-001 AI-71): Google's Gemini models, called with the
// service's own account, with its manifest beside it. Its connections are AI connections of their own (p1-ai-providers
// design D2); the adapter is internal/adapters/gemini.
package gemini

import (
	_ "embed"
	"errors"

	adapter "github.com/cognizhi/fyniq/internal/adapters/gemini"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/ports"
)

//go:embed manifest.json
var manifest []byte

// Binding is the Gemini connector's manifest and its adapter.
func Binding() (connector.Binding, error) {
	m, err := connector.Parse(manifest)
	if err != nil {
		return connector.Binding{}, err
	}
	return connector.Binding{Manifest: m, Model: func(c connector.Config) (ports.ModelProvider, error) {
		project := c.String("project", "")
		if project == "" {
			return nil, errors.New("the connection needs the Google Cloud project Gemini is enabled in")
		}
		if c.Tokens == nil {
			return nil, errors.New("the connection needs the service's own account")
		}
		return adapter.New("", project, c.String("location", "asia-southeast1"), c.Tokens, c.HTTP), nil
	}}, nil
}
