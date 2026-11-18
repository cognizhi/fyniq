package backup

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

type fakeDB struct {
	uris []string
	fail string
}

func (f *fakeDB) Export(_ context.Context, uri string) (string, error) {
	if f.fail != "" && strings.Contains(uri, f.fail) {
		return "", errors.New("the instance refused")
	}
	f.uris = append(f.uris, uri)
	return "exported to " + uri, nil
}

// The export is named by its Singapore start time, so a retry never replaces one; a month's first week also writes the month's copy, kept a year (DR-02,
// DR-04, NFR-DR-01).
// req: DR-02
// req: DR-04
// req: NFR-DR-01
func TestTheWeeklyExportAndTheMonthsCopy(t *testing.T) {
	b := Buckets{Weekly: "fyniq-production-db-weekly", Monthly: "fyniq-production-db-monthly"}
	// Saturday 19:00 UTC is Sunday 03:00 in Singapore: the export's slot.
	first := time.Date(2099, 2, 6, 19, 0, 0, 0, time.UTC) // Sunday 7 Feb 2099 in Singapore
	if got := Names(b, first); !slices.Equal(got, []string{"gs://fyniq-production-db-weekly/weekly/fyniq-2099-02-07-030000.sql.gz",
		"gs://fyniq-production-db-monthly/monthly/fyniq-2099-02-07-030000.sql.gz"}) {
		t.Errorf("the month's first week: %v", got)
	}
	if got := Names(b, first.AddDate(0, 0, 7)); len(got) != 1 || got[0] != "gs://fyniq-production-db-weekly/weekly/fyniq-2099-02-14-030000.sql.gz" {
		t.Errorf("a later week: %v", got)
	}
	db := &fakeDB{}
	done, err := Export(context.Background(), db, b, first)
	if err != nil || len(done) != 2 || len(db.uris) != 2 {
		t.Fatalf("the export: %v %v", done, err)
	}
	if retry := Names(b, first.Add(time.Minute)); slices.Contains(Names(b, first), retry[0]) {
		t.Errorf("a retry would replace the export: %v", retry)
	}
	if _, err := Export(context.Background(), &fakeDB{fail: "weekly"}, b, first); err == nil || !strings.Contains(err.Error(), "weekly/fyniq-2099-02-07-030000") {
		t.Errorf("a refused export: %v", err)
	}
	if _, err := Export(context.Background(), db, Buckets{}, first); err == nil {
		t.Error("an export with no buckets passed")
	}
}
