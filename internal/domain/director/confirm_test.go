package director

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-EQ-12 (design D13), worked by hand: the books start on 1 Oct 2025 and the first year ends on 30 Sep 2026. On 15 Oct
// 2026 the latest year end is 30 Sep 2026; on 29 Sep 2026 no year has ended. Mei Lin confirmed -1,000.00 on 2500 at that
// end: with the ledger holding -1,000.00 there, she is confirmed; holding -1,200.00 after an entry dated in the year was
// posted later, changed since; with nothing confirmed, not confirmed.
// req: FR-EQ-12
// req: equity/founders-money Unconfirmed balance on the close checklist
func TestYearEndConfirmationState(t *testing.T) {
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
	start, end := day(2025, time.October, 1), day(2026, time.September, 30)
	p := storage.OrganisationProfile{FYStartMonth: 10, BooksStart: &start, FirstYearEnd: &end}
	if got, ok := LastYearEnd(p, day(2026, time.October, 15)); !ok || !got.Equal(end) {
		t.Errorf("on 15 Oct 2026: %s %v, want 2026-09-30", got, ok)
	}
	if _, ok := LastYearEnd(p, day(2026, time.September, 29)); ok {
		t.Error("on 29 Sep 2026 a year has ended")
	}
	if !IsYearEnd(p, end) || IsYearEnd(p, day(2026, time.August, 31)) {
		t.Error("the year ends are wrong")
	}
	if got, ok := LastYearEnd(p, day(2027, time.December, 1)); !ok || !got.Equal(day(2027, time.September, 30)) {
		t.Errorf("on 1 Dec 2027: %s %v, want 2027-09-30", got, ok)
	}

	sgd := money.MustLookup("SGD")
	amount := func(s string) money.Money {
		m, err := money.Parse(s, sgd)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	loan, deferred := uuid.New(), uuid.New()
	confirmed := []RecordedConfirmation{{ID: uuid.New(), ConfirmedAt: day(2026, time.October, 9),
		Confirmed: Confirmation{YearEnd: end, Balances: map[uuid.UUID]money.Money{loan: amount("-1000.00"), deferred: amount("0.00")}}}}
	latest := LatestFor(confirmed, end)
	if s := StateOf(latest, map[uuid.UUID]money.Money{loan: amount("-1000.00")}); s != Confirmed {
		t.Errorf("the same balances: %s", s)
	}
	if s := StateOf(latest, map[uuid.UUID]money.Money{loan: amount("-1200.00")}); s != ChangedSince {
		t.Errorf("a later entry in the year: %s", s)
	}
	if s := StateOf(latest, map[uuid.UUID]money.Money{loan: amount("-1000.00"), deferred: amount("-50.00")}); s != ChangedSince {
		t.Errorf("a fee owed since: %s", s)
	}
	if s := StateOf(LatestFor(confirmed, day(2027, time.September, 30)), nil); s != NotConfirmed {
		t.Errorf("another year: %s", s)
	}
}
