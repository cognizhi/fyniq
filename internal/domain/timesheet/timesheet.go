// Package timesheet is a person's time on projects (PRD-001 FR-PRJ-02, FR-PRJ-05, FR-PRJ-07): whole minutes by project, task
// and day, a week at a time, the timer, the days nobody entered, and who may decide a week. Time is a management view and
// posts nothing (FR-PRJ-12). Every problem comes back together (ENG-22), in plain words with its code (ENG-23).
package timesheet

import (
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The error codes (ENG-23).
const (
	CodeMinutes  = "TIME-01" // minutes that are not whole, or beyond a day, or none
	CodeDay      = "TIME-02" // a day outside the week, or a week that does not start on a Monday
	CodeProject  = "TIME-03" // a project the person is not on, or that is closed
	CodeTask     = "TIME-04" // a task that is not the project's, or is out of use
	CodeBillable = "TIME-05" // billable time on an internal project
	CodeLocked   = "TIME-06" // an entry that is submitted or approved
	CodeTimer    = "TIME-07" // a timer that runs already, runs too long or is not running
	CodeDecide   = "TIME-08" // a week that its decider may not decide
	CodeSubmit   = "TIME-09" // a week with nothing to submit
	CodeReverse  = "TIME-10" // an entry that cannot be reversed
)

// MaxMinutes is the most one entry holds: a whole day.
const MaxMinutes = 1440

// Problem is what is wrong with a field, and how to fix it.
type Problem struct {
	Code    string
	Field   string
	Message string
	Fix     string
}

// Week is the Monday of the ISO week a day falls in.
func Week(day time.Time) time.Time {
	back := (int(day.Weekday()) + 6) % 7
	return time.Date(day.Year(), day.Month(), day.Day()-back, 0, 0, 0, 0, time.UTC)
}

// Days are the seven days of a week from its Monday.
func Days(week time.Time) []time.Time {
	out := make([]time.Time, 7)
	for i := range out {
		out[i] = week.AddDate(0, 0, i)
	}
	return out
}

// ProjectFact is what a check of an entry needs of a project.
type ProjectFact struct {
	Name       string
	Status     string // planned, active, on_hold, closed
	Billing    string // time_and_materials, fixed_fee, retainer, internal
	Member     bool   // the person is an active member or manager of it
	Configured bool
}

// TaskFact is what a check needs of a task.
type TaskFact struct {
	ProjectID uuid.UUID
	Name      string
	Active    bool
	Billable  bool // billable by default
}

// Know is what the check of a week needs of the organisation: its projects and tasks.
type Know struct {
	Projects map[uuid.UUID]ProjectFact
	Tasks    map[uuid.UUID]TaskFact
}

// Entry is an entry of a saved week as entered: the ID of one that exists, else new.
type Entry struct {
	ID        uuid.UUID // uuid.Nil for a new entry
	ProjectID uuid.UUID
	TaskID    uuid.UUID
	Day       time.Time
	Minutes   int
	Billable  bool
	Note      string
}

// CheckWeek checks the entries of a person's week as entered, every problem at once, field i naming the entry. A closed
// project takes no new time, an entry stays on its own project and task or one of that project's in use, and time on an
// internal project is not billable.
func CheckWeek(entries []Entry, week time.Time, k Know, existing map[uuid.UUID]storage.TimeEntry) []Problem {
	var probs []Problem
	add := func(i int, code, field, msg, fix string) {
		probs = append(probs, Problem{code, fmt.Sprintf("body.entries[%d].%s", i, field), msg, fix})
	}
	if week.IsZero() || week.Weekday() != time.Monday {
		probs = append(probs, Problem{CodeDay, "path.week_start", "is not a Monday", "Use the first day of the week, a Monday, such as 2026-11-16"})
	}
	last := week.AddDate(0, 0, 6)
	for i, e := range entries {
		old, had := existing[e.ID]
		if e.ID != uuid.Nil && !had {
			add(i, CodeLocked, "id", "is not a draft or returned entry of this week", "Reload the week and change only entries still open")
			continue
		}
		if had && old.Status != storage.TimeDraft && old.Status != storage.TimeReturned {
			add(i, CodeLocked, "id", "is submitted or approved, so it cannot change", "Correct an approved entry with a reversing entry, and enter the right time as a new one")
			continue
		}
		if e.Minutes <= 0 || e.Minutes > MaxMinutes {
			add(i, CodeMinutes, "minutes", fmt.Sprintf("%d minutes is not between 1 and %d", e.Minutes, MaxMinutes), "Enter the time as whole minutes, up to a day")
		}
		if e.Day.Before(week) || e.Day.After(last) {
			add(i, CodeDay, "work_date", "is not a day of this week", "Choose a day from "+week.Format(time.DateOnly)+" to "+last.Format(time.DateOnly))
		}
		p, ok := k.Projects[e.ProjectID]
		switch {
		case !ok || !p.Configured:
			add(i, CodeProject, "project_id", "is not a project you can log time on", "Choose a project you are on")
			continue
		case !p.Member:
			add(i, CodeProject, "project_id", fmt.Sprintf("you are not on %s", p.Name), "Ask a manager of the project to add you")
			continue
		case p.Status == storage.ProjectClosed && !had:
			add(i, CodeProject, "project_id", fmt.Sprintf("%s is closed", p.Name), "Choose an open project, or ask for it to be reopened")
			continue
		}
		t, ok := k.Tasks[e.TaskID]
		switch {
		case !ok || t.ProjectID != e.ProjectID:
			add(i, CodeTask, "task_id", fmt.Sprintf("is not a task of %s", p.Name), "Choose one of the project's tasks")
		case !t.Active && !(had && old.TaskID == e.TaskID):
			add(i, CodeTask, "task_id", fmt.Sprintf("%s is out of use", t.Name), "Choose a task in use")
		}
		if had && (old.ProjectID != e.ProjectID) {
			add(i, CodeProject, "project_id", "cannot change: an entry stays on its project", "Remove the entry and add it on the other project")
		}
		if e.Billable && p.Billing == storage.BillingInternal {
			add(i, CodeBillable, "billable", fmt.Sprintf("%s is an internal project", p.Name), "Time on an internal project is not billable")
		}
	}
	return probs
}

// TimerMinutes is the time a timer ran as whole minutes: the nearest minute, half up, and at least one (RND-TIME). A timer
// that ran past a day is refused, because an entry is a day's time at most.
func TimerMinutes(from, to time.Time) (int, *Problem) {
	d := to.Sub(from)
	if d < 0 {
		return 0, &Problem{CodeTimer, "timer", "was started after it was stopped", "Start the timer again"}
	}
	m := int(math.Floor(d.Minutes() + 0.5))
	m = max(m, 1)
	if m > MaxMinutes {
		return 0, &Problem{CodeTimer, "timer", fmt.Sprintf("ran for %d hours, more than a day", m/60), "Drop the timer and enter the day's time by hand"}
	}
	return m, nil
}

// Missing are the working days of a week up to today on which a person entered no time: the weekdays, less the public
// holidays (holidays by day, from the statutory calendar's pack), on which the net minutes of their entries are not above
// zero. A person's working pattern and leave join with FR-PPL-07.
func Missing(entries []storage.TimeEntry, week, today time.Time, holidays map[time.Time]string) []time.Time {
	net := map[time.Time]int{}
	for _, e := range entries {
		net[e.WorkDate] += e.Minutes
	}
	var out []time.Time
	for _, d := range Days(week)[:5] {
		if d.After(today) {
			break
		}
		if _, off := holidays[d]; off {
			continue
		}
		if net[d] <= 0 {
			out = append(out, d)
		}
	}
	return out
}

// MayDecide says whether a person may decide a week: they are not the person, they hold A on "Timesheets and project time"
// (the Approver or the Controller), and they manage the project or are the Controller (design D4). Naming a manager gives
// no right, so a manager without A is refused, and one who holds A only as the Approver is refused for another project.
func MayDecide(decider, person uuid.UUID, holdsA, manages, controller bool) *Problem {
	switch {
	case decider == person:
		return &Problem{CodeDecide, "approval", "nobody decides their own time", "Another manager of the project, or the Controller, decides it"}
	case !holdsA:
		return &Problem{CodeDecide, "approval", "your roles do not approve time", "The Approver or the Controller approves time: ask an Administrator for the role"}
	case !manages && !controller:
		return &Problem{CodeDecide, "approval", "you do not manage this project", "Only a manager of the project, or the Controller, decides its time"}
	}
	return nil
}

// Net sums the minutes of entries: a reversing entry takes away what it reverses.
func Net(entries []storage.TimeEntry) int {
	total := 0
	for _, e := range entries {
		total += e.Minutes
	}
	return total
}
