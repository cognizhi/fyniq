package ledger

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// RoleConvertibleInstruments is 2700 Convertible notes / SAFE, keyed by instrument (FR-EQ-18, design D6): each line names
// the recorded SAFE or note it is for, so each one's balance reads from the ledger (ENG-04).
const RoleConvertibleInstruments = "convertible-instruments"

// CodeInstrument is a line on convertible instruments without its instrument, or an instrument on another line.
const CodeInstrument = "LEDGER-49"

// InstrumentAccount reports whether the account holds SAFEs and convertible notes, keyed by instrument.
func (a Account) InstrumentAccount() bool { return a.Role == RoleConvertibleInstruments }

// instrumentProblem is what is wrong with the instrument of a line to acc, if anything; field names the line's instrument:
// a line on convertible instruments names a recorded SAFE or note, and no other line one. A rule whose settings list no
// instruments (Settings.Instruments nil) checks only that it is there exactly when it must be; the database refuses the
// wrong one either way (FY292).
func instrumentProblem(s Settings, acc Account, instrument uuid.UUID, field string) *Problem {
	switch {
	case acc.InstrumentAccount() && instrument == uuid.Nil:
		return &Problem{CodeInstrument, field, fmt.Sprintf("account %s holds SAFEs and convertible notes, so the line names its instrument", acc.Code),
			"Choose the recorded SAFE or note the money is for"}
	case acc.InstrumentAccount() && s.Instruments != nil && !s.Instruments[instrument]:
		return &Problem{CodeInstrument, field, "the instrument is not a recorded SAFE or convertible note of this organisation",
			"Record the instrument first: another person approves it"}
	case !acc.InstrumentAccount() && instrument != uuid.Nil:
		return &Problem{CodeInstrument, field, fmt.Sprintf("account %s does not hold SAFEs or convertible notes, so the line names no instrument", acc.Code),
			"Leave the instrument out, or post to 2700 Convertible notes / SAFE"}
	}
	return nil
}

// RuleInstrumentConversion posts a SAFE's or note's conversion into shares (FR-EQ-18, design D6).
const RuleInstrumentConversion = "instrument_conversion@1"

const instrumentConversionVersion = 1

// InstrumentConversion is one SAFE or note converting into shares, as the rule reads it: the share issue it makes (its ID
// is the entry's source), the day ACRA's register shows the allotment, the instrument, 2700, the class's share-capital
// account and the amount converted, round(shares × the conversion price) under RND-SHARE.
type InstrumentConversion struct {
	ID         uuid.UUID
	PostOn     time.Time
	Memo       string
	Instrument uuid.UUID
	Holding    uuid.UUID
	Class      uuid.UUID
	Amount     money.Money
}

// InstrumentConversionIdentity is the posting identity of a conversion under a rule version (§11.4).
func InstrumentConversionIdentity(id uuid.UUID, rule string) string {
	return identity(SourceShareIssue, id, instrumentConversionVersion, rule)
}

// PostInstrumentConversion turns a conversion into its entry (instrument_conversion@1), on the day ACRA's register shows
// the allotment: Dr 2700 naming the instrument · Cr the class's share capital, both with the amount converted. What the
// conversion leaves of the instrument stays on 2700 until it is settled under its terms. Every rule is checked first and
// every failure returned together (ENG-22).
func PostInstrumentConversion(c InstrumentConversion, s Settings) (Entry, error) {
	var probs Problems
	add := func(code, field, msg, fix string) { probs = append(probs, Problem{code, field, msg, fix}) }
	if c.PostOn.IsZero() {
		add(CodeNoDate, "effective_on", "the conversion has no effective date", "Enter the day ACRA's register of members shows the allotment")
	}
	if len(c.Memo) > MaxMemo {
		add(CodeTooLong, "memo", fmt.Sprintf("the memo has %d characters; the most is %d", len(c.Memo), MaxMemo), "Shorten it")
	}
	holding, ok := s.Accounts[c.Holding]
	switch {
	case !ok || !holding.InstrumentAccount() || holding.Group || !holding.Active:
		add(CodeEquity, "holding", "the organisation has no active convertible instruments account", "Give 2700 Convertible notes / SAFE the role convertible-instruments")
	default:
		if p := instrumentProblem(s, holding, c.Instrument, "instrument_id"); p != nil {
			probs = append(probs, *p)
		}
	}
	if class, ok := s.Accounts[c.Class]; !ok || !class.ShareCapital || class.Group || !class.Active {
		add(CodeEquity, "class_account_id", "the class is not an active share-capital account", "Choose 3010 Ordinary or 3020 Preference share capital")
	}
	if c.Amount.Currency() != s.Base || c.Amount.Sign() <= 0 {
		add(CodeEquity, "amount", fmt.Sprintf("the amount is %s %s", c.Amount, c.Amount.Currency()), fmt.Sprintf("Convert an amount in %s, more than zero", s.Base))
	}
	if len(probs) > 0 {
		return Entry{}, probs
	}
	base := money.Identity(s.Base)
	y, m, d := c.PostOn.Date()
	return Entry{
		PostingDate:     time.Date(y, m, d, 0, 0, 0, 0, time.UTC),
		Source:          Source{Type: SourceShareIssue, ID: c.ID.String(), Version: instrumentConversionVersion},
		RuleVersion:     RuleInstrumentConversion,
		PostingIdentity: InstrumentConversionIdentity(c.ID, RuleInstrumentConversion),
		Memo:            c.Memo,
		Lines: []Line{
			{AccountID: c.Holding, Amount: c.Amount, Rate: base, Base: c.Amount, Description: c.Memo, Instrument: c.Instrument},
			{AccountID: c.Class, Amount: c.Amount.Neg(), Rate: base, Base: c.Amount.Neg(), Description: c.Memo},
		},
	}, nil
}

// InstrumentConversionRules are every rule version the engine has posted conversions with, by name.
var InstrumentConversionRules = map[string]func(c InstrumentConversion, s Settings) (Entry, error){
	RuleInstrumentConversion: PostInstrumentConversion,
}

// ReplayInstrumentConversion re-runs the rule version that posted a conversion's entry over the same conversion (ENG-06).
func ReplayInstrumentConversion(rule string, c InstrumentConversion, s Settings) (Entry, error) {
	f, ok := InstrumentConversionRules[rule]
	if !ok {
		return Entry{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("the engine has no rule version %s", rule),
			"Restore the rule version: an old version is never removed while an entry names it (ENG-06)"}}
	}
	return f(c, s)
}
