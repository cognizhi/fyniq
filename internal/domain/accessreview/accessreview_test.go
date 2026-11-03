package accessreview

import (
	"testing"
	"time"
)

func day(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

// req: FR-CTL-06
func TestQuarters(t *testing.T) {
	for in, want := range map[string]string{"2026-01-01": "2026-01-01", "2026-03-31": "2026-01-01", "2026-04-01": "2026-04-01",
		"2026-08-15": "2026-07-01", "2026-12-31": "2026-10-01"} {
		if got := QuarterOf(day(in)).Format(time.DateOnly); got != want {
			t.Errorf("QuarterOf(%s) = %s, want %s", in, got, want)
		}
	}
	if got := Label(day("2026-07-01")); got != "Q3 2026" {
		t.Errorf("Label = %q", got)
	}
	if got := DueBy(day("2026-07-01")).Format(time.DateOnly); got != "2026-10-14" {
		t.Errorf("DueBy = %s, want 2026-10-14", got)
	}
}

// req: FR-CTL-06, C.2 #7
func TestWhichQuarter(t *testing.T) {
	since := day("2026-01-10")
	// On 1 October the third quarter has just ended: it is the one to attest, and the second is the one due.
	if q, ok := ToAttest(day("2026-10-01"), since); !ok || q != day("2026-07-01") {
		t.Errorf("ToAttest = %s %v", q.Format(time.DateOnly), ok)
	}
	if q, ok := Due(day("2026-10-14"), since); !ok || q != day("2026-04-01") {
		t.Errorf("Due on the last day of grace = %s %v, want the second quarter", q.Format(time.DateOnly), ok)
	}
	if q, ok := Due(day("2026-10-15"), since); !ok || q != day("2026-07-01") {
		t.Errorf("Due the day after = %s %v, want the third quarter", q.Format(time.DateOnly), ok)
	}
	// An organisation that began after a quarter ended has nothing to review for it.
	if _, ok := ToAttest(day("2026-10-20"), day("2026-10-02")); ok {
		t.Error("an organisation that began in the fourth quarter has the third to attest")
	}
	if _, ok := Due(day("2026-10-20"), day("2026-09-30")); !ok {
		t.Error("an organisation that began on the third quarter's last day has no third quarter due")
	}
}

// req: C.2 #7
func TestCovered(t *testing.T) {
	q2, q3 := day("2026-04-01"), day("2026-07-01")
	if Covered(q3, []time.Time{q2}) {
		t.Error("an earlier quarter's attestation covers a later quarter")
	}
	if !Covered(q2, []time.Time{q3}) || !Covered(q3, []time.Time{q3}) {
		t.Error("a quarter's own or a later attestation does not cover it")
	}
}
