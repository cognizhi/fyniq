// Package paymentbatch is a batch of payments to suppliers and claimants, paid from one SGD money account on one date
// (FR-BNK-12; PRD-001 rev 0.154 and rev 0.156; p1-payments-out design D1, D2): what it holds, the checks it is asked for,
// approved and released under, and how its items group into one payment per payee. A batch pays approved bills and
// expense claims whole, for what is open on them, to Singapore bank accounts in force (FR-CT-03, FR-AP-22).
package paymentbatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/billpayment"
	"github.com/cognizhi/fyniq/internal/domain/claim"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/payee"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable codes of a batch's refusals and warnings (ENG-23).
const (
	CodeNotBatch    = "PB-01" // the document is not a payment batch
	CodeBadDraft    = "PB-02" // the saved batch cannot be read
	CodeAccount     = "PB-03" // the money account is not an open SGD bank account
	CodeDate        = "PB-04" // the payment date is not a date
	CodeMethod      = "PB-05" // the method is not FAST or GIRO
	CodeEmpty       = "PB-06" // the batch pays nothing
	CodeTwice       = "PB-07" // an item is in the batch twice
	CodeItem        = "PB-08" // the item is not a posted bill or an approved expense claim
	CodeCurrency    = "PB-09" // the bill is not in SGD
	CodeNothingOpen = "PB-10" // nothing is open on the item
	CodeInAnother   = "PB-11" // the item is in another approved batch
	CodeNoAccount   = "PB-12" // the payee has no approved SGD account at a Singapore bank
	CodeWithholding = "PB-13" // the bill's withholding-tax decision is pending (FR-AP-19)
	CodeNotDraft    = "PB-14" // only a draft changes or is asked for
	CodeTooMany     = "PB-15" // more items than a batch holds
	CodeTooLong     = "PB-16" // the notes are too long
)

// The kinds of item.
const (
	KindBill  = "bill"
	KindClaim = "claim"
)

// The methods of payment a batch's file names.
const (
	FAST = "fast" // FAST: within minutes, up to the bank's limit
	GIRO = "giro" // GIRO: the next working days
)

// The limits of a batch.
const (
	MaxItems = 500
	MaxNotes = 500
)

// Item is a bill or an expense claim a batch pays whole, for what is open on it.
type Item struct {
	Kind       string    `json:"kind"`
	DocumentID uuid.UUID `json:"document_id"`
}

// Draft is a batch as entered.
type Draft struct {
	MoneyAccountID uuid.UUID
	PaymentDate    time.Time
	Method         string
	Items          []Item
	Notes          string
}

// Stored is a batch's version as its document keeps it: MoneyAccountID is the chart's money account, as a payment's. The
// database reads money_account_id, date and the items' document_id (FY240, FY241).
type Stored struct {
	MoneyAccountID uuid.UUID `json:"money_account_id"`
	PaymentDate    string    `json:"date"`
	Method         string    `json:"method"`
	Items          []Item    `json:"items"`
	Notes          string    `json:"notes,omitempty"`
}

// Problem is one rule that failed: the batch's own (DocumentID nil) or an item's.
type Problem struct {
	Code, Field, Message, Fix string
	DocumentID                *uuid.UUID
}

// Problems are a batch's failed rules, all at once (ENG-21).
type Problems []Problem

func (p Problems) Error() string {
	parts := make([]string, len(p))
	for i, x := range p {
		parts[i] = fmt.Sprintf("%s %s: %s", x.Code, x.Field, x.Message)
	}
	return strings.Join(parts, "; ")
}

// Error is a batch command's refusal with a stable code.
type Error struct {
	Code, Message, Fix string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Fix) }

// Read is a batch's version as stored.
func Read(doc storage.Document) (Stored, error) {
	if doc.Type != storage.DocumentPaymentBatch {
		return Stored{}, &Error{Code: CodeNotBatch, Message: fmt.Sprintf("document %s is a %s, not a payment batch", doc.ID, doc.Type), Fix: "Open it with its own module"}
	}
	var s Stored
	if err := json.Unmarshal(doc.Payload, &s); err != nil {
		return Stored{}, &Error{Code: CodeBadDraft, Message: "the saved batch cannot be read", Fix: "Prepare the batch again"}
	}
	return s, nil
}

// Encode is the stored version of d.
func Encode(d Draft) ([]byte, error) {
	items := d.Items
	if items == nil {
		items = []Item{}
	}
	return json.Marshal(Stored{MoneyAccountID: d.MoneyAccountID, PaymentDate: d.PaymentDate.Format(time.DateOnly), Method: d.Method, Items: items,
		Notes: strings.TrimSpace(d.Notes)})
}

// Check returns every rule d breaks by itself, or nil: its method, its date, its items' kinds and count, each item once.
func Check(d Draft) Problems {
	var out Problems
	add := func(code, field, message, fix string) { out = append(out, Problem{Code: code, Field: field, Message: message, Fix: fix}) }
	if d.Method != FAST && d.Method != GIRO {
		add(CodeMethod, "method", fmt.Sprintf("%q is not a method of payment", d.Method), "Choose fast or giro")
	}
	if d.PaymentDate.IsZero() {
		add(CodeDate, "payment_date", "is not a date", "Send the payment date as YYYY-MM-DD")
	}
	if d.MoneyAccountID == uuid.Nil {
		add(CodeAccount, "money_account_id", "no money account is named", "Choose the SGD bank account the batch is paid from")
	}
	if len([]rune(d.Notes)) > MaxNotes {
		add(CodeTooLong, "notes", fmt.Sprintf("is longer than %d characters", MaxNotes), "Shorten the notes")
	}
	switch {
	case len(d.Items) == 0:
		add(CodeEmpty, "items", "the batch pays nothing", "Add approved bills or expense claims")
	case len(d.Items) > MaxItems:
		add(CodeTooMany, "items", fmt.Sprintf("a batch pays at most %d items", MaxItems), "Split them over two batches")
	}
	seen := map[uuid.UUID]bool{}
	for i, x := range d.Items {
		field := fmt.Sprintf("items[%d]", i)
		id := x.DocumentID
		switch {
		case x.Kind != KindBill && x.Kind != KindClaim:
			out = append(out, Problem{Code: CodeItem, Field: field + ".kind", Message: fmt.Sprintf("%q is not a kind of item", x.Kind), Fix: "Choose bill or claim", DocumentID: &id})
		case seen[x.DocumentID]:
			out = append(out, Problem{Code: CodeTwice, Field: field, Message: "the item is in the batch twice", Fix: "Remove the second one", DocumentID: &id})
		}
		seen[x.DocumentID] = true
	}
	return out
}

// Line is an item as the batch pays it: its document, what is open on it, and the problems that stop it.
type Line struct {
	Item
	Number   string
	Date     string
	Open     money.Money
	Problems Problems
}

// Payment is one payee's part of a batch: a supplier's bills as one bill payment, or a claimant's claims as one
// reimbursement (design D5), into the payee's account in force.
type Payment struct {
	Payee   payee.Payee
	Account *storage.PayeeBankAccount // the account in force; nil: none
	Bank    *payee.Stored             // the account's bank, name and kind, from its change; nil: none
	Frozen  bool                      // a change of the payee's account waits for approval
	Lines   []Line
	Amount  money.Money
}

// Batch is a batch as its checks read it at a moment: its content, its money account, its payments by payee, its total,
// and what stops it (✖) or warns (⚠).
type Batch struct {
	Doc      storage.Document
	Stored   Stored
	Date     time.Time
	Account  *storage.MoneyAccount
	Payments []Payment
	Total    money.Money
	Errors   Problems
	Warnings Problems
}

// Accounts are the payees' accounts in force a batch pays, payee to change: what its approval's envelope holds, so an
// account changed after the approval stops the release (G4-055).
func (b Batch) Accounts() map[string]any {
	out := map[string]any{}
	for _, p := range b.Payments {
		change := ""
		if p.Account != nil {
			change = p.Account.ChangeID.String()
		}
		out[Key(p.Payee)] = change
	}
	return out
}

// Key names a payee in an envelope: vendor:<contact> or person:<user>.
func Key(p payee.Payee) string {
	if p.IsPerson() {
		return "person:" + p.PersonID.String()
	}
	return "vendor:" + p.ContactID.String()
}

// SGD is the currency a batch pays in (the owner's decision of 27 Oct 2026).
var SGD = money.MustLookup("SGD")

// Evaluate reads a batch and runs every check of design D2 at now, all at once (ENG-23): the money account, each item
// (posted and open, in SGD, not in another approved batch, no withholding decision pending), and each payee (an
// approved SGD account at a Singapore bank, no change waiting). Approved is every other approved, unreleased batch's
// items, by document; a batch is never in its own.
func Evaluate(ctx context.Context, u storage.UnitOfWork, doc storage.Document, settings ledger.Settings, approved map[uuid.UUID]uuid.UUID,
	now time.Time) (Batch, error) {
	s, err := Read(doc)
	if err != nil {
		return Batch{}, err
	}
	out := Batch{Doc: doc, Stored: s, Total: money.Zero(SGD)}
	date, derr := time.Parse(time.DateOnly, s.PaymentDate)
	out.Date = date
	d := Draft{MoneyAccountID: s.MoneyAccountID, PaymentDate: date, Method: s.Method, Items: s.Items, Notes: s.Notes}
	if derr != nil {
		d.PaymentDate = time.Time{}
	}
	out.Errors = Check(d)
	if s.MoneyAccountID != uuid.Nil {
		acc, err := moneyAccount(ctx, u, s.MoneyAccountID)
		switch {
		case err != nil:
			return Batch{}, err
		case acc == nil:
			out.Errors = append(out.Errors, Problem{Code: CodeAccount, Field: "money_account_id", Message: "the account is not a bank account in Banking",
				Fix: "Choose one of the organisation's SGD bank accounts, set up in Banking"})
		default:
			out.Account = acc
			if acc.Currency != SGD.Code || acc.Kind != storage.MoneyBank || acc.ClosedOn != nil && !acc.ClosedOn.After(date) {
				out.Errors = append(out.Errors, Problem{Code: CodeAccount, Field: "money_account_id",
					Message: fmt.Sprintf("%s is not an open SGD bank account", acc.Name), Fix: "Choose an open SGD bank account: a batch pays SGD by FAST or GIRO"})
			}
		}
	}
	base := settings.Base
	byPayee := map[payee.Payee]*Payment{}
	var order []payee.Payee
	claims := map[uuid.UUID]storage.Document{}
	var claimDocs []storage.Document
	for _, x := range s.Items {
		if x.Kind == KindClaim {
			c, err := u.Documents().Get(ctx, x.DocumentID)
			if err != nil && !errors.Is(err, ports.ErrNotFound) {
				return Batch{}, err
			}
			if err == nil {
				claims[c.ID] = c
				claimDocs = append(claimDocs, c)
			}
		}
	}
	owed, err := claim.Balances(ctx, u, claimDocs, base, time.Time{})
	if err != nil {
		return Batch{}, err
	}
	for i, x := range s.Items {
		id := x.DocumentID
		field := fmt.Sprintf("items[%d]", i)
		line := Line{Item: x, Open: money.Zero(SGD)}
		fail := func(code, message, fix string) {
			line.Problems = append(line.Problems, Problem{Code: code, Field: field, Message: message, Fix: fix, DocumentID: &id})
		}
		var who payee.Payee
		switch x.Kind {
		case KindBill:
			o, err := billpayment.OpenBill(ctx, u, x.DocumentID, settings)
			var be *billpayment.Error
			if errors.As(err, &be) {
				fail(CodeItem, be.Message, "Choose a posted bill")
				break
			} else if err != nil {
				return Batch{}, err
			}
			line.Number, line.Date = o.Number, o.Bill.Date
			draft, err := bill.Read(o.Bill, base)
			if err != nil {
				return Batch{}, err
			}
			who = payee.Vendor(draft.SupplierID)
			if o.Open.Currency().Code != SGD.Code {
				fail(CodeCurrency, fmt.Sprintf("%s is in %s; a batch pays SGD", name(o.Number, id), o.Open.Currency().Code),
					"Record its payment by hand: overseas and foreign-currency payments stay out of batches")
				break
			}
			line.Open = o.Open
			if o.Open.Sign() <= 0 {
				fail(CodeNothingOpen, fmt.Sprintf("nothing is open on %s", name(o.Number, id)), "Remove it: it is paid")
			}
			held, err := bill.HeldPayment(ctx, u, o.Bill, base, o.PostedAt)
			if err != nil {
				return Batch{}, err
			}
			if held != "" {
				fail(CodeWithholding, fmt.Sprintf("%s waits for withholding: %s", name(o.Number, id), held),
					"Record the tax agent's withholding decision for the supplier first (FR-AP-19)")
			}
		case KindClaim:
			c, ok := claims[x.DocumentID]
			if !ok || c.Type != storage.DocumentExpenseClaim || c.Status != storage.DocumentPosted {
				fail(CodeItem, fmt.Sprintf("%s is not an approved expense claim", x.DocumentID), "Choose an approved claim")
				break
			}
			line.Number, line.Date = c.Number, c.Date
			draft, err := claim.Read(c, base)
			if err != nil {
				return Batch{}, err
			}
			who = payee.Person(draft.ClaimantID)
			if b, ok := owed[c.ID]; ok {
				line.Open = b.Open
			}
			if line.Open.Sign() <= 0 {
				fail(CodeNothingOpen, fmt.Sprintf("nothing is owed on %s", name(c.Number, id)), "Remove it: it is paid back")
			}
		}
		if other, ok := approved[x.DocumentID]; ok && other != doc.ID {
			fail(CodeInAnother, fmt.Sprintf("%s is in another approved batch", name(line.Number, id)), "Release or cancel that batch first, or remove it here")
		}
		if who == (payee.Payee{}) {
			out.Errors = append(out.Errors, line.Problems...)
			continue
		}
		p := byPayee[who]
		if p == nil {
			p = &Payment{Payee: who, Amount: money.Zero(SGD)}
			byPayee[who] = p
			order = append(order, who)
		}
		p.Lines = append(p.Lines, line)
		out.Errors = append(out.Errors, line.Problems...)
		if line.Open.Sign() > 0 {
			if p.Amount, err = p.Amount.Add(line.Open); err != nil {
				return Batch{}, err
			}
		}
	}
	for _, who := range order {
		p := byPayee[who]
		if err := account(ctx, u, p, now); err != nil {
			return Batch{}, err
		}
		for _, l := range p.Lines {
			id := l.DocumentID
			switch {
			case p.Frozen:
				out.Errors = append(out.Errors, Problem{Code: payee.CodeFrozen, Field: "items", DocumentID: &id,
					Message: fmt.Sprintf("a change of the bank account %s pays into waits for approval, so payments are frozen", name(l.Number, id)),
					Fix: "Have the change approved, rejected or withdrawn first"})
			case p.Account == nil || p.Bank == nil || p.Bank.Kind != payee.Singapore:
				out.Errors = append(out.Errors, Problem{Code: CodeNoAccount, Field: "items", DocumentID: &id,
					Message: fmt.Sprintf("the payee of %s has no approved SGD account at a Singapore bank", name(l.Number, id)),
					Fix: "Have their bank details entered and approved first"})
			}
		}
		if out.Total, err = out.Total.Add(p.Amount); err != nil {
			return Batch{}, err
		}
		out.Payments = append(out.Payments, *p)
	}
	return out, nil
}

// moneyAccount is Banking's account on a chart account, nil when it has none. A batch names the chart's money account, as
// a payment recorded by hand does; Banking's account gives its kind, currency and identifier.
func moneyAccount(ctx context.Context, u storage.UnitOfWork, chart uuid.UUID) (*storage.MoneyAccount, error) {
	all, err := u.Banking().MoneyAccounts(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range all {
		if m.AccountID == chart {
			return &m, nil
		}
	}
	return nil, nil
}

// account finds a payment's account in force in SGD, its bank, and whether a change of it waits.
func account(ctx context.Context, u storage.UnitOfWork, p *Payment, now time.Time) error {
	waiting, err := payee.WaitingFor(ctx, u, p.Payee, now)
	if err != nil {
		return err
	}
	p.Frozen = len(waiting) > 0
	acc, err := payee.InForceFor(ctx, u, p.Payee, SGD.Code)
	if err != nil || acc == nil {
		return err
	}
	change, err := u.Documents().Get(ctx, acc.ChangeID)
	if err != nil {
		return err
	}
	s, err := payee.Read(change)
	if err != nil {
		return err
	}
	p.Account, p.Bank = acc, &s
	return nil
}

// name is a document as a message names it: its number, or its ID while it has none.
func name(number string, id uuid.UUID) string {
	if number != "" {
		return number
	}
	return id.String()
}

// Approved are the items of every approved, unreleased batch, document to batch: what another batch may not pay, and what
// a payment recorded by hand may not settle (FY241).
func Approved(ctx context.Context, u storage.UnitOfWork) (map[uuid.UUID]uuid.UUID, error) {
	out := map[uuid.UUID]uuid.UUID{}
	var afterDate string
	var afterID *uuid.UUID
	for {
		page, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentPaymentBatch, Status: storage.DocumentPosted,
			AfterDate: afterDate, AfterID: afterID, Limit: 200})
		if err != nil {
			return nil, err
		}
		for _, d := range page {
			r, err := u.PaymentBatches().ReleaseOf(ctx, d.ID)
			if err != nil {
				return nil, err
			}
			if r != nil {
				continue
			}
			s, err := Read(d)
			if err != nil {
				return nil, err
			}
			for _, x := range s.Items {
				out[x.DocumentID] = d.ID
			}
		}
		if len(page) < 200 {
			return out, nil
		}
		afterDate, afterID = page[len(page)-1].Date, &page[len(page)-1].ID
	}
}

// Suggested are the items a new batch for a payment date starts with (FR-AP-13): every posted SGD bill with something
// open whose planned payment date is on or before it, then every approved expense claim with something owed, oldest first.
// None in an approved batch already. A suggestion never adds itself: the person preparing the batch keeps or drops each.
func Suggested(ctx context.Context, u storage.UnitOfWork, settings ledger.Settings, on time.Time) ([]Item, error) {
	approved, err := Approved(ctx, u)
	if err != nil {
		return nil, err
	}
	bills, err := all(ctx, u, storage.DocumentBill)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(bills))
	for i, b := range bills {
		ids[i] = b.ID
	}
	plans, err := u.BillPlans().Of(ctx, ids)
	if err != nil {
		return nil, err
	}
	var out []Item
	for _, b := range bills {
		plan, ok := plans[b.ID]
		if _, taken := approved[b.ID]; taken || !ok || plan.PlannedOn == nil || plan.PlannedOn.After(on) {
			continue
		}
		o, err := billpayment.OpenBill(ctx, u, b.ID, settings)
		var be *billpayment.Error
		if errors.As(err, &be) {
			continue
		} else if err != nil {
			return nil, err
		}
		if o.Open.Currency().Code == SGD.Code && o.Open.Sign() > 0 {
			out = append(out, Item{Kind: KindBill, DocumentID: b.ID})
		}
	}
	claims, err := all(ctx, u, storage.DocumentExpenseClaim)
	if err != nil {
		return nil, err
	}
	owed, err := claim.Balances(ctx, u, claims, settings.Base, time.Time{})
	if err != nil {
		return nil, err
	}
	for _, c := range claims {
		if _, taken := approved[c.ID]; taken {
			continue
		}
		if b, ok := owed[c.ID]; ok && b.Open.Sign() > 0 {
			out = append(out, Item{Kind: KindClaim, DocumentID: c.ID})
		}
	}
	return out, nil
}

// all are the posted documents of a type, oldest first.
func all(ctx context.Context, u storage.UnitOfWork, t storage.DocumentType) ([]storage.Document, error) {
	var out []storage.Document
	var afterDate string
	var afterID *uuid.UUID
	for {
		page, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: t, Status: storage.DocumentPosted, AfterDate: afterDate, AfterID: afterID, Limit: 500})
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		if len(page) < 500 {
			break
		}
		afterDate, afterID = page[len(page)-1].Date, &page[len(page)-1].ID
	}
	slices.SortStableFunc(out, func(x, y storage.Document) int { return strings.Compare(x.Date, y.Date) })
	return out, nil
}
