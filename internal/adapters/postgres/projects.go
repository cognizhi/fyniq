package postgres

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) Projects() storage.Projects { return projects{u} }

type projects struct{ u *unitOfWork }

func toProject(r sqlcdb.ProjectsRow) storage.Project {
	p := storage.Project{ID: r.ID, DimensionID: r.DimensionID, Code: r.Code, Name: r.Name, Active: r.IsActive, ValueVersion: int(r.ValueVersion),
		Configured: r.Configured, CustomerID: r.CustomerID, Currency: r.Currency, MinorUnits: int(r.MinorUnits), Status: r.Status,
		BillingMethod: r.BillingMethod, StartsOn: nullDate(r.StartsOn), EndsOn: nullDate(r.EndsOn), BudgetAmount: toDecPtr(r.BudgetAmount),
		Version: int(r.Version), CreatedAt: r.CreatedAt, ChangedAt: r.ChangedAt}
	if r.BudgetMinutes != nil {
		m := int(*r.BudgetMinutes)
		p.BudgetMinutes = &m
	}
	return p
}

// minutesParam is a budget in minutes as a nullable column takes it: the schema bounds it to 100 million, which the
// domain checks first.
func minutesParam(m *int) *int32 {
	if m == nil {
		return nil
	}
	v := small(*m)
	return &v
}

// dayText is a date as the audit trail records it: nil when not set.
func dayText(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format(time.DateOnly)
}

func minutesText(m *int) any {
	if m == nil {
		return nil
	}
	return *m
}

func (r projects) List(ctx context.Context) ([]storage.Project, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.Projects(ctx, nil)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.Project, 0, len(rows))
	for _, x := range rows {
		out = append(out, toProject(x))
	}
	return out, nil
}

func (r projects) Get(ctx context.Context, id uuid.UUID) (storage.Project, error) {
	if err := r.u.check(); err != nil {
		return storage.Project{}, err
	}
	rows, err := r.u.q.Projects(ctx, &id)
	if err != nil {
		return storage.Project{}, mapError(err)
	}
	if len(rows) == 0 {
		return storage.Project{}, ports.ErrNotFound
	}
	return toProject(rows[0]), nil
}

func (r projects) Create(ctx context.Context, n storage.NewProject) (storage.Project, error) {
	if err := r.u.check(); err != nil {
		return storage.Project{}, err
	}
	_, err := r.u.q.InsertProject(ctx, sqlcdb.InsertProjectParams{ID: n.ID, CustomerID: n.CustomerID, Currency: n.Currency, Status: n.Status,
		BillingMethod: n.BillingMethod, StartsOn: pgDate(n.StartsOn), EndsOn: pgDate(n.EndsOn), BudgetMinutes: minutesParam(n.BudgetMinutes),
		BudgetAmount: decPtr(n.BudgetAmount)})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.Project{}, &storage.Error{Code: storage.CodeInvalid, Message: "unknown currency " + n.Currency}
	}
	if err != nil {
		return storage.Project{}, mapError(err)
	}
	out, err := r.Get(ctx, n.ID)
	if err != nil {
		return out, err
	}
	return out, r.u.event(ctx, "project.created", "project", out.ID, &out.Version, map[string][2]any{"code": {nil, out.Code}, "name": {nil, out.Name},
		"customer_id": {nil, idText(out.CustomerID)}, "currency": {nil, out.Currency}, "status": {nil, out.Status},
		"billing_method": {nil, out.BillingMethod}, "starts_on": {nil, dayText(out.StartsOn)}, "ends_on": {nil, dayText(out.EndsOn)},
		"budget_minutes": {nil, minutesText(out.BudgetMinutes)}, "budget_amount": {nil, decText(out.BudgetAmount)}})
}

func (r projects) Change(ctx context.Context, id uuid.UUID, version int, c storage.ProjectChange) (storage.Project, error) {
	before, err := r.Get(ctx, id)
	if err != nil {
		return before, err
	}
	_, err = r.u.q.UpdateProject(ctx, sqlcdb.UpdateProjectParams{CustomerID: c.CustomerID, Status: c.Status, BillingMethod: c.BillingMethod,
		StartsOn: pgDate(c.StartsOn), EndsOn: pgDate(c.EndsOn), BudgetMinutes: minutesParam(c.BudgetMinutes), BudgetAmount: decPtr(c.BudgetAmount),
		ID: id, Version: small(version)})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.Project{}, &storage.Error{Code: storage.CodeVersion, Message: "the project " + before.Code + " changed since it was read"}
	}
	if err != nil {
		return storage.Project{}, mapError(err)
	}
	after, err := r.Get(ctx, id)
	if err != nil {
		return after, err
	}
	changes := map[string][2]any{}
	for _, f := range []struct {
		name          string
		before, after any
	}{{"customer_id", idText(before.CustomerID), idText(after.CustomerID)}, {"status", before.Status, after.Status},
		{"billing_method", before.BillingMethod, after.BillingMethod}, {"starts_on", dayText(before.StartsOn), dayText(after.StartsOn)},
		{"ends_on", dayText(before.EndsOn), dayText(after.EndsOn)}, {"budget_minutes", minutesText(before.BudgetMinutes), minutesText(after.BudgetMinutes)},
		{"budget_amount", decText(before.BudgetAmount), decText(after.BudgetAmount)}} {
		if f.before != f.after {
			changes[f.name] = [2]any{f.before, f.after}
		}
	}
	changes["version"] = [2]any{before.Version, after.Version}
	return after, r.u.event(ctx, "project.changed", "project", id, &after.Version, changes)
}

func toProjectMember(r sqlcdb.ProjectMembersRow) storage.ProjectMember {
	return storage.ProjectMember{ProjectID: r.ProjectID, UserID: r.UserID, Manager: r.IsManager, Active: r.IsActive}
}

func (r projects) Members(ctx context.Context, id uuid.UUID) ([]storage.ProjectMember, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ProjectMembers(ctx, id)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.ProjectMember, 0, len(rows))
	for _, x := range rows {
		out = append(out, toProjectMember(x))
	}
	return out, nil
}

func (r projects) SetMembers(ctx context.Context, id uuid.UUID, people []storage.ProjectPerson) error {
	existing, err := r.Members(ctx, id)
	if err != nil {
		return err
	}
	want := map[uuid.UUID]bool{} // person → manager
	for _, p := range people {
		want[p.UserID] = p.Manager
	}
	var changed bool
	for _, m := range existing {
		manager, named := want[m.UserID]
		switch {
		case named && (!m.Active || m.Manager != manager):
			changed = true
			if err := r.u.q.UpdateProjectMember(ctx, sqlcdb.UpdateProjectMemberParams{ProjectID: id, UserID: m.UserID, IsManager: manager, IsActive: true}); err != nil {
				return mapError(err)
			}
		case !named && m.Active:
			changed = true
			if err := r.u.q.UpdateProjectMember(ctx, sqlcdb.UpdateProjectMemberParams{ProjectID: id, UserID: m.UserID, IsManager: false, IsActive: false}); err != nil {
				return mapError(err)
			}
		}
	}
	for _, p := range people {
		if slices.IndexFunc(existing, func(m storage.ProjectMember) bool { return m.UserID == p.UserID }) >= 0 {
			continue
		}
		changed = true
		if err := r.u.q.InsertProjectMember(ctx, sqlcdb.InsertProjectMemberParams{ProjectID: id, UserID: p.UserID, IsManager: p.Manager}); err != nil {
			return mapError(err)
		}
	}
	if !changed {
		return nil
	}
	return r.u.event(ctx, "project.people_changed", "project", id, nil, map[string][2]any{
		"managers": {idTexts(managersOf(existing)), idTexts(managersWanted(people))},
		"members":  {idTexts(activeOf(existing)), idTexts(peopleIDs(people))}})
}

func managersOf(ms []storage.ProjectMember) []uuid.UUID {
	var out []uuid.UUID
	for _, m := range ms {
		if m.Active && m.Manager {
			out = append(out, m.UserID)
		}
	}
	sortIDs(out)
	return out
}

func activeOf(ms []storage.ProjectMember) []uuid.UUID {
	var out []uuid.UUID
	for _, m := range ms {
		if m.Active {
			out = append(out, m.UserID)
		}
	}
	sortIDs(out)
	return out
}

func managersWanted(ps []storage.ProjectPerson) []uuid.UUID {
	var out []uuid.UUID
	for _, p := range ps {
		if p.Manager {
			out = append(out, p.UserID)
		}
	}
	sortIDs(out)
	return out
}

func peopleIDs(ps []storage.ProjectPerson) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.UserID)
	}
	sortIDs(out)
	return out
}

func sortIDs(ids []uuid.UUID) {
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
}

func (r projects) OfUser(ctx context.Context, user uuid.UUID) ([]uuid.UUID, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	ids, err := r.u.q.ProjectsOfUser(ctx, user)
	if err != nil {
		return nil, mapError(err)
	}
	return ids, nil
}

func toProjectTask(r sqlcdb.FyniqProjectTask) storage.ProjectTask {
	return storage.ProjectTask{ID: r.ID, ProjectID: r.ProjectID, Name: r.Name, Billable: r.Billable, Active: r.IsActive, Version: int(r.Version)}
}

func (r projects) Tasks(ctx context.Context, id uuid.UUID) ([]storage.ProjectTask, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ProjectTasks(ctx, id)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.ProjectTask, 0, len(rows))
	for _, x := range rows {
		out = append(out, toProjectTask(x))
	}
	return out, nil
}

func (r projects) GetTask(ctx context.Context, id uuid.UUID) (storage.ProjectTask, error) {
	if err := r.u.check(); err != nil {
		return storage.ProjectTask{}, err
	}
	row, err := r.u.q.ProjectTask(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.ProjectTask{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.ProjectTask{}, mapError(err)
	}
	return toProjectTask(row), nil
}

func (r projects) CreateTask(ctx context.Context, project uuid.UUID, name string, billable bool) (storage.ProjectTask, error) {
	if err := r.u.check(); err != nil {
		return storage.ProjectTask{}, err
	}
	row, err := r.u.q.InsertProjectTask(ctx, sqlcdb.InsertProjectTaskParams{ProjectID: project, Name: name, Billable: billable})
	if err != nil {
		return storage.ProjectTask{}, mapError(err)
	}
	out := toProjectTask(row)
	return out, r.u.event(ctx, "project.task_added", "project", project, nil, map[string][2]any{"task_id": {nil, out.ID.String()},
		"name": {nil, out.Name}, "billable": {nil, out.Billable}})
}

func (r projects) ChangeTask(ctx context.Context, id uuid.UUID, version int, name string, billable, active bool) (storage.ProjectTask, error) {
	before, err := r.GetTask(ctx, id)
	if err != nil {
		return before, err
	}
	row, err := r.u.q.UpdateProjectTask(ctx, sqlcdb.UpdateProjectTaskParams{Name: name, Billable: billable, IsActive: active, ID: id, Version: small(version)})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.ProjectTask{}, &storage.Error{Code: storage.CodeVersion, Message: "the task " + before.Name + " changed since it was read"}
	}
	if err != nil {
		return storage.ProjectTask{}, mapError(err)
	}
	after := toProjectTask(row)
	changes := map[string][2]any{"task_id": {id.String(), id.String()}, "version": {before.Version, after.Version}}
	for _, f := range []struct {
		name          string
		before, after any
	}{{"name", before.Name, after.Name}, {"billable", before.Billable, after.Billable}, {"active", before.Active, after.Active}} {
		if f.before != f.after {
			changes[f.name] = [2]any{f.before, f.after}
		}
	}
	return after, r.u.event(ctx, "project.task_changed", "project", before.ProjectID, nil, changes)
}
