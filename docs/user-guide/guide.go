// Package userguide embeds the user guide's Markdown (PRD-001 §7.9, FR-UX-32): the raw sources the platform knowledge
// base is compiled from and that kb.search falls back to (AI-51). The web app builds the same files in; the migrator
// loads these into the global knowledge-base tables (cmd/migrate).
package userguide

import "embed"

// FS is the guide's directory: its pages, and Nemura's guide in nemura/.
//
//go:embed *.md nemura/*.md
var FS embed.FS
