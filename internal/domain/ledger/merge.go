package ledger

import (
	"cmp"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// The source type and rule version of an account merge (FR-COA-04, Appendix B.8; PRD-001 rev 0.174).
const (
	SourceAccountMerge = "account_merge"
	RuleAccountMerge   = "account_merge@1"
)

// CodeAccountMerge is a merge the rule cannot post: the same account twice, accounts of different types, or a system,
// money or group account.
const CodeAccountMerge = "LEDGER-47"

// MergeRateSource is the source of the rate a merge moves a foreign-currency amount at.
const MergeRateSource = "account_merge"

// MergeHolding is what the account merged holds in one currency and dimension set at the merge's date: the amount in
// the currency and its recorded base amount.
type MergeHolding struct {
	Amount money.Money
	Base   money.Money
	Values []uuid.UUID
}

// AccountMerge is the merge of one account into another as the rule reads it: its document, the date, the two
// accounts and what the one merged holds, each currency and dimension set apart.
type AccountMerge struct {
	ID       uuid.UUID
	Version  int
	Date     time.Time
	Memo     string
	From     uuid.UUID
	Into     uuid.UUID
	Holdings []MergeHolding
}

// AccountMergeIdentity is the posting identity of a merge's version under a rule version (§11.4).
func AccountMergeIdentity(id uuid.UUID, version int, rule string) string {
	return identity(SourceAccountMerge, id, version, rule)
}

// PostAccountMerge turns an approved merge into its entry (account_merge@1, Appendix B.8): for each holding, the account
// merged gives up the amount and the account it is merged into takes it, at the holding's recorded base value. A
// base-currency amount moves as it is; a foreign one moves at the rate its base implies under RND-RATE (1 when it
// implies none), and the base the rounding leaves moves on base-currency lines between the same two accounts. Each line
// keeps the holding's dimension values. An entry with no lines is a merge of an account holding nothing, which posts
// nothing. Every rule is checked first and every failure returned together (ENG-22).
func PostAccountMerge(m AccountMerge, s Settings) (Entry, error) {
	var probs Problems
	add := func(code, field, msg, fix string) { probs = append(probs, Problem{code, field, msg, fix}) }
	if m.Date.IsZero() {
		add(CodeNoDate, "date", "the merge has no date", "Enter the day the balance moves, in an open period")
	}
	if len(m.Memo) > MaxMemo {
		add(CodeTooLong, "memo", fmt.Sprintf("the memo has %d characters; the most is %d", len(m.Memo), MaxMemo), "Shorten it")
	}
	from, fromOK := s.Accounts[m.From]
	into, intoOK := s.Accounts[m.Into]
	for _, x := range []struct {
		acc   Account
		ok    bool
		field string
	}{{from, fromOK, "account_id"}, {into, intoOK, "into_account_id"}} {
		switch {
		case !x.ok:
			add(CodeUnknownAccount, x.field, "the account does not exist in this organisation", "Choose an account from the chart of accounts")
		case x.acc.Group:
			add(CodeAccountMerge, x.field, fmt.Sprintf("account %s is a group heading", x.acc.Code), "Merge an account under it")
		case x.acc.Role != "" || x.acc.ShareCapital:
			add(CodeAccountMerge, x.field, fmt.Sprintf("account %s is a system account", x.acc.Code), "Merge an account that has no system role")
		case x.acc.Money:
			add(CodeAccountMerge, x.field, fmt.Sprintf("account %s is a money account", x.acc.Code), "Move a money account's balance with a transfer")
		case !x.acc.Active:
			add(CodeInactiveAccount, x.field, fmt.Sprintf("account %s is inactive", x.acc.Code), "Reactivate it, or choose another account")
		}
	}
	switch {
	case m.From == m.Into:
		add(CodeAccountMerge, "into_account_id", "an account cannot be merged into itself", "Choose another account to merge it into")
	case fromOK && intoOK && from.Type != into.Type:
		add(CodeAccountMerge, "into_account_id", fmt.Sprintf("account %s is %s and account %s is %s", from.Code, from.Type, into.Code, into.Type),
			"Merge an account into one of the same type")
	}
	for i, h := range m.Holdings {
		f := func(name string) string { return fmt.Sprintf("holdings[%d].%s", i, name) }
		if h.Base.Currency() != s.Base {
			add(CodeAccountMerge, f("base"), fmt.Sprintf("the base amount is in %s, not the base currency %s", h.Base.Currency(), s.Base), "")
		}
		if c, err := h.Amount.Cmp(h.Base); h.Amount.Currency() == s.Base && (err != nil || c != 0) {
			add(CodeAccountMerge, f("base"), fmt.Sprintf("a holding in %s has a base amount of %s, not its amount %s", s.Base, h.Base, h.Amount), "")
		}
	}
	if len(probs) > 0 {
		return Entry{}, probs
	}

	holdings := slices.Clone(m.Holdings)
	slices.SortStableFunc(holdings, func(a, b MergeHolding) int {
		return cmp.Or(cmp.Compare(a.Amount.Currency().Code, b.Amount.Currency().Code), slices.CompareFunc(canonical(a.Values), canonical(b.Values),
			func(x, y uuid.UUID) int { return slices.Compare(x[:], y[:]) }))
	})
	identityRate := money.Identity(s.Base)
	var lines []Line
	move := func(amount money.Money, rate money.Rate, base money.Money, values []uuid.UUID) {
		values = canonical(values)
		lines = append(lines,
			Line{AccountID: m.From, Amount: amount.Neg(), Rate: rate, Base: base.Neg(), Description: m.Memo, Values: values},
			Line{AccountID: m.Into, Amount: amount, Rate: rate, Base: base, Description: m.Memo, Values: values})
	}
	for _, h := range holdings {
		if h.Amount.Currency() == s.Base {
			if !h.Amount.IsZero() {
				move(h.Amount, identityRate, h.Amount, h.Values)
			}
			continue
		}
		rest := h.Base
		if !h.Amount.IsZero() {
			rate := mergeRate(h.Amount, h.Base, s.Base)
			base, err := rounding.FX(h.Amount, rate)
			if err != nil {
				return Entry{}, Problems{{CodeAccountMerge, "holdings", fmt.Sprintf("%s %s does not convert: %v", h.Amount, h.Amount.Currency(), err),
					"Check the account's balance"}}
			}
			move(h.Amount, rate, base, h.Values)
			if rest, err = rest.Sub(base); err != nil {
				return Entry{}, err
			}
		}
		if !rest.IsZero() {
			move(rest, identityRate, rest, h.Values)
		}
	}
	y, mo, d := m.Date.Date()
	return Entry{
		PostingDate:     time.Date(y, mo, d, 0, 0, 0, 0, time.UTC),
		Source:          Source{Type: SourceAccountMerge, ID: m.ID.String(), Version: m.Version},
		RuleVersion:     RuleAccountMerge,
		PostingIdentity: AccountMergeIdentity(m.ID, m.Version, RuleAccountMerge),
		Memo:            m.Memo,
		Lines:           lines,
	}, nil
}

// mergeRate is the rate a foreign holding moves at: the one its base implies under RND-RATE, or 1 when the amount and
// its base differ in sign or imply no rate a stored rate can hold (Appendix B.8).
func mergeRate(amount, base money.Money, to money.Currency) money.Rate {
	if amount.Sign() == base.Sign() && !base.IsZero() {
		a, b := amount, base
		if amount.Sign() < 0 {
			a, b = amount.Neg(), base.Neg()
		}
		if r, err := rounding.Implied(b, a, MergeRateSource); err == nil {
			return r
		}
	}
	r, err := money.ParseRate("1", amount.Currency(), to, MergeRateSource)
	if err != nil {
		panic(err) // a rate of 1 always parses
	}
	return r
}

// AccountMergeRule turns one version of a merge into its entry under one rule version.
type AccountMergeRule func(m AccountMerge, s Settings) (Entry, error)

// AccountMergeRules are every rule version the engine has posted merges with, by name.
var AccountMergeRules = map[string]AccountMergeRule{
	RuleAccountMerge: PostAccountMerge,
}

// ReplayAccountMerge re-runs the rule version that posted a merge's entry over the same version (ENG-06).
func ReplayAccountMerge(rule string, m AccountMerge, s Settings) (Entry, error) {
	r, ok := AccountMergeRules[rule]
	if !ok {
		return Entry{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("the engine has no rule version %s", rule),
			"Restore the rule version: an old version is never removed while an entry names it (ENG-06)"}}
	}
	return r(m, s)
}
