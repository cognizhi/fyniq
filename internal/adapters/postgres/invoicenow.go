package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) InvoiceNow() storage.InvoiceNow { return invoiceNow{u} }

type invoiceNow struct{ u *unitOfWork }

type invoiceSubmissionRow = sqlcdb.InsertInvoiceSubmissionRow

func toInvoiceSubmission(r invoiceSubmissionRow) storage.InvoiceSubmission {
	return storage.InvoiceSubmission{ID: r.ID, DocumentID: r.DocumentID, DocumentUUID: r.DocumentUuid, Version: int(r.Version), Kind: r.Kind, Direction: r.Direction,
		Status: r.Status, Recipient: str(r.Recipient), TransmissionID: str(r.TransmissionID), IRASAck: str(r.IrasAck), LastError: str(r.LastError),
		PayloadSHA256: r.PayloadSha256, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, CreatedBy: r.CreatedBy}
}

func submissionChanges(s storage.InvoiceSubmission) map[string][2]any {
	return map[string][2]any{"document_id": {nil, s.DocumentID.String()}, "document_uuid": {nil, s.DocumentUUID.String()}, "version": {nil, s.Version},
		"kind": {nil, s.Kind}, "direction": {nil, s.Direction}, "status": {nil, s.Status}}
}

func (r invoiceNow) Queue(ctx context.Context, n storage.NewInvoiceSubmission) (storage.InvoiceSubmission, error) {
	if err := r.u.check(); err != nil {
		return storage.InvoiceSubmission{}, err
	}
	docUUID, version := n.DocumentUUID, int32(1)
	if docUUID == uuid.Nil {
		docUUID = uuid.New()
	}
	last, err := r.u.q.InvoiceSubmissionLatestOfDocument(ctx, n.DocumentID)
	switch {
	case err == nil:
		docUUID, version = last.DocumentUuid, last.Version+1
	case !errors.Is(err, pgx.ErrNoRows):
		return storage.InvoiceSubmission{}, mapError(err)
	}
	row, err := r.u.q.InsertInvoiceSubmission(ctx, sqlcdb.InsertInvoiceSubmissionParams{DocumentID: n.DocumentID, DocumentUuid: docUUID, Version: version,
		Kind: n.Kind, Direction: n.Direction, Recipient: opt(n.Recipient), PayloadSha256: n.PayloadSHA256, At: n.At, CreatedBy: r.u.actor(ctx)})
	if err != nil {
		return storage.InvoiceSubmission{}, mapError(err)
	}
	out := toInvoiceSubmission(row)
	if err := r.u.q.InsertInvoiceEvent(ctx, sqlcdb.InsertInvoiceEventParams{SubmissionID: out.ID, At: n.At, Status: out.Status, Actor: r.u.actor(ctx)}); err != nil {
		return storage.InvoiceSubmission{}, mapError(err)
	}
	return out, r.u.event(ctx, "invoicenow.submission.queued", "invoicenow_submission", out.ID, nil, submissionChanges(out))
}

func (r invoiceNow) Submission(ctx context.Context, id uuid.UUID) (storage.InvoiceSubmission, error) {
	if err := r.u.check(); err != nil {
		return storage.InvoiceSubmission{}, err
	}
	row, err := r.u.q.InvoiceSubmissionOf(ctx, id)
	if err != nil {
		return storage.InvoiceSubmission{}, mapError(err)
	}
	return toInvoiceSubmission(invoiceSubmissionRow(row)), nil
}

func (r invoiceNow) Submissions(ctx context.Context, f storage.InvoiceSubmissionFilter) ([]storage.InvoiceSubmission, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.u.q.ListInvoiceSubmissions(ctx, sqlcdb.ListInvoiceSubmissionsParams{Status: f.Status, DocumentID: f.DocumentID, After: f.After, RowLimit: int32(limit)})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.InvoiceSubmission, 0, len(rows))
	for _, x := range rows {
		out = append(out, toInvoiceSubmission(invoiceSubmissionRow(x)))
	}
	return out, nil
}

func (r invoiceNow) Latest(ctx context.Context, documents []uuid.UUID) ([]storage.InvoiceSubmission, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	if len(documents) == 0 {
		return nil, nil
	}
	rows, err := r.u.q.LatestInvoiceSubmissions(ctx, documents)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.InvoiceSubmission, 0, len(rows))
	for _, x := range rows {
		out = append(out, toInvoiceSubmission(invoiceSubmissionRow(x)))
	}
	return out, nil
}

func (r invoiceNow) Move(ctx context.Context, id uuid.UUID, m storage.InvoiceSubmissionMove) (storage.InvoiceSubmission, error) {
	if err := r.u.check(); err != nil {
		return storage.InvoiceSubmission{}, err
	}
	before, err := r.Submission(ctx, id)
	if err != nil {
		return storage.InvoiceSubmission{}, err
	}
	row, err := r.u.q.MoveInvoiceSubmission(ctx, sqlcdb.MoveInvoiceSubmissionParams{ID: id, Status: m.Status, TransmissionID: opt(m.TransmissionID), IrasAck: opt(m.IRASAck),
		LastError: opt(m.Error), At: m.At})
	if err != nil {
		return storage.InvoiceSubmission{}, mapError(err)
	}
	out := toInvoiceSubmission(invoiceSubmissionRow(row))
	if err := r.u.q.InsertInvoiceEvent(ctx, sqlcdb.InsertInvoiceEventParams{SubmissionID: id, At: m.At, Status: m.Status, Detail: opt(firstOf(m.Error, m.IRASAck)), Actor: r.u.actor(ctx)}); err != nil {
		return storage.InvoiceSubmission{}, mapError(err)
	}
	return out, r.u.event(ctx, "invoicenow.submission."+m.Status, "invoicenow_submission", id, nil,
		map[string][2]any{"status": {before.Status, out.Status}, "iras_ack": {nil, nilIfEmpty(out.IRASAck)}, "transmission_id": {nil, nilIfEmpty(out.TransmissionID)}})
}

func firstOf(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (r invoiceNow) Events(ctx context.Context, id uuid.UUID) ([]storage.InvoiceEvent, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.InvoiceEventsOf(ctx, id)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.InvoiceEvent, 0, len(rows))
	for _, x := range rows {
		out = append(out, storage.InvoiceEvent{At: x.At, Status: x.Status, Detail: str(x.Detail), Actor: x.Actor})
	}
	return out, nil
}

type exclusionRow = sqlcdb.InsertInvoiceExclusionRow

func toExclusion(r exclusionRow) storage.InvoiceExclusion {
	return storage.InvoiceExclusion{ID: r.ID, DocumentID: r.DocumentID, Reason: r.Reason, Detail: str(r.Detail), RecordedBy: r.RecordedBy, RecordedAt: r.RecordedAt,
		WithdrawnAt: r.WithdrawnAt, WithdrawnBy: str(r.WithdrawnBy)}
}

func (r invoiceNow) Exclude(ctx context.Context, document uuid.UUID, reason, detail string, at time.Time) (storage.InvoiceExclusion, error) {
	if err := r.u.check(); err != nil {
		return storage.InvoiceExclusion{}, err
	}
	row, err := r.u.q.InsertInvoiceExclusion(ctx, sqlcdb.InsertInvoiceExclusionParams{DocumentID: document, Reason: reason, Detail: opt(detail), RecordedBy: r.u.actor(ctx), At: at})
	if err != nil {
		return storage.InvoiceExclusion{}, mapError(err)
	}
	out := toExclusion(row)
	return out, r.u.event(ctx, "invoicenow.exclusion.recorded", "invoicenow_exclusion", out.ID, nil,
		map[string][2]any{"document_id": {nil, document.String()}, "reason": {nil, reason}, "detail": {nil, nilIfEmpty(detail)}})
}

func (r invoiceNow) WithdrawExclusion(ctx context.Context, id uuid.UUID, at time.Time) (storage.InvoiceExclusion, error) {
	if err := r.u.check(); err != nil {
		return storage.InvoiceExclusion{}, err
	}
	by := r.u.actor(ctx)
	row, err := r.u.q.WithdrawInvoiceExclusion(ctx, sqlcdb.WithdrawInvoiceExclusionParams{ID: id, At: &at, By: &by})
	if errors.Is(err, pgx.ErrNoRows) {
		if _, gone := r.u.q.InvoiceExclusionExists(ctx, id); errors.Is(gone, pgx.ErrNoRows) {
			return storage.InvoiceExclusion{}, ports.ErrNotFound
		}
		return storage.InvoiceExclusion{}, &storage.Error{Code: storage.CodeVersion, Message: "the exclusion was withdrawn already"}
	}
	if err != nil {
		return storage.InvoiceExclusion{}, mapError(err)
	}
	out := toExclusion(exclusionRow(row))
	return out, r.u.event(ctx, "invoicenow.exclusion.withdrawn", "invoicenow_exclusion", id, nil, map[string][2]any{"withdrawn_at": {nil, at.UTC().Format(time.RFC3339)}})
}

func (r invoiceNow) Exclusions(ctx context.Context, documents []uuid.UUID) ([]storage.InvoiceExclusion, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	if len(documents) == 0 {
		return nil, nil
	}
	rows, err := r.u.q.LiveInvoiceExclusions(ctx, documents)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.InvoiceExclusion, 0, len(rows))
	for _, x := range rows {
		out = append(out, toExclusion(exclusionRow(x)))
	}
	return out, nil
}

type contingencyRow = sqlcdb.InsertInvoiceContingencyRow

func toContingency(r contingencyRow) storage.InvoiceContingency {
	return storage.InvoiceContingency{ID: r.ID, PeriodFrom: r.PeriodFrom, PeriodTo: r.PeriodTo, Reason: r.Reason, Evidence: r.Evidence, RequestedBy: r.RequestedBy,
		RequestedAt: r.RequestedAt, ApprovedBy: str(r.ApprovedBy), ApprovedAt: r.ApprovedAt, WithdrawnAt: r.WithdrawnAt}
}

func (r invoiceNow) RequestContingency(ctx context.Context, n storage.NewInvoiceContingency) (storage.InvoiceContingency, error) {
	if err := r.u.check(); err != nil {
		return storage.InvoiceContingency{}, err
	}
	row, err := r.u.q.InsertInvoiceContingency(ctx, sqlcdb.InsertInvoiceContingencyParams{PeriodFrom: n.PeriodFrom, PeriodTo: n.PeriodTo, Reason: n.Reason,
		Evidence: n.Evidence, RequestedBy: r.u.actor(ctx), At: n.At})
	if err != nil {
		return storage.InvoiceContingency{}, mapError(err)
	}
	out := toContingency(row)
	return out, r.u.event(ctx, "invoicenow.contingency.requested", "invoicenow_contingency", out.ID, nil,
		map[string][2]any{"period_from": {nil, n.PeriodFrom.Format(time.DateOnly)}, "period_to": {nil, n.PeriodTo.Format(time.DateOnly)}, "reason": {nil, n.Reason}})
}

func (r invoiceNow) ApproveContingency(ctx context.Context, id uuid.UUID, at time.Time) (storage.InvoiceContingency, error) {
	if err := r.u.check(); err != nil {
		return storage.InvoiceContingency{}, err
	}
	by := r.u.actor(ctx)
	row, err := r.u.q.ApproveInvoiceContingency(ctx, sqlcdb.ApproveInvoiceContingencyParams{ID: id, By: &by, At: &at})
	if errors.Is(err, pgx.ErrNoRows) {
		if _, gone := r.u.q.InvoiceContingencyOf(ctx, id); errors.Is(gone, pgx.ErrNoRows) {
			return storage.InvoiceContingency{}, ports.ErrNotFound
		}
		return storage.InvoiceContingency{}, &storage.Error{Code: storage.CodeVersion, Message: "the contingency was approved or withdrawn already"}
	}
	if err != nil {
		return storage.InvoiceContingency{}, mapError(err)
	}
	out := toContingency(contingencyRow(row))
	return out, r.u.event(ctx, "invoicenow.contingency.approved", "invoicenow_contingency", id, nil, map[string][2]any{"approved_at": {nil, at.UTC().Format(time.RFC3339)}})
}

func (r invoiceNow) Contingency(ctx context.Context, id uuid.UUID) (storage.InvoiceContingency, error) {
	if err := r.u.check(); err != nil {
		return storage.InvoiceContingency{}, err
	}
	row, err := r.u.q.InvoiceContingencyOf(ctx, id)
	if err != nil {
		return storage.InvoiceContingency{}, mapError(err)
	}
	return toContingency(contingencyRow(row)), nil
}

func (r invoiceNow) Contingencies(ctx context.Context, from, to time.Time) ([]storage.InvoiceContingency, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.InvoiceContingenciesCovering(ctx, sqlcdb.InvoiceContingenciesCoveringParams{PeriodFrom: from, PeriodTo: to})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.InvoiceContingency, 0, len(rows))
	for _, x := range rows {
		out = append(out, toContingency(contingencyRow(x)))
	}
	return out, nil
}

type invoiceInboundRow = sqlcdb.InsertInvoiceInboundRow

func toInvoiceInbound(r invoiceInboundRow) storage.InvoiceInbound {
	return storage.InvoiceInbound{ID: r.ID, MessageID: r.MessageID, Cursor: r.Cursor, Sender: r.Sender, Number: r.Number, ReceivedAt: r.ReceivedAt, SHA256: r.Sha256,
		Status: r.Status, BillID: r.BillID, Note: str(r.Note)}
}

func (r invoiceNow) KeepInbound(ctx context.Context, n storage.NewInvoiceInbound) (storage.InvoiceInbound, error) {
	if err := r.u.check(); err != nil {
		return storage.InvoiceInbound{}, err
	}
	status := n.Status
	if status == "" {
		status = "received"
	}
	sum := sha256.Sum256(n.Payload)
	row, err := r.u.q.InsertInvoiceInbound(ctx, sqlcdb.InsertInvoiceInboundParams{MessageID: n.MessageID, Cursor: n.Cursor, Sender: n.Sender, Number: n.Number, At: n.At,
		Sha256: sum[:], Payload: n.Payload, Status: status, BillID: n.BillID, Note: opt(n.Note), CreatedBy: r.u.actor(ctx)})
	if err != nil {
		return storage.InvoiceInbound{}, mapError(err)
	}
	out := toInvoiceInbound(row)
	return out, r.u.event(ctx, "invoicenow.inbound.received", "invoicenow_inbound", out.ID, nil,
		map[string][2]any{"status": {nil, out.Status}, "sender": {nil, out.Sender}, "number": {nil, out.Number}, "sha256": {nil, hex.EncodeToString(sum[:])}})
}

func (r invoiceNow) Inbound(ctx context.Context, id uuid.UUID) (storage.InvoiceInbound, error) {
	if err := r.u.check(); err != nil {
		return storage.InvoiceInbound{}, err
	}
	row, err := r.u.q.InvoiceInboundOf(ctx, id)
	if err != nil {
		return storage.InvoiceInbound{}, mapError(err)
	}
	return toInvoiceInbound(invoiceInboundRow(row)), nil
}

func (r invoiceNow) InboundPayload(ctx context.Context, id uuid.UUID) ([]byte, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	b, err := r.u.q.InvoiceInboundPayload(ctx, id)
	return b, mapError(err)
}

func (r invoiceNow) InboundPage(ctx context.Context, after *uuid.UUID, limit int) ([]storage.InvoiceInbound, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.u.q.ListInvoiceInbound(ctx, sqlcdb.ListInvoiceInboundParams{After: after, RowLimit: int32(limit)})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.InvoiceInbound, 0, len(rows))
	for _, x := range rows {
		out = append(out, toInvoiceInbound(invoiceInboundRow(x)))
	}
	return out, nil
}

func (r invoiceNow) InboundCursor(ctx context.Context) (string, error) {
	if err := r.u.check(); err != nil {
		return "", err
	}
	c, err := r.u.q.InvoiceInboundCursor(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return c, mapError(err)
}

func (r invoiceNow) InboundDuplicate(ctx context.Context, sender, number string) (*uuid.UUID, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	id, err := r.u.q.InvoiceInboundDuplicate(ctx, sqlcdb.InvoiceInboundDuplicateParams{Sender: sender, Number: number})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, mapError(err)
	}
	return &id, nil
}

func (r invoiceNow) SettleInbound(ctx context.Context, id uuid.UUID, status string, bill *uuid.UUID, note string) (storage.InvoiceInbound, error) {
	if err := r.u.check(); err != nil {
		return storage.InvoiceInbound{}, err
	}
	row, err := r.u.q.SettleInvoiceInbound(ctx, sqlcdb.SettleInvoiceInboundParams{ID: id, Status: status, BillID: bill, Note: opt(note)})
	if errors.Is(err, pgx.ErrNoRows) {
		if _, gone := r.u.q.InvoiceInboundOf(ctx, id); errors.Is(gone, pgx.ErrNoRows) {
			return storage.InvoiceInbound{}, ports.ErrNotFound
		}
		return storage.InvoiceInbound{}, &storage.Error{Code: storage.CodeVersion, Message: "the received document was drafted or refused already"}
	}
	if err != nil {
		return storage.InvoiceInbound{}, mapError(err)
	}
	out := toInvoiceInbound(invoiceInboundRow(row))
	return out, r.u.event(ctx, "invoicenow.inbound."+status, "invoicenow_inbound", id, nil, map[string][2]any{"status": {"received", status}})
}
