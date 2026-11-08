// Package retention is the ledger's purge after retention (PRD-001 AUD-10, open question 38, the owner's decisions of
// rev 0.230 and rev 0.238; p1-audit-explorer design D7b): which financial year may go, and the carry-forward entries
// that replace its lines, so every balance from the boundary on is unchanged.
package retention

import (
	"fmt"
	"slices"
	"strings"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// RuleVersion names the carry-forward rule its entries record (journal.RuleCarryForward posts them).
const RuleVersion = "purge-carry-forward@1"

// RateSource is the FX rate source of a carried foreign-currency line.
const RateSource = "carry-forward"

// BoundarySource prefixes a carry-forward entry's posting identity; no entry but the purge's may claim it as a source.
const BoundarySource = "ledger_boundary"

// Account types and roles the rule reads (storage's names).
const (
	Revenue          = "revenue"
	Expense          = "expense"
	RetainedEarnings = "retained-earnings"
)

// Account is what the rule needs of an account.
type Account struct {
	Code string
	Type string // asset, liability, equity, revenue, expense
	Role string
}

// Source is where an entry comes from: a document's type and ID.
type Source struct {
	Type string
	ID   string
}

// Line is a line of an entry being purged: its entry's source, its account (by key), its dimension set ("" none), its
// person and instrument ("" none), its amount in its currency and its base amount.
type Line struct {
	Source       Source
	Account      string
	DimensionSet string
	Party        string
	Instrument   string
	Amount       money.Money
	Base         money.Money
}

// Input is a year's purged lines and what the rule needs to carry them.
type Input struct {
	Base             money.Currency
	Accounts         map[string]Account
	RetainedEarnings string // the retained-earnings account's key
	Lines            []Line
}

// CarriedLine is a line of a carry-forward entry. A base-only line has a foreign currency, amount 0 and a base amount.
type CarriedLine struct {
	Account      string
	DimensionSet string
	Party        string
	Instrument   string
	Amount       money.Money
	Rate         money.Rate
	Base         money.Money
}

// Entry is a carry-forward entry; the ledger dates it the day before the boundary, as every report from the boundary on
// reads it as its opening balance.
type Entry struct {
	Source Source
	Lines  []CarriedLine
}

// key is what a balance is carried by.
type key struct {
	source     Source // the line's source document
	account    string
	set        string
	currency   string
	party      string
	instrument string
}

type sum struct{ amount, base money.Money }

// IsProfitOrLoss says whether an account is revenue or expense, which year-end folds into retained earnings.
func IsProfitOrLoss(a Account) bool { return a.Type == Revenue || a.Type == Expense }

// CarryForward is the rule purge-carry-forward@1 (AUD-10): the entries that replace the purged lines, one per source
// document, under that document's source.
//   - A balance-sheet line carries by account, dimension set, currency, person and instrument, so every sub-ledger,
//     every reconciliation of an account by source and each director's account and SAFE still read their own (INV-07,
//     FR-EQ-01, FR-EQ-18, INV-13).
//   - A revenue or expense line carries into retained earnings, by dimension set, in the base currency (FR-GL-10).
//
// Each purged entry balances, so each document's carry-forward does (INV-01). A carried foreign-currency balance is one
// line at the rate its base and amount give, rounded to 10 decimals (1 when that is not a rate), plus a base-only line
// for any residual the FX rule cannot express (INV-05). A balance of zero in both is not carried. The entries come by
// source type and ID; their lines by account code, dimension set, currency, person and instrument.
func CarryForward(in Input) ([]Entry, error) {
	sums := map[key]*sum{}
	add := func(k key, amount, base money.Money) error {
		s, ok := sums[k]
		if !ok {
			s = &sum{money.Zero(amount.Currency()), money.Zero(in.Base)}
			sums[k] = s
		}
		var err error
		if s.amount, err = s.amount.Add(amount); err != nil {
			return err
		}
		s.base, err = s.base.Add(base)
		return err
	}
	re, ok := in.Accounts[in.RetainedEarnings]
	if !ok {
		return nil, fmt.Errorf("retention: no retained-earnings account %q", in.RetainedEarnings)
	}
	if IsProfitOrLoss(re) {
		return nil, fmt.Errorf("retention: the retained-earnings account %s is revenue or expense", re.Code)
	}
	for _, l := range in.Lines {
		a, ok := in.Accounts[l.Account]
		if !ok {
			return nil, fmt.Errorf("retention: a line of %s %s names no account %q", l.Source.Type, l.Source.ID, l.Account)
		}
		if l.Base.Currency() != in.Base {
			return nil, fmt.Errorf("retention: a line of %s %s has its base in %s", l.Source.Type, l.Source.ID, l.Base.Currency())
		}
		var err error
		if IsProfitOrLoss(a) {
			err = add(key{source: l.Source, account: in.RetainedEarnings, set: l.DimensionSet, currency: in.Base.Code}, l.Base, l.Base)
		} else {
			err = add(key{source: l.Source, account: l.Account, set: l.DimensionSet, currency: l.Amount.Currency().Code, party: l.Party,
				instrument: l.Instrument}, l.Amount, l.Base)
		}
		if err != nil {
			return nil, err
		}
	}
	keys := make([]key, 0, len(sums))
	for k, s := range sums {
		if !s.amount.IsZero() || !s.base.IsZero() {
			keys = append(keys, k)
		}
	}
	slices.SortFunc(keys, func(x, y key) int {
		return cmpAll(strings.Compare(x.source.Type, y.source.Type), strings.Compare(x.source.ID, y.source.ID),
			strings.Compare(in.Accounts[x.account].Code, in.Accounts[y.account].Code), strings.Compare(x.set, y.set),
			strings.Compare(x.currency, y.currency), strings.Compare(x.party, y.party), strings.Compare(x.instrument, y.instrument))
	})
	out := []Entry{}
	for _, k := range keys {
		lines, err := carried(in.Base, k, sums[k])
		if err != nil {
			return nil, err
		}
		if len(out) == 0 || out[len(out)-1].Source != k.source {
			out = append(out, Entry{Source: k.source})
		}
		e := &out[len(out)-1]
		e.Lines = append(e.Lines, lines...)
	}
	for _, e := range out {
		if err := balanced(in.Base, e); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// carried is a balance's lines: one at the rate its base and amount give, and a base-only line for any residual.
func carried(base money.Currency, k key, s *sum) ([]CarriedLine, error) {
	if s.amount.Currency() == base {
		if !s.amount.Equal(s.base) {
			return nil, fmt.Errorf("retention: %s in the base currency has an amount %s and a base %s", k.account, s.amount, s.base)
		}
		l := baseLine(base, k.account, k.set, s.base)
		l.Party, l.Instrument = k.party, k.instrument
		return []CarriedLine{l}, nil
	}
	if s.amount.IsZero() {
		return []CarriedLine{{Account: k.account, DimensionSet: k.set, Party: k.party, Instrument: k.instrument, Amount: s.amount,
			Rate: unitRate(s.amount.Currency(), base), Base: s.base}}, nil
	}
	rate, err := rateOf(s.amount, s.base)
	if err != nil {
		return nil, err
	}
	b1, err := rounding.FX(s.amount, rate)
	if err != nil {
		return nil, err
	}
	out := []CarriedLine{{Account: k.account, DimensionSet: k.set, Party: k.party, Instrument: k.instrument, Amount: s.amount, Rate: rate, Base: b1}}
	if residual, err := s.base.Sub(b1); err != nil {
		return nil, err
	} else if !residual.IsZero() {
		out = append(out, CarriedLine{Account: k.account, DimensionSet: k.set, Party: k.party, Instrument: k.instrument,
			Amount: money.Zero(s.amount.Currency()), Rate: unitRate(s.amount.Currency(), base), Base: residual})
	}
	return out, nil
}

// rateOf is base ÷ amount, rounded half away from zero to 10 decimals; 1 when that is not above zero or does not fit a
// rate's 10 integer digits (NUMERIC(20,10)).
func rateOf(amount, base money.Money) (money.Rate, error) {
	ctx := apd.BaseContext.WithPrecision(60)
	ctx.Rounding = apd.RoundHalfUp
	var q, r apd.Decimal
	if _, err := ctx.Quo(&q, base.Decimal(), amount.Decimal()); err != nil {
		return money.Rate{}, err
	}
	if _, err := ctx.Quantize(&r, &q, -10); err != nil {
		return money.Rate{}, err
	}
	if r.Sign() <= 0 || r.Cmp(apd.New(1, money.RateIntegerDigits)) >= 0 {
		return unitRate(amount.Currency(), base.Currency()), nil
	}
	if _, _, err := ctx.Reduce(&r, &r); err != nil {
		return money.Rate{}, err
	}
	return money.ParseRate(r.Text('f'), amount.Currency(), base.Currency(), RateSource)
}

func unitRate(from, to money.Currency) money.Rate {
	r, _ := money.ParseRate("1", from, to, RateSource) // "1" is always a rate
	return r
}

// baseLine is a line in the base currency.
func baseLine(base money.Currency, account, set string, amount money.Money) CarriedLine {
	return CarriedLine{Account: account, DimensionSet: set, Amount: amount, Rate: unitRate(base, base), Base: amount}
}

// balanced checks an entry balances in the base currency (INV-01).
func balanced(base money.Currency, e Entry) error {
	total := money.Zero(base)
	for _, l := range e.Lines {
		var err error
		if total, err = total.Add(l.Base); err != nil {
			return err
		}
	}
	if !total.IsZero() {
		return fmt.Errorf("retention: the carry-forward of %s %s does not balance by %s: the purged lines did not balance", e.Source.Type, e.Source.ID, total)
	}
	return nil
}

func cmpAll(cs ...int) int {
	for _, c := range cs {
		if c != 0 {
			return c
		}
	}
	return 0
}
