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

// A template is whole: a name, one to sixty tasks, each with a title, one of four assignees and an offset within range; the standard template passes and its tasks are due from the start date; a joiner's
// task is decided by the joiner, a manager's by the manager and any by the HR team (NM-ONB-01).
// req: NM-ONB-01
func TestATemplateIsWholeAndItsTasksAreDueFromTheStartDate(t *testing.T) {
	if err := onboarding.CheckTemplate("Office joiner", onboarding.Standard()); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		name  string
		tasks []onboarding.Task
	}{
		"no name":       {" ", onboarding.Standard()},
		"no tasks":      {"x", nil},
		"unknown who":   {"x", []onboarding.Task{{Title: "a", Assignee: "boss"}}},
		"no title":      {"x", []onboarding.Task{{Title: " ", Assignee: "hr"}}},
		"too early":     {"x", []onboarding.Task{{Title: "a", Assignee: "hr", Offset: -61}}},
		"too late":      {"x", []onboarding.Task{{Title: "a", Assignee: "hr", Offset: 366}}},
		"detail length": {"x", []onboarding.Task{{Title: "a", Assignee: "hr", Detail: strings.Repeat("x", 1001)}}},
	} {
		if err := onboarding.CheckTemplate(tc.name, tc.tasks); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	start := d("2026-12-01")
	kets := onboarding.Task{}
	for _, task := range onboarding.Standard() {
		if strings.Contains(task.Title, "KETs") {
			kets = task
		}
	}
	if got := onboarding.DueOn(start, kets.Offset); got != d("2026-12-15") {
		t.Errorf("the KETs are due %v: they are due within 14 days of starting", got)
	}
	if got := onboarding.DueOn(start, -3); got != d("2026-11-28") {
		t.Errorf("three days before: %v", got)
	}
	for _, tc := range []struct {
		assignee string
		who      onboarding.Who
		want     bool
	}{
		{"joiner", onboarding.Who{Joiner: true}, true}, {"joiner", onboarding.Who{Manager: true, IT: true}, false}, {"manager", onboarding.Who{Manager: true}, true},
		{"manager", onboarding.Who{Joiner: true}, false}, {"it", onboarding.Who{IT: true}, true}, {"it", onboarding.Who{Manager: true, Joiner: true}, false},
		{"hr", onboarding.Who{HR: true}, true}, {"hr", onboarding.Who{IT: true, Manager: true, Joiner: true}, false}, {"joiner", onboarding.Who{HR: true}, true},
	} {
		if got := onboarding.CanDecide(tc.assignee, tc.who); got != tc.want {
			t.Errorf("%s by %+v: %v", tc.assignee, tc.who, got)
		}
	}
	if !onboarding.Overdue("open", d("2026-12-01"), d("2026-12-02")) || onboarding.Overdue("done", d("2026-12-01"), d("2026-12-02")) || onboarding.Overdue("open", d("2026-12-01"), d("2026-12-01")) {
		t.Error("overdue is an open task past its due date")
	}
}
