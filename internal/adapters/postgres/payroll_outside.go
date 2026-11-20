package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ payroll outside Nemura (FR-PAY-15, FR-PPL-05)

func (u *unitOfWork) PayrollOutside() storage.PayrollOutside { return payrollOutside{u} }

type payrollOutside struct{ u *unitOfWork }

func toArrangement(x sqlcdb.FyniqPayrollArrangement) storage.PayrollArrangement {
	out := storage.PayrollArrangement{ID: x.ID, Item: x.Item, Method: x.Method, Note: x.Note, EvidenceID: idOf(x.EvidenceID), EffectiveFrom: x.EffectiveFrom, RecordedBy: x.RecordedBy, RecordedAt: x.RecordedAt}
	if x.Provider != nil {
		out.Provider = *x.Provider
	}
	return out
}

func (r payrollOutside) Arrangements(ctx context.Context, month time.Time) ([]storage.PayrollArrangement, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.PayrollArrangements(ctx, month)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.PayrollArrangement, len(rows))
	for i, x := range rows {
		out[i] = toArrangement(x)
	}
	return out, nil
}

func (r payrollOutside) ArrangementHistory(ctx context.Context) ([]storage.PayrollArrangement, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.PayrollArrangementHistory(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.PayrollArrangement, len(rows))
	for i, x := range rows {
		out[i] = toArrangement(x)
	}
	return out, nil
}

func (r payrollOutside) Arrange(ctx context.Context, a storage.PayrollArrangement) (storage.PayrollArrangement, error) {
	if err := r.u.check(); err != nil {
		return storage.PayrollArrangement{}, err
	}
	p := sqlcdb.InsertPayrollArrangementParams{Item: a.Item, Method: a.Method, Note: a.Note, EvidenceID: someID(a.EvidenceID), EffectiveFrom: a.EffectiveFrom, RecordedBy: a.RecordedBy, RecordedAt: a.RecordedAt}
	if a.Provider != "" {
		p.Provider = &a.Provider
	}
	row, err := r.u.q.InsertPayrollArrangement(ctx, p)
	if err != nil {
		return storage.PayrollArrangement{}, mapError(err)
	}
	out := toArrangement(row)
	return out, r.u.event(ctx, "payroll_arrangement.recorded", "payroll_arrangement", out.ID, nil, map[string][2]any{"item": {nil, out.Item}, "method": {nil, out.Method},
		"effective_from": {nil, out.EffectiveFrom.Format(time.DateOnly)}, "evidence_id": {nil, out.EvidenceID.String()}})
}

func toImport(x sqlcdb.FyniqPayrollImport) (storage.PayrollImport, error) {
	out := storage.PayrollImport{ID: x.ID, JournalID: x.JournalID, SourceSystem: x.SourceSystem, Period: x.Period, Version: int(x.Version), FileSHA256: x.FileSha256, LineCount: int(x.LineCount),
		CreatedBy: x.CreatedBy, CreatedAt: x.CreatedAt}
	return out, json.Unmarshal(x.Totals, &out.Totals)
}

func (r payrollOutside) Import(ctx context.Context, i storage.PayrollImport) (storage.PayrollImport, error) {
	if err := r.u.check(); err != nil {
		return storage.PayrollImport{}, err
	}
	totals, err := json.Marshal(i.Totals)
	if err != nil {
		return storage.PayrollImport{}, err
	}
	row, err := r.u.q.InsertPayrollImport(ctx, sqlcdb.InsertPayrollImportParams{SourceSystem: i.SourceSystem, Period: i.Period, Version: small(i.Version), FileSha256: i.FileSHA256, JournalID: i.JournalID,
		LineCount: small(i.LineCount), Totals: totals, CreatedBy: i.CreatedBy, CreatedAt: i.CreatedAt})
	if err != nil {
		return storage.PayrollImport{}, mapError(err)
	}
	out, err := toImport(row)
	if err != nil {
		return out, err
	}
	v := out.Version
	return out, r.u.event(ctx, "payroll_import.recorded", "payroll_import", out.ID, &v, map[string][2]any{"system": {nil, out.SourceSystem}, "period": {nil, out.Period.Format("2006-01")},
		"file_sha256": {nil, out.FileSHA256}, "lines": {nil, out.LineCount}, "journal_id": {nil, out.JournalID.String()}})
}

func (r payrollOutside) LatestImport(ctx context.Context, system string, period time.Time) (storage.PayrollImport, error) {
	if err := r.u.check(); err != nil {
		return storage.PayrollImport{}, err
	}
	row, err := r.u.q.LatestPayrollImport(ctx, sqlcdb.LatestPayrollImportParams{SourceSystem: system, Period: period})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.PayrollImport{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.PayrollImport{}, mapError(err)
	}
	return toImport(row)
}

func (r payrollOutside) ImportVersion(ctx context.Context, system string, period time.Time, version int) (storage.PayrollImport, error) {
	if err := r.u.check(); err != nil {
		return storage.PayrollImport{}, err
	}
	row, err := r.u.q.PayrollImportVersion(ctx, sqlcdb.PayrollImportVersionParams{SourceSystem: system, Period: period, Version: small(version)})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.PayrollImport{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.PayrollImport{}, mapError(err)
	}
	return toImport(row)
}

func (r payrollOutside) Imports(ctx context.Context) ([]storage.PayrollImport, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.PayrollImports(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.PayrollImport, 0, len(rows))
	for _, x := range rows {
		i, err := toImport(x)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, nil
}

var _ = uuid.Nil
