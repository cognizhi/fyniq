// Package packs embeds Nemura's own statutory packs (PRD-002 NM-PACK-01; owner decision O1, 19 Nov 2026): one directory per pack, one per version inside,
// in the format and under the validation of Fyniq's packs (internal/domain/pack), in Nemura's own place so that standalone needs nothing of Fyniq's.
package packs

import (
	"embed"
	"io/fs"
)

// FS holds every Nemura pack version.
//
//go:embed sg-people
var FS embed.FS

// Files is the packs, rooted at the pack directories.
func Files() fs.FS { return FS }
