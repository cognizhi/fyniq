package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/domain/aiprice"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) AIQualifications() storage.AIQualifications { return aiQualifications{u} }

type aiQualifications struct{ u *unitOfWork }

func toAIQualification(r sqlcdb.LatestAIQualificationsRow) storage.AIQualification {
	q := storage.AIQualification{ID: r.ID, Agent: r.Agent, ConnectionID: r.ConnectionID, Model: r.Model, Version: r.Version, Suite: r.Suite,
		SuiteVersion: int(r.SuiteVersion), Threshold: aiprice.Trim(r.Threshold), Cases: int(r.Cases), Passed: int(r.Passed), Result: r.Result,
		Run: r.RunID, RanBy: r.RanBy, RanAt: r.RanAt, OutOfDateAt: r.OutOfDateAt}
	if r.OutOfDateWhy != nil {
		q.OutOfDateWhy = *r.OutOfDateWhy
	}
	return q
}

func (r aiQualifications) Latest(ctx context.Context, connection uuid.UUID) ([]storage.AIQualification, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.LatestAIQualifications(ctx, connection)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.AIQualification, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAIQualification(row))
	}
	return out, nil
}

func (r aiQualifications) Record(ctx context.Context, q storage.AIQualification) (storage.AIQualification, error) {
	if err := r.u.check(); err != nil {
		return storage.AIQualification{}, err
	}
	id, err := r.u.q.InsertAIQualification(ctx, sqlcdb.InsertAIQualificationParams{Agent: q.Agent, ConnectionID: q.ConnectionID, Model: q.Model,
		Version: q.Version, Suite: q.Suite, SuiteVersion: int32(min(max(q.SuiteVersion, 0), 1<<30)), Threshold: q.Threshold, // #nosec G115 -- capped
		Cases: int32(min(max(q.Cases, 0), 1<<30)), Passed: int32(min(max(q.Passed, 0), 1<<30)), Result: q.Result, RunID: q.Run, // #nosec G115 -- capped
		RanBy: q.RanBy, RanAt: q.RanAt})
	if err != nil {
		return storage.AIQualification{}, mapError(err)
	}
	q.ID = id
	return q, r.u.event(ctx, "ai_qualification.recorded", "ai_qualification", id, nil, map[string][2]any{"qualification": {nil, map[string]any{
		"agent": q.Agent, "connection_id": q.ConnectionID.String(), "model": q.Model, "version": q.Version, "suite": q.Suite,
		"suite_version": q.SuiteVersion, "threshold": q.Threshold, "cases": q.Cases, "passed": q.Passed, "result": q.Result, "run": q.Run.String()}}})
}

func (r aiQualifications) OutOfDate(ctx context.Context, id uuid.UUID, why string, at time.Time) error {
	if err := r.u.check(); err != nil {
		return err
	}
	if len(why) > 500 {
		why = why[:497] + "…"
	}
	n, err := r.u.q.AIQualificationOutOfDate(ctx, sqlcdb.AIQualificationOutOfDateParams{At: &at, Why: &why, ID: id})
	if err != nil {
		return mapError(err)
	}
	if n == 0 {
		return ports.ErrNotFound
	}
	return r.u.event(ctx, "ai_qualification.out_of_date", "ai_qualification", id, nil, map[string][2]any{"out_of_date": {nil, why}})
}

func (r aiQualifications) Due(ctx context.Context, before time.Time) ([]storage.AIQualification, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.DueAIQualifications(ctx, before)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.AIQualification, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAIQualification(sqlcdb.LatestAIQualificationsRow(row)))
	}
	return out, nil
}
