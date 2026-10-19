package gst

import (
	"slices"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func day(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

func ptr(t time.Time) *time.Time { return &t }

// The GST status on any date follows the registration's effective dates.
// req: FR-ORG-03
func TestTheStatusFollowsTheEffectiveDates(t *testing.T) {
	regs := []storage.GSTRegistration{
		{GSTNumber: "202312345K", RegisteredFrom: day("2026-04-01"), DeregisteredFrom: ptr(day("2028-01-01"))},
		{GSTNumber: "202312345K", RegisteredFrom: day("2029-07-01")}, // registered again, still registered
	}
	for d, want := range map[string]bool{"2026-03-31": false, "2026-04-01": true, "2027-12-31": true, "2028-01-01": false,
		"2029-06-30": false, "2029-07-01": true, "2035-01-01": true} {
		if _, got := StatusOn(regs, day(d)); got != want {
			t.Errorf("registered on %s: %v, want %v", d, got, want)
		}
	}
}

// The return periods follow the frequency and the quarters' alignment, and never run outside the
// registration.
// req: FR-ORG-03
func TestReturnPeriodsFollowTheFrequencyAndAlignment(t *testing.T) {
	quarterly := func(startMonth int, from string, to *time.Time) storage.GSTRegistration {
		return storage.GSTRegistration{ReturnFrequency: storage.ReturnQuarterly, QuarterStartMonth: startMonth, RegisteredFrom: day(from), DeregisteredFrom: to}
	}
	monthly := storage.GSTRegistration{ReturnFrequency: storage.ReturnMonthly, RegisteredFrom: day("2026-01-01"), DeregisteredFrom: ptr(day("2027-02-15"))}
	for _, c := range []struct {
		name       string
		r          storage.GSTRegistration
		on         string
		start, end string
	}{
		{"calendar quarters", quarterly(1, "2026-01-01", nil), "2026-05-10", "2026-04-01", "2026-06-30"},
		{"a short first quarter", quarterly(1, "2026-05-15", nil), "2026-05-20", "2026-05-15", "2026-06-30"},
		{"quarters from March, across the year end", quarterly(3, "2026-01-01", nil), "2027-01-15", "2026-12-01", "2027-02-28"},
		{"quarters from February", quarterly(2, "2026-01-01", nil), "2026-04-30", "2026-02-01", "2026-04-30"},
		{"a month", monthly, "2026-02-10", "2026-02-01", "2026-02-28"},
		{"the last, short month before deregistration", monthly, "2027-02-10", "2027-02-01", "2027-02-14"},
	} {
		start, end, ok := ReturnPeriod(c.r, day(c.on))
		if !ok || !start.Equal(day(c.start)) || !end.Equal(day(c.end)) {
			t.Errorf("%s, on %s: %s to %s (%v); want %s to %s", c.name, c.on, start.Format(time.DateOnly), end.Format(time.DateOnly), ok, c.start, c.end)
		}
	}
	if _, _, ok := ReturnPeriod(monthly, day("2027-02-15")); ok {
		t.Error("a return period on the day of deregistration")
	}
}

// Invoice basis can be chosen; cash basis is not offered before the Cash Accounting Scheme's rules are
// pinned (ENG-45), and every other broken rule is reported with it.
// req: FR-ORG-03
func TestInvoiceBasisOnlyAndEveryRuleAtOnce(t *testing.T) {
	ok := storage.NewGSTRegistration{RegisteredFrom: day("2026-04-01"), GSTNumber: "202312345K", ReturnFrequency: storage.ReturnQuarterly,
		QuarterStartMonth: 1, AccountingBasis: storage.BasisInvoice}
	if p := Check(ok); p != nil {
		t.Fatalf("an invoice-basis registration: %v", p)
	}
	cash := ok
	cash.AccountingBasis = "cash"
	if p := Check(cash); len(p) != 1 || p[0].Code != CodeBasis {
		t.Errorf("cash basis: %v", p)
	}
	bad := storage.NewGSTRegistration{RegisteredFrom: day("2026-04-01"), DeregisteredFrom: ptr(day("2026-04-01")), GSTNumber: " ",
		ReturnFrequency: "yearly", QuarterStartMonth: 4, AccountingBasis: "cash"}
	var got []string
	for _, x := range Check(bad) {
		got = append(got, x.Code+" "+x.Field)
	}
	want := []string{"GST-01 deregistered_from", "GST-02 gst_number", "GST-03 return_frequency", "GST-04 quarter_start_month", "GST-05 accounting_basis"}
	if !slices.Equal(got, want) {
		t.Errorf("problems:\n got %v\nwant %v", got, want)
	}
}
