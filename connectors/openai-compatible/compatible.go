// Package compatible is the OpenAI-compatible connector (PRD-001 AI-71): any endpoint that speaks OpenAI's Chat
// Completions API, from a preset (Moonshot Kimi, xAI Grok, OpenRouter, a LiteLLM gateway, vLLM, Ollama, LM Studio) or
// its own base URL, with its manifest beside it. Its connections are AI connections of their own (p1-ai-providers
// design D2); the adapter is internal/adapters/compatible.
package compatible

import (
	_ "embed"
	"errors"

	adapter "github.com/cognizhi/fyniq/internal/adapters/compatible"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/ports"
)

//go:embed manifest.json
var manifest []byte

// Binding is the OpenAI-compatible connector's manifest and its adapter.
func Binding() (connector.Binding, error) {
	m, err := connector.Parse(manifest)
	if err != nil {
		return connector.Binding{}, err
	}
	return connector.Binding{Manifest: m, Model: func(c connector.Config) (ports.ModelProvider, error) {
		var preset connector.ModelPreset
		for _, p := range m.Model.Presets {
			if p.ID == c.String("preset", "") {
				preset = p
			}
		}
		base := c.String("base_url", preset.BaseURL)
		if base == "" {
			return nil, errors.New("the connection needs its endpoint's base URL")
		}
		key := c.Secrets["api_key"]
		if key == "" && c.String("auth", "api-key") != "none" {
			return nil, errors.New("the connection's API key is not set: set it in Settings › AI providers")
		}
		return adapter.New(base, key, proven(c.Values["proven"]), c.HTTP), nil // the connection's client, through the guard
	}}, nil
}

// proven is what the connection's probe proved, as capabilities, and nothing the adapter cannot carry (AI-71).
func proven(v any) ports.ModelCapabilities {
	var caps ports.ModelCapabilities
	list, _ := v.([]any)
	for _, f := range list {
		switch f {
		case "tools":
			caps.Tools = adapter.Carried.Tools
		case "strict-output":
			caps.StrictOutput = adapter.Carried.StrictOutput
		case "streaming":
			caps.Streaming = adapter.Carried.Streaming
		case "images":
			caps.Images = adapter.Carried.Images
		case "reasoning":
			caps.Reasoning = adapter.Carried.Reasoning
		}
	}
	return caps
}
