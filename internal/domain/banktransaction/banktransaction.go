// Package banktransaction is a bank line categorised (FR-BNK-06, FR-BNK-11; p1-bank-import-reconcile task 4.1, design
// D4): the document bank_transaction that records what a line was for, its parts' accounts, GST codes and spreads, and
// posts by the rule bank_transaction@1 (Appendix B.1). Each version keeps what the line said (its date, amount and
// money account) and what posting decided (the GST rate of each part and the exchange rate), so a replay reproduces
// its entry whatever is recorded later. The posting service in package journal writes it (the one posting path).
package banktransaction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/gst"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable codes of a bank transaction's refusals (ENG-23).
const (
	CodeNotTransaction = "BANKTXN-01" // the document is not a bank transaction
	CodeBadDraft       = "BANKTXN-02" // the saved transaction cannot be read
	CodeLine           = "BANKTXN-03" // the bank line does not exist, or is not in a money account here
	CodeGSTCode        = "BANKTXN-04" // a GST code the sg-gst version on the line's date does not have, or none at all
	CodeContact        = "BANKTXN-05" // the contact does not exist in this organisation
	CodeTooLong        = "BANKTXN-06" // the reference is too long
	CodeRest           = "BANKTXN-07" // more than one part takes the rest of the line
)

// MaxReference is the longest reference a bank transaction keeps.
const MaxReference = 140

// Error is a bank transaction's refusal with a stable code.
type Error struct {
	Code    string
	Message string
	Fix     string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Fix) }

// Part is one part of a line as a person or a rule codes it: its account, its signed amount in the line's currency
// (nil: the rest of the line, for at most one part, so a whole line is one part with none), its GST code ("" for the
// account's default), the months it spreads over (0 for none) and its description.
type Part struct {
	AccountID   uuid.UUID
	Amount      *money.Money
	GSTCode     string
	Months      int
	Description string
}

// Draft is a categorisation: the line, the contact and the reference it names, and its parts.
type Draft struct {
	LineID    uuid.UUID
	ContactID *uuid.UUID
	Reference string
	Parts     []Part
}

// SavedPart is a part as a version keeps it: its amount and its GST code as resolved, and the GST rate it posted with.
type SavedPart struct {
	AccountID   uuid.UUID
	Amount      money.Money
	GSTCode     string
	GSTRate     string
	Months      int
	Description string
}

// Saved is a version as it is stored: the line, its money account and the chart account that holds it, the line's
// booking date and signed amount, the rate into the base currency for a line in another currency, the contact,
// reference and memo, the sg-gst version its codes are read from, and its parts.
type Saved struct {
	LineID         uuid.UUID
	MoneyAccountID uuid.UUID
	AccountID      uuid.UUID
	Date           time.Time
	Amount         money.Money
	Rate           *money.Rate
	ContactID      *uuid.UUID
	Reference      string
	Memo           string
	GSTVersion     string
	Parts          []SavedPart
}

type partPayload struct {
	AccountID    uuid.UUID   `json:"account_id"`
	Amount       money.Money `json:"amount"`
	GSTCode      string      `json:"gst_code"`
	GSTRate      string      `json:"gst_rate,omitempty"`
	SpreadMonths int         `json:"spread_months,omitempty"`
	Description  string      `json:"description,omitempty"`
}

type payload struct {
	LineID         uuid.UUID     `json:"bank_line_id"`
	MoneyAccountID uuid.UUID     `json:"money_account_id"`
	AccountID      uuid.UUID     `json:"account_id"`
	Date           string        `json:"date"`
	Amount         money.Money   `json:"amount"`
	FXRate         string        `json:"fx_rate,omitempty"`
	FXRateSource   string        `json:"fx_rate_source,omitempty"`
	ContactID      *uuid.UUID    `json:"contact_id,omitempty"`
	Reference      string        `json:"reference,omitempty"`
	Memo           string        `json:"memo"`
	GSTVersion     string        `json:"gst_version"`
	Lines          []partPayload `json:"lines"`
}

// Encode is a saved version as a document version stores it; the spread query reads its date and its lines' months.
func Encode(s Saved) ([]byte, error) {
	p := payload{LineID: s.LineID, MoneyAccountID: s.MoneyAccountID, AccountID: s.AccountID, Date: s.Date.Format(time.DateOnly), Amount: s.Amount,
		ContactID: s.ContactID, Reference: s.Reference, Memo: s.Memo, GSTVersion: s.GSTVersion, Lines: []partPayload{}}
	if s.Rate != nil {
		p.FXRate, p.FXRateSource = s.Rate.String(), s.Rate.Source
	}
	for _, x := range s.Parts {
		p.Lines = append(p.Lines, partPayload{AccountID: x.AccountID, Amount: x.Amount, GSTCode: x.GSTCode, GSTRate: x.GSTRate, SpreadMonths: x.Months,
			Description: x.Description})
	}
	return json.Marshal(p)
}

// IsTransaction says whether a document is a bank transaction.
func IsTransaction(doc storage.Document) bool { return doc.Type == storage.DocumentBankTransaction }

// Read reads a bank transaction version as it was saved; base is the base currency its rate converts into.
func Read(doc storage.Document, base money.Currency) (Saved, error) {
	if !IsTransaction(doc) {
		return Saved{}, &Error{CodeNotTransaction, fmt.Sprintf("document %s is a %s, not a bank transaction", doc.ID, doc.Type), "Open it with its own module"}
	}
	bad := func(what string) error {
		return &Error{CodeBadDraft, "the saved bank transaction cannot be read: " + what, "Undo the categorisation and categorise the line again"}
	}
	var p payload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return Saved{}, bad(err.Error())
	}
	on, err := time.Parse(time.DateOnly, p.Date)
	if err != nil {
		return Saved{}, bad(fmt.Sprintf("the date %q", p.Date))
	}
	s := Saved{LineID: p.LineID, MoneyAccountID: p.MoneyAccountID, AccountID: p.AccountID, Date: on, Amount: p.Amount, ContactID: p.ContactID,
		Reference: p.Reference, Memo: p.Memo, GSTVersion: p.GSTVersion}
	if p.FXRate != "" {
		r, err := money.ParseRate(p.FXRate, p.Amount.Currency(), base, p.FXRateSource)
		if err != nil {
			return Saved{}, bad(err.Error())
		}
		s.Rate = &r
	}
	for _, x := range p.Lines {
		s.Parts = append(s.Parts, SavedPart{AccountID: x.AccountID, Amount: x.Amount, GSTCode: x.GSTCode, GSTRate: x.GSTRate, Months: x.SpreadMonths,
			Description: x.Description})
	}
	return s, nil
}

// Transaction is a saved version as the rule reads it.
func Transaction(doc storage.Document, s Saved) ledger.BankTransaction {
	t := ledger.BankTransaction{ID: doc.ID, Version: doc.Version, Date: s.Date, Memo: s.Memo, Account: s.AccountID, Amount: s.Amount, Rate: s.Rate}
	for _, p := range s.Parts {
		t.Parts = append(t.Parts, ledger.BankPart{AccountID: p.AccountID, Amount: p.Amount, GSTRate: p.GSTRate, Months: p.Months, Description: p.Description})
	}
	return t
}

// Prepare decides what a categorisation of a line posts, against the line as the bank gave it: its money account and
// the chart account that holds it, its booking date (the tax point) and its signed amount, each part's amount (the
// part with none takes the rest of the line), each part's GST code (the account's default when none is given) and the
// rate it bears (the sg-gst version in force on the line's date: the standard rate for a code that carries GST, 0 for
// a zero-rated or exempt code, and none before the organisation is GST-registered on that date, FR-ORG-03), and, for a
// line in another currency, the cascade's rate for its date (ENG-40). Problems the rule would refuse come back as
// ledger.Problems, with the engine's own from PostBankTransaction; a missing rate is FX-01.
func Prepare(ctx context.Context, u storage.UnitOfWork, d Draft, s ledger.Settings) (Saved, ledger.Problems, error) {
	var probs ledger.Problems
	add := func(code, field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: code, Field: field, Message: msg, Fix: fix})
	}
	line, err := u.Banking().Line(ctx, d.LineID)
	if errors.Is(err, ports.ErrNotFound) {
		return Saved{}, nil, &Error{CodeLine, "the bank line does not exist in this organisation", "Choose a line from the Categorise queue"}
	}
	if err != nil {
		return Saved{}, nil, err
	}
	acct, err := u.Banking().MoneyAccount(ctx, line.MoneyAccountID)
	if err != nil {
		return Saved{}, nil, err
	}
	cur, err := money.Lookup(line.Currency)
	if err != nil {
		return Saved{}, nil, err
	}
	amount, err := money.ParseStored(string(line.Amount), cur)
	if err != nil {
		return Saved{}, nil, err
	}
	out := Saved{LineID: line.ID, MoneyAccountID: acct.ID, AccountID: acct.AccountID, Date: line.BookingDate, Amount: amount, ContactID: d.ContactID,
		Reference: d.Reference, Memo: memo(acct, line)}
	if len(d.Reference) > MaxReference {
		return Saved{}, nil, &Error{CodeTooLong, fmt.Sprintf("the reference has %d characters; the most is %d", len(d.Reference), MaxReference), "Shorten it"}
	}
	if d.ContactID != nil {
		if _, err := u.Contacts().Get(ctx, *d.ContactID); errors.Is(err, ports.ErrNotFound) {
			add(CodeContact, "contact_id", "the contact does not exist in this organisation", "Choose a contact, or leave it out")
		} else if err != nil {
			return Saved{}, nil, err
		}
	}

	// The parts' amounts: the one without takes what the others leave of the line.
	rest := amount
	open := -1
	for i, p := range d.Parts {
		if p.Amount == nil {
			if open >= 0 {
				add(CodeRest, fmt.Sprintf("parts[%d].amount", i+1), "only one part takes the rest of the line", "Enter an amount for every part but one")
			}
			open = i
			continue
		}
		if p.Amount.Currency() == cur {
			rest, _ = rest.Sub(*p.Amount)
		}
	}

	version, err := gst.Version(ctx, u, line.BookingDate)
	if err != nil {
		return Saved{}, nil, err
	}
	out.GSTVersion = version.ID
	regs, err := u.GSTRegistrations().List(ctx)
	if err != nil {
		return Saved{}, nil, err
	}
	_, registered := gst.StatusOn(regs, line.BookingDate)
	standard, _ := pack.RateOn(version, line.BookingDate)
	for i, p := range d.Parts {
		x := SavedPart{AccountID: p.AccountID, GSTCode: p.GSTCode, Months: p.Months, Description: p.Description}
		if p.Amount != nil {
			x.Amount = *p.Amount
		} else {
			x.Amount = rest
		}
		if x.GSTCode == "" {
			if a, err := u.Accounts().Get(ctx, p.AccountID); err == nil {
				x.GSTCode = a.DefaultGSTCode
			} else if !errors.Is(err, ports.ErrNotFound) {
				return Saved{}, nil, err
			}
		}
		field := fmt.Sprintf("parts[%d].gst_code", i+1)
		code, ok := pack.Code(version, x.GSTCode)
		switch {
		case x.GSTCode == "":
			add(CodeGSTCode, field, "the part has no GST code, and its account has no default", "Choose the GST code, such as SR, TX or NA")
		case !ok:
			add(CodeGSTCode, field, fmt.Sprintf("%q is not a GST code of %s", x.GSTCode, version.ID), "Choose a GST code from the list, such as SR, TX or NA")
		case registered && code.StandardRated:
			x.GSTRate = standard
		case registered && code.ValueBox != 0: // zero-rated and exempt supplies and purchases are reported at no GST
			x.GSTRate = "0"
		}
		out.Parts = append(out.Parts, x)
	}

	acc, known := s.Accounts[acct.AccountID]
	if cur != s.Base && known && acc.Currency == cur.Code {
		f, err := fx.Lookup(ctx, u, s.Base, cur, line.BookingDate)
		var fe *fx.Error
		switch {
		case errors.As(err, &fe):
			add(fe.Code, "fx_rate", fe.Message, fe.Fix)
		case err != nil:
			return Saved{}, nil, err
		default:
			rate := f.Rate
			out.Rate = &rate
		}
	}
	return out, probs, nil
}

// memo names a line in its entry: the money account, and the counterparty and description the bank gave, cut to the
// longest memo an entry keeps.
func memo(a storage.MoneyAccount, l storage.BankLine) string {
	m := a.Name
	if what := strings.TrimSpace(l.Counterparty + " " + l.Description); what != "" {
		m += ": " + what
	}
	for len(m) > ledger.MaxMemo {
		_, n := utf8.DecodeLastRuneInString(m)
		m = m[:len(m)-n]
	}
	return m
}
