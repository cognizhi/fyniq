// Package deposit is money a customer pays ahead of an invoice (FR-AR-09; p1-sales task 3.2): a part payment, which
// bears GST when received once the organisation is GST-registered (ENG-45), or a security deposit, which bears none
// until it is used. Its content as each version saves it, what the posting rule needs from it, and what is left of it
// after the invoices that deduct it and the refunds of it. The posting service in package journal writes it (the one
// posting path).
package deposit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/gst"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable codes of a deposit's refusals (ENG-23).
const (
	CodeNotDeposit = "DEPOSIT-01" // the document is not a customer deposit
	CodeBadDraft   = "DEPOSIT-02" // the saved deposit cannot be read
	CodeCustomer   = "DEPOSIT-03" // no customer, or one that is not a customer
	CodeKind       = "DEPOSIT-04" // not a part payment or a security deposit
	CodeGSTCode    = "DEPOSIT-05" // a part payment's GST code that is not a supply code, or a security deposit with one
	CodeNotPosted  = "DEPOSIT-06" // only a posted deposit is voided
	CodeVoidDate   = "DEPOSIT-07" // a void dated before the deposit
	CodeUsed       = "DEPOSIT-08" // a deposit an invoice deducts or a refund settles is not voided until those are
	CodeTooLong    = "DEPOSIT-09" // the reference or the notes are too long
)

// The kinds of deposit (FR-AR-09).
const (
	KindPartPayment = "part_payment" // consideration paid ahead: GST is due on it when received, once registered
	KindSecurity    = "security"     // held as security: no GST until it is used as payment
)

// Kinds are the kinds, in the order they are offered.
var Kinds = []string{KindPartPayment, KindSecurity}

// The longest reference and notes a deposit keeps.
const (
	MaxReference = 140
	MaxNotes     = 2000
)

// Error is a deposit command's refusal with a stable code.
type Error struct {
	Code    string
	Message string
	Fix     string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Fix) }

// Draft is a deposit as recorded: the customer, its kind, the date it came in, the money account, its currency and
// rate as typed (nil: the cascade's for the date), what a base-currency account received for a deposit in another
// currency, its gross amount, a part payment's GST code, the bank's reference and notes.
type Draft struct {
	CustomerID uuid.UUID
	Kind       string
	Date       time.Time
	AccountID  uuid.UUID
	Currency   money.Currency
	Rate       *money.Rate
	Received   *money.Money
	Amount     money.Money
	GSTCode    string
	Reference  string
	Notes      string
}

type payload struct {
	CustomerID   uuid.UUID    `json:"customer_id"`
	Kind         string       `json:"kind"`
	Date         string       `json:"date,omitempty"`
	AccountID    uuid.UUID    `json:"money_account_id"`
	Currency     string       `json:"currency"`
	FXRate       string       `json:"fx_rate,omitempty"`
	FXRateSource string       `json:"fx_rate_source,omitempty"`
	Received     *money.Money `json:"received,omitempty"`
	Amount       money.Money  `json:"amount"`
	GSTCode      string       `json:"gst_code,omitempty"`
	Reference    string       `json:"reference,omitempty"`
	Notes        string       `json:"notes,omitempty"`
}

// Encode is a draft as a document version stores it.
func Encode(d Draft) ([]byte, error) {
	if len(d.Reference) > MaxReference {
		return nil, &Error{CodeTooLong, fmt.Sprintf("the reference has %d characters; the most is %d", len(d.Reference), MaxReference), "Shorten it"}
	}
	if len(d.Notes) > MaxNotes {
		return nil, &Error{CodeTooLong, fmt.Sprintf("the notes have %d characters; the most is %d", len(d.Notes), MaxNotes), "Shorten them"}
	}
	p := payload{CustomerID: d.CustomerID, Kind: d.Kind, AccountID: d.AccountID, Currency: d.Currency.Code, Received: d.Received, Amount: d.Amount,
		GSTCode: d.GSTCode, Reference: d.Reference, Notes: d.Notes}
	if !d.Date.IsZero() {
		p.Date = d.Date.Format(time.DateOnly)
	}
	if d.Rate != nil {
		p.FXRate, p.FXRateSource = d.Rate.String(), d.Rate.Source
	}
	return json.Marshal(p)
}

// IsDeposit says whether a document is a customer deposit.
func IsDeposit(doc storage.Document) bool { return doc.Type == storage.DocumentCustomerDeposit }

// Read reads a deposit version as it was saved; base is the base currency a typed rate converts into.
func Read(doc storage.Document, base money.Currency) (Draft, error) {
	if !IsDeposit(doc) {
		return Draft{}, &Error{CodeNotDeposit, fmt.Sprintf("document %s is a %s, not a customer deposit", doc.ID, doc.Type), "Open it with its own module"}
	}
	bad := func(what string) error {
		return &Error{CodeBadDraft, "the saved deposit cannot be read: " + what, "Record the deposit again"}
	}
	var p payload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return Draft{}, bad(err.Error())
	}
	cur, err := money.Lookup(p.Currency)
	if err != nil {
		return Draft{}, bad(err.Error())
	}
	d := Draft{CustomerID: p.CustomerID, Kind: p.Kind, AccountID: p.AccountID, Currency: cur, Received: p.Received, Amount: p.Amount, GSTCode: p.GSTCode,
		Reference: p.Reference, Notes: p.Notes}
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
	return d, nil
}

// Prepared is a deposit version as the rule reads it, with its customer and whether it bears GST.
type Prepared struct {
	Draft      Draft
	Deposit    ledger.CustomerDeposit
	Customer   storage.Contact
	Registered bool // the organisation is GST-registered on the deposit's date
	GSTVersion string
}

// Prepare reads one version of a deposit for the rule: its customer, its kind, its rate (the typed one, else the
// cascade's for its date when the money account is in the deposit's currency), and the GST it bears: a part payment's
// code from the sg-gst version in force on its date, none before the organisation is GST-registered (FR-ORG-03); a
// security deposit names no code and bears none until it is used. Problems the rule would refuse come back as
// ledger.Problems, with the engine's own, from PostCustomerDeposit.
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
	if d.CustomerID == uuid.Nil {
		add(CodeCustomer, "customer_id", "the deposit has no customer", "Choose the customer who paid it")
	} else {
		c, err := u.Contacts().Get(ctx, d.CustomerID)
		switch {
		case errors.Is(err, ports.ErrNotFound):
			add(CodeCustomer, "customer_id", "the customer does not exist in this organisation", "Choose a customer from your contacts")
		case err != nil:
			return Prepared{}, nil, err
		case !c.Customer:
			add(CodeCustomer, "customer_id", fmt.Sprintf("%s is not a customer", c.LegalName), "Mark the contact as a customer, or choose another")
		default:
			out.Customer = c
		}
	}
	if !slices.Contains(Kinds, d.Kind) {
		add(CodeKind, "kind", fmt.Sprintf("%q is not a kind of deposit", d.Kind), "Choose part_payment or security")
	}
	r := ledger.CustomerDeposit{ID: doc.ID, Version: doc.Version, Date: d.Date, Memo: memo(out.Customer, doc), Account: d.AccountID, Currency: d.Currency,
		Rate: d.Rate, Received: d.Received, Amount: d.Amount}
	acc, known := s.Accounts[d.AccountID]
	if d.Currency != s.Base && d.Rate == nil && known && acc.Currency == d.Currency.Code && !d.Date.IsZero() {
		f, err := fx.Lookup(ctx, u, s.Base, d.Currency, d.Date)
		var fe *fx.Error
		switch {
		case errors.As(err, &fe):
			add(fe.Code, "fx_rate", fe.Message, fe.Fix)
		case err != nil:
			return Prepared{}, nil, err
		default:
			rate := f.Rate
			r.Rate = &rate
		}
	}

	// GST: a part payment's code from the version in force on its date, and its rate only once registered.
	version, err := gst.Version(ctx, u, d.Date)
	if err != nil {
		return Prepared{}, nil, err
	}
	out.GSTVersion = version.ID
	if !d.Date.IsZero() {
		regs, err := u.GSTRegistrations().List(ctx)
		if err != nil {
			return Prepared{}, nil, err
		}
		_, out.Registered = gst.StatusOn(regs, d.Date)
	}
	switch d.Kind {
	case KindSecurity:
		if d.GSTCode != "" {
			add(CodeGSTCode, "gst_code", "a security deposit bears no GST until it is used as payment", "Leave the GST code out, or record a part payment")
		}
	case KindPartPayment:
		code, ok := pack.Code(version, d.GSTCode)
		if !ok || code.Direction != pack.DirSupply {
			add(CodeGSTCode, "gst_code", fmt.Sprintf("%q is not a GST code for supplies", d.GSTCode), "Choose the GST code of what it pays for, such as SR, ZR or OS")
			break
		}
		if out.Registered {
			standard, _ := pack.RateOn(version, d.Date)
			switch {
			case code.StandardRated:
				r.GSTRate = standard
			case code.ValueBox != 0: // zero-rated and exempt supplies are reported at no GST
				r.GSTRate = "0"
			}
		}
	}
	out.Deposit = r
	return out, probs, nil
}

// memo names a deposit in its entry: the customer and the deposit's number, once it has one.
func memo(c storage.Contact, doc storage.Document) string {
	parts := []string{}
	for _, x := range []string{c.LegalName, doc.Number} {
		if x != "" {
			parts = append(parts, x)
		}
	}
	if len(parts) == 0 {
		return "Customer deposit"
	}
	return strings.Join(parts, " ")
}

// Open is a posted deposit as an invoice or a refund finds it: its customer and kind, its gross amount in its currency,
// what it came to in the base currency (gross, its GST and the rest, which 2410 holds), and what is used of it (deducted
// by invoices or refunded) and left, in its currency.
type Open struct {
	Deposit  storage.Document
	Number   string
	Customer uuid.UUID
	Kind     string
	Total    money.Money
	Base     money.Money
	GST      money.Money
	Net      money.Money
	Used     money.Money
	Left     money.Money
}

// OpenDeposit is what is left of a posted deposit: CodeNotDeposit when the document is not one. s finds the accounts
// its entry posted to.
func OpenDeposit(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, s ledger.Settings) (Open, error) {
	doc, err := u.Documents().Get(ctx, id)
	if errors.Is(err, ports.ErrNotFound) {
		return Open{}, &Error{CodeNotDeposit, fmt.Sprintf("deposit %s does not exist in this organisation", id), "Choose one of the customer's deposits"}
	}
	if err != nil {
		return Open{}, err
	}
	if !IsDeposit(doc) || doc.Status != storage.DocumentPosted {
		return Open{}, &Error{CodeNotDeposit, fmt.Sprintf("%s is not a posted customer deposit", cmp(doc.Number, doc.ID.String())), "Choose one of the customer's deposits"}
	}
	d, err := Read(doc, s.Base)
	if err != nil {
		return Open{}, err
	}
	e, err := u.Ledger().ByIdentity(ctx, ledger.CustomerDepositIdentity(doc.ID, doc.Version, ledger.RuleCustomerDeposit))
	if err != nil {
		return Open{}, err
	}
	out := Open{Deposit: doc, Number: doc.Number, Customer: d.CustomerID, Kind: d.Kind, Total: d.Amount, Base: money.Zero(s.Base), GST: money.Zero(s.Base),
		Net: money.Zero(s.Base), Used: money.Zero(d.Currency)}
	for _, l := range e.Lines {
		base, err := money.ParseStored(string(l.BaseAmount), s.Base)
		if err != nil {
			return Open{}, err
		}
		switch {
		case l.AccountID == d.AccountID:
			out.Base = base
		case s.Accounts[l.AccountID].Role == ledger.RoleCustomerDeposits:
			out.Net = base.Neg()
		case s.Accounts[l.AccountID].Role == ledger.RoleGSTControl:
			out.GST = base.Neg()
		}
	}
	allocs, err := u.Allocations().OfDocument(ctx, doc.ID)
	if err != nil {
		return Open{}, err
	}
	for _, a := range allocs {
		x, err := money.ParseStored(string(a.Amount), d.Currency)
		if err != nil {
			return Open{}, err
		}
		if out.Used, err = out.Used.Add(x); err != nil {
			return Open{}, err
		}
	}
	if out.Left, err = out.Total.Sub(out.Used); err != nil {
		return Open{}, err
	}
	return out, nil
}

func cmp(s, otherwise string) string {
	if s == "" {
		return otherwise
	}
	return s
}
