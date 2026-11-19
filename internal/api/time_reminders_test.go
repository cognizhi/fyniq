//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/notify/reminders"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
)

// FR-PRJ-05, FR-NTF-01 (p1-projects task 2.6): on a Friday the worker tells each person on an active project of the weekdays of the week with
// no time entered, once; on any other day, or while Projects and time are switched off, it tells no one.
// req: FR-PRJ-05
// req: FR-NTF-01
func TestOnFridayEachPersonIsToldOfTheDaysWithNoTime(t *testing.T) {
	e := newEnv(t)
	e.projectsOn()
	acc := e.as(authz.Accountant)
	wei := e.joinedIn(e.books.Org, "Wei Remind", "approver")
	priya := e.joinedIn(e.books.Org, "Priya Remind", "employee")
	sam := e.joinedIn(e.books.Org, "Sam Remind", "employee")
	p := e.call(http.MethodPost, "/v1/projects", acc, map[string]any{"code": "REM-1", "name": "Remind", "currency": "SGD", "billing_method": "time_and_materials",
		"people": []map[string]any{{"user_id": wei.String(), "manager": true}, {"user_id": priya.String()}, {"user_id": sam.String()}}})
	if p.Status != 201 {
		t.Fatalf("the project: %d %s", p.Status, p.Raw)
	}
	pid := p.str("id")
	task := e.call(http.MethodPost, "/v1/project-tasks", acc, map[string]any{"project_id": pid, "name": "Work"}).str("id")
	// Priya has entered Wednesday 14 October 2026; the others nothing.
	tok := e.person(priya, authz.Employee)
	if r := e.saveWeek(tok, "2026-10-12", map[string]any{"project_id": pid, "task_id": task, "work_date": "2026-10-14", "minutes": 120}); r.Status != 200 {
		t.Fatalf("saving the week: %d %s", r.Status, r.Raw)
	}
	worker, err := postgres.Open(context.Background(), db.URL("fyniq_worker"))
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	remind := func(org uuid.UUID, on time.Time) int {
		t.Helper()
		d := outbox.Dispatcher{Store: worker, Queue: local.NewMemory(), Clock: clock.NewFrozen(on)}
		n, err := reminders.TimeMissingIn(context.Background(), worker, d, org)
		if err != nil {
			t.Fatalf("the reminders on %s: %v", on.Format(time.DateOnly), err)
		}
		return n
	}
	// Thursday 15 October: nothing. Friday 16 October at 16:00 Singapore time: Wei, Priya and Sam each.
	if n := remind(e.books.Org, porttest.Day(10, 15).Add(8*time.Hour)); n != 0 {
		t.Errorf("on a Thursday: %d notices", n)
	}
	if n := remind(e.books.Org, porttest.Day(10, 16).Add(8*time.Hour)); n != 3 {
		t.Errorf("on the Friday: %d notices, want 3", n)
	}
	if n := remind(e.books.Org, porttest.Day(10, 16).Add(9*time.Hour)); n != 0 {
		t.Errorf("on the Friday again: %d notices", n)
	}
	trail := auditTrail(t, e)
	if !strings.Contains(trail, "No time is entered for Mon 12 Oct, Tue 13 Oct, Thu 15 Oct, Fri 16 Oct. Add it before you submit the week.") ||
		!strings.Contains(trail, "No time is entered for Mon 12 Oct, Tue 13 Oct, Wed 14 Oct, Thu 15 Oct, Fri 16 Oct.") {
		t.Errorf("the notices: %s", trail)
	}
	// An organisation with Projects and time off is told nothing.
	other := porttest.NewBooks(t, e.store, "Reminder off "+uuid.NewString()[:8]+" Pte Ltd").Org
	if n := remind(other, porttest.Day(10, 16).Add(8*time.Hour)); n != 0 {
		t.Errorf("with the module off: %d notices", n)
	}
}
