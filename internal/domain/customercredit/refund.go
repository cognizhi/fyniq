package customercredit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// CodeNotRefund refuses a document that is not a customer refund.
const CodeNotRefund = "APPLY-10"

// MaxReference is the longest bank reference a refund keeps.
const MaxReference = 140

// RefundDraft is money paid back to a customer against a credit, as recorded (FR-AR-04, FR-AR-18): the credit, the date
// the money left, the money account it left, the rate as typed (nil: the cascade's for the date), what a base-currency
// account paid for a credit in another currency, the bank's reference and the amount, in the credit's currency.
type RefundDraft struct {
	CreditID  uuid.UUID
	Date      time.Time
	AccountID uuid.UUID
	Rate      *money.Rate
	Paid      *money.Money
	Reference string
	Amount    money.Money
}

type refundPayload struct {
	CreditID     uuid.UUID    `json:"credit_id"`
	Date         string       `json:"date"`
	AccountID    uuid.UUID    `json:"money_account_id"`
	FXRate       string       `json:"fx_rate,omitempty"`
	FXRateSource string       `json:"fx_rate_source,omitempty"`
	Paid         *money.Money `json:"paid,omitempty"`
	Reference    string       `json:"reference,omitempty"`
	Amount       money.Money  `json:"amount"`
}

// EncodeRefund is a refund as a document version stores it.
func EncodeRefund(d RefundDraft) ([]byte, error) {
	if len(d.Reference) > MaxReference {
		return nil, &Error{CodeBadDraft, fmt.Sprintf("the reference has %d characters; the most is %d", len(d.Reference), MaxReference), "Shorten it"}
	}
	p := refundPayload{CreditID: d.CreditID, AccountID: d.AccountID, Paid: d.Paid, Reference: d.Reference, Amount: d.Amount}
	if !d.Date.IsZero() {
		p.Date = d.Date.Format(time.DateOnly)
	}
	if d.Rate != nil {
		p.FXRate, p.FXRateSource = d.Rate.String(), d.Rate.Source
	}
	return json.Marshal(p)
}

// ReadRefund reads a refund version as it was saved; base is the base currency a typed rate converts into.
func ReadRefund(doc storage.Document, base money.Currency) (RefundDraft, error) {
	if doc.Type != storage.DocumentCustomerRefund {
		return RefundDraft{}, &Error{CodeNotRefund, fmt.Sprintf("document %s is a %s, not a customer refund", doc.ID, doc.Type), "Open it with its own module"}
	}
	bad := func(what string) error {
		return &Error{CodeBadDraft, "the saved refund cannot be read: " + what, "Record the refund again"}
	}
	var p refundPayload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return RefundDraft{}, bad(err.Error())
	}
	d := RefundDraft{CreditID: p.CreditID, AccountID: p.AccountID, Paid: p.Paid, Reference: p.Reference, Amount: p.Amount}
	var err error
	if p.Date != "" {
		if d.Date, err = time.Parse(time.DateOnly, p.Date); err != nil {
			return RefundDraft{}, bad(fmt.Sprintf("the date %q", p.Date))
		}
	}
	if p.FXRate != "" {
		r, err := money.ParseRate(p.FXRate, p.Amount.Currency(), base, p.FXRateSource)
		if err != nil {
			return RefundDraft{}, bad(err.Error())
		}
		d.Rate = &r
	}
	return d, nil
}

// PreparedRefund is a refund version as the rule reads it, with the credit it settles and its customer.
type PreparedRefund struct {
	Draft    RefundDraft
	Refund   ledger.CustomerRefund
	Credit   Credit
	Customer storage.Contact
}

// PrepareRefund reads one version of a refund for the rule: the credit as it stands (an issued credit note, or a
// receipt's unapplied amount), the rate (the typed one, else the cascade's for the date when the money account is in
// the credit's currency), at most what is left of the credit (APPLY-05), and the credit's carrying amount left when it
// takes all of it. Lock the credit first when the refund is to be written.
func PrepareRefund(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings) (PreparedRefund, ledger.Problems, error) {
	d, err := ReadRefund(doc, s.Base)
	if err != nil {
		return PreparedRefund{}, nil, err
	}
	out := PreparedRefund{Draft: d}
	var probs ledger.Problems
	add := func(code, field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: code, Field: field, Message: msg, Fix: fix})
	}
	c, err := CreditOf(ctx, u, d.CreditID, s)
	var ce *Error
	if errors.As(err, &ce) {
		add(ce.Code, "credit_id", ce.Message, ce.Fix)
		return out, probs, nil
	}
	if err != nil {
		return PreparedRefund{}, nil, err
	}
	out.Credit = c
	if cust, err := u.Contacts().Get(ctx, c.Customer); err == nil {
		out.Customer = cust
	} else if !errors.Is(err, ports.ErrNotFound) {
		return PreparedRefund{}, nil, err
	}
	cur := c.Left.Currency()
	memo := fmt.Sprintf("Refund of %s", Label(c.Doc))
	if out.Customer.LegalName != "" {
		memo = out.Customer.LegalName + ", " + memo
	}
	if d.Reference != "" {
		memo += ", " + d.Reference
	}
	if len(memo) > ledger.MaxMemo {
		memo = memo[:ledger.MaxMemo]
	}
	r := ledger.CustomerRefund{ID: doc.ID, Version: doc.Version, Date: d.Date, Memo: memo, Account: d.AccountID, Currency: cur, Rate: d.Rate, Paid: d.Paid,
		Credit: ledger.PaidBill{BillID: c.Doc.ID, Amount: d.Amount, StoredRate: c.Rate}}
	acc, known := s.Accounts[d.AccountID]
	if cur != s.Base && d.Rate == nil && known && acc.Currency == cur.Code && !d.Date.IsZero() {
		f, err := fx.Lookup(ctx, u, s.Base, cur, d.Date)
		var fe *fx.Error
		switch {
		case errors.As(err, &fe):
			add(fe.Code, "fx_rate", fe.Message, fe.Fix)
		case err != nil:
			return PreparedRefund{}, nil, err
		default:
			rate := f.Rate
			r.Rate = &rate
		}
	}
	if d.Amount.Currency() == cur {
		cmp, err := d.Amount.Cmp(c.Left)
		if err != nil {
			return PreparedRefund{}, nil, err
		}
		switch {
		case cmp > 0:
			add(CodeTooMuch, "amount", fmt.Sprintf("%s has %s %s left; the refund is %s", Label(c.Doc), c.Left, cur, d.Amount),
				"Record at most what is left of the credit")
		case cmp == 0:
			left := c.Carrying
			r.Credit.Clears = &left
		}
	}
	out.Refund = r
	return out, probs, nil
}
