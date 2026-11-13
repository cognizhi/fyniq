// Package anthropic is the Anthropic connector (PRD-001 AI-71): Claude through the Anthropic Messages API with an API
// key, or through Claude on Google Cloud's Agent Platform with the service's own account, with its manifest beside it.
// Its connections are AI connections of their own (p1-ai-providers design D2); the adapter is
// internal/adapters/anthropic.
package anthropic

import (
	_ "embed"
	"errors"

	adapter "github.com/cognizhi/fyniq/internal/adapters/anthropic"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/ports"
)

//go:embed manifest.json
var manifest []byte

// Binding is the Anthropic connector's manifest and its adapter.
func Binding() (connector.Binding, error) {
	m, err := connector.Parse(manifest)
	if err != nil {
		return connector.Binding{}, err
	}
	return connector.Binding{Manifest: m, Model: func(c connector.Config) (ports.ModelProvider, error) {
		if c.String("endpoint", "agent-platform") == "agent-platform" {
			project := c.String("project", "")
			if project == "" {
				return nil, errors.New("the Agent Platform endpoint needs the Google Cloud project Claude is enabled in")
			}
			if c.Tokens == nil {
				return nil, errors.New("the Agent Platform endpoint needs the service's own account")
			}
			return adapter.NewAgentPlatform("", project, c.String("location", "asia-southeast1"), c.Tokens, c.HTTP), nil
		}
		key, err := c.Secret("api_key")
		if err != nil {
			return nil, errors.New("the connection's API key is not set: set it in Settings › AI providers")
		}
		return adapter.New(c.String("base_url", adapter.DefaultBase), key, c.HTTP), nil // the connection's client, through the guard
	}}, nil
}
