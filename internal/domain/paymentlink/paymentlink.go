// Package paymentlink is the payment link of an invoice (PRD-001 FR-AR-13, FR-PSP-02; p2-bank-feeds-payments design D1 to D3): what
// may be asked of a provider for an invoice, and what a provider's charge under a link means for it. The amount of a link is the
// invoice's open balance read here, on the server, in the same unit of work that records the link; nothing a client sends changes
// it. Nothing here posts: the receipt is auto-reconcile's, through the receipt command that already exists.
package paymentlink

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/receivables"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Stable codes of a refused link (ENG-23).
const (
	CodeNotIssued = "PSP-09" // the invoice is not an issued invoice with something open
	CodeNoAccount = "PSP-10" // no provider account of the connector collects the invoice's currency
	CodeLive      = "PSP-11" // the invoice has a live link already
)

// Error is a refusal in plain words.
type Error struct{ Code, Message, Fix string }

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Fix) }

// Methods are the payment methods a link offers: PayNow through HitPay, and cards [live-verify: the account's enabled methods].
var Methods = []string{"paynow_online", "card"}

// ExpiryAfterDue is how long after the invoice's due date a link keeps working.
const ExpiryAfterDue = 30 * 24 * time.Hour

// Basis is an invoice's open balance, read as a link needs it.
type Basis struct {
	Invoice  storage.Document
	Draft    invoice.Draft
	Customer uuid.UUID
	Open     money.Money
}

// OpenInvoice reads invoice id as at now: it must be an issued invoice with something open. The balance is the AR sub-ledger's
// (what the invoice's entry debited, less what is allocated to it), the same one every receipt and the ageing read.
func OpenInvoice(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, now time.Time) (Basis, error) {
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return Basis{}, err
	}
	refuse := func(why string) (Basis, error) {
		return Basis{}, &Error{CodeNotIssued, "A payment link is for an issued invoice with something open: " + why + ".", "Issue the invoice, or pick one that is still owed"}
	}
	if !invoice.IsInvoice(doc) || invoice.IsCreditNote(doc) {
		return refuse("this is not an invoice")
	}
	if doc.Status == storage.DocumentDraft {
		return refuse("this one is a draft")
	}
	org, err := u.Organisation(ctx)
	if err != nil {
		return Basis{}, err
	}
	base, err := money.Lookup(org.BaseCurrency)
	if err != nil {
		return Basis{}, err
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return Basis{}, err
	}
	ar := uuid.Nil
	for _, a := range accounts {
		if string(a.Role) == ledger.RoleAccountsReceivable {
			ar = a.ID
		}
	}
	if ar == uuid.Nil {
		return refuse("no accounts receivable account is set")
	}
	d, err := invoice.Read(doc, base)
	if err != nil {
		return Basis{}, err
	}
	l, err := receivables.Open(ctx, u, base, ar, now)
	if err != nil {
		return Basis{}, err
	}
	for _, c := range l.Customers {
		for _, it := range c.Items {
			if it.Kind == receivables.KindInvoice && it.DocumentID == id && it.Open.Sign() > 0 {
				return Basis{Invoice: doc, Draft: d, Customer: c.ID, Open: it.Open}, nil
			}
		}
	}
	return refuse("it is paid, void or has nothing open")
}

// Collects says whether a provider account of connector collects currency: a payment-gateway money account of that currency
// that the connector feeds and that is set up under Bank data (so its charges land in a clearing account the books know).
func Collects(ctx context.Context, u storage.UnitOfWork, connector uuid.UUID, currency string) (bool, error) {
	accounts, err := u.PSP().Accounts(ctx)
	if err != nil {
		return false, err
	}
	for _, a := range accounts {
		if a.ConnectorID != connector {
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

// Request is the payment request an invoice's open balance asks for, and when its link expires.
func Request(b Basis, now time.Time) ports.PaymentRequest {
	exp := b.Draft.DueDate.Add(ExpiryAfterDue)
	if b.Draft.DueDate.IsZero() || exp.Before(now.Add(24*time.Hour)) {
		exp = now.Add(ExpiryAfterDue)
	}
	return ports.PaymentRequest{Reference: b.Invoice.Number, Amount: b.Open.String(), Currency: b.Open.Currency().Code,
		Purpose: "Invoice " + b.Invoice.Number, Methods: Methods, ExpiresAt: &exp}
}

// Check runs every refusal of a link for invoice id at connector, and answers what its payment request would ask for; it writes
// and calls nothing, so a preview (FR-INT-14) is this alone.
func Check(ctx context.Context, u storage.UnitOfWork, connector, invoiceID uuid.UUID, now time.Time) (Basis, ports.PaymentRequest, error) {
	b, err := OpenInvoice(ctx, u, invoiceID, now)
	if err != nil {
		return Basis{}, ports.PaymentRequest{}, err
	}
	if b.Invoice.Number == "" {
		return Basis{}, ports.PaymentRequest{}, &Error{CodeNotIssued, "The invoice has no number yet.", "Issue the invoice first"}
	}
	existing, err := u.PaymentLinks().OfInvoice(ctx, invoiceID)
	if err != nil {
		return Basis{}, ports.PaymentRequest{}, err
	}
	for _, e := range existing {
		if e.Status == storage.LinkLive && !Expired(e, now) {
			return Basis{}, ports.PaymentRequest{}, &Error{CodeLive, "The invoice has a payment link already.", "Cancel it first if the customer needs a new one"}
		}
	}
	ok, err := Collects(ctx, u, connector, b.Open.Currency().Code)
	if err != nil {
		return Basis{}, ports.PaymentRequest{}, err
	}
	if !ok {
		return Basis{}, ports.PaymentRequest{}, &Error{CodeNoAccount, "No provider account of this connector collects " + b.Open.Currency().Code + ".",
			"Set up a provider money account in " + b.Open.Currency().Code + " under Banking › Provider payouts first"}
	}
	return b, Request(b, now), nil
}

// Expired says whether a link past its date still shows as live in the database: it works no more, and a new one may be made.
func Expired(l storage.PaymentLink, now time.Time) bool {
	return l.Status == storage.LinkLive && l.ExpiresAt != nil && !now.Before(*l.ExpiresAt)
}

// Create asks the provider for the invoice's payment request and records the link, in u. One live link per invoice: the
// database refuses a second, and Check says so in words first. The provider is called last, after every refusal, so a refused
// link leaves nothing at the provider.
func Create(ctx context.Context, u storage.UnitOfWork, links ports.PaymentLinks, connector, invoiceID uuid.UUID, now time.Time) (storage.PaymentLink, error) {
	_, req, err := Check(ctx, u, connector, invoiceID, now)
	if err != nil {
		return storage.PaymentLink{}, err
	}
	made, err := links.CreatePaymentRequest(ctx, req)
	if err != nil {
		return storage.PaymentLink{}, err
	}
	// A link past its date is closed as expired, so the invoice has one live link again (one live link per invoice, in the database).
	old, err := u.PaymentLinks().OfInvoice(ctx, invoiceID)
	if err != nil {
		return storage.PaymentLink{}, err
	}
	for _, e := range old {
		if Expired(e, now) {
			if _, err := u.PaymentLinks().Close(ctx, e.ID, storage.LinkExpired, "It ran past its date", now, e.Version); err != nil {
				return storage.PaymentLink{}, err
			}
		}
	}
	return u.PaymentLinks().Create(ctx, storage.NewPaymentLink{ConnectorID: connector, InvoiceID: invoiceID, ProviderRequestID: made.ID, URL: made.URL,
		Reference: req.Reference, Currency: req.Currency, Amount: storage.Decimal(req.Amount), ExpiresAt: made.ExpiresAt, At: now})
}
