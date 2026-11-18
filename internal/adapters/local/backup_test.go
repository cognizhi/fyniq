//go:build local

package local

import (
	"context"
	"strings"
	"testing"
)

// Local mode's export refuses a URI that is not gs://<bucket>/<name> or climbs out of its folder, and says so when the
// machine has no pg_dump rather than pretending (DR-02).
// req: DR-02
func TestLocalExportNamesAndItsAbsence(t *testing.T) {
	p := PGDump{URL: "postgres://nobody@127.0.0.1:1/none", Dir: t.TempDir()}
	for _, bad := range []string{"weekly/x.sql.gz", "gs://weekly", "gs://weekly/../x.sql.gz"} {
		if _, err := p.Export(context.Background(), bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	t.Setenv("PATH", t.TempDir()) // no pg_dump
	what, err := p.Export(context.Background(), "gs://weekly/weekly/fyniq-2099-02-07.sql.gz")
	if err != nil || !strings.HasPrefix(what, "skipped") {
		t.Errorf("without pg_dump: %q %v", what, err)
	}
}
