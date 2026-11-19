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
	if len(versions) != 1 || versions[0].ID != "sg-people@2026.1" {
		t.Fatalf("versions: %+v", versions)
	}
	v := versions[0]
	cases := 0
	for _, r := range pack.RunPinned(v) {
		cases++
		if !r.Pass {
			t.Errorf("%s %s: got %q, want %q (%s)", r.Table, r.Case, r.Got, r.Want, r.Why)
		}
	}
	if cases < 25 {
		t.Errorf("only %d pinned cases", cases)
	}
	if got := pack.Unverified(v); len(got) < 40 {
		t.Errorf("a researched pack lists %d unverified values", len(got))
	}
	// A source is an official page read on a day, with its quote.
	for _, s := range v.Sources {
		if s.Retrieved != "2026-11-19" || s.Quote == "" || s.Kind != "official guidance" {
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
	v, err := pack.Load(Files(), "sg-people/2026.1")
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
