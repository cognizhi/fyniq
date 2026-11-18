// Package agents holds the scheduled agents' skill files (PRD-001 §9.12, AI-61; p1-scheduled-agents design D1): one
// directory per agent with its SKILL.md, embedded so that a run follows the skill the release ships. internal/agents
// loads, checks and pins them.
package agents

import "embed"

// Skills are the agents' skill files, as agents/<name>/SKILL.md.
//
//go:embed */SKILL.md
var Skills embed.FS
