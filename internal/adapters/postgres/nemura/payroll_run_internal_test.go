package nemura

import (
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/nemura/leave"
	"github.com/cognizhi/fyniq/internal/nemura/payroll"
)

// A part month is counted in working days (NM-PAY-12, payroll.PartMonthMethod; owner decision of 21 Nov 2026, MOM's method): the working days of the person's pattern the pay record covers, with the start and
// leaving days both counted, over the working days of the month, rounded once to the cent (RND-PAY-RATIO). Rest days are left out and public holidays are counted. Hand-worked goldens of what the adapter
// reads from a person's records; the pure rule is run against the oracle's by test/differential/part-months.
// req: NM-PAY-12, RND-PAY-RATIO
func TestAPartMonthIsCountedByWorkingDays(t *testing.T) {
	d := func(s string) time.Time { v, _ := time.Parse(time.DateOnly, s); return v }
	ptr := func(s string) *time.Time { v := d(s); return &v }
	basic := apd.New(450000, -2)
	if payroll.PartMonthMethod != "working_days" {
		t.Fatalf("the method: %s", payroll.PartMonthMethod)
	}
	monToFri, monToSat := leave.Pattern{Weekdays: []int{1, 2, 3, 4, 5}}, leave.Pattern{Weekdays: []int{1, 2, 3, 4, 5, 6}}
	for _, tc := range []struct {
		name    string
		recs    []payRec
		start   string
		leaving *time.Time
		month   string
		pat     leave.Pattern
		set     bool
		days    int
		pay     string
	}{
		{"a full month (22 working days in June 2026)", []payRec{{From: d("2026-01-05")}}, "2025-01-01", nil, "2026-06-01", monToFri, false, 22, "4500.00"},
		{"a joiner on Wed 10 June: 15 of 22 (not 21 of 30)", []payRec{{From: d("2026-01-05")}}, "2026-06-10", nil, "2026-06-01", monToFri, false, 15, "3068.18"},
		{"a leaver on Mon 15 June", []payRec{{From: d("2026-01-05")}}, "2025-01-01", ptr("2026-06-15"), "2026-06-01", monToFri, true, 11, "2250.00"},
		{"a joiner and a leaver in one month, the leaving day a Saturday", []payRec{{From: d("2026-01-05")}}, "2026-06-10", ptr("2026-06-20"), "2026-06-01", monToFri, true, 8, "1636.36"},
		{"a pay change on 16 June, the first record", []payRec{{From: d("2026-01-05"), To: ptr("2026-06-16")}}, "2025-01-01", nil, "2026-06-01", monToFri, true, 11, "2250.00"},
		{"a pay change on 16 June, the second record", []payRec{{From: d("2026-06-16")}}, "2025-01-01", nil, "2026-06-01", monToFri, true, 11, "2250.00"},
		{"a record that starts after the leaving day", []payRec{{From: d("2026-07-01")}}, "2025-01-01", ptr("2026-06-30"), "2026-06-01", monToFri, true, 0, "0"},
		{"the last working day of a 23-day July", []payRec{{From: d("2026-01-05")}}, "2026-07-31", nil, "2026-07-01", monToFri, true, 1, "195.65"},
		{"a leap February in full (21 working days)", []payRec{{From: d("2027-01-05")}}, "2025-01-01", ptr("2028-02-29"), "2028-02-01", monToFri, true, 21, "4500.00"},
		{"the first day of the month only", []payRec{{From: d("2026-01-05")}}, "2025-01-01", ptr("2026-06-01"), "2026-06-01", monToFri, true, 1, "204.55"},
		{"a six-day week, joiner on 10 June: 18 of 26", []payRec{{From: d("2026-01-05")}}, "2026-06-10", nil, "2026-06-01", monToSat, true, 18, "3115.38"},
		{"a joiner on a Saturday in a five-day week: two working days remain", []payRec{{From: d("2026-01-05")}}, "2026-06-27", nil, "2026-06-01", monToFri, true, 2, "409.09"},
		{"a joiner on a rest day with nothing after it", []payRec{{From: d("2026-01-05")}}, "2026-06-06", ptr("2026-06-07"), "2026-06-01", monToFri, true, 0, "0"},
		{"a public holiday counts: joiner on Wed 5 Aug 2026, National Day on Mon 10 Aug", []payRec{{From: d("2026-01-05")}}, "2026-08-05", nil, "2026-08-01", monToFri, true, 19, "4071.43"},
	} {
		total, pay := 0, apd.New(0, 0)
		works := worksOn(tc.pat, tc.set)
		den := payroll.WorkingDays(works, d(tc.month), monthEnd(d(tc.month)))
		for _, r := range tc.recs {
			if from, to, ok := segmentSpan(r, d(tc.start), tc.leaving, d(tc.month)); ok {
				if n := payroll.WorkingDays(works, from, to); n > 0 {
					total += n
					pay = addDec(pay, payroll.Ratio(basic, n, den))
				}
			}
		}
		if total != tc.days || pay.Text('f') != tc.pay {
			t.Errorf("%s: %d days, pay %s; want %d and %s", tc.name, total, pay.Text('f'), tc.days, tc.pay)
		}
	}
	// With no pattern set the days are Monday to Friday, as the leave counts read them.
	if !worksOn(leave.Pattern{}, false)(time.Friday) || worksOn(leave.Pattern{}, false)(time.Saturday) || worksOn(leave.Pattern{}, false)(time.Sunday) {
		t.Error("the default pattern")
	}
	// The two records of a pay change on 16 June cover the whole month between them.
	first, _, _ := segmentSpan(payRec{From: d("2026-01-05"), To: ptr("2026-06-16")}, d("2025-01-01"), nil, d("2026-06-01"))
	_, endFirst, _ := segmentSpan(payRec{From: d("2026-01-05"), To: ptr("2026-06-16")}, d("2025-01-01"), nil, d("2026-06-01"))
	startSecond, _, _ := segmentSpan(payRec{From: d("2026-06-16")}, d("2025-01-01"), nil, d("2026-06-01"))
	if !first.Equal(d("2026-06-01")) || !endFirst.Equal(d("2026-06-15")) || !startSecond.Equal(d("2026-06-16")) {
		t.Errorf("the spans of a pay change: %s to %s, then from %s", first, endFirst, startSecond)
	}
}
