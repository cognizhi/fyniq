package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) InvoiceDisputes() storage.InvoiceDisputes { return invoiceDisputes{u} }

type invoiceDisputes struct{ u *unitOfWork }

func toInvoiceDispute(r sqlcdb.GetInvoiceDisputeRow) storage.InvoiceDispute {
	d := storage.InvoiceDispute{ID: r.ID, InvoiceID: r.InvoiceID, Kind: r.Kind, Currency: str(r.Currency), Reason: str(r.Reason), Status: r.Status,
		Resolution: str(r.Resolution), Version: int(r.Version), CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, ResolvedBy: str(r.ResolvedBy), ResolvedAt: r.ResolvedAt}
	if r.PromisedOn.Valid {
		t := r.PromisedOn.Time.UTC()
		d.PromisedOn = &t
	}
	if r.Amount != nil {
		a := storage.Decimal(*r.Amount)
		d.Amount = &a
	}
	return d
}

func (d invoiceDisputes) Create(ctx context.Context, n storage.NewInvoiceDispute) (storage.InvoiceDispute, error) {
	if err := d.u.check(); err != nil {
		return storage.InvoiceDispute{}, err
	}
	var amount *string
	if n.Amount != nil {
		s := string(*n.Amount)
		amount = &s
	}
	var promised pgtype.Date
	if n.PromisedOn != nil {
		promised = pgtype.Date{Time: *n.PromisedOn, Valid: true}
	}
	r, err := d.u.q.InsertInvoiceDispute(ctx, sqlcdb.InsertInvoiceDisputeParams{InvoiceID: n.InvoiceID, Kind: n.Kind, Amount: amount, Currency: opt(n.Currency),
		Reason: opt(n.Reason), PromisedOn: promised, CreatedBy: d.u.actor(ctx), CreatedAt: n.At})
	if err != nil {
		return storage.InvoiceDispute{}, mapError(err)
	}
	out := toInvoiceDispute(sqlcdb.GetInvoiceDisputeRow(r))
	changes := map[string][2]any{"invoice_id": {nil, out.InvoiceID.String()}, "kind": {nil, out.Kind}, "reason": {nil, nilIfEmpty(out.Reason)}}
	if out.Amount != nil {
		changes["amount"] = [2]any{nil, string(*out.Amount)}
		changes["currency"] = [2]any{nil, out.Currency}
	}
	if out.PromisedOn != nil {
		changes["promised_on"] = [2]any{nil, out.PromisedOn.Format(time.DateOnly)}
	}
	return out, d.u.event(ctx, "invoice_dispute.created", "invoice_dispute", out.ID, nil, changes)
}

func (d invoiceDisputes) Get(ctx context.Context, id uuid.UUID) (storage.InvoiceDispute, error) {
	if err := d.u.check(); err != nil {
		return storage.InvoiceDispute{}, err
	}
	r, err := d.u.q.GetInvoiceDispute(ctx, id)
	if err != nil {
		return storage.InvoiceDispute{}, mapError(err)
	}
	return toInvoiceDispute(r), nil
}

func (d invoiceDisputes) OfInvoice(ctx context.Context, invoice uuid.UUID) ([]storage.InvoiceDispute, error) {
	if err := d.u.check(); err != nil {
		return nil, err
	}
	rows, err := d.u.q.InvoiceDisputesOfInvoice(ctx, invoice)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.InvoiceDispute, 0, len(rows))
	for _, r := range rows {
		out = append(out, toInvoiceDispute(sqlcdb.GetInvoiceDisputeRow(r)))
	}
	return out, nil
}

func (d invoiceDisputes) Open(ctx context.Context) ([]storage.InvoiceDispute, error) {
	if err := d.u.check(); err != nil {
		return nil, err
	}
	rows, err := d.u.q.OpenInvoiceDisputes(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.InvoiceDispute, 0, len(rows))
	for _, r := range rows {
		out = append(out, toInvoiceDispute(sqlcdb.GetInvoiceDisputeRow(r)))
	}
	return out, nil
}

func (d invoiceDisputes) Resolve(ctx context.Context, id uuid.UUID, resolution string, at time.Time, version int) (storage.InvoiceDispute, error) {
	if err := d.u.check(); err != nil {
		return storage.InvoiceDispute{}, err
	}
	r, err := d.u.q.ResolveInvoiceDispute(ctx, sqlcdb.ResolveInvoiceDisputeParams{ID: id, Version: int32(version), Resolution: opt(resolution), ResolvedBy: opt(d.u.actor(ctx)), ResolvedAt: &at})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.InvoiceDispute{}, &storage.Error{Code: storage.CodeVersion, Message: "the record was changed by someone else", Hint: "Reload it and try again"}
	}
	if err != nil {
		return storage.InvoiceDispute{}, mapError(err)
	}
	out := toInvoiceDispute(sqlcdb.GetInvoiceDisputeRow(r))
	v := out.Version
	return out, d.u.event(ctx, "invoice_dispute.resolved", "invoice_dispute", out.ID, &v, map[string][2]any{"status": {"open", "resolved"}, "resolution": {nil, resolution}})
}
