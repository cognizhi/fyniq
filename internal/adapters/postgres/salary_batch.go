package postgres

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ salary payment batches (FR-PAY-11, FR-PPL-12, 14, 17)

func (u *unitOfWork) SalaryBatches() storage.SalaryBatches { return salaryBatches{u} }

type salaryBatches struct{ u *unitOfWork }

func toSalaryBatch(x sqlcdb.FyniqSalaryBatch) storage.SalaryBatch {
	out := storage.SalaryBatch{ID: x.ID, LinkID: x.LinkID, RunID: x.RunID, ReceiptID: x.ReceiptID, Seq: int(x.Seq), SourceSystem: x.SourceSystem, RunVersion: int(x.RunVersion),
		WageMonth: x.WageMonth, Status: x.Status, Version: int(x.Version), LinesVersion: int(x.LinesVersion), CreatedAt: x.CreatedAt, ChangedAt: x.ChangedAt}
	if x.PayDate.Valid {
		d := x.PayDate.Time
		out.PayDate = &d
	}
	out.MoneyAccountID = idOf(x.MoneyAccountID)
	if x.Method != nil {
		out.Method = *x.Method
	}
	if x.PreparedBy != nil {
		out.PreparedBy = *x.PreparedBy
	}
	if x.CancelReason != nil {
		out.CancelReason = *x.CancelReason
	}
	return out
}

func toSalaryLine(x sqlcdb.FyniqSalaryBatchLine) storage.SalaryLine {
	return storage.SalaryLine{ID: x.ID, BatchID: x.BatchID, LineRef: x.LineRef, PersonID: x.PersonID, BankDetailID: x.BankDetailID, EventID: x.EventID, LineVersion: int(x.LineVersion),
		BankDetailVersion: int(x.BankDetailVersion), Amount: storage.Decimal(x.Amount), BankDetailDigest: x.BankDetailDigest, Sealed: x.Sealed, CreatedAt: x.CreatedAt}
}

func (r salaryBatches) NextSeq(ctx context.Context) (int, error) {
	if err := r.u.check(); err != nil {
		return 0, err
	}
	n, err := r.u.q.NextSalaryBatchSeq(ctx)
	return int(n), mapError(err)
}

func (r salaryBatches) insertLines(ctx context.Context, batch uuid.UUID, lines []storage.SalaryLine, at time.Time) error {
	for _, l := range lines {
		if _, err := r.u.q.InsertSalaryBatchLine(ctx, sqlcdb.InsertSalaryBatchLineParams{BatchID: batch, LineRef: l.LineRef, LineVersion: small(l.LineVersion), PersonID: l.PersonID,
			Amount: string(l.Amount), BankDetailID: l.BankDetailID, BankDetailVersion: small(l.BankDetailVersion), BankDetailDigest: l.BankDetailDigest, Sealed: l.Sealed,
			EventID: l.EventID, CreatedAt: at}); err != nil {
			return mapError(err)
		}
	}
	return nil
}

func (r salaryBatches) Create(ctx context.Context, b storage.SalaryBatch) (storage.SalaryBatch, error) {
	if err := r.u.check(); err != nil {
		return storage.SalaryBatch{}, err
	}
	row, err := r.u.q.InsertSalaryBatch(ctx, sqlcdb.InsertSalaryBatchParams{Seq: small(b.Seq), LinkID: b.LinkID, SourceSystem: b.SourceSystem, RunID: b.RunID, RunVersion: small(b.RunVersion),
		ReceiptID: b.ReceiptID, WageMonth: b.WageMonth, LinesVersion: small(max(b.LinesVersion, 1)), CreatedAt: b.CreatedAt})
	if err != nil {
		return storage.SalaryBatch{}, mapError(err)
	}
	out := toSalaryBatch(row)
	v := out.Version
	// The audit names the batch and its run; never a person, an account or an amount (FR-PPL-17).
	return out, r.u.event(ctx, "salary_batch.received", "salary_batch", out.ID, &v, map[string][2]any{"run": {nil, out.RunID.String()}, "run_version": {nil, out.RunVersion},
		"number": {nil, fmt.Sprintf("SB-%04d", out.Seq)}})
}

func (r salaryBatches) AddLines(ctx context.Context, batch uuid.UUID, lines []storage.SalaryLine, at time.Time) error {
	if err := r.u.check(); err != nil {
		return err
	}
	b, err := r.u.q.SalaryBatch(ctx, batch)
	if err != nil {
		return mapError(err)
	}
	if err := r.insertLines(ctx, batch, lines, at); err != nil {
		return err
	}
	for _, l := range lines {
		if _, err := r.event(ctx, storage.SalaryLineEvent{BatchID: batch, LineRef: l.LineRef, PersonID: l.PersonID, RunID: b.RunID, State: storage.LineScheduled, Source: storage.LineByArrival,
			RecordedBy: "service:link", At: at}); err != nil {
			return err
		}
	}
	return r.u.event(ctx, "salary_batch.lines_added", "salary_batch", batch, nil, map[string][2]any{"lines": {nil, len(lines)}})
}

func (r salaryBatches) Get(ctx context.Context, id uuid.UUID, forUpdate bool) (storage.SalaryBatch, error) {
	if err := r.u.check(); err != nil {
		return storage.SalaryBatch{}, err
	}
	var row sqlcdb.FyniqSalaryBatch
	var err error
	if forUpdate {
		row, err = r.u.q.SalaryBatchForUpdate(ctx, id)
	} else {
		row, err = r.u.q.SalaryBatch(ctx, id)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.SalaryBatch{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.SalaryBatch{}, mapError(err)
	}
	return toSalaryBatch(row), nil
}

func (r salaryBatches) OfRun(ctx context.Context, source string, run uuid.UUID, version int) (storage.SalaryBatch, error) {
	if err := r.u.check(); err != nil {
		return storage.SalaryBatch{}, err
	}
	row, err := r.u.q.SalaryBatchOfRun(ctx, sqlcdb.SalaryBatchOfRunParams{SourceSystem: source, RunID: run, RunVersion: small(version)})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.SalaryBatch{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.SalaryBatch{}, mapError(err)
	}
	return toSalaryBatch(row), nil
}

func (r salaryBatches) List(ctx context.Context) ([]storage.SalaryBatch, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.SalaryBatches(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.SalaryBatch, len(rows))
	for i, x := range rows {
		out[i] = toSalaryBatch(x)
	}
	return out, nil
}

func (r salaryBatches) Lines(ctx context.Context, batch uuid.UUID) ([]storage.SalaryLine, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.SalaryBatchLines(ctx, batch)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.SalaryLine, len(rows))
	for i, x := range rows {
		out[i] = toSalaryLine(x)
	}
	return out, nil
}

func (r salaryBatches) Revise(ctx context.Context, batch uuid.UUID, linesVersion int, lines []storage.SalaryLine, at time.Time) (storage.SalaryBatch, error) {
	if err := r.u.check(); err != nil {
		return storage.SalaryBatch{}, err
	}
	if err := r.insertLines(ctx, batch, lines, at); err != nil {
		return storage.SalaryBatch{}, err
	}
	row, err := r.u.q.ReviseSalaryBatch(ctx, sqlcdb.ReviseSalaryBatchParams{ID: batch, LinesVersion: small(linesVersion), At: at})
	if err != nil {
		return storage.SalaryBatch{}, mapError(err)
	}
	out := toSalaryBatch(row)
	v := out.Version
	return out, r.u.event(ctx, "salary_batch.lines_revised", "salary_batch", out.ID, &v, map[string][2]any{"lines": {nil, len(lines)}, "lines_version": {nil, out.LinesVersion}})
}

func (r salaryBatches) Prepare(ctx context.Context, batch uuid.UUID, version int, account uuid.UUID, on time.Time, method, by string, at time.Time) (storage.SalaryBatch, error) {
	if err := r.u.check(); err != nil {
		return storage.SalaryBatch{}, err
	}
	row, err := r.u.q.PrepareSalaryBatch(ctx, sqlcdb.PrepareSalaryBatchParams{PayDate: pgDate(&on), MoneyAccountID: someID(account), Method: &method, PreparedBy: &by, At: at, ID: batch, Version: small(version)})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.SalaryBatch{}, &storage.Error{Code: storage.CodeVersion, Message: "the salary batch changed since it was read"}
	}
	if err != nil {
		return storage.SalaryBatch{}, mapError(err)
	}
	out := toSalaryBatch(row)
	v := out.Version
	return out, r.u.event(ctx, "salary_batch.prepared", "salary_batch", out.ID, &v, map[string][2]any{"money_account_id": {nil, account.String()}, "pay_date": {nil, on.Format(time.DateOnly)}, "method": {nil, method}})
}

func (r salaryBatches) Approve(ctx context.Context, batch uuid.UUID, approval uuid.UUID, at time.Time) (storage.SalaryBatch, error) {
	if err := r.u.check(); err != nil {
		return storage.SalaryBatch{}, err
	}
	row, err := r.u.q.ApproveSalaryBatch(ctx, sqlcdb.ApproveSalaryBatchParams{ID: batch, At: at})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.SalaryBatch{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.SalaryBatch{}, mapError(err)
	}
	out := toSalaryBatch(row)
	v := out.Version
	return out, r.u.event(ctx, "salary_batch.approved", "salary_batch", out.ID, &v, map[string][2]any{"approval_id": {nil, approval.String()}})
}

func (r salaryBatches) Cancel(ctx context.Context, batch uuid.UUID, reason string, at time.Time) (storage.SalaryBatch, error) {
	if err := r.u.check(); err != nil {
		return storage.SalaryBatch{}, err
	}
	row, err := r.u.q.CancelSalaryBatch(ctx, sqlcdb.CancelSalaryBatchParams{ID: batch, Reason: &reason, At: at})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.SalaryBatch{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.SalaryBatch{}, mapError(err)
	}
	out := toSalaryBatch(row)
	v := out.Version
	return out, r.u.event(ctx, "salary_batch.cancelled", "salary_batch", out.ID, &v, map[string][2]any{"reason": {nil, reason}})
}

func toSalaryEvent(x sqlcdb.FyniqSalaryLineEvent) storage.SalaryLineEvent {
	out := storage.SalaryLineEvent{ID: x.ID, BatchID: x.BatchID, LineRef: x.LineRef, PersonID: x.PersonID, RunID: x.RunID, State: x.State, Source: x.Source, RecordedBy: x.RecordedBy, At: x.At,
		BankLineID: idOf(x.BankLineID), EvidenceID: idOf(x.EvidenceID)}
	if x.InstructionID != nil {
		out.InstructionID = *x.InstructionID
	}
	if x.Amount != nil {
		d := storage.Decimal(*x.Amount)
		out.Amount = &d
	}
	if x.OnDate.Valid {
		t := x.OnDate.Time
		out.OnDate = &t
	}
	return out
}

func (r salaryBatches) Events(ctx context.Context, batch uuid.UUID) ([]storage.SalaryLineEvent, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.SalaryLineEvents(ctx, batch)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.SalaryLineEvent, len(rows))
	for i, x := range rows {
		out[i] = toSalaryEvent(x)
	}
	return out, nil
}

// event writes a line's state; the callers audit it once for what they did (a batch of lines is one audit, not one per person).
func (r salaryBatches) event(ctx context.Context, e storage.SalaryLineEvent) (uuid.UUID, error) {
	p := sqlcdb.InsertSalaryLineEventParams{BatchID: e.BatchID, LineRef: e.LineRef, PersonID: e.PersonID, RunID: e.RunID, State: e.State, Source: e.Source,
		RecordedBy: e.RecordedBy, At: e.At, BankLineID: someID(e.BankLineID), EvidenceID: someID(e.EvidenceID), OnDate: pgDate(e.OnDate)}
	if e.InstructionID != "" {
		p.InstructionID = &e.InstructionID
	}
	if e.Amount != nil {
		a := string(*e.Amount)
		p.Amount = &a
	}
	row, err := r.u.q.InsertSalaryLineEvent(ctx, p)
	return row.ID, mapError(err)
}

func (r salaryBatches) AddEvent(ctx context.Context, e storage.SalaryLineEvent) (uuid.UUID, error) {
	if err := r.u.check(); err != nil {
		return uuid.Nil, err
	}
	id, err := r.event(ctx, e)
	if err != nil {
		return uuid.Nil, err
	}
	return id, r.u.event(ctx, "salary_batch.line_state", "salary_batch", e.BatchID, nil, map[string][2]any{"line": {nil, e.LineRef.String()}, "state": {nil, e.State}, "source": {nil, e.Source}})
}

func (r salaryBatches) StatesOfRunPerson(ctx context.Context, run, person uuid.UUID) ([]string, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.SalaryLatestStatesOfRunPerson(ctx, sqlcdb.SalaryLatestStatesOfRunPersonParams{RunID: run, PersonID: person})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]string, len(rows))
	for i, x := range rows {
		out[i] = x.State
	}
	return out, nil
}

func toSalaryRelease(x sqlcdb.FyniqSalaryRelease) storage.SalaryRelease {
	return storage.SalaryRelease{ID: x.ID, BatchID: x.BatchID, ApprovalID: x.ApprovalID, ReleasedBy: x.ReleasedBy, BatchVersion: int(x.BatchVersion), Lines: int(x.Lines), ReleasedAt: x.ReleasedAt,
		Writer: x.Writer, FileName: x.FileName, MediaType: x.MediaType, Sealed: x.FileSealed, SHA256: x.FileSha256, Total: storage.Decimal(x.Total)}
}

func (r salaryBatches) Release(ctx context.Context, x storage.SalaryRelease, lines []storage.SalaryReleaseLine, at time.Time) (storage.SalaryRelease, error) {
	if err := r.u.check(); err != nil {
		return storage.SalaryRelease{}, err
	}
	on := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
	row, err := r.u.q.InsertSalaryRelease(ctx, sqlcdb.InsertSalaryReleaseParams{BatchID: x.BatchID, BatchVersion: small(x.BatchVersion), ApprovalID: x.ApprovalID, ReleasedBy: x.ReleasedBy,
		ReleasedAt: x.ReleasedAt, Writer: x.Writer, FileName: x.FileName, MediaType: x.MediaType, FileSealed: x.Sealed, FileSha256: x.SHA256, Lines: small(len(lines)), Total: string(x.Total)})
	if err != nil {
		return storage.SalaryRelease{}, mapError(err)
	}
	out := toSalaryRelease(row)
	for _, l := range lines {
		if err := r.u.q.InsertSalaryReleaseLine(ctx, sqlcdb.InsertSalaryReleaseLineParams{ReleaseID: out.ID, BatchID: out.BatchID, LineRef: l.LineRef, LineID: l.LineID, InstructionID: l.InstructionID}); err != nil {
			return storage.SalaryRelease{}, mapError(err)
		}
		// A person is in the line's own state history, which is not in the audit: the batch's audit below names counts.
		line, err := r.u.q.SalaryBatchLine(ctx, l.LineID)
		if err != nil {
			return storage.SalaryRelease{}, mapError(err)
		}
		id, err := r.event(ctx, storage.SalaryLineEvent{BatchID: out.BatchID, LineRef: l.LineRef, PersonID: line.PersonID, RunID: runOfBatch(ctx, r, out.BatchID), State: storage.LineReleased,
			Source: storage.LineByRelease, InstructionID: l.InstructionID, OnDate: &on, RecordedBy: "user:" + out.ReleasedBy.String(), At: at})
		if err != nil {
			return storage.SalaryRelease{}, err
		}
		out.StateEvents = append(out.StateEvents, id)
	}
	v := out.BatchVersion
	return out, r.u.event(ctx, "salary_batch.released", "salary_batch", out.BatchID, &v, map[string][2]any{"release": {nil, map[string]any{"id": out.ID.String(), "writer": out.Writer,
		"file_name": out.FileName, "sha256": hex.EncodeToString(out.SHA256), "lines": len(lines), "approval_id": out.ApprovalID.String()}}})
}

// runOfBatch is the run a batch is for, which each line's event names (an unknown outcome blocks by run and person).
func runOfBatch(ctx context.Context, r salaryBatches, batch uuid.UUID) uuid.UUID {
	b, err := r.u.q.SalaryBatch(ctx, batch)
	if err != nil {
		return uuid.Nil
	}
	return b.RunID
}

func (r salaryBatches) Releases(ctx context.Context, batch uuid.UUID) ([]storage.SalaryRelease, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.SalaryReleases(ctx, batch)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.SalaryRelease, len(rows))
	for i, x := range rows {
		out[i] = toSalaryRelease(x)
	}
	return out, nil
}

func (r salaryBatches) ReleaseOf(ctx context.Context, id uuid.UUID) (storage.SalaryRelease, error) {
	if err := r.u.check(); err != nil {
		return storage.SalaryRelease{}, err
	}
	row, err := r.u.q.SalaryRelease(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.SalaryRelease{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.SalaryRelease{}, mapError(err)
	}
	return toSalaryRelease(row), nil
}

func (r salaryBatches) ReleaseLines(ctx context.Context, batch uuid.UUID) ([]storage.SalaryReleaseLine, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.SalaryReleaseLines(ctx, batch)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.SalaryReleaseLine, len(rows))
	for i, x := range rows {
		out[i] = storage.SalaryReleaseLine{ID: x.ID, ReleaseID: x.ReleaseID, BatchID: x.BatchID, LineRef: x.LineRef, LineID: x.LineID, InstructionID: x.InstructionID}
	}
	return out, nil
}

func (r salaryBatches) IsRunParty(ctx context.Context, batch, user uuid.UUID) (bool, error) {
	if err := r.u.check(); err != nil {
		return false, err
	}
	party, err := r.u.q.SalaryIsRunParty(ctx, sqlcdb.SalaryIsRunPartyParams{Who: user, BatchID: batch})
	return party, mapError(err)
}

func (r salaryBatches) StatusOfEvent(ctx context.Context, event uuid.UUID) (storage.SalaryStatus, error) {
	if err := r.u.check(); err != nil {
		return storage.SalaryStatus{}, err
	}
	x, err := r.u.q.SalaryStatusOfEvent(ctx, event)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.SalaryStatus{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.SalaryStatus{}, mapError(err)
	}
	out := storage.SalaryStatus{EventID: x.ID, BatchID: x.BatchID, LineRef: x.LineRef, PersonID: x.PersonID, RunID: x.RunID, State: x.State, EvidenceID: idOf(x.EvidenceID), On: x.At}
	switch {
	case x.EventInstruction != nil:
		out.InstructionID = *x.EventInstruction
	case x.ReleaseInstruction != nil:
		out.InstructionID = *x.ReleaseInstruction
	}
	switch {
	case x.EventAmount != nil:
		out.Amount = storage.Decimal(*x.EventAmount)
	case x.LineAmount != nil:
		out.Amount = storage.Decimal(*x.LineAmount)
	}
	if x.OnDate.Valid {
		out.On = x.OnDate.Time
	}
	return out, nil
}
