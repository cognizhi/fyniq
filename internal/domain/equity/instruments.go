package equity

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The codes of SAFEs, convertible notes and their templates (FR-EQ-18; p1-equity-register design D6).
const (
	CodeInstrument = "EQUITY-14" // an instrument that does not fit: its form, holder, amount, terms or class
	CodeTemplate   = "EQUITY-15" // a template that is not known, or enabled already
)

// The forms of instrument (FR-EQ-18).
const (
	FormPostMoneySAFE   = "post_money_safe"  // a SAFE whose valuation cap counts the company after the SAFEs convert
	FormPreMoneySAFE    = "pre_money_safe"   // a SAFE whose valuation cap counts the shares before the round
	FormDiscountSAFE    = "discount_safe"    // a SAFE with a discount on the round price and no cap
	FormMFNSAFE         = "mfn_safe"         // a SAFE that takes the best terms of a later instrument
	FormConvertibleNote = "convertible_note" // a loan that converts, with interest and a maturity
)

// The vocabularies an instrument's terms take.
var (
	Forms            = []string{FormPostMoneySAFE, FormPreMoneySAFE, FormDiscountSAFE, FormMFNSAFE, FormConvertibleNote}
	Triggers         = []string{"priced_round", "maturity", "liquidity_event", "other"}
	Denominators     = []string{"post_money_capitalisation", "pre_money_shares", "as_terms"}
	Classifications  = []string{"", "liability", "equity"} // the accountant's, per instrument [verify]
	MaxPriceDecimals = 8
)

// Instrument is a SAFE or convertible note (FR-EQ-18, design D6): its holder, amount and the day it was signed; its cap,
// discount, most-favoured-nation clause, interest and maturity; what triggers its conversion and the class it converts
// into; how its terms count the shares before the round; how a note's interest converts; the conversion price's
// precision; and the accountant's classification, liability or equity [verify per instrument].
type Instrument struct {
	Form             string
	HolderID         uuid.UUID
	Amount           money.Money
	SignedOn         time.Time
	ValuationCap     string // an amount in the base currency; for a SAFE with a cap
	Discount         string // a percentage off the round price, such as "20"
	MFN              bool
	InterestRate     string // a note's simple interest, a percentage a year
	MaturityOn       *time.Time
	Trigger          string
	ClassAccountID   uuid.UUID
	ClassID          *uuid.UUID
	Denominator      string
	DenominatorNote  string
	InterestConverts bool
	PriceDecimals    int
	Classification   string
	Template         string // the template its terms follow, when one does
	ProRata          bool   // a pro rata side letter
	Note             string
	Date             time.Time
}

type instrumentPayload struct {
	Date             string `json:"date"`
	Form             string `json:"form"`
	HolderID         string `json:"holder_id"`
	Amount           string `json:"amount"`
	Currency         string `json:"currency"`
	SignedOn         string `json:"signed_on"`
	ValuationCap     string `json:"valuation_cap,omitempty"`
	Discount         string `json:"discount,omitempty"`
	MFN              bool   `json:"mfn,omitempty"`
	InterestRate     string `json:"interest_rate,omitempty"`
	MaturityOn       string `json:"maturity_on,omitempty"`
	Trigger          string `json:"trigger"`
	ClassAccountID   string `json:"class_account_id"`
	ClassID          string `json:"class_id,omitempty"`
	Denominator      string `json:"denominator"`
	DenominatorNote  string `json:"denominator_note,omitempty"`
	InterestConverts bool   `json:"interest_converts,omitempty"`
	PriceDecimals    int    `json:"price_decimals"`
	Classification   string `json:"classification,omitempty"`
	Template         string `json:"template,omitempty"`
	ProRata          bool   `json:"pro_rata,omitempty"`
	Note             string `json:"note,omitempty"`
}

// Template is a form the engine may convert by itself once an organisation enables it with its lawyer's confirmation
// (FR-EQ-18, question 33). Any term it does not cover converts by a draft for the accountant.
type Template struct {
	Key, Name, Form, Covers, Source string
}

// Templates are the forms Fyniq knows. Only YC's Singapore post-money SAFE with a valuation cap and no discount.
var Templates = []Template{{
	Key: "yc-sg-post-money-cap@1", Name: "SAFE: Valuation Cap, No Discount (Singapore)", Form: FormPostMoneySAFE,
	Covers: "A post-money valuation cap, no discount, no most-favoured-nation clause and no interest, converting into shares at the next priced round.",
	Source: "Y Combinator's Singapore SAFE, with its optional pro rata side letter (checked 14 Oct 2026) [verify the conversion terms with a Singapore lawyer]",
}}

// TemplateOf is a known template, or nil.
func TemplateOf(key string) *Template {
	for i := range Templates {
		if Templates[i].Key == key {
			return &Templates[i]
		}
	}
	return nil
}

// above is whether s is a plain number above zero and below hi, exactly.
func above(s string, hi *apd.Decimal) bool {
	if !decimalText.MatchString(s) {
		return false
	}
	var d apd.Decimal
	if _, _, err := d.SetString(s); err != nil {
		return false
	}
	return d.Sign() > 0 && d.Cmp(hi) < 0
}

var (
	hundred  = apd.New(100, 0)
	capLimit = apd.New(1, 15) // a valuation cap below S$1,000,000,000,000,000: NUMERIC(20,4)'s integer digits, with room
)

// CheckInstrument is what is wrong with an instrument, every problem at once (ENG-22): the class's account is checked
// against the chart in s, and the amount and cap are in the base currency.
func CheckInstrument(i Instrument, s ledger.Settings) ledger.Problems {
	var probs ledger.Problems
	add := func(field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: CodeInstrument, Field: field, Message: msg, Fix: fix})
	}
	if !slices.Contains(Forms, i.Form) {
		add("form", fmt.Sprintf("%q is not a form of SAFE or note", i.Form), "Choose post_money_safe, pre_money_safe, discount_safe, mfn_safe or convertible_note")
	}
	if i.HolderID == uuid.Nil {
		add("holder_id", "the holder is missing", "Choose the investor who holds it")
	}
	if i.Amount.Currency() != s.Base || i.Amount.Sign() <= 0 {
		add("amount", "the amount is not above zero in the base currency", "Enter what the investor paid, such as 125000.00")
	}
	if i.SignedOn.IsZero() {
		add("signed_on", "the day it was signed is missing", "Enter the day both sides signed it")
	}
	capped := i.Form == FormPostMoneySAFE || i.Form == FormPreMoneySAFE
	switch {
	case capped && i.ValuationCap == "":
		add("valuation_cap", "a SAFE with a cap needs its valuation cap", "Enter the cap, such as 2500000")
	case i.ValuationCap != "" && !above(i.ValuationCap, capLimit):
		add("valuation_cap", fmt.Sprintf("%q is not an amount above zero", i.ValuationCap), "Enter the cap as a plain number, such as 2500000")
	}
	switch {
	case i.Form == FormDiscountSAFE && i.Discount == "":
		add("discount", "a discount SAFE needs its discount", "Enter the percentage off the round price, such as 20")
	case i.Discount != "" && !above(i.Discount, hundred):
		add("discount", fmt.Sprintf("%q is not a percentage between 0 and 100", i.Discount), "Enter it as a plain number, such as 20")
	}
	if i.Form == FormMFNSAFE && !i.MFN {
		add("mfn", "an MFN SAFE takes the most-favoured-nation clause", "Tick the clause, or choose another form")
	}
	if i.Form == FormConvertibleNote {
		if i.InterestRate == "" || (i.InterestRate != "0" && !above(i.InterestRate, hundred)) {
			add("interest_rate", "a note needs its interest rate, a percentage a year from 0", "Enter it as a plain number, such as 8")
		}
		if i.MaturityOn == nil {
			add("maturity_on", "a note needs its maturity", "Enter the day it falls due")
		}
	} else if i.InterestRate != "" || i.InterestConverts {
		add("interest_rate", "only a convertible note bears interest", "Leave the interest out, or choose convertible_note")
	}
	if i.MaturityOn != nil && !i.SignedOn.IsZero() && !i.MaturityOn.After(i.SignedOn) {
		add("maturity_on", "the maturity is not after the day it was signed", "Enter the day it falls due")
	}
	if !slices.Contains(Triggers, i.Trigger) {
		add("trigger", fmt.Sprintf("%q is not what triggers a conversion", i.Trigger), "Choose priced_round, maturity, liquidity_event or other")
	}
	if a, ok := s.Accounts[i.ClassAccountID]; !ok || !a.ShareCapital || a.Group || !a.Active {
		add("class_account_id", "the class it converts into is not an active share-capital account", "Choose 3010 Ordinary or 3020 Preference share capital")
	}
	if !slices.Contains(Denominators, i.Denominator) {
		add("denominator", fmt.Sprintf("%q is not how the terms count the shares", i.Denominator),
			"Choose post_money_capitalisation, pre_money_shares, or as_terms with a note")
	} else if i.Denominator == "as_terms" && strings.TrimSpace(i.DenominatorNote) == "" {
		add("denominator_note", "the terms' count of the shares is not described", "Quote how the terms count the shares before the round")
	}
	if utf8.RuneCountInString(i.DenominatorNote) > MaxNote {
		add("denominator_note", fmt.Sprintf("the note is longer than %d characters", MaxNote), "Shorten it")
	}
	if i.PriceDecimals < 0 || i.PriceDecimals > MaxPriceDecimals {
		add("price_decimals", fmt.Sprintf("the price's precision is outside 0 to %d decimals", MaxPriceDecimals), "Enter the decimals the terms give the conversion price, such as 4")
	}
	if !slices.Contains(Classifications, i.Classification) {
		add("classification", fmt.Sprintf("%q is not a classification", i.Classification), "Choose liability or equity, as the accountant decides, or leave it out")
	}
	if i.Template != "" {
		if t := TemplateOf(i.Template); t == nil {
			add("template", fmt.Sprintf("%q is not a template Fyniq knows", i.Template), "Leave it out, or choose yc-sg-post-money-cap@1")
		} else if t.Form != i.Form {
			add("template", fmt.Sprintf("the template is for a %s", strings.ReplaceAll(t.Form, "_", " ")), "Choose that form, or leave the template out")
		}
	}
	if utf8.RuneCountInString(i.Note) > MaxNote {
		add("note", fmt.Sprintf("the note is longer than %d characters", MaxNote), "Shorten it")
	}
	return probs
}

// EncodeInstrument is an instrument as a document version keeps it.
func EncodeInstrument(i Instrument) ([]byte, error) {
	p := instrumentPayload{Date: i.Date.Format(time.DateOnly), Form: i.Form, HolderID: idText(i.HolderID), Amount: i.Amount.String(),
		Currency: i.Amount.Currency().Code, SignedOn: dateText(i.SignedOn), ValuationCap: i.ValuationCap, Discount: i.Discount, MFN: i.MFN,
		InterestRate: i.InterestRate, Trigger: i.Trigger, ClassAccountID: idText(i.ClassAccountID), ClassID: optText(i.ClassID),
		Denominator: i.Denominator, DenominatorNote: i.DenominatorNote, InterestConverts: i.InterestConverts, PriceDecimals: i.PriceDecimals,
		Classification: i.Classification, Template: i.Template, ProRata: i.ProRata, Note: i.Note}
	if i.MaturityOn != nil {
		p.MaturityOn = i.MaturityOn.Format(time.DateOnly)
	}
	return json.Marshal(p)
}

func dateText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.DateOnly)
}

// ReadInstrument reads an instrument's version as it was saved.
func ReadInstrument(doc storage.Document) (Instrument, error) {
	var p instrumentPayload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return Instrument{}, err
	}
	cur, err := money.Lookup(p.Currency)
	if err != nil {
		return Instrument{}, err
	}
	amount, err := money.Parse(p.Amount, cur)
	if err != nil {
		return Instrument{}, err
	}
	out := Instrument{Form: p.Form, Amount: amount, ValuationCap: p.ValuationCap, Discount: p.Discount, MFN: p.MFN, InterestRate: p.InterestRate,
		Trigger: p.Trigger, Denominator: p.Denominator, DenominatorNote: p.DenominatorNote, InterestConverts: p.InterestConverts,
		PriceDecimals: p.PriceDecimals, Classification: p.Classification, Template: p.Template, ProRata: p.ProRata, Note: p.Note}
	if out.HolderID, err = uuid.Parse(p.HolderID); err != nil {
		return Instrument{}, err
	}
	if out.ClassAccountID, err = uuid.Parse(p.ClassAccountID); err != nil {
		return Instrument{}, err
	}
	if p.ClassID != "" {
		id, err := uuid.Parse(p.ClassID)
		if err != nil {
			return Instrument{}, err
		}
		out.ClassID = &id
	}
	if out.SignedOn, err = time.Parse(time.DateOnly, p.SignedOn); err != nil {
		return Instrument{}, err
	}
	if p.MaturityOn != "" {
		m, err := time.Parse(time.DateOnly, p.MaturityOn)
		if err != nil {
			return Instrument{}, err
		}
		out.MaturityOn = &m
	}
	if out.Date, err = time.Parse(time.DateOnly, p.Date); err != nil {
		return Instrument{}, err
	}
	return out, nil
}

// checkedInstrument checks an instrument against the chart and its holder against the contacts, every problem at once.
func checkedInstrument(ctx context.Context, u storage.UnitOfWork, i Instrument, s ledger.Settings) error {
	probs := CheckInstrument(i, s)
	if i.HolderID != uuid.Nil {
		if _, err := Holder(ctx, u, i.HolderID); err != nil {
			var lp ledger.Problems
			if !asProblems(err, &lp) {
				return err
			}
			for _, p := range lp {
				probs = append(probs, ledger.Problem{Code: CodeInstrument, Field: "holder_id", Message: p.Message, Fix: "Mark the contact an investor in Contacts first"})
			}
		}
	}
	if i.ClassID != nil {
		c, err := Class(ctx, u, *i.ClassID)
		if err != nil || c.Class.AccountID != i.ClassAccountID {
			probs = append(probs, ledger.Problem{Code: CodeInstrument, Field: "class_id", Message: "the class is not a recorded class of that account",
				Fix: "Choose one of the account's recorded classes, or leave it out for the account's first"})
		}
	}
	if len(probs) > 0 {
		return probs
	}
	return nil
}

func asProblems(err error, out *ledger.Problems) bool {
	p, ok := err.(ledger.Problems)
	if ok {
		*out = p
	}
	return ok
}

// DraftInstrumentIn saves an instrument to record in u: a draft, to attach the signed instrument to.
func DraftInstrumentIn(ctx context.Context, u storage.UnitOfWork, i Instrument, s ledger.Settings) (storage.Document, error) {
	if err := checkedInstrument(ctx, u, i, s); err != nil {
		return storage.Document{}, err
	}
	b, err := EncodeInstrument(i)
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentInstrument, Payload: b})
}

// ReviseInstrumentIn saves a new version of an instrument not recorded yet, in u; its holder stays.
func ReviseInstrumentIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, i Instrument, s ledger.Settings) (storage.Document, error) {
	doc, err := Drafted(ctx, u, storage.DocumentInstrument, id)
	if err != nil {
		return doc, err
	}
	was, err := ReadInstrument(doc)
	if err != nil {
		return doc, err
	}
	i.HolderID, i.Date = was.HolderID, was.Date
	if err := checkedInstrument(ctx, u, i, s); err != nil {
		return doc, err
	}
	b, err := EncodeInstrument(i)
	if err != nil {
		return doc, err
	}
	return u.Documents().Revise(ctx, id, b)
}

// Instruments are every instrument, drafts and recorded, oldest first.
func Instruments(ctx context.Context, u storage.UnitOfWork) ([]storage.Document, error) {
	return all(ctx, u, storage.DocumentInstrument)
}

type enablementPayload struct {
	Date     string `json:"date"`
	Template string `json:"template"`
	Note     string `json:"note,omitempty"`
}

// Enablement is a template's enablement: drafted, then recorded on another person's approval with its lawyer's
// confirmation attached.
type Enablement struct {
	Template string
	Note     string
	Date     time.Time
}

// ReadEnablement reads a template enablement's version.
func ReadEnablement(doc storage.Document) (Enablement, error) {
	var p enablementPayload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return Enablement{}, err
	}
	d, err := time.Parse(time.DateOnly, p.Date)
	return Enablement{Template: p.Template, Note: p.Note, Date: d}, err
}

// Enablements are every template enablement, drafts and recorded, by template.
func Enablements(ctx context.Context, u storage.UnitOfWork) (map[string][]storage.Document, error) {
	docs, err := all(ctx, u, storage.DocumentInstrumentTemplate)
	if err != nil {
		return nil, err
	}
	out := map[string][]storage.Document{}
	for _, d := range docs {
		e, err := ReadEnablement(d)
		if err != nil {
			return nil, err
		}
		out[e.Template] = append(out[e.Template], d)
	}
	return out, nil
}

// DraftEnablementIn drafts the enablement of a known template in u, or returns the draft waiting already; a template
// enabled already is EQUITY-15.
func DraftEnablementIn(ctx context.Context, u storage.UnitOfWork, key, note string, today time.Time) (storage.Document, error) {
	if TemplateOf(key) == nil {
		return storage.Document{}, &Error{CodeTemplate, fmt.Sprintf("%q is not a template Fyniq knows", key), "Choose yc-sg-post-money-cap@1"}
	}
	all, err := Enablements(ctx, u)
	if err != nil {
		return storage.Document{}, err
	}
	for _, d := range all[key] {
		if d.Status == storage.DocumentPosted {
			return storage.Document{}, &Error{CodeTemplate, "the template is enabled already", "Nothing to do: its instruments convert by themselves"}
		}
		if d.Status == storage.DocumentDraft {
			return d, nil
		}
	}
	if utf8.RuneCountInString(note) > MaxNote {
		return storage.Document{}, ledger.Problems{{Code: CodeTemplate, Field: "note", Message: fmt.Sprintf("the note is longer than %d characters", MaxNote), Fix: "Shorten it"}}
	}
	b, err := json.Marshal(enablementPayload{Date: today.Format(time.DateOnly), Template: key, Note: strings.TrimSpace(note)})
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentInstrumentTemplate, Payload: b})
}

// Enabled reports whether an organisation has recorded the enablement of a template.
func Enabled(ctx context.Context, u storage.UnitOfWork, key string) (bool, error) {
	all, err := Enablements(ctx, u)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(all[key], func(d storage.Document) bool { return d.Status == storage.DocumentPosted }), nil
}
