package nemura

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/performance"
	"github.com/cognizhi/fyniq/internal/ports"
)

// Goals and KPIs (PRD-002 NM-PRF-01, NM-PRF-02). Company and team goals are read by every member so that a personal goal can align with one [owner to confirm]; a personal goal and a role KPI are read by the person,
// their line manager (the reporting line) and the people team, and changed by the person, their manager and the HR administrator or Owner. A reader who may not read one is told there is none. The audit events name
// identities, scopes and states, never a title or a value. Values are decimal strings and progress is worked out exactly.

var noGoal = &performance.Refusal{Code: performance.CodeNotFound, Message: "There is no such goal or person for you."}

func perfError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "NM214":
			return &performance.Refusal{Code: performance.CodeConflict, Message: "That is closed already, or its words and target cannot change: a goal is done or cancelled once and an update adds to the history."}
		case "23514", "22003":
			return &performance.Refusal{Code: performance.CodeInvalid, Message: "That is not a valid goal or measure: check the lengths, the dates and the numbers."}
		}
	}
	return err
}

type perfWho struct {
	member          uuid.UUID
	admin, hr, lead bool // HR administrator or Owner; HR officer, administrator or Owner; holds the line manager role
}

func whoPerf(member uuid.UUID, held []authz.Role) perfWho {
	w := perfWho{member: member}
	for _, r := range held {
		switch r {
		case authz.HROfficer:
			w.hr = true
		case authz.HRAdmin, authz.Owner:
			w.admin, w.hr = true, true
		case authz.LineManager:
			w.lead = true
		}
	}
	return w
}

// relation is how the member stands to a person: they are the person, or their line manager today.
func relation(ctx context.Context, tx pgx.Tx, member, person uuid.UUID, today time.Time) (self, manager bool, err error) {
	err = tx.QueryRow(ctx, `SELECT coalesce(p.member_id = $1, false), EXISTS (SELECT 1 FROM nemura.reporting_line r WHERE r.report_id = p.member_id AND r.manager_id = $1 AND (r.effective_to IS NULL OR r.effective_to > $3::date))
FROM nemura.person p WHERE p.id = $2`, member, person, day(today)).Scan(&self, &manager)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, noGoal
	}
	return
}

func (w perfWho) mayChange(self, manager bool) bool { return self || manager || w.admin }
func (w perfWho) mayRead(self, manager bool) bool   { return self || manager || w.hr }

func ratOf(s string) (*big.Rat, bool) { return new(big.Rat).SetString(s) }

const measureSelect = `SELECT m.id, m.title, m.unit, m.start_value::numeric(20,4)::text, m.target::numeric(20,4)::text,
  coalesce((SELECT u.value FROM nemura.measure_update u WHERE u.measure_id = m.id ORDER BY u.at DESC, u.id DESC LIMIT 1), m.start_value)::numeric(20,4)::text, m.down, m.created_at FROM nemura.measure m `

func trimZeros(s string) string {
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	if s == "" || s == "-" {
		return "0"
	}
	return s
}

func scanMeasures(rows pgx.Rows, from, due, today time.Time) ([]api.Measure, error) {
	defer rows.Close()
	var out []api.Measure
	for rows.Next() {
		var m api.Measure
		var created time.Time
		if err := rows.Scan(&m.ID, &m.Title, &m.Unit, &m.Start, &m.Target, &m.Current, &m.Down, &created); err != nil {
			return nil, err
		}
		m.Start, m.Target, m.Current = trimZeros(m.Start), trimZeros(m.Target), trimZeros(m.Current)
		m.Progress = performance.Progress(performance.Measure{Start: m.Start, Target: m.Target, Current: m.Current, Down: m.Down})
		start := from
		if start.IsZero() {
			start = dateOf(created)
		}
		m.Status = performance.Status(m.Progress, start, due, today)
		out = append(out, m)
	}
	return out, rows.Err()
}

func goalViews(ctx context.Context, tx pgx.Tx, where string, args []any, today time.Time) ([]api.Goal, error) {
	rows, err := tx.Query(ctx, `SELECT g.id, g.scope, coalesce(g.person_id::text, ''), coalesce(p.legal_name, ''), coalesce(g.team, ''), coalesce(g.parent_id::text, ''), g.title, g.starts_on, g.due_on, g.state
FROM nemura.goal g LEFT JOIN nemura.person p ON p.organisation_id = g.organisation_id AND p.id = g.person_id WHERE `+where+` ORDER BY g.due_on, g.id`, args...)
	if err != nil {
		return nil, err
	}
	var out []api.Goal
	var starts, dues []time.Time
	for rows.Next() {
		var g api.Goal
		var s, d time.Time
		if err := rows.Scan(&g.ID, &g.Scope, &g.PersonID, &g.Person, &g.Team, &g.ParentID, &g.Title, &s, &d, &g.State); err != nil {
			rows.Close()
			return nil, err
		}
		g.StartsOn, g.DueOn = day(s), day(d)
		out = append(out, g)
		starts, dues = append(starts, s), append(dues, d)
	}
	rows.Close()
	for i := range out {
		ms, err := tx.Query(ctx, measureSelect+`WHERE m.goal_id = $1 ORDER BY m.created_at, m.id`, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Measures, err = scanMeasures(ms, starts[i], dues[i], today)
		if err != nil {
			return nil, err
		}
		if out[i].Measures == nil {
			out[i].Measures = []api.Measure{}
		}
		sum := 0
		for _, m := range out[i].Measures {
			sum += m.Progress
		}
		if n := len(out[i].Measures); n > 0 {
			out[i].Progress = sum / n
		}
	}
	return out, nil
}

func kpiViews(ctx context.Context, tx pgx.Tx, person uuid.UUID, today time.Time) ([]api.Measure, error) {
	rows, err := tx.Query(ctx, measureSelect+`WHERE m.person_id = $1 ORDER BY m.created_at, m.id`, person)
	if err != nil {
		return nil, err
	}
	return scanMeasures(rows, time.Time{}, time.Time{}, today)
}

func personOfMember(ctx context.Context, tx pgx.Tx, member uuid.UUID) (uuid.UUID, bool, error) {
	var p uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM nemura.person WHERE member_id = $1`, member).Scan(&p)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	return p, err == nil, err
}

func toPerfMeasures(in []api.MeasureInput) ([]performance.Measure, error) {
	out := make([]performance.Measure, len(in))
	for i, m := range in {
		start := m.Start
		if start == "" {
			start = "0"
		}
		down := false
		if rs, ok1 := ratOf(start); ok1 {
			if rt, ok2 := ratOf(m.Target); ok2 {
				down = rt.Cmp(rs) < 0
			}
		}
		out[i] = performance.Measure{Title: strings.TrimSpace(m.Title), Unit: m.Unit, Start: start, Target: m.Target, Current: start, Down: down}
	}
	return out, nil
}

// SetGoal sets a goal with its key results for the member, if their rights reach it.
func SetGoal(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, member uuid.UUID, held []authz.Role, in api.GoalInput) (api.Goal, error) {
	w, today := whoPerf(member, held), dateOf(at)
	due, err := time.Parse(time.DateOnly, in.DueOn)
	if err != nil {
		return api.Goal{}, &performance.Refusal{Code: performance.CodeInvalid, Message: "The due date is not a date: write it as 2026-12-31."}
	}
	ms, err := toPerfMeasures(in.Measures)
	if err != nil {
		return api.Goal{}, err
	}
	if err := performance.CheckGoal(in.Scope, in.Title, in.Team, due, today, ms); err != nil {
		return api.Goal{}, err
	}
	var person *uuid.UUID
	switch in.Scope {
	case performance.Company:
		if !w.admin {
			return api.Goal{}, &performance.Refusal{Code: performance.CodeNotFound, Message: "Only the HR administrator and the Owner set company goals."}
		}
	case performance.Team:
		if !w.admin && !w.lead {
			return api.Goal{}, &performance.Refusal{Code: performance.CodeNotFound, Message: "Only a line manager, the HR administrator and the Owner set team goals."}
		}
	default:
		p := uuid.Nil
		if in.PersonID != "" {
			if p, err = uuid.Parse(in.PersonID); err != nil {
				return api.Goal{}, noGoal
			}
		} else {
			own, ok, err := personOfMember(ctx, tx, member)
			if err != nil {
				return api.Goal{}, err
			}
			if !ok {
				return api.Goal{}, &performance.Refusal{Code: performance.CodeInvalid, Message: "You have no person record to set a goal on: name the person the goal is for."}
			}
			p = own
		}
		self, manager, err := relation(ctx, tx, member, p, today)
		if err != nil {
			return api.Goal{}, err
		}
		if !w.mayChange(self, manager) {
			return api.Goal{}, noGoal
		}
		person = &p
	}
	var parent *uuid.UUID
	if in.ParentID != "" {
		pid, err := uuid.Parse(in.ParentID)
		if err != nil {
			return api.Goal{}, &performance.Refusal{Code: performance.CodeNotFound, Message: "The goal to align with does not exist."}
		}
		var scope string
		if err := tx.QueryRow(ctx, `SELECT scope FROM nemura.goal WHERE id = $1`, pid).Scan(&scope); err != nil {
			return api.Goal{}, &performance.Refusal{Code: performance.CodeNotFound, Message: "The goal to align with does not exist."}
		}
		if !(scope == performance.Company && in.Scope != performance.Company) && !(scope == performance.Team && in.Scope == performance.Personal) {
			return api.Goal{}, &performance.Refusal{Code: performance.CodeInvalid, Message: "A goal aligns with a broader one: a team goal with the company's, a personal goal with the company's or its team's."}
		}
		parent = &pid
	}
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.goal (id, scope, person_id, team, parent_id, title, starts_on, due_on, created_by, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		id, in.Scope, person, orNil(strings.TrimSpace(in.Team)), parent, strings.TrimSpace(in.Title), day(today), day(due), member, at); err != nil {
		return api.Goal{}, perfError(err)
	}
	for _, m := range ms {
		if _, err := tx.Exec(ctx, `INSERT INTO nemura.measure (goal_id, title, unit, start_value, target, down, created_by, created_at) VALUES ($1, $2, $3, $4::numeric, $5::numeric, $6, $7, $8)`,
			id, m.Title, m.Unit, m.Start, m.Target, m.Down, member, at); err != nil {
			return api.Goal{}, perfError(err)
		}
	}
	if err := Audit(ctx, tx, org, at, user(member), "nemura.goal.set", audit.Object{Type: "goal", ID: id}, map[string]audit.Change{"scope": {nil, in.Scope}, "measures": {nil, len(ms)}}, ""); err != nil {
		return api.Goal{}, err
	}
	gs, err := goalViews(ctx, tx, `g.id = $1`, []any{id}, today)
	if err != nil || len(gs) == 0 {
		return api.Goal{}, err
	}
	return gs[0], nil
}

// goalRights loads a goal's scope and owner and what the member may do with it.
func goalRights(ctx context.Context, tx pgx.Tx, w perfWho, id uuid.UUID, today time.Time) (scope, state string, read, change bool, err error) {
	var person *uuid.UUID
	var creator uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT scope, state, person_id, created_by FROM nemura.goal WHERE id = $1`, id).Scan(&scope, &state, &person, &creator); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", false, false, noGoal
		}
		return
	}
	switch scope {
	case performance.Company:
		return scope, state, true, w.admin, nil
	case performance.Team:
		return scope, state, true, w.admin || creator == w.member, nil
	}
	self, manager, err := relation(ctx, tx, w.member, *person, today)
	if err != nil {
		return
	}
	return scope, state, w.mayRead(self, manager), w.mayChange(self, manager), nil
}

// CloseGoal marks a goal done or cancelled, once.
func CloseGoal(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, member uuid.UUID, held []authz.Role, id uuid.UUID, state string) error {
	if state != "done" && state != "cancelled" {
		return &performance.Refusal{Code: performance.CodeInvalid, Message: "A goal is marked done, or cancelled when it no longer applies."}
	}
	_, cur, read, change, err := goalRights(ctx, tx, whoPerf(member, held), id, dateOf(at))
	if err != nil {
		return err
	}
	if !read || !change {
		return noGoal
	}
	if cur != "open" {
		return &performance.Refusal{Code: performance.CodeConflict, Message: "That goal is closed already: it is done or cancelled once."}
	}
	if _, err := tx.Exec(ctx, `UPDATE nemura.goal SET state = $2, closed_at = $3 WHERE id = $1`, id, state, at); err != nil {
		return perfError(err)
	}
	return Audit(ctx, tx, org, at, user(member), "nemura.goal.closed", audit.Object{Type: "goal", ID: id}, map[string]audit.Change{"state": {"open", state}}, "")
}

// UpdateMeasure records the value of a key result or KPI now. A key result is changed by whoever may change its goal, a KPI by the person it is for, their line manager or the HR administrator or Owner.
func UpdateMeasure(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, member uuid.UUID, held []authz.Role, id uuid.UUID, value, note string) (api.Measure, error) {
	w, today := whoPerf(member, held), dateOf(at)
	if err := performance.CheckValue(value); err != nil {
		return api.Measure{}, err
	}
	if len([]rune(note)) > 300 {
		return api.Measure{}, &performance.Refusal{Code: performance.CodeInvalid, Message: "The note is at most 300 characters."}
	}
	var goal, person *uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT goal_id, person_id FROM nemura.measure WHERE id = $1`, id).Scan(&goal, &person); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return api.Measure{}, noGoal
		}
		return api.Measure{}, err
	}
	if goal != nil {
		_, state, read, change, err := goalRights(ctx, tx, w, *goal, today)
		if err != nil {
			return api.Measure{}, err
		}
		if !read || !change {
			return api.Measure{}, noGoal
		}
		if state != "open" {
			return api.Measure{}, &performance.Refusal{Code: performance.CodeConflict, Message: "That goal is closed: its key results are no longer updated."}
		}
	} else {
		self, manager, err := relation(ctx, tx, member, *person, today)
		if err != nil {
			return api.Measure{}, err
		}
		if !w.mayChange(self, manager) {
			return api.Measure{}, noGoal
		}
	}
	uid := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.measure_update (id, measure_id, value, note, by_member, at) VALUES ($1, $2, $3::numeric, $4, $5, $6)`, uid, id, value, orNil(strings.TrimSpace(note)), member, at); err != nil {
		return api.Measure{}, perfError(err)
	}
	if err := Audit(ctx, tx, org, at, user(member), "nemura.measure.updated", audit.Object{Type: "measure_update", ID: uid}, map[string]audit.Change{"measure": {nil, id.String()}}, ""); err != nil {
		return api.Measure{}, err
	}
	var from, due time.Time
	if goal != nil {
		if err := tx.QueryRow(ctx, `SELECT starts_on, due_on FROM nemura.goal WHERE id = $1`, *goal).Scan(&from, &due); err != nil {
			return api.Measure{}, err
		}
	}
	rows, err := tx.Query(ctx, measureSelect+`WHERE m.id = $1`, id)
	if err != nil {
		return api.Measure{}, err
	}
	ms, err := scanMeasures(rows, from, due, today)
	if err != nil || len(ms) == 0 {
		return api.Measure{}, err
	}
	return ms[0], nil
}

// SetKPI sets a role KPI for a person: by their line manager or the HR administrator or Owner, never by the person themselves.
func SetKPI(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, member uuid.UUID, held []authz.Role, person uuid.UUID, in api.MeasureInput) (api.Measure, error) {
	w, today := whoPerf(member, held), dateOf(at)
	ms, err := toPerfMeasures([]api.MeasureInput{in})
	if err != nil {
		return api.Measure{}, err
	}
	if err := performance.Check(ms[0]); err != nil {
		return api.Measure{}, err
	}
	self, manager, err := relation(ctx, tx, member, person, today)
	if err != nil {
		return api.Measure{}, err
	}
	if !(manager || w.admin) || (self && !w.admin) {
		return api.Measure{}, noGoal
	}
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.measure (id, person_id, title, unit, start_value, target, down, created_by, created_at) VALUES ($1, $2, $3, $4, $5::numeric, $6::numeric, $7, $8, $9)`,
		id, person, ms[0].Title, ms[0].Unit, ms[0].Start, ms[0].Target, ms[0].Down, member, at); err != nil {
		return api.Measure{}, perfError(err)
	}
	// a KPI is audited through its first update, which records the starting value and who set it
	uid := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.measure_update (id, measure_id, value, note, by_member, at) VALUES ($1, $2, $3::numeric, 'Set', $4, $5)`, uid, id, ms[0].Start, member, at); err != nil {
		return api.Measure{}, perfError(err)
	}
	if err := Audit(ctx, tx, org, at, user(member), "nemura.kpi.set", audit.Object{Type: "measure_update", ID: uid}, map[string]audit.Change{"person_id": {nil, person.String()}, "measure": {nil, id.String()}}, ""); err != nil {
		return api.Measure{}, err
	}
	list, err := kpiViews(ctx, tx, person, today)
	if err != nil {
		return api.Measure{}, err
	}
	for _, m := range list {
		if m.ID == id.String() {
			return m, nil
		}
	}
	return api.Measure{}, nil
}

// GoalsOfPerson reads a person's personal goals and KPIs, if the member is the person, their line manager or in the people team.
func GoalsOfPerson(ctx context.Context, tx pgx.Tx, member uuid.UUID, held []authz.Role, person uuid.UUID, today time.Time) ([]api.Goal, []api.Measure, error) {
	self, manager, err := relation(ctx, tx, member, person, today)
	if err != nil {
		return nil, nil, err
	}
	if !whoPerf(member, held).mayRead(self, manager) {
		return nil, nil, noGoal
	}
	goals, err := goalViews(ctx, tx, `g.person_id = $1`, []any{person}, today)
	if err != nil {
		return nil, nil, err
	}
	kpis, err := kpiViews(ctx, tx, person, today)
	return goals, kpis, err
}

// MyGoals reads the company's and the teams' goals and the caller's own goals and KPIs.
func MyGoals(ctx context.Context, tx pgx.Tx, member uuid.UUID, today time.Time) (company, team, mine []api.Goal, kpis []api.Measure, err error) {
	if company, err = goalViews(ctx, tx, `g.scope = 'company'`, nil, today); err != nil {
		return
	}
	if team, err = goalViews(ctx, tx, `g.scope = 'team'`, nil, today); err != nil {
		return
	}
	p, ok, err := personOfMember(ctx, tx, member)
	if err != nil || !ok {
		return
	}
	if mine, err = goalViews(ctx, tx, `g.person_id = $1`, []any{p}, today); err != nil {
		return
	}
	kpis, err = kpiViews(ctx, tx, p, today)
	return
}

// PerformanceService is goals and KPIs behind the API.
type PerformanceService struct {
	Store *Store
	Clock ports.Clock
}

var _ api.Performance = PerformanceService{}

func (p PerformanceService) in(ctx context.Context, s api.Session, fn func(ctx context.Context, tx pgx.Tx, at time.Time) error) error {
	at := p.Clock.Now()
	return p.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error { return fn(ctx, tx, at) })
}

func (p PerformanceService) Mine(ctx context.Context, s api.Session) (co, te, mine []api.Goal, kpis []api.Measure, err error) {
	err = p.in(ctx, s, func(ctx context.Context, tx pgx.Tx, at time.Time) (err error) {
		co, te, mine, kpis, err = MyGoals(ctx, tx, s.Member, dateOf(at))
		return
	})
	return
}

func (p PerformanceService) OfPerson(ctx context.Context, s api.Session, held []authz.Role, person uuid.UUID) (g []api.Goal, k []api.Measure, err error) {
	err = p.in(ctx, s, func(ctx context.Context, tx pgx.Tx, at time.Time) (err error) {
		g, k, err = GoalsOfPerson(ctx, tx, s.Member, held, person, dateOf(at))
		return
	})
	return
}

func (p PerformanceService) SetGoal(ctx context.Context, s api.Session, held []authz.Role, in api.GoalInput) (g api.Goal, err error) {
	err = p.in(ctx, s, func(ctx context.Context, tx pgx.Tx, at time.Time) (err error) {
		g, err = SetGoal(ctx, tx, s.Organisation, at, s.Member, held, in)
		return
	})
	return
}

func (p PerformanceService) CloseGoal(ctx context.Context, s api.Session, held []authz.Role, id uuid.UUID, state string) error {
	return p.in(ctx, s, func(ctx context.Context, tx pgx.Tx, at time.Time) error {
		return CloseGoal(ctx, tx, s.Organisation, at, s.Member, held, id, state)
	})
}

func (p PerformanceService) Update(ctx context.Context, s api.Session, held []authz.Role, measure uuid.UUID, value, note string) (m api.Measure, err error) {
	err = p.in(ctx, s, func(ctx context.Context, tx pgx.Tx, at time.Time) (err error) {
		m, err = UpdateMeasure(ctx, tx, s.Organisation, at, s.Member, held, measure, value, note)
		return
	})
	return
}

func (p PerformanceService) SetKPI(ctx context.Context, s api.Session, held []authz.Role, person uuid.UUID, in api.MeasureInput) (m api.Measure, err error) {
	err = p.in(ctx, s, func(ctx context.Context, tx pgx.Tx, at time.Time) (err error) {
		m, err = SetKPI(ctx, tx, s.Organisation, at, s.Member, held, person, in)
		return
	})
	return
}
