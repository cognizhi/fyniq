package ledger

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// The source type and rule version of a director's fee (FR-EQ-04, §10.2; p1-founders-money design D8).
const (
	SourceDirectorFee = "director_fee"
	RuleDirectorFee   = "director_fee@1"
)

// CodeDirectorFee is a director's fee that does not fit: an account it may not post to, or an amount that is not more
// than zero in the base currency.
const CodeDirectorFee = "LEDGER-43"

// DirectorFee is one approved version of a director's fee as the rule reads it: the day the person became entitled to
// it (the general meeting that voted it), the director, the account it is a cost on (6050 Directors' fees by default)
// and the amount in the base currency.
type DirectorFee struct {
	ID         uuid.UUID
	Version    int
	EntitledOn time.Time
	Memo       string
	Party      uuid.UUID
	Account    uuid.UUID
	Amount     money.Money
}

// DirectorFeeIdentity is the posting identity of a fee's version under a rule version (§11.4).
func DirectorFeeIdentity(id uuid.UUID, version int, rule string) string {
	return identity(SourceDirectorFee, id, version, rule)
}

// PostDirectorFee turns one approved version of a director's fee into its entry (director_fee@1), on the day the person
// became entitled to it: Dr the fee's account (6050 Directors' fees) with the amount · Cr 2120 Deferred remuneration
// payable with the amount, naming the director (B.7: a fee). Paying it later debits 2120. Every rule is checked first and
// every failure returned together (ENG-22).
func PostDirectorFee(f DirectorFee, s Settings) (Entry, error) {
	var probs Problems
	add := func(code, field, msg, fix string) { probs = append(probs, Problem{code, field, msg, fix}) }
	if f.EntitledOn.IsZero() {
		add(CodeNoDate, "entitled_on", "the fee has no day the director became entitled to it", "Enter the day of the general meeting that voted it")
	}
	if len(f.Memo) > MaxMemo {
		add(CodeTooLong, "memo", fmt.Sprintf("the memo has %d characters; the most is %d", len(f.Memo), MaxMemo), "Shorten it")
	}
	acc, ok := s.Accounts[f.Account]
	switch {
	case !ok:
		add(CodeUnknownAccount, "account_id", "the account does not exist in this organisation", "Choose 6050 Directors' fees, or another cost account")
	case acc.Group || !acc.Active:
		add(CodeDirectorFee, "account_id", fmt.Sprintf("account %s is not an active, postable account", acc.Code), "Choose an active account that is not a group")
	case acc.ShareCapital || acc.Money || acc.DirectorAccount() || acc.Role == RoleGSTControl:
		add(CodeDirectorFee, "account_id", fmt.Sprintf("account %s is share capital, a money account, a director's account or GST control", acc.Code),
			"Choose 6050 Directors' fees, or another cost account")
	}
	owed, hasOwed := s.byRole(RoleDeferredRemuneration)
	if !hasOwed {
		add(CodeSystemRole, "contact_id", "the chart has no deferred remuneration payable account",
			"Load the standard chart's 2120 Deferred remuneration payable, or give a liability account the role")
	} else if p := partyProblem(s, owed, f.Party, "contact_id"); p != nil {
		probs = append(probs, *p)
	}
	if f.Amount.Currency() != s.Base || f.Amount.Sign() <= 0 {
		add(CodeDirectorFee, "amount", fmt.Sprintf("the amount is %s %s", f.Amount, f.Amount.Currency()), fmt.Sprintf("Enter the fee in %s, more than zero", s.Base))
	}
	if len(probs) > 0 {
		return Entry{}, probs
	}
	y, m, d := f.EntitledOn.Date()
	base := money.Identity(s.Base)
	return Entry{
		PostingDate:     time.Date(y, m, d, 0, 0, 0, 0, time.UTC),
		Source:          Source{Type: SourceDirectorFee, ID: f.ID.String(), Version: f.Version},
		RuleVersion:     RuleDirectorFee,
		PostingIdentity: DirectorFeeIdentity(f.ID, f.Version, RuleDirectorFee),
		Memo:            f.Memo,
		Lines: []Line{
			{AccountID: f.Account, Amount: f.Amount, Rate: base, Base: f.Amount, Description: f.Memo},
			{AccountID: owed.ID, Amount: f.Amount.Neg(), Rate: base, Base: f.Amount.Neg(), Description: f.Memo, Party: f.Party},
		},
	}, nil
}

// DirectorFeeRule turns one version of a fee into its entry under one rule version.
type DirectorFeeRule func(f DirectorFee, s Settings) (Entry, error)

// DirectorFeeRules are every rule version the engine has posted directors' fees with, by name.
var DirectorFeeRules = map[string]DirectorFeeRule{
	RuleDirectorFee: PostDirectorFee,
}

// ReplayDirectorFee re-runs the rule version that posted a fee's entry over the same version (ENG-06).
func ReplayDirectorFee(rule string, f DirectorFee, s Settings) (Entry, error) {
	r, ok := DirectorFeeRules[rule]
	if !ok {
		return Entry{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("the engine has no rule version %s", rule),
			"Restore the rule version: an old version is never removed while an entry names it (ENG-06)"}}
	}
	return r(f, s)
}
