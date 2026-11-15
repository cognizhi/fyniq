// Package ai holds the AI's contracts (PRD-001 §9, AI-49, AI-24; p1-ai-assistant design D5, D10): the read-tool
// registry, one tools/<name>.json per tool in the shape MCP lists a tool, with its version, where it runs, its scope and
// its limits; the system prompts, one prompts/<agent>@<version>.md per version; and the proposal catalogue, one
// tasks/<id>.json per task type (§9.6; p1-ai-proposals-core design D1). They are embedded so that the API, the AI service
// and the executor serve and check what the release ships; internal/aitool, internal/assistant and internal/proposal
// read them.
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

// Tasks are the proposal catalogue's task types, as tasks/<id>.json: each a change the AI may propose and a person must
// approve. A change is a new version, reviewed with its evals (CMP-AI-03).
//
//go:embed tasks
var Tasks embed.FS
