package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) PaymentLinks() storage.PaymentLinks { return paymentLinks{u} }

type paymentLinks struct{ u *unitOfWork }

func toPaymentLink(r sqlcdb.GetPaymentLinkRow) storage.PaymentLink {
	return storage.PaymentLink{ID: r.ID, ConnectorID: r.ConnectorID, InvoiceID: r.InvoiceID, ProviderRequestID: r.ProviderRequestID, URL: r.Url,
		Reference: r.Reference, Currency: r.Currency, Amount: storage.Decimal(r.Amount), Status: r.Status, ClosedReason: str(r.ClosedReason),
		ExpiresAt: r.ExpiresAt, Version: int(r.Version), CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, ClosedAt: r.ClosedAt}
}

func (l paymentLinks) Create(ctx context.Context, n storage.NewPaymentLink) (storage.PaymentLink, error) {
	if err := l.u.check(); err != nil {
		return storage.PaymentLink{}, err
	}
	r, err := l.u.q.InsertPaymentLink(ctx, sqlcdb.InsertPaymentLinkParams{ConnectorID: n.ConnectorID, InvoiceID: n.InvoiceID,
		ProviderRequestID: n.ProviderRequestID, Url: n.URL, Reference: n.Reference, Currency: n.Currency, Amount: string(n.Amount),
		ExpiresAt: n.ExpiresAt, CreatedBy: l.u.actor(ctx), CreatedAt: n.At})
	if err != nil {
		return storage.PaymentLink{}, mapError(err)
	}
	k := toPaymentLink(sqlcdb.GetPaymentLinkRow(r))
	return k, l.u.event(ctx, "payment_link.created", "payment_link", k.ID, nil, map[string][2]any{"connector_id": {nil, k.ConnectorID.String()},
		"invoice_id": {nil, k.InvoiceID.String()}, "provider_request_id": {nil, k.ProviderRequestID}, "reference": {nil, k.Reference},
		"amount": {nil, string(k.Amount)}, "currency": {nil, k.Currency}})
}

func (l paymentLinks) Get(ctx context.Context, id uuid.UUID) (storage.PaymentLink, error) {
	if err := l.u.check(); err != nil {
		return storage.PaymentLink{}, err
	}
	r, err := l.u.q.GetPaymentLink(ctx, id)
	if err != nil {
		return storage.PaymentLink{}, mapError(err)
	}
	return toPaymentLink(r), nil
}

func (l paymentLinks) OfInvoice(ctx context.Context, invoice uuid.UUID) ([]storage.PaymentLink, error) {
	if err := l.u.check(); err != nil {
		return nil, err
	}
	rows, err := l.u.q.PaymentLinksOfInvoice(ctx, invoice)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.PaymentLink, 0, len(rows))
	for _, r := range rows {
		out = append(out, toPaymentLink(sqlcdb.GetPaymentLinkRow(r)))
	}
	return out, nil
}

func (l paymentLinks) ByRequest(ctx context.Context, connector uuid.UUID, request string) (storage.PaymentLink, error) {
	if err := l.u.check(); err != nil {
		return storage.PaymentLink{}, err
	}
	r, err := l.u.q.PaymentLinkByRequest(ctx, sqlcdb.PaymentLinkByRequestParams{ConnectorID: connector, ProviderRequestID: request})
	if err != nil {
		return storage.PaymentLink{}, mapError(err)
	}
	return toPaymentLink(sqlcdb.GetPaymentLinkRow(r)), nil
}

func (l paymentLinks) Close(ctx context.Context, id uuid.UUID, status, reason string, at time.Time, version int) (storage.PaymentLink, error) {
	if err := l.u.check(); err != nil {
		return storage.PaymentLink{}, err
	}
	r, err := l.u.q.ClosePaymentLink(ctx, sqlcdb.ClosePaymentLinkParams{ID: id, Version: int32(version), Status: status, ClosedReason: opt(reason), ClosedAt: &at})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.PaymentLink{}, &storage.Error{Code: storage.CodeVersion, Message: "the payment link was changed by someone else", Hint: "Reload it and try again"}
	}
	if err != nil {
		return storage.PaymentLink{}, mapError(err)
	}
	k := toPaymentLink(sqlcdb.GetPaymentLinkRow(r))
	v := k.Version
	return k, l.u.event(ctx, "payment_link.closed", "payment_link", k.ID, &v, map[string][2]any{"status": {storage.LinkLive, status}, "reason": {nil, nilIfEmpty(reason)}})
}

func toLinkPayment(r sqlcdb.InsertPaymentLinkPaymentRow) storage.PaymentLinkPayment {
	return storage.PaymentLinkPayment{ID: r.ID, LinkID: r.LinkID, ConnectorID: r.ConnectorID, ProviderPaymentID: r.ProviderPaymentID, Currency: r.Currency,
		Amount: storage.Decimal(r.Amount), PaidAt: r.PaidAt, SeenAt: r.SeenAt}
}

func (l paymentLinks) RecordPayment(ctx context.Context, n storage.NewPaymentLinkPayment) (storage.PaymentLinkPayment, bool, error) {
	if err := l.u.check(); err != nil {
		return storage.PaymentLinkPayment{}, false, err
	}
	r, err := l.u.q.InsertPaymentLinkPayment(ctx, sqlcdb.InsertPaymentLinkPaymentParams{LinkID: n.LinkID, ConnectorID: n.ConnectorID,
		ProviderPaymentID: n.ProviderPaymentID, Currency: n.Currency, Amount: string(n.Amount), PaidAt: n.PaidAt, SeenAt: n.SeenAt})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.PaymentLinkPayment{}, false, nil // recorded already: the first row stands
	}
	if err != nil {
		return storage.PaymentLinkPayment{}, false, mapError(err)
	}
	p := toLinkPayment(r)
	return p, true, l.u.event(ctx, "payment_link_payment.recorded", "payment_link_payment", p.ID, nil, map[string][2]any{"link_id": {nil, p.LinkID.String()},
		"provider_payment_id": {nil, p.ProviderPaymentID}, "amount": {nil, string(p.Amount)}, "currency": {nil, p.Currency}})
}

func (l paymentLinks) PaymentByCharge(ctx context.Context, connector uuid.UUID, charge string) (storage.PaymentLinkPayment, error) {
	if err := l.u.check(); err != nil {
		return storage.PaymentLinkPayment{}, err
	}
	r, err := l.u.q.PaymentLinkPaymentByCharge(ctx, sqlcdb.PaymentLinkPaymentByChargeParams{ConnectorID: connector, ProviderPaymentID: charge})
	if err != nil {
		return storage.PaymentLinkPayment{}, mapError(err)
	}
	return toLinkPayment(sqlcdb.InsertPaymentLinkPaymentRow(r)), nil
}

func (l paymentLinks) PaymentsOf(ctx context.Context, link uuid.UUID) ([]storage.PaymentLinkPayment, error) {
	if err := l.u.check(); err != nil {
		return nil, err
	}
	rows, err := l.u.q.PaymentLinkPaymentsOf(ctx, link)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.PaymentLinkPayment, 0, len(rows))
	for _, r := range rows {
		out = append(out, toLinkPayment(sqlcdb.InsertPaymentLinkPaymentRow(r)))
	}
	return out, nil
}
