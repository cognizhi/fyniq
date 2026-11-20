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
	"github.com/cognizhi/fyniq/internal/nemura/onboarding"
	"github.com/cognizhi/fyniq/internal/nemura/people"
)

type planTeam struct {
	c                    *company
	mia, jon, ada, other uuid.UUID // members: the manager, the joiner, an Administrator, someone unrelated
	pJon                 uuid.UUID
	now                  time.Time
}

func newPlanTeam(t *testing.T) *planTeam {
	t.Helper()
	p := &planTeam{c: newCompany(t), now: today.Add(11 * time.Hour)}
	c := p.c
	class := &people.Classification{ContractType: "service", EACovered: true, PartIVClass: "none", Residency: "citizen"}
	job := people.Job{Title: "Engineer", Department: "Engineering", Worksite: "SG", EmploymentType: "full_time"}
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		for name, dst := range map[string]*uuid.UUID{"Mia Manager": &p.mia, "Jon Joiner": &p.jon, "Ada Admin": &p.ada, "Ola Other": &p.other} {
			if *dst, err = nemura.AddMember(ctx, tx, c.org, p.now, actor, name); err != nil {
				return err
			}
		}
		if _, err = nemura.GrantRole(ctx, tx, c.org, p.now, c.owner, p.ada, authz.Administrator, today, nil); err != nil {
			return err
		}
		mm := p.mia
		pMia, err := nemura.AddPerson(ctx, tx, c.org, p.now, c.owner, people.Person{LegalName: "Mia Manager", EmploymentStart: d("2025-01-06"), MemberID: &mm}, job, class)
		if err != nil {
			return err
		}
		jm := p.jon
		job.Manager = &pMia
		p.pJon, err = nemura.AddPerson(ctx, tx, c.org, p.now, c.owner, people.Person{LegalName: "Jon Joiner", EmploymentStart: today.AddDate(0, 0, 9), MemberID: &jm}, job, class)
		return err
	})
	return p
}

func (p *planTeam) mine(member uuid.UUID, held ...authz.Role) []api.PlanTask {
	var out []api.PlanTask
	if err := p.c.do(func(ctx context.Context, tx pgx.Tx) (err error) {
		out, err = nemura.MyOnboardingTasks(ctx, tx, member, held, today)
		return
	}); err != nil {
		p.c.t.Fatal(err)
	}
	return out
}

func (p *planTeam) decide(by uuid.UUID, held []authz.Role, id, state string, on time.Time) error {
	return p.c.do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.DecideOnboardingTask(ctx, tx, p.c.org, p.now, by, held, uuid.MustParse(id), state, "", on)
	})
}

func planCode(err error) string {
	var r *onboarding.Refusal
	if errors.As(err, &r) {
		return r.Code
	}
	return ""
}

// A plan is made from the standard template or the company's own, each task due from the person's start date, once for a person; templates are made whole, named once and archived, which never changes a
// plan already made (NM-ONB-01).
// req: NM-ONB-01
func TestAPlanIsMadeFromATemplateWithEachTaskDueFromTheStartDate(t *testing.T) {
	p := newPlanTeam(t)
	c := p.c
	do := func(fn func(ctx context.Context, tx pgx.Tx) error) error { return c.do(fn) }
	var tpl uuid.UUID
	if err := do(func(ctx context.Context, tx pgx.Tx) (err error) {
		tpl, err = nemura.MakeOnboardingTemplate(ctx, tx, c.org, p.now, c.hr, "Office joiner", nil) // none: the standard tasks
		return
	}); err != nil {
		t.Fatal(err)
	}
	if err := do(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.MakeOnboardingTemplate(ctx, tx, c.org, p.now, c.hr, "office JOINER", nil)
		return err
	}); planCode(err) != onboarding.CodeConflict {
		t.Errorf("the same name twice: %v", err)
	}
	if err := do(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.MakeOnboardingTemplate(ctx, tx, c.org, p.now, c.hr, "Bad", []onboarding.Task{{Title: "x", Assignee: "boss"}})
		return err
	}); planCode(err) != onboarding.CodeInvalid {
		t.Errorf("a task for nobody: %v", err)
	}
	var plan uuid.UUID
	if err := do(func(ctx context.Context, tx pgx.Tx) (err error) {
		plan, err = nemura.StartOnboardingPlan(ctx, tx, c.org, p.now, c.hr, p.pJon, &tpl)
		return
	}); err != nil {
		t.Fatal(err)
	}
	var plans []api.Plan
	if err := do(func(ctx context.Context, tx pgx.Tx) (err error) {
		plans, err = nemura.OnboardingPlans(ctx, tx, today)
		return
	}); err != nil || len(plans) != 1 || plans[0].ID != plan.String() {
		t.Fatalf("%+v %v", plans, err)
	}
	start := today.AddDate(0, 0, 9)
	if pl := plans[0]; pl.Template != "Office joiner" || pl.StartDate != start.Format(time.DateOnly) || pl.Total != len(onboarding.Standard()) || pl.Done != 0 {
		t.Errorf("the plan: %+v", pl)
	}
	for _, task := range plans[0].Tasks {
		if task.Title == "Issue the key employment terms (KETs)" && task.DueOn != start.AddDate(0, 0, 14).Format(time.DateOnly) {
			t.Errorf("the KETs are due %s, 14 days from the start", task.DueOn)
		}
	}
	// Once for a person; archived templates are not used, and archiving changes no plan.
	if err := do(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.StartOnboardingPlan(ctx, tx, c.org, p.now, c.hr, p.pJon, nil)
		return err
	}); planCode(err) != onboarding.CodeConflict {
		t.Errorf("a second plan: %v", err)
	}
	if err := do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.ArchiveOnboardingTemplate(ctx, tx, c.org, p.now, c.hr, tpl)
	}); err != nil {
		t.Fatal(err)
	}
	if err := do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.ArchiveOnboardingTemplate(ctx, tx, c.org, p.now, c.hr, tpl)
	}); planCode(err) != onboarding.CodeConflict {
		t.Errorf("archiving twice: %v", err)
	}
	if err := do(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.StartOnboardingPlan(ctx, tx, c.org, p.now, c.hr, p.pJon, &tpl)
		return err
	}); planCode(err) == "" {
		t.Errorf("a plan from an archived template: %v", err)
	}
	if err := do(func(ctx context.Context, tx pgx.Tx) (err error) {
		plans, err = nemura.OnboardingPlans(ctx, tx, today)
		return
	}); err != nil || plans[0].Total != len(onboarding.Standard()) {
		t.Errorf("the plan changed when its template was archived: %+v %v", plans, err)
	}
	// Another organisation sees none of it.
	other := newCompany(t)
	var seen []api.Plan
	if err := other.do(func(ctx context.Context, tx pgx.Tx) (err error) {
		seen, err = nemura.OnboardingPlans(ctx, tx, today)
		return
	}); err != nil || len(seen) != 0 {
		t.Errorf("another organisation sees %d plans: %v", len(seen), err)
	}
}

// Each person reads and decides only the tasks that are theirs: the joiner's own, the line manager's, IT's (an Administrator, HR administrator or the Owner) and HR's; a task is decided once, only the
// people team skips one, and a task's words and due date never change, even straight to the database (NM-ONB-01).
// req: NM-ONB-01
// asvs: V8.2.1, V8.3.1
func TestEachPersonReadsAndDecidesOnlyTheTasksThatAreTheirs(t *testing.T) {
	p := newPlanTeam(t)
	c := p.c
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.StartOnboardingPlan(ctx, tx, c.org, p.now, c.hr, p.pJon, nil)
		return err
	})
	std := onboarding.Standard()
	count := func(who string) (n int) {
		for _, t := range std {
			if t.Assignee == who {
				n++
			}
		}
		return n
	}
	team := []authz.Role{authz.HRAdmin}
	if got := len(p.mine(p.jon, authz.Employee)); got != count("joiner") {
		t.Errorf("the joiner sees %d tasks", got)
	}
	if got := len(p.mine(p.mia, authz.LineManager)); got != count("manager") {
		t.Errorf("the manager sees %d tasks", got)
	}
	if got := len(p.mine(p.ada, authz.Administrator)); got != count("it") {
		t.Errorf("IT sees %d tasks", got)
	}
	if got := len(p.mine(c.hr, team...)); got != count("hr")+count("it") {
		t.Errorf("an HR administrator sees %d tasks (HR's and IT's)", got)
	}
	if got := len(p.mine(p.other, authz.Employee)); got != 0 {
		t.Errorf("someone unrelated sees %d tasks", got)
	}
	if got := len(p.mine(c.pay, authz.PayrollAdmin)); got != 0 {
		t.Errorf("the payroll administrator sees %d tasks", got)
	}
	find := func(list []api.PlanTask, assignee string) api.PlanTask {
		for _, t := range list {
			if t.Assignee == assignee {
				return t
			}
		}
		t.Fatalf("no %s task", assignee)
		return api.PlanTask{}
	}
	hrTask := find(p.mine(c.hr, team...), "hr")
	itTask := find(p.mine(p.ada, authz.Administrator), "it")
	joinerTask := find(p.mine(p.jon, authz.Employee), "joiner")
	mgrTask := find(p.mine(p.mia, authz.LineManager), "manager")
	// Not yours: the joiner on IT's, the manager on the joiner's, IT on HR's, someone unrelated on anything.
	for name, tc := range map[string]struct {
		by   uuid.UUID
		held []authz.Role
		task api.PlanTask
	}{
		"the joiner deciding IT's":    {p.jon, []authz.Role{authz.Employee}, itTask},
		"the manager deciding theirs": {p.mia, []authz.Role{authz.LineManager}, joinerTask},
		"IT deciding HR's":            {p.ada, []authz.Role{authz.Administrator}, hrTask},
		"someone unrelated":           {p.other, []authz.Role{authz.Employee}, mgrTask},
		"the payroll administrator":   {c.pay, []authz.Role{authz.PayrollAdmin}, hrTask},
	} {
		if name == "the manager deciding theirs" {
			tc.task = joinerTask // a manager does not decide the joiner's own task
		}
		if err := p.decide(tc.by, tc.held, tc.task.ID, "done", today); planCode(err) != onboarding.CodeNotFound {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Yours: the joiner's own, the manager's, IT's; once; skipping is the people team's alone.
	if err := p.decide(p.jon, []authz.Role{authz.Employee}, joinerTask.ID, "skipped", today); planCode(err) != onboarding.CodeInvalid {
		t.Errorf("a joiner skipping their own task: %v", err)
	}
	for name, tc := range map[string]struct {
		by   uuid.UUID
		held []authz.Role
		task api.PlanTask
	}{"the joiner": {p.jon, []authz.Role{authz.Employee}, joinerTask}, "the manager": {p.mia, []authz.Role{authz.LineManager}, mgrTask}, "IT": {p.ada, []authz.Role{authz.Administrator}, itTask}} {
		if err := p.decide(tc.by, tc.held, tc.task.ID, "done", today); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := p.decide(p.jon, []authz.Role{authz.Employee}, joinerTask.ID, "done", today); planCode(err) != onboarding.CodeConflict {
		t.Errorf("deciding twice: %v", err)
	}
	if err := p.decide(c.hr, team, hrTask.ID, "skipped", today); err != nil {
		t.Errorf("the people team skipping: %v", err)
	}
	if got := p.mine(p.jon, authz.Employee); len(got) != count("joiner") || got[0].State != "done" || got[0].DecidedBy != "Jon Joiner" {
		t.Errorf("the joiner's list after deciding: %+v", got)
	}
	// An open task past its due date is overdue, from the day after.
	var plans []api.Plan
	c.must(func(ctx context.Context, tx pgx.Tx) (err error) {
		plans, err = nemura.OnboardingPlans(ctx, tx, today.AddDate(0, 1, 0))
		return
	})
	late := 0
	for _, task := range plans[0].Tasks {
		if task.Overdue {
			late++
		}
	}
	if late == 0 || plans[0].Done != 4 {
		t.Errorf("overdue %d, done %d", late, plans[0].Done)
	}
	// A task added to a plan is due on its day and is decided by its assignee.
	var added api.PlanTask
	c.must(func(ctx context.Context, tx pgx.Tx) (err error) {
		added, err = nemura.AddOnboardingTask(ctx, tx, c.org, p.now, c.hr, uuid.MustParse(plans[0].ID), api.PlanTaskInput{Title: "Order a security pass", Assignee: "joiner", DueOn: "2099-03-12"}, today)
		return
	})
	if added.State != "open" || added.DueOn != "2099-03-12" {
		t.Errorf("the added task: %+v", added)
	}
	// The database refuses to change a task's words or due date, to delete one, to reopen one or to change a plan.
	for name, sql := range map[string]string{
		"retitling":     `UPDATE nemura.onboarding_task SET title = 'Something else' WHERE id = '` + added.ID + `'`,
		"a new date":    `UPDATE nemura.onboarding_task SET due_on = due_on + 30 WHERE id = '` + added.ID + `'`,
		"deleting":      `DELETE FROM nemura.onboarding_task WHERE id = '` + added.ID + `'`,
		"reopening":     `UPDATE nemura.onboarding_task SET state = 'open', decided_at = NULL, decided_by = NULL WHERE id = '` + joinerTask.ID + `'`,
		"deleting plan": `DELETE FROM nemura.onboarding_plan`,
	} {
		if err := c.do(func(ctx context.Context, tx pgx.Tx) error { _, err := tx.Exec(ctx, sql); return err }); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// A task that closes by its flow is not marked done by hand, can be skipped when it does not apply, and is closed by the flow in its own transaction, once, by whoever ran the flow; a template names the flow
// from a short list, and the plan copies it (NM-ONB-01, rev 0.75).
// req: NM-ONB-01
// asvs: V8.2.1, V8.3.1
func TestATaskThatClosesByItsFlowIsClosedByItAndNeverMarkedDoneByHand(t *testing.T) {
	p := newPlanTeam(t)
	c := p.c
	tasks := []onboarding.Task{
		{Title: "Issue the KETs", Assignee: onboarding.ForHR, Offset: 14, Closes: onboarding.ClosesKETs},
		{Title: "Invite them to Fyniq", Assignee: onboarding.ForHR, Offset: 1, Closes: onboarding.ClosesInvitation},
		{Title: "Say hello", Assignee: onboarding.ForManager, Offset: 1},
	}
	if err := c.do(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.MakeOnboardingTemplate(ctx, tx, c.org, p.now, c.hr, "Bad flow", []onboarding.Task{{Title: "x", Assignee: "hr", Closes: "payroll"}})
		return err
	}); planCode(err) != onboarding.CodeInvalid {
		t.Errorf("a flow that does not exist: %v", err)
	}
	var tpl uuid.UUID
	c.must(func(ctx context.Context, tx pgx.Tx) (err error) {
		tpl, err = nemura.MakeOnboardingTemplate(ctx, tx, c.org, p.now, c.hr, "With flows", tasks)
		return
	})
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.StartOnboardingPlan(ctx, tx, c.org, p.now, c.hr, p.pJon, &tpl)
		return err
	})
	var plans []api.Plan
	c.must(func(ctx context.Context, tx pgx.Tx) (err error) {
		plans, err = nemura.OnboardingPlans(ctx, tx, today)
		return
	})
	byTitle := map[string]api.PlanTask{}
	for _, task := range plans[0].Tasks {
		byTitle[task.Title] = task
	}
	if byTitle["Issue the KETs"].Closes != "kets" || byTitle["Invite them to Fyniq"].Closes != "fyniq_invitation" || byTitle["Say hello"].Closes != "" {
		t.Fatalf("the plan copies the flows: %+v", byTitle)
	}
	hr := []authz.Role{authz.HRAdmin}
	decide := func(id, state string) error {
		return c.do(func(ctx context.Context, tx pgx.Tx) error {
			return nemura.DecideOnboardingTask(ctx, tx, c.org, p.now, c.hr, hr, mustUUID(id), state, "", today)
		})
	}
	if err := decide(byTitle["Issue the KETs"].ID, "done"); planCode(err) != onboarding.CodeConflict {
		t.Errorf("a KETs task marked done by hand: %v", err)
	}
	// The flow closes it, by whoever ran it, and only once.
	var n int
	c.must(func(ctx context.Context, tx pgx.Tx) (err error) {
		n, err = nemura.CloseOnboardingTasksByFlow(ctx, tx, c.org, p.now, c.owner, p.pJon, onboarding.ClosesKETs)
		return
	})
	if n != 1 {
		t.Errorf("the KETs flow closed %d tasks", n)
	}
	c.must(func(ctx context.Context, tx pgx.Tx) (err error) {
		n, err = nemura.CloseOnboardingTasksByFlow(ctx, tx, c.org, p.now, c.owner, p.pJon, onboarding.ClosesKETs)
		return
	})
	if n != 0 {
		t.Errorf("the flow closed a task a second time: %d", n)
	}
	// A task that does not apply is skipped by the people team.
	if err := decide(byTitle["Invite them to Fyniq"].ID, "skipped"); err != nil {
		t.Errorf("skipping an invitation task: %v", err)
	}
	c.must(func(ctx context.Context, tx pgx.Tx) (err error) {
		plans, err = nemura.OnboardingPlans(ctx, tx, today)
		return
	})
	for _, task := range plans[0].Tasks {
		switch task.Title {
		case "Issue the KETs":
			if task.State != "done" || task.DecidedBy != "Owen Owner" || task.Note == "" {
				t.Errorf("the KETs task after its flow: %+v", task)
			}
		case "Invite them to Fyniq":
			if task.State != "skipped" {
				t.Errorf("the invitation task after a skip: %+v", task)
			}
		}
	}
	// The flow's column never changes under a task, even straight to the database.
	if err := c.do(func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE nemura.onboarding_task SET closes = NULL WHERE title = 'Say hello'`)
		return err
	}); pgErr(err) != "NM212" && pgErr(err) != "42501" {
		t.Errorf("a task's flow rewritten: %v", err)
	}
}
