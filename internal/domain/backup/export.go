// Package backup is the weekly export of the database (DR-02, DR-04, NFR-DR-01; p1-operations-dr design D1): each
// Sunday at 03:00 Singapore time, the whole database as SQL into a retention-locked bucket in the audit project, kept 35
// days, so the books survive the loss of the production project with at most a week lost; and in each month's first
// week a second copy into a bucket kept a year, DR-02's monthly backups. Restoring one is the quarterly drill's
// (docs/runbooks/restore-drill.md); the shredding register is re-applied on restore (§11.8).
package backup

import (
	"context"
	"fmt"
	"time"

	"github.com/cognizhi/fyniq/internal/ports"
)

// Job is the platform job of the export.
const Job = "db.export"

// Buckets are where the exports go: the weekly bucket (35 days) and the monthly one (a year).
type Buckets struct {
	Weekly  string `json:"weekly"`
	Monthly string `json:"monthly"`
}

// singapore is the time zone the export's date is named in.
var singapore = time.FixedZone("SGT", 8*3600)

// Names are the objects an export at now writes: the week's, and the month's in the month's first seven days. Each is
// named by the Singapore time it started, so a retry writes a new object: a locked bucket refuses to replace one.
func Names(b Buckets, now time.Time) []string {
	at := now.In(singapore)
	stamp := at.Format("2006-01-02-150405")
	out := []string{fmt.Sprintf("gs://%s/weekly/fyniq-%s.sql.gz", b.Weekly, stamp)}
	if at.Day() <= 7 {
		out = append(out, fmt.Sprintf("gs://%s/monthly/fyniq-%s.sql.gz", b.Monthly, stamp))
	}
	return out
}

// Export writes the week's export, and the month's when it is due, and says what it did.
func Export(ctx context.Context, db ports.DatabaseBackup, b Buckets, now time.Time) ([]string, error) {
	if b.Weekly == "" || b.Monthly == "" {
		return nil, fmt.Errorf("the export's buckets are not configured (FYNIQ_GCP exports)")
	}
	var done []string
	for _, uri := range Names(b, now) {
		what, err := db.Export(ctx, uri)
		if err != nil {
			return done, fmt.Errorf("the export to %s: %w", uri, err)
		}
		done = append(done, what)
	}
	return done, nil
}
