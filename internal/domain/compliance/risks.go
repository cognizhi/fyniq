package compliance

import (
	"strings"
	"time"

	registry "github.com/cognizhi/fyniq/compliance"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ReviewMonths is how often a risk is reviewed unless its version says otherwise: each quarter (design D7).
const ReviewMonths = 3

// Day is t's date in Singapore, the books' time zone, as a date at UTC midnight.
func Day(t time.Time) time.Time {
	y, m, d := t.In(clock.Singapore).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// NextQuarterEnd is the last day of the calendar quarter after the one t falls in: a new risk's first review (design D7).
func NextQuarterEnd(t time.Time) time.Time {
	d := Day(t)
	first := time.Date(d.Year(), time.Month((int(d.Month())-1)/3*3+1), 1, 0, 0, 0, 0, time.UTC) // this quarter's first day
	return first.AddDate(0, 6, -1)
}

// NextReview is the review date a review on t sets: months after its day.
func NextReview(t time.Time, months int) time.Time {
	if months < 1 {
		months = ReviewMonths
	}
	return Day(t).AddDate(0, months, 0)
}

// SeedRisks are the operator's starting risks (CMP-06, the owner's decision of 9 Nov 2026): §19's, open, each to be
// reviewed each quarter, the first time at the end of the next quarter.
func SeedRisks(seed []registry.SeedRisk, savedBy string, now time.Time) []storage.NewRisk {
	out := make([]storage.NewRisk, len(seed))
	for i, r := range seed {
		out[i] = storage.NewRisk{Ref: r.Ref, SavedBy: savedBy, At: now, RiskFields: storage.RiskFields{Title: r.Title, Description: r.Description,
			Owner: r.Owner, Likelihood: r.Likelihood, Impact: r.Impact, Mitigation: r.Mitigation, ReviewMonths: ReviewMonths,
			NextReviewOn: NextQuarterEnd(now), State: "open"}}
	}
	return out
}

// RiskProblem is what is wrong with a field of a risk, and how to fix it (ENG-23).
type RiskProblem struct{ Field, Message, Fix string }

// CheckRisk refuses a risk's fields that the register does not keep, naming each.
func CheckRisk(f storage.RiskFields, today time.Time) []RiskProblem {
	var out []RiskProblem
	if t := strings.TrimSpace(f.Title); t == "" || len(t) > 300 {
		out = append(out, RiskProblem{"title", "is empty or longer than 300 characters", "Say the risk in a sentence"})
	}
	if len(f.Description) > 4000 {
		out = append(out, RiskProblem{"description", "is longer than 4,000 characters", "Shorten it"})
	}
	if t := strings.TrimSpace(f.Mitigation); t == "" || len(t) > 4000 {
		out = append(out, RiskProblem{"mitigation", "is empty or longer than 4,000 characters", "Say how the risk is reduced"})
	}
	if f.ReviewMonths < 1 || f.ReviewMonths > 12 {
		out = append(out, RiskProblem{"review_months", "is not between 1 and 12", "Review it at least yearly: 3 is each quarter"})
	}
	if !f.NextReviewOn.After(Day(today)) {
		out = append(out, RiskProblem{"next_review_on", "is not after today", "Choose a later day for its next review"})
	}
	return out
}
