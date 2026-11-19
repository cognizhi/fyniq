package postgres

import (
	"context"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) Billing() storage.Billing { return billing{u} }

type billing struct{ u *unitOfWork }

func (b billing) Billed(ctx context.Context, project uuid.UUID) ([]storage.BilledItem, error) {
	if err := b.u.check(); err != nil {
		return nil, err
	}
	rows, err := b.u.q.BilledItems(ctx, project)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.BilledItem, len(rows))
	for i, r := range rows {
		out[i] = storage.BilledItem{Kind: r.Kind, RefID: r.RefID, Line: int(r.LineNo), InvoiceID: r.InvoiceID}
	}
	return out, nil
}

func (b billing) BillableDocuments(ctx context.Context, project uuid.UUID, after *uuid.UUID, limit int) ([]uuid.UUID, error) {
	if err := b.u.check(); err != nil {
		return nil, err
	}
	ids, err := b.u.q.BillableDocuments(ctx, sqlcdb.BillableDocumentsParams{ProjectID: project.String(), AfterID: after,
		MaxRows: int32(min(max(limit, 1), 1000))}) // #nosec G115 -- 1 to 1,000
	return ids, mapError(err)
}
