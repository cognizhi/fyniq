package onboarding

import (
	"slices"
	"strings"
	"time"
)

// Onboarding plans (PRD-002 NM-ONB-01): a template is a list of tasks for HR, the manager, IT and the new joiner, each due a number of days from the start date. The rules here are pure; the adapter
// stores them and the database holds the same shape as a backstop.

// Who a task is for.
const (
	ForHR      = "hr"
	ForManager = "manager"
	ForIT      = "it"
	ForJoiner  = "joiner"
)

// Assignees are the four kinds of assignee, in the order the page lists them.
var Assignees = []string{ForHR, ForManager, ForIT, ForJoiner}

// Bounds of a template.
const (
	MaxTasks    = 60
	MinOffset   = -60 // days before the start date: a joiner is often prepared for two months ahead
	MaxOffset   = 365
	MaxTitle    = 200
	MaxDetail   = 1000
	MaxName     = 100
	MaxTaskNote = 300
	TaskOpen    = "open"
	TaskDone    = "done"
	TaskSkipped = "skipped"
)

// Task is one task of a template or a plan, before its due date is worked out.
type Task struct {
	Title    string
	Detail   string
	Assignee string
	Offset   int    // days from the start date; negative before it
	Closes   string // the flow that closes the task by itself, or empty: ClosesKETs or ClosesInvitation (rev 0.75)
}

// The flows that close a task by themselves (PRD-002 NM-ONB-01, rev 0.75): issuing the person's key employment terms, and inviting the person to Fyniq. A task that closes by its flow is not marked done by
// hand (it can be skipped when it does not apply), so a plan never says done for what did not happen.
const (
	ClosesKETs       = "kets"
	ClosesInvitation = "fyniq_invitation"
)

// ClosesFlows are the flows a task may close by.
var ClosesFlows = []string{ClosesKETs, ClosesInvitation}

// Standard is the company's starting template, from the tasks NM-ONB-01 names: key employment terms within 14 days of starting (the Employment Act's rule, PRD-002 Appendix A), bank and CPF details
// before the first pay run, pass collection, equipment, accounts and introductions. The offsets are a starting point a company changes by making its own template [owner to confirm].
func Standard() []Task {
	return []Task{
		{"Send the offer documents and a welcome note", "Confirm the start date, the first-day details and who the joiner meets.", ForHR, -7, ""},
		{"Prepare the laptop and equipment", "Order, set up and label what the role needs.", ForIT, -3, ""},
		{"Create the accounts", "Email, chat and the tools the role needs, each with its owner.", ForIT, -2, ""},
		{"Collect the pass, if the joiner needs one", "Employment Pass or S Pass: note the date it is collected and keep the card's expiry.", ForHR, 0, ""},
		{"Introductions and the first week", "Introduce the joiner to the team, and agree the first week's plan.", ForManager, 1, ""},
		{"Add your bank account and CPF details", "Add them under My details before the first pay run, so the first pay is right.", ForJoiner, 5, ""},
		{"Issue the key employment terms (KETs)", "Within 14 days of the start date for employees the Employment Act covers. This task closes by itself when the KETs letter is issued.", ForHR, 14, ClosesKETs},
		{"First check-in", "A conversation about how the first weeks went and what is missing.", ForManager, 30, ""},
	}
}

// CheckTemplate refuses a template that is not whole, naming the field.
func CheckTemplate(name string, tasks []Task) error {
	if n := len([]rune(strings.TrimSpace(name))); n < 1 || n > MaxName {
		return invalid("The name of a template is 1 to %d characters: say what it is for, such as Office joiner.", MaxName)
	}
	if len(tasks) == 0 || len(tasks) > MaxTasks {
		return invalid("A template has 1 to %d tasks: add at least one.", MaxTasks)
	}
	for i, t := range tasks {
		if err := CheckTask(t); err != nil {
			return invalid("Task %d: %s", i+1, err.(*Refusal).Message)
		}
	}
	return nil
}

// CheckTask refuses a task that is not whole.
func CheckTask(t Task) error {
	if n := len([]rune(strings.TrimSpace(t.Title))); n < 1 || n > MaxTitle {
		return invalid("The title is 1 to %d characters.", MaxTitle)
	}
	if len([]rune(t.Detail)) > MaxDetail {
		return invalid("The detail is at most %d characters.", MaxDetail)
	}
	known := false
	for _, a := range Assignees {
		known = known || a == t.Assignee
	}
	if !known {
		return invalid("Choose who it is for: hr, manager, it or joiner.")
	}
	if t.Closes != "" && !slices.Contains(ClosesFlows, t.Closes) {
		return invalid("A task closes by the KETs letter or the invitation to Fyniq: choose one, or leave it empty.")
	}
	if t.Offset < MinOffset || t.Offset > MaxOffset {
		return invalid("The due date is %d days before to %d days after the start date.", -MinOffset, MaxOffset)
	}
	return nil
}

// DueOn is the day a task is due: the start date and the offset.
func DueOn(start time.Time, offset int) time.Time { return start.AddDate(0, 0, offset) }

// Who is asking to see or decide a task.
type Who struct {
	Joiner  bool // the plan is theirs
	Manager bool // they are the joiner's line manager today
	IT      bool // an Administrator, an HR administrator or the Owner
	HR      bool // an HR officer, an HR administrator or the Owner
}

// CanDecide reports whether the person may mark a task of the assignee kind done or skipped: the person it is for, and the HR team for any task (they run the plan and may step in).
func CanDecide(assignee string, w Who) bool {
	if w.HR {
		return true
	}
	switch assignee {
	case ForJoiner:
		return w.Joiner
	case ForManager:
		return w.Manager
	case ForIT:
		return w.IT
	}
	return false
}

// Overdue reports whether an open task is past its due date on the day.
func Overdue(state string, due, day time.Time) bool { return state == TaskOpen && due.Before(day) }
