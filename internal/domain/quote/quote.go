// Package quote is a quote to a customer, or a sales order a customer placed (FR-AR-01; p1-sales task 4.1): the lines
// an invoice would have, a validity date, and its status. It posts nothing. Its content is an invoice's (package
// invoice), so it is read and checked the way an invoice is; sending it numbers it and posts the document with no
// entry, and cancelling it reverses the document, with no entry.
package quote

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable codes of a quote's refusals (ENG-23).
const (
	CodeNotQuote = "QUOTE-01" // the document is not a quote or a sales order
	CodeBadDraft = "QUOTE-02" // the saved quote cannot be read
	CodeValidity = "QUOTE-03" // a quote with no validity date, or one before its date
	CodeNotDraft = "QUOTE-04" // only a draft changes or is sent
	CodeNotOpen  = "QUOTE-05" // only a sent quote or order is cancelled
	CodeContent  = "QUOTE-06" // a quote that names an invoice it credits or deposits it deducts
	CodeAnswered = "QUOTE-07" // a quote the customer answered already, or that expired
	CodeAnswer   = "QUOTE-08" // an answer with no date, or a name or note that is too long
)

// The statuses a quote or an order shows (FR-AR-01): draft, sent, the customer's answer (accepted, declined), expired,
// and cancelled. Invoicing follows with its own slice.
const (
	StatusDraft     = "draft"
	StatusSent      = "sent"
	StatusAccepted  = "accepted"
	StatusDeclined  = "declined"
	StatusExpired   = "expired"
	StatusCancelled = "cancelled"
)

// The longest name and note an answer keeps.
const (
	MaxName = 200
	MaxNote = 2000
)

// Error is a quote command's refusal with a stable code.
type Error struct {
	Code    string
	Message string
	Fix     string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Fix) }

// Draft is a quote or order as entered: an invoice's content (its customer, reference, date, currency and rate, lines
// and notes), and the date it is valid until.
type Draft struct {
	Content    invoice.Draft
	ValidUntil time.Time
}

// Encode is a draft as a document version stores it: an invoice's content (package invoice), with valid_until beside
// its fields, so its date is where every document's is. A quote credits no invoice and deducts no deposit.
func Encode(d Draft) ([]byte, error) {
	if d.Content.Credits != uuid.Nil || len(d.Content.Deposits) > 0 {
		return nil, &Error{CodeContent, "a quote or an order credits no invoice and deducts no deposit", "Leave them out; the invoice made from it deducts deposits"}
	}
	c, err := invoice.Encode(d.Content)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(c, &fields); err != nil {
		return nil, err
	}
	if !d.ValidUntil.IsZero() {
		v, _ := json.Marshal(d.ValidUntil.Format(time.DateOnly))
		fields["valid_until"] = v
	}
	return json.Marshal(fields)
}

// IsQuote says whether a document is a quote or a sales order: what this package reads.
func IsQuote(doc storage.Document) bool {
	return doc.Type == storage.DocumentQuote || doc.Type == storage.DocumentSalesOrder
}

// Kind is what a document of this package is, in words.
func Kind(doc storage.Document) string {
	if doc.Type == storage.DocumentSalesOrder {
		return "sales order"
	}
	return "quote"
}

// Read reads a quote or order version as it was saved; base is the base currency a typed rate converts into.
func Read(doc storage.Document, base money.Currency) (Draft, error) {
	if !IsQuote(doc) {
		return Draft{}, &Error{CodeNotQuote, fmt.Sprintf("document %s is a %s, not a quote or a sales order", doc.ID, doc.Type), "Open it with its own module"}
	}
	var p struct {
		ValidUntil string `json:"valid_until"`
	}
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return Draft{}, &Error{CodeBadDraft, "the saved quote cannot be read: " + err.Error(), "Save the quote again"}
	}
	c, err := invoice.ReadContent(doc.Payload, base)
	if err != nil {
		return Draft{}, err
	}
	d := Draft{Content: c}
	if p.ValidUntil != "" {
		if d.ValidUntil, err = time.Parse(time.DateOnly, p.ValidUntil); err != nil {
			return Draft{}, &Error{CodeBadDraft, fmt.Sprintf("the saved quote's validity date %q cannot be read", p.ValidUntil), "Save the quote again"}
		}
	}
	return d, nil
}

// Status is the status a quote or order shows from its document and its outcome, if any (FR-AR-01): a cancelled one is
// cancelled whatever its answer.
func Status(doc storage.Document, outcome *storage.QuoteOutcome) string {
	switch {
	case doc.Status == storage.DocumentReversed:
		return StatusCancelled
	case doc.Status != storage.DocumentPosted:
		return StatusDraft
	case outcome == nil:
		return StatusSent
	}
	return outcome.Outcome // accepted, declined or expired
}

// Open says whether a quote or order is in the pipeline: sent, or accepted, and not past its validity date on day.
func Open(doc storage.Document, outcome *storage.QuoteOutcome, validUntil, day time.Time) bool {
	s := Status(doc, outcome)
	return (s == StatusSent || s == StatusAccepted) && (validUntil.IsZero() || !validUntil.Before(day))
}

// Prepared is a quote or order version as an invoice of the same content would be read: its customer, its totals in
// its currency and whether it bears GST (package invoice), and its total in the base currency at its rate, which the
// pipeline adds up.
type Prepared struct {
	Draft   Draft
	Invoice invoice.Prepared
	Base    money.Money
}

// Check reads one version of a quote or order as an invoice of the same content and date would be read, and runs the
// invoice rule over it, posting nothing: what stops it being sent (✖), the invoice's own checks and the rule's, and
// its validity date: a quote names one on or after its date; an order may name none.
func Check(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings) (Prepared, ledger.Problems, error) {
	d, err := Read(doc, s.Base)
	if err != nil {
		return Prepared{}, nil, err
	}
	content, err := invoice.Encode(d.Content)
	if err != nil {
		return Prepared{}, nil, err
	}
	as := storage.Document{ID: doc.ID, Version: doc.Version, Type: storage.DocumentInvoice, Status: storage.DocumentDraft, Number: doc.Number, Payload: content}
	p, probs, err := invoice.Prepare(ctx, u, as, s.Base)
	if err != nil {
		return Prepared{}, nil, err
	}
	out := Prepared{Draft: d, Invoice: p, Base: money.Zero(s.Base)}
	add := func(code, field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: code, Field: field, Message: msg, Fix: fix})
	}
	switch {
	case doc.Type == storage.DocumentQuote && d.ValidUntil.IsZero():
		add(CodeValidity, "valid_until", "the quote has no validity date", "Enter the date it is valid until")
	case !d.ValidUntil.IsZero() && !d.Content.Date.IsZero() && d.ValidUntil.Before(d.Content.Date):
		add(CodeValidity, "valid_until", fmt.Sprintf("it is valid until %s, before its date %s", d.ValidUntil.Format(time.DateOnly), d.Content.Date.Format(time.DateOnly)),
			"Enter a validity date on or after its date")
	}
	_, err = ledger.PostInvoice(p.Invoice, s)
	if ps, ok := err.(ledger.Problems); ok {
		for _, x := range ps {
			if (x.Code == ledger.CodeMissingRate && probs.Has(fx.CodeNoRate)) || (x.Code == ledger.CodeGSTRate && probs.Has(ledger.CodeGSTRate)) {
				continue // said once
			}
			probs = append(probs, x)
		}
	} else if err != nil {
		return Prepared{}, nil, err
	}
	rate := money.Identity(s.Base)
	if p.Invoice.Rate != nil {
		rate = *p.Invoice.Rate
	}
	if p.Totals.Total.Currency() == rate.From {
		if b, err := rounding.FX(p.Totals.Total, rate); err == nil {
			out.Base = b
		}
	}
	return out, probs, nil
}
