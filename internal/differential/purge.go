package differential

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/retention"
	schemas "github.com/cognizhi/fyniq/test/differential"
)

// Purges (README, "Purges"; p1-audit-explorer design D7b, AUD-10): a fyniq-purge@1 document is a ledger and a
// boundary; both sides keep what the purge keeps, carry the rest forward and write fyniq-purge-result@1, compared to the
// cent, with the purged ledger's positions from the boundary on, which must be the unpurged ledger's.

// The purge formats.
const (
	PurgeSchema       = "fyniq-purge@1"
	PurgeResultSchema = "fyniq-purge-result@1"
)

// Purge is a fyniq-purge@1 document.
type Purge struct {
	Schema           string         `json:"schema"`
	Name             string         `json:"name"`
	BaseCurrency     string         `json:"base_currency"`
	Currencies       map[string]int `json:"currencies"`
	FYStartMonth     int            `json:"fy_start_month"`
	Boundary         string         `json:"boundary"`
	RetainedEarnings string         `json:"retained_earnings"`
	Suspense         string         `json:"suspense"`
	Accounts         []struct {
		Code    string `json:"code"`
		Type    string `json:"type"`
		Role    string `json:"role"`
		Control bool   `json:"control"`
	} `json:"accounts"`
	Entries []struct {
		ID     string `json:"id"`
		Source struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		} `json:"source"`
		PostingDate string `json:"posting_date"`
		Reverses    string `json:"reverses"`
		Named       bool   `json:"named"`
		Lines       []struct {
			Account      string `json:"account"`
			DimensionSet string `json:"dimension_set"`
			Currency     string `json:"currency"`
			Party        string `json:"party"`
			Instrument   string `json:"instrument"`
			Amount       string `json:"amount"`
			BaseAmount   string `json:"base_amount"`
		} `json:"lines"`
	} `json:"entries"`

	book purgeBook
}

// purgeBook is a document read: its accounts, its boundary and its entries' lines as the rule takes them.
type purgeBook struct {
	base     money.Currency
	accounts map[string]retention.Account
	boundary time.Time
	refs     []retention.EntryRef
	named    map[string]bool
	lines    map[string][]datedLine // by entry ID
}

type datedLine struct {
	on time.Time
	retention.Line
}

var purgeSchema = compile("purge.schema.json", schemas.PurgeSchema)

// ParsePurge reads a fyniq-purge@1 document, checked against purge.schema.json and the README's rules, as the oracle
// checks it.
func ParsePurge(b []byte) (p Purge, err error) {
	if err := noDuplicateKeys(b); err != nil {
		return p, &InvalidError{Reason: err.Error()}
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
	if err != nil {
		return p, &InvalidError{Reason: err.Error()}
	}
	if err := purgeSchema.Validate(doc); err != nil {
		return p, &InvalidError{Reason: err.Error()}
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return p, &InvalidError{Reason: err.Error()}
	}
	invalid := func(format string, args ...any) (Purge, error) {
		return p, &InvalidError{Reason: fmt.Sprintf(format, args...)}
	}
	currency := func(code string) (money.Currency, bool) {
		units, ok := p.Currencies[code]
		return money.Currency{Code: code, MinorUnits: units}, ok
	}
	bk := purgeBook{accounts: map[string]retention.Account{}, named: map[string]bool{}, lines: map[string][]datedLine{}}
	var ok bool
	if bk.base, ok = currency(p.BaseCurrency); !ok {
		return invalid("the base currency %s is not in currencies", p.BaseCurrency)
	}
	for _, a := range p.Accounts {
		if _, dup := bk.accounts[a.Code]; dup {
			return invalid("two accounts have the code %s", a.Code)
		}
		bk.accounts[a.Code] = retention.Account{Code: a.Code, Type: a.Type, Role: a.Role, Control: a.Control}
	}
	for what, code := range map[string]string{"retained earnings": p.RetainedEarnings, "suspense": p.Suspense} {
		a, ok := bk.accounts[code]
		if !ok {
			return invalid("the %s account %s is not an account", what, code)
		}
		if retention.IsControl(a) || retention.IsProfitOrLoss(a) {
			return invalid("the %s account %s is a control account, revenue or expense", what, code)
		}
	}
	if bk.boundary, err = time.Parse(time.DateOnly, p.Boundary); err != nil {
		return invalid("the boundary %q is not a calendar date", p.Boundary)
	}
	if bk.boundary.Day() != 1 || int(bk.boundary.Month()) != p.FYStartMonth {
		return invalid("the boundary %s is not the first day of a financial year", p.Boundary)
	}
	ids := map[string]bool{}
	for _, e := range p.Entries {
		if ids[e.ID] {
			return invalid("two entries have the ID %s", e.ID)
		}
		ids[e.ID] = true
	}
	for _, e := range p.Entries {
		if e.Reverses != "" && !ids[e.Reverses] {
			return invalid("%s reverses %s, which is not an entry", e.ID, e.Reverses)
		}
		on, err := time.Parse(time.DateOnly, e.PostingDate)
		if err != nil {
			return invalid("%s: posting_date %q is not a calendar date", e.ID, e.PostingDate)
		}
		bk.refs = append(bk.refs, retention.EntryRef{ID: e.ID, Date: on, Reverses: e.Reverses})
		bk.named[e.ID] = e.Named
		balance := money.Zero(bk.base)
		for _, l := range e.Lines {
			where := fmt.Sprintf("%s: a line on %s", e.ID, l.Account)
			if _, ok := bk.accounts[l.Account]; !ok {
				return invalid("%s: no such account", where)
			}
			cur, ok := currency(l.Currency)
			if !ok {
				return invalid("%s: the currency %s is not in currencies", where, l.Currency)
			}
			amount, err := money.Parse(l.Amount, cur)
			if err != nil {
				return invalid("%s: %v", where, err)
			}
			base, err := money.Parse(l.BaseAmount, bk.base)
			if err != nil {
				return invalid("%s: %v", where, err)
			}
			if cur == bk.base && !amount.Equal(base) {
				return invalid("%s: in the base currency, the amount %s is not the base amount %s", where, amount, base)
			}
			if balance, err = balance.Add(base); err != nil {
				return invalid("%s: %v", where, err)
			}
			bk.lines[e.ID] = append(bk.lines[e.ID], datedLine{on, retention.Line{Source: retention.Source{Type: e.Source.Type, ID: e.Source.ID},
				Account: l.Account, DimensionSet: l.DimensionSet, Party: l.Party, Instrument: l.Instrument, Amount: amount, Base: base}})
		}
		if !balance.IsZero() {
			return invalid("%s does not balance in the base currency: %s", e.ID, balance)
		}
	}
	p.book = bk
	return p, nil
}

// positionKey is what a position sums by: account, dimension set, currency, person, instrument and, for a control
// account, the source.
type positionKey struct {
	account, set, currency, party, instrument, sourceType, sourceID string
	sourced                                                         bool
}

// positions are the lines' positions at each date (README, "Purges").
func (p Purge) positions(lines []datedLine, dates []time.Time) ([]map[positionKey][2]money.Money, error) {
	bk := p.book
	out := make([]map[positionKey][2]money.Money, len(dates))
	for i, d := range dates {
		y := d.Year()
		if int(d.Month()) < p.FYStartMonth {
			y--
		}
		start := time.Date(y, time.Month(p.FYStartMonth), 1, 0, 0, 0, 0, time.UTC)
		sums := map[positionKey][2]money.Money{}
		for _, l := range lines {
			if l.on.After(d) {
				continue
			}
			a := bk.accounts[l.Account]
			k := positionKey{account: l.Account, set: l.DimensionSet, currency: l.Amount.Currency().Code, party: l.Party, instrument: l.Instrument}
			amount := l.Amount
			switch {
			case retention.IsProfitOrLoss(a) && l.on.Before(start):
				k, amount = positionKey{account: p.RetainedEarnings, set: l.DimensionSet, currency: bk.base.Code}, l.Base
			case retention.IsControl(a):
				k.sourceType, k.sourceID, k.sourced = l.Source.Type, l.Source.ID, true
			}
			s, ok := sums[k]
			if !ok {
				s = [2]money.Money{money.Zero(amount.Currency()), money.Zero(bk.base)}
			}
			var err error
			if s[0], err = s[0].Add(amount); err != nil {
				return nil, err
			}
			if s[1], err = s[1].Add(l.Base); err != nil {
				return nil, err
			}
			sums[k] = s
		}
		for k, s := range sums {
			if s[0].IsZero() && s[1].IsZero() {
				delete(sums, k)
			}
		}
		out[i] = sums
	}
	return out, nil
}

// RunPurge purges a document through the engine's rule (internal/domain/retention).
func RunPurge(p Purge) (map[string]any, error) {
	bk := p.book
	kept := retention.Kept(bk.refs, bk.named, bk.boundary)
	var purged []retention.Line
	var keptBefore []string
	var left, whole []datedLine
	var after []time.Time
	count := 0
	for _, r := range bk.refs {
		whole = append(whole, bk.lines[r.ID]...)
		if !kept[r.ID] {
			count++
			for _, l := range bk.lines[r.ID] {
				purged = append(purged, l.Line)
			}
			continue
		}
		left = append(left, bk.lines[r.ID]...)
		if r.Date.Before(bk.boundary) {
			keptBefore = append(keptBefore, r.ID)
		} else if r.Date.After(bk.boundary) {
			after = append(after, r.Date)
		}
	}
	slices.Sort(keptBefore)
	entries, err := retention.CarryForward(retention.Input{Base: bk.base, Accounts: bk.accounts, RetainedEarnings: p.RetainedEarnings,
		Suspense: p.Suspense, Boundary: retention.Source{Type: retention.BoundarySource, ID: p.Boundary}, Lines: purged})
	if err != nil {
		return nil, err
	}
	var shown []any
	for _, e := range entries {
		var lines []any
		for _, l := range e.Lines {
			lines = append(lines, map[string]any{"account": l.Account, "dimension_set": l.DimensionSet, "currency": l.Amount.Currency().Code,
				"party": orNull(l.Party), "instrument": orNull(l.Instrument), "amount": l.Amount.String(), "rate": l.Rate.String(),
				"base_amount": l.Base.String()})
			left = append(left, datedLine{bk.boundary, retention.Line{Source: e.Source, Account: l.Account, DimensionSet: l.DimensionSet,
				Party: l.Party, Instrument: l.Instrument, Amount: l.Amount, Base: l.Base}})
		}
		shown = append(shown, map[string]any{"source": map[string]any{"type": e.Source.Type, "id": e.Source.ID}, "lines": lines})
	}
	slices.SortFunc(after, func(a, b time.Time) int { return a.Compare(b) })
	after = slices.CompactFunc(after, time.Time.Equal)
	dates := append([]time.Time{bk.boundary}, after...)
	if len(after) > 0 {
		for y := bk.boundary.AddDate(1, 0, 0); !y.After(after[len(after)-1]); y = y.AddDate(1, 0, 0) {
			if !slices.ContainsFunc(dates, y.Equal) {
				dates = append(dates, y)
			}
		}
	}
	slices.SortFunc(dates, func(a, b time.Time) int { return a.Compare(b) })
	before, err := p.positions(whole, dates)
	if err != nil {
		return nil, err
	}
	now, err := p.positions(left, dates)
	if err != nil {
		return nil, err
	}
	unchanged := true
	var positions []any
	for i, d := range dates {
		if len(before[i]) != len(now[i]) {
			unchanged = false
		}
		keys := make([]positionKey, 0, len(now[i]))
		for k, s := range now[i] {
			keys = append(keys, k)
			if b, ok := before[i][k]; !ok || !b[0].Equal(s[0]) || !b[1].Equal(s[1]) {
				unchanged = false
			}
		}
		slices.SortFunc(keys, func(x, y positionKey) int {
			return cmpStrings(x.account, y.account, x.set, y.set, x.currency, y.currency, x.party, y.party, x.instrument, y.instrument,
				x.sourceType, y.sourceType, x.sourceID, y.sourceID)
		})
		var balances []any
		for _, k := range keys {
			var source any
			if k.sourced {
				source = map[string]any{"type": k.sourceType, "id": k.sourceID}
			}
			balances = append(balances, map[string]any{"account": k.account, "dimension_set": k.set, "currency": k.currency,
				"party": orNull(k.party), "instrument": orNull(k.instrument), "source": source,
				"amount": now[i][k][0].String(), "base_amount": now[i][k][1].String()})
		}
		positions = append(positions, map[string]any{"date": d.Format(time.DateOnly), "balances": orEmpty(balances)})
	}
	return map[string]any{"schema": PurgeResultSchema, "name": p.Name, "kept": orEmptyStrings(keptBefore), "purged": count,
		"entries": orEmpty(shown), "positions": positions, "unchanged": unchanged}, nil
}

// cmpStrings compares pairs in turn: (a1, b1), then (a2, b2), and so on.
func cmpStrings(pairs ...string) int {
	for i := 0; i+1 < len(pairs); i += 2 {
		if c := strings.Compare(pairs[i], pairs[i+1]); c != 0 {
			return c
		}
	}
	return 0
}

// orNull is a person or instrument as the result writes it: null for none.
func orNull(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func orEmpty(xs []any) []any {
	if xs == nil {
		return []any{}
	}
	return xs
}

func orEmptyStrings(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}

// PurgeDifference is the first difference between two fyniq-purge-result@1 results, as ForecastDifference compares;
// "" when they agree.
func PurgeDifference(engine, oracle []byte) string { return ForecastDifference(engine, oracle) }
