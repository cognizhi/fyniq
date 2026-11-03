package ledger

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// The source type and rule version of a director's repayment (FR-EQ-05, FR-BNK-12; p1-founders-money design D10): what
// a payment batch's release pays a director or shareholder of the settlements approved to be repaid.
const (
	SourceDirectorRepayment = "director_repayment"
	RuleDirectorRepayment   = "director_repayment@1"
)

// CodeRepayment is a repayment that does not fit: a settlement on an account that is not a director's account, or an
// amount that is not more than zero in the base currency.
const CodeRepayment = "LEDGER-45"

// RepaidSettlement is what a repayment pays of one settlement: the director's account it is owed on, and the amount in
// the base currency.
type RepaidSettlement struct {
	SettlementID uuid.UUID
	Account      uuid.UUID
	Amount       money.Money
}

// DirectorRepayment is one version of a repayment to a director or shareholder as the rule reads it: the day the bank
// pays it (the batch's payment date), the person, the money account it leaves, and what it pays of each settlement.
type DirectorRepayment struct {
	ID          uuid.UUID
	Version     int
	Date        time.Time
	Memo        string
	Party       uuid.UUID
	Account     uuid.UUID
	Settlements []RepaidSettlement
}

// DirectorRepaymentIdentity is the posting identity of a repayment's version under a rule version (§11.4).
func DirectorRepaymentIdentity(id uuid.UUID, version int, rule string) string {
	return identity(SourceDirectorRepayment, id, version, rule)
}

// PostDirectorRepayment turns one version of a repayment into its entry (director_repayment@1) and the settlements it
// records: Dr each settlement's director's account (2500 or 2120) with what it pays, naming the person (B.7: paid out) ·
// Cr the money account with the total, in the base currency (FR-EQ-05). Every rule is checked first and every failure
// returned together (ENG-22).
func PostDirectorRepayment(r DirectorRepayment, s Settings) (Entry, []Settlement, error) {
	var probs Problems
	add := func(code, field, msg, fix string) { probs = append(probs, Problem{code, field, msg, fix}) }
	if r.Date.IsZero() {
		add(CodeNoDate, "date", "the repayment has no date", "Release it from a batch with a payment date")
	}
	if len(r.Memo) > MaxMemo {
		add(CodeTooLong, "memo", fmt.Sprintf("the memo has %d characters; the most is %d", len(r.Memo), MaxMemo), "Shorten the reference")
	}
	if len(r.Settlements) == 0 {
		add(CodeTooFewLines, "settlements", "the repayment pays no settlement", "Choose the settlements it repays")
	}
	acc, ok := s.Accounts[r.Account]
	switch {
	case !ok:
		add(CodeUnknownAccount, "money_account", "the account does not exist in this organisation", "Choose the bank account it is paid from")
	case acc.Group || !acc.Active || !acc.Money:
		add(CodeNotMoneyAccount, "money_account", fmt.Sprintf("account %s is not an active money account", acc.Code), "Choose the bank account it is paid from")
	case acc.Currency != "" && acc.Currency != s.Base.Code:
		add(CodePaymentAmount, "money_account", fmt.Sprintf("account %s holds %s, but what the company owes its directors is in %s", acc.Code, acc.Currency, s.Base),
			fmt.Sprintf("Repay it from an account in %s", s.Base))
	}
	total := money.Zero(s.Base)
	var lines []Line
	var out []Settlement
	for i, x := range r.Settlements {
		field := fmt.Sprintf("settlements[%d]", i+1)
		owed, ok := s.Accounts[x.Account]
		switch {
		case !ok:
			add(CodeUnknownAccount, field+".account_id", "the account does not exist in this organisation", "Repay a settlement on 2500 or 2120")
			continue
		case !owed.DirectorAccount():
			add(CodeRepayment, field+".account_id", fmt.Sprintf("account %s is not a director's account", owed.Code), "Repay a settlement on 2500 or 2120")
			continue
		}
		if p := partyProblem(s, owed, r.Party, "contact_id"); p != nil {
			probs = append(probs, *p)
			continue
		}
		if x.Amount.Currency() != s.Base || x.Amount.Sign() <= 0 {
			add(CodeRepayment, field+".amount", fmt.Sprintf("it repays %s %s of the settlement", x.Amount, x.Amount.Currency()),
				fmt.Sprintf("Repay it in %s, more than zero", s.Base))
			continue
		}
		total, _ = total.Add(x.Amount)
		lines = append(lines, Line{AccountID: x.Account, Amount: x.Amount, Rate: money.Identity(s.Base), Base: x.Amount, Description: r.Memo, Party: r.Party})
		out = append(out, Settlement{DocumentID: x.SettlementID, Amount: x.Amount, Base: x.Amount, RealisedFX: money.Zero(s.Base), Tolerance: money.Zero(s.Base)})
	}
	if len(probs) > 0 {
		return Entry{}, nil, probs
	}
	lines = append(lines, Line{AccountID: r.Account, Amount: total.Neg(), Rate: money.Identity(s.Base), Base: total.Neg(), Description: r.Memo})
	y, m, d := r.Date.Date()
	return Entry{
		PostingDate:     time.Date(y, m, d, 0, 0, 0, 0, time.UTC),
		Source:          Source{Type: SourceDirectorRepayment, ID: r.ID.String(), Version: r.Version},
		RuleVersion:     RuleDirectorRepayment,
		PostingIdentity: DirectorRepaymentIdentity(r.ID, r.Version, RuleDirectorRepayment),
		Memo:            r.Memo,
		Lines:           lines,
	}, out, nil
}

// DirectorRepaymentRules are every rule version the engine has posted repayments with, by name.
var DirectorRepaymentRules = map[string]func(r DirectorRepayment, s Settings) (Entry, []Settlement, error){
	RuleDirectorRepayment: PostDirectorRepayment,
}

// ReplayDirectorRepayment re-runs the rule version that posted a repayment's entry over the same version (ENG-06).
func ReplayDirectorRepayment(rule string, r DirectorRepayment, s Settings) (Entry, error) {
	f, ok := DirectorRepaymentRules[rule]
	if !ok {
		return Entry{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("the engine has no rule version %s", rule),
			"Restore the rule version: an old version is never removed while an entry names it (ENG-06)"}}
	}
	e, _, err := f(r, s)
	return e, err
}
