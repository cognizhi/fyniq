// Package kb embeds the platform knowledge base's compiled pages (PRD-001 §9.11, AI-50, AI-56): kb/pages, the generated
// index and the schema, as the release ships them. The migrator loads them into the global knowledge-base tables
// (cmd/migrate); internal/domain/knowledge reads and lints them.
package kb

import "embed"

// FS is the knowledge base's directory: pages/, index.md and schema.md.
//
//go:embed pages index.md schema.md
var FS embed.FS
