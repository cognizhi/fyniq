package statement

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/subledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Sent is a statement as its document keeps it: what it said on the day it was made, never recomputed.
type Sent struct {
	Date       time.Time // the day it was made
	CustomerID uuid.UUID
	Currency   money.Currency
	From, To   time.Time
	Opening    money.Money
	Lines      []SentLine
	Closing    money.Money
	Overdue    money.Money
	Ageing     [5]money.Money // by subledger.Buckets
	Credits    money.Money    // unapplied credits in the closing balance, negative
}

// SentLine is a statement's line as kept.
type SentLine struct {
	Date        time.Time
	Kind        string
	DocumentID  uuid.UUID
	Number      string
	Description string
	Amount      money.Money
	Balance     money.Money
}

type sentPayload struct {
	Date       string            `json:"date"`
	CustomerID uuid.UUID         `json:"customer_id"`
	Currency   string            `json:"currency"`
	From       string            `json:"period_start"`
	To         string            `json:"period_end"`
	Opening    string            `json:"opening_balance"`
	Lines      []sentLinePayload `json:"lines"`
	Closing    string            `json:"closing_balance"`
	Overdue    string            `json:"overdue"`
	Ageing     map[string]string `json:"ageing"`
	Credits    string            `json:"credits"`
}

type sentLinePayload struct {
	Date        string    `json:"date"`
	Kind        string    `json:"kind"`
	DocumentID  uuid.UUID `json:"document_id"`
	Number      string    `json:"number,omitempty"`
	Description string    `json:"description"`
	Amount      string    `json:"amount"`
	Balance     string    `json:"balance"`
}

// Encode is a statement as its document's version stores it, made on day.
func Encode(s Statement, day time.Time) ([]byte, error) {
	p := sentPayload{Date: day.Format(time.DateOnly), CustomerID: s.CustomerID, Currency: s.Currency.Code, From: s.From.Format(time.DateOnly),
		To: s.To.Format(time.DateOnly), Opening: s.Opening.String(), Lines: []sentLinePayload{}, Closing: s.Closing.String(),
		Overdue: s.Overdue.String(), Ageing: map[string]string{}, Credits: s.Ageing.Credits.String()}
	for i, b := range subledger.Buckets {
		p.Ageing[b] = s.Ageing.Buckets[i].String()
	}
	for _, l := range s.Lines {
		p.Lines = append(p.Lines, sentLinePayload{Date: l.Date.Format(time.DateOnly), Kind: l.Kind, DocumentID: l.DocumentID, Number: l.Number,
			Description: l.Description(), Amount: l.Amount.String(), Balance: l.Balance.String()})
	}
	return json.Marshal(p)
}

// Read reads a statement's document as it was made.
func Read(doc storage.Document) (Sent, error) {
	if doc.Type != storage.DocumentStatement {
		return Sent{}, &Error{CodeNotStatement, fmt.Sprintf("document %s is a %s, not a statement", doc.ID, doc.Type), "Open it with its own module"}
	}
	var p sentPayload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return Sent{}, fmt.Errorf("statement %s: %w", doc.ID, err)
	}
	c, err := money.Lookup(p.Currency)
	if err != nil {
		return Sent{}, err
	}
	day := func(s string) (time.Time, error) { return time.Parse(time.DateOnly, s) }
	amount := func(s string) (money.Money, error) { return money.ParseStored(s, c) }
	out := Sent{CustomerID: p.CustomerID, Currency: c}
	if out.Date, err = day(p.Date); err != nil {
		return Sent{}, err
	}
	if out.From, err = day(p.From); err != nil {
		return Sent{}, err
	}
	if out.To, err = day(p.To); err != nil {
		return Sent{}, err
	}
	for _, x := range []struct {
		dst *money.Money
		src string
	}{{&out.Opening, p.Opening}, {&out.Closing, p.Closing}, {&out.Overdue, p.Overdue}, {&out.Credits, p.Credits}} {
		if *x.dst, err = amount(x.src); err != nil {
			return Sent{}, err
		}
	}
	for i, b := range subledger.Buckets {
		if out.Ageing[i], err = amount(p.Ageing[b]); err != nil {
			return Sent{}, err
		}
	}
	for _, l := range p.Lines {
		x := SentLine{Kind: l.Kind, DocumentID: l.DocumentID, Number: l.Number, Description: l.Description}
		if x.Date, err = day(l.Date); err != nil {
			return Sent{}, err
		}
		if x.Amount, err = amount(l.Amount); err != nil {
			return Sent{}, err
		}
		if x.Balance, err = amount(l.Balance); err != nil {
			return Sent{}, err
		}
		out.Lines = append(out.Lines, x)
	}
	return out, nil
}

// Make makes customer's statement in cur from from to to as at today, in u, and keeps it as a statement document, sent
// and numbered from its sequence (ST-): what a person then emails. One that does not add up (STATEMENT-03), or of a
// currency in which the customer has nothing (STATEMENT-04), is refused.
func Make(ctx context.Context, u storage.UnitOfWork, base money.Currency, customer uuid.UUID, cur money.Currency, from, to, today time.Time) (storage.Document, Statement, error) {
	if err := CheckPeriod(from, to, today); err != nil {
		return storage.Document{}, Statement{}, err
	}
	s, err := Of(ctx, u, base, customer, cur, from, to)
	if err != nil {
		return storage.Document{}, Statement{}, err
	}
	if !s.AddsUp() {
		return storage.Document{}, s, &Error{CodeAddsUp, fmt.Sprintf("the statement's lines come to %s less than the closing balance the receivables give", s.Difference),
			"Look for a document of the customer's that moved its balance outside invoices, credit notes, receipts, refunds and write-offs; the statement is not made until it adds up"}
	}
	if s.Opening.IsZero() && s.Closing.IsZero() && len(s.Lines) == 0 {
		return storage.Document{}, s, &Error{CodeCurrency, fmt.Sprintf("%s has nothing in %s in the period", s.CustomerName, cur.Code),
			"Choose a currency the customer trades in, or another period"}
	}
	payload, err := Encode(s, today)
	if err != nil {
		return storage.Document{}, s, err
	}
	doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentStatement, Payload: payload})
	if err != nil {
		return storage.Document{}, s, err
	}
	if doc.Number, err = u.Sequences().Number(ctx, doc.ID, doc.Type); err != nil {
		return storage.Document{}, s, err
	}
	if err := u.Documents().SetStatus(ctx, doc.ID, storage.DocumentPosted); err != nil {
		return storage.Document{}, s, err
	}
	doc, err = u.Documents().Get(ctx, doc.ID)
	return doc, s, err
}
