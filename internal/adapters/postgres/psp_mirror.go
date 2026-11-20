package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) PSPMirror() storage.PSPMirror { return pspMirror{u} }

type pspMirror struct{ u *unitOfWork }

func toPSPCustomer(r sqlcdb.GetPSPCustomerRow) storage.PSPCustomer {
	return storage.PSPCustomer{ID: r.ID, ConnectorID: r.ConnectorID, ProviderID: r.ProviderCustomerID, Name: str(r.Name), Email: str(r.Email), ContactID: r.ContactID, SeenAt: r.SeenAt}
}

func (m pspMirror) SaveCustomer(ctx context.Context, n storage.NewPSPCustomer) (storage.PSPCustomer, bool, error) {
	if err := m.u.check(); err != nil {
		return storage.PSPCustomer{}, false, err
	}
	r, err := m.u.q.UpsertPSPCustomer(ctx, sqlcdb.UpsertPSPCustomerParams{ConnectorID: n.ConnectorID, ProviderCustomerID: n.ProviderID, Name: opt(n.Name), Email: opt(n.Email), SeenAt: n.SeenAt})
	if errors.Is(err, pgx.ErrNoRows) {
		c, err := m.Customer(ctx, n.ConnectorID, n.ProviderID)
		return c, false, err // nothing changed
	}
	if err != nil {
		return storage.PSPCustomer{}, false, mapError(err)
	}
	c := toPSPCustomer(sqlcdb.GetPSPCustomerRow{ID: r.ID, ConnectorID: r.ConnectorID, ProviderCustomerID: r.ProviderCustomerID, Name: r.Name, Email: r.Email, ContactID: r.ContactID, SeenAt: r.SeenAt})
	action := "psp_customer.changed"
	if r.Inserted {
		action = "psp_customer.mirrored"
	}
	return c, true, m.u.event(ctx, action, "psp_customer", c.ID, nil, map[string][2]any{"connector_id": {nil, c.ConnectorID.String()}, "provider_customer_id": {nil, c.ProviderID}})
}

func (m pspMirror) Customer(ctx context.Context, connector uuid.UUID, providerID string) (storage.PSPCustomer, error) {
	if err := m.u.check(); err != nil {
		return storage.PSPCustomer{}, err
	}
	r, err := m.u.q.GetPSPCustomer(ctx, sqlcdb.GetPSPCustomerParams{ConnectorID: connector, ProviderCustomerID: providerID})
	if err != nil {
		return storage.PSPCustomer{}, mapError(err)
	}
	return toPSPCustomer(r), nil
}

func toPSPSubscription(r sqlcdb.UpsertPSPSubscriptionRow) storage.PSPSubscription {
	return storage.PSPSubscription{ID: r.ID, ConnectorID: r.ConnectorID, ProviderID: r.ProviderSubscriptionID, ProviderCustomerID: r.ProviderCustomerID, Status: r.Status,
		Currency: r.Currency, Amount: storage.Decimal(r.Amount), IntervalUnit: r.IntervalUnit, IntervalCount: int(r.IntervalCount), PeriodStart: nullDate(r.PeriodStart),
		PeriodEnd: nullDate(r.PeriodEnd), CancelAtPeriodEnd: r.CancelAtPeriodEnd, CanceledOn: nullDate(r.CanceledOn), SeenAt: r.SeenAt}
}

func (m pspMirror) SaveSubscription(ctx context.Context, n storage.NewPSPSubscription) (storage.PSPSubscription, bool, error) {
	if err := m.u.check(); err != nil {
		return storage.PSPSubscription{}, false, err
	}
	r, err := m.u.q.UpsertPSPSubscription(ctx, sqlcdb.UpsertPSPSubscriptionParams{ConnectorID: n.ConnectorID, ProviderSubscriptionID: n.ProviderID, ProviderCustomerID: n.ProviderCustomerID,
		Status: n.Status, Currency: n.Currency, Amount: string(n.Amount), IntervalUnit: n.IntervalUnit, IntervalCount: int32(n.IntervalCount), // #nosec G115 -- bounded by a CHECK
		PeriodStart: pgDate(n.PeriodStart), PeriodEnd: pgDate(n.PeriodEnd), CancelAtPeriodEnd: n.CancelAtPeriodEnd, CanceledOn: pgDate(n.CanceledOn), SeenAt: n.SeenAt})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.PSPSubscription{ConnectorID: n.ConnectorID, ProviderID: n.ProviderID}, false, nil // nothing changed
	}
	if err != nil {
		return storage.PSPSubscription{}, false, mapError(err)
	}
	s := toPSPSubscription(r)
	return s, true, m.u.event(ctx, "psp_subscription.mirrored", "psp_subscription", s.ID, nil, map[string][2]any{"provider_subscription_id": {nil, s.ProviderID},
		"status": {nil, s.Status}, "amount": {nil, string(s.Amount)}, "currency": {nil, s.Currency}})
}

func (m pspMirror) Subscriptions(ctx context.Context) ([]storage.PSPSubscription, error) {
	if err := m.u.check(); err != nil {
		return nil, err
	}
	rows, err := m.u.q.PSPSubscriptions(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.PSPSubscription, 0, len(rows))
	for _, r := range rows {
		out = append(out, toPSPSubscription(sqlcdb.UpsertPSPSubscriptionRow(r)))
	}
	return out, nil
}

func toPSPInvoice(r sqlcdb.GetPSPInvoiceRow) storage.PSPInvoice {
	return storage.PSPInvoice{ID: r.ID, ConnectorID: r.ConnectorID, ProviderID: r.ProviderInvoiceID, ProviderCustomerID: r.ProviderCustomerID,
		ProviderSubscriptionID: str(r.ProviderSubscriptionID), Number: str(r.Number), Status: r.Status, Currency: r.Currency, Total: storage.Decimal(r.Total),
		AmountPaid: storage.Decimal(r.AmountPaid), AttemptCount: int(r.AttemptCount), NextAttemptOn: nullDate(r.NextAttemptOn), Outcome: str(r.Outcome), HostedURL: str(r.HostedUrl),
		FinalizedOn: nullDate(r.FinalizedOn), DueOn: nullDate(r.DueOn), Lines: json.RawMessage(r.DraftLines), State: r.State, ParkedReason: str(r.ParkedReason),
		DocumentID: r.DocumentID, Version: int(r.Version), SeenAt: r.SeenAt}
}

func (m pspMirror) SaveInvoice(ctx context.Context, n storage.NewPSPInvoice) (storage.PSPInvoice, bool, error) {
	if err := m.u.check(); err != nil {
		return storage.PSPInvoice{}, false, err
	}
	lines := []byte(n.Lines)
	if len(lines) == 0 {
		lines = []byte("[]")
	}
	before, err := m.Invoice(ctx, n.ConnectorID, n.ProviderID)
	if errors.Is(err, ports.ErrNotFound) {
		r, err := m.u.q.InsertPSPInvoice(ctx, sqlcdb.InsertPSPInvoiceParams{ConnectorID: n.ConnectorID, ProviderInvoiceID: n.ProviderID, ProviderCustomerID: n.ProviderCustomerID,
			ProviderSubscriptionID: opt(n.ProviderSubscriptionID), Number: opt(n.Number), Status: n.Status, Currency: n.Currency, Total: string(n.Total), AmountPaid: string(n.AmountPaid),
			AttemptCount: int32(n.AttemptCount), NextAttemptOn: pgDate(n.NextAttemptOn), Outcome: opt(n.Outcome), HostedUrl: opt(n.HostedURL), FinalizedOn: pgDate(n.FinalizedOn), // #nosec G115 -- bounded by a CHECK
			DueOn: pgDate(n.DueOn), DraftLines: lines, SeenAt: n.SeenAt})
		if err != nil {
			return storage.PSPInvoice{}, false, mapError(err)
		}
		i := toPSPInvoice(sqlcdb.GetPSPInvoiceRow(r))
		return i, true, m.u.event(ctx, "psp_invoice.mirrored", "psp_invoice", i.ID, nil, map[string][2]any{"provider_invoice_id": {nil, i.ProviderID}, "status": {nil, i.Status},
			"total": {nil, string(i.Total)}, "currency": {nil, i.Currency}})
	}
	if err != nil {
		return storage.PSPInvoice{}, false, err
	}
	same := before.Status == n.Status && before.Number == n.Number && before.AttemptCount == n.AttemptCount && dayOf(before.NextAttemptOn) == dayOf(n.NextAttemptOn) &&
		before.Outcome == n.Outcome && before.HostedURL == n.HostedURL && dayOf(before.FinalizedOn) == dayOf(n.FinalizedOn) && dayOf(before.DueOn) == dayOf(n.DueOn) &&
		before.AmountPaid == n.AmountPaid && before.ProviderSubscriptionID == n.ProviderSubscriptionID &&
		(before.DocumentID != nil || (before.Total == n.Total && jsonSame(before.Lines, lines)))
	if same {
		return before, false, nil
	}
	r, err := m.u.q.UpdatePSPInvoice(ctx, sqlcdb.UpdatePSPInvoiceParams{ID: before.ID, ProviderSubscriptionID: opt(n.ProviderSubscriptionID), Number: opt(n.Number), Status: n.Status,
		AmountPaid: string(n.AmountPaid), AttemptCount: int32(n.AttemptCount), NextAttemptOn: pgDate(n.NextAttemptOn), Outcome: opt(n.Outcome), HostedUrl: opt(n.HostedURL), // #nosec G115 -- bounded by a CHECK
		FinalizedOn: pgDate(n.FinalizedOn), DueOn: pgDate(n.DueOn), Total: string(n.Total), DraftLines: lines, SeenAt: n.SeenAt})
	if err != nil {
		return storage.PSPInvoice{}, false, mapError(err)
	}
	i := toPSPInvoice(sqlcdb.GetPSPInvoiceRow(r))
	v := i.Version
	return i, true, m.u.event(ctx, "psp_invoice.changed", "psp_invoice", i.ID, &v, map[string][2]any{"status": {before.Status, i.Status}, "attempt_count": {before.AttemptCount, i.AttemptCount},
		"outcome": {nilIfEmpty(before.Outcome), nilIfEmpty(i.Outcome)}})
}

func jsonSame(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return string(a) == string(b)
	}
	xa, _ := json.Marshal(x)
	ya, _ := json.Marshal(y)
	return string(xa) == string(ya)
}

func (m pspMirror) Invoice(ctx context.Context, connector uuid.UUID, providerID string) (storage.PSPInvoice, error) {
	if err := m.u.check(); err != nil {
		return storage.PSPInvoice{}, err
	}
	r, err := m.u.q.GetPSPInvoice(ctx, sqlcdb.GetPSPInvoiceParams{ConnectorID: connector, ProviderInvoiceID: providerID})
	if err != nil {
		return storage.PSPInvoice{}, mapError(err)
	}
	return toPSPInvoice(r), nil
}

func (m pspMirror) Invoices(ctx context.Context, state string) ([]storage.PSPInvoice, error) {
	if err := m.u.check(); err != nil {
		return nil, err
	}
	rows, err := m.u.q.PSPInvoicesInState(ctx, state)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.PSPInvoice, 0, len(rows))
	for _, r := range rows {
		out = append(out, toPSPInvoice(sqlcdb.GetPSPInvoiceRow(r)))
	}
	return out, nil
}

func (m pspMirror) Park(ctx context.Context, id uuid.UUID, why string) error {
	if err := m.u.check(); err != nil {
		return err
	}
	if len(why) > 500 {
		why = why[:500]
	}
	if err := m.u.q.ParkPSPInvoice(ctx, sqlcdb.ParkPSPInvoiceParams{ID: id, ParkedReason: &why}); err != nil {
		return mapError(err)
	}
	return m.u.event(ctx, "psp_invoice.parked", "psp_invoice", id, nil, map[string][2]any{"state": {nil, storage.PSPInvoiceParked}, "reason": {nil, why}})
}

func (m pspMirror) MakeDraft(ctx context.Context, connector uuid.UUID, providerID string) (uuid.UUID, uuid.UUID, bool, error) {
	if err := m.u.check(); err != nil {
		return uuid.Nil, uuid.Nil, false, err
	}
	r, err := m.u.q.CreateProviderInvoiceDraft(ctx, sqlcdb.CreateProviderInvoiceDraftParams{ConnectorID: connector, ProviderInvoiceID: providerID})
	if err != nil {
		return uuid.Nil, uuid.Nil, false, mapError(err)
	}
	doc, err := m.u.q.GetDocument(ctx, r.DocumentID)
	if err != nil {
		return uuid.Nil, uuid.Nil, false, mapError(err)
	}
	payload, err := jsonValue(doc.Payload)
	if err != nil {
		return uuid.Nil, uuid.Nil, false, err
	}
	v := int(doc.CurrentVersion)
	if err := m.u.event(ctx, "document.created", "document", r.DocumentID, &v, map[string][2]any{"type": {nil, doc.Type}, "status": {nil, doc.Status}, "payload": {nil, payload}}); err != nil {
		return uuid.Nil, uuid.Nil, false, err
	}
	if r.ContactMade {
		if err := m.u.event(ctx, "contact.created", "contact", r.ContactID, nil, map[string][2]any{"source": {nil, "payment provider customer"}}); err != nil {
			return uuid.Nil, uuid.Nil, false, err
		}
	}
	inv, err := m.Invoice(ctx, connector, providerID)
	if err != nil {
		return uuid.Nil, uuid.Nil, false, err
	}
	iv := inv.Version
	if err := m.u.event(ctx, "psp_invoice.drafted", "psp_invoice", inv.ID, &iv, map[string][2]any{"state": {storage.PSPInvoiceMirrored, storage.PSPInvoiceDrafted}, "document_id": {nil, r.DocumentID.String()}}); err != nil {
		return uuid.Nil, uuid.Nil, false, err
	}
	cust, err := m.Customer(ctx, connector, inv.ProviderCustomerID)
	if err != nil {
		return uuid.Nil, uuid.Nil, false, err
	}
	return r.DocumentID, r.ContactID, r.ContactMade, m.u.event(ctx, "psp_customer.changed", "psp_customer", cust.ID, nil, map[string][2]any{"contact_id": {nil, r.ContactID.String()}})
}

func (m pspMirror) Posted(ctx context.Context, id uuid.UUID) error {
	if err := m.u.check(); err != nil {
		return err
	}
	if err := m.u.q.PostedPSPInvoice(ctx, id); err != nil {
		return mapError(err)
	}
	return m.u.event(ctx, "psp_invoice.posted", "psp_invoice", id, nil, map[string][2]any{"state": {storage.PSPInvoiceDrafted, storage.PSPInvoicePosted}})
}

func (m pspMirror) Documents(ctx context.Context) (map[uuid.UUID]bool, error) {
	if err := m.u.check(); err != nil {
		return nil, err
	}
	ids, err := m.u.q.PSPInvoiceDocuments(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

var _ = time.Time{}

func (m pspMirror) RecordChargeInvoice(ctx context.Context, connector uuid.UUID, charge, providerInvoice string, at time.Time) (bool, error) {
	if err := m.u.check(); err != nil {
		return false, err
	}
	id, err := m.u.q.InsertPSPChargeInvoice(ctx, sqlcdb.InsertPSPChargeInvoiceParams{ConnectorID: connector, ChargeID: charge, ProviderInvoiceID: providerInvoice, SeenAt: at})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, mapError(err)
	}
	return true, m.u.event(ctx, "psp_charge_invoice.recorded", "psp_charge_invoice", id, nil, map[string][2]any{"charge_id": {nil, charge}, "provider_invoice_id": {nil, providerInvoice}})
}

func (m pspMirror) InvoiceOfCharge(ctx context.Context, connector uuid.UUID, charge string) (string, error) {
	if err := m.u.check(); err != nil {
		return "", err
	}
	id, err := m.u.q.PSPInvoiceOfCharge(ctx, sqlcdb.PSPInvoiceOfChargeParams{ConnectorID: connector, ChargeID: charge})
	if err != nil {
		return "", mapError(err)
	}
	return id, nil
}

func toPSPCreditNote(r sqlcdb.GetPSPCreditNoteRow) storage.PSPCreditNote {
	return storage.PSPCreditNote{ID: r.ID, ConnectorID: r.ConnectorID, ProviderID: r.ProviderCreditNoteID, ProviderInvoiceID: r.ProviderInvoiceID, Number: str(r.Number), Status: r.Status,
		Currency: r.Currency, Total: storage.Decimal(r.Total), Reason: str(r.Reason), IssuedOn: nullDate(r.IssuedOn), Lines: json.RawMessage(r.DraftLines), State: r.State,
		ParkedReason: str(r.ParkedReason), DocumentID: r.DocumentID, Version: int(r.Version), SeenAt: r.SeenAt}
}

func (m pspMirror) CreditNote(ctx context.Context, connector uuid.UUID, providerID string) (storage.PSPCreditNote, error) {
	if err := m.u.check(); err != nil {
		return storage.PSPCreditNote{}, err
	}
	r, err := m.u.q.GetPSPCreditNote(ctx, sqlcdb.GetPSPCreditNoteParams{ConnectorID: connector, ProviderCreditNoteID: providerID})
	if err != nil {
		return storage.PSPCreditNote{}, mapError(err)
	}
	return toPSPCreditNote(r), nil
}

func (m pspMirror) SaveCreditNote(ctx context.Context, n storage.NewPSPCreditNote) (storage.PSPCreditNote, bool, error) {
	if err := m.u.check(); err != nil {
		return storage.PSPCreditNote{}, false, err
	}
	lines := []byte(n.Lines)
	if len(lines) == 0 {
		lines = []byte("[]")
	}
	before, err := m.CreditNote(ctx, n.ConnectorID, n.ProviderID)
	if errors.Is(err, ports.ErrNotFound) {
		r, err := m.u.q.InsertPSPCreditNote(ctx, sqlcdb.InsertPSPCreditNoteParams{ConnectorID: n.ConnectorID, ProviderCreditNoteID: n.ProviderID, ProviderInvoiceID: n.ProviderInvoiceID,
			Number: opt(n.Number), Status: n.Status, Currency: n.Currency, Total: string(n.Total), Reason: opt(n.Reason), IssuedOn: pgDate(n.IssuedOn), DraftLines: lines, SeenAt: n.SeenAt})
		if err != nil {
			return storage.PSPCreditNote{}, false, mapError(err)
		}
		c := toPSPCreditNote(sqlcdb.GetPSPCreditNoteRow(r))
		return c, true, m.u.event(ctx, "psp_credit_note.mirrored", "psp_credit_note", c.ID, nil, map[string][2]any{"provider_credit_note_id": {nil, c.ProviderID},
			"provider_invoice_id": {nil, c.ProviderInvoiceID}, "status": {nil, c.Status}, "total": {nil, string(c.Total)}, "currency": {nil, c.Currency}})
	}
	if err != nil {
		return storage.PSPCreditNote{}, false, err
	}
	if before.Status == n.Status && before.Number == n.Number && before.Reason == n.Reason && dayOf(before.IssuedOn) == dayOf(n.IssuedOn) &&
		(before.DocumentID != nil || (sameAmount(before.Total, n.Total) && jsonSame(before.Lines, lines))) {
		return before, false, nil
	}
	r, err := m.u.q.UpdatePSPCreditNote(ctx, sqlcdb.UpdatePSPCreditNoteParams{ID: before.ID, Number: opt(n.Number), Status: n.Status, Reason: opt(n.Reason), IssuedOn: pgDate(n.IssuedOn),
		Total: string(n.Total), DraftLines: lines, SeenAt: n.SeenAt})
	if err != nil {
		return storage.PSPCreditNote{}, false, mapError(err)
	}
	c := toPSPCreditNote(sqlcdb.GetPSPCreditNoteRow(r))
	v := c.Version
	return c, true, m.u.event(ctx, "psp_credit_note.changed", "psp_credit_note", c.ID, &v, map[string][2]any{"status": {before.Status, c.Status}})
}

func (m pspMirror) CreditNotes(ctx context.Context, state string) ([]storage.PSPCreditNote, error) {
	if err := m.u.check(); err != nil {
		return nil, err
	}
	rows, err := m.u.q.PSPCreditNotesInState(ctx, state)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.PSPCreditNote, 0, len(rows))
	for _, r := range rows {
		out = append(out, toPSPCreditNote(sqlcdb.GetPSPCreditNoteRow(r)))
	}
	return out, nil
}

func (m pspMirror) ParkCreditNote(ctx context.Context, id uuid.UUID, why string) error {
	if err := m.u.check(); err != nil {
		return err
	}
	if len(why) > 500 {
		why = why[:500]
	}
	if err := m.u.q.ParkPSPCreditNote(ctx, sqlcdb.ParkPSPCreditNoteParams{ID: id, ParkedReason: &why}); err != nil {
		return mapError(err)
	}
	return m.u.event(ctx, "psp_credit_note.parked", "psp_credit_note", id, nil, map[string][2]any{"state": {nil, storage.PSPInvoiceParked}, "reason": {nil, why}})
}

func (m pspMirror) MakeCreditDraft(ctx context.Context, connector uuid.UUID, providerID string) (uuid.UUID, error) {
	if err := m.u.check(); err != nil {
		return uuid.Nil, err
	}
	docID, err := m.u.q.CreateProviderCreditNoteDraft(ctx, sqlcdb.CreateProviderCreditNoteDraftParams{ConnectorID: connector, ProviderCreditNoteID: providerID})
	if err != nil {
		return uuid.Nil, mapError(err)
	}
	doc, err := m.u.q.GetDocument(ctx, docID)
	if err != nil {
		return uuid.Nil, mapError(err)
	}
	payload, err := jsonValue(doc.Payload)
	if err != nil {
		return uuid.Nil, err
	}
	v := int(doc.CurrentVersion)
	if err := m.u.event(ctx, "document.created", "document", docID, &v, map[string][2]any{"type": {nil, doc.Type}, "status": {nil, doc.Status}, "payload": {nil, payload}}); err != nil {
		return uuid.Nil, err
	}
	cn, err := m.CreditNote(ctx, connector, providerID)
	if err != nil {
		return uuid.Nil, err
	}
	cv := cn.Version
	return docID, m.u.event(ctx, "psp_credit_note.drafted", "psp_credit_note", cn.ID, &cv, map[string][2]any{"state": {storage.PSPInvoiceMirrored, storage.PSPInvoiceDrafted}, "document_id": {nil, docID.String()}})
}

func (m pspMirror) CreditNotePosted(ctx context.Context, id uuid.UUID) error {
	if err := m.u.check(); err != nil {
		return err
	}
	if err := m.u.q.PostedPSPCreditNote(ctx, id); err != nil {
		return mapError(err)
	}
	return m.u.event(ctx, "psp_credit_note.posted", "psp_credit_note", id, nil, map[string][2]any{"state": {storage.PSPInvoiceDrafted, storage.PSPInvoicePosted}})
}

// sameAmount says whether two decimal strings are the same number, whatever their trailing zeros ("30.0000" as the database keeps it, "30.00" as the provider's reading gives it).
func sameAmount(a, b storage.Decimal) bool {
	x, ok1 := new(big.Rat).SetString(string(a))
	y, ok2 := new(big.Rat).SetString(string(b))
	return ok1 && ok2 && x.Cmp(y) == 0
}
