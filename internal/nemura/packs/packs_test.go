package packs

import (
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/pack"
)

// The Singapore people pack loads under the shared validator with every source declared, and every hand-worked case passes against its own tables; its
// values are researched and say so, so no calculation takes one as verified (NM-PACK-01, 02, 06, 07).
// req: NM-PACK-01, NM-PACK-02
func TestTheSingaporePeoplePackLoadsAndItsPinnedCasesPass(t *testing.T) {
	versions, err := pack.LoadAll(Files())
	if err != nil {
		t.Fatalf("the packs do not load: %v", err)
	}
	if len(versions) != 2 || versions[0].ID != "sg-people@2026.1" || versions[1].ID != "sg-people@2026.2" || versions[1].Supersedes != "sg-people@2026.1" {
		t.Fatalf("%d versions", len(versions))
	}
	for _, old := range versions[:1] { // the first version is kept as it was: it still loads and its cases pass
		for _, r := range pack.RunPinned(old) {
			if !r.Pass {
				t.Errorf("%s %s: %s", old.ID, r.Case, r.Why)
			}
		}
	}
	v := versions[1]
	cases := 0
	for _, r := range pack.RunPinned(v) {
		cases++
		if !r.Pass {
			t.Errorf("%s %s: got %q, want %q (%s)", r.Table, r.Case, r.Got, r.Want, r.Why)
		}
	}
	if cases < 33 {
		t.Errorf("only %d pinned cases", cases)
	}
	if got := pack.Unverified(v); len(got) < 40 {
		t.Errorf("a researched pack lists %d unverified values", len(got))
	}
	// A source is an official page read on a day, with its quote.
	for _, s := range v.Sources {
		if (s.Retrieved != "2026-11-19" && s.Retrieved != "2026-11-20") || s.Quote == "" || s.Kind != "official guidance" {
			t.Errorf("source %s: %+v", s.ID, s)
		}
	}
	// The next year's calendar: 2027 is loaded; 2028 is not, and the pack says so (NM-PACK-05).
	if len(pack.HolidaysIn(v, 2027)) != 11 || len(pack.HolidaysIn(v, 2028)) != 0 {
		t.Errorf("calendar: 2027 %d, 2028 %d", len(pack.HolidaysIn(v, 2027)), len(pack.HolidaysIn(v, 2028)))
	}
	if _, _, ok := pack.RetirementAges(v, time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)); !ok {
		t.Error("no retirement age for a person born in 1980")
	}
}

// A planted wrong value fails its pinned case, an unknown status and a re-employment age below the retirement age are refused, so a wrong table cannot
// pass (ENG-32).
// req: NM-PACK-02
func TestAWrongValueOrStatusIsRefused(t *testing.T) {
	v, err := pack.Load(Files(), "sg-people/2026.2")
	if err != nil {
		t.Fatal(err)
	}
	bad := v
	bad.Tables = map[string]pack.Table{}
	for k, tb := range v.Tables {
		bad.Tables[k] = tb
	}
	al := bad.Tables["annual_leave"]
	rows := make([][]string, len(al.Rows))
	for i, r := range al.Rows {
		rows[i] = append([]string(nil), r...)
	}
	rows[0][1] = "9" // 9 days in the first year
	al.Rows = rows
	bad.Tables["annual_leave"] = al
	failed := 0
	for _, r := range pack.RunPinned(bad) {
		if !r.Pass {
			failed++
		}
	}
	if failed == 0 {
		t.Error("a wrong table passed its pinned cases")
	}
}

// The government-paid parental leave schemes are read by the birth date of the child: maternity's split between the employer and the Government, paternity's
// four weeks from 1 April 2025, and shared parental leave's six weeks then ten from 1 April 2026; a scheme's cap and claim window come with it (NM-LV-14).
// req: NM-PACK-06
func TestParentalLeaveSchemesAreKeyedByTheChildsBirthDate(t *testing.T) {
	v, err := pack.Load(Files(), "sg-people/2026.2")
	if err != nil {
		t.Fatal(err)
	}
	d := func(s string) time.Time { x, _ := pack.ParseDate(s); return x }
	got, ok := pack.ParentalLeave(v, "maternity_citizen_1st_2nd", d("2026-06-01"))
	if !ok || got.TotalWeeks != 16 || got.EmployerWeeks != 8 || got.GovernmentWeeks != 8 || got.CapPerPeriod != 10000 || got.CapPeriodWeeks != 4 || got.CapTotal != 20000 {
		t.Errorf("maternity: %+v", got)
	}
	if got, ok := pack.ParentalLeave(v, "paternity_citizen", d("2025-04-01")); !ok || got.TotalWeeks != 4 || got.CapPerPeriod != 2500 || got.CapTotal != 10000 || got.ClaimMonths != 3 {
		t.Errorf("paternity: %+v %v", got, ok)
	}
	for born, want := range map[string]string{"2025-04-01": "shared_parental_6_weeks", "2026-03-31": "shared_parental_6_weeks", "2026-04-01": "shared_parental_10_weeks"} {
		six, ok6 := pack.ParentalLeave(v, "shared_parental_6_weeks", d(born))
		ten, ok10 := pack.ParentalLeave(v, "shared_parental_10_weeks", d(born))
		if (want == "shared_parental_6_weeks") != ok6 || (want == "shared_parental_10_weeks") != ok10 || (ok6 && six.TotalWeeks != 6) || (ok10 && ten.TotalWeeks != 10) {
			t.Errorf("born %s: 6 weeks %v, 10 weeks %v", born, ok6, ok10)
		}
	}
	// A figure the page does not state is -1, never a guess; the first version holds no such table.
	if got, _ := pack.ParentalLeave(v, "maternity_employment_act", d("2026-06-01")); got.EmployerWeeks != -1 || got.TotalWeeks != 12 {
		t.Errorf("the Employment Act's 12 weeks: %+v", got)
	}
	first, err := pack.Load(Files(), "sg-people/2026.1")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := pack.ParentalLeave(first, "paternity_citizen", d("2026-06-01")); ok {
		t.Error("the first version holds the parental leave table")
	}
}
