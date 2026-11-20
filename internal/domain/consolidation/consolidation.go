// Package consolidation works out a group's consolidated figures (PRD-001 FR-ENT-06, RND-TRANSLATE; p3-multi-entity design D7 and D8): each entity's net monthly
// movements, already on the parent's chart, translated into the group currency, the translation reserve that balances each entity's translated trial balance to nothing,
// and the pair-by-pair elimination of mirror accounts. It only works out figures from what it is given; nothing is posted to any ledger, and nothing is plugged: what two
// entities do not agree on stays where it is and is listed as a difference. The independent twin is oracle/src/oracle/consolidation.py (QA-ENG-02).
//
// The method rests on SFRS(I) 1-21 and IAS 21 paragraphs 39 and 40 [verify] and the scope is wholly-owned entities [accountant to confirm]:
//   - an asset or liability account: its cumulative balance at the closing rate of the run's last month, translated once (section "balance");
//   - an equity account: each month's movement at that month's closing rate, summed (section "equity");
//   - a revenue or expense account: each month's movement at that month's average rate, summed over the run's months (section "result") and, for the months before
//     the run, kept apart (section "brought_forward");
//   - the translation reserve of an entity is the negative sum of all its translated lines, so its translated trial balance foots exactly.
package consolidation

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// Codes of the plain refusals.
const (
	CodeInput = "CONSOL-01"
	CodeRate  = "CONSOL-02"
)

// Error is a refusal with the field it names and how to fix it (ENG-23).
type Error struct{ Code, Message, Fix string }

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Fix) }

func refuse(code, fix, format string, a ...any) error {
	return &Error{Code: code, Message: fmt.Sprintf(format, a...), Fix: fix}
}

// The account types of the parent's chart.
const (
	Asset     = "asset"
	Liability = "liability"
	Equity    = "equity"
	Revenue   = "revenue"
	Expense   = "expense"
)

// The sections a translated line sits in.
const (
	SectionBalance        = "balance"
	SectionEquity         = "equity"
	SectionResult         = "result"
	SectionBroughtForward = "brought_forward"
)

var monthPattern = regexp.MustCompile(`^[0-9]{4}-(0[1-9]|1[0-2])$`)

// Line is the net movement of one account of an entity, mapped to the parent's chart, in one month, in the entity's currency, a debit positive and a credit negative.
// Counterparty is the entity an intercompany account holds a balance with, or empty.
type Line struct {
	Account      string
	Type         string
	Counterparty string
	Month        string // YYYY-MM
	Amount       money.Money
}

// Entity is one entity's figures: its currency and every month's lines from its books' start through the run's last month.
type Entity struct {
	ID       string
	Currency money.Currency
	Lines    []Line
}

// Rate is the group's translation rates for a currency in a month: group currency per one unit of the entity's currency (RND-RATE).
type Rate struct {
	Currency string
	Month    string
	Closing  money.Rate
	Average  money.Rate
}

// Pair says two group accounts mirror each other: A normally holds a debit (due from, an intercompany expense, an investment) and B a credit.
type Pair struct{ A, B string }

// Input is a run: the group currency, the months it reports (From through To), the rates, the entities and the elimination pairs.
type Input struct {
	Group    money.Currency
	From, To string
	Rates    []Rate
	Entities []Entity
	Pairs    []Pair
}

// Translated is a line in the group currency.
type Translated struct {
	Account      string
	Counterparty string
	Section      string
	Type         string
	Amount       money.Money
}

// EntityResult is an entity's translated lines (none of zero) and its translation reserve.
type EntityResult struct {
	ID      string
	Lines   []Translated
	Reserve money.Money
}

// Elimination is an amount taken off one entity's line.
type Elimination struct {
	Entity string
	Translated
}

// Difference is what two entities do not agree on for one pair: Debit is the amount of the debit entity's account A (counterparty the credit entity), Credit the credit entity's account B (a credit, negative),
// Matched what was eliminated from both, and Difference their sum (the debit side's excess when positive).
type Difference struct {
	DebitEntity, CreditEntity string
	A, B                      string
	Debit, Credit             money.Money
	Matched, Difference       money.Money
}

// Total is a consolidated account in a section, after the eliminations, across entities and counterparties (none of zero).
type Total struct {
	Account string
	Section string
	Amount  money.Money
}

// Result is a run's figures.
type Result struct {
	Entities     []EntityResult
	Eliminations []Elimination
	Differences  []Difference
	Consolidated []Total
	// Reserve is the sum of the entities' translation reserves.
	Reserve money.Money
}

func sectionOf(t string) string {
	switch t {
	case Asset, Liability:
		return SectionBalance
	case Equity:
		return SectionEquity
	}
	return SectionResult
}

type rates struct {
	by map[string]Rate
}

func (r rates) of(currency, month string) (Rate, error) {
	x, ok := r.by[currency+"|"+month]
	if !ok {
		return Rate{}, refuse(CodeRate, "Enter the closing and average rate for the month", "there is no translation rate for %s in %s", currency, month)
	}
	return x, nil
}

// Work translates and eliminates a run.
func Work(in Input) (Result, error) {
	zero := money.Zero(in.Group)
	if !monthPattern.MatchString(in.From) || !monthPattern.MatchString(in.To) || in.From > in.To {
		return Result{}, refuse(CodeInput, "Give the run's first and last month, first not after last", "the run's months are %q to %q", in.From, in.To)
	}
	rt := rates{by: map[string]Rate{}}
	for _, r := range in.Rates {
		if !monthPattern.MatchString(r.Month) {
			return Result{}, refuse(CodeInput, "Give a rate's month as YYYY-MM", "a rate has the month %q", r.Month)
		}
		if r.Closing.To != in.Group || r.Average.To != in.Group || r.Closing.From.Code != r.Currency || r.Average.From.Code != r.Currency {
			return Result{}, refuse(CodeRate, "Enter the rates as group currency per one unit of the entity's currency", "the rates of %s in %s are not %s per %s", r.Currency, r.Month, in.Group.Code, r.Currency)
		}
		rt.by[r.Currency+"|"+r.Month] = r
	}
	known := map[string]bool{}
	types := map[string]string{}
	for _, e := range in.Entities {
		if known[e.ID] || e.ID == "" {
			return Result{}, refuse(CodeInput, "Give each entity its own ID", "the entity %q is listed twice or has no ID", e.ID)
		}
		known[e.ID] = true
	}
	res := Result{Reserve: zero}
	byEntity := map[string]map[string]*Translated{}
	for _, e := range in.Entities {
		lines, reserve, err := translate(in, e, rt, known, types)
		if err != nil {
			return Result{}, err
		}
		er := EntityResult{ID: e.ID, Reserve: reserve}
		idx := map[string]*Translated{}
		for i := range lines {
			if lines[i].Amount.IsZero() {
				continue
			}
			er.Lines = append(er.Lines, lines[i])
		}
		for i := range er.Lines {
			l := &er.Lines[i]
			idx[l.Account+"|"+l.Counterparty+"|"+l.Section] = l
		}
		byEntity[e.ID] = idx
		res.Entities = append(res.Entities, er)
		var err2 error
		if res.Reserve, err2 = res.Reserve.Add(reserve); err2 != nil {
			return Result{}, err2
		}
	}
	// Eliminations, pair by pair and entity pair by entity pair, in the order the run lists them.
	for _, p := range in.Pairs {
		for _, a := range in.Entities {
			for _, b := range in.Entities {
				if a.ID == b.ID {
					continue
				}
				x, xl := pick(byEntity[a.ID], p.A, b.ID, zero)
				y, yl := pick(byEntity[b.ID], p.B, a.ID, zero)
				diff, err := x.Add(y)
				if err != nil {
					return Result{}, err
				}
				matched := zero
				if x.Sign() > 0 && y.Sign() < 0 {
					matched = x
					if c, _ := x.Cmp(y.Neg()); c > 0 {
						matched = y.Neg()
					}
				}
				if !matched.IsZero() {
					res.Eliminations = append(res.Eliminations,
						Elimination{Entity: a.ID, Translated: Translated{Account: p.A, Counterparty: b.ID, Section: xl.Section, Type: xl.Type, Amount: matched.Neg()}},
						Elimination{Entity: b.ID, Translated: Translated{Account: p.B, Counterparty: a.ID, Section: yl.Section, Type: yl.Type, Amount: matched}})
				}
				if !diff.IsZero() {
					res.Differences = append(res.Differences, Difference{DebitEntity: a.ID, CreditEntity: b.ID, A: p.A, B: p.B, Debit: x, Credit: y, Matched: matched, Difference: diff})
				}
			}
		}
	}
	// The consolidated figures: every entity's lines and the eliminations, by account and section.
	sums := map[string]money.Money{}
	add := func(account, section string, m money.Money) error {
		k := account + "|" + section
		cur, ok := sums[k]
		if !ok {
			cur = zero
		}
		v, err := cur.Add(m)
		sums[k] = v
		return err
	}
	for _, e := range res.Entities {
		for _, l := range e.Lines {
			if err := add(l.Account, l.Section, l.Amount); err != nil {
				return Result{}, err
			}
		}
	}
	for _, el := range res.Eliminations {
		if err := add(el.Account, el.Section, el.Amount); err != nil {
			return Result{}, err
		}
	}
	keys := make([]string, 0, len(sums))
	for k := range sums {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if sums[k].IsZero() {
			continue
		}
		account, section, _ := strings.Cut(k, "|")
		res.Consolidated = append(res.Consolidated, Total{Account: account, Section: section, Amount: sums[k]})
	}
	return res, nil
}

// pick is an entity's amount on an account with a counterparty, in the account's own section, and the line (a zero amount when it has none).
func pick(lines map[string]*Translated, account, counterparty string, zero money.Money) (money.Money, Translated) {
	for k, l := range lines {
		if l.Account == account && l.Counterparty == counterparty && l.Section == sectionOf(l.Type) {
			_ = k
			return l.Amount, *l
		}
	}
	return zero, Translated{Account: account, Counterparty: counterparty}
}

type key struct{ account, counterparty, section string }

func translate(in Input, e Entity, rt rates, entities map[string]bool, types map[string]string) ([]Translated, money.Money, error) {
	zero := money.Zero(in.Group)
	same := e.Currency == in.Group
	type acc struct {
		typ   string
		month map[string]money.Money // exact, by month, in the entity's currency
	}
	accs := map[key]*acc{}
	var order []key
	for _, l := range e.Lines {
		switch l.Type {
		case Asset, Liability, Equity, Revenue, Expense:
		default:
			return nil, zero, refuse(CodeInput, "Use asset, liability, equity, revenue or expense", "the account %s has the type %q", l.Account, l.Type)
		}
		if t, ok := types[l.Account]; ok && t != l.Type {
			return nil, zero, refuse(CodeInput, "Give an account of the group's chart one type", "the account %s is both %s and %s", l.Account, t, l.Type)
		}
		types[l.Account] = l.Type
		if l.Amount.Currency() != e.Currency {
			return nil, zero, refuse(CodeInput, "Give each entity's lines in its own currency", "a line of %s is in %s, not %s", e.ID, l.Amount.Currency().Code, e.Currency.Code)
		}
		if !monthPattern.MatchString(l.Month) || l.Month > in.To {
			return nil, zero, refuse(CodeInput, "Give a line's month as YYYY-MM, no later than the run's last month", "the line of %s on %s has the month %q", e.ID, l.Account, l.Month)
		}
		if l.Counterparty != "" && (!entities[l.Counterparty] || l.Counterparty == e.ID) {
			return nil, zero, refuse(CodeInput, "Name another entity of the run as the counterparty", "the line of %s on %s names %q as the counterparty", e.ID, l.Account, l.Counterparty)
		}
		k := key{l.Account, l.Counterparty, ""}
		a, ok := accs[k]
		if !ok {
			a = &acc{typ: l.Type, month: map[string]money.Money{}}
			accs[k] = a
			order = append(order, k)
		}
		cur, ok := a.month[l.Month]
		if !ok {
			cur = money.Zero(e.Currency)
		}
		v, err := cur.Add(l.Amount)
		if err != nil {
			return nil, zero, err
		}
		a.month[l.Month] = v
	}
	conv := func(m money.Money, month string, closing bool) (money.Money, error) {
		if same {
			return m, nil
		}
		r, err := rt.of(e.Currency.Code, month)
		if err != nil {
			return zero, err
		}
		rate := r.Average
		if closing {
			rate = r.Closing
		}
		return rounding.Translate(m, rate)
	}
	var out []Translated
	total := zero
	emit := func(k key, typ, section string, m money.Money) error {
		out = append(out, Translated{Account: k.account, Counterparty: k.counterparty, Section: section, Type: typ, Amount: m})
		var err error
		total, err = total.Add(m)
		return err
	}
	for _, k := range order {
		a := accs[k]
		months := make([]string, 0, len(a.month))
		for m := range a.month {
			months = append(months, m)
		}
		sort.Strings(months)
		switch a.typ {
		case Asset, Liability:
			sum := money.Zero(e.Currency)
			for _, m := range months {
				var err error
				if sum, err = sum.Add(a.month[m]); err != nil {
					return nil, zero, err
				}
			}
			t, err := conv(sum, in.To, true)
			if err != nil {
				return nil, zero, err
			}
			if err := emit(k, a.typ, SectionBalance, t); err != nil {
				return nil, zero, err
			}
		case Equity:
			sum := zero
			for _, m := range months {
				t, err := conv(a.month[m], m, true)
				if err != nil {
					return nil, zero, err
				}
				if sum, err = sum.Add(t); err != nil {
					return nil, zero, err
				}
			}
			if err := emit(k, a.typ, SectionEquity, sum); err != nil {
				return nil, zero, err
			}
		default:
			inRange, before := zero, zero
			for _, m := range months {
				t, err := conv(a.month[m], m, false)
				if err != nil {
					return nil, zero, err
				}
				if m < in.From {
					before, err = before.Add(t)
				} else {
					inRange, err = inRange.Add(t)
				}
				if err != nil {
					return nil, zero, err
				}
			}
			if err := emit(k, a.typ, SectionResult, inRange); err != nil {
				return nil, zero, err
			}
			if err := emit(k, a.typ, SectionBroughtForward, before); err != nil {
				return nil, zero, err
			}
		}
	}
	return out, total.Neg(), nil
}
