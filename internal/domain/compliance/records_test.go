package compliance

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A committed file is kept as its record says, or the daily check says what is wrong in words: missing, kept without a
// locked retention, or kept for less time than its record (CTL-AUD-03, NFR-RET-01).
// req: CTL-AUD-03
// req: NFR-RET-01
// asvs: V14.2.7
func TestWhatBreaksARecordsRetention(t *testing.T) {
	committed := time.Date(2099, 3, 1, 2, 0, 0, 0, time.UTC)
	until := time.Date(2107, 4, 1, 0, 0, 0, 0, time.UTC)
	e := storage.Evidence{ID: uuid.MustParse("01900000-0000-7000-8000-000000000001"), FinalisedAt: &committed, RetainUntil: &until}
	for _, c := range []struct {
		what string
		kept ports.BlobRetention
		err  error
		want string
	}{
		{"kept as its record says", ports.BlobRetention{Locked: true, RetainUntil: until}, nil, ""},
		{"kept longer, on hold", ports.BlobRetention{Locked: true, RetainUntil: until.AddDate(1, 0, 0), Held: true}, nil, ""},
		{"missing", ports.BlobRetention{}, ports.ErrNotFound, "Committed, but missing from the records store"},
		{"unlocked", ports.BlobRetention{RetainUntil: until}, nil, "Kept without a locked retention, so it could be deleted before its time"},
		{"short", ports.BlobRetention{Locked: true, RetainUntil: until.AddDate(-1, 0, 0)}, nil, "Kept until 1 Apr 2106, before its record's 1 Apr 2107"},
	} {
		f, bad := lockFinding(e, c.kept, c.err)
		if c.want == "" {
			if bad {
				t.Errorf("%s: found %+v", c.what, f)
			}
			continue
		}
		if !bad || f.Detail != c.want || f.ID != e.ID.String() || f.Kind != "evidence" || f.At != "2099-03-01T02:00:00Z" {
			t.Errorf("%s: %+v, %v", c.what, f, bad)
		}
	}
	if f, bad := lockFinding(storage.Evidence{ID: e.ID, FinalisedAt: &committed}, ports.BlobRetention{Locked: true}, nil); !bad ||
		f.Detail != "Committed with no retain-until in its record" {
		t.Errorf("a record with no retain-until: %+v", f)
	}
	if _, bad := lockFinding(e, ports.BlobRetention{}, errors.Join(ports.ErrNotFound)); !bad {
		t.Error("a wrapped not-found passed")
	}
}
