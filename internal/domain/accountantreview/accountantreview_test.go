package accountantreview

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func month(y int, m time.Month) storage.Period {
	s := time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
	return storage.Period{ID: uuid.New(), StartsOn: s, EndsOn: s.AddDate(0, 1, -1)}
}

// A quarter is covered by a signed-off review of its last month; it is due until 60 days after it ends and overdue
// after; a review still open shows as in review, and a superseded one covers nothing (FM-7, C.2 #61; the owner's
// decisions of 6 Nov 2026).
// req: FM-7
// req: C.2 #61
// req: access/team-modes A quarter overdue for its review
// req: controls/platform-checks A quarter unreviewed after 60 days
// req: controls/platform-checks Reviewed, or not due yet
func TestAQuarterIsCoveredByAReviewOfItsLastMonth(t *testing.T) {
	jun, sep, dec := month(2026, 6), month(2026, 9), month(2026, 12)
	periods := []storage.Period{jun, sep, dec}
	signed := storage.AccountantReview{PeriodID: jun.ID, Status: storage.AccountantReviewSignedOff}
	gone := storage.AccountantReview{PeriodID: sep.ID, Status: storage.AccountantReviewSuperseded}
	open := storage.AccountantReview{PeriodID: sep.ID, Status: storage.AccountantReviewInReview}
	for name, c := range map[string]struct {
		reviews []storage.AccountantReview
		today   time.Time
		want    map[string]string
	}{
		"Q2 signed, Q3 due on 29 Nov": {[]storage.AccountantReview{signed}, time.Date(2026, 11, 29, 9, 0, 0, 0, time.UTC),
			map[string]string{"2026-Q2": Reviewed, "2026-Q3": Due}},
		"Q3 overdue from 30 Nov": {[]storage.AccountantReview{signed, gone}, time.Date(2026, 11, 30, 9, 0, 0, 0, time.UTC),
			map[string]string{"2026-Q2": Reviewed, "2026-Q3": Late}},
		"Q3 in review within its 60 days": {[]storage.AccountantReview{signed, gone, open}, time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC),
			map[string]string{"2026-Q2": Reviewed, "2026-Q3": InReview}},
		"Q3 in review but overdue": {[]storage.AccountantReview{signed, open}, time.Date(2026, 12, 15, 9, 0, 0, 0, time.UTC),
			map[string]string{"2026-Q2": Reviewed, "2026-Q3": Late}},
	} {
		got := Quarters(periods, c.reviews, time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC), c.today)
		if len(got) != len(c.want) {
			t.Errorf("%s: %d quarters", name, len(got))
		}
		for _, q := range got {
			if q.Status != c.want[q.Key()] {
				t.Errorf("%s: %s (%s) is %s, want %s", name, q.Key(), q.Label(), q.Status, c.want[q.Key()])
			}
		}
	}
	if q := Quarters(periods, nil, time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)); q[0].Key() != "2026-Q3" ||
		q[0].Label() != "Jul–Sep 2026" || q[0].Period.ID != sep.ID || !q[0].DueBy.Equal(time.Date(2026, 11, 29, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("the newest quarter: %+v", q[0])
	}
}
