package journal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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
			return &quote.Error{Code: quote.CodeNotDraft, Message: fmt.Sprintf("the %s is %s, not a draft", quote.Kind(doc), quote.Status(doc, nil)),
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
			return &quote.Error{Code: quote.CodeNotDraft, Message: fmt.Sprintf("the %s is %s, not a draft", quote.Kind(doc), quote.Status(doc, nil)),
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
			return &quote.Error{Code: quote.CodeNotOpen, Message: fmt.Sprintf("the %s is %s, not sent", quote.Kind(doc), quote.Status(doc, nil)),
				Fix: "Cancel a sent quote or order; a draft is simply not sent"}
		}
		o, err := outcomeOf(ctx, u, id)
		if err != nil {
			return err
		}
		if s := quote.Status(doc, o); s == quote.StatusDeclined || s == quote.StatusExpired {
			return &quote.Error{Code: quote.CodeAnswered, Message: fmt.Sprintf("the %s is %s already", quote.Kind(doc), s), Fix: "Nothing is left to cancel"}
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

// QuoteExpirer is who expires quotes past their validity date: the scheduler (FR-AR-01).
var QuoteExpirer = storage.Actor{Type: "scheduler", ID: "quotes.expiry"}

// outcomeOf is a quote's outcome, nil when it has none.
func outcomeOf(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (*storage.QuoteOutcome, error) {
	os, err := u.QuoteOutcomes().Of(ctx, []uuid.UUID{id})
	if err != nil {
		return nil, err
	}
	if o, ok := os[id]; ok {
		return &o, nil
	}
	return nil, nil
}

// QuoteAnswer is the customer's answer to a sent quote or order, as a person records it: the day, and for an
// acceptance the name of who accepted it on the customer's side; a note either way.
type QuoteAnswer struct {
	On   time.Time
	Name string
	Note string
}

// Accept records that the customer accepted a sent quote or order (FR-AR-01); Decline that they declined it. A quote
// has one answer, which never changes, and one that expired takes none (QUOTE-07); a quote past its validity date is
// still accepted when the customer says so in time, which the day records.
func (s Quotes) Accept(ctx context.Context, org, id uuid.UUID, a QuoteAnswer) (storage.QuoteOutcome, error) {
	return s.answer(ctx, org, id, storage.QuoteAccepted, a)
}

// Decline records that the customer declined a sent quote or order (FR-AR-01).
func (s Quotes) Decline(ctx context.Context, org, id uuid.UUID, a QuoteAnswer) (storage.QuoteOutcome, error) {
	return s.answer(ctx, org, id, storage.QuoteDeclined, a)
}

func (s Quotes) answer(ctx context.Context, org, id uuid.UUID, outcome string, a QuoteAnswer) (storage.QuoteOutcome, error) {
	name, note := strings.TrimSpace(a.Name), strings.TrimSpace(a.Note)
	if a.On.IsZero() {
		return storage.QuoteOutcome{}, &quote.Error{Code: quote.CodeAnswer, Message: "the answer has no date", Fix: "Enter the day the customer answered"}
	}
	if len(name) > quote.MaxName || len(note) > quote.MaxNote {
		return storage.QuoteOutcome{}, &quote.Error{Code: quote.CodeAnswer, Message: fmt.Sprintf("the name has %d characters and the note %d; the most are %d and %d",
			len(name), len(note), quote.MaxName, quote.MaxNote), Fix: "Shorten them"}
	}
	var out storage.QuoteOutcome
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Lock(ctx, id)
		if err != nil {
			return err
		}
		if err := isQuote(doc); err != nil {
			return err
		}
		o, err := outcomeOf(ctx, u, id)
		if err != nil {
			return err
		}
		switch st := quote.Status(doc, o); {
		case o != nil:
			return &quote.Error{Code: quote.CodeAnswered, Message: fmt.Sprintf("the %s is %s already", quote.Kind(doc), st), Fix: "A quote has one answer; send another quote"}
		case doc.Status != storage.DocumentPosted:
			return &quote.Error{Code: quote.CodeNotOpen, Message: fmt.Sprintf("the %s is %s, not sent", quote.Kind(doc), st), Fix: "Send it first"}
		}
		out, err = u.QuoteOutcomes().Record(ctx, storage.NewQuoteOutcome{DocumentID: id, Outcome: outcome, On: a.On, Name: name, Note: note})
		return err
	})
	return out, err
}

// ExpireDue expires the organisation's sent quotes whose validity date is before day and that have no answer
// (FR-AR-01), as the scheduler; an order without a validity date never expires. It answers the quotes it expired.
func (s Quotes) ExpireDue(ctx context.Context, org uuid.UUID, day time.Time) ([]uuid.UUID, error) {
	actor := QuoteExpirer
	ctx = storage.WithAudit(ctx, storage.AuditContext{Actor: &actor, Reason: "Past its validity date with no answer"})
	var out []uuid.UUID
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		for _, t := range []storage.DocumentType{storage.DocumentQuote, storage.DocumentSalesOrder} {
			f := storage.DocumentFilter{Type: t, Status: storage.DocumentPosted, Limit: 500}
			for {
				docs, err := u.Documents().Page(ctx, f)
				if err != nil {
					return err
				}
				ids := make([]uuid.UUID, 0, len(docs))
				for _, d := range docs {
					ids = append(ids, d.ID)
				}
				answered, err := u.QuoteOutcomes().Of(ctx, ids)
				if err != nil {
					return err
				}
				for _, doc := range docs {
					if _, ok := answered[doc.ID]; ok {
						continue
					}
					d, err := quote.Read(doc, settings.Base)
					if err != nil || d.ValidUntil.IsZero() || !d.ValidUntil.Before(day) {
						continue // unreadable ones are left to a person
					}
					if _, err := u.QuoteOutcomes().Record(ctx, storage.NewQuoteOutcome{DocumentID: doc.ID, Outcome: storage.QuoteExpired, On: day}); err != nil {
						return err
					}
					out = append(out, doc.ID)
				}
				if len(docs) < f.Limit {
					break
				}
				last := docs[len(docs)-1]
				f.AfterDate, f.AfterID = last.Date, &last.ID
			}
		}
		return nil
	})
	return out, err
}

// ExpireQuotesDueAll expires the quotes of every organisation past their validity date by its own today: the daily
// job quotes.expiry.
func ExpireQuotesDueAll(ctx context.Context, store storage.Store, now time.Time) (int, error) {
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return 0, err
	}
	s := Quotes{Store: store}
	n := 0
	var errs []error
	for _, org := range orgs {
		day, err := todayIn(ctx, store, org, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("organisation %s: %w", org, err))
			continue
		}
		ids, err := s.ExpireDue(ctx, org, day)
		n += len(ids)
		if err != nil {
			errs = append(errs, fmt.Errorf("organisation %s: %w", org, err))
		}
	}
	return n, errors.Join(errs...)
}
