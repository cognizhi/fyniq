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

// ------------------------------------------------------------------ the Auditor's engagement, requests and samples (FR-UX-42)

func (u *unitOfWork) AuditEngagements() storage.AuditEngagements { return auditEngagements{u} }

type auditEngagements struct{ u *unitOfWork }

func toEngagement(r sqlcdb.FyniqAuditorEngagement) storage.AuditEngagement {
	return storage.AuditEngagement{ID: r.ID, Name: r.Name, Firm: r.Firm, AuditorUser: r.AuditorUserID, PeriodFrom: r.PeriodFrom, PeriodTo: r.PeriodTo, OpensOn: r.OpensOn,
		ClosesOn: r.ClosesOn, Status: r.Status, OpenedBy: r.OpenedBy, OpenedAt: r.OpenedAt, ConfirmedBy: deref(r.ConfirmedBy), ConfirmedAt: r.ConfirmedAt, ClosedBy: deref(r.ClosedBy), ClosedAt: r.ClosedAt}
}

func toAuditRequest(r sqlcdb.FyniqAuditorRequest) storage.AuditRequest {
	out := storage.AuditRequest{ID: r.ID, Engagement: r.EngagementID, Number: int(r.Number), Title: r.Title, Detail: r.Detail, Assignee: r.AssigneeUserID, Status: r.Status,
		RequestedBy: r.RequestedBy, RequestedAt: r.RequestedAt, Answer: deref(r.Answer), EvidenceIDs: r.EvidenceIds, AnsweredBy: deref(r.AnsweredBy), AnsweredAt: r.AnsweredAt, ClosedBy: deref(r.ClosedBy), ClosedAt: r.ClosedAt}
	if r.DueOn.Valid {
		d := r.DueOn.Time
		out.DueOn = &d
	}
	if out.EvidenceIDs == nil {
		out.EvidenceIDs = []uuid.UUID{}
	}
	return out
}

func eng(row sqlcdb.FyniqAuditorEngagement, err error) (storage.AuditEngagement, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.AuditEngagement{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.AuditEngagement{}, mapError(err)
	}
	return toEngagement(row), nil
}

func req(row sqlcdb.FyniqAuditorRequest, err error) (storage.AuditRequest, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.AuditRequest{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.AuditRequest{}, mapError(err)
	}
	return toAuditRequest(row), nil
}

func (r auditEngagements) List(ctx context.Context) ([]storage.AuditEngagement, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ListAuditEngagements(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.AuditEngagement, 0, len(rows))
	for _, x := range rows {
		out = append(out, toEngagement(x))
	}
	return out, nil
}

func (r auditEngagements) Get(ctx context.Context, id uuid.UUID) (storage.AuditEngagement, error) {
	if err := r.u.check(); err != nil {
		return storage.AuditEngagement{}, err
	}
	return eng(r.u.q.GetAuditEngagement(ctx, id))
}

func (r auditEngagements) Open(ctx context.Context, e storage.AuditEngagement) (storage.AuditEngagement, error) {
	if err := r.u.check(); err != nil {
		return storage.AuditEngagement{}, err
	}
	got, err := eng(r.u.q.InsertAuditEngagement(ctx, sqlcdb.InsertAuditEngagementParams{Name: e.Name, Firm: e.Firm, AuditorUserID: e.AuditorUser, PeriodFrom: e.PeriodFrom, PeriodTo: e.PeriodTo,
		OpensOn: e.OpensOn, ClosesOn: e.ClosesOn, OpenedBy: e.OpenedBy, OpenedAt: e.OpenedAt}))
	if err != nil {
		return got, err
	}
	return got, r.u.event(ctx, "auditor_engagement.opened", "auditor_engagement", got.ID, nil, map[string][2]any{"name": {nil, got.Name}, "auditor_user_id": {nil, got.AuditorUser.String()}, "status": {nil, got.Status}})
}

func (r auditEngagements) Confirm(ctx context.Context, id uuid.UUID, by string, at time.Time) (storage.AuditEngagement, error) {
	if err := r.u.check(); err != nil {
		return storage.AuditEngagement{}, err
	}
	row, err := r.u.q.ConfirmAuditEngagement(ctx, sqlcdb.ConfirmAuditEngagementParams{ID: id, ConfirmedBy: &by, ConfirmedAt: &at})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.AuditEngagement{}, &storage.Error{Code: storage.CodeAuditEngagement, Message: "the engagement is not pending"}
	}
	got, err := eng(row, err)
	if err != nil {
		return got, err
	}
	return got, r.u.event(ctx, "auditor_engagement.confirmed", "auditor_engagement", got.ID, nil, map[string][2]any{"status": {"pending", "active"}})
}

func (r auditEngagements) Close(ctx context.Context, id uuid.UUID, by string, at time.Time) (storage.AuditEngagement, error) {
	if err := r.u.check(); err != nil {
		return storage.AuditEngagement{}, err
	}
	row, err := r.u.q.CloseAuditEngagement(ctx, sqlcdb.CloseAuditEngagementParams{ID: id, ClosedBy: &by, ClosedAt: &at})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.AuditEngagement{}, &storage.Error{Code: storage.CodeAuditEngagement, Message: "the engagement is closed already"}
	}
	got, err := eng(row, err)
	if err != nil {
		return got, err
	}
	return got, r.u.event(ctx, "auditor_engagement.closed", "auditor_engagement", got.ID, nil, map[string][2]any{"status": {"open", "closed"}})
}

func (r auditEngagements) Requests(ctx context.Context, engagement uuid.UUID) ([]storage.AuditRequest, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ListAuditRequests(ctx, engagement)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.AuditRequest, 0, len(rows))
	for _, x := range rows {
		out = append(out, toAuditRequest(x))
	}
	return out, nil
}

func (r auditEngagements) Request(ctx context.Context, id uuid.UUID) (storage.AuditRequest, error) {
	if err := r.u.check(); err != nil {
		return storage.AuditRequest{}, err
	}
	return req(r.u.q.GetAuditRequest(ctx, id))
}

func (r auditEngagements) Ask(ctx context.Context, a storage.AuditRequest) (storage.AuditRequest, error) {
	if err := r.u.check(); err != nil {
		return storage.AuditRequest{}, err
	}
	n, err := r.u.q.NextAuditRequestNumber(ctx, a.Engagement)
	if err != nil {
		return storage.AuditRequest{}, mapError(err)
	}
	got, err := req(r.u.q.InsertAuditRequest(ctx, sqlcdb.InsertAuditRequestParams{EngagementID: a.Engagement, Number: n, Title: a.Title, Detail: a.Detail, DueOn: optDate(derefTime(a.DueOn)),
		AssigneeUserID: a.Assignee, RequestedBy: a.RequestedBy, RequestedAt: a.RequestedAt}))
	if err != nil {
		return got, err
	}
	return got, r.u.event(ctx, "auditor_request.asked", "auditor_request", got.ID, nil, map[string][2]any{"number": {nil, got.Number}, "title": {nil, got.Title}, "assignee": {nil, got.Assignee.String()}})
}

func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

func (r auditEngagements) Answer(ctx context.Context, id uuid.UUID, answer string, evidence []uuid.UUID, by string, at time.Time) (storage.AuditRequest, error) {
	if err := r.u.check(); err != nil {
		return storage.AuditRequest{}, err
	}
	if evidence == nil {
		evidence = []uuid.UUID{}
	}
	row, err := r.u.q.AnswerAuditRequest(ctx, sqlcdb.AnswerAuditRequestParams{ID: id, Answer: &answer, EvidenceIds: evidence, AnsweredBy: &by, AnsweredAt: &at})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.AuditRequest{}, &storage.Error{Code: storage.CodeAuditEngagement, Message: "the request is closed"}
	}
	got, err := req(row, err)
	if err != nil {
		return got, err
	}
	return got, r.u.event(ctx, "auditor_request.answered", "auditor_request", got.ID, nil, map[string][2]any{"status": {nil, got.Status}, "evidence": {nil, len(evidence)}})
}

func (r auditEngagements) CloseRequest(ctx context.Context, id uuid.UUID, by string, at time.Time) (storage.AuditRequest, error) {
	if err := r.u.check(); err != nil {
		return storage.AuditRequest{}, err
	}
	row, err := r.u.q.CloseAuditRequest(ctx, sqlcdb.CloseAuditRequestParams{ID: id, ClosedBy: &by, ClosedAt: &at})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.AuditRequest{}, &storage.Error{Code: storage.CodeAuditEngagement, Message: "the request is not answered"}
	}
	got, err := req(row, err)
	if err != nil {
		return got, err
	}
	return got, r.u.event(ctx, "auditor_request.closed", "auditor_request", got.ID, nil, map[string][2]any{"status": {"answered", "closed"}})
}

func (r auditEngagements) Samples(ctx context.Context, engagement uuid.UUID) ([]storage.AuditSample, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ListAuditSamples(ctx, engagement)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.AuditSample, 0, len(rows))
	for _, x := range rows {
		out = append(out, storage.AuditSample{ID: x.ID, Engagement: x.EngagementID, Rule: x.Rule, Seed: x.Seed, Population: int(x.Population), Items: x.Items, DrawnBy: x.DrawnBy, DrawnAt: x.DrawnAt})
	}
	return out, nil
}

func (r auditEngagements) Draw(ctx context.Context, s storage.AuditSample) (storage.AuditSample, error) {
	if err := r.u.check(); err != nil {
		return storage.AuditSample{}, err
	}
	x, err := r.u.q.InsertAuditSample(ctx, sqlcdb.InsertAuditSampleParams{EngagementID: s.Engagement, Rule: s.Rule, Seed: s.Seed, Population: int32(s.Population), Items: s.Items, DrawnBy: s.DrawnBy, DrawnAt: s.DrawnAt}) // #nosec G115 -- bounded by the caller
	if err != nil {
		return storage.AuditSample{}, mapError(err)
	}
	out := storage.AuditSample{ID: x.ID, Engagement: x.EngagementID, Rule: x.Rule, Seed: x.Seed, Population: int(x.Population), Items: x.Items, DrawnBy: x.DrawnBy, DrawnAt: x.DrawnAt}
	return out, r.u.event(ctx, "auditor_sample.drawn", "auditor_sample", out.ID, nil, map[string][2]any{"population": {nil, out.Population}, "seed": {nil, out.Seed}})
}
