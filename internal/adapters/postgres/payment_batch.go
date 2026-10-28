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
	return storage.PaymentBatchRelease{ID: r.ID, BatchID: r.BatchID, BatchVersion: int(r.BatchVersion), ApprovalID: r.ApprovalID, ReleasedBy: r.ReleasedBy,
		ReleasedAt: r.ReleasedAt, Writer: r.Writer, FileName: r.FileName, MediaType: r.MediaType, Sealed: r.FileSealed, SHA256: r.FileSha256,
		Payments: int(r.Payments), Currency: r.Currency, Total: storage.Decimal(r.Total)}
}

func (r paymentBatches) Release(ctx context.Context, x storage.PaymentBatchRelease) (storage.PaymentBatchRelease, error) {
	if err := r.u.check(); err != nil {
		return storage.PaymentBatchRelease{}, err
	}
	row, err := r.u.q.InsertPaymentBatchRelease(ctx, sqlcdb.InsertPaymentBatchReleaseParams{BatchID: x.BatchID, BatchVersion: small(x.BatchVersion),
		ApprovalID: x.ApprovalID, ReleasedBy: x.ReleasedBy, ReleasedAt: x.ReleasedAt, Writer: x.Writer, FileName: x.FileName, MediaType: x.MediaType,
		FileSealed: x.Sealed, FileSha256: x.SHA256, Payments: small(x.Payments), Total: string(x.Total), Currency: x.Currency})
	if err != nil {
		return storage.PaymentBatchRelease{}, mapError(err)
	}
	out := toPaymentBatchRelease(row)
	v := out.BatchVersion
	// The file's name and hash only: never its content, which holds full account numbers (AUD-02).
	if err := r.u.event(ctx, "payment_batch.released", "document", out.BatchID, &v, map[string][2]any{
		"release": {nil, map[string]any{"id": out.ID.String(), "approval_id": out.ApprovalID.String(), "writer": out.Writer, "file_name": out.FileName,
			"sha256": hex.EncodeToString(out.SHA256), "payments": out.Payments, "total": map[string]any{"amount": string(out.Total), "currency": out.Currency}}},
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
