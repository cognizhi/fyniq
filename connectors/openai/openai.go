// Package openai is the OpenAI connector (PRD-001 AI-71): OpenAI's models through the Responses API with a project
// API key, with its manifest beside it. Its connections are AI connections of their own (p1-ai-providers design D2);
// the adapter is internal/adapters/openai.
package openai

import (
	_ "embed"
	"errors"

	adapter "github.com/cognizhi/fyniq/internal/adapters/openai"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/ports"
)

//go:embed manifest.json
var manifest []byte

// Binding is the OpenAI connector's manifest and its adapter.
func Binding() (connector.Binding, error) {
	m, err := connector.Parse(manifest)
	if err != nil {
		return connector.Binding{}, err
	}
	return connector.Binding{Manifest: m, Model: func(c connector.Config) (ports.ModelProvider, error) {
		key, err := c.Secret("api_key")
		if err != nil {
			return nil, errors.New("the connection's API key is not set: set it in Settings › AI providers")
		}
		return adapter.New(c.String("base_url", adapter.DefaultBase), key, c.HTTP), nil // the connection's client, through the guard
	}}, nil
}
