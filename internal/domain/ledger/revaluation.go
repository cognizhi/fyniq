package ledger

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// The month-end FX revaluation's source type and rule version (FR-GL-04, posting rule "FX revaluation (month end)";
// PRD-001 rev 0.72). Its entry and the entry's reversal both carry the source, which is the one the database lets
// write base-only lines (INV-05).
const (
	SourceFXRevaluation = "fx_revaluation"
	RuleFXRevaluation   = "fx_revaluation@1"
)

// The codes of the revaluation's own refusals.
const (
	CodeNotRevalued = "LEDGER-19" // an item's account is not marked for revaluation, or the unrealised-FX account is missing
	CodeItem        = "LEDGER-20" // an item's currencies or rate do not fit
)

// RevaluationItem is one account's balance in one foreign currency at a period's end, as the balance service gave it,
// or one open document of a control account's sub-ledger in it (rev 0.92), and the period-end rate it is revalued at.
type RevaluationItem struct {
	AccountID uuid.UUID
	Open      money.Money // in the foreign currency
	Carrying  money.Money // its base value in the books, in the base currency
	Rate      money.Rate  // the foreign currency into the base, with its source (ENG-40)
	Of        string      // the document it is, such as BILL-0007; empty for an account's balance
}

// Change is the item's unrealised gain (positive, a debit) or loss: RND-FX(open × rate) − carrying (FR-GL-04).
func (it RevaluationItem) Change() (money.Money, error) {
	target, err := rounding.FX(it.Open, it.Rate)
	if err != nil {
		return money.Money{}, err
	}
	return target.Sub(it.Carrying)
}

// Revaluation is one run of a period's revaluation, as its document version records it.
type Revaluation struct {
	ID         uuid.UUID // its document
	Version    int
	Date       time.Time // the period's last day
	Items      []RevaluationItem
	Unrealised uuid.UUID // the account with the unrealised-fx role
}

// RevaluationIdentity is the posting identity of a revaluation's version under a rule version.
func RevaluationIdentity(id uuid.UUID, version int, rule string) string {
	return identity(SourceFXRevaluation, id, version, rule)
}

// Revalue is the posting rule "FX revaluation (month end)" (FR-GL-04, INV-05): for each item whose change is not zero,
// a base-only line on its account (amount 0 in its currency, at its rate, the change as its base amount, naming the
// document an item of a control account's sub-ledger is: rev 0.92, no earlier item named one), and against
// them the unrealised-FX account: one line for the losses (a debit) and one for the gains (a credit). Items without a
// change post nothing, and a run with none has no lines: there is nothing to post.
func Revalue(r Revaluation, s Settings) (Entry, error) {
	var problems Problems
	add := func(code, field, msg, fix string) { problems = append(problems, Problem{code, field, msg, fix}) }
	if r.Date.IsZero() {
		add(CodeNoDate, "date", "the revaluation has no date", "Date it on the period's last day")
	}
	if a, ok := s.Accounts[r.Unrealised]; !ok || !a.UnrealisedFX {
		add(CodeNotRevalued, "unrealised_account_id", "no account has the unrealised-fx role", "Give an income account the unrealised-fx role, as 7040 has in the standard chart")
	}
	out := Entry{
		PostingDate:     time.Date(r.Date.Year(), r.Date.Month(), r.Date.Day(), 0, 0, 0, 0, time.UTC),
		Source:          Source{Type: SourceFXRevaluation, ID: r.ID.String(), Version: r.Version},
		RuleVersion:     RuleFXRevaluation,
		PostingIdentity: RevaluationIdentity(r.ID, r.Version, RuleFXRevaluation),
		Memo:            "FX revaluation at " + r.Date.Format("2 Jan 2006"),
	}
	gains, losses := money.Zero(s.Base), money.Zero(s.Base)
	for i, it := range r.Items {
		f := func(name string) string { return fmt.Sprintf("items[%d].%s", i+1, name) }
		cur := it.Open.Currency()
		switch a, ok := s.Accounts[it.AccountID]; {
		case !ok:
			add(CodeUnknownAccount, f("account_id"), "the account is not in the chart", "Revalue the organisation's own accounts")
			continue
		case !a.Revalue:
			add(CodeNotRevalued, f("account_id"), fmt.Sprintf("account %s is not marked for revaluation", a.Code), "Mark it in the chart, or leave it out")
			continue
		}
		if cur == s.Base || it.Carrying.Currency() != s.Base || it.Rate.From != cur || it.Rate.To != s.Base {
			add(CodeItem, f("rate"), fmt.Sprintf("the item is in %s, carried in %s, at a rate from %s into %s", cur, it.Carrying.Currency(), it.Rate.From, it.Rate.To),
				fmt.Sprintf("Revalue a foreign currency, carried in %s, at a rate into %s", s.Base, s.Base))
			continue
		}
		change, err := it.Change()
		if err != nil {
			add(CodeBaseRange, f("open"), err.Error(), "Check the balance and the rate")
			continue
		}
		if change.IsZero() {
			continue
		}
		desc := fmt.Sprintf("%s %s revalued at %s", it.Open, cur, it.Rate)
		if it.Of != "" {
			desc = it.Of + ": " + desc
		}
		out.Lines = append(out.Lines, Line{AccountID: it.AccountID, Amount: money.Zero(cur), Rate: it.Rate, Base: change, Description: desc})
		if change.Sign() > 0 {
			gains, _ = gains.Add(change)
		} else {
			losses, _ = losses.Add(change)
		}
	}
	if len(problems) > 0 {
		return Entry{}, problems
	}
	base := money.Identity(s.Base)
	if !losses.IsZero() {
		out.Lines = append(out.Lines, Line{AccountID: r.Unrealised, Amount: losses.Neg(), Rate: base, Base: losses.Neg(), Description: "Unrealised FX loss"})
	}
	if !gains.IsZero() {
		out.Lines = append(out.Lines, Line{AccountID: r.Unrealised, Amount: gains.Neg(), Rate: base, Base: gains.Neg(), Description: "Unrealised FX gain"})
	}
	return out, nil
}

// RevaluationRule turns one version of a revaluation into its entry under one rule version.
type RevaluationRule func(r Revaluation, s Settings) (Entry, error)

// RevaluationRules are every revaluation rule version the engine has posted with, by name: like Rules, a version is
// never removed or changed while an entry names it.
var RevaluationRules = map[string]RevaluationRule{
	RuleFXRevaluation: Revalue,
}

// ReplayRevaluation re-runs the rule version that posted a revaluation's entry over the same version (ENG-06).
func ReplayRevaluation(rule string, r Revaluation, s Settings) (Entry, error) {
	f, ok := RevaluationRules[rule]
	if !ok {
		return Entry{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("the engine has no rule version %s", rule),
			"Restore the rule version: an old version is never removed while an entry names it (ENG-06)"}}
	}
	return f(r, s)
}
