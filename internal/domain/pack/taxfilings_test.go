package pack_test

import (
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/pack"
)

func dayOf(s string) time.Time {
	d, err := pack.ParseDate(s)
	if err != nil {
		panic(err)
	}
	return d
}

// The pack loads and its ten pinned cases, worked by hand from IRAS's pages, pass.
// req: FR-AR-11
// req: FR-TAX-06
// req: QA-ENG-08
func TestTheTaxFilingsPackLoadsAndItsPinnedCasesPass(t *testing.T) {
	v := version(t, "sg-tax-filings/2026.1")
	rs := pack.RunPinned(v)
	if len(rs) != 10 {
		t.Fatalf("%d pinned cases ran, want 10", len(rs))
	}
	for _, r := range rs {
		if !r.Pass {
			t.Errorf("%s %s: got %q, want %q (%s)", r.Table, r.Case, r.Got, r.Want, r.Why)
		}
	}
}

// The twelve months end on the anniversary, a month's end stays a month's end, insolvency after the twelve months changes nothing, and the five years end the day before the
// anniversary; a debt written off before the twelve months waits for them, one written off after has no wait.
// req: FR-AR-11
func TestBadDebtReliefDatesFollowIRAsPeriods(t *testing.T) {
	v := version(t, "sg-tax-filings/2026.1")
	for name, tc := range map[string]struct {
		supply, writtenOff, insolvent, today string
		status, from, by                     string
	}{
		"31 Jan clamps to the end of February": {"2027-01-31", "2027-02-01", "", "2028-02-28", pack.ReliefEligible, "2028-01-31", "2032-01-30"},
		"29 Feb supply, twelve months":         {"2028-02-29", "2028-03-31", "", "2029-02-27", pack.ReliefNotYet, "2029-02-28", "2033-02-27"},
		"insolvent after the twelve months":    {"2026-01-15", "2026-02-01", "2027-03-01", "2027-01-14", pack.ReliefNotYet, "2027-01-15", "2031-01-14"},
		"written off long after":               {"2026-01-15", "2028-05-05", "", "2028-05-05", pack.ReliefEligible, "2028-05-05", "2031-01-14"},
		"the last day to claim":                {"2026-01-15", "2026-02-01", "", "2031-01-14", pack.ReliefEligible, "2027-01-15", "2031-01-14"},
	} {
		t.Run(name, func(t *testing.T) {
			var ins *time.Time
			if tc.insolvent != "" {
				d := dayOf(tc.insolvent)
				ins = &d
			}
			r, err := pack.BadDebtRelief(v, dayOf(tc.supply), dayOf(tc.writtenOff), ins, dayOf(tc.today))
			if err != nil {
				t.Fatal(err)
			}
			if r.Status != tc.status || r.EligibleFrom.Format(time.DateOnly) != tc.from || r.ClaimBy.Format(time.DateOnly) != tc.by {
				t.Errorf("%+v, want %s from %s by %s", r, tc.status, tc.from, tc.by)
			}
		})
	}
}

// A revenue that is not an amount is refused, and the optional form is offered only at the Lite limit.
// req: FR-TAX-06
func TestTheFilingFormRefusesARevenueThatIsNotAnAmount(t *testing.T) {
	v := version(t, "sg-tax-filings/2026.1")
	if _, _, err := pack.FilingForm(v, "lots"); err == nil {
		t.Error("a revenue of lots was accepted")
	}
	if _, _, err := pack.FilingForm(v, "-1"); err == nil {
		t.Error("a negative revenue was accepted")
	}
	if f, o, err := pack.FilingForm(v, "0"); err != nil || f != "C-S (Lite)" || o != "C-S" {
		t.Errorf("a company with no revenue: %s %s %v", f, o, err)
	}
	if f, o, err := pack.FilingForm(v, "4999999.99"); err != nil || f != "C-S" || o != "" {
		t.Errorf("a company just under S$5 million: %s %s %v", f, o, err)
	}
}
