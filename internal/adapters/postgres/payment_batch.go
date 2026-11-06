package postgres

import (
	"context"
	"encoding/hex"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the releases of payment batches (FR-BNK-12)

func (u *unitOfWork) PaymentBatches() storage.PaymentBatches { return paymentBatches{u} }

type paymentBatches struct{ u *unitOfWork }

func toPaymentBatchRelease(r sqlcdb.FyniqPaymentBatchRelease) storage.PaymentBatchRelease {
	out := storage.PaymentBatchRelease{ID: r.ID, BatchID: r.BatchID, BatchVersion: int(r.BatchVersion), ReleasedBy: r.ReleasedBy,
		ReleasedAt: r.ReleasedAt, Writer: r.Writer, FileName: r.FileName, MediaType: r.MediaType, Sealed: r.FileSealed, SHA256: r.FileSha256,
		Payments: int(r.Payments), Currency: r.Currency, Total: storage.Decimal(r.Total)}
	if r.ApprovalID != nil {
		out.ApprovalID = *r.ApprovalID
	}
	if r.BreakGlassID != nil {
		out.BreakGlassID = *r.BreakGlassID
	}
	return out
}

// someID is id, or nil for uuid.Nil.
func someID(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

func (r paymentBatches) Release(ctx context.Context, x storage.PaymentBatchRelease) (storage.PaymentBatchRelease, error) {
	if err := r.u.check(); err != nil {
		return storage.PaymentBatchRelease{}, err
	}
	row, err := r.u.q.InsertPaymentBatchRelease(ctx, sqlcdb.InsertPaymentBatchReleaseParams{BatchID: x.BatchID, BatchVersion: small(x.BatchVersion),
		ApprovalID: someID(x.ApprovalID), BreakGlassID: someID(x.BreakGlassID), ReleasedBy: x.ReleasedBy, ReleasedAt: x.ReleasedAt, Writer: x.Writer, FileName: x.FileName, MediaType: x.MediaType,
		FileSealed: x.Sealed, FileSha256: x.SHA256, Payments: small(x.Payments), Total: string(x.Total), Currency: x.Currency})
	if err != nil {
		return storage.PaymentBatchRelease{}, mapError(err)
	}
	out := toPaymentBatchRelease(row)
	v := out.BatchVersion
	// The file's name and hash only: never its content, which holds full account numbers (AUD-02).
	if err := r.u.event(ctx, "payment_batch.released", "document", out.BatchID, &v, map[string][2]any{
		"release": {nil, releaseChange(out)},
	}); err != nil {
		return storage.PaymentBatchRelease{}, err
	}
	return out, nil
}

func (r paymentBatches) ReleaseOf(ctx context.Context, batchID uuid.UUID) (*storage.PaymentBatchRelease, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.PaymentBatchReleaseOf(ctx, batchID)
	if err != nil {
		return nil, mapError(err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out := toPaymentBatchRelease(rows[0])
	return &out, nil
}

func (r paymentBatches) Link(ctx context.Context, releaseID, paymentID uuid.UUID) error {
	if err := r.u.check(); err != nil {
		return err
	}
	if err := r.u.q.InsertPaymentBatchPayment(ctx, sqlcdb.InsertPaymentBatchPaymentParams{ReleaseID: releaseID, PaymentID: paymentID}); err != nil {
		return mapError(err)
	}
	return r.u.event(ctx, "document.paid_in_batch", "document", paymentID, nil, map[string][2]any{"release_id": {nil, releaseID.String()}})
}

func (r paymentBatches) Payments(ctx context.Context, releaseID uuid.UUID) ([]uuid.UUID, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	ids, err := r.u.q.PaymentBatchPayments(ctx, releaseID)
	return ids, mapError(err)
}

func (r paymentBatches) ReleaseOfPayment(ctx context.Context, paymentID uuid.UUID) (*storage.PaymentBatchRelease, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.PaymentBatchReleaseOfPayment(ctx, paymentID)
	if err != nil || len(rows) == 0 {
		return nil, mapError(err)
	}
	out := toPaymentBatchRelease(rows[0])
	return &out, nil
}

// releaseChange is what a release's audit event holds: under an approval or a break-glass, and the file's name and hash.
func releaseChange(r storage.PaymentBatchRelease) map[string]any {
	out := map[string]any{"id": r.ID.String(), "writer": r.Writer, "file_name": r.FileName, "sha256": hex.EncodeToString(r.SHA256),
		"payments": r.Payments, "total": map[string]any{"amount": string(r.Total), "currency": r.Currency}}
	if r.BreakGlassID != uuid.Nil {
		out["break_glass_id"] = r.BreakGlassID.String()
	} else {
		out["approval_id"] = r.ApprovalID.String()
	}
	return out
}
