package onboarding_test

import (
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/nemura/onboarding"
)

func d(s string) time.Time  { t, _ := time.Parse(time.DateOnly, s); return t }
func p(s string) *time.Time { t := d(s); return &t }

// A leaving keeps the dates that start statutory clocks apart and is whole: a kind, a last day after the start, notice given by the last day, and the day employees were told
// only for a retrenchment (NM-ONB-03).
// req: NM-ONB-03
func TestALeavingKeepsItsDatesApartAndIsWhole(t *testing.T) {
	start := d("2025-03-04")
	ok := []onboarding.Leaving{
		{Kind: "resignation", NoticeGiven: p("2026-10-01"), LastDay: d("2026-11-30")},
		{Kind: "retrenchment", NoticeGiven: p("2026-10-01"), Retrenchment: p("2026-09-28"), LastDay: d("2026-11-30"), EquipmentReturned: p("2026-11-30")},
		{Kind: "end_of_contract", LastDay: d("2026-12-31")},
	}
	for _, l := range ok {
		if err := onboarding.Check(l, start); err != nil {
			t.Errorf("%+v: %v", l, err)
		}
	}
	for _, tc := range []struct {
		l    onboarding.Leaving
		want string
	}{
		{onboarding.Leaving{Kind: "quit", LastDay: d("2026-11-30")}, "kind of leaving"}, {onboarding.Leaving{Kind: "resignation"}, "last day is required"},
		{onboarding.Leaving{Kind: "resignation", LastDay: d("2025-03-01")}, "before the person started"},
		{onboarding.Leaving{Kind: "resignation", NoticeGiven: p("2026-12-01"), LastDay: d("2026-11-30")}, "after the last day"},
		{onboarding.Leaving{Kind: "resignation", Retrenchment: p("2026-10-01"), LastDay: d("2026-11-30")}, "for a retrenchment only"},
		{onboarding.Leaving{Kind: "retrenchment", Retrenchment: p("2026-12-01"), LastDay: d("2026-11-30")}, "after the last day"},
	} {
		err := onboarding.Check(tc.l, start)
		r, isRefusal := err.(*onboarding.Refusal)
		if !isRefusal || r.Code != onboarding.CodeInvalid || !strings.Contains(r.Message, tc.want) {
			t.Errorf("%+v: %v", tc.l, err)
		}
	}
	if got := onboarding.AccessNarrowsOn(d("2026-11-30")); got != d("2026-12-01") {
		t.Errorf("the account narrows on %v", got)
	}
}
