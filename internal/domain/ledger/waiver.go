package ledger

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// The source type and rule version of a director's settlement (FR-EQ-05, FR-EQ-07; p1-founders-money design D10). Only a
// waiver posts under it; a loan kept posts nothing, and a repayment posts as a payment of the batch that released it.
const (
	SourceDirectorSettlement = "director_settlement"
	RuleDirectorWaiver       = "director_settlement@1"
)

// CodeWaiver is a waiver that does not fit: an account it may not debit or credit, or an amount that is not more than
// zero in the base currency.
const CodeWaiver = "LEDGER-44"

// DirectorWaiver is one approved waiver as the rule reads it: the day it posts (the organisation's today on approval),
// the person, the director's account the amount is waived on, the account it credits (3300 Capital contribution, or an
// income account), and the amount in the base currency.
type DirectorWaiver struct {
	ID      uuid.UUID
	Version int
	PostOn  time.Time
	Memo    string
	Party   uuid.UUID
	Account uuid.UUID
	Credit  uuid.UUID
	Amount  money.Money
}

// DirectorWaiverIdentity is the posting identity of a settlement's version under a rule version (§11.4).
func DirectorWaiverIdentity(id uuid.UUID, version int, rule string) string {
	return identity(SourceDirectorSettlement, id, version, rule)
}

// PostDirectorWaiver turns one approved waiver into its entry (director_settlement@1): Dr the director's account (2500
// or 2120) with the amount, naming the person (B.7: waived) · Cr the account the accountant chose with the amount (FR-EQ-07).
// Every rule is checked first and every failure returned together (ENG-22).
func PostDirectorWaiver(w DirectorWaiver, s Settings) (Entry, error) {
	var probs Problems
	add := func(code, field, msg, fix string) { probs = append(probs, Problem{code, field, msg, fix}) }
	if w.PostOn.IsZero() {
		add(CodeNoDate, "date", "the waiver has no posting day", "Approve it again")
	}
	if len(w.Memo) > MaxMemo {
		add(CodeTooLong, "reason", fmt.Sprintf("the memo has %d characters; the most is %d", len(w.Memo), MaxMemo), "Shorten the reason")
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
	credit, ok := s.Accounts[w.Credit]
	switch {
	case !ok:
		add(CodeUnknownAccount, "credit_account_id", "the account a waiver credits does not exist in this organisation", "Choose 3300 Capital contribution, or an income account")
	case credit.Group || !credit.Active || credit.DirectorAccount() || credit.Money || credit.ShareCapital || credit.Role == RoleGSTControl:
		add(CodeWaiver, "credit_account_id", fmt.Sprintf("account %s is not one a waiver credits", credit.Code), "Choose 3300 Capital contribution, or an income account")
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
		RuleVersion:     RuleDirectorWaiver,
		PostingIdentity: DirectorWaiverIdentity(w.ID, w.Version, RuleDirectorWaiver),
		Memo:            w.Memo,
		Lines: []Line{
			{AccountID: w.Account, Amount: w.Amount, Rate: base, Base: w.Amount, Description: w.Memo, Party: w.Party},
			{AccountID: w.Credit, Amount: w.Amount.Neg(), Rate: base, Base: w.Amount.Neg(), Description: w.Memo},
		},
	}, nil
}

// DirectorWaiverRules are every rule version the engine has posted waivers with, by name.
var DirectorWaiverRules = map[string]func(w DirectorWaiver, s Settings) (Entry, error){
	RuleDirectorWaiver: PostDirectorWaiver,
}

// ReplayDirectorWaiver re-runs the rule version that posted a waiver's entry over the same version (ENG-06).
func ReplayDirectorWaiver(rule string, w DirectorWaiver, s Settings) (Entry, error) {
	f, ok := DirectorWaiverRules[rule]
	if !ok {
		return Entry{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("the engine has no rule version %s", rule),
			"Restore the rule version: an old version is never removed while an entry names it (ENG-06)"}}
	}
	return f(w, s)
}
