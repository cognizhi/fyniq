// Package accountantreview is the accountant's review (PRD-001 FM-7; rev 0.176, the owner's decisions of 6 Nov 2026):
// what a review covers and when a quarter needs one. A review covers one closed month, the close it reviews with that
// close's snapshot (AUD-04), normally the quarter's last month, whose snapshot is the trial balance at the quarter end.
// In Founder mode a calendar quarter is overdue 60 days after it ends while no signed-off review covers its last month
// (C.2 #61). The database keeps the review's own rules (FY264 to FY266).
package accountantreview

import (
	"fmt"
	"time"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable codes of the accountant's review (ENG-23).
const (
	CodeNotClosed   = "REVIEW-01" // the month is not closed
	CodeReviewed    = "REVIEW-02" // its close has a review already
	CodeNotInReview = "REVIEW-03" // the review is signed off or superseded
	CodeAnswered    = "REVIEW-04" // the query is answered already
	CodeNotJournal  = "REVIEW-05" // not a journal of the organisation
	CodeOpenQueries = "REVIEW-06" // a query has no answer yet
)

// Overdue is how long after a quarter ends it waits for its review (the owner's decision of 6 Nov 2026).
const Overdue = 60 * 24 * time.Hour

// The states of a quarter's review.
const (
	Reviewed = "reviewed"  // a signed-off review, not superseded, covers its last month
	InReview = "in_review" // a review of its last month is open
	Due      = "due"       // ended, not reviewed, within 60 days of its end
	Late     = "overdue"   // ended more than 60 days ago, not reviewed
)

// Quarter is a calendar quarter that has ended, with the month whose review covers it.
type Quarter struct {
	Year, Number int
	StartsOn     time.Time // its first day
	EndsOn       time.Time // its last day
	DueBy        time.Time // 60 days after it ends
	Period       *storage.Period
	Review       *storage.AccountantReview // the review that covers it, or the one open; nil for none
	Status       string
}

// Label is the quarter as people say it, such as "Jul–Sep 2026".
func (q Quarter) Label() string {
	return q.StartsOn.Format("Jan") + "–" + q.EndsOn.Format("Jan 2006")
}

// Key is the quarter as a key, such as "2026-Q3".
func (q Quarter) Key() string { return fmt.Sprintf("%d-Q%d", q.Year, q.Number) }

// Quarters are the calendar quarters that ended on or after from and before today, newest first, each with the
// period holding its last day and the review of that period that covers it.
func Quarters(periods []storage.Period, reviews []storage.AccountantReview, from, today time.Time) []Quarter {
	var out []Quarter
	day := func(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC) }
	today, from = day(today), day(from)
	start := time.Date(from.Year(), time.Month((int(from.Month())-1)/3*3+1), 1, 0, 0, 0, 0, time.UTC)
	for s := start; ; s = s.AddDate(0, 3, 0) {
		end := s.AddDate(0, 3, -1)
		if !end.Before(today) {
			break
		}
		q := Quarter{Year: s.Year(), Number: (int(s.Month())-1)/3 + 1, StartsOn: s, EndsOn: end, DueBy: end.Add(Overdue)}
		for i := range periods {
			if p := periods[i]; !end.Before(day(p.StartsOn)) && !end.After(day(p.EndsOn)) {
				q.Period = &periods[i]
			}
		}
		q.Status = Due
		if today.After(q.DueBy) {
			q.Status = Late
		}
		for i := range reviews {
			r := &reviews[i]
			if q.Period == nil || r.PeriodID != q.Period.ID || r.Status == storage.AccountantReviewSuperseded {
				continue
			}
			q.Review = r
			if r.Status == storage.AccountantReviewSignedOff {
				q.Status = Reviewed
				break
			}
			if q.Status == Due {
				q.Status = InReview
			}
		}
		out = append([]Quarter{q}, out...)
	}
	return out
}
