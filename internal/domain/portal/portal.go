// Package portal is the customer portal (PRD-001 FR-AR-14; p2-customer-portal-subscriptions design D1, D2): what one customer may
// see through a link, and nothing else. A link names one customer contact of one organisation; every read here takes that
// customer from the link's own row, never from the request, and a document that is not that customer's is not found, as one that
// does not exist is. The portal reads only: it posts nothing, and it never asks a provider for anything (it shows a payment
// link a person already made).
package portal

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/receivables"
	"github.com/cognizhi/fyniq/internal/domain/statement"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// CodeLink is the one answer to a link that opens nothing: unknown, malformed, expired or revoked alike, and a document or customer
// that is not the link's (the threat model's T1, T9).
const CodeLink = "PORTAL-01"

// DefaultDays and MaxDays are how long a link lives: 30 days, at most 90 (the database holds the 90).
const (
	DefaultDays = 30
	MaxDays     = 90
)

// Scope is what a link opens: the organisation, the customer and the link itself.
type Scope struct {
	Org  uuid.UUID
	Link storage.PortalLink
}

// Live says whether a link opens anything at now.
func Live(l storage.PortalLink, now time.Time) bool {
	return l.RevokedAt == nil && now.Before(l.ExpiresAt)
}

// Invoice is one open invoice of the customer, with the provider's page where it can be paid when a person made one.
type Invoice struct {
	ID         uuid.UUID
	Number     string
	Date       time.Time
	DueDate    time.Time
	Total      money.Money
	Open       money.Money
	Overdue    bool
	PaymentURL string
}

// Summary is what the portal's first page shows.
type Summary struct {
	Customer   string
	Invoices   []Invoice
	Currencies []money.Currency
	Base       money.Currency
	Today      time.Time
}

// customerOf reads the link's customer, which must still be an active customer.
func customerOf(ctx context.Context, u storage.UnitOfWork, s Scope) (storage.Contact, error) {
	c, err := u.Contacts().Get(ctx, s.Link.ContactID)
	if errors.Is(err, ports.ErrNotFound) || (err == nil && !c.Active) {
		return storage.Contact{}, ports.ErrNotFound
	}
	return c, err
}

// Read is the customer's open invoices as at today: issued, not voided, with something open, and a live payment link where one
// exists. Credit notes and receipts show in the statement.
func Read(ctx context.Context, u storage.UnitOfWork, s Scope, today time.Time) (Summary, error) {
	out := Summary{Today: today}
	c, err := customerOf(ctx, u, s)
	if err != nil {
		return out, err
	}
	out.Customer = c.LegalName
	org, err := u.Organisation(ctx)
	if err != nil {
		return out, err
	}
	if out.Base, err = money.Lookup(org.BaseCurrency); err != nil {
		return out, err
	}
	ar, err := receivableAccount(ctx, u)
	if err != nil {
		return out, err
	}
	l, err := receivables.Open(ctx, u, out.Base, ar, today)
	if err != nil {
		return out, err
	}
	for _, cu := range l.Customers {
		if cu.ID != s.Link.ContactID { // the link's customer only, whoever else is in the organisation
			continue
		}
		for _, it := range cu.Items {
			if it.Kind != receivables.KindInvoice || it.Open.Sign() <= 0 {
				continue
			}
			inv := Invoice{ID: it.DocumentID, Number: it.Number, Date: it.Date, DueDate: it.DueDate, Total: it.Total, Open: it.Open,
				Overdue: !it.DueDate.IsZero() && it.DueDate.Before(today)}
			if links, err := u.PaymentLinks().OfInvoice(ctx, it.DocumentID); err != nil {
				return out, err
			} else if len(links) > 0 && links[0].Status == storage.LinkLive && (links[0].ExpiresAt == nil || links[0].ExpiresAt.After(today)) && links[0].Currency == it.Open.Currency().Code {
				inv.PaymentURL = links[0].URL
			}
			out.Invoices = append(out.Invoices, inv)
		}
	}
	slices.SortStableFunc(out.Invoices, func(a, b Invoice) int {
		if d := a.DueDate.Compare(b.DueDate); d != 0 {
			return d
		}
		return a.Date.Compare(b.Date)
	})
	if out.Currencies, err = statement.Currencies(ctx, u, out.Base, s.Link.ContactID, today.AddDate(-1, 0, 0), today); err != nil {
		return out, err
	}
	return out, nil
}

func receivableAccount(ctx context.Context, u storage.UnitOfWork) (uuid.UUID, error) {
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	for _, a := range accounts {
		if string(a.Role) == ledger.RoleAccountsReceivable {
			return a.ID, nil
		}
	}
	return uuid.Nil, ports.ErrNotFound
}

// Invoice reads the issued invoice id of the link's customer: a draft, a voided one, a credit note, another customer's and one that
// does not exist are all ports.ErrNotFound.
func InvoiceOf(ctx context.Context, u storage.UnitOfWork, s Scope, id uuid.UUID) (storage.Document, invoice.Draft, error) {
	org, err := u.Organisation(ctx)
	if err != nil {
		return storage.Document{}, invoice.Draft{}, err
	}
	base, err := money.Lookup(org.BaseCurrency)
	if err != nil {
		return storage.Document{}, invoice.Draft{}, err
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return storage.Document{}, invoice.Draft{}, err
	}
	if !invoice.IsInvoice(doc) || invoice.IsCreditNote(doc) || doc.Status != storage.DocumentPosted {
		return storage.Document{}, invoice.Draft{}, ports.ErrNotFound
	}
	d, err := invoice.Read(doc, base)
	if err != nil {
		return storage.Document{}, invoice.Draft{}, err
	}
	if d.CustomerID != s.Link.ContactID {
		return storage.Document{}, invoice.Draft{}, ports.ErrNotFound
	}
	if _, err := customerOf(ctx, u, s); err != nil { // an inactive customer's portal opens nothing
		return storage.Document{}, invoice.Draft{}, err
	}
	return doc, d, nil
}

// Statement is the customer's statement of account in cur from from to to, for the link's customer only.
func Statement(ctx context.Context, u storage.UnitOfWork, s Scope, cur string, from, to, today time.Time) (statement.Statement, error) {
	org, err := u.Organisation(ctx)
	if err != nil {
		return statement.Statement{}, err
	}
	base, err := money.Lookup(org.BaseCurrency)
	if err != nil {
		return statement.Statement{}, err
	}
	c, err := money.Lookup(cur)
	if err != nil {
		return statement.Statement{}, ports.ErrNotFound
	}
	if err := statement.CheckPeriod(from, to, today); err != nil {
		return statement.Statement{}, err
	}
	if _, err := customerOf(ctx, u, s); err != nil {
		return statement.Statement{}, err
	}
	return statement.Of(ctx, u, base, s.Link.ContactID, c, from, to)
}
