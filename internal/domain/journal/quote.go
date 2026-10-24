package journal

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/quote"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Quotes runs the quote and sales order commands (FR-AR-01; p1-sales task 4.1) against a store, each in one unit of
// work. A quote or an order posts nothing: sending it numbers it and posts the document with no entry, and cancelling
// it reverses the document with no entry.
type Quotes struct {
	Store storage.Store
}

func isQuote(doc storage.Document) error {
	if !quote.IsQuote(doc) {
		return &quote.Error{Code: quote.CodeNotQuote, Message: fmt.Sprintf("document %s is a %s, not a quote or a sales order", doc.ID, doc.Type),
			Fix: "Open it with its own module"}
	}
	return nil
}

// Create saves a draft quote, or a draft sales order when order is true (✎). It is numbered when it is sent.
func (s Quotes) Create(ctx context.Context, org uuid.UUID, d quote.Draft, order bool) (storage.Document, error) {
	b, err := quote.Encode(d)
	if err != nil {
		return storage.Document{}, err
	}
	t := storage.DocumentQuote
	if order {
		t = storage.DocumentSalesOrder
	}
	var doc storage.Document
	err = s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err = createNumbered(ctx, u, t, b)
		return err
	})
	return doc, err
}

// Revise saves a draft's new content as its next version. A sent quote never changes: cancel it and send another.
func (s Quotes) Revise(ctx context.Context, org, id uuid.UUID, d quote.Draft) (storage.Document, error) {
	b, err := quote.Encode(d)
	if err != nil {
		return storage.Document{}, err
	}
	var out storage.Document
	err = s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Lock(ctx, id)
		if err != nil {
			return err
		}
		if err := isQuote(doc); err != nil {
			return err
		}
		if doc.Status != storage.DocumentDraft {
			return &quote.Error{Code: quote.CodeNotDraft, Message: fmt.Sprintf("the %s is %s, not a draft", quote.Kind(doc), quote.Status(doc)),
				Fix: "A sent quote never changes: cancel it and send another"}
		}
		out, err = u.Documents().Revise(ctx, id, b)
		return err
	})
	return out, err
}

// CheckQuote reads a quote's or order's version as sending it would, and writes nothing: what it comes to and what
// stops it (✖).
func CheckQuote(ctx context.Context, u storage.UnitOfWork, doc storage.Document) (quote.Prepared, ledger.Problems, error) {
	if err := isQuote(doc); err != nil {
		return quote.Prepared{}, nil, err
	}
	settings, err := loadSettings(ctx, u)
	if err != nil {
		return quote.Prepared{}, nil, err
	}
	return quote.Check(ctx, u, doc, settings)
}

// Send sends a draft (FR-AR-01): its number (gap-free, in this transaction) and the document posted, with no entry. A
// quote the checks refuse is not sent and takes no number. Emailing it follows with p1-sales task 5.2.
func (s Quotes) Send(ctx context.Context, org, id uuid.UUID) (storage.Document, error) {
	var out storage.Document
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Lock(ctx, id)
		if err != nil {
			return err
		}
		if err := isQuote(doc); err != nil {
			return err
		}
		if doc.Status != storage.DocumentDraft {
			return &quote.Error{Code: quote.CodeNotDraft, Message: fmt.Sprintf("the %s is %s, not a draft", quote.Kind(doc), quote.Status(doc)),
				Fix: "Only a draft is sent"}
		}
		if _, probs, err := CheckQuote(ctx, u, doc); err != nil {
			return err
		} else if len(probs) > 0 {
			return probs
		}
		if doc.Number, err = u.Sequences().Number(ctx, id, doc.Type); err != nil {
			return err
		}
		if err := u.Documents().SetStatus(ctx, id, storage.DocumentPosted); err != nil {
			return err
		}
		doc.Status = storage.DocumentPosted
		out = doc
		return nil
	})
	return out, err
}

// Cancel cancels a sent quote or order, with a reason: the document is reversed, with no entry.
func (s Quotes) Cancel(ctx context.Context, org, id uuid.UUID, reason string) (storage.Document, error) {
	if reason != "" {
		ctx = storage.WithReason(ctx, reason)
	}
	var out storage.Document
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Lock(ctx, id)
		if err != nil {
			return err
		}
		if err := isQuote(doc); err != nil {
			return err
		}
		if doc.Status != storage.DocumentPosted {
			return &quote.Error{Code: quote.CodeNotOpen, Message: fmt.Sprintf("the %s is %s, not sent", quote.Kind(doc), quote.Status(doc)),
				Fix: "Cancel a sent quote or order; a draft is simply not sent"}
		}
		if err := u.Documents().SetStatus(ctx, id, storage.DocumentReversed); err != nil {
			return err
		}
		doc.Status = storage.DocumentReversed
		out = doc
		return nil
	})
	return out, err
}
