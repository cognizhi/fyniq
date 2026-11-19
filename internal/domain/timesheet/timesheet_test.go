package timesheet_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/timesheet"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func day(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

// UC-PRJ-01: a timer stopped after 2 h 15 min is 135 minutes; the nearest minute, half up, at least one, at most a day.
// req: FR-PRJ-05
// req: RND-TIME
func TestATimerIsTheNearestWholeMinute(t *testing.T) {
	start := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		ran  time.Duration
		want int
	}{{2*time.Hour + 15*time.Minute, 135}, {29 * time.Second, 1}, {90 * time.Second, 2}, {89 * time.Second, 1}, {24 * time.Hour, 1440}} {
		if got, pr := timesheet.TimerMinutes(start, start.Add(c.ran)); pr != nil || got != c.want {
			t.Errorf("%v: %d %v, want %d", c.ran, got, pr, c.want)
		}
	}
	if _, pr := timesheet.TimerMinutes(start, start.Add(25*time.Hour)); pr == nil || pr.Code != timesheet.CodeTimer {
		t.Errorf("a timer past a day: %v", pr)
	}
	if _, pr := timesheet.TimerMinutes(start, start.Add(-time.Minute)); pr == nil {
		t.Error("a timer stopped before it started")
	}
}

// A working day with no time up to today is missing, less the public holidays; a reversal takes away what it reverses (FR-PRJ-05).
// req: FR-PRJ-05
// req: FR-PRJ-12
func TestMissingDaysAreWorkingDaysWithNoTime(t *testing.T) {
	week := day("2026-11-16")
	holidays := map[time.Time]string{day("2026-11-17"): "A holiday"}
	entries := []storage.TimeEntry{{WorkDate: day("2026-11-16"), Minutes: 480}, {WorkDate: day("2026-11-19"), Minutes: 60}, {WorkDate: day("2026-11-19"), Minutes: -60, ReversesID: &uuid.UUID{}}}
	got := timesheet.Missing(entries, week, day("2026-11-20"), holidays)
	if len(got) != 3 || !got[0].Equal(day("2026-11-18")) || !got[1].Equal(day("2026-11-19")) || !got[2].Equal(day("2026-11-20")) {
		t.Errorf("missing on Wednesday, Thursday (a reversed day is empty) and Friday, not on the holiday: %v", got)
	}
	if got := timesheet.Missing(nil, week, day("2026-11-16"), nil); len(got) != 1 {
		t.Errorf("today counts, tomorrow does not: %v", got)
	}
}

// Nobody decides their own time; the Approver decides only the projects they manage and the Controller any; a role without A
// decides nothing, whoever names them a manager (design D4; FR-PRJ-07).
// req: FR-PRJ-07
// req: FR-UX-40
func TestWhoMayDecideAWeek(t *testing.T) {
	me, you := uuid.New(), uuid.New()
	for _, c := range []struct {
		name                        string
		decider, person             uuid.UUID
		holdsA, manages, controller bool
		want                        bool
	}{
		{"a manager who is an Approver", me, you, true, true, false, true},
		{"the Controller", me, you, true, false, true, true},
		{"their own week", me, me, true, true, true, false},
		{"a manager without A", me, you, false, true, false, false},
		{"an Approver of another project", me, you, true, false, false, false},
	} {
		if got := timesheet.MayDecide(c.decider, c.person, c.holdsA, c.manages, c.controller) == nil; got != c.want {
			t.Errorf("%s: allowed %v, want %v", c.name, got, c.want)
		}
	}
}

// A week is checked with every problem at once: minutes in range, a day of the week, a project the person is on and open, a
// task of it in use, no billable time on an internal project, and no change to a submitted entry (FR-PRJ-05).
// req: FR-PRJ-05
// req: ENG-22
func TestAWeekIsCheckedWithEveryProblemAtOnce(t *testing.T) {
	week := day("2026-11-16")
	proj, closed, internal, other, task, dead := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	k := timesheet.Know{
		Projects: map[uuid.UUID]timesheet.ProjectFact{
			proj:     {Name: "Atlas", Status: "active", Billing: "time_and_materials", Member: true, Configured: true},
			closed:   {Name: "Old", Status: "closed", Billing: "time_and_materials", Member: true, Configured: true},
			internal: {Name: "Tools", Status: "active", Billing: "internal", Member: true, Configured: true},
			other:    {Name: "Not mine", Status: "active", Billing: "internal", Configured: true},
		},
		Tasks: map[uuid.UUID]timesheet.TaskFact{task: {ProjectID: proj, Name: "Build", Active: true}, dead: {ProjectID: proj, Name: "Old task"}},
	}
	locked := uuid.New()
	existing := map[uuid.UUID]storage.TimeEntry{locked: {ID: locked, ProjectID: proj, Status: storage.TimeSubmitted}}
	entries := []timesheet.Entry{
		{ProjectID: proj, TaskID: task, Day: day("2026-11-17"), Minutes: 135},                    // fine
		{ProjectID: proj, TaskID: task, Day: day("2026-11-24"), Minutes: 60},                     // another week
		{ProjectID: proj, TaskID: task, Day: day("2026-11-17"), Minutes: 1500},                   // beyond a day
		{ProjectID: closed, TaskID: task, Day: day("2026-11-17"), Minutes: 60},                   // closed
		{ProjectID: other, TaskID: task, Day: day("2026-11-17"), Minutes: 60},                    // not a member
		{ProjectID: proj, TaskID: dead, Day: day("2026-11-17"), Minutes: 60},                     // out of use
		{ProjectID: internal, TaskID: task, Day: day("2026-11-17"), Minutes: 60, Billable: true}, // internal, billable, and a task of another project
		{ID: locked, ProjectID: proj, TaskID: task, Day: day("2026-11-17"), Minutes: 60},         // submitted
	}
	got := map[string]string{}
	for _, p := range timesheet.CheckWeek(entries, week, k, existing) {
		got[p.Field] = p.Code
	}
	want := map[string]string{
		"body.entries[1].work_date": timesheet.CodeDay, "body.entries[2].minutes": timesheet.CodeMinutes, "body.entries[3].project_id": timesheet.CodeProject,
		"body.entries[4].project_id": timesheet.CodeProject, "body.entries[5].task_id": timesheet.CodeTask, "body.entries[6].task_id": timesheet.CodeTask,
		"body.entries[6].billable": timesheet.CodeBillable, "body.entries[7].id": timesheet.CodeLocked,
	}
	for f, c := range want {
		if got[f] != c {
			t.Errorf("%s: %q, want %s", f, got[f], c)
		}
	}
	if _, bad := got["body.entries[0].minutes"]; bad || len(got) != len(want) {
		t.Errorf("the good entry passes and nothing else is wrong: %v", got)
	}
	if p := timesheet.CheckWeek(nil, day("2026-11-17"), k, nil); len(p) != 1 || p[0].Code != timesheet.CodeDay {
		t.Errorf("a week that does not start on a Monday: %v", p)
	}
}
