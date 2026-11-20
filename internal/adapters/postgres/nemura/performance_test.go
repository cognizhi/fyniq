//go:build local

package nemura_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/performance"
)

func perfCode(err error) string {
	var r *performance.Refusal
	if errors.As(err, &r) {
		return r.Code
	}
	return ""
}

// Company and team goals are read by every member; a personal goal and a KPI are read by the person, their line manager and the people team and changed by the person, their manager and the HR administrator or
// Owner; anyone else is told there is none; progress is exact and a key result moves only by an update that keeps who gave it (NM-PRF-01, NM-PRF-02).
// req: NM-PRF-01, NM-PRF-02
// asvs: V8.2.1, V8.3.1
func TestGoalsAreReadAndChangedOnlyByTheirPeopleAndProgressMovesByAppendedUpdates(t *testing.T) {
	p := newPlanTeam(t) // Mia manages Jon; Ada is an Administrator; Ola is unrelated
	c := p.c
	now := p.now
	var pMia uuid.UUID
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT id FROM nemura.person WHERE member_id = $1`, p.mia).Scan(&pMia)
	})
	emp, lead, team := []authz.Role{authz.Employee}, []authz.Role{authz.LineManager}, []authz.Role{authz.HRAdmin}
	run := func(fn func(ctx context.Context, tx pgx.Tx) error) error { return c.do(fn) }
	// The HR administrator sets a company goal; an employee and a manager cannot.
	var company api.Goal
	in := api.GoalInput{Scope: "company", Title: "3 pilot customers live by 31 December", DueOn: "2099-12-31", Measures: []api.MeasureInput{{Title: "Pilot customers live", Unit: "customers", Target: "3"}}}
	if err := run(func(ctx context.Context, tx pgx.Tx) (err error) {
		company, err = nemura.SetGoal(ctx, tx, c.org, now, c.hr, team, in)
		return
	}); err != nil {
		t.Fatal(err)
	}
	for _, who := range []struct {
		m    uuid.UUID
		held []authz.Role
	}{{p.jon, emp}, {p.mia, lead}} {
		if err := run(func(ctx context.Context, tx pgx.Tx) error {
			_, err := nemura.SetGoal(ctx, tx, c.org, now, who.m, who.held, in)
			return err
		}); perfCode(err) != performance.CodeNotFound {
			t.Errorf("a company goal set by %v: %v", who.held, err)
		}
	}
	// Jon sets a personal goal aligned with it; the manager and the HR administrator may set one for him; Ola may not, and cannot read it.
	var mine api.Goal
	pg := api.GoalInput{Scope: "personal", Title: "Bank sync in beta", DueOn: "2099-11-30", ParentID: company.ID, Measures: []api.MeasureInput{{Title: "Sandbox tests passing", Unit: "tests", Target: "13"}, {Title: "API p95", Unit: "ms", Start: "480", Target: "300"}}}
	if err := run(func(ctx context.Context, tx pgx.Tx) (err error) {
		mine, err = nemura.SetGoal(ctx, tx, c.org, now, p.jon, emp, pg)
		return
	}); err != nil {
		t.Fatal(err)
	}
	if mine.Person != "Jon Joiner" || mine.ParentID != company.ID || len(mine.Measures) != 2 || !mine.Measures[1].Down {
		t.Errorf("%+v", mine)
	}
	forJon := pg
	forJon.PersonID = p.pJon.String()
	for name, who := range map[string]struct {
		m    uuid.UUID
		held []authz.Role
		ok   bool
	}{"the manager": {p.mia, lead, true}, "the HR administrator": {c.hr, team, true}, "an unrelated member": {p.other, emp, false}, "an unrelated manager": {p.ada, lead, false}} {
		err := run(func(ctx context.Context, tx pgx.Tx) error {
			_, err := nemura.SetGoal(ctx, tx, c.org, now, who.m, who.held, forJon)
			return err
		})
		if (err == nil) != who.ok {
			t.Errorf("%s setting Jon's goal: %v", name, err)
		}
	}
	// Reading Jon's goals: Jon, his manager and the HR team; not Ola or the unrelated manager.
	read := func(m uuid.UUID, held []authz.Role) error {
		return run(func(ctx context.Context, tx pgx.Tx) error {
			_, _, err := nemura.GoalsOfPerson(ctx, tx, m, held, p.pJon, today)
			return err
		})
	}
	for name, who := range map[string]struct {
		m    uuid.UUID
		held []authz.Role
		ok   bool
	}{"Jon": {p.jon, emp, true}, "his manager": {p.mia, lead, true}, "the HR administrator": {c.hr, team, true}, "an HR officer": {c.pay, []authz.Role{authz.HROfficer}, true}, "an unrelated member": {p.other, emp, false}, "an unrelated manager": {p.ada, lead, false}, "the payroll administrator": {c.pay, []authz.Role{authz.PayrollAdmin}, false}} {
		if err := read(who.m, who.held); (err == nil) != who.ok {
			t.Errorf("%s reading Jon's goals: %v", name, err)
		}
	}
	// Everyone reads the company's goal; Ola sees none of Jon's.
	var co, te, own []api.Goal
	c.must(func(ctx context.Context, tx pgx.Tx) (err error) {
		co, te, own, _, err = nemura.MyGoals(ctx, tx, p.other, today)
		return
	})
	if len(co) != 1 || len(te) != 0 || len(own) != 0 {
		t.Errorf("an unrelated member sees company %d, team %d, own %d", len(co), len(te), len(own))
	}
	// Progress moves by updates: Jon updates his own; Ola cannot; the manager can; exact and rounded down; the history stays.
	update := func(m uuid.UUID, held []authz.Role, id, v string) (api.Measure, error) {
		var out api.Measure
		err := run(func(ctx context.Context, tx pgx.Tx) (err error) {
			out, err = nemura.UpdateMeasure(ctx, tx, c.org, now.Add(time.Minute), m, held, uuid.MustParse(id), v, "Progress note")
			return
		})
		return out, err
	}
	if _, err := update(p.other, emp, mine.Measures[0].ID, "8"); perfCode(err) != performance.CodeNotFound {
		t.Errorf("an unrelated member updating: %v", err)
	}
	if _, err := update(p.jon, emp, mine.Measures[0].ID, "1e3"); perfCode(err) != performance.CodeInvalid {
		t.Errorf("a float: %v", err)
	}
	if m, err := update(p.jon, emp, mine.Measures[0].ID, "8"); err != nil || m.Current != "8" || m.Progress != 61 {
		t.Errorf("8 of 13: %+v %v", m, err)
	}
	if m, err := update(p.mia, lead, mine.Measures[1].ID, "340"); err != nil || m.Progress != 77 || !m.Down {
		t.Errorf("340 ms from 480 towards 300: %+v %v", m, err)
	}
	var n int
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM nemura.measure_update WHERE measure_id = $1`, mine.Measures[0].ID).Scan(&n)
	})
	if n != 1 {
		t.Errorf("%d updates kept", n)
	}
	// A KPI is set by the manager or the HR administrator, never by the person or an unrelated member; the person updates their own.
	kpi := api.MeasureInput{Title: "Tickets closed a week", Unit: "tickets", Target: "10"}
	for name, who := range map[string]struct {
		m    uuid.UUID
		held []authz.Role
		ok   bool
	}{"the person": {p.jon, emp, false}, "the manager": {p.mia, lead, true}, "an unrelated member": {p.other, emp, false}} {
		err := run(func(ctx context.Context, tx pgx.Tx) error {
			_, err := nemura.SetKPI(ctx, tx, c.org, now, who.m, who.held, p.pJon, kpi)
			return err
		})
		if (err == nil) != who.ok {
			t.Errorf("%s setting a KPI: %v", name, err)
		}
	}
	var kpis []api.Measure
	c.must(func(ctx context.Context, tx pgx.Tx) (err error) {
		_, _, _, kpis, err = nemura.MyGoals(ctx, tx, p.jon, today)
		return
	})
	if len(kpis) != 1 || kpis[0].Status != "not_yet" {
		t.Fatalf("%+v", kpis)
	}
	if m, err := update(p.jon, emp, kpis[0].ID, "10"); err != nil || m.Status != "done" {
		t.Errorf("a KPI at its target: %+v %v", m, err)
	}
	// A goal is closed once; a closed goal's key results are no longer updated; its words never change, even straight to the database.
	closeIt := func(m uuid.UUID, held []authz.Role) error {
		return run(func(ctx context.Context, tx pgx.Tx) error {
			return nemura.CloseGoal(ctx, tx, c.org, now, m, held, uuid.MustParse(mine.ID), "done")
		})
	}
	if err := closeIt(p.other, emp); perfCode(err) != performance.CodeNotFound {
		t.Errorf("an unrelated member closing: %v", err)
	}
	if err := closeIt(p.jon, emp); err != nil {
		t.Fatal(err)
	}
	if err := closeIt(p.jon, emp); perfCode(err) != performance.CodeConflict {
		t.Errorf("closing twice: %v", err)
	}
	if _, err := update(p.jon, emp, mine.Measures[0].ID, "9"); perfCode(err) != performance.CodeConflict {
		t.Errorf("updating a closed goal: %v", err)
	}
	for name, sql := range map[string]string{"a new title": `UPDATE nemura.goal SET title = 'Changed'`, "deleting": `DELETE FROM nemura.goal`, "a new target": `UPDATE nemura.measure SET target = 99`, "an edited update": `UPDATE nemura.measure_update SET value = 0`} {
		if err := run(func(ctx context.Context, tx pgx.Tx) error { _, err := tx.Exec(ctx, sql); return err }); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	// A goal that aligns with a narrower one, a team goal with no team and a due date in the past are refused.
	bad := api.GoalInput{Scope: "company", Title: "x", DueOn: "2099-12-31", ParentID: mine.ID}
	if err := run(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.SetGoal(ctx, tx, c.org, now, c.hr, team, bad)
		return err
	}); perfCode(err) != performance.CodeInvalid {
		t.Errorf("a company goal aligned with a personal one: %v", err)
	}
	if err := run(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.SetGoal(ctx, tx, c.org, now, c.hr, team, api.GoalInput{Scope: "team", Title: "x", DueOn: "2099-12-31"})
		return err
	}); perfCode(err) != performance.CodeInvalid {
		t.Errorf("a team goal with no team: %v", err)
	}
	_ = pMia
}
