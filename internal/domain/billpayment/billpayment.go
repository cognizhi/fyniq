// Package billpayment is a payment of suppliers' bills recorded by hand (FR-AP-02, FR-AR-18; the owner's decision of
// 22 Oct 2026, until bank import matches payments): its content as it is saved, and what the posting rule needs from
// it. Reading a payment for the rule happens here: its supplier, the money account it was paid from, the rate (the
// typed one, else the cascade's), and each bill it settles as the bill stands: posted, the supplier's, in the
// payment's currency, with the amount and the carrying amount still open. The posting service in package journal
// writes it (the one posting path).
package billpayment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/subledger"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable codes of a bill payment's refusals (ENG-23).
const (
	CodeNotPayment = "BILLPAY-01" // the document is not a bill payment
	CodeBadDraft   = "BILLPAY-02" // the saved payment cannot be read
	CodeSupplier   = "BILLPAY-03" // no supplier, or one that is not a vendor
	CodeBill       = "BILLPAY-04" // a bill that is not a posted bill of the payment's supplier and currency, or named twice
	CodeOverpaid   = "BILLPAY-05" // more than a bill has open
	CodeNotPosted  = "BILLPAY-06" // only a posted payment is voided
	CodeVoidDate   = "BILLPAY-07" // a void dated before the payment
	CodeTooLong    = "BILLPAY-08" // the reference or the notes are too long
	CodeTooMany    = "BILLPAY-09" // more bills than one payment settles
	CodeHeld       = "BILLPAY-11" // a bill whose withholding the tax agent has not decided, or decided anew since it posted (FR-AP-19)
	CodeTolerance  = "BILLPAY-12" // what a payment in full leaves of a bill is above the settlement tolerance (FR-BNK-08)
)

// The longest reference and notes a payment keeps, and the most bills it settles.
const (
	MaxReference = 140
	MaxNotes     = 2000
	MaxBills     = 100
)

// Error is a bill payment command's refusal with a stable code.
type Error struct {
	Code    string
	Message string
	Fix     string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Fix) }

// Part is what a payment settles of one bill, in the bill's currency. InFull settles the bill in full: what is left of
// it after Amount clears as a settlement difference within tolerance (FR-BNK-08).
type Part struct {
	BillID uuid.UUID
	Amount money.Money
	InFull bool
}

// Draft is a payment as recorded: the supplier, the date the money left, the money account it left, the bills'
// currency and its rate, what the account paid when it is in the base currency and the bills are not, the bank's
// reference, the parts it settles, what is left over as a credit with the supplier, and notes.
type Draft struct {
	SupplierID uuid.UUID
	Date       time.Time
	AccountID  uuid.UUID
	Currency   money.Currency
	Rate       *money.Rate  // as typed; nil takes the cascade's rate for the date (ENG-40)
	Paid       *money.Money // in the base currency
	Reference  string
	Bills      []Part
	Unapplied  money.Money
	Notes      string
}

// payload is a payment as a document version stores it: dates as text, amounts with their currency.
type payload struct {
	SupplierID   uuid.UUID     `json:"supplier_id"`
	Date         string        `json:"date"`
	AccountID    uuid.UUID     `json:"money_account_id"`
	Currency     string        `json:"currency"`
	FXRate       string        `json:"fx_rate,omitempty"`
	FXRateSource string        `json:"fx_rate_source,omitempty"`
	Paid         *money.Money  `json:"paid,omitempty"`
	Reference    string        `json:"reference,omitempty"`
	Bills        []payloadPart `json:"bills"`
	Unapplied    money.Money   `json:"unapplied"`
	Notes        string        `json:"notes,omitempty"`
}

type payloadPart struct {
	BillID uuid.UUID   `json:"bill_id"`
	Amount money.Money `json:"amount"`
	InFull bool        `json:"in_full,omitempty"`
}

// Encode is a draft as a document version stores it.
func Encode(d Draft) ([]byte, error) {
	if len(d.Reference) > MaxReference {
		return nil, &Error{CodeTooLong, fmt.Sprintf("the reference has %d characters; the most is %d", len(d.Reference), MaxReference), "Shorten it"}
	}
	if len(d.Notes) > MaxNotes {
		return nil, &Error{CodeTooLong, fmt.Sprintf("the notes have %d characters; the most is %d", len(d.Notes), MaxNotes), "Shorten them"}
	}
	if len(d.Bills) > MaxBills {
		return nil, &Error{CodeTooMany, fmt.Sprintf("the payment settles %d bills; the most is %d", len(d.Bills), MaxBills), "Record it as more than one payment"}
	}
	p := payload{SupplierID: d.SupplierID, AccountID: d.AccountID, Currency: d.Currency.Code, Paid: d.Paid, Reference: d.Reference,
		Bills: []payloadPart{}, Unapplied: d.Unapplied, Notes: d.Notes}
	if !d.Date.IsZero() {
		p.Date = d.Date.Format(time.DateOnly)
	}
	if d.Rate != nil {
		p.FXRate, p.FXRateSource = d.Rate.String(), d.Rate.Source
	}
	for _, x := range d.Bills {
		p.Bills = append(p.Bills, payloadPart{BillID: x.BillID, Amount: x.Amount, InFull: x.InFull})
	}
	return json.Marshal(p)
}

// Read reads a payment version as it was saved; base is the organisation's base currency, which a typed rate converts
// into.
func Read(doc storage.Document, base money.Currency) (Draft, error) {
	if doc.Type != storage.DocumentBillPayment {
		return Draft{}, &Error{CodeNotPayment, fmt.Sprintf("document %s is a %s, not a bill payment", doc.ID, doc.Type), "Open it with its own module"}
	}
	bad := func(what string) error {
		return &Error{CodeBadDraft, fmt.Sprintf("the saved payment cannot be read: %s", what), "Record the payment again"}
	}
	var p payload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return Draft{}, bad(err.Error())
	}
	cur, err := money.Lookup(p.Currency)
	if err != nil {
		return Draft{}, bad(err.Error())
	}
	d := Draft{SupplierID: p.SupplierID, AccountID: p.AccountID, Currency: cur, Paid: p.Paid, Reference: p.Reference, Unapplied: p.Unapplied, Notes: p.Notes}
	if p.Unapplied.Currency().Code == "" {
		d.Unapplied = money.Zero(cur)
	}
	if p.Date != "" {
		if d.Date, err = time.Parse(time.DateOnly, p.Date); err != nil {
			return Draft{}, bad(fmt.Sprintf("the date %q", p.Date))
		}
	}
	if p.FXRate != "" {
		r, err := money.ParseRate(p.FXRate, cur, base, p.FXRateSource)
		if err != nil {
			return Draft{}, bad(err.Error())
		}
		d.Rate = &r
	}
	for _, x := range p.Bills {
		d.Bills = append(d.Bills, Part{BillID: x.BillID, Amount: x.Amount, InFull: x.InFull})
	}
	return d, nil
}

// Total is what the payment paid in the bills' currency: every part and the unapplied amount.
func (d Draft) Total() (money.Money, error) {
	out := d.Unapplied
	if out.Currency() != d.Currency {
		out = money.Zero(d.Currency)
	}
	for _, x := range d.Bills {
		var err error
		if out, err = out.Add(x.Amount); err != nil {
			return money.Money{}, err
		}
	}
	return out, nil
}

// Open is a posted bill as a payment finds it: its number, its total and its rate as its entry credited accounts
// payable, and what is open of it before the payment, in its currency and as a carrying amount in the base currency
// (its credit to 2010 less every allocation's base amount).
type Open struct {
	Bill     storage.Document
	Number   string
	Total    money.Money
	Rate     money.Rate
	Open     money.Money
	Carrying money.Money
	PostedAt time.Time // when its entry was recorded
}

// OpenBill is what is open of a posted bill: CodeBill when the document is not a posted bill. s finds its accounts
// payable line.
func OpenBill(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, s ledger.Settings) (Open, error) {
	doc, err := u.Documents().Get(ctx, id)
	if errors.Is(err, ports.ErrNotFound) {
		return Open{}, &Error{CodeBill, fmt.Sprintf("bill %s does not exist in this organisation", id), "Choose one of the supplier's posted bills"}
	}
	if err != nil {
		return Open{}, err
	}
	if doc.Type != storage.DocumentBill || doc.Status != storage.DocumentPosted {
		return Open{}, &Error{CodeBill, fmt.Sprintf("%s is not a posted bill", cmp(doc.Number, doc.ID.String())), "Choose one of the supplier's posted bills"}
	}
	e, err := u.Ledger().ByIdentity(ctx, ledger.BillIdentity(doc.ID, doc.Version, ledger.RuleBill))
	if err != nil {
		return Open{}, err
	}
	out := Open{Bill: doc, Number: doc.Number, PostedAt: e.RecordedAt}
	found := false
	for _, l := range e.Lines {
		if s.Accounts[l.AccountID].Role != ledger.RoleAccountsPayable {
			continue
		}
		cur, err := money.Lookup(l.Currency)
		if err != nil {
			return Open{}, err
		}
		amount, err := money.ParseStored(string(l.Amount), cur)
		if err != nil {
			return Open{}, err
		}
		base, err := money.ParseStored(string(l.BaseAmount), s.Base)
		if err != nil {
			return Open{}, err
		}
		if out.Rate, err = money.ParseRate(string(l.FXRate), cur, s.Base, l.FXRateSource); err != nil {
			return Open{}, err
		}
		out.Total, out.Carrying, found = amount.Neg(), base.Neg(), true
		break
	}
	if !found {
		return Open{}, &Error{CodeBill, fmt.Sprintf("%s credits no accounts payable account", cmp(doc.Number, doc.ID.String())), "Check the chart's accounts payable account"}
	}
	out.Open = out.Total
	allocs, err := u.Allocations().OfDocument(ctx, id)
	if err != nil {
		return Open{}, err
	}
	for _, a := range allocs {
		amount, err := money.ParseStored(string(a.Amount), out.Total.Currency())
		if err != nil {
			return Open{}, err
		}
		base, err := money.ParseStored(string(a.BaseAmount), s.Base)
		if err != nil {
			return Open{}, err
		}
		if out.Open, err = out.Open.Sub(amount); err != nil {
			return Open{}, err
		}
		if out.Carrying, err = out.Carrying.Sub(base); err != nil {
			return Open{}, err
		}
	}
	return out, nil
}

func cmp(s, otherwise string) string {
	if s == "" {
		return otherwise
	}
	return s
}

// Prepared is a payment version as the rule reads it, with what it shows beside its entry: its supplier, and each bill
// it settles as it stood before the payment.
type Prepared struct {
	Draft    Draft
	Payment  ledger.BillPayment
	Supplier storage.Contact
	Bills    []Open
}

// Prepare reads one version of a payment for the rule: its supplier, its rate (the typed one, else the cascade's for its
// date when the money account is in the bills' currency), and each bill it settles. A bill that is not a posted bill of
// the supplier in the payment's currency, a bill named twice, and a part above what the bill has open are refused
// (over-allocation, ✖); the part that settles all that is open of a bill clears its carrying amount (ledger.PaidBill).
// Problems the rule would refuse come back as ledger.Problems, with the engine's own, from PostBillPayment. Lock the
// bills first when the payment is to be written, so no other payment settles them meanwhile.
func Prepare(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings) (Prepared, ledger.Problems, error) {
	d, err := Read(doc, s.Base)
	if err != nil {
		return Prepared{}, nil, err
	}
	out := Prepared{Draft: d}
	var probs ledger.Problems
	add := func(code, field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: code, Field: field, Message: msg, Fix: fix})
	}

	if d.SupplierID == uuid.Nil {
		add(CodeSupplier, "supplier_id", "the payment has no supplier", "Choose the supplier who was paid")
	} else {
		c, err := u.Contacts().Get(ctx, d.SupplierID)
		switch {
		case errors.Is(err, ports.ErrNotFound):
			add(CodeSupplier, "supplier_id", "the supplier does not exist in this organisation", "Choose a supplier from your contacts")
		case err != nil:
			return Prepared{}, nil, err
		case !c.Vendor:
			add(CodeSupplier, "supplier_id", fmt.Sprintf("%s is not a supplier", c.LegalName), "Mark the contact as a supplier, or choose another")
		default:
			out.Supplier = c
		}
	}

	p := ledger.BillPayment{ID: doc.ID, Version: doc.Version, Date: d.Date, Memo: memo(out.Supplier, d), Account: d.AccountID, Currency: d.Currency,
		Rate: d.Rate, Paid: d.Paid, Unapplied: d.Unapplied}
	acc, known := s.Accounts[d.AccountID]
	inCurrency := known && acc.Currency == d.Currency.Code
	if d.Currency != s.Base && d.Rate == nil && inCurrency && !d.Date.IsZero() {
		f, err := fx.Lookup(ctx, u, s.Base, d.Currency, d.Date)
		var fe *fx.Error
		switch {
		case errors.As(err, &fe):
			add(fe.Code, "fx_rate", fe.Message, fe.Fix)
		case err != nil:
			return Prepared{}, nil, err
		default:
			r := f.Rate
			p.Rate = &r
		}
	}

	seen := map[uuid.UUID]bool{}
	var tol *subledger.Tolerance // read once, when a part settles in full with a difference
	for i, x := range d.Bills {
		field := fmt.Sprintf("bills[%d].bill_id", i+1)
		if seen[x.BillID] {
			add(CodeBill, field, "the payment names the bill twice", "Settle each bill once, with the whole amount paid of it")
			continue
		}
		seen[x.BillID] = true
		o, err := OpenBill(ctx, u, x.BillID, s)
		var be *Error
		if errors.As(err, &be) {
			add(be.Code, field, be.Message, be.Fix)
			continue
		}
		if err != nil {
			return Prepared{}, nil, err
		}
		b, err := bill.Read(o.Bill, s.Base)
		if err != nil {
			return Prepared{}, nil, err
		}
		name := cmp(o.Number, "the bill")
		switch {
		case b.SupplierID != d.SupplierID:
			add(CodeBill, field, fmt.Sprintf("%s is another supplier's", name), "Pay each supplier's bills separately")
			continue
		case o.Total.Currency() != d.Currency:
			add(CodeBill, field, fmt.Sprintf("%s is in %s; the payment is in %s", name, o.Total.Currency(), d.Currency), "Pay bills of one currency together")
			continue
		}
		held, err := bill.HeldPayment(ctx, u, o.Bill, s.Base, o.PostedAt)
		if err != nil {
			return Prepared{}, nil, err
		}
		if held != "" {
			add(CodeHeld, field, fmt.Sprintf("%s waits for withholding tax: %s", name, held),
				"Record the tax agent's decision for the supplier and the nature of the payment, then edit the bill so it posts with it")
			continue
		}
		out.Bills = append(out.Bills, o)
		part := ledger.PaidBill{BillID: x.BillID, Amount: x.Amount, StoredRate: o.Rate}
		if x.Amount.Currency() == d.Currency {
			c, err := x.Amount.Cmp(o.Open)
			if err != nil {
				return Prepared{}, nil, err
			}
			switch {
			case c > 0:
				add(CodeOverpaid, fmt.Sprintf("bills[%d].amount", i+1), fmt.Sprintf("%s has %s %s open; the payment settles %s of it", name, o.Open, d.Currency, x.Amount),
					"Settle at most what is open, and keep the rest as a credit with the supplier")
			case c == 0:
				left := o.Carrying
				part.Clears = &left
			case x.InFull: // a settlement difference within tolerance clears the rest (FR-BNK-08)
				short, diff, total, err := subledger.Difference(x.Amount, o.Open, o.Total, o.Rate)
				if err != nil {
					return Prepared{}, nil, err
				}
				if tol == nil {
					t, err := subledger.LoadTolerance(ctx, u, s.Base, d.Date)
					if err != nil {
						return Prepared{}, nil, err
					}
					tol = &t
				}
				ok, why, err := tol.Allows(diff, total, d.Currency != s.Base)
				if err != nil {
					return Prepared{}, nil, err
				}
				if !ok {
					add(CodeTolerance, fmt.Sprintf("bills[%d].in_full", i+1), fmt.Sprintf("%s has %s %s open after this payment: %s", name, short, d.Currency, why),
						"Leave it open, or record a supplier credit note for what is left")
					break
				}
				if err := tol.Take(diff); err != nil {
					return Prepared{}, nil, err
				}
				left := o.Carrying
				part.Clears, part.Short = &left, &short
			}
		}
		p.Bills = append(p.Bills, part)
	}
	out.Payment = p
	return out, probs, nil
}

// memo names a payment in its entry: the supplier and the bank's reference.
func memo(c storage.Contact, d Draft) string {
	switch {
	case c.LegalName != "" && d.Reference != "":
		return c.LegalName + ", " + d.Reference
	case c.LegalName != "":
		return c.LegalName
	}
	return d.Reference
}
