package checks

import (
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/nemura/packs"
)

func d(s string) time.Time { t, _ := time.Parse(time.DateOnly, s); return t }

// Each message of the checks is triggered by a fixture: a pass that expires within 30 days or has expired with no renewal or leaving date is ✖, one that is
// renewed, belongs to someone leaving or is far off is not; next year's calendar is warned about from 1 December; an unverified value in use with no
// decision is ✖ and a decision or verification clears it (N.1 #3, #8, #13).
// req: NM-PPL-03, NM-PACK-05, NM-PACK-07
func TestEachPeopleCheckIsTriggeredByAFixture(t *testing.T) {
	on := d("2026-12-01")
	leaving := d("2027-01-15")
	got := WorkPasses([]Pass{
		{Person: "a", Type: "employment_pass", Expires: d("2026-12-20")},                     // within 30 days
		{Person: "b", Type: "s_pass", Expires: d("2026-11-01")},                              // expired
		{Person: "c", Type: "employment_pass", Expires: d("2026-12-20"), RenewedLater: true}, // renewed
		{Person: "d", Type: "work_permit", Expires: d("2026-12-20"), LeavingDate: &leaving},  // leaving
		{Person: "e", Type: "employment_pass", Expires: d("2027-06-01")},                     // far off
		{Person: "f", Type: "employment_pass", Expires: d("2026-12-31")},                     // the 30th day
	}, on)
	if len(got) != 3 || got[0].Subject != "a" || got[1].Subject != "b" || got[2].Subject != "f" || got[0].Status != Fail || !strings.Contains(got[1].Message, "expired") {
		t.Fatalf("work passes: %+v", got)
	}
	v, err := pack.Load(packs.Files(), "sg-people/2026.1")
	if err != nil {
		t.Fatal(err)
	}
	if got := Calendar(v, d("2026-11-30")); len(got) != 0 {
		t.Errorf("before 1 December: %+v", got)
	}
	if got := Calendar(v, d("2026-12-01")); len(got) != 0 {
		t.Errorf("2027's calendar is loaded: %+v", got)
	}
	if got := Calendar(v, d("2027-12-01")); len(got) != 1 || got[0].Check != 8 || !strings.Contains(got[0].Message, "2028") {
		t.Errorf("2028's calendar is missing: %+v", got)
	}
	used := []pack.Ref{{Table: "annual_leave", Key: "1", Status: pack.Researched}, {Table: "timing", Key: "x", Status: pack.Verified}}
	if got := UnverifiedInUse(v.ID, used, nil); len(got) != 1 || got[0].Check != 13 || got[0].Status != Fail {
		t.Errorf("unverified in use: %+v", got)
	}
	if got := UnverifiedInUse(v.ID, used, []Decision{{v.ID, "annual_leave", "1"}}); len(got) != 0 {
		t.Errorf("a decision clears it: %+v", got)
	}
	if got := UnverifiedInUse(v.ID, used, []Decision{{"sg-people@2026.2", "annual_leave", "1"}}); len(got) != 1 {
		t.Errorf("a decision for another version does not: %+v", got)
	}
}
