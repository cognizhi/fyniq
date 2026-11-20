package psp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The Model B mirror (PRD-001 FR-PSP-03, FR-PSP-07; p1-payment-providers 3.1, 3.2, 4.1 to 4.3). A provider whose account names a revenue account bills
// the customers itself: its customers, subscriptions and invoices are mirrored read-only, with each invoice's collection state, from the object each
// event names and the sweep's lists. An open or paid invoice the invoice rules can take becomes a draft AR document through the database's narrow
// function create_provider_invoice_draft, the worker's only way to a document; the API posts it when the invoice rules' total is the provider's to
// the cent. Anything else is parked for a person, and nothing is retried or charged at the provider (FR-INT-27).

// revenueOf says whether connector id bills through the provider: one of its accounts names a revenue account, and the currency of the first that does.
func revenueOf(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (bool, error) {
	all, err := u.PSP().Accounts(ctx)
	if err != nil {
		return false, err
	}
	for _, a := range all {
		if a.ConnectorID == id && a.RevenueAccountID != nil {
			return true, nil
		}
	}
	return false, nil
}

// revenueFor says whether an account of connector id books revenue in currency.
func revenueFor(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, currency string) (bool, error) {
	all, err := u.PSP().Accounts(ctx)
	if err != nil {
		return false, err
	}
	for _, a := range all {
		if a.ConnectorID != id || a.RevenueAccountID == nil {
			continue
		}
		m, err := u.Banking().MoneyAccount(ctx, a.MoneyAccountID)
		if err != nil {
			return false, err
		}
		if m.Currency == currency && m.ClosedOn == nil {
			return true, nil
		}
	}
	return false, nil
}

// monthsOf is how many calendar months a service period touches: its last day is the day before its end day, so 1 Nov to 1 Dec is one month and 15 Nov to
// 15 Dec is two (design D6, [accountant to confirm]).
func monthsOf(start, end time.Time) int {
	if start.IsZero() || !end.After(start) {
		return 0
	}
	// By the day (UTC): a period of 1 Nov to 1 Dec at any hour ends on 30 Nov.
	last := day(end).AddDate(0, 0, -1)
	if last.Before(day(start)) {
		last = day(start)
	}
	return (last.Year()*12 + int(last.Month())) - (start.Year()*12 + int(start.Month())) + 1
}

type draftLine struct {
	Description  string `json:"description"`
	Quantity     string `json:"quantity"`
	UnitPrice    string `json:"unit_price"`
	SpreadMonths int    `json:"spread_months,omitempty"`
	SpreadFrom   string `json:"spread_from,omitempty"`
}

// linesOf are a provider invoice's lines as the invoice rules read them, each at its amount with quantity 1, spread over the months of its service
// period; the account and GST code are the function's.
func linesOf(inv *ports.InvoiceData) json.RawMessage {
	out := []draftLine{}
	for _, l := range inv.Lines {
		d := draftLine{Description: strings.TrimSpace(l.Description), Quantity: "1", UnitPrice: l.Amount}
		if d.Description == "" {
			d.Description = "Subscription"
		}
		if len(d.Description) > 200 {
			d.Description = d.Description[:200]
		}
		if n := monthsOf(l.PeriodStart, l.PeriodEnd); n > 1 {
			d.SpreadMonths = n
			d.SpreadFrom = time.Date(l.PeriodStart.Year(), l.PeriodStart.Month(), 1, 0, 0, 0, 0, time.UTC).Format(time.DateOnly)
		}
		out = append(out, d)
	}
	b, _ := json.Marshal(out)
	return b
}

func dayPtr(t *time.Time) *time.Time {
	if t == nil || t.IsZero() {
		return nil
	}
	d := day(*t)
	return &d
}

// MirrorObject mirrors one fetched object of a Model B provider in u, and reports the reason to park the event for a person when the object cannot be
// mirrored. A provider with no revenue account mirrors nothing: it is not billing through the provider.
func MirrorObject(ctx context.Context, u storage.UnitOfWork, connector uuid.UUID, obj ports.ProviderObject, now time.Time) (string, error) {
	on, err := revenueOf(ctx, u, connector)
	if err != nil || !on {
		return "", err
	}
	switch obj.Kind {
	case ports.ProviderCustomer:
		if obj.Customer == nil {
			return "the customer could not be read", nil
		}
		_, _, err := u.PSPMirror().SaveCustomer(ctx, storage.NewPSPCustomer{ConnectorID: connector, ProviderID: obj.ID, Name: obj.Customer.Name, Email: obj.Customer.Email, SeenAt: now})
		return "", err
	case ports.ProviderSubscription:
		s := obj.Subscription
		if s == nil || s.CustomerID == "" {
			return "the subscription could not be read", nil
		}
		_, _, err := u.PSPMirror().SaveSubscription(ctx, storage.NewPSPSubscription{ConnectorID: connector, ProviderID: obj.ID, ProviderCustomerID: s.CustomerID, Status: s.Status,
			Currency: obj.Currency, Amount: storage.Decimal(s.Amount), IntervalUnit: s.IntervalUnit, IntervalCount: s.IntervalCount, PeriodStart: dayPtr(s.PeriodStart),
			PeriodEnd: dayPtr(s.PeriodEnd), CancelAtPeriodEnd: s.CancelAtPeriodEnd, CanceledOn: dayPtr(s.CanceledAt), SeenAt: now})
		return "", err
	case ports.ProviderInvoice:
		return mirrorInvoice(ctx, u, connector, obj, now)
	case ports.ProviderCreditNote:
		return mirrorCreditNote(ctx, u, connector, obj, now)
	}
	return "", nil
}

// creditLinesOf are a provider credit note's lines as the invoice rules read them, each at its amount with quantity 1; the account and GST code are the function's.
func creditLinesOf(cn *ports.CreditNoteData) json.RawMessage {
	out := []draftLine{}
	for _, l := range cn.Lines {
		d := draftLine{Description: strings.TrimSpace(l.Description), Quantity: "1", UnitPrice: l.Amount}
		if d.Description == "" {
			d.Description = "Credit"
		}
		if len(d.Description) > 200 {
			d.Description = d.Description[:200]
		}
		out = append(out, d)
	}
	b, _ := json.Marshal(out)
	return b
}

func mirrorCreditNote(ctx context.Context, u storage.UnitOfWork, connector uuid.UUID, obj ports.ProviderObject, now time.Time) (string, error) {
	cn := obj.CreditNote
	if cn == nil || cn.InvoiceID == "" || obj.Currency == "" || (cn.Status != "issued" && cn.Status != "void") {
		return "the credit note could not be read", nil
	}
	issued := dayPtr(&obj.Created)
	reason := cn.Reason
	if len(reason) > 500 {
		reason = reason[:500]
	}
	saved, _, err := u.PSPMirror().SaveCreditNote(ctx, storage.NewPSPCreditNote{ConnectorID: connector, ProviderID: obj.ID, ProviderInvoiceID: cn.InvoiceID, Number: cn.Number, Status: cn.Status,
		Currency: obj.Currency, Total: storage.Decimal(cn.Total), Reason: reason, IssuedOn: issued, Lines: creditLinesOf(cn), SeenAt: now})
	if err != nil {
		return "", err
	}
	switch {
	case saved.State == storage.PSPInvoiceMirrored && saved.Status == "issued" && cn.Unsupported != "":
		return "", u.PSPMirror().ParkCreditNote(ctx, saved.ID, "Not made into a credit note: it has "+cn.Unsupported+" (FR-PSP-07). A person decides it.")
	case saved.State == storage.PSPInvoiceDrafted && saved.Status == "void":
		return "", u.PSPMirror().ParkCreditNote(ctx, saved.ID, "The provider voided it after its draft was made: a person discards the draft (FR-PSP-07).")
	}
	return "", nil
}

// MakeCreditDrafts makes the draft AR credit note of each mirrored issued credit note of connector id that is ready: the AR invoice of the provider invoice it credits is issued
// and its provider account books revenue in its currency. One that is not ready stays mirrored, and the next sweep tries again. It returns how many drafts were made.
func MakeCreditDrafts(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (int, error) {
	notes, err := u.PSPMirror().CreditNotes(ctx, storage.PSPInvoiceMirrored)
	if err != nil {
		return 0, err
	}
	made := 0
	for _, cn := range notes {
		if cn.ConnectorID != id || cn.Status != "issued" || cn.IssuedOn == nil || string(cn.Lines) == "[]" {
			continue
		}
		inv, err := u.PSPMirror().Invoice(ctx, id, cn.ProviderInvoiceID)
		if errors.Is(err, ports.ErrNotFound) || (err == nil && inv.State != storage.PSPInvoicePosted) {
			continue // its invoice is not issued here yet
		}
		if err != nil {
			return made, err
		}
		ok, err := revenueFor(ctx, u, id, cn.Currency)
		if err != nil {
			return made, err
		}
		if !ok {
			continue
		}
		if _, err := u.PSPMirror().MakeCreditDraft(ctx, id, cn.ProviderID); err != nil {
			return made, fmt.Errorf("provider credit note %s: %w", cn.ProviderID, err)
		}
		made++
	}
	return made, nil
}

func mirrorInvoice(ctx context.Context, u storage.UnitOfWork, connector uuid.UUID, obj ports.ProviderObject, now time.Time) (string, error) {
	inv := obj.Invoice
	if inv == nil || inv.CustomerID == "" || obj.Currency == "" {
		return "the invoice could not be read", nil
	}
	if _, _, err := u.PSPMirror().SaveCustomer(ctx, storage.NewPSPCustomer{ConnectorID: connector, ProviderID: inv.CustomerID, Name: inv.CustomerName, Email: inv.CustomerEmail, SeenAt: now}); err != nil {
		return "", err
	}
	outcome := ""
	switch inv.Status {
	case "paid", "uncollectible", "void":
		outcome = inv.Status
	}
	finalized := dayPtr(inv.Finalized)
	if finalized == nil && (inv.Status == "open" || inv.Status == "paid") {
		d := day(obj.Created)
		finalized = &d
	}
	saved, _, err := u.PSPMirror().SaveInvoice(ctx, storage.NewPSPInvoice{ConnectorID: connector, ProviderID: obj.ID, ProviderCustomerID: inv.CustomerID, ProviderSubscriptionID: inv.SubscriptionID,
		Number: inv.Number, Status: inv.Status, Currency: obj.Currency, Total: storage.Decimal(inv.Total), AmountPaid: storage.Decimal(inv.AmountPaid), AttemptCount: inv.AttemptCount,
		NextAttemptOn: dayPtr(inv.NextAttempt), Outcome: outcome, HostedURL: inv.HostedURL, FinalizedOn: finalized, DueOn: dayPtr(inv.Due), Lines: linesOf(inv), SeenAt: now})
	if err != nil {
		return "", err
	}
	if saved.State == storage.PSPInvoiceMirrored && (inv.Status == "open" || inv.Status == "paid") && inv.Unsupported != "" {
		return "", u.PSPMirror().Park(ctx, saved.ID, "Not made into an invoice: it has "+inv.Unsupported+" (FR-PSP-07). A person decides it.")
	}
	if saved.State == storage.PSPInvoiceDrafted && (inv.Status == "void" || inv.Status == "uncollectible") {
		return "", u.PSPMirror().Park(ctx, saved.ID, "The provider marked it "+inv.Status+" after its draft was made: a person discards the draft (FR-PSP-07).")
	}
	return "", nil
}

// MakeDrafts makes the draft AR document of each mirrored open or paid invoice of connector id that is ready: its provider account books revenue in its
// currency. One whose account is not set up yet stays mirrored, and the next sweep tries again. It returns how many drafts were made.
func MakeDrafts(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (int, error) {
	invoices, err := u.PSPMirror().Invoices(ctx, storage.PSPInvoiceMirrored)
	if err != nil {
		return 0, err
	}
	made := 0
	for _, inv := range invoices {
		if inv.ConnectorID != id || (inv.Status != "open" && inv.Status != "paid") || inv.FinalizedOn == nil || string(inv.Lines) == "[]" {
			continue
		}
		ok, err := revenueFor(ctx, u, id, inv.Currency)
		if err != nil {
			return made, err
		}
		if !ok {
			continue
		}
		if _, _, _, err := u.PSPMirror().MakeDraft(ctx, id, inv.ProviderID); err != nil {
			return made, fmt.Errorf("provider invoice %s: %w", inv.ProviderID, err)
		}
		made++
	}
	return made, nil
}
