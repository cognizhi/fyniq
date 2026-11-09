package compliance

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/team"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// reviewIs fails unless r has the status, the counts and a detail saying says.
func reviewIs(t *testing.T, what string, r Review, status string, waiting, overdue int, says string) {
	t.Helper()
	if r.Status != status || r.Waiting != waiting || r.Overdue != overdue || !strings.Contains(r.Detail, says) {
		t.Errorf("%s: %s, %d waiting, %d overdue, %q; want %s, %d, %d, saying %q", what, r.Status, r.Waiting, r.Overdue, r.Detail,
			status, waiting, overdue, says)
	}
}

// Each recurring review reads its own source under its own rule (design D6): what waits, what of it is overdue, and the
// last sign-off; nothing waiting is signed, or nothing due when nothing was ever signed off; a review the team mode does
// not call for does not apply.
// req: CMP-03
// req: FR-CTL-06
// req: FM-3
// req: compliance/compliance-center Attestation recorded
func TestEachReviewReadsItsOwnSource(t *testing.T) {
	now := time.Date(2099, 11, 20, 4, 0, 0, 0, time.UTC)
	today := Day(now)
	alice, bob := uuid.New(), uuid.New()
	ptr := func(t time.Time) *time.Time { return &t }

	t.Run("the review log", func(t *testing.T) {
		entries := []storage.ReviewLogEntry{
			{ID: uuid.New(), Kind: "self_approval", LoggedAt: now.AddDate(0, 0, -20)},
			{ID: uuid.New(), Kind: "self_approval", LoggedAt: now.AddDate(0, 0, -3)},
			{ID: uuid.New(), Kind: "self_approval", LoggedAt: now.AddDate(0, 0, -30), ReviewedBy: &bob, ReviewedAt: ptr(now.AddDate(0, 0, -25))},
			{ID: uuid.New(), Kind: storage.ReviewBreakGlass, LoggedAt: now.AddDate(0, 0, -40)},
		}
		r := reviewLogReview(entries, team.Founder, now)
		reviewIs(t, "two waiting, one past 14 days", r, ReviewOverdue, 2, 1, "2 logged action(s)")
		if r.LastBy != "user:"+bob.String() || !r.LastAt.Equal(now.AddDate(0, 0, -25)) || !r.DueOn.Equal(Day(now.AddDate(0, 0, -6))) {
			t.Errorf("the review log's sign-off and due date: %+v", r)
		}
		reviewIs(t, "one waiting within 14 days", reviewLogReview(entries[1:], team.Founder, now), ReviewDue, 1, 0, "0 of them past 14 days")
		reviewIs(t, "all reviewed", reviewLogReview(entries[2:], team.Founder, now), ReviewSigned, 0, 0, "No logged action waits")
		reviewIs(t, "Standard mode", reviewLogReview(entries, team.Standard, now), ReviewNotApplicable, 0, 0, "Standard mode")
	})

	t.Run("the access review", func(t *testing.T) {
		members := []storage.Member{{UserID: alice, Since: time.Date(2099, 1, 10, 0, 0, 0, 0, time.UTC)}}
		q2 := storage.AccessReviewAttestation{Quarter: time.Date(2099, 4, 1, 0, 0, 0, 0, time.UTC), AttestedBy: "user:" + alice.String(), AttestedAt: time.Date(2099, 7, 5, 0, 0, 0, 0, time.UTC)}
		r := accessReviewOf(members, []storage.AccessReviewAttestation{q2}, today)
		reviewIs(t, "Q3 past 14 October", r, ReviewOverdue, 1, 1, "Q3 2099")
		if r.LastBy != q2.AttestedBy || !r.DueOn.Equal(time.Date(2099, 10, 14, 0, 0, 0, 0, time.UTC)) {
			t.Errorf("the access review's sign-off and due date: %+v", r)
		}
		r = accessReviewOf(members, []storage.AccessReviewAttestation{q2}, time.Date(2099, 10, 5, 0, 0, 0, 0, time.UTC))
		reviewIs(t, "Q3 within its 14 days", r, ReviewDue, 1, 0, "due by 14 Oct 2099")
		q3 := storage.AccessReviewAttestation{Quarter: time.Date(2099, 7, 1, 0, 0, 0, 0, time.UTC), AttestedBy: "user:" + alice.String(), AttestedAt: time.Date(2099, 10, 3, 0, 0, 0, 0, time.UTC)}
		reviewIs(t, "Q3 attested", accessReviewOf(members, []storage.AccessReviewAttestation{q2, q3}, today), ReviewSigned, 0, 0, "No quarter")
		reviewIs(t, "no quarter ended yet", accessReviewOf(members, nil, time.Date(2099, 2, 1, 0, 0, 0, 0, time.UTC)), ReviewNothingDue, 0, 0, "No quarter")
	})

	t.Run("the accountant's review", func(t *testing.T) {
		start := time.Date(2099, 4, 1, 0, 0, 0, 0, time.UTC)
		org := storage.Organisation{Profile: storage.OrganisationProfile{BooksStart: &start}}
		var periods []storage.Period
		for m := 4; m <= 10; m++ {
			s := time.Date(2099, time.Month(m), 1, 0, 0, 0, 0, time.UTC)
			periods = append(periods, storage.Period{ID: uuid.New(), StartsOn: s, EndsOn: s.AddDate(0, 1, -1), Status: storage.PeriodClosed})
		}
		june := storage.AccountantReview{ID: uuid.New(), PeriodID: periods[2].ID, Status: "signed_off", SignedBy: &bob, SignedAt: ptr(time.Date(2099, 8, 1, 0, 0, 0, 0, time.UTC))}
		r := accountantReviewOf(team.Founder, org, periods, []storage.AccountantReview{june}, today)
		reviewIs(t, "Jul–Sep within its 60 days", r, ReviewDue, 1, 0, "Jul–Sep 2099")
		if r.LastBy != "user:"+bob.String() || !r.DueOn.Equal(time.Date(2099, 11, 29, 0, 0, 0, 0, time.UTC)) {
			t.Errorf("the accountant's review's sign-off and due date: %+v", r)
		}
		reviewIs(t, "both quarters waiting, Apr–Jun past 60 days", accountantReviewOf(team.Founder, org, periods, nil, today), ReviewOverdue, 2, 1, "Apr–Jun 2099")
		reviewIs(t, "Standard mode", accountantReviewOf(team.Standard, org, periods, nil, today), ReviewNotApplicable, 0, 0, "FM-7")
		reviewIs(t, "no books", accountantReviewOf(team.Founder, storage.Organisation{}, nil, nil, today), ReviewNothingDue, 0, 0, "No books yet")
	})

	t.Run("the close's sign-off", func(t *testing.T) {
		month := func(m int, status storage.PeriodStatus) storage.Period {
			s := time.Date(2099, time.Month(m), 1, 0, 0, 0, 0, time.UTC)
			return storage.Period{ID: uuid.New(), StartsOn: s, EndsOn: s.AddDate(0, 1, -1), Status: status}
		}
		periods := []storage.Period{month(8, storage.PeriodClosed), month(9, storage.PeriodOpen), month(10, storage.PeriodOpen), month(11, storage.PeriodOpen)}
		closes := map[uuid.UUID]*storage.PeriodClose{periods[0].ID: {AttestedBy: "user:" + alice.String(), AttestedAt: time.Date(2099, 9, 10, 0, 0, 0, 0, time.UTC)}}
		closeOf := func(p storage.Period) (*storage.PeriodClose, error) { return closes[p.ID], nil }
		tenth := time.Date(2099, 11, 10, 0, 0, 0, 0, time.UTC)
		r, err := closeSignOff(periods, closeOf, tenth)
		if err != nil {
			t.Fatal(err)
		}
		reviewIs(t, "September past 15 days, October not", r, ReviewOverdue, 2, 1, "September 2099")
		if r.LastBy != "user:"+alice.String() || !r.DueOn.Equal(time.Date(2099, 10, 15, 0, 0, 0, 0, time.UTC)) {
			t.Errorf("the close's sign-off and due date: %+v", r)
		}
		r, _ = closeSignOff(periods[:1], closeOf, tenth)
		reviewIs(t, "every month closed", r, ReviewSigned, 0, 0, "Every month that ended is closed.")
		r, _ = closeSignOff(periods[3:], closeOf, tenth)
		reviewIs(t, "the month not ended", r, ReviewNothingDue, 0, 0, "Every month that ended is closed.")
	})

	t.Run("the break-glass post-review", func(t *testing.T) {
		ended := func(daysAgo int) storage.BreakGlass {
			at := now.AddDate(0, 0, -daysAgo)
			return storage.BreakGlass{ID: uuid.New(), StartedAt: at.Add(-time.Hour), ExpiresAt: at.Add(23 * time.Hour), EndedAt: &at}
		}
		old, recent, reviewed := ended(10), ended(2), ended(20)
		active := storage.BreakGlass{ID: uuid.New(), StartedAt: now.Add(-time.Hour), ExpiresAt: now.Add(23 * time.Hour)}
		entries := []storage.ReviewLogEntry{{Kind: storage.ReviewBreakGlass, ObjectKey: reviewed.ID.String(), ReviewedBy: &bob, ReviewedAt: ptr(now.AddDate(0, 0, -15))}}
		r := breakGlassReview([]storage.BreakGlass{old, recent, reviewed, active}, entries, now)
		reviewIs(t, "one past 7 days, one within", r, ReviewOverdue, 2, 1, "2 break-glass(es)")
		if r.LastBy != "user:"+bob.String() {
			t.Errorf("the break-glass's sign-off: %+v", r)
		}
		reviewIs(t, "one within 7 days", breakGlassReview([]storage.BreakGlass{recent, reviewed}, entries, now), ReviewDue, 1, 0, "0 of them past 7 days")
		reviewIs(t, "every one reviewed", breakGlassReview([]storage.BreakGlass{reviewed, active}, entries, now), ReviewSigned, 0, 0, "Every break-glass is reviewed.")
		reviewIs(t, "none used", breakGlassReview(nil, nil, now), ReviewNothingDue, 0, 0, "No break-glass was used.")
	})

	t.Run("the policy acknowledgements", func(t *testing.T) {
		members := []storage.Member{{UserID: alice, Since: now.AddDate(-1, 0, 0)}, {UserID: bob, Since: now.AddDate(0, 0, -5)}}
		lib := storage.PolicyLibrary{
			Versions: []storage.PublishedPolicy{
				{Key: "backup", Version: 1, PublishedAt: now.AddDate(0, 0, -60)},
				{Key: "backup", Version: 2, PublishedAt: now.AddDate(0, 0, -40)},
				{Key: "notice-contacts", Version: 1, PublishedAt: now.AddDate(0, 0, -40)}, // given to contacts: no member acknowledges it
			},
			Acknowledgements: []storage.PolicyAcknowledgement{{Key: "backup", Version: 1, By: "user:" + alice.String(), At: now.AddDate(0, 0, -59)}},
		}
		r := policyAcknowledgements(lib, members, now)
		reviewIs(t, "version 2: Alice past 30 days, Bob joined 5 days ago", r, ReviewOverdue, 2, 1, "2 acknowledgement(s)")
		if !r.DueOn.Equal(Day(now.AddDate(0, 0, -10))) || r.LastBy != "user:"+alice.String() {
			t.Errorf("the acknowledgements' due date and sign-off: %+v", r)
		}
		lib.Acknowledgements = append(lib.Acknowledgements, storage.PolicyAcknowledgement{Key: "backup", Version: 2, By: "user:" + alice.String(), At: now.AddDate(0, 0, -1)})
		reviewIs(t, "only Bob, within 30 days of joining", policyAcknowledgements(lib, members, now), ReviewDue, 1, 0, "0 of them overdue")
		lib.Acknowledgements = append(lib.Acknowledgements, storage.PolicyAcknowledgement{Key: "backup", Version: 2, By: "user:" + bob.String(), At: now})
		reviewIs(t, "everyone acknowledged", policyAcknowledgements(lib, members, now), ReviewSigned, 0, 0, "Every member acknowledged")
		reviewIs(t, "nothing published", policyAcknowledgements(storage.PolicyLibrary{}, members, now), ReviewNothingDue, 0, 0, "No policy is published yet.")
	})

	t.Run("the risk reviews", func(t *testing.T) {
		risk := func(ref, state string, next time.Time, reviewed *time.Time) storage.Risk {
			r := storage.Risk{Ref: ref, RiskFields: storage.RiskFields{State: state, NextReviewOn: next}}
			if reviewed != nil {
				r.LastReview = &storage.RiskReview{ReviewedBy: "user:" + alice.String(), ReviewedAt: *reviewed}
			}
			return r
		}
		late := risk("R1", "open", today.AddDate(0, 0, -3), nil)
		soon := risk("R2", "open", today.AddDate(0, 1, 0), ptr(now.AddDate(0, -2, 0)))
		closed := risk("R3", "closed", today.AddDate(0, 0, -30), nil)
		reviewIs(t, "R1 past its date", riskReviews([]storage.Risk{late, soon, closed}, today), ReviewOverdue, 1, 1, "1 risk(s)")
		r := riskReviews([]storage.Risk{soon, closed}, today)
		reviewIs(t, "R2 reviewed, next a month on", r, ReviewSigned, 0, 0, "R2's")
		if !r.DueOn.Equal(today.AddDate(0, 1, 0)) {
			t.Errorf("the next review date: %+v", r)
		}
		reviewIs(t, "never reviewed, none past", riskReviews([]storage.Risk{risk("R4", "open", today.AddDate(0, 0, 9), nil)}, today), ReviewNothingDue, 0, 0, "R4's")
		reviewIs(t, "an empty register", riskReviews(nil, today), ReviewNothingDue, 0, 0, "The risk register is empty.")
	})
}
