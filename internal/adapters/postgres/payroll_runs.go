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

// ------------------------------------------------------------------ pay runs from the people platform (FR-PPL-04, 09, 11)

func (u *unitOfWork) PayrollRuns() storage.PayrollRuns { return payrollRuns{u} }

type payrollRuns struct{ u *unitOfWork }

func (r payrollRuns) Mapping(ctx context.Context) (storage.PayrollMapping, error) {
	if err := r.u.check(); err != nil {
		return storage.PayrollMapping{}, err
	}
	m, err := r.u.q.LatestPayrollMapping(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.PayrollMapping{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.PayrollMapping{}, mapError(err)
	}
	rows, err := r.u.q.PayrollMappingEntries(ctx, m.ID)
	if err != nil {
		return storage.PayrollMapping{}, mapError(err)
	}
	out := storage.PayrollMapping{ID: m.ID, Version: int(m.Version), SetBy: m.SetBy, SetAt: m.SetAt, Entries: map[string]uuid.UUID{}}
	for _, e := range rows {
		out.Entries[e.Key] = e.AccountID
	}
	return out, nil
}

func (r payrollRuns) SetMapping(ctx context.Context, entries map[string]uuid.UUID, by string, at time.Time) (storage.PayrollMapping, error) {
	if err := r.u.check(); err != nil {
		return storage.PayrollMapping{}, err
	}
	next := int32(1)
	if cur, err := r.u.q.LatestPayrollMapping(ctx); err == nil {
		next = cur.Version + 1
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return storage.PayrollMapping{}, mapError(err)
	}
	m, err := r.u.q.InsertPayrollMapping(ctx, sqlcdb.InsertPayrollMappingParams{Version: next, SetBy: by, SetAt: at})
	if err != nil {
		return storage.PayrollMapping{}, mapError(err)
	}
	keys := map[string]string{}
	for k, acc := range entries {
		if err := r.u.q.InsertPayrollMappingEntry(ctx, sqlcdb.InsertPayrollMappingEntryParams{MappingID: m.ID, Key: k, AccountID: acc}); err != nil {
			return storage.PayrollMapping{}, mapError(err)
		}
		keys[k] = acc.String()
	}
	v := int(m.Version)
	if err := r.u.event(ctx, "payroll_mapping.set", "payroll_mapping", m.ID, &v, map[string][2]any{"entries": {nil, keys}}); err != nil {
		return storage.PayrollMapping{}, err
	}
	return storage.PayrollMapping{ID: m.ID, Version: v, Entries: entries, SetBy: by, SetAt: at}, nil
}

func toReceipt(x sqlcdb.LatestPayrollReceiptRow) (storage.PayrollRunReceipt, error) {
	out := storage.PayrollRunReceipt{ID: x.ID, LinkID: x.LinkID, Epoch: int(x.Epoch), SourceSystem: x.SourceSystem, RunID: x.RunID, Version: int(x.Version),
		WageMonth: x.WageMonth, EnvelopeSHA256: x.EnvelopeSha256, ApproverIssuer: x.ApproverIssuer, ApproverSubject: x.ApproverSubject, PreparerIssuer: x.PreparerIssuer,
		PreparerSubject: x.PreparerSubject, MappingVersion: int(x.MappingVersion), EntryID: idOf(x.EntryID), ReceivedAt: x.ReceivedAt}
	return out, json.Unmarshal(x.Totals, &out.Totals)
}

func (r payrollRuns) LatestReceipt(ctx context.Context, source string, run uuid.UUID) (storage.PayrollRunReceipt, error) {
	if err := r.u.check(); err != nil {
		return storage.PayrollRunReceipt{}, err
	}
	x, err := r.u.q.LatestPayrollReceipt(ctx, sqlcdb.LatestPayrollReceiptParams{SourceSystem: source, RunID: run})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.PayrollRunReceipt{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.PayrollRunReceipt{}, mapError(err)
	}
	return toReceipt(x)
}

func (r payrollRuns) EnvelopeUsed(ctx context.Context, sha string) (bool, error) {
	if err := r.u.check(); err != nil {
		return false, err
	}
	used, err := r.u.q.PayrollEnvelopeUsed(ctx, sha)
	return used, mapError(err)
}

func (r payrollRuns) Receive(ctx context.Context, x storage.PayrollRunReceipt) (storage.PayrollRunReceipt, error) {
	if err := r.u.check(); err != nil {
		return storage.PayrollRunReceipt{}, err
	}
	totals, err := json.Marshal(x.Totals)
	if err != nil {
		return storage.PayrollRunReceipt{}, err
	}
	id, err := r.u.q.InsertPayrollReceipt(ctx, sqlcdb.InsertPayrollReceiptParams{LinkID: x.LinkID, Epoch: int32(x.Epoch), SourceSystem: x.SourceSystem, RunID: x.RunID, // #nosec G115 -- bounded by the CHECKs
		Version: int32(x.Version), WageMonth: x.WageMonth, EnvelopeSha256: x.EnvelopeSHA256, ApproverIssuer: x.ApproverIssuer, // #nosec G115 -- a version counts up
		ApproverSubject: x.ApproverSubject, PreparerIssuer: x.PreparerIssuer, PreparerSubject: x.PreparerSubject, MappingVersion: int32(x.MappingVersion), // #nosec G115 -- as above
		EntryID: optionalID(x.EntryID), Totals: totals, ReceivedAt: x.ReceivedAt})
	if err != nil {
		return storage.PayrollRunReceipt{}, mapError(err)
	}
	x.ID = id
	v := x.Version
	// The audit trail names the run, its version, who approved it and the entry; never a figure.
	return x, r.u.event(ctx, "payroll_run.received", "payroll_run_receipt", id, &v, map[string][2]any{"run": {nil, x.RunID.String()}, "version": {nil, x.Version},
		"approver": {nil, x.ApproverIssuer + "|" + x.ApproverSubject}, "mapping_version": {nil, x.MappingVersion}, "entry_id": {nil, x.EntryID.String()}})
}

func (r payrollRuns) ReceiptMonths(ctx context.Context) ([]time.Time, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	months, err := r.u.q.PayrollReceiptMonths(ctx)
	return months, mapError(err)
}

func (r payrollRuns) RecordRefusal(ctx context.Context, link, run uuid.UUID, version int, code string, at time.Time) error {
	if err := r.u.check(); err != nil {
		return err
	}
	id, err := r.u.q.InsertPayrollRunRefusal(ctx, sqlcdb.InsertPayrollRunRefusalParams{LinkID: link, RunID: run, Version: int32(version), Code: code, RefusedAt: at}) // #nosec G115 -- a version is small
	if err != nil {
		return mapError(err)
	}
	return r.u.event(ctx, "payroll_run_refusal.recorded", "payroll_run_refusal", id, nil, map[string][2]any{"run_id": {nil, run.String()}, "version": {nil, version}, "code": {nil, code}})
}

func (r payrollRuns) OpenRefusals(ctx context.Context) ([]storage.PayrollRunRefusal, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.OpenPayrollRunRefusals(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.PayrollRunRefusal, len(rows))
	for i, x := range rows {
		out[i] = storage.PayrollRunRefusal{RunID: x.RunID, Version: int(x.Version), Code: x.Code, RefusedAt: x.RefusedAt}
	}
	return out, nil
}
