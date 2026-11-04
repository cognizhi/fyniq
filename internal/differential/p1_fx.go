package differential

import (
	"cmp"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// The month-end revaluation (README, "Month-end revaluation (FR-GL-04), RND-RATE and GST in another currency"), worked
// out as fx.Prepare and the receivables' and payables' sub-ledgers work it out before they call ledger.Revalue: each
// account marked revalue holds an item per foreign currency at the month's last day, an account with a sub-ledger an
// item per open document and one for what it holds beyond them, each taken to the month's period-end rate.

// PeriodEndRate is a currency's rate for a month's last day, as the organisation stored it: a rate, base per one unit,
// or a quote with its form, which RND-RATE stores.
type PeriodEndRate struct {
	Date         string `json:"date"`
	Currency     string `json:"currency"`
	FXRate       string `json:"fx_rate,omitempty"`
	Quote        string `json:"quote,omitempty"`
	Form         string `json:"form,omitempty"`
	FXRateSource string `json:"fx_rate_source"`
}

// stored is the rate the row stores, base per one unit of its currency: its rate, or RND-RATE of its quote.
func (p PeriodEndRate) stored(base money.Currency) (money.Rate, error) {
	cur, err := money.Lookup(p.Currency)
	if err != nil {
		return money.Rate{}, err
	}
	if p.Quote == "" {
		return money.ParseRate(p.FXRate, cur, base, p.FXRateSource)
	}
	q, err := money.ParseQuote(p.Quote, "quote")
	if err != nil {
		return money.Rate{}, err
	}
	return rounding.Rate(q, money.QuoteForm(p.Form), cur, base, p.FXRateSource)
}

// held is a document of the receivable or payable account as its sub-ledger gives it: its lines on the account, signed
// as the account holds them, from its day, and what settlements did to it since, each on its own day.
type held struct {
	account      uuid.UUID
	day          time.Time
	amount, base money.Money
	moves        []heldMove
}

type heldMove struct {
	day          time.Time
	amount, base money.Money
}

// at is the document's item at the end of day: its lines less what settlements dated on or before it settled.
func (h *held) at(day time.Time) (money.Money, money.Money) {
	amount, base := h.amount, h.base
	for _, m := range h.moves {
		if !m.day.After(day) {
			amount, _ = amount.Add(m.amount)
			base, _ = base.Add(m.base)
		}
	}
	return amount, base
}

// controlLines sums an entry's lines on the account with role in a currency other than the base: what a document of
// the receivable or payable account puts on it.
func (r *p1) controlLines(e ledger.Entry, role string) (uuid.UUID, money.Money, money.Money, bool) {
	var account uuid.UUID
	var amount, base money.Money
	found := false
	for _, l := range e.Lines {
		if r.settings.Accounts[l.AccountID].Role != role || l.Amount.Currency() == r.base {
			continue
		}
		if !found {
			account, amount, base, found = l.AccountID, money.Zero(l.Amount.Currency()), money.Zero(r.base), true
		}
		amount, _ = amount.Add(l.Amount)
		base, _ = base.Add(l.Base)
	}
	return account, amount, base, found && (!amount.IsZero() || !base.IsZero())
}

// hold keeps a posted invoice, credit note, bill or vendor credit in another currency as its sub-ledger keeps it.
func (r *p1) hold(id string, day time.Time, e ledger.Entry, role string) {
	if account, amount, base, ok := r.controlLines(e, role); ok {
		r.held[id] = &held{account: account, day: day, amount: amount, base: base}
	}
}

// holdCredit keeps what a receipt or a payment in another currency left unapplied on the account, a customer's or a
// supplier's credit (FR-AR-18): its lines on the account less what it settled of documents.
func (r *p1) holdCredit(id string, day time.Time, e ledger.Entry, role string, settlements []ledger.Settlement) {
	account, amount, base, ok := r.controlLines(e, role)
	if !ok {
		return
	}
	for _, s := range settlements {
		if s.Amount.Currency() != amount.Currency() {
			continue
		}
		a, b := s.Amount, s.Base
		if role == ledger.RoleAccountsReceivable { // a receipt credits what it settles
			a, b = a.Neg(), b.Neg()
		}
		amount, _ = amount.Sub(a)
		base, _ = base.Sub(b)
	}
	if !amount.IsZero() || !base.IsZero() {
		r.held[id] = &held{account: account, day: day, amount: amount, base: base}
	}
}

// moveHeld records what a settlement on day did to the document it settled: less of what is open of it, on the
// account's side opposite to the document's lines.
func (r *p1) moveHeld(id string, day time.Time, s ledger.Settlement) {
	h := r.held[id]
	if h == nil || s.Amount.Currency() != h.amount.Currency() {
		return
	}
	a, b := s.Amount, s.Base
	if h.amount.Sign() > 0 || h.amount.IsZero() && h.base.Sign() > 0 {
		a, b = a.Neg(), b.Neg()
	}
	h.moves = append(h.moves, heldMove{day: day, amount: a, base: b})
}

// revaluations are the months' revaluations and their reversals, as the scheduler posts them after every document,
// reversal and edit (README, "The revaluation entry and its reversal"): from the month of the first posted entry to the
// last month whose last day is on or before as_of, each read from the entries dated on or before its last day, waiting
// when a currency of an item has no rate, posting nothing when nothing changes, and reversed on the next month's
// first day. They come after every other scheduled entry of their day.
func (r *p1) revaluations(book []booked, sched []scheduled) []scheduled {
	unrealised, flagged := uuid.Nil, false
	for id, a := range r.settings.Accounts {
		if a.UnrealisedFX {
			unrealised = id
		}
		flagged = flagged || a.Revalue
	}
	if !flagged || unrealised == uuid.Nil || r.asOf.IsZero() {
		return nil
	}
	type dated struct {
		day   time.Time
		lines []ledger.Line
	}
	var entries []dated
	for _, b := range book {
		entries = append(entries, dated{b.entry.PostingDate, b.entry.Lines})
	}
	for _, x := range sched {
		entries = append(entries, dated{x.entry.PostingDate, x.entry.Lines})
	}
	if len(entries) == 0 {
		return nil
	}
	first := slices.MinFunc(entries, func(a, b dated) int { return a.day.Compare(b.day) }).day
	index := len(r.s.Documents) // after every document's scheduled entries of the day
	var out []scheduled
	for month := time.Date(first.Year(), first.Month(), 1, 0, 0, 0, 0, time.UTC); ; month = month.AddDate(0, 1, 0) {
		end := month.AddDate(0, 1, -1)
		if end.After(r.asOf) {
			break
		}
		type key struct {
			account uuid.UUID
			cur     string
		}
		balances := map[key][2]money.Money{}
		var keys []key
		for _, e := range entries {
			if e.day.After(end) {
				continue
			}
			for _, l := range e.lines {
				if !r.settings.Accounts[l.AccountID].Revalue || l.Amount.Currency() == r.base {
					continue
				}
				k := key{l.AccountID, l.Amount.Currency().Code}
				b, ok := balances[k]
				if !ok {
					b = [2]money.Money{money.Zero(l.Amount.Currency()), money.Zero(r.base)}
					keys = append(keys, k)
				}
				b[0], _ = b[0].Add(l.Amount)
				b[1], _ = b[1].Add(l.Base)
				balances[k] = b
			}
		}
		slices.SortFunc(keys, func(a, b key) int {
			return cmp.Or(cmp.Compare(r.codeOf[a.account], r.codeOf[b.account]), cmp.Compare(a.cur, b.cur))
		})
		var items []ledger.RevaluationItem
		waits := false
		item := func(account uuid.UUID, open, carrying money.Money, of string) {
			rate, ok := r.periodEndRate(end, open.Currency())
			if !ok {
				waits = true
				return
			}
			items = append(items, ledger.RevaluationItem{AccountID: account, Open: open, Carrying: carrying, Rate: rate, Of: of})
		}
		for _, k := range keys {
			b := balances[k]
			if b[0].IsZero() && b[1].IsZero() {
				continue // the balance service has no row for it
			}
			role := r.settings.Accounts[k.account].Role
			if role != ledger.RoleAccountsReceivable && role != ledger.RoleAccountsPayable {
				item(k.account, b[0], b[1], "")
				continue
			}
			open, carried := money.Zero(b[0].Currency()), money.Zero(r.base)
			ids := make([]string, 0, len(r.held))
			for id := range r.held {
				ids = append(ids, id)
			}
			slices.Sort(ids)
			for _, id := range ids {
				h := r.held[id]
				if h.account != k.account || h.amount.Currency() != b[0].Currency() || h.day.After(end) {
					continue
				}
				a, c := h.at(end)
				if a.IsZero() && c.IsZero() {
					continue
				}
				item(k.account, a, c, id)
				open, _ = open.Add(a)
				carried, _ = carried.Add(c)
			}
			// What the account holds beyond its documents.
			restOpen, _ := b[0].Sub(open)
			restBase, _ := b[1].Sub(carried)
			if !restOpen.IsZero() || !restBase.IsZero() {
				item(k.account, restOpen, restBase, "")
			}
		}
		if waits || len(items) == 0 {
			continue
		}
		doc := "revaluation-" + end.Format("2006-01")
		e, err := ledger.Revalue(ledger.Revaluation{ID: stableID("document", doc), Version: 1, Date: end, Items: items, Unrealised: unrealised}, r.settings)
		if err != nil {
			continue
		}
		e.Lines = canonical(e.Lines, r.codeOf, r.personOf, r.base)
		if len(e.Lines) == 0 {
			continue // every item is at the rate
		}
		out = append(out, scheduled{index: index, doc: doc, kind: "revaluation", entry: e, asStored: true})
		on := end.AddDate(0, 0, 1)
		if on.After(r.asOf) {
			continue
		}
		if rev, err := ledger.Reverse(e, on, ""); err == nil {
			out = append(out, scheduled{index: index, doc: doc, kind: "revaluation_reversal", entry: rev, asStored: true})
		}
	}
	return out
}

// periodEndRate is the rate the settings store for a currency on a month's last day.
func (r *p1) periodEndRate(end time.Time, cur money.Currency) (money.Rate, bool) {
	for _, p := range r.s.Settings.PeriodEndRates {
		if p.Date != end.Format(time.DateOnly) || p.Currency != cur.Code {
			continue
		}
		rate, err := p.stored(r.base)
		return rate, err == nil
	}
	return money.Rate{}, false
}

// checkPeriodEndRates names the first period-end rate the organisation could not have stored (README, "The revalued
// accounts and the period-end rates"), or returns "".
func checkPeriodEndRates(s Scenario) string {
	if s.Settings == nil {
		return ""
	}
	base, err := money.Lookup(s.BaseCurrency)
	if err != nil {
		return ""
	}
	seen := map[[2]string]bool{}
	for _, p := range s.Settings.PeriodEndRates {
		day, err := time.Parse(time.DateOnly, p.Date)
		switch {
		case err != nil || day.AddDate(0, 0, 1).Day() != 1:
			return fmt.Sprintf("the period-end rate of %s on %s is not on a month's last day", p.Currency, p.Date)
		case p.Currency == s.BaseCurrency:
			return fmt.Sprintf("the period-end rate on %s is for the base currency %s", p.Date, p.Currency)
		case seen[[2]string{p.Currency, p.Date}]:
			return fmt.Sprintf("two period-end rates of %s on %s", p.Currency, p.Date)
		}
		if _, ok := s.Currencies[p.Currency]; !ok {
			return fmt.Sprintf("the period-end rate on %s is for %s, which is not in currencies", p.Date, p.Currency)
		}
		seen[[2]string{p.Currency, p.Date}] = true
		if _, err := p.stored(base); err != nil {
			return fmt.Sprintf("the period-end rate of %s on %s cannot be stored: %v", p.Currency, p.Date, err)
		}
	}
	return ""
}
