// Package nemuradb holds Nemura's database migrations (PRD-002 ADR-N1, NM-ARC-02), embedded so the nemura image carries
// them. Nemura has its own PostgreSQL database and its own stream, numbered from 000001 and independent of Fyniq's
// (db/migrations): no migration here touches Fyniq's database, and none there touches Nemura's.
package nemuradb

import "embed"

// Migrations are the goose SQL migrations, in order. A merged migration is never edited; a change is a new forward
// migration (PRD-001 §12.10).
//
//go:embed migrations/*.sql
var Migrations embed.FS
