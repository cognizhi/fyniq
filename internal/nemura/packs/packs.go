// Package packs embeds Nemura's own statutory packs (PRD-002 NM-PACK-01; owner decision O1, 19 Nov 2026): one directory per pack, one per version inside,
// in the format and under the validation of Fyniq's packs (internal/domain/pack), in Nemura's own place so that standalone needs nothing of Fyniq's.
package packs

import (
	"embed"
	"io/fs"
)

// FS holds every version of Nemura's people pack.
//
//go:embed sg-people
var FS embed.FS

// Files is the people packs, rooted at the pack directories.
func Files() fs.FS { return FS }

// Statutory holds the CPF, SDL and SHG packs that payroll reads (PRD-001 B.3; p1-nemura-payroll D1): copied from Fyniq's so that a standalone Nemura needs nothing of
// Fyniq's, kept apart from the people packs so that the settings page lists only those, and kept equal to Fyniq's by a test (internal/archtest).
//
//go:embed sg-cpf sg-sdl sg-shg
var Statutory embed.FS

// StatutoryFiles is the statutory packs, rooted at the pack directories.
func StatutoryFiles() fs.FS { return Statutory }
