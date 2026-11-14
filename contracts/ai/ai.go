// Package ai holds the AI's contracts (PRD-001 §9, AI-49, AI-24; p1-ai-assistant design D5, D10): the read-tool
// registry, one tools/<name>.json per tool in the shape MCP lists a tool, with its version, where it runs, its scope and
// its limits; and the system prompts, one prompts/<agent>@<version>.md per version. They are embedded so that the API
// and the AI service serve and check what the release ships; internal/aitool and internal/assistant read them.
package ai

import "embed"

// Tools are the read tools' definitions, as tools/<name>.json.
//
//go:embed tools/*.json
var Tools embed.FS

// Prompts are the system prompts, as prompts/<agent>@<version>.md: a change is a new version, reviewed with its evals
// (CMP-AI-03).
//
//go:embed prompts/*.md
var Prompts embed.FS
