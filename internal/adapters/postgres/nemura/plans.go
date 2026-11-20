package nemura

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/onboarding"
	"github.com/cognizhi/fyniq/internal/ports"
)

// Onboarding plans (PRD-002 NM-ONB-01): templates of tasks, a plan made from one for a person with each task due from their start date, and each person's decision of the tasks that are theirs. A template's
// tasks are copied into a plan when it is made, so a template changed or archived later never changes a plan. The audit events name identities and titles, never a person's private facts.

var noTemplate = &onboarding.Refusal{Code: onboarding.CodeNotFound, Message: "There is no such template."}

func planError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "NM212":
			return &onboarding.Refusal{Code: onboarding.CodeConflict, Message: "That is done or archived already: it happens once, and its words and due date never change."}
		case "23505":
			return &onboarding.Refusal{Code: onboarding.CodeConflict, Message: "That exists already: a template's name is used once among the live ones, and a person has one plan."}
		}
	}
	return err
}

func toTasks(in []api.PlanTemplateTask) []onboarding.Task {
	out := make([]onboarding.Task, len(in))
	for i, t := range in {
		out[i] = onboarding.Task{Title: strings.TrimSpace(t.Title), Detail: strings.TrimSpace(t.Detail), Assignee: t.Assignee, Offset: t.DueOffsetDays, Closes: t.Closes}
	}
	return out
}

// MakeOnboardingTemplate makes a template; with no tasks it copies the standard template.
func MakeOnboardingTemplate(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, name string, tasks []onboarding.Task) (uuid.UUID, error) {
	if len(tasks) == 0 {
		tasks = onboarding.Standard()
	}
	name = strings.TrimSpace(name)
	if err := onboarding.CheckTemplate(name, tasks); err != nil {
		return uuid.Nil, err
	}
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.onboarding_template (id, name, created_by, created_at) VALUES ($1, $2, $3, $4)`, id, name, by, at); err != nil {
		return uuid.Nil, planError(err)
	}
	for i, t := range tasks {
		if _, err := tx.Exec(ctx, `INSERT INTO nemura.onboarding_template_task (template_id, position, title, detail, assignee, due_offset_days, closes) VALUES ($1, $2, $3, $4, $5, $6, $7)`, id, i+1, t.Title, t.Detail, t.Assignee, t.Offset, orNil(t.Closes)); err != nil {
			return uuid.Nil, planError(err)
		}
	}
	return id, Audit(ctx, tx, org, at, user(by), "nemura.onboarding_template.made", audit.Object{Type: "onboarding_template", ID: id}, map[string]audit.Change{"name": {nil, name}, "tasks": {nil, len(tasks)}}, "")
}

// OnboardingTemplates lists the templates with their tasks, the live ones first.
func OnboardingTemplates(ctx context.Context, tx pgx.Tx) ([]api.PlanTemplate, error) {
	rows, err := tx.Query(ctx, `SELECT id, name, archived_at IS NOT NULL FROM nemura.onboarding_template ORDER BY archived_at IS NOT NULL, created_at DESC, id`)
	if err != nil {
		return nil, err
	}
	var out []api.PlanTemplate
	for rows.Next() {
		t := api.PlanTemplate{Tasks: []api.PlanTemplateTask{}}
		if err := rows.Scan(&t.ID, &t.Name, &t.Archived); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, t)
	}
	rows.Close()
	for i := range out {
		ts, err := tx.Query(ctx, `SELECT title, detail, assignee, due_offset_days, coalesce(closes, '') FROM nemura.onboarding_template_task WHERE template_id = $1 ORDER BY position`, out[i].ID)
		if err != nil {
			return nil, err
		}
		for ts.Next() {
			var t api.PlanTemplateTask
			if err := ts.Scan(&t.Title, &t.Detail, &t.Assignee, &t.DueOffsetDays, &t.Closes); err != nil {
				ts.Close()
				return nil, err
			}
			out[i].Tasks = append(out[i].Tasks, t)
		}
		ts.Close()
	}
	return out, nil
}

// ArchiveOnboardingTemplate archives a live template.
func ArchiveOnboardingTemplate(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, id uuid.UUID) error {
	var archived bool
	if err := tx.QueryRow(ctx, `SELECT archived_at IS NOT NULL FROM nemura.onboarding_template WHERE id = $1 FOR UPDATE`, id).Scan(&archived); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return noTemplate
		}
		return err
	}
	if archived {
		return &onboarding.Refusal{Code: onboarding.CodeConflict, Message: "That template is archived already."}
	}
	if _, err := tx.Exec(ctx, `UPDATE nemura.onboarding_template SET archived_at = $2 WHERE id = $1`, id, at); err != nil {
		return planError(err)
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.onboarding_template.archived", audit.Object{Type: "onboarding_template", ID: id}, map[string]audit.Change{"archived": {false, true}}, "")
}

// StartOnboardingPlan makes a person's plan from a live template (the standard tasks when none is named) with each task due from their start date.
func StartOnboardingPlan(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, person uuid.UUID, template *uuid.UUID) (uuid.UUID, error) {
	var start time.Time
	var legal string
	var left *time.Time
	if err := tx.QueryRow(ctx, `SELECT employment_start, legal_name, leaving_date FROM nemura.person WHERE id = $1`, person).Scan(&start, &legal, &left); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, &onboarding.Refusal{Code: onboarding.CodeNotFound, Message: "There is no such person in this organisation."}
		}
		return uuid.Nil, err
	}
	if left != nil {
		return uuid.Nil, &onboarding.Refusal{Code: onboarding.CodeInvalid, Message: "This person has left: a plan is for someone who joins."}
	}
	name, tasks := "Standard", onboarding.Standard()
	if template != nil {
		var archived bool
		if err := tx.QueryRow(ctx, `SELECT name, archived_at IS NOT NULL FROM nemura.onboarding_template WHERE id = $1`, *template).Scan(&name, &archived); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return uuid.Nil, noTemplate
			}
			return uuid.Nil, err
		}
		if archived {
			return uuid.Nil, &onboarding.Refusal{Code: onboarding.CodeInvalid, Message: "That template is archived: choose a live one or make a new template."}
		}
		ts, err := tx.Query(ctx, `SELECT title, detail, assignee, due_offset_days, coalesce(closes, '') FROM nemura.onboarding_template_task WHERE template_id = $1 ORDER BY position`, *template)
		if err != nil {
			return uuid.Nil, err
		}
		tasks = nil
		for ts.Next() {
			var t onboarding.Task
			if err := ts.Scan(&t.Title, &t.Detail, &t.Assignee, &t.Offset, &t.Closes); err != nil {
				ts.Close()
				return uuid.Nil, err
			}
			tasks = append(tasks, t)
		}
		ts.Close()
	}
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.onboarding_plan (id, person_id, template_name, start_date, created_by, created_at) VALUES ($1, $2, $3, $4, $5, $6)`, id, person, name, day(start), by, at); err != nil {
		return uuid.Nil, planError(err)
	}
	for i, t := range tasks {
		if _, err := tx.Exec(ctx, `INSERT INTO nemura.onboarding_task (plan_id, person_id, position, title, detail, assignee, due_on, closes) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			id, person, i+1, t.Title, t.Detail, t.Assignee, day(onboarding.DueOn(start, t.Offset)), orNil(t.Closes)); err != nil {
			return uuid.Nil, planError(err)
		}
	}
	if err := Audit(ctx, tx, org, at, user(by), "nemura.onboarding_plan.started", audit.Object{Type: "onboarding_plan", ID: id}, map[string]audit.Change{"person_id": {nil, person.String()}, "template": {nil, name}, "tasks": {nil, len(tasks)}}, ""); err != nil {
		return uuid.Nil, err
	}
	// A flow that happened before the plan started (the KETs issued, the person invited) closes its task by its own check: the task is never left open for someone to skip.
	var kets, invited bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM nemura.letter_issued WHERE person_id = $1 AND type_key = 'kets'), EXISTS (SELECT 1 FROM nemura.join_task WHERE person_id = $1 AND kind = 'fyniq_invitation' AND state = 'done')`, person).Scan(&kets, &invited); err != nil {
		return uuid.Nil, err
	}
	for _, f := range []struct {
		flow     string
		happened bool
	}{{onboarding.ClosesKETs, kets}, {onboarding.ClosesInvitation, invited}} {
		flow := f.flow
		if f.happened {
			if _, err := closeTasksByFlow(ctx, tx, org, at, by, person, flow, "Closed by itself when the plan started: "+closesFlow[flow]+" before it."); err != nil {
				return uuid.Nil, err
			}
		}
	}
	return id, nil
}

const taskColumns = `t.id, t.plan_id, t.person_id, p.legal_name, t.title, t.detail, t.assignee, t.due_on, t.state, coalesce(m.display_name, ''), t.decided_at, coalesce(t.note, ''), coalesce(t.closes, '')`

func scanTask(row pgx.Row, today time.Time) (api.PlanTask, error) {
	var t api.PlanTask
	var due time.Time
	var decided *time.Time
	if err := row.Scan(&t.ID, &t.PlanID, &t.PersonID, &t.Person, &t.Title, &t.Detail, &t.Assignee, &due, &t.State, &t.DecidedBy, &decided, &t.Note, &t.Closes); err != nil {
		return t, err
	}
	t.DueOn, t.Overdue = day(due), onboarding.Overdue(t.State, due, today)
	if decided != nil {
		t.DecidedAt = decided.UTC().Format(time.RFC3339)
	}
	return t, nil
}

const taskFrom = ` FROM nemura.onboarding_task t JOIN nemura.person p ON p.organisation_id = t.organisation_id AND p.id = t.person_id LEFT JOIN nemura.member m ON m.organisation_id = t.organisation_id AND m.id = t.decided_by `

// OnboardingPlans lists every plan with its tasks, the ones with the most left first.
func OnboardingPlans(ctx context.Context, tx pgx.Tx, today time.Time) ([]api.Plan, error) {
	rows, err := tx.Query(ctx, `SELECT pl.id, pl.person_id, p.legal_name, pl.template_name, pl.start_date,
  (SELECT count(*) FROM nemura.onboarding_task t WHERE t.plan_id = pl.id AND t.state <> 'open'), (SELECT count(*) FROM nemura.onboarding_task t WHERE t.plan_id = pl.id)
FROM nemura.onboarding_plan pl JOIN nemura.person p ON p.organisation_id = pl.organisation_id AND p.id = pl.person_id ORDER BY pl.start_date DESC, pl.id`)
	if err != nil {
		return nil, err
	}
	var out []api.Plan
	for rows.Next() {
		var pl api.Plan
		var start time.Time
		if err := rows.Scan(&pl.ID, &pl.PersonID, &pl.Person, &pl.Template, &start, &pl.Done, &pl.Total); err != nil {
			rows.Close()
			return nil, err
		}
		pl.StartDate, pl.Tasks = day(start), []api.PlanTask{}
		out = append(out, pl)
	}
	rows.Close()
	for i := range out {
		ts, err := tx.Query(ctx, `SELECT `+taskColumns+taskFrom+`WHERE t.plan_id = $1 ORDER BY t.due_on, t.position`, out[i].ID)
		if err != nil {
			return nil, err
		}
		for ts.Next() {
			t, err := scanTask(ts, today)
			if err != nil {
				ts.Close()
				return nil, err
			}
			out[i].Tasks = append(out[i].Tasks, t)
		}
		ts.Close()
	}
	slices.SortStableFunc(out, func(a, b api.Plan) int { return (b.Total - b.Done) - (a.Total - a.Done) })
	return out, nil
}

// AddOnboardingTask adds a task to a plan, due on a day.
func AddOnboardingTask(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, plan uuid.UUID, in api.PlanTaskInput, today time.Time) (api.PlanTask, error) {
	due, err := time.Parse(time.DateOnly, in.DueOn)
	if err != nil {
		return api.PlanTask{}, &onboarding.Refusal{Code: onboarding.CodeInvalid, Message: "The due date is not a date: write it as 2026-12-03."}
	}
	t := onboarding.Task{Title: strings.TrimSpace(in.Title), Detail: strings.TrimSpace(in.Detail), Assignee: in.Assignee}
	if err := onboarding.CheckTask(t); err != nil {
		return api.PlanTask{}, err
	}
	var person uuid.UUID
	var n int
	if err := tx.QueryRow(ctx, `SELECT person_id, (SELECT count(*) FROM nemura.onboarding_task WHERE plan_id = $1) FROM nemura.onboarding_plan WHERE id = $1`, plan).Scan(&person, &n); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return api.PlanTask{}, &onboarding.Refusal{Code: onboarding.CodeNotFound, Message: "There is no such plan."}
		}
		return api.PlanTask{}, err
	}
	if n >= onboarding.MaxTasks*2 {
		return api.PlanTask{}, &onboarding.Refusal{Code: onboarding.CodeInvalid, Message: "A plan holds at most 120 tasks: finish some before adding more."}
	}
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.onboarding_task (id, plan_id, person_id, position, title, detail, assignee, due_on) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, id, plan, person, n+1, t.Title, t.Detail, t.Assignee, day(due)); err != nil {
		return api.PlanTask{}, planError(err)
	}
	if err := Audit(ctx, tx, org, at, user(by), "nemura.onboarding_task.added", audit.Object{Type: "onboarding_task", ID: id}, map[string]audit.Change{"plan": {nil, plan.String()}, "assignee": {nil, t.Assignee}}, ""); err != nil {
		return api.PlanTask{}, err
	}
	return scanTask(tx.QueryRow(ctx, `SELECT `+taskColumns+taskFrom+`WHERE t.id = $1`, id), today)
}

func whoIs(held []authz.Role) (team, it bool) {
	for _, r := range held {
		switch r {
		case authz.HROfficer:
			team = true
		case authz.HRAdmin, authz.Owner:
			team, it = true, true
		case authz.Administrator:
			it = true
		}
	}
	return team, it
}

// MyOnboardingTasks lists the tasks the member can act on: their own as the joiner, their reports' as the line manager, IT's and HR's by their roles; open ones and those decided in the last 14 days.
func MyOnboardingTasks(ctx context.Context, tx pgx.Tx, member uuid.UUID, held []authz.Role, today time.Time) ([]api.PlanTask, error) {
	team, it := whoIs(held)
	rows, err := tx.Query(ctx, `SELECT `+taskColumns+taskFrom+`WHERE (
  (t.assignee = 'joiner' AND p.member_id = $1)
  OR (t.assignee = 'manager' AND EXISTS (SELECT 1 FROM nemura.reporting_line r WHERE r.report_id = p.member_id AND r.manager_id = $1 AND (r.effective_to IS NULL OR r.effective_to > $2::date)))
  OR (t.assignee = 'it' AND $3) OR (t.assignee = 'hr' AND $4))
 AND (t.state = 'open' OR t.decided_at > $2::date - 14)
ORDER BY t.due_on, t.position, t.id`, member, day(today), it, team)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []api.PlanTask
	for rows.Next() {
		t, err := scanTask(rows, today)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// A line manager whose line starts on the joiner's start date is the manager before it: preparing a joiner comes first, so the line's own start is not asked.

// DecideOnboardingTask marks a task done or skipped, once, if it is the member's to decide.
func DecideOnboardingTask(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, held []authz.Role, id uuid.UUID, state, note string, today time.Time) error {
	notYours := &onboarding.Refusal{Code: onboarding.CodeNotFound, Message: "There is no such task for you."}
	if state != onboarding.TaskDone && state != onboarding.TaskSkipped {
		return &onboarding.Refusal{Code: onboarding.CodeInvalid, Message: "A task is marked done, or skipped when it does not apply."}
	}
	if len([]rune(note)) > onboarding.MaxTaskNote {
		return &onboarding.Refusal{Code: onboarding.CodeInvalid, Message: "The note is at most 300 characters."}
	}
	team, it := whoIs(held)
	var assignee, current, closes string
	var joiner, manager bool
	err := tx.QueryRow(ctx, `SELECT t.assignee, t.state, coalesce(t.closes, ''), coalesce(p.member_id = $2, false),
  EXISTS (SELECT 1 FROM nemura.reporting_line r WHERE r.report_id = p.member_id AND r.manager_id = $2 AND (r.effective_to IS NULL OR r.effective_to > $3::date))
FROM nemura.onboarding_task t JOIN nemura.person p ON p.organisation_id = t.organisation_id AND p.id = t.person_id WHERE t.id = $1 FOR UPDATE OF t`, id, by, day(today)).Scan(&assignee, &current, &closes, &joiner, &manager)
	if errors.Is(err, pgx.ErrNoRows) {
		return notYours
	}
	if err != nil {
		return err
	}
	if !onboarding.CanDecide(assignee, onboarding.Who{Joiner: joiner, Manager: manager, IT: it, HR: team}) {
		return notYours
	}
	if state == onboarding.TaskSkipped && !team {
		return &onboarding.Refusal{Code: onboarding.CodeInvalid, Message: "Only the people team skips a task that does not apply."}
	}
	if current != onboarding.TaskOpen {
		return &onboarding.Refusal{Code: onboarding.CodeConflict, Message: "That task is done or skipped already: it is decided once."}
	}
	if closes != "" && state == onboarding.TaskDone {
		return &onboarding.Refusal{Code: onboarding.CodeConflict, Message: "That task closes by itself when " + closesFlow[closes] + ": do that and it is done. If it does not apply, skip it."}
	}
	if _, err := tx.Exec(ctx, `UPDATE nemura.onboarding_task SET state = $2, decided_by = $3, decided_at = $4, note = $5 WHERE id = $1`, id, state, by, at, orNil(strings.TrimSpace(note))); err != nil {
		return planError(err)
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.onboarding_task.decided", audit.Object{Type: "onboarding_task", ID: id}, map[string]audit.Change{"state": {"open", state}}, "")
}

// dateOf is the day of a moment, with no time, for comparing a due date.
func dateOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// PlanService is onboarding plans behind the API.
type PlanService struct {
	Store *Store
	Clock ports.Clock
}

var _ api.Plans = PlanService{}

func (p PlanService) in(ctx context.Context, s api.Session, fn func(ctx context.Context, tx pgx.Tx, at time.Time) error) error {
	at := p.Clock.Now()
	return p.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error { return fn(ctx, tx, at) })
}

func (p PlanService) Templates(ctx context.Context, s api.Session) (out []api.PlanTemplate, err error) {
	err = p.in(ctx, s, func(ctx context.Context, tx pgx.Tx, _ time.Time) (err error) {
		out, err = OnboardingTemplates(ctx, tx)
		return
	})
	return
}

func (p PlanService) MakeTemplate(ctx context.Context, s api.Session, name string, tasks []api.PlanTemplateTask) (api.PlanTemplate, error) {
	var out api.PlanTemplate
	err := p.in(ctx, s, func(ctx context.Context, tx pgx.Tx, at time.Time) error {
		id, err := MakeOnboardingTemplate(ctx, tx, s.Organisation, at, s.Member, name, toTasks(tasks))
		if err != nil {
			return err
		}
		all, err := OnboardingTemplates(ctx, tx)
		for _, t := range all {
			if t.ID == id.String() {
				out = t
			}
		}
		return err
	})
	return out, err
}

func (p PlanService) ArchiveTemplate(ctx context.Context, s api.Session, id uuid.UUID) error {
	return p.in(ctx, s, func(ctx context.Context, tx pgx.Tx, at time.Time) error {
		return ArchiveOnboardingTemplate(ctx, tx, s.Organisation, at, s.Member, id)
	})
}

func (p PlanService) Start(ctx context.Context, s api.Session, person uuid.UUID, template *uuid.UUID) (api.Plan, error) {
	var out api.Plan
	err := p.in(ctx, s, func(ctx context.Context, tx pgx.Tx, at time.Time) error {
		id, err := StartOnboardingPlan(ctx, tx, s.Organisation, at, s.Member, person, template)
		if err != nil {
			return err
		}
		all, err := OnboardingPlans(ctx, tx, dateOf(at))
		for _, pl := range all {
			if pl.ID == id.String() {
				out = pl
			}
		}
		return err
	})
	return out, err
}

func (p PlanService) List(ctx context.Context, s api.Session) (out []api.Plan, err error) {
	err = p.in(ctx, s, func(ctx context.Context, tx pgx.Tx, at time.Time) (err error) {
		out, err = OnboardingPlans(ctx, tx, dateOf(at))
		return
	})
	return
}

func (p PlanService) AddTask(ctx context.Context, s api.Session, plan uuid.UUID, in api.PlanTaskInput) (out api.PlanTask, err error) {
	err = p.in(ctx, s, func(ctx context.Context, tx pgx.Tx, at time.Time) (err error) {
		out, err = AddOnboardingTask(ctx, tx, s.Organisation, at, s.Member, plan, in, dateOf(at))
		return
	})
	return
}

func (p PlanService) Mine(ctx context.Context, s api.Session, held []authz.Role) (out []api.PlanTask, err error) {
	err = p.in(ctx, s, func(ctx context.Context, tx pgx.Tx, at time.Time) (err error) {
		out, err = MyOnboardingTasks(ctx, tx, s.Member, held, dateOf(at))
		return
	})
	return
}

func (p PlanService) Decide(ctx context.Context, s api.Session, held []authz.Role, task uuid.UUID, state, note string) error {
	return p.in(ctx, s, func(ctx context.Context, tx pgx.Tx, at time.Time) error {
		return DecideOnboardingTask(ctx, tx, s.Organisation, at, s.Member, held, task, state, note, dateOf(at))
	})
}

// closesFlow says, in words, what closes a task by itself.
var closesFlow = map[string]string{onboarding.ClosesKETs: "the person's key employment terms letter is issued", onboarding.ClosesInvitation: "the person is invited to Fyniq"}

// CloseOnboardingTasksByFlow marks done the open tasks of a person's plan that close by the flow that has just happened, by the member who ran it, in the flow's own transaction, one audit event each. It
// closes nothing for a person with no plan or no such task, and never a task already decided (NM-ONB-01, rev 0.75).
func CloseOnboardingTasksByFlow(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, person uuid.UUID, flow string) (int, error) {
	return closeTasksByFlow(ctx, tx, org, at, by, person, flow, "Closed by itself when "+closesFlow[flow]+".")
}

func closeTasksByFlow(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, person uuid.UUID, flow, note string) (int, error) {
	rows, err := tx.Query(ctx, `UPDATE nemura.onboarding_task SET state = 'done', decided_by = $3, decided_at = $4, note = $5 WHERE person_id = $1 AND closes = $2 AND state = 'open' RETURNING id`,
		person, flow, by, at, note)
	if err != nil {
		return 0, planError(err)
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, id := range ids {
		if err := Audit(ctx, tx, org, at, user(by), "nemura.onboarding_task.decided", audit.Object{Type: "onboarding_task", ID: id}, map[string]audit.Change{"state": {"open", onboarding.TaskDone}, "closed_by": {nil, flow}}, ""); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}
