// Package writeoff is the write-off of what is left of an invoice (FR-AR-17; FR-BNK-08's reason-coded adjustment;
// p1-sales task 2.3): its content as each version saves it, and what the posting rule needs from it. A write-off names
// one invoice, the amount written off in its currency (at most what is open), one reason, the account the reason
// writes off to, and why. It posts only on another person's approval (FR-APR-01; SOD-9), through the posting service
// in package journal.
package writeoff

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/receipt"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable codes of a write-off's refusals (ENG-23).
const (
	CodeNotWriteOff = "WRITEOFF-01" // the document is not a write-off
	CodeBadDraft    = "WRITEOFF-02" // the saved write-off cannot be read
	CodeInvoice     = "WRITEOFF-03" // no invoice, or one that is not issued
	CodeTooMuch     = "WRITEOFF-04" // more than the invoice has open
	CodeReason      = "WRITEOFF-05" // a reason that is not one of the five
	CodeAccount     = "WRITEOFF-06" // no account for the reason: a bad debt or a migrated residual names one
	CodeNotDraft    = "WRITEOFF-07" // only a draft changes or is asked to post
	CodeNotPosted   = "WRITEOFF-08" // only a posted write-off is voided
	CodeVoidDate    = "WRITEOFF-09" // a void dated before the write-off
	CodeTooLong     = "WRITEOFF-12" // the explanation is too long, or missing
)

// The reasons (FR-AR-17) and the account each writes off to when none is named (p1-sales design, provisional): an agreed
// discount to 4095 Discounts & refunds given; bank or provider charges and an unexplained cross-currency difference to
// 7030 FX & rounding; a bad debt and a migrated residual to the account named, until the chart-of-accounts review assigns
// one (Appendix B.5) [verify].
const (
	ReasonDiscount      = "discount"
	ReasonCharges       = "charges"
	ReasonBadDebt       = "bad_debt"
	ReasonCrossCurrency = "cross_currency"
	ReasonMigrated      = "migrated"
)

// Reasons are the reasons, in the order they are offered.
var Reasons = []string{ReasonDiscount, ReasonCharges, ReasonBadDebt, ReasonCrossCurrency, ReasonMigrated}

// reasonWords name each reason in a memo.
var reasonWords = map[string]string{ReasonDiscount: "agreed discount", ReasonCharges: "bank charges", ReasonBadDebt: "bad debt",
	ReasonCrossCurrency: "cross-currency difference", ReasonMigrated: "migrated residual"}

// DiscountAccount is the code of the standard chart's account a discount writes off to by default.
const DiscountAccount = "4095"

// MaxNote is the longest explanation a write-off keeps.
const MaxNote = 1000

// Error is a write-off command's refusal with a stable code.
type Error struct {
	Code    string
	Message string
	Fix     string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Fix) }

// Draft is a write-off as entered.
type Draft struct {
	InvoiceID uuid.UUID
	Date      time.Time
	Amount    money.Money
	Reason    string
	AccountID uuid.UUID // uuid.Nil takes the reason's account
	Note      string
}

type payload struct {
	InvoiceID uuid.UUID   `json:"invoice_id"`
	Date      string      `json:"date,omitempty"`
	Amount    money.Money `json:"amount"`
	Reason    string      `json:"reason"`
	AccountID *uuid.UUID  `json:"account_id,omitempty"`
	Note      string      `json:"note"`
}

// Encode is a draft as a document version stores it.
func Encode(d Draft) ([]byte, error) {
	note := strings.TrimSpace(d.Note)
	if note == "" || len(note) > MaxNote {
		return nil, &Error{CodeTooLong, fmt.Sprintf("the explanation has %d characters; it needs 1 to %d", len(note), MaxNote), "Say why it is written off"}
	}
	p := payload{InvoiceID: d.InvoiceID, Amount: d.Amount, Reason: d.Reason, Note: note}
	if !d.Date.IsZero() {
		p.Date = d.Date.Format(time.DateOnly)
	}
	if d.AccountID != uuid.Nil {
		id := d.AccountID
		p.AccountID = &id
	}
	return json.Marshal(p)
}

// IsWriteOff says whether a document is a write-off.
func IsWriteOff(doc storage.Document) bool { return doc.Type == storage.DocumentWriteOff }

// Read reads a write-off version as it was saved.
func Read(doc storage.Document) (Draft, error) {
	if !IsWriteOff(doc) {
		return Draft{}, &Error{CodeNotWriteOff, fmt.Sprintf("document %s is a %s, not a write-off", doc.ID, doc.Type), "Open it with its own module"}
	}
	var p payload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return Draft{}, &Error{CodeBadDraft, "the saved write-off cannot be read: " + err.Error(), "Save the write-off again"}
	}
	d := Draft{InvoiceID: p.InvoiceID, Amount: p.Amount, Reason: p.Reason, Note: p.Note}
	if p.AccountID != nil {
		d.AccountID = *p.AccountID
	}
	if p.Date != "" {
		t, err := time.Parse(time.DateOnly, p.Date)
		if err != nil {
			return Draft{}, &Error{CodeBadDraft, fmt.Sprintf("the saved write-off cannot be read: the date %q", p.Date), "Save the write-off again"}
		}
		d.Date = t
	}
	return d, nil
}

// Prepared is a write-off version as the rule reads it, with what it shows beside its entry: the invoice as it stood
// before the write-off, its customer, and the account the reason writes off to.
type Prepared struct {
	Draft    Draft
	WriteOff ledger.WriteOff
	Invoice  receipt.Open
	Customer storage.Contact
	Account  storage.Account
}

// Prepare reads one version of a write-off for the rule: the invoice (issued, with what is open of it and its stored
// rate), the reason and its account, and the amount against what is open: all of it clears the invoice's carrying
// amount. Problems come back as ledger.Problems, with the engine's own, from PostWriteOff. Lock the invoice first when
// the write-off is to be written, so no receipt settles it meanwhile.
func Prepare(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings) (Prepared, ledger.Problems, error) {
	d, err := Read(doc)
	if err != nil {
		return Prepared{}, nil, err
	}
	out := Prepared{Draft: d}
	var probs ledger.Problems
	add := func(code, field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: code, Field: field, Message: msg, Fix: fix})
	}
	w := ledger.WriteOff{ID: doc.ID, Version: doc.Version, Date: d.Date, InvoiceID: d.InvoiceID, Amount: d.Amount, StoredRate: money.Identity(s.Base),
		Account: d.AccountID}

	known := false
	for _, r := range Reasons {
		known = known || r == d.Reason
	}
	if !known {
		add(CodeReason, "reason", fmt.Sprintf("%q is not a reason for a write-off", d.Reason), "Choose discount, charges, bad_debt, cross_currency or migrated")
	}
	if d.AccountID == uuid.Nil && known {
		switch d.Reason {
		case ReasonDiscount:
			a, err := u.Accounts().ByCode(ctx, DiscountAccount)
			switch {
			case errors.Is(err, ports.ErrNotFound):
				add(CodeAccount, "account_id", "the chart has no account "+DiscountAccount+" for discounts", "Choose the account the discount writes off to")
			case err != nil:
				return Prepared{}, nil, err
			default:
				w.Account = a.ID
			}
		case ReasonCharges, ReasonCrossCurrency:
			for _, a := range s.Accounts {
				if a.Role == ledger.RoleFXRounding {
					w.Account = a.ID
				}
			}
			if w.Account == uuid.Nil {
				add(CodeAccount, "account_id", "the chart has no FX and rounding account", "Choose the account it writes off to")
			}
		default:
			add(CodeAccount, "account_id", fmt.Sprintf("a %s names the account it writes off to", reasonWords[d.Reason]),
				"Choose the account, such as a bad-debt expense account, with the accountant [verify]")
		}
	}
	if w.Account != uuid.Nil {
		if a, err := u.Accounts().Get(ctx, w.Account); err == nil {
			out.Account = a
		} else if !errors.Is(err, ports.ErrNotFound) {
			return Prepared{}, nil, err
		}
	}

	name := "the invoice"
	if d.InvoiceID == uuid.Nil {
		add(CodeInvoice, "invoice_id", "the write-off names no invoice", "Choose the issued invoice it writes off")
	} else {
		o, err := receipt.OpenInvoice(ctx, u, d.InvoiceID, s)
		var re *receipt.Error
		switch {
		case errors.As(err, &re):
			add(CodeInvoice, "invoice_id", re.Message, re.Fix)
		case err != nil:
			return Prepared{}, nil, err
		default:
			out.Invoice, w.StoredRate = o, o.Rate
			name = cmp(o.Number, name)
			if inv, err := invoice.Read(o.Invoice, s.Base); err == nil {
				if c, err := u.Contacts().Get(ctx, inv.CustomerID); err == nil {
					out.Customer = c
				}
			}
			if d.Amount.Currency() == o.Open.Currency() {
				c, err := d.Amount.Cmp(o.Open)
				if err != nil {
					return Prepared{}, nil, err
				}
				switch {
				case c > 0:
					add(CodeTooMuch, "amount", fmt.Sprintf("%s has %s %s open; the write-off takes %s", name, o.Open, o.Open.Currency(), d.Amount),
						"Write off at most what is open")
				case c == 0:
					left := o.Carrying
					w.Clears = &left
				}
			} else {
				add(CodeTooMuch, "amount", fmt.Sprintf("%s is in %s; the write-off is in %s", name, o.Open.Currency(), d.Amount.Currency()),
					"Write off in the invoice's currency")
			}
		}
	}
	w.Memo = fmt.Sprintf("Write-off of %s", name)
	if out.Customer.LegalName != "" {
		w.Memo += " to " + out.Customer.LegalName
	}
	if words, ok := reasonWords[d.Reason]; ok {
		w.Memo += ": " + words
	}
	if len(w.Memo) > ledger.MaxMemo {
		w.Memo = w.Memo[:ledger.MaxMemo]
	}
	out.WriteOff = w
	return out, probs, nil
}

func cmp(s, otherwise string) string {
	if s == "" {
		return otherwise
	}
	return s
}
