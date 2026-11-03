package director

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable codes of a director's fee (FR-EQ-04; design D8).
const (
	CodeNotFee      = "FOUNDER-15" // the document is not a director's fee
	CodeFee         = "FOUNDER-16" // a fee that does not fit: its person, dates, amount or memo
	CodeNoVote      = "FOUNDER-17" // a fee sent for approval with no shareholders' resolution attached
	CodeFeePosted   = "FOUNDER-18" // a fee already posted, or waiting for its approval, which changes no more
	CodeOwnFee      = "FOUNDER-19" // an approval by the director the fee is paid to
	FeesAccountCode = "6050"       // the standard chart's Directors' fees, the account a fee is a cost on by default
)

// MaxFeeMemo is the longest memo a fee keeps.
const MaxFeeMemo = 500

// Fee is a director's fee the shareholders voted (FR-EQ-04, §10.2): the director, the day they became entitled to it
// (the general meeting that voted it), the period it covers, the amount in the base currency, the account it is a cost
// on (6050 Directors' fees by default) and a memo. It needs the shareholders' resolution as its evidence, and posts once
// another person approves it (director_fee.post), never the director it is paid to.
type Fee struct {
	ContactID  uuid.UUID
	EntitledOn time.Time
	PeriodFrom time.Time
	PeriodTo   time.Time
	Amount     money.Money
	AccountID  uuid.UUID
	Memo       string
}

type feePayload struct {
	Date       string       `json:"date"` // the day the director became entitled to it: what lists sort by
	ContactID  uuid.UUID    `json:"contact_id"`
	PeriodFrom string       `json:"period_from"`
	PeriodTo   string       `json:"period_to"`
	Amount     *money.Money `json:"amount"`
	AccountID  uuid.UUID    `json:"account_id"`
	Memo       string       `json:"memo,omitempty"`
}

// EncodeFee is a fee as a document version stores it.
func EncodeFee(f Fee) ([]byte, error) {
	amount := f.Amount
	return json.Marshal(feePayload{Date: dayOf(f.EntitledOn), ContactID: f.ContactID, PeriodFrom: dayOf(f.PeriodFrom), PeriodTo: dayOf(f.PeriodTo),
		Amount: &amount, AccountID: f.AccountID, Memo: f.Memo})
}

// IsFee says whether a document is a director's fee.
func IsFee(doc storage.Document) bool { return doc.Type == storage.DocumentDirectorFee }

// ReadFee reads a fee's version as it was saved.
func ReadFee(doc storage.Document) (Fee, error) {
	if !IsFee(doc) {
		return Fee{}, &Error{CodeNotFee, fmt.Sprintf("document %s is a %s, not a director's fee", doc.ID, doc.Type), "Open it with its own module"}
	}
	bad := func(what string) error {
		return &Error{CodeFee, "the saved fee cannot be read: " + what, "Record it again"}
	}
	var p feePayload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return Fee{}, bad(err.Error())
	}
	if p.Amount == nil {
		return Fee{}, bad("it has no amount")
	}
	f := Fee{ContactID: p.ContactID, Amount: *p.Amount, AccountID: p.AccountID, Memo: p.Memo}
	for _, d := range []struct {
		text string
		into *time.Time
	}{{p.Date, &f.EntitledOn}, {p.PeriodFrom, &f.PeriodFrom}, {p.PeriodTo, &f.PeriodTo}} {
		if d.text == "" {
			continue
		}
		t, err := time.Parse(time.DateOnly, d.text)
		if err != nil {
			return Fee{}, bad(fmt.Sprintf("the date %q", d.text))
		}
		*d.into = t
	}
	return f, nil
}

// CheckFee is what is wrong with a fee as recorded, every problem at once (ENG-22): a person who is not a director, no
// entitlement day, a period that ends before it starts, an amount not more than zero in the base currency, or a memo too
// long. The rule checks the account when it posts.
func CheckFee(ctx context.Context, u storage.UnitOfWork, f Fee, base money.Currency) (ledger.Problems, error) {
	var probs ledger.Problems
	add := func(code, field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: code, Field: field, Message: msg, Fix: fix})
	}
	ct, err := u.Contacts().Get(ctx, f.ContactID)
	switch {
	case errors.Is(err, ports.ErrNotFound):
		add(CodeNotPerson, "contact_id", "the contact does not exist in this organisation", "Choose the director the fee is voted to")
	case err != nil:
		return nil, err
	case !ct.Director:
		add(CodeNotPerson, "contact_id", ct.LegalName+" is not a director", "Mark the contact as a director in Contacts, or choose another")
	}
	if f.EntitledOn.IsZero() {
		add(CodeFee, "entitled_on", "the fee has no day the director became entitled to it", "Enter the day of the general meeting that voted it")
	}
	switch {
	case f.PeriodFrom.IsZero() || f.PeriodTo.IsZero():
		add(CodeFee, "period", "the fee covers no period", "Enter the first and last day of the period it is for")
	case f.PeriodTo.Before(f.PeriodFrom):
		add(CodeFee, "period", fmt.Sprintf("the period ends on %s, before it starts on %s", dayOf(f.PeriodTo), dayOf(f.PeriodFrom)), "Put the days in order")
	}
	if f.Amount.Currency() != base || f.Amount.Sign() <= 0 {
		add(CodeFee, "amount", fmt.Sprintf("the amount is %s %s", f.Amount, f.Amount.Currency()), fmt.Sprintf("Enter the fee in %s, more than zero", base))
	}
	if f.AccountID == uuid.Nil {
		add(CodeFee, "account_id", "the fee names no account", "Choose 6050 Directors' fees, or another cost account")
	}
	if n := utf8.RuneCountInString(f.Memo); n > MaxFeeMemo {
		add(CodeFee, "memo", fmt.Sprintf("the memo has %d characters; the most is %d", n, MaxFeeMemo), "Shorten it")
	}
	return probs, nil
}

// Rule is an approved fee as the rule reads it.
func (f Fee) Rule(doc storage.Document) ledger.DirectorFee {
	return ledger.DirectorFee{ID: doc.ID, Version: doc.Version, EntitledOn: f.EntitledOn, Memo: strings.TrimSpace(doc.Number + " " + f.Memo),
		Party: f.ContactID, Account: f.AccountID, Amount: f.Amount}
}
