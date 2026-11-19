package policy

import (
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/nemura/packs"
)

var day = time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)

func statute(t *testing.T) Rules {
	return Rules{AnnualLeaveDays: []int{7, 8, 9, 10, 11, 12, 13, 14}, SickOutpatientDays: []int{5, 8, 11, 14}, SickHospitalDays: []int{15, 30, 45, 60},
		ChildcareCitizenDays: 6, ExtendedChildcareDays: 2, ChildcareNonCitizenDays: 2}
}

// A policy is checked against each statutory rule: the statute itself passes, less on any rule is refused with that rule named and its source, more is
// noted, a policy of the wrong shape is refused, and the values the check read are listed with their status so unverified ones can be decided on
// (NM-PACK-04, NM-PACK-08).
// req: NM-PACK-04, NM-PACK-08
func TestAPolicyIsCheckedAgainstEachStatutoryRule(t *testing.T) {
	v, err := pack.Load(packs.Files(), "sg-people/2026.1")
	if err != nil {
		t.Fatal(err)
	}
	if got := Check(v, statute(t), day); len(got.Breaches) != 0 || len(got.Notes) != 0 || len(got.Used) == 0 {
		t.Fatalf("the statute itself: %+v", got)
	}
	// Less on one rule is refused, naming it and its source.
	r := statute(t)
	r.AnnualLeaveDays = []int{7, 8, 9, 10, 11, 12, 13, 12} // 12 days in year 8
	r.SickHospitalDays = []int{15, 30, 45, 55}
	r.ChildcareCitizenDays = 5
	got := Check(v, r, day)
	if len(got.Breaches) != 3 {
		t.Fatalf("breaches: %+v", got.Breaches)
	}
	for _, b := range got.Breaches {
		if b.Source == "" || !strings.Contains(b.Message, "below the statutory") || b.Policy >= b.Statute {
			t.Errorf("a breach: %+v", b)
		}
	}
	if got.Breaches[0].Rule != "Annual leave in year 8 of service" || got.Breaches[0].Statute != 14 || got.Breaches[0].Source != "mom-annual-leave" {
		t.Errorf("the first breach: %+v", got.Breaches[0])
	}
	// More is noted, not refused; a single value repeats for later years.
	r = statute(t)
	r.AnnualLeaveDays = []int{14}
	r.ChildcareNonCitizenDays = 4
	got = Check(v, r, day)
	if len(got.Breaches) != 0 || len(got.Notes) < 2 {
		t.Errorf("more than the statute: %+v", got)
	}
	// A wrong shape is refused as a breach of the form.
	got = Check(v, Rules{}, day)
	if len(got.Breaches) < 2 {
		t.Errorf("an empty policy: %+v", got.Breaches)
	}
	// Every value read is researched in this version, and says so.
	for _, u := range Check(v, statute(t), day).Used {
		if u.Status != pack.Researched {
			t.Errorf("%s %s is %s", u.Table, u.Key, u.Status)
		}
	}
	// Before the pack is in force, nothing is compared: there is no statute to check against.
	if got := Check(v, Rules{AnnualLeaveDays: []int{1}, SickOutpatientDays: []int{0, 0, 0, 0}, SickHospitalDays: []int{0, 0, 0, 0}}, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)); len(got.Breaches) != 0 {
		t.Errorf("before the pack: %+v", got.Breaches)
	}
}
