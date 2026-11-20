package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) ProjectRevenue() storage.ProjectRevenue { return projectRevenue{u} }

type projectRevenue struct{ u *unitOfWork }

func toElection(r sqlcdb.GetProjectRevenueElectionRow) storage.ProjectRevenueElection {
	e := storage.ProjectRevenueElection{ProjectID: r.ProjectID, Method: r.Method, ExcludedCost: storage.Decimal(r.ExcludedCost), ExcludedReason: r.ExcludedReason, Version: int(r.Version), SetBy: r.SetBy, SetAt: r.SetAt}
	if r.EstimatedCost != "" {
		d := storage.Decimal(r.EstimatedCost)
		e.EstimatedCost = &d
	}
	return e
}

func (p projectRevenue) Election(ctx context.Context, project uuid.UUID) (storage.ProjectRevenueElection, error) {
	if err := p.u.check(); err != nil {
		return storage.ProjectRevenueElection{}, err
	}
	r, err := p.u.q.GetProjectRevenueElection(ctx, project)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.ProjectRevenueElection{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.ProjectRevenueElection{}, mapError(err)
	}
	return toElection(r), nil
}

func (p projectRevenue) Elections(ctx context.Context) ([]storage.ProjectRevenueElection, error) {
	if err := p.u.check(); err != nil {
		return nil, err
	}
	rows, err := p.u.q.ListProjectRevenueElections(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.ProjectRevenueElection, 0, len(rows))
	for _, r := range rows {
		out = append(out, toElection(sqlcdb.GetProjectRevenueElectionRow(r)))
	}
	return out, nil
}

func (p projectRevenue) Elect(ctx context.Context, project uuid.UUID, version int, method string, estimated *storage.Decimal, excluded storage.Decimal, reason string, at time.Time) (storage.ProjectRevenueElection, error) {
	if err := p.u.check(); err != nil {
		return storage.ProjectRevenueElection{}, err
	}
	var est *string
	if estimated != nil {
		s := string(*estimated)
		est = &s
	}
	var out storage.ProjectRevenueElection
	var oldMethod any
	if version == 0 {
		r, err := p.u.q.InsertProjectRevenueElection(ctx, sqlcdb.InsertProjectRevenueElectionParams{ProjectID: project, Method: method, EstimatedCost: est, ExcludedCost: string(excluded), ExcludedReason: optStr(reason), SetBy: p.u.actor(ctx), SetAt: at})
		if err != nil {
			return storage.ProjectRevenueElection{}, mapError(err)
		}
		out = toElection(sqlcdb.GetProjectRevenueElectionRow(r))
	} else {
		old, err := p.Election(ctx, project)
		if err != nil {
			return storage.ProjectRevenueElection{}, err
		}
		oldMethod = old.Method
		r, err := p.u.q.UpdateProjectRevenueElection(ctx, sqlcdb.UpdateProjectRevenueElectionParams{ProjectID: project, Version: int32(version), Method: method, EstimatedCost: est, ExcludedCost: string(excluded), ExcludedReason: optStr(reason), SetBy: p.u.actor(ctx), SetAt: at})
		if errors.Is(err, pgx.ErrNoRows) {
			return storage.ProjectRevenueElection{}, &storage.Error{Code: storage.CodeVersion, Message: "the election was changed by someone else", Hint: "Reload it and try again"}
		}
		if err != nil {
			return storage.ProjectRevenueElection{}, mapError(err)
		}
		out = toElection(sqlcdb.GetProjectRevenueElectionRow(r))
	}
	v := out.Version
	changes := map[string][2]any{"method": {oldMethod, out.Method}}
	if out.EstimatedCost != nil {
		changes["estimated_cost"] = [2]any{nil, string(*out.EstimatedCost)}
	}
	changes["excluded_cost"] = [2]any{nil, string(out.ExcludedCost)}
	if out.ExcludedReason != "" {
		changes["excluded_reason"] = [2]any{nil, out.ExcludedReason}
	}
	return out, p.u.event(ctx, "project_revenue_election.set", "project_revenue_election", project, &v, changes)
}

func toRun(r sqlcdb.InsertProjectRevenueRunRow) storage.ProjectRevenueRun {
	return storage.ProjectRevenueRun{ID: r.ID, ProjectID: r.ProjectID, Month: r.Month.UTC(), JournalID: r.JournalID, Adjustment: storage.Decimal(r.Adjustment), Earned: storage.Decimal(r.Earned),
		CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt}
}

func (p projectRevenue) Runs(ctx context.Context, project uuid.UUID) ([]storage.ProjectRevenueRun, error) {
	if err := p.u.check(); err != nil {
		return nil, err
	}
	rows, err := p.u.q.ProjectRevenueRunsOf(ctx, project)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.ProjectRevenueRun, 0, len(rows))
	for _, r := range rows {
		out = append(out, toRun(sqlcdb.InsertProjectRevenueRunRow(r)))
	}
	return out, nil
}

func (p projectRevenue) Record(ctx context.Context, n storage.NewProjectRevenueRun) (storage.ProjectRevenueRun, error) {
	if err := p.u.check(); err != nil {
		return storage.ProjectRevenueRun{}, err
	}
	r, err := p.u.q.InsertProjectRevenueRun(ctx, sqlcdb.InsertProjectRevenueRunParams{ProjectID: n.ProjectID, Month: n.Month, JournalID: n.JournalID, Adjustment: string(n.Adjustment),
		Earned: string(n.Earned), CreatedBy: p.u.actor(ctx), CreatedAt: n.At})
	if err != nil {
		var pg interface{ SQLState() string }
		if errors.As(err, &pg) && pg.SQLState() == "23505" {
			return storage.ProjectRevenueRun{}, &storage.Error{Code: storage.CodeInvalid, Message: "that month has a revenue journal proposed already", Hint: "Open the journal it made"}
		}
		return storage.ProjectRevenueRun{}, mapError(err)
	}
	out := toRun(r)
	return out, p.u.event(ctx, "project_revenue_run.created", "project_revenue_run", out.ID, nil, map[string][2]any{"project_id": {nil, out.ProjectID.String()},
		"month": {nil, out.Month.Format(time.DateOnly)}, "journal_id": {nil, out.JournalID.String()}, "adjustment": {nil, string(out.Adjustment)}, "earned": {nil, string(out.Earned)}})
}

// optStr is a text for a nullable column: nil when empty.
func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (p projectRevenue) DraftByFunction(ctx context.Context, project uuid.UUID, month time.Time, payload []byte, adjustment, earned storage.Decimal) (uuid.UUID, error) {
	if err := p.u.check(); err != nil {
		return uuid.Nil, err
	}
	doc, err := p.u.q.CreateProjectRevenueDraft(ctx, sqlcdb.CreateProjectRevenueDraftParams{ProjectID: project, Month: month, Payload: payload, Adjustment: string(adjustment), Earned: string(earned)})
	if err != nil {
		return uuid.Nil, mapError(err)
	}
	row, err := p.u.q.GetDocument(ctx, doc)
	if err != nil {
		return uuid.Nil, mapError(err)
	}
	pl, err := jsonValue(row.Payload)
	if err != nil {
		return uuid.Nil, err
	}
	v := int(row.CurrentVersion)
	if err := p.u.event(ctx, "document.created", "document", doc, &v, map[string][2]any{"type": {nil, row.Type}, "status": {nil, row.Status}, "payload": {nil, pl}}); err != nil {
		return uuid.Nil, err
	}
	runs, err := p.Runs(ctx, project)
	if err != nil {
		return uuid.Nil, err
	}
	for _, r := range runs {
		if r.JournalID == doc {
			return doc, p.u.event(ctx, "project_revenue_run.created", "project_revenue_run", r.ID, nil, map[string][2]any{"project_id": {nil, r.ProjectID.String()},
				"month": {nil, r.Month.Format(time.DateOnly)}, "journal_id": {nil, r.JournalID.String()}, "adjustment": {nil, string(r.Adjustment)}, "earned": {nil, string(r.Earned)}})
		}
	}
	return doc, errors.New("the run of the draft is not there")
}
