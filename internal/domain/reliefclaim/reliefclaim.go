// Package reliefclaim is the claim of GST bad debt relief (FR-AR-11; p2-tax-filings task 3.2): the document that records it, and what the
// posting rule bad_debt_relief@1 needs from the books. A claim names one posted bad-debt write-off, the day it is claimed, the F5 period
// it goes in and a note; it keeps, as it was when it posted, every figure the rule read (the amounts written off and owed, the output
// tax, what earlier claims of the invoice were for), so the entry replays exactly (ENG-06). It posts at once, is never edited, and is
// undone by a reversal, through the posting service in package journal.
package reliefclaim

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/gst"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/domain/receipt"
	"github.com/cognizhi/fyniq/internal/domain/writeoff"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable codes of a claim's refusals (ENG-23).
const (
	CodeNotClaim   = "RELIEF-01" // the document is not a claim
	CodeBadClaim   = "RELIEF-02" // the saved claim cannot be read
	CodeWriteOff   = "RELIEF-03" // no posted bad-debt write-off of an invoice
	CodeClaimed    = "RELIEF-04" // the write-off is claimed already
	CodeCredited   = "RELIEF-05" // a credit note credits the invoice
	CodeNoPack     = "RELIEF-06" // no sg-tax-filings version holds the dates
	CodeNotYet     = "RELIEF-07" // the twelve months have not passed
	CodeExpired    = "RELIEF-08" // the five years are over
	CodeFuture     = "RELIEF-09" // dated after today
	CodePeriod     = "RELIEF-10" // the F5 period ends before the claim
	CodeNote       = "RELIEF-11" // the note is too long
	CodeNotPosted  = "RELIEF-12" // only a posted claim is voided
	CodeVoidDate   = "RELIEF-13" // a void dated before the claim
	CodeNoOutputGS = "RELIEF-14" // the invoice's entry holds no output tax
)

// MaxNote is the longest note a claim keeps.
const MaxNote = 1000

// Error is a claim command's refusal with a stable code.
type Error struct {
	Code    string
	Message string
	Fix     string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Fix) }

// Request is what a person enters: the write-off, the day, the F5 period it goes in (its last day) and a note.
type Request struct {
	WriteOffID  uuid.UUID
	Date        time.Time
	F5PeriodEnd time.Time // zero when left out
	Note        string
}

type payload struct {
	WriteOffID    uuid.UUID   `json:"write_off_id"`
	InvoiceID     uuid.UUID   `json:"invoice_id"`
	Date          string      `json:"date"`
	F5PeriodEnd   string      `json:"f5_period_end,omitempty"`
	Note          string      `json:"note,omitempty"`
	Pack          string      `json:"pack"`
	Written       money.Money `json:"written"`
	Owed          money.Money `json:"owed"`
	WrittenBefore money.Money `json:"written_before"`
	Output        money.Money `json:"output"`
	ClaimedBefore money.Money `json:"claimed_before"`
	AccountID     uuid.UUID   `json:"account_id"`
	Memo          string      `json:"memo"`
	GST           money.Money `json:"gst"`
	EligibleFrom  string      `json:"eligible_from"`
	ClaimBy       string      `json:"claim_by"`
}

// Claim is a saved claim as the screens and the replay read it.
type Claim struct {
	WriteOffID   uuid.UUID
	InvoiceID    uuid.UUID
	Date         time.Time
	F5PeriodEnd  time.Time
	Note         string
	Pack         string
	GST          money.Money
	Written      money.Money
	Rule         ledger.BadDebtRelief // as the rule read it
	EligibleFrom time.Time
	ClaimBy      time.Time
}

// IsClaim says whether a document is a claim of relief.
func IsClaim(doc storage.Document) bool { return doc.Type == storage.DocumentBadDebtRelief }

// Read reads a claim version as it was saved.
func Read(doc storage.Document) (Claim, error) {
	if !IsClaim(doc) {
		return Claim{}, &Error{CodeNotClaim, fmt.Sprintf("document %s is a %s, not a claim of relief", doc.ID, doc.Type), "Open it with its own module"}
	}
	var p payload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return Claim{}, &Error{CodeBadClaim, "the saved claim cannot be read: " + err.Error(), "Contact support: a claim is never edited"}
	}
	day := func(s string) (time.Time, error) {
		if s == "" {
			return time.Time{}, nil
		}
		return time.Parse(time.DateOnly, s)
	}
	c := Claim{WriteOffID: p.WriteOffID, InvoiceID: p.InvoiceID, Note: p.Note, Pack: p.Pack, GST: p.GST, Written: p.Written}
	var err error
	for _, x := range []struct {
		to *time.Time
		s  string
	}{{&c.Date, p.Date}, {&c.F5PeriodEnd, p.F5PeriodEnd}, {&c.EligibleFrom, p.EligibleFrom}, {&c.ClaimBy, p.ClaimBy}} {
		if *x.to, err = day(x.s); err != nil {
			return Claim{}, &Error{CodeBadClaim, "the saved claim cannot be read: the date " + x.s, "Contact support: a claim is never edited"}
		}
	}
	c.Rule = ledger.BadDebtRelief{ID: doc.ID, Version: doc.Version, Date: c.Date, Memo: p.Memo, WriteOffID: p.WriteOffID, InvoiceID: p.InvoiceID, Written: p.Written,
		Owed: p.Owed, WrittenBefore: p.WrittenBefore, Output: p.Output, ClaimedBefore: p.ClaimedBefore, Account: p.AccountID}
	return c, nil
}

// Prepared is a claim as posting it would post it.
type Prepared struct {
	Payload []byte
	Rule    ledger.BadDebtRelief
	GST     money.Money
	Relief  pack.Relief
}

func isLiveClaim(doc storage.Document) bool {
	return IsClaim(doc) && doc.Status == storage.DocumentPosted
}

// Earlier is what an invoice's live claims are for: the amounts written off (in the invoice's currency) and the GST claimed (in the base
// currency), over every posted claim whose invoice is invoiceID.
func Earlier(ctx context.Context, u storage.UnitOfWork, invoiceID uuid.UUID, cur money.Currency, base money.Currency) (written, claimed money.Money, err error) {
	written, claimed = money.Zero(cur), money.Zero(base)
	filter := storage.DocumentFilter{Type: storage.DocumentBadDebtRelief, Status: storage.DocumentPosted, Limit: 200}
	for {
		page, err := u.Documents().Page(ctx, filter)
		if err != nil {
			return written, claimed, err
		}
		for _, doc := range page {
			c, err := Read(doc)
			if err != nil {
				return written, claimed, err
			}
			if c.InvoiceID != invoiceID {
				continue
			}
			if written, err = written.Add(c.Written); err != nil {
				return written, claimed, err
			}
			if claimed, err = claimed.Add(c.GST); err != nil {
				return written, claimed, err
			}
		}
		if len(page) < filter.Limit {
			return written, claimed, nil
		}
		last := page[len(page)-1]
		filter.AfterDate, filter.AfterID = last.Date, &last.ID
	}
}

// OutputOf is the output tax an invoice's entry posted to the GST control account, in the base currency: what the invoice left there, net
// of what the deposits it deducted had borne.
func OutputOf(ctx context.Context, u storage.UnitOfWork, inv storage.Document, s ledger.Settings) (money.Money, error) {
	e, err := u.Ledger().ByIdentity(ctx, invoice.IdentityOf(inv))
	if err != nil {
		return money.Money{}, err
	}
	out := money.Zero(s.Base)
	for _, l := range e.Lines {
		if s.Accounts[l.AccountID].Role != ledger.RoleGSTControl {
			continue
		}
		base, err := money.ParseStored(string(l.BaseAmount), s.Base)
		if err != nil {
			return money.Money{}, err
		}
		if out, err = out.Sub(base); err != nil {
			return money.Money{}, err
		}
	}
	return out, nil
}

// Prepare reads the write-off a request names and the books behind it, and returns the claim as it would post: refused, with a code and a way
// to fix it, unless the write-off is a posted bad-debt write-off of a posted invoice that no credit note credits, not claimed, within the
// sg-tax-filings pack's dates on the day claimed, and the day is not after today. The invoice must be locked first by a command that writes.
func Prepare(ctx context.Context, u storage.UnitOfWork, r Request, s ledger.Settings, today time.Time) (Prepared, error) {
	refuse := func(code, msg, fix string) (Prepared, error) { return Prepared{}, &Error{code, msg, fix} }
	if len(strings.TrimSpace(r.Note)) > MaxNote {
		return refuse(CodeNote, fmt.Sprintf("the note has %d characters; the most is %d", len(strings.TrimSpace(r.Note)), MaxNote), "Shorten it")
	}
	doc, err := u.Documents().Get(ctx, r.WriteOffID)
	if errors.Is(err, ports.ErrNotFound) {
		return refuse(CodeWriteOff, fmt.Sprintf("write-off %s does not exist in this organisation", r.WriteOffID), "Choose a posted write-off from the bad-debt queue")
	}
	if err != nil {
		return Prepared{}, err
	}
	if doc.Type != storage.DocumentWriteOff || doc.Status != storage.DocumentPosted {
		return refuse(CodeWriteOff, fmt.Sprintf("%s is not a posted write-off", orID(doc)), "Choose a posted write-off from the bad-debt queue")
	}
	w, err := writeoff.Read(doc)
	if err != nil {
		return Prepared{}, err
	}
	if w.Reason != writeoff.ReasonBadDebt {
		return refuse(CodeWriteOff, fmt.Sprintf("%s is written off as %s, not as a bad debt", orID(doc), w.Reason), "GST bad debt relief is for a debt written off as bad")
	}
	if r.Date.After(today) {
		return refuse(CodeFuture, fmt.Sprintf("the claim is dated %s, after today, %s", r.Date.Format(time.DateOnly), today.Format(time.DateOnly)), "Date it today or earlier")
	}
	if !r.F5PeriodEnd.IsZero() && r.F5PeriodEnd.Before(r.Date) {
		return refuse(CodePeriod, fmt.Sprintf("the F5 period ends on %s, before the claim on %s", r.F5PeriodEnd.Format(time.DateOnly), r.Date.Format(time.DateOnly)), "Name the period the claim goes in")
	}
	inv, err := u.Documents().Get(ctx, w.InvoiceID)
	if err != nil {
		return Prepared{}, err
	}
	o, err := receipt.OpenInvoice(ctx, u, w.InvoiceID, s)
	var re *receipt.Error
	if errors.As(err, &re) {
		return refuse(CodeWriteOff, re.Message, re.Fix)
	}
	if err != nil {
		return Prepared{}, err
	}
	// the claim: once per write-off, whatever its document's number
	live := storage.DocumentFilter{Type: storage.DocumentBadDebtRelief, Status: storage.DocumentPosted, Limit: 200}
	for {
		page, err := u.Documents().Page(ctx, live)
		if err != nil {
			return Prepared{}, err
		}
		for _, d := range page {
			if c, err := Read(d); err == nil && c.WriteOffID == r.WriteOffID {
				return refuse(CodeClaimed, fmt.Sprintf("%s is claimed already, on %s", orID(doc), c.Date.Format(time.DateOnly)), "Void that claim to claim again")
			}
		}
		if len(page) < live.Limit {
			break
		}
		last := page[len(page)-1]
		live.AfterDate, live.AfterID = last.Date, &last.ID
	}
	if credited, err := hasCreditNote(ctx, u, w.InvoiceID, s.Base); err != nil {
		return Prepared{}, err
	} else if credited {
		return refuse(CodeCredited, fmt.Sprintf("a credit note credits %s, which adjusted its output tax", orID(inv)), "Claim relief by hand with the accountant: the output tax left is not the invoice's")
	}
	d, err := invoice.Read(inv, s.Base)
	if err != nil {
		return Prepared{}, err
	}
	v, ok, err := gst.PackOn(ctx, u, "sg-tax-filings", d.Date)
	if err != nil {
		return Prepared{}, err
	}
	if !ok {
		return refuse(CodeNoPack, "no sg-tax-filings version holds the dates for a supply on "+d.Date.Format(time.DateOnly), "Activate the statutory pack that covers it")
	}
	rel, err := pack.BadDebtRelief(v, d.Date, w.Date, nil, r.Date)
	if err != nil {
		return Prepared{}, err
	}
	switch rel.Status {
	case pack.ReliefNotYet:
		return refuse(CodeNotYet, fmt.Sprintf("relief may be claimed from %s: twelve months from the supply on %s have not passed", rel.EligibleFrom.Format(time.DateOnly), d.Date.Format(time.DateOnly)),
			"Claim on or after that day, unless the debtor was insolvent first")
	case pack.ReliefExpired:
		return refuse(CodeExpired, fmt.Sprintf("the last day to claim was %s, five years from the supply on %s", rel.ClaimBy.Format(time.DateOnly), d.Date.Format(time.DateOnly)), "Relief out of time cannot be claimed")
	}
	output, err := OutputOf(ctx, u, inv, s)
	if err != nil {
		return Prepared{}, err
	}
	before, claimed, err := Earlier(ctx, u, w.InvoiceID, o.Total.Currency(), s.Base)
	if err != nil {
		return Prepared{}, err
	}
	memo := "GST bad debt relief on " + orID(doc)
	if o.Number != "" {
		memo += " (" + o.Number + ")"
	}
	if len(memo) > ledger.MaxMemo {
		memo = memo[:ledger.MaxMemo]
	}
	account := w.AccountID
	rule := ledger.BadDebtRelief{ID: uuid.Nil, Version: 1, Date: r.Date, Memo: memo, WriteOffID: r.WriteOffID, InvoiceID: w.InvoiceID, Written: w.Amount, Owed: o.Total,
		WrittenBefore: before, Output: output, ClaimedBefore: claimed, Account: account}
	share, err := ledger.ReliefOf(rule, s.Base)
	if err != nil {
		return refuse(CodeWriteOff, err.Error(), "Claim for a write-off of what the invoice owed, once")
	}
	p := payload{WriteOffID: r.WriteOffID, InvoiceID: w.InvoiceID, Date: r.Date.Format(time.DateOnly), Note: strings.TrimSpace(r.Note), Pack: v.ID, Written: w.Amount, Owed: o.Total,
		WrittenBefore: before, Output: output, ClaimedBefore: claimed, AccountID: account, Memo: memo, GST: share, EligibleFrom: rel.EligibleFrom.Format(time.DateOnly),
		ClaimBy: rel.ClaimBy.Format(time.DateOnly)}
	if !r.F5PeriodEnd.IsZero() {
		p.F5PeriodEnd = r.F5PeriodEnd.Format(time.DateOnly)
	}
	b, err := json.Marshal(p)
	if err != nil {
		return Prepared{}, err
	}
	return Prepared{Payload: b, Rule: rule, GST: share, Relief: rel}, nil
}

func orID(doc storage.Document) string {
	if doc.Number != "" {
		return doc.Number
	}
	return doc.ID.String()
}

// hasCreditNote says whether an issued credit note credits the invoice.
func hasCreditNote(ctx context.Context, u storage.UnitOfWork, invoiceID uuid.UUID, base money.Currency) (bool, error) {
	f := storage.DocumentFilter{Type: storage.DocumentCreditNote, Status: storage.DocumentPosted, Limit: 200}
	for {
		page, err := u.Documents().Page(ctx, f)
		if err != nil {
			return false, err
		}
		for _, d := range page {
			if c, err := invoice.Read(d, base); err == nil && c.Credits == invoiceID {
				return true, nil
			}
		}
		if len(page) < f.Limit {
			return false, nil
		}
		last := page[len(page)-1]
		f.AfterDate, f.AfterID = last.Date, &last.ID
	}
}
