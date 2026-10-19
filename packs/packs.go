// Package packs embeds the statutory parameter packs (PRD-001 §10.6, ENG-30): one directory per
// version, packs/<pack>/<year>.<n>/, holding pack.toml (the metadata and the sources), the data tables
// as CSV and pinned/, the hand-pinned statutory tests. internal/domain/pack reads and validates them;
// the migrator copies them into the global read-only pack tables (fyniq_migrator only, §11.4).
package packs

import "embed"

// FS holds every pack version; a pack is a top-level directory.
//
//go:embed sg-gst
var FS embed.FS
