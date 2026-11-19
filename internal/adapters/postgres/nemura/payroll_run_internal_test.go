package nemura

import (
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/nemura/payroll"
)

// A part month is counted by calendar days (NM-PAY-12, payroll.PartMonthMethod): the days a pay record covers over the days of the month, with the start and leaving days both counted, and the pay is
// basic × days ÷ days of the month rounded once to the cent (RND-PAY-RATIO). Hand-worked goldens; the working-day count MOM's page describes would give 15 of 22 days for the joiner of 10 June (3,068.18)
// and is not what the PRD says: the method is an owner decision that is pending, so these pin what it is today.
// req: NM-PAY-12, RND-PAY-RATIO
func TestAPartMonthIsCountedByCalendarDays(t *testing.T) {
	d := func(s string) time.Time { v, _ := time.Parse(time.DateOnly, s); return v }
	ptr := func(s string) *time.Time { v := d(s); return &v }
	basic := apd.New(450000, -2)
	if payroll.PartMonthMethod != "calendar_days" {
		t.Fatalf("the method: %s", payroll.PartMonthMethod)
	}
	for _, tc := range []struct {
		name    string
		recs    []payRec
		start   string
		leaving *time.Time
		month   string
		days    int
		pay     string
	}{
		{"a full month", []payRec{{From: d("2026-01-05")}}, "2025-01-01", nil, "2026-06-01", 30, "4500.00"},
		{"a joiner on 10 June", []payRec{{From: d("2026-01-05")}}, "2026-06-10", nil, "2026-06-01", 21, "3150.00"},
		{"a leaver on 15 June", []payRec{{From: d("2026-01-05")}}, "2025-01-01", ptr("2026-06-15"), "2026-06-01", 15, "2250.00"},
		{"a joiner and a leaver in one month", []payRec{{From: d("2026-01-05")}}, "2026-06-10", ptr("2026-06-20"), "2026-06-01", 11, "1650.00"},
		{"a pay change on 16 June, the first record", []payRec{{From: d("2026-01-05"), To: ptr("2026-06-16")}}, "2025-01-01", nil, "2026-06-01", 15, "2250.00"},
		{"a pay change on 16 June, the second record", []payRec{{From: d("2026-06-16")}}, "2025-01-01", nil, "2026-06-01", 15, "2250.00"},
		{"a record that starts after the leaving day", []payRec{{From: d("2026-07-01")}}, "2025-01-01", ptr("2026-06-30"), "2026-06-01", 0, "0"},
		{"the last day of a 31-day month", []payRec{{From: d("2026-01-05")}}, "2026-07-31", nil, "2026-07-01", 1, "145.16"},
		{"a leap February in full", []payRec{{From: d("2027-01-05")}}, "2025-01-01", ptr("2028-02-29"), "2028-02-01", 29, "4500.00"},
		{"the first day of the month only", []payRec{{From: d("2026-01-05")}}, "2025-01-01", ptr("2026-06-01"), "2026-06-01", 1, "150.00"},
	} {
		total, pay := 0, apd.New(0, 0)
		days := monthEnd(d(tc.month)).Day()
		for _, r := range tc.recs {
			n := segmentDays(r, d(tc.start), tc.leaving, d(tc.month))
			total += n
			if n > 0 {
				pay = addDec(pay, payroll.Ratio(basic, n, days))
			}
		}
		if tc.name == "a pay change on 16 June, the first record" {
			// the second record's days are the rest of the month: together they are the whole month
			second := segmentDays(payRec{From: d("2026-06-16")}, d(tc.start), tc.leaving, d(tc.month))
			if total+second != 30 {
				t.Errorf("the two records of a pay change cover %d days, not 30", total+second)
			}
		}
		if total != tc.days || pay.Text('f') != tc.pay {
			t.Errorf("%s: %d days, pay %s; want %d and %s", tc.name, total, pay.Text('f'), tc.days, tc.pay)
		}
	}
}
