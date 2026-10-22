package closing

import (
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A template's tasks are trimmed and keyed from their titles, and every rule they break is said at once: one to 40
// tasks, a title of up to 200 characters, a detail of up to 500, an owner among the roles that work on the close, a
// due day from 0 to 60, and a key of its own. The recommended template meets them.
// req: FR-GL-07
func TestTheChecklistTemplateRules(t *testing.T) {
	if _, probs := CheckTemplate(Recommended); len(probs) != 0 {
		t.Fatalf("the recommended template: %+v", probs)
	}
	got, probs := CheckTemplate([]Task{
		{Title: "  Bank reconciled  ", DueDays: 7},
		{Title: "Bank reconciled", OwnerRole: "controller"},
		{Title: "Year-end stock count, if any!", Detail: " counted ", DueDays: 60},
		{Key: "docs", Title: "Documents"},
	})
	if len(probs) != 0 || got[0].Key != "bank-reconciled" || got[1].Key != "bank-reconciled-2" || got[0].Title != "Bank reconciled" ||
		got[2].Key != "year-end-stock-count-if-any" || got[2].Detail != "counted" || got[3].Key != "docs" {
		t.Fatalf("keys from titles: %+v %+v", got, probs)
	}
	_, probs = CheckTemplate([]Task{
		{Title: " "},
		{Title: "x", Detail: strings.Repeat("d", 501), OwnerRole: "employee", DueDays: 61},
		{Key: "Bad Key", Title: "y"},
		{Key: "same", Title: "a"},
		{Key: "same", Title: "b", DueDays: -1},
		{Title: strings.Repeat("t", 201)},
	})
	var fields []string
	for _, p := range probs {
		if p.Code != CodeTemplate {
			t.Errorf("code %s", p.Code)
		}
		fields = append(fields, p.Field)
	}
	want := "tasks[0].title tasks[1].detail tasks[1].owner_role tasks[1].due_days tasks[2].key tasks[4].due_days tasks[4].key tasks[5].title"
	if strings.Join(fields, " ") != want {
		t.Errorf("the problems: %v", fields)
	}
	if _, probs := CheckTemplate(nil); len(probs) != 1 || probs[0].Field != "tasks" {
		t.Errorf("no tasks: %+v", probs)
	}
	many := make([]Task, MaxTasks+1)
	for i := range many {
		many[i] = Task{Title: "t"}
	}
	if _, probs := CheckTemplate(many); len(probs) != 1 || probs[0].Field != "tasks" {
		t.Errorf("too many tasks: %+v", probs)
	}
	if k := keyOf(strings.Repeat("abcde ", 12)); len(k) > 40 || strings.HasSuffix(k, "-") {
		t.Errorf("a long key: %q", k)
	}
	if k := keyOf("!!!"); k != "task" {
		t.Errorf("a key from no letters: %q", k)
	}
}

// The close's check of the checklist: ✖ while a task is neither done nor skipped (Appendix C.2 #58).
// req: FR-GL-07
// req: C.2 #58
func TestTheChecklistCheck(t *testing.T) {
	c := Checklist{Template: Template{Version: 3}, Items: []Item{{State: storage.ChecklistDone}, {State: storage.ChecklistOpen}, {State: storage.ChecklistSkipped}}}
	if k := checklistCheck(c); k.Key != "checklist" || k.Severity != Failed ||
		k.Detail != "1 of 3 checklist task(s) are neither done nor skipped with a reason: do them, or skip one that does not apply this month, then run the checks again" {
		t.Errorf("an open task: %+v", k)
	}
	c.Items[1].State = storage.ChecklistDone
	if k := checklistCheck(c); k.Severity != OK || k.Detail != "2 done, 1 skipped, on version 3 of the template" {
		t.Errorf("every task done or skipped: %+v", k)
	}
}
