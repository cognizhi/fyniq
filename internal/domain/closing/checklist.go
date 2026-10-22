package closing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The close checklist (PRD-001 FR-GL-07, FM-4; the founders' decisions of 22 Oct 2026, built in rev 0.68): each month's
// close has the tasks of the organisation's template, each with the role that usually does it and the day after the
// month's end it is due. A task is done, or skipped for the month with a reason; the close checks refuse the close while
// one is neither (Appendix C.2 #58). The Accountant or the Controller changes the template, as a new version; until the
// organisation saves one, the platform's recommended template applies.

// The stable error codes of the checklist (ENG-23).
const (
	CodeTemplate   = "CLOSE-11" // a template the rules refuse
	CodeNoTask     = "CLOSE-12" // no such task on the month's checklist
	CodeSkipReason = "CLOSE-13" // skipping a task needs a reason
)

// Task is one task of the close checklist.
type Task struct {
	Key       string `json:"key"`
	Title     string `json:"title"`
	Detail    string `json:"detail,omitempty"`
	OwnerRole string `json:"owner_role,omitempty"` // the role that usually does it
	DueDays   int    `json:"due_days"`             // due this many days after the month's end
}

// Recommended is the platform's template, version 0: the monthly close of a small company, which an organisation
// changes to its own.
var Recommended = []Task{
	{Key: "documents", Title: "Sales, purchases and expense claims entered", Detail: "Every invoice, bill and claim dated in the month is in Fyniq", OwnerRole: "accountant", DueDays: 5},
	{Key: "bank", Title: "Bank, card and wallet accounts reconciled", Detail: "Each balance agrees with its statement at the month's end", OwnerRole: "accountant", DueDays: 7},
	{Key: "payroll", Title: "Payroll and CPF posted", Detail: "The month's pay runs and CPF contributions are posted; skip it while there is no payroll", OwnerRole: "accountant", DueDays: 7},
	{Key: "accruals", Title: "Accruals and prepayments reviewed", Detail: "Costs incurred but not billed are accrued, and costs paid ahead are spread", OwnerRole: "accountant", DueDays: 10},
	{Key: "fx", Title: "Foreign-currency balances revalued", Detail: "Foreign-currency bank, receivable and payable balances at the month-end rate; skip it with no such balances", OwnerRole: "accountant", DueDays: 10},
	{Key: "review", Title: "The month reviewed", Detail: "The month's journals, the trial balance and the variances read through before the attestation", OwnerRole: "controller", DueDays: 12},
}

// Template is a version of the checklist template; version 0 is Recommended.
type Template struct {
	Version   int
	Tasks     []Task
	WrittenBy string     // empty for the recommended template
	WrittenAt *time.Time // nil for the recommended template
}

// The limits of a template.
const (
	MaxTasks   = 40
	MaxDueDays = 60
)

// Roles a task may name as its owner: the roles that work on the close.
var ownerRoles = []string{"owner", "controller", "accountant", "bookkeeper", "ar_specialist", "ap_specialist", "payroll_administrator", "tax_agent"}

var keyFormat = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// Problem is one rule a template breaks: its field, what is wrong and how to fix it.
type Problem struct {
	Code, Field, Message, Fix string
}

// keyOf makes a key from a title: its words in lower case, joined by hyphens.
func keyOf(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteRune('-')
		}
	}
	k := strings.Trim(b.String(), "-")
	if len(k) > 40 {
		k = strings.TrimRight(k[:40], "-")
	}
	if k == "" {
		k = "task"
	}
	return k
}

// CheckTemplate trims a template's tasks, gives a task without a key one from its title, and returns every rule it
// breaks: one to 40 tasks, each with a title of up to 200 characters, a detail of up to 500, an owner among the roles
// that work on the close, a due day from 0 to 60, and a key of its own.
func CheckTemplate(tasks []Task) ([]Task, []Problem) {
	var out []Task
	var probs []Problem
	if len(tasks) < 1 || len(tasks) > MaxTasks {
		probs = append(probs, Problem{CodeTemplate, "tasks", fmt.Sprintf("the template has %d tasks", len(tasks)), fmt.Sprintf("Keep 1 to %d tasks", MaxTasks)})
	}
	seen := map[string]bool{}
	for i, t := range tasks {
		at := func(f string) string { return fmt.Sprintf("tasks[%d].%s", i, f) }
		t.Title, t.Detail, t.Key, t.OwnerRole = strings.TrimSpace(t.Title), strings.TrimSpace(t.Detail), strings.TrimSpace(t.Key), strings.TrimSpace(t.OwnerRole)
		if n := utf8.RuneCountInString(t.Title); n < 1 || n > 200 {
			probs = append(probs, Problem{CodeTemplate, at("title"), "the title is blank or longer than 200 characters", "Say what the task is, in a line"})
		}
		if utf8.RuneCountInString(t.Detail) > 500 {
			probs = append(probs, Problem{CodeTemplate, at("detail"), "the detail is longer than 500 characters", "Shorten it"})
		}
		if t.OwnerRole != "" && !slices.Contains(ownerRoles, t.OwnerRole) {
			probs = append(probs, Problem{CodeTemplate, at("owner_role"), fmt.Sprintf("%q is not a role that works on the close", t.OwnerRole),
				"Name one of " + strings.Join(ownerRoles, ", ") + ", or none"})
		}
		if t.DueDays < 0 || t.DueDays > MaxDueDays {
			probs = append(probs, Problem{CodeTemplate, at("due_days"), fmt.Sprintf("%d days after the month's end", t.DueDays), fmt.Sprintf("Give 0 to %d days", MaxDueDays)})
		}
		if t.Key == "" {
			base, n := keyOf(t.Title), 2
			for t.Key = base; seen[t.Key]; n++ {
				t.Key = fmt.Sprintf("%s-%d", strings.TrimRight(base[:min(len(base), 37)], "-"), n)
			}
		}
		switch {
		case !keyFormat.MatchString(t.Key):
			probs = append(probs, Problem{CodeTemplate, at("key"), fmt.Sprintf("%q is not a task key", t.Key),
				"Use up to 40 lower-case letters, digits and hyphens, or leave it out for a new task"})
		case seen[t.Key]:
			probs = append(probs, Problem{CodeTemplate, at("key"), fmt.Sprintf("the key %q is used twice", t.Key), "Give each task its own key"})
		}
		seen[t.Key] = true
		out = append(out, t)
	}
	return out, probs
}

// CurrentTemplate is the organisation's latest template, or the recommended one while it has saved none.
func CurrentTemplate(ctx context.Context, u storage.UnitOfWork) (Template, error) {
	t, err := u.Closes().ChecklistTemplate(ctx)
	if errors.Is(err, ports.ErrNotFound) {
		return Template{Version: 0, Tasks: slices.Clone(Recommended)}, nil
	}
	if err != nil {
		return Template{}, err
	}
	var tasks []Task
	if err := json.Unmarshal(t.Tasks, &tasks); err != nil {
		return Template{}, fmt.Errorf("closing: checklist template version %d: %w", t.Version, err)
	}
	at := t.WrittenAt
	return Template{Version: t.Version, Tasks: tasks, WrittenBy: t.WrittenBy, WrittenAt: &at}, nil
}

// SaveTemplate records the template's next version in u, once it meets the rules.
func SaveTemplate(ctx context.Context, u storage.UnitOfWork, tasks []Task) (Template, []Problem, error) {
	checked, probs := CheckTemplate(tasks)
	if len(probs) > 0 {
		return Template{}, probs, nil
	}
	raw, err := json.Marshal(checked)
	if err != nil {
		return Template{}, nil, err
	}
	if _, err := u.Closes().SaveChecklistTemplate(ctx, raw); err != nil {
		return Template{}, nil, err
	}
	t, err := CurrentTemplate(ctx, u)
	return t, nil, err
}

// Item is one task of a month's checklist and where it stands.
type Item struct {
	Task
	State string    // done, skipped or open
	Due   time.Time // the month's end plus the task's due days
	Entry *storage.ChecklistEntry
}

// Checklist is a month's checklist: the current template's tasks and their latest entries.
type Checklist struct {
	Period   storage.Period
	Template Template
	Items    []Item
}

// Count counts the items in a state.
func (c Checklist) Count(state string) int {
	n := 0
	for _, it := range c.Items {
		if it.State == state {
			n++
		}
	}
	return n
}

// ChecklistOf is a period's checklist in u.
func ChecklistOf(ctx context.Context, u storage.UnitOfWork, periodID uuid.UUID) (Checklist, error) {
	p, err := u.Periods().Get(ctx, periodID)
	if err != nil {
		return Checklist{}, err
	}
	return checklistOf(ctx, u, p)
}

func checklistOf(ctx context.Context, u storage.UnitOfWork, p storage.Period) (Checklist, error) {
	t, err := CurrentTemplate(ctx, u)
	if err != nil {
		return Checklist{}, err
	}
	entries, err := u.Closes().ChecklistEntries(ctx, p.ID)
	if err != nil {
		return Checklist{}, err
	}
	latest := make(map[string]*storage.ChecklistEntry, len(entries))
	for i := range entries {
		latest[entries[i].TaskKey] = &entries[i]
	}
	c := Checklist{Period: p, Template: t}
	for _, task := range t.Tasks {
		it := Item{Task: task, State: storage.ChecklistOpen, Due: p.EndsOn.AddDate(0, 0, task.DueDays), Entry: latest[task.Key]}
		if it.Entry != nil {
			it.State = it.Entry.State
		}
		c.Items = append(c.Items, it)
	}
	return c, nil
}

// MarkTask records a task of an open period's checklist as done, skipped with a reason, or open again, in u.
func MarkTask(ctx context.Context, u storage.UnitOfWork, periodID uuid.UUID, key, state, note string) (Item, error) {
	p, err := u.Periods().Get(ctx, periodID)
	if err != nil {
		return Item{}, err
	}
	if p.Status != storage.PeriodOpen {
		return Item{}, &Error{CodeNotOpen, fmt.Sprintf("the period %s to %s is %s", p.StartsOn.Format(time.DateOnly), p.EndsOn.Format(time.DateOnly), p.Status),
			"Mark checklist tasks while the period is open: ask to reopen it first"}
	}
	note = strings.TrimSpace(note)
	if state != storage.ChecklistDone && state != storage.ChecklistSkipped && state != storage.ChecklistOpen {
		return Item{}, &Error{CodeNoTask, fmt.Sprintf("%q is not a state of a checklist task", state), "Mark it done, skipped or open"}
	}
	if state == storage.ChecklistSkipped && note == "" {
		return Item{}, &Error{CodeSkipReason, "a task is skipped with a reason", "Say why the task does not apply this month"}
	}
	c, err := checklistOf(ctx, u, p)
	if err != nil {
		return Item{}, err
	}
	i := slices.IndexFunc(c.Items, func(it Item) bool { return it.Key == key })
	if i < 0 {
		return Item{}, &Error{CodeNoTask, fmt.Sprintf("the checklist has no task %q", key), "Mark one of the tasks the month's checklist lists"}
	}
	e, err := u.Closes().MarkChecklistTask(ctx, storage.NewChecklistEntry{PeriodID: p.ID, TaskKey: key, TemplateVersion: c.Template.Version, State: state, Note: note})
	if err != nil {
		return Item{}, err
	}
	it := c.Items[i]
	it.State, it.Entry = state, &e
	return it, nil
}

// checklistCheck is the close's check of the checklist (Appendix C.2 #58): ✖ while a task is neither done nor skipped.
func checklistCheck(c Checklist) Check {
	open, done, skipped := c.Count(storage.ChecklistOpen), c.Count(storage.ChecklistDone), c.Count(storage.ChecklistSkipped)
	if open > 0 {
		return Check{"checklist", Failed, "Checklist tasks are not done", fmt.Sprintf(
			"%d of %d checklist task(s) are neither done nor skipped with a reason: do them, or skip one that does not apply this month, then run the checks again",
			open, len(c.Items))}
	}
	return Check{"checklist", OK, "Every checklist task is done or skipped", fmt.Sprintf("%d done, %d skipped, on version %d of the template", done, skipped, c.Template.Version)}
}
