package quote

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// LineState is what a quote line has been invoiced (FR-AR-02): the quantity quoted, on issued invoices, and on draft
// invoices, each invoice line taking its own quantity.
type LineState struct {
	Quoted, Invoiced, Drafted money.Quantity
}

// Left is what of the line no invoice, issued or draft, takes yet; never below zero, as an invoice may invoice more
// than was quoted.
func (s LineState) Left() money.Quantity { return atLeastZero(s.Quoted.Sub(s.Invoiced).Sub(s.Drafted)) }

// Uninvoiced is what of the line no issued invoice takes yet: what it still adds to the pipeline.
func (s LineState) Uninvoiced() money.Quantity { return atLeastZero(s.Quoted.Sub(s.Invoiced)) }

func atLeastZero(q money.Quantity) money.Quantity {
	if q.Sign() < 0 {
		return money.Quantity{}
	}
	return q
}

// Invoicing is each line's state from the invoices made from the quote, issued and draft, except the invoice except
// (uuid.Nil for none), with those invoices. Only invoices of the quote's customer and currency count: no other can be
// issued (QUOTE-11).
func Invoicing(ctx context.Context, u storage.UnitOfWork, quoteID uuid.UUID, d Draft, base money.Currency, except uuid.UUID) ([]LineState, []storage.Document, error) {
	out := make([]LineState, len(d.Content.Lines))
	for i, l := range d.Content.Lines {
		out[i].Quoted = l.Quantity
	}
	docs, err := u.Documents().InvoicesOfQuote(ctx, quoteID)
	if err != nil {
		return nil, nil, err
	}
	var kept []storage.Document
	for _, doc := range docs {
		if doc.ID == except {
			continue
		}
		inv, err := invoice.Read(doc, base)
		if err != nil {
			return nil, nil, err
		}
		if inv.CustomerID != d.Content.CustomerID || inv.Currency != d.Content.Currency {
			continue
		}
		for _, l := range inv.Lines {
			if l.Quote == nil || l.Quote.QuoteID != quoteID || l.Quote.Line < 1 || l.Quote.Line > len(out) {
				continue
			}
			s := &out[l.Quote.Line-1]
			if doc.Status == storage.DocumentPosted {
				s.Invoiced = s.Invoiced.Add(l.Quantity)
			} else {
				s.Drafted = s.Drafted.Add(l.Quantity)
			}
		}
		kept = append(kept, doc)
	}
	return out, kept, nil
}

// Invoiced says whether issued invoices take every line of a quote in full.
func Invoiced(states []LineState) bool {
	for _, s := range states {
		if s.Uninvoiced().Sign() > 0 {
			return false
		}
	}
	return len(states) > 0
}

// StatusOf is the status a quote or order shows once its invoicing is known (FR-AR-01): invoiced when it is sent and
// issued invoices take every line in full, else Status.
func StatusOf(doc storage.Document, outcome *storage.QuoteOutcome, states []LineState) string {
	if doc.Status == storage.DocumentPosted && Invoiced(states) {
		return StatusInvoiced
	}
	return Status(doc, outcome)
}

// Uninvoiced is the quote's content as what no issued invoice takes yet: each line at its uninvoiced quantity, the
// lines with none left dropped. The pipeline counts what it comes to.
func (d Draft) Uninvoiced(states []LineState) Draft {
	out := d
	out.Content.Lines = nil
	for i, l := range d.Content.Lines {
		if i >= len(states) {
			break
		}
		if q := states[i].Uninvoiced(); q.Sign() > 0 {
			l.Quantity = q
			out.Content.Lines = append(out.Content.Lines, l)
		}
	}
	return out
}

// CheckInvoiceLines checks the lines of an invoice made from quotes or sales orders, as the invoice would issue
// (FR-AR-02): each names a line of a quote or order of the invoice's customer and currency that the customer accepted
// and that is not cancelled. A credit note's line names no quote. A line may invoice more than was quoted: what was
// delivered is what is invoiced.
func CheckInvoiceLines(ctx context.Context, u storage.UnitOfWork, doc storage.Document, d invoice.Draft, base money.Currency) (ledger.Problems, error) {
	var probs ledger.Problems
	add := func(field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: CodeQuoteLine, Field: field, Message: msg, Fix: fix})
	}
	quotes := map[uuid.UUID]Draft{}
	refused := map[uuid.UUID]bool{}
	for i, l := range d.Lines {
		if l.Quote == nil {
			continue
		}
		f := fmt.Sprintf("lines[%d].quote", i+1)
		if invoice.IsCreditNote(doc) {
			add(f, "a credit note's line is not made from a quote", "Remove the quote from the line")
			continue
		}
		if refused[l.Quote.QuoteID] {
			continue // said once
		}
		q, ok := quotes[l.Quote.QuoteID]
		if !ok {
			qd, err := u.Documents().Get(ctx, l.Quote.QuoteID)
			if errors.Is(err, ports.ErrNotFound) || (err == nil && !IsQuote(qd)) {
				add(f, "the line names a quote that does not exist in this organisation", "Make the invoice from the quote again")
				refused[l.Quote.QuoteID] = true
				continue
			}
			if err != nil {
				return nil, err
			}
			outcomes, err := u.QuoteOutcomes().Of(ctx, []uuid.UUID{qd.ID})
			if err != nil {
				return nil, err
			}
			var o *storage.QuoteOutcome
			if x, ok := outcomes[qd.ID]; ok {
				o = &x
			}
			if st := Status(qd, o); st != StatusAccepted {
				add(f, fmt.Sprintf("%s %s is %s, not accepted", Kind(qd), qd.Number, st), "Invoice a quote or order the customer accepted, that is not cancelled")
				refused[l.Quote.QuoteID] = true
				continue
			}
			if q, err = Read(qd, base); err != nil {
				return nil, err
			}
			if q.Content.CustomerID != d.CustomerID || q.Content.Currency != d.Currency {
				add(f, fmt.Sprintf("%s %s is for another customer or currency", Kind(qd), qd.Number), "Invoice a quote of the invoice's own customer, in its currency")
				refused[l.Quote.QuoteID] = true
				continue
			}
			quotes[l.Quote.QuoteID] = q
		}
		if l.Quote.Line < 1 || l.Quote.Line > len(q.Content.Lines) {
			add(f, fmt.Sprintf("the quote has no line %d", l.Quote.Line), "Make the invoice from the quote again")
		}
	}
	return probs, nil
}
