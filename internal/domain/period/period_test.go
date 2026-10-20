package period

import (
	"errors"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func date(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

func spans(y Year) []string {
	var out []string
	for _, m := range y.Months {
		out = append(out, m.StartsOn.Format(time.DateOnly)+".."+m.EndsOn.Format(time.DateOnly))
	}
	return out
}

func code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// req: FR-ORG-02
// req: ledger/posting-rules Posting conventions
func TestYearsAreCalendarMonthsCutAtTheBooksStartAndTheYearEnds(t *testing.T) {
	c := Calendar{BooksStart: date("2026-03-15"), FirstYearEnd: date("2027-03-31")}

	first := c.Year(1)
	got := spans(first)
	if len(got) != 13 || got[0] != "2026-03-15..2026-03-31" || got[1] != "2026-04-01..2026-04-30" || got[12] != "2027-03-01..2027-03-31" {
		t.Errorf("the first year, a short first month then twelve calendar months: %v", got)
	}
	second := c.Year(2)
	if second.StartsOn != date("2027-04-01") || second.EndsOn != date("2028-03-31") || len(second.Months) != 12 {
		t.Errorf("the second year is the twelve months after the first: %v", spans(second))
	}
	if feb := second.Months[10]; feb.EndsOn != date("2028-02-29") {
		t.Errorf("a leap February ends on the 29th: %v", feb)
	}

	for day, want := range map[string]int{"2026-03-14": 0, "2026-03-15": 1, "2027-03-31": 1, "2027-04-01": 2, "2029-12-31": 4} {
		if n := c.Number(date(day)); n != want {
			t.Errorf("%s is in year %d, not %d", day, n, want)
		}
	}
}

func TestTheNextYearToOpen(t *testing.T) {
	c := Calendar{BooksStart: date("2026-01-01"), FirstYearEnd: date("2026-12-31")}
	periods := func(y Year) (out []storage.Period) {
		for _, m := range y.Months {
			out = append(out, storage.Period{StartsOn: m.StartsOn, EndsOn: m.EndsOn})
		}
		return out
	}
	if y := c.Next(nil); y.Number != 1 || y.StartsOn != date("2026-01-01") {
		t.Errorf("with no period the first year opens: %+v", y.Span)
	}
	if y := c.Next(periods(c.Year(1))); y.Number != 2 || y.StartsOn != date("2027-01-01") {
		t.Errorf("then the second: %+v", y.Span)
	}
	// Periods that stop before their year's end (the sample company's, made before the years were set): the
	// rest of that year opens.
	part := periods(c.Year(1))[:9]
	if y := c.Next(part); y.Number != 1 || y.StartsOn != date("2026-10-01") || len(y.Months) != 3 {
		t.Errorf("the rest of the first year: %+v %v", y.Span, spans(y))
	}
	// Periods from before the books start leave the first year to open.
	early := []storage.Period{{StartsOn: date("2025-12-01"), EndsOn: date("2025-12-31")}}
	if y := c.Next(early); y.Number != 1 {
		t.Errorf("periods before the books start: %+v", y.Span)
	}
}

func TestAYearOpensAtMostOneYearAhead(t *testing.T) {
	c := Calendar{BooksStart: date("2026-04-01"), FirstYearEnd: date("2027-03-31")}
	today := date("2026-10-21") // in year 1
	if err := c.MayOpen(c.Year(2), today); err != nil {
		t.Errorf("the next year may open: %v", err)
	}
	if err := c.MayOpen(c.Year(3), today); code(err) != CodeTooFar {
		t.Errorf("two years ahead: %v", err)
	}
	if err := c.MayOpen(c.Year(2), date("2026-01-01")); err != nil {
		t.Errorf("before the books start the first two years may open: %v", err)
	}
}

func TestTheYearsAreFixedOnceAPeriodExists(t *testing.T) {
	start, end, other := date("2026-04-01"), date("2027-03-31"), date("2026-05-01")
	before := storage.OrganisationProfile{BooksStart: &start, FirstYearEnd: &end, FYStartMonth: 4}
	moved := before
	moved.BooksStart = &other
	if err := Fixed(before, moved, 0); err != nil {
		t.Errorf("with no period the years may change: %v", err)
	}
	if err := Fixed(before, moved, 12); code(err) != CodeYearsOpen {
		t.Errorf("the books start moved with periods open: %v", err)
	}
	month := before
	month.FYStartMonth = 5
	if err := Fixed(before, month, 12); code(err) != CodeYearsOpen {
		t.Errorf("the start month changed with periods open: %v", err)
	}
	same := before
	same.LegalName = "Renamed Pte Ltd"
	if err := Fixed(before, same, 12); err != nil {
		t.Errorf("other fields still change: %v", err)
	}
	unset := storage.OrganisationProfile{FYStartMonth: 1}
	if err := Fixed(unset, before, 12); err != nil {
		t.Errorf("years set for the first time beside existing periods: %v", err)
	}
	if _, err := CalendarOf(storage.OrganisationProfile{BooksStart: &start}); code(err) != CodeNoYear {
		t.Errorf("no first year end: %v", err)
	}
}
