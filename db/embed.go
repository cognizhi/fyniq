// Package db holds the database migrations (PRD-001 §11.3, §12.4), embedded so the migrate binary
// carries them. The rules, roles, grants and error codes are in README.md.
package db

import "embed"

// Migrations are the goose SQL migrations, in order. A merged migration is never edited; a change is
// a new forward migration (§12.10).
//
//go:embed migrations/*.sql
var Migrations embed.FS
