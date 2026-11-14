// Package ai holds the AI's contracts (PRD-001 §9, AI-49; p1-ai-assistant design D5): the read-tool registry, one
// tools/<name>.json per tool in the shape MCP lists a tool, with its version, where it runs, its scope and its limits.
// It is embedded so that the API serves and checks the registry the release ships; internal/aitool reads it.
package ai

import "embed"

// Tools are the read tools' definitions, as tools/<name>.json.
//
//go:embed tools/*.json
var Tools embed.FS
