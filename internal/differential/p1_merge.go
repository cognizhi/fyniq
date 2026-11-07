package differential

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/period"
)

// Account merges (README, "Account merges"; FR-COA-04, Appendix B.8): what the account holds from the entries posted
// before the merge, each currency and dimension set apart, moved by the engine's rule (account_merge@1) as the approval's
// unit of work moves it; the account inactive from then on.

// postedLine is a line of an entry posted so far, with its entry's date.
type postedLine struct {
	day  time.Time
	line ledger.Line
}

// toCome is a spread's release to an account, posted by the scheduler on its date.
type toCome struct {
	date    time.Time
	account uuid.UUID
}

// reversing is a manual journal that reverses by itself on its date, as it posted.
type reversing struct {
	entry ledger.Entry
	on    time.Time
}

type mergeDoc struct {
	Account string `json:"account"`
	Into    string `json:"into"`
}

// holdingKey is a holding's currency and dimension set.
type holdingKey struct {
	currency string
	values   string
}

// accountMerge posts the merge of d's account into the other on date, or reports it rejected (the section's rules 1 to
// 5; the engine's own checks cover 1 and 2).
func (r *p1) accountMerge(d Document, date time.Time) (ledger.Entry, bool) {
	var x mergeDoc
	if err := json.Unmarshal(d.Raw, &x); err != nil {
		return ledger.Entry{}, false
	}
	from, into := stableID("account", x.Account), stableID("account", x.Into)
	acc, ok := r.settings.Accounts[from]
	if !ok || date.After(r.asOf) || acc.Revalue || r.settings.Accounts[into].Revalue { // revalued accounts: not in the format yet
		return ledger.Entry{}, false
	}
	if r.s.Settings.BooksStart != "" {
		if books, err := time.Parse(time.DateOnly, r.s.Settings.BooksStart); err != nil || date.Before(books) {
			return ledger.Entry{}, false
		}
	}
	start := time.Time{} // a balance-sheet account moves its whole balance
	if acc.Type == "revenue" || acc.Type == "expense" {
		books, err1 := time.Parse(time.DateOnly, r.s.Settings.BooksStart)
		end, err2 := time.Parse(time.DateOnly, r.s.Settings.FirstYearEnd)
		if err1 != nil || err2 != nil {
			return ledger.Entry{}, false
		}
		cal := period.Calendar{BooksStart: books, FirstYearEnd: end}
		n := cal.Number(date)
		if n == 0 {
			return ledger.Entry{}, false
		}
		start = cal.Year(n).StartsOn
	}

	lines := slices.Clone(r.posted)
	for _, p := range r.pending { // the releases scheduled so far, as they post
		if !p.entry.PostingDate.After(date) {
			for _, l := range p.entry.Lines {
				lines = append(lines, postedLine{day: p.entry.PostingDate, line: l})
			}
		}
	}
	for _, j := range r.journals { // the automatic reversals of the journals before it
		if j.on.After(date) {
			for _, l := range j.entry.Lines {
				if l.AccountID == from {
					return ledger.Entry{}, false // rule 5: it still posts to the account
				}
			}
			continue
		}
		rev, err := ledger.Reverse(j.entry, j.on, "")
		if err != nil {
			return ledger.Entry{}, false
		}
		for _, l := range rev.Lines {
			lines = append(lines, postedLine{day: j.on, line: l})
		}
	}
	for _, t := range r.toCome { // rule 5: a release to the account after the date
		if t.account == from && t.date.After(date) {
			return ledger.Entry{}, false
		}
	}
	for _, a := range r.assets { // rule 5: an asset whose cost is on the account, not disposed of by the date
		if a.account == from && (a.disposed.IsZero() || a.disposed.After(date)) {
			return ledger.Entry{}, false
		}
	}

	sums := map[holdingKey]*ledger.MergeHolding{}
	var keys []holdingKey
	for _, p := range lines {
		l := p.line
		if l.AccountID != from {
			continue
		}
		if p.day.After(date) {
			return ledger.Entry{}, false // rule 4: a line after the date
		}
		if p.day.Before(start) {
			continue
		}
		values := canonicalValues(l.Values)
		k := holdingKey{l.Amount.Currency().Code, setKey(values)}
		h, seen := sums[k]
		if !seen {
			h = &ledger.MergeHolding{Amount: money.Zero(l.Amount.Currency()), Base: money.Zero(r.base), Values: values}
			sums[k] = h
			keys = append(keys, k)
		}
		var err error
		if h.Amount, err = h.Amount.Add(l.Amount); err != nil {
			return ledger.Entry{}, false
		}
		if h.Base, err = h.Base.Add(l.Base); err != nil {
			return ledger.Entry{}, false
		}
	}
	slices.SortFunc(keys, func(a, b holdingKey) int {
		return cmp.Or(cmp.Compare(a.currency, b.currency), cmp.Compare(a.values, b.values))
	})
	m := ledger.AccountMerge{ID: stableID("document", d.ID), Version: max(d.Version, 1), Date: date, Memo: "Merge of " + x.Account + " into " + x.Into,
		From: from, Into: into}
	for _, k := range keys {
		if h := sums[k]; !h.Amount.IsZero() || !h.Base.IsZero() {
			m.Holdings = append(m.Holdings, *h)
		}
	}
	e, err := ledger.PostAccountMerge(m, r.settings)
	if err != nil || !r.fits(e) {
		return ledger.Entry{}, false
	}
	acc.Active = false // inactive for good, merged into the other
	r.settings.Accounts[from] = acc
	return e, true
}

// canonicalValues are dimension values in a stable order, as a set.
func canonicalValues(values []uuid.UUID) []uuid.UUID {
	out := slices.Clone(values)
	slices.SortFunc(out, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	return out
}

// setKey names a set of dimension values.
func setKey(values []uuid.UUID) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = v.String()
	}
	return strings.Join(parts, ",")
}

// touchesInactive reports whether an entry has a line on an account no longer active: one merged since it posted.
func touchesInactive(e ledger.Entry, s ledger.Settings) bool {
	for _, l := range e.Lines {
		if a, ok := s.Accounts[l.AccountID]; ok && !a.Active {
			return true
		}
	}
	return false
}
