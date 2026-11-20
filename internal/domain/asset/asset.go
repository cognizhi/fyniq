// Package asset is the fixed asset register (FR-FA-01…03; PRD-001 §8.9; p1-fixed-assets): an asset's content as each
// version of its fixed_asset document saves it, and the checks it meets. A registered asset posts nothing of its own:
// its cost is already on its asset account, put there by the bill line or bank line it came from, or, for an asset
// bought before the books start, by the opening balances with its accumulated depreciation on 1590 (design D1). Each
// version carries what came before it (design D2): its cost, residual value, life and depreciation start, and the
// accumulated depreciation up to the first month it posts, so its schedule is a pure function of the version.
package asset

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable codes of an asset's refusals (ENG-23).
const (
	CodeNotAsset   = "ASSET-01" // the document is not a fixed asset
	CodeBadContent = "ASSET-02" // the saved asset cannot be read
	CodeSource     = "ASSET-03" // the source is not a line of a posted bill or bank line that debits an asset account, or has an asset already
	CodeAccount    = "ASSET-04" // the account is not an active asset account of the register
	CodeLife       = "ASSET-05" // a useful life outside 1 to MaxLifeMonths months
	CodeResidual   = "ASSET-06" // a residual value below zero, or above the cost
	CodeStart      = "ASSET-07" // a depreciation start before the purchase month
	CodeMethod     = "ASSET-08" // a method other than straight-line (reducing balance follows in P2)
	CodeOpening    = "ASSET-09" // an asset bought before the books start without them, or with figures that do not fit
	CodeTooLong    = "ASSET-10" // a name or notes longer than the most kept
	CodePosted     = "ASSET-11" // a change of what posted months depend on, once a month of the asset has posted
	CodeDisposed   = "ASSET-12" // a change of an asset nothing of which is left
	CodeAssetVoid  = "ASSET-20" // (rev 0.151) a void refused: a disposal of the asset stands, or a month to reverse is in a period not open
	CodeVoided     = "ASSET-21" // (rev 0.151) an asset voided: it changes, disposes and voids no more
	CodeTaxMethod  = "ASSET-23" // (rev 0.582) a capital allowance election that is not section 19, 19A(1) or 19A(10A), or whose working life the pack does not allow
	CodeTaxFixed   = "ASSET-24" // (rev 0.582) a section 19 election changed: the company's election of a working life is irrevocable
)

// The longest texts an asset keeps, and its longest life.
const (
	MaxName       = 200
	MaxNotes      = 2000
	MaxLifeMonths = 600 // 50 years
)

// StraightLine is the depreciation method of P1 (FR-FA-01): cost less residual, evenly over the life in months.
const StraightLine = "straight_line"

// RegisterGroups are the report groups of the asset accounts an asset sits on: its category (design D1) [verify with the
// accountant's chart review].
var RegisterGroups = []string{"Property, plant & equipment", "Intangible assets"}

// InRegister says whether an account is one an asset sits on: active, postable, in one of the register's groups.
func InRegister(a storage.Account) bool {
	if a.Group || !a.Active {
		return false
	}
	for _, g := range RegisterGroups {
		if a.ReportGroup == g {
			return true
		}
	}
	return false
}

// The capital allowance methods an asset may be elected under (FR-FA-04): the pack sg-capital-allowances holds their values.
const (
	TaxS19     = "s19"     // section 19: the initial allowance and annual allowances over an elected working life
	TaxS19A1   = "s19a1"   // section 19A(1): a third a year over 3 years
	TaxS19A10A = "s19a10a" // section 19A(10A): the whole cost in one year, for a low-value asset
)

// TaxElection is the capital allowance claim a company elects for an asset (FR-FA-04, rev 0.582): the method and, under section 19, the working life the Sixth Schedule
// prescribes for it (6, 12 or 16 years) and the years elected, which the pack allows for that life. Nil until the company elects: the capital allowance schedule lists such an
// asset apart and never guesses. It changes nothing in the books: the depreciation is the asset's own.
type TaxElection struct {
	Method          string
	PrescribedYears int
	Years           int
}

// CheckTax is the election's own shape: a known method, and under section 19 a prescribed life and the years elected, under the others neither. Which years the prescribed life
// allows is the pack's (the caller asks sg-capital-allowances).
func CheckTax(t *TaxElection) *Error {
	if t == nil {
		return nil
	}
	switch t.Method {
	case TaxS19:
		if t.PrescribedYears < 1 || t.Years < 1 {
			return &Error{CodeTaxMethod, "a section 19 election names the working life the Sixth Schedule prescribes and the years elected", "Send both, such as 6 and 6 for a motor vehicle"}
		}
	case TaxS19A1, TaxS19A10A:
		if t.PrescribedYears != 0 || t.Years != 0 {
			return &Error{CodeTaxMethod, "a working life belongs to a section 19 election only", "Leave the working life out"}
		}
	default:
		return &Error{CodeTaxMethod, fmt.Sprintf("the method %q is not section 19, 19A(1) or 19A(10A)", t.Method), "Use s19, s19a1 or s19a10a"}
	}
	return nil
}

// Source is the line an asset was registered from: a line (from 1) of a bill, or a part of a categorised bank line, at
// the version that posted it.
type Source struct {
	DocumentID uuid.UUID
	Version    int
	Line       int
}

// Asset is one version of a registered asset: its name, the asset account it sits on (its category), its source (nil
// for one bought before the books start), the day it was bought, its cost and residual value in the base currency, its
// useful life in months, its method, and the month depreciation starts (the purchase month, or a later one when it went
// into use later). Carried is the accumulated depreciation up to From, the first month this version posts: nothing for a
// new asset's first version; the opening accumulated depreciation for one bought before the books start, From being the
// books start's month; what earlier versions posted, for a later one (design D2). Disposed marks a version that a
// disposal of the whole made: nothing of it is left, and nothing more depreciates. DisposalID is the disposal that made
// the version, nil for any other.
type Asset struct {
	Name        string
	AccountID   uuid.UUID
	Source      *Source
	PurchasedOn time.Time
	Cost        money.Money
	Residual    money.Money
	LifeMonths  int
	Method      string
	Start       time.Time
	Carried     money.Money
	From        time.Time
	Disposed    bool
	DisposalID  *uuid.UUID
	Notes       string
	Tax         *TaxElection
}

type taxPayload struct {
	Method          string `json:"method"`
	PrescribedYears int    `json:"prescribed_years,omitempty"`
	Years           int    `json:"years,omitempty"`
}

type sourcePayload struct {
	DocumentID uuid.UUID `json:"document_id"`
	Version    int       `json:"version"`
	Line       int       `json:"line"`
}

type payload struct {
	Name        string         `json:"name"`
	AccountID   uuid.UUID      `json:"account_id"`
	Source      *sourcePayload `json:"source,omitempty"`
	PurchasedOn string         `json:"purchased_on"`
	Date        string         `json:"date"` // the purchase date again, under the name a list of documents sorts by
	Cost        *money.Money   `json:"cost"`
	Residual    *money.Money   `json:"residual"`
	LifeMonths  int            `json:"life_months"`
	Method      string         `json:"method"`
	Start       string         `json:"start"`
	Carried     *money.Money   `json:"carried"`
	From        string         `json:"from"`
	Disposed    bool           `json:"disposed,omitempty"`
	DisposalID  *uuid.UUID     `json:"disposal_id,omitempty"`
	Notes       string         `json:"notes,omitempty"`
	Tax         *taxPayload    `json:"tax,omitempty"`
}

// Error is an asset command's refusal with a stable code.
type Error struct {
	Code    string
	Message string
	Fix     string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Fix) }

// Month is the first day of t's month, in UTC.
func Month(t time.Time) time.Time {
	y, m, _ := t.Date()
	return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
}

// End is the last month an asset depreciates in: start + life − 1 (B.4).
func (a Asset) End() time.Time { return Month(a.Start).AddDate(0, a.LifeMonths-1, 0) }

const monthLayout = "2006-01"

// Encode is an asset version as its document stores it.
func Encode(a Asset) ([]byte, error) {
	cost, residual, carried := a.Cost, a.Residual, a.Carried
	p := payload{Name: a.Name, AccountID: a.AccountID, PurchasedOn: a.PurchasedOn.Format(time.DateOnly), Date: a.PurchasedOn.Format(time.DateOnly),
		Cost: &cost, Residual: &residual, LifeMonths: a.LifeMonths, Method: a.Method, Start: a.Start.Format(monthLayout), Carried: &carried,
		From: a.From.Format(monthLayout), Disposed: a.Disposed, DisposalID: a.DisposalID, Notes: a.Notes}
	if a.Source != nil {
		p.Source = &sourcePayload{DocumentID: a.Source.DocumentID, Version: a.Source.Version, Line: a.Source.Line}
	}
	if a.Tax != nil {
		p.Tax = &taxPayload{Method: a.Tax.Method, PrescribedYears: a.Tax.PrescribedYears, Years: a.Tax.Years}
	}
	return json.Marshal(p)
}

// IsAsset says whether a document is a registered asset.
func IsAsset(doc storage.Document) bool { return doc.Type == storage.DocumentFixedAsset }

// Read reads an asset version as it was saved.
func Read(doc storage.Document) (Asset, error) {
	if !IsAsset(doc) {
		return Asset{}, &Error{CodeNotAsset, fmt.Sprintf("document %s is a %s, not a fixed asset", doc.ID, doc.Type), "Open it with its own module"}
	}
	bad := func(what string) (Asset, error) {
		return Asset{}, &Error{CodeBadContent, "the saved asset cannot be read: " + what, "Register it again"}
	}
	var p payload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return bad(err.Error())
	}
	if p.Cost == nil || p.Residual == nil || p.Carried == nil {
		return bad("it has no cost, residual value or carried depreciation")
	}
	a := Asset{Name: p.Name, AccountID: p.AccountID, Cost: *p.Cost, Residual: *p.Residual, LifeMonths: p.LifeMonths, Method: p.Method, Carried: *p.Carried,
		Disposed: p.Disposed, DisposalID: p.DisposalID, Notes: p.Notes}
	var err error
	if a.PurchasedOn, err = time.Parse(time.DateOnly, p.PurchasedOn); err != nil {
		return bad(fmt.Sprintf("the purchase date %q", p.PurchasedOn))
	}
	if a.Start, err = time.Parse(monthLayout, p.Start); err != nil {
		return bad(fmt.Sprintf("the start %q", p.Start))
	}
	if a.From, err = time.Parse(monthLayout, p.From); err != nil {
		return bad(fmt.Sprintf("the first month %q", p.From))
	}
	if p.Source != nil {
		a.Source = &Source{DocumentID: p.Source.DocumentID, Version: p.Source.Version, Line: p.Source.Line}
	}
	if p.Tax != nil {
		a.Tax = &TaxElection{Method: p.Tax.Method, PrescribedYears: p.Tax.PrescribedYears, Years: p.Tax.Years}
	}
	return a, nil
}

// Check runs an asset version's own rules, every problem at once (ENG-23): its name and notes, its account (acc, nil
// when the chart has no such account), its life, residual, method and start, and the figures of one bought before the
// books start (booksStart, zero when the organisation has none).
func Check(a Asset, acc *storage.Account, base money.Currency, booksStart time.Time) ledger.Problems {
	var probs ledger.Problems
	add := func(code, field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: code, Field: field, Message: msg, Fix: fix})
	}
	switch name := strings.TrimSpace(a.Name); {
	case name == "":
		add(CodeTooLong, "name", "the asset has no name", "Name it as you would find it in the office, such as \"MacBook Pro 14, Wei Ling\"")
	case len(a.Name) > MaxName:
		add(CodeTooLong, "name", fmt.Sprintf("the name has %d characters; the most is %d", len(a.Name), MaxName), "Shorten it")
	}
	if len(a.Notes) > MaxNotes {
		add(CodeTooLong, "notes", fmt.Sprintf("the notes have %d characters; the most is %d", len(a.Notes), MaxNotes), "Shorten them")
	}
	switch {
	case acc == nil:
		add(CodeAccount, "account_id", "the account does not exist in this organisation", "Choose an asset account, such as 1510 Computer equipment")
	case !InRegister(*acc):
		add(CodeAccount, "account_id", fmt.Sprintf("account %s is not an active asset account of the register", acc.Code),
			"Choose an account whose report group is Property, plant & equipment or Intangible assets")
	}
	if a.LifeMonths < 1 || a.LifeMonths > MaxLifeMonths {
		add(CodeLife, "life_months", fmt.Sprintf("the useful life is %d months", a.LifeMonths), fmt.Sprintf("Enter a life of 1 to %d months", MaxLifeMonths))
	}
	if a.Method != StraightLine {
		add(CodeMethod, "method", fmt.Sprintf("the method %q is not available", a.Method), "Use straight-line; reducing balance follows in a later release")
	}
	for _, m := range []struct {
		field string
		v     money.Money
	}{{"cost", a.Cost}, {"residual", a.Residual}, {"carried", a.Carried}} {
		if m.v.Currency() != base {
			add(CodeResidual, m.field, fmt.Sprintf("the %s is in %s, not the base currency %s", m.field, m.v.Currency(), base),
				"Enter it in the base currency")
			return probs
		}
	}
	if a.Cost.Sign() <= 0 {
		add(CodeOpening, "cost", fmt.Sprintf("the cost is %s %s", a.Cost, base), "An asset costs more than zero")
	}
	above, _ := a.Residual.Cmp(a.Cost)
	switch {
	case a.Residual.Sign() < 0:
		add(CodeResidual, "residual", fmt.Sprintf("the residual value is %s %s", a.Residual, base), "Enter zero or more: what the asset is worth at the end of its life")
	case above > 0:
		add(CodeResidual, "residual", fmt.Sprintf("the residual value %s %s is above the cost %s %s", a.Residual, base, a.Cost, base),
			"Enter a residual value of at most the cost")
	}
	if Month(a.Start).Before(Month(a.PurchasedOn)) {
		add(CodeStart, "start", fmt.Sprintf("depreciation starts in %s, before the asset was bought in %s", a.Start.Format("January 2006"),
			a.PurchasedOn.Format("January 2006")), "Start it in the month it was bought, or the month it went into use")
	}
	if a.Source != nil {
		return probs
	}
	// An asset bought before the books start: its cost and accumulated depreciation are in the opening balances.
	switch {
	case booksStart.IsZero():
		add(CodeOpening, "purchased_on", "the organisation has no books start", "Set the books start in Settings › Organisation, then register the asset")
	case !a.PurchasedOn.Before(booksStart):
		add(CodeOpening, "purchased_on", fmt.Sprintf("the asset was bought on %s, on or after the books start %s", a.PurchasedOn.Format(time.DateOnly),
			booksStart.Format(time.DateOnly)), "Register it from the bill or bank line that bought it")
	}
	left, err := a.Cost.Sub(a.Residual)
	over, _ := a.Carried.Cmp(left)
	switch {
	case a.Carried.Sign() < 0:
		add(CodeOpening, "opening_accumulated", fmt.Sprintf("the accumulated depreciation is %s %s", a.Carried, base), "Enter zero or more, as the opening balances carry it on 1590")
	case err == nil && over > 0:
		add(CodeOpening, "opening_accumulated", fmt.Sprintf("the accumulated depreciation %s %s is more than the cost less the residual value, %s %s", a.Carried, base, left, base),
			"Enter what the opening balances carry for it on 1590")
	}
	return probs
}
