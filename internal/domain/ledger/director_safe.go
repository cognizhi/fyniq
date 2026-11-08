package ledger

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// RuleDirectorSAFE posts a director's balance turned into a SAFE or convertible note (FR-EQ-05, FR-EQ-18), a route of
// the director's settlement.
const RuleDirectorSAFE = "director_safe@1"

// DirectorSAFE is one approved settlement into a SAFE or note as the rule reads it: the day it posts (the organisation's
// today on approval), the person, the director's account the balance is on, 2700, the recorded instrument it becomes,
// and the amount in the base currency.
type DirectorSAFE struct {
	ID         uuid.UUID
	Version    int
	PostOn     time.Time
	Memo       string
	Party      uuid.UUID
	Account    uuid.UUID
	Holding    uuid.UUID
	Instrument uuid.UUID
	Amount     money.Money
}

// PostDirectorSAFE turns one approved settlement into a SAFE or note into its entry (director_safe@1): Dr the director's
// account (2500 or 2120) with the amount, naming the person · Cr 2700 Convertible notes / SAFE with the amount, naming the
// instrument, whose money it becomes. No cash moves. Every rule is checked first and every failure returned together
// (ENG-22).
func PostDirectorSAFE(w DirectorSAFE, s Settings) (Entry, error) {
	var probs Problems
	add := func(code, field, msg, fix string) { probs = append(probs, Problem{code, field, msg, fix}) }
	if w.PostOn.IsZero() {
		add(CodeNoDate, "date", "the settlement has no posting day", "Approve it again")
	}
	if len(w.Memo) > MaxMemo {
		add(CodeTooLong, "memo", fmt.Sprintf("the memo has %d characters; the most is %d", len(w.Memo), MaxMemo), "Shorten it")
	}
	owed, ok := s.Accounts[w.Account]
	switch {
	case !ok:
		add(CodeUnknownAccount, "account_id", "the account does not exist in this organisation", "Choose the director's account the amount is owed on")
	case !owed.DirectorAccount():
		add(CodeWaiver, "account_id", fmt.Sprintf("account %s is not a director's account", owed.Code), "Choose 2500 or 2120")
	default:
		if p := partyProblem(s, owed, w.Party, "contact_id"); p != nil {
			probs = append(probs, *p)
		}
	}
	holding, ok := s.Accounts[w.Holding]
	switch {
	case !ok || !holding.InstrumentAccount() || holding.Group || !holding.Active:
		add(CodeEquity, "holding", "the organisation has no active convertible instruments account", "Give 2700 Convertible notes / SAFE the role convertible-instruments")
	default:
		if p := instrumentProblem(s, holding, w.Instrument, "safe.instrument_id"); p != nil {
			probs = append(probs, *p)
		}
	}
	if w.Amount.Currency() != s.Base || w.Amount.Sign() <= 0 {
		add(CodeWaiver, "amount", fmt.Sprintf("the amount is %s %s", w.Amount, w.Amount.Currency()), fmt.Sprintf("Enter the amount in %s, more than zero", s.Base))
	}
	if len(probs) > 0 {
		return Entry{}, probs
	}
	y, m, d := w.PostOn.Date()
	base := money.Identity(s.Base)
	return Entry{
		PostingDate:     time.Date(y, m, d, 0, 0, 0, 0, time.UTC),
		Source:          Source{Type: SourceDirectorSettlement, ID: w.ID.String(), Version: w.Version},
		RuleVersion:     RuleDirectorSAFE,
		PostingIdentity: DirectorWaiverIdentity(w.ID, w.Version, RuleDirectorSAFE),
		Memo:            w.Memo,
		Lines: []Line{
			{AccountID: w.Account, Amount: w.Amount, Rate: base, Base: w.Amount, Description: w.Memo, Party: w.Party},
			{AccountID: w.Holding, Amount: w.Amount.Neg(), Rate: base, Base: w.Amount.Neg(), Description: w.Memo, Instrument: w.Instrument},
		},
	}, nil
}

// DirectorSAFERules are every rule version the engine has posted settlements into a SAFE or note with, by name.
var DirectorSAFERules = map[string]func(w DirectorSAFE, s Settings) (Entry, error){
	RuleDirectorSAFE: PostDirectorSAFE,
}

// ReplayDirectorSAFE re-runs the rule version that posted a settlement into a SAFE or note over the same version (ENG-06).
func ReplayDirectorSAFE(rule string, w DirectorSAFE, s Settings) (Entry, error) {
	f, ok := DirectorSAFERules[rule]
	if !ok {
		return Entry{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("the engine has no rule version %s", rule),
			"Restore the rule version: an old version is never removed while an entry names it (ENG-06)"}}
	}
	return f(w, s)
}
