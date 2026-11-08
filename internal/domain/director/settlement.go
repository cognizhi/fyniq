package director

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable codes of a settlement (FR-EQ-05, FR-EQ-07; design D10).
const (
	CodeNotSettlement = "FOUNDER-27" // the document is not a settlement
	CodeSettlement    = "FOUNDER-28" // a settlement that does not fit: its person, account, amount, route or details
	CodeSettled       = "FOUNDER-29" // a settlement already asked for or decided, which changes no more
	CodeNoAgreement   = "FOUNDER-30" // a settlement sent for approval with no loan note, minute or deed attached
)

// The settlement routes (FR-EQ-05; the owner's decision of 3 Nov 2026): keep it as a loan on terms, repay it, convert it
// to shares (D11), waive it (FR-EQ-07), or turn it into a recorded SAFE or note the person holds (FR-EQ-18, with the
// equity register).
const (
	RouteKeep    = "keep"
	RouteRepay   = "repay"
	RouteConvert = "convert"
	RouteWaive   = "waive"
	RouteSAFE    = "safe"
)

// The capacities in which a person waives what the company owes them (FR-EQ-07): as a shareholder, the waiver is a
// capital contribution; otherwise it is income of the company.
const (
	CapacityShareholder = "shareholder"
	CapacityOther       = "other"
)

// The longest text a settlement keeps.
const (
	MaxReason = 1000
	MaxRepay  = 200
)

// Terms are what a loan kept is on (D10): its interest rate a year (a percentage, "0" by default), the day it is repaid
// or nil for on demand, and whether it is subordinated to other creditors.
type Terms struct {
	InterestRate string
	RepayOn      *time.Time
	Subordinated bool
}

// Waiver is what a waiver credits (D10, FR-EQ-07): the account (the capital-contribution account, 3300, or an income
// account the accountant chooses), the capacity in which the person waives, and why.
type Waiver struct {
	CreditAccountID uuid.UUID
	Capacity        string
	Reason          string
}

// Settlement is a settlement of what the company owes a director or shareholder on one director's account (FR-EQ-05,
// design D10): the person, the account (2500 or 2120), the amount (at most what is owed when it is asked for), the route
// and its details, and the day it was drafted. Another person approves it (director_settlement.approve), never the
// person, and the accountant too when every available approver benefits (FM-9).
type Settlement struct {
	Date       time.Time
	ContactID  uuid.UUID
	AccountID  uuid.UUID
	Amount     money.Money
	Route      string
	Terms      *Terms
	Waiver     *Waiver
	Conversion *Conversion
	// SAFE is the recorded SAFE or note the balance becomes (RouteSAFE): its money, Dr the director's account · Cr 2700.
	SAFE *SAFE
	// DeductedEarlier tags a waiver of amounts the company deducted as costs earlier (pay or fees on 2120), for the tax
	// computation: the waiver may be taxable to the company [verify]. Set when it is approved.
	DeductedEarlier bool
	// ApprovedOn is the organisation's day a conversion was approved: no effective date is earlier, and the shareholders'
	// approval lapses from it (D12). Set when it is approved.
	ApprovedOn *time.Time
}

// SAFE is the recorded SAFE or convertible note a director's balance becomes (FR-EQ-05, FR-EQ-18): the person holds it,
// and its money is the amount settled.
type SAFE struct {
	InstrumentID uuid.UUID
}

type safePayload struct {
	InstrumentID uuid.UUID `json:"instrument_id"`
}

type conversionPayload struct {
	ClassAccountID      uuid.UUID `json:"class_account_id"`
	Price               string    `json:"price"`
	PriceBasis          string    `json:"price_basis"`
	Capacity            string    `json:"capacity"`
	Measurement         string    `json:"measurement"`
	FairValuePrice      string    `json:"fair_value_price,omitempty"`
	DifferenceAccountID uuid.UUID `json:"difference_account_id"`
	Note                string    `json:"note,omitempty"`
}

type termsPayload struct {
	InterestRate string `json:"interest_rate"`
	RepayOn      string `json:"repay_on,omitempty"`
	Subordinated bool   `json:"subordinated,omitempty"`
}

type waiverPayload struct {
	CreditAccountID uuid.UUID `json:"credit_account_id"`
	Capacity        string    `json:"capacity"`
	Reason          string    `json:"reason"`
}

type settlementPayload struct {
	Date            string             `json:"date"`
	ContactID       uuid.UUID          `json:"contact_id"`
	AccountID       uuid.UUID          `json:"account_id"`
	Amount          *money.Money       `json:"amount"`
	Route           string             `json:"route"`
	Terms           *termsPayload      `json:"terms,omitempty"`
	Waiver          *waiverPayload     `json:"waiver,omitempty"`
	Conversion      *conversionPayload `json:"conversion,omitempty"`
	SAFE            *safePayload       `json:"safe,omitempty"`
	DeductedEarlier bool               `json:"deducted_earlier,omitempty"`
	ApprovedOn      string             `json:"approved_on,omitempty"`
}

// EncodeSettlement is a settlement as a document version stores it.
func EncodeSettlement(s Settlement) ([]byte, error) {
	amount := s.Amount
	p := settlementPayload{Date: dayOf(s.Date), ContactID: s.ContactID, AccountID: s.AccountID, Amount: &amount, Route: s.Route, DeductedEarlier: s.DeductedEarlier}
	if t := s.Terms; t != nil {
		p.Terms = &termsPayload{InterestRate: t.InterestRate, Subordinated: t.Subordinated}
		if t.RepayOn != nil {
			p.Terms.RepayOn = dayOf(*t.RepayOn)
		}
	}
	if w := s.Waiver; w != nil {
		p.Waiver = &waiverPayload{CreditAccountID: w.CreditAccountID, Capacity: w.Capacity, Reason: w.Reason}
	}
	if c := s.Conversion; c != nil {
		x := conversionPayload(*c)
		p.Conversion = &x
	}
	if x := s.SAFE; x != nil {
		p.SAFE = &safePayload{InstrumentID: x.InstrumentID}
	}
	if s.ApprovedOn != nil {
		p.ApprovedOn = dayOf(*s.ApprovedOn)
	}
	return json.Marshal(p)
}

// IsSettlement says whether a document is a settlement.
func IsSettlement(doc storage.Document) bool { return doc.Type == storage.DocumentDirectorSettlement }

// ReadSettlement reads a settlement's version as it was saved.
func ReadSettlement(doc storage.Document) (Settlement, error) {
	if !IsSettlement(doc) {
		return Settlement{}, &Error{CodeNotSettlement, fmt.Sprintf("document %s is a %s, not a settlement", doc.ID, doc.Type), "Open it with its own module"}
	}
	bad := func(what string) error {
		return &Error{CodeSettlement, "the saved settlement cannot be read: " + what, "Draft it again"}
	}
	var p settlementPayload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return Settlement{}, bad(err.Error())
	}
	if p.Amount == nil {
		return Settlement{}, bad("it has no amount")
	}
	date, err := time.Parse(time.DateOnly, p.Date)
	if err != nil {
		return Settlement{}, bad(fmt.Sprintf("the date %q", p.Date))
	}
	s := Settlement{Date: date, ContactID: p.ContactID, AccountID: p.AccountID, Amount: *p.Amount, Route: p.Route, DeductedEarlier: p.DeductedEarlier}
	if t := p.Terms; t != nil {
		s.Terms = &Terms{InterestRate: t.InterestRate, Subordinated: t.Subordinated}
		if t.RepayOn != "" {
			d, err := time.Parse(time.DateOnly, t.RepayOn)
			if err != nil {
				return Settlement{}, bad(fmt.Sprintf("the repayment date %q", t.RepayOn))
			}
			s.Terms.RepayOn = &d
		}
	}
	if w := p.Waiver; w != nil {
		s.Waiver = &Waiver{CreditAccountID: w.CreditAccountID, Capacity: w.Capacity, Reason: w.Reason}
	}
	if c := p.Conversion; c != nil {
		x := Conversion(*c)
		s.Conversion = &x
	}
	if x := p.SAFE; x != nil {
		s.SAFE = &SAFE{InstrumentID: x.InstrumentID}
	}
	if p.ApprovedOn != "" {
		on, err := time.Parse(time.DateOnly, p.ApprovedOn)
		if err != nil {
			return Settlement{}, bad(fmt.Sprintf("the approval day %q", p.ApprovedOn))
		}
		s.ApprovedOn = &on
	}
	return s, nil
}

// Owed is what the company owes the person on one director's account at the end of a day: the credit there, positive,
// or zero when there is none.
func Owed(ctx context.Context, u storage.UnitOfWork, base money.Currency, contact, account uuid.UUID, day time.Time) (money.Money, error) {
	people, err := balance.People(ctx, u, base, day)
	if err != nil {
		return money.Money{}, err
	}
	for _, p := range people {
		if p.Party != contact {
			continue
		}
		if b, ok := p.Accounts[account]; ok && b.Sign() < 0 {
			return b.Neg(), nil
		}
	}
	return money.Zero(base), nil
}

// CheckSettlement is what is wrong with a settlement as drafted, every problem at once (ENG-22): a person who is not a
// director or shareholder; an account that is not a director's account; an amount not more than zero in the base
// currency, or more than is owed on the account at the end of today less what other settlements approved to be repaid
// still have open; a route not built yet; a route's details missing or out of range; or, to repay, no approved SGD
// account at a Singapore bank to repay the person into (CodeNoRepayTo).
func CheckSettlement(ctx context.Context, u storage.UnitOfWork, s Settlement, s2 ledger.Settings, today time.Time) (ledger.Problems, error) {
	var probs ledger.Problems
	add := func(code, field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: code, Field: field, Message: msg, Fix: fix})
	}
	base := s2.Base
	ct, err := u.Contacts().Get(ctx, s.ContactID)
	switch {
	case errors.Is(err, ports.ErrNotFound):
		add(CodeNotPerson, "contact_id", "the contact does not exist in this organisation", "Choose the director or shareholder the company owes")
	case err != nil:
		return nil, err
	case !ct.Director && !ct.Shareholder:
		add(CodeNotPerson, "contact_id", ct.LegalName+" is not a director or shareholder", "Choose a director or shareholder")
	}
	acc, ok := s2.Accounts[s.AccountID]
	if !ok || !acc.DirectorAccount() {
		add(CodeSettlement, "account_id", "the account is not a director's account", "Choose 2500 Loan from director / shareholder or 2120 Deferred remuneration payable")
	}
	switch {
	case s.Amount.Currency() != base || s.Amount.Sign() <= 0:
		add(CodeSettlement, "amount", fmt.Sprintf("the amount is %s %s", s.Amount, s.Amount.Currency()), fmt.Sprintf("Enter the amount in %s, more than zero", base))
	case ok && acc.DirectorAccount():
		owed, err := Owed(ctx, u, base, s.ContactID, s.AccountID, today)
		if err != nil {
			return nil, err
		}
		// What other settlements approved to be repaid still have open is spoken for (design D10).
		repay, err := PayableSettlements(ctx, u, base)
		if err != nil {
			return nil, err
		}
		for _, p := range repay {
			if p.Settlement.ContactID == s.ContactID && p.Settlement.AccountID == s.AccountID {
				if owed, err = owed.Sub(p.Open); err != nil {
					return nil, err
				}
			}
		}
		if c, _ := s.Amount.Cmp(owed); c > 0 {
			add(CodeSettlement, "amount", fmt.Sprintf("the amount %s is more than the %s the company owes on %s, less what is approved to be repaid", s.Amount, owed, acc.Code),
				"Settle at most what is owed")
		}
	}
	switch s.Route {
	case RouteKeep:
		t := s.Terms
		if t == nil {
			t = &Terms{InterestRate: "0"}
		}
		var rate apd.Decimal
		if _, _, err := rate.SetString(t.InterestRate); err != nil || rate.Negative || rate.Cmp(apd.New(100, 0)) > 0 {
			add(CodeSettlement, "terms.interest_rate", fmt.Sprintf("the interest rate %q is not a percentage from 0 to 100", t.InterestRate),
				"Enter the rate a year, such as 0 for an interest-free loan")
		}
		if t.RepayOn != nil && !t.RepayOn.After(today) {
			add(CodeSettlement, "terms.repay_on", "the repayment day is not in the future", "Choose a later day, or leave it out for on demand")
		}
	case RouteWaive:
		w := s.Waiver
		if w == nil {
			add(CodeSettlement, "waiver", "a waiver needs the account it credits, the capacity and the reason", "Fill in the waiver")
			break
		}
		credit, ok := s2.Accounts[w.CreditAccountID]
		switch {
		case !ok || credit.Group || !credit.Active:
			add(CodeSettlement, "waiver.credit_account_id", "the account a waiver credits is not an active, postable account", "Choose 3300 Capital contribution, or an income account")
		case credit.DirectorAccount() || credit.Money || credit.ShareCapital || credit.Role == ledger.RoleGSTControl:
			add(CodeSettlement, "waiver.credit_account_id", fmt.Sprintf("account %s is not one a waiver credits", credit.Code), "Choose 3300 Capital contribution, or an income account")
		}
		if w.Capacity != CapacityShareholder && w.Capacity != CapacityOther {
			add(CodeSettlement, "waiver.capacity", fmt.Sprintf("the capacity %q is not shareholder or other", w.Capacity), "Choose shareholder or other")
		}
		switch n := utf8.RuneCountInString(w.Reason); {
		case strings.TrimSpace(w.Reason) == "":
			add(CodeSettlement, "waiver.reason", "the waiver says no reason", "Say why the person waives it, such as ahead of the seed round")
		case n > MaxReason:
			add(CodeSettlement, "waiver.reason", fmt.Sprintf("the reason has %d characters; the most is %d", n, MaxReason), "Shorten it")
		}
	case RouteRepay:
		if ct.ID == uuid.Nil {
			break
		}
		_, acc, _, err := RepayTo(ctx, u, s.ContactID)
		if err != nil {
			return nil, err
		}
		if acc == nil {
			probs = append(probs, noRepayTo(ct.LegalName))
		}
	case RouteConvert:
		probs = append(probs, checkConversion(s, s2)...)
	case RouteSAFE: // the instrument itself is checked with the equity register (journal.checkSettlement)
		if s.SAFE == nil || s.SAFE.InstrumentID == uuid.Nil {
			add(CodeSettlement, "safe.instrument_id", "the settlement names no SAFE or note", "Record the SAFE or note the person holds, then choose it")
		}
	default:
		add(CodeSettlement, "route", fmt.Sprintf("the route %q is not one Fyniq knows", s.Route), "Choose keep, repay, convert, waive or safe")
	}
	return probs, nil
}

// Rule is an approved waiver as the rule reads it.
func (s Settlement) Rule(doc storage.Document, postOn time.Time) ledger.DirectorWaiver {
	w := Waiver{}
	if s.Waiver != nil {
		w = *s.Waiver
	}
	return ledger.DirectorWaiver{ID: doc.ID, Version: doc.Version, PostOn: postOn, Memo: strings.TrimSpace(doc.Number + " " + w.Reason), Party: s.ContactID,
		Account: s.AccountID, Credit: w.CreditAccountID, Amount: s.Amount}
}

// SAFERule is an approved settlement into a SAFE or note as the rule reads it, posting on postOn: Dr the director's
// account · Cr holding, 2700, naming the instrument.
func (s Settlement) SAFERule(doc storage.Document, postOn time.Time, holding uuid.UUID) ledger.DirectorSAFE {
	var instrument uuid.UUID
	if s.SAFE != nil {
		instrument = s.SAFE.InstrumentID
	}
	return ledger.DirectorSAFE{ID: doc.ID, Version: doc.Version, PostOn: postOn, Memo: strings.TrimSpace(doc.Number + " into a SAFE or note"), Party: s.ContactID,
		Account: s.AccountID, Holding: holding, Instrument: instrument, Amount: s.Amount}
}

// The settings of the founders' money (FR-EQ-05; recommended default 15, FR-ORG-08).
const (
	RouteSetting      = "founders.default_route"
	PriceBasisSetting = "founders.price_basis"
)

// DefaultRoute is the settlement route offered first, in force on a day: keep as a loan by default.
func DefaultRoute(ctx context.Context, u storage.UnitOfWork, on time.Time) (string, error) {
	changes, err := u.Settings().Changes(ctx, RouteSetting)
	if err != nil {
		return "", err
	}
	def, _ := settings.Lookup(RouteSetting)
	return settings.On(def, changes, on).Value, nil
}

// DefaultPriceBasis is the price basis a conversion is offered first, in force on a day: the latest priced round by
// default.
func DefaultPriceBasis(ctx context.Context, u storage.UnitOfWork, on time.Time) (string, error) {
	changes, err := u.Settings().Changes(ctx, PriceBasisSetting)
	if err != nil {
		return "", err
	}
	def, _ := settings.Lookup(PriceBasisSetting)
	return settings.On(def, changes, on).Value, nil
}

// SettlementsSince are the settlements of every person that are waiting for approval (draft documents with a pending
// request, which the caller passes as asked) or were decided on or after a day: the ones FM-9 counts as a benefit.
func SettlementsSince(ctx context.Context, u storage.UnitOfWork, asked map[string]bool, since time.Time) (map[uuid.UUID]bool, error) {
	out := map[uuid.UUID]bool{}
	var afterDate string
	var afterID *uuid.UUID
	for {
		docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentDirectorSettlement, AfterDate: afterDate, AfterID: afterID, Limit: 500})
		if err != nil {
			return nil, err
		}
		for _, doc := range docs {
			afterDate, afterID = doc.Date, &doc.ID
			waiting := doc.Status == storage.DocumentDraft && asked[doc.ID.String()]
			recent := doc.Status == storage.DocumentPosted && doc.PostedAt != nil && !doc.PostedAt.Before(since)
			if !waiting && !recent {
				continue
			}
			s, err := ReadSettlement(doc)
			if err != nil {
				return nil, err
			}
			out[s.ContactID] = true
		}
		if len(docs) < 500 {
			return out, nil
		}
	}
}

// EveryApproverBenefits says whether every member who could sign a settlement benefits from one of their own (FM-9,
// design D10): candidates are the members who could sign it (who hold A on "Director accounts", other than its
// requester and the person settled); contactOf is the director or shareholder a member signs in as, if any; benefits are
// the persons with a settlement waiting or decided in the last 30 days. With no candidate, it is false: no step is added
// that nobody could stand in for. [accountant to confirm]
func EveryApproverBenefits(candidates []uuid.UUID, contactOf func(uuid.UUID) (uuid.UUID, bool), benefits map[uuid.UUID]bool) bool {
	for _, m := range candidates {
		c, ok := contactOf(m)
		if !ok || !benefits[c] {
			return false
		}
	}
	return len(candidates) > 0
}
