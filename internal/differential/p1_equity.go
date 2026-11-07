package differential

import (
	"cmp"
	"encoding/json"
	"slices"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
)

// stage keeps a movement of a document being posted; the runner commits a document's movements only once it is
// accepted, so a rejected document moves no holding (README, "Cap tables").
func (r *p1) stage(person, class string, shares apd.Decimal, date time.Time) {
	r.staged = append(r.staged, equity.Move{ID: uuid.New(), On: date, Shares: shares,
		Key: equity.HoldingKey{Holder: stableID("party", person), Account: account(class)}})
}

// commit records the staged movements of the document just accepted.
func (r *p1) commit() {
	r.moves = append(r.moves, r.staged...)
	r.staged = nil
}

// holding is what person holds of class at the end of day, from the movements committed so far.
func (r *p1) holding(person, class string, day time.Time) apd.Decimal {
	var n apd.Decimal
	k := equity.HoldingKey{Holder: stableID("party", person), Account: account(class)}
	for _, m := range r.moves {
		if m.Key == k && !m.On.After(day) {
			_, _ = apd.BaseContext.Add(&n, &n, &m.Shares)
		}
	}
	return n
}

// shareTransfer moves shares of a class between holders on its date and posts nothing, as an equity transaction of kind
// transfer does when it takes effect (FR-EQ-17): refused when the seller's holding would fall below zero at the end of
// any day from its date on, as the database's FY288 refuses it (README, "Share transfers").
func (r *p1) shareTransfer(d Document, date time.Time) (ledger.Entry, bool) {
	var x struct {
		Seller string `json:"seller"`
		Buyer  string `json:"buyer"`
		Class  string `json:"class"`
		Shares string `json:"shares"`
	}
	if json.Unmarshal(d.Raw, &x) != nil || !wholeShares.MatchString(x.Shares) || x.Seller == x.Buyer {
		return ledger.Entry{}, false
	}
	holders := r.settings.Subscribers // the people and subscribers
	if !holders[stableID("party", x.Seller)] || !holders[stableID("party", x.Buyer)] {
		return ledger.Entry{}, false
	}
	if a, ok := r.settings.Accounts[account(x.Class)]; !ok || !a.ShareCapital || a.Group || !a.Active {
		return ledger.Entry{}, false
	}
	var n apd.Decimal
	if _, _, err := n.SetString(x.Shares); err != nil {
		return ledger.Entry{}, false
	}
	days := []time.Time{date}
	for _, m := range r.moves {
		if m.Key.Holder == stableID("party", x.Seller) && m.Key.Account == account(x.Class) && m.On.After(date) {
			days = append(days, m.On)
		}
	}
	for _, day := range days {
		left := r.holding(x.Seller, x.Class, day)
		if left.Cmp(&n) < 0 {
			return ledger.Entry{}, false
		}
	}
	var out apd.Decimal
	out.Neg(&n)
	r.stage(x.Seller, x.Class, out, date)
	r.stage(x.Buyer, x.Class, n, date)
	return ledger.Entry{}, true // no entry: a transfer moves holdings only
}

// capTables are the cap table at the end of each of the scenario's days, from equity.TableAt over the movements, each
// class's votes per share the attribute of its account (one when left out): by holder, then class, in code-point order
// of the scenario's names for them (README, "Cap tables").
func (r *p1) capTables() ([]CapTable, error) {
	votes := map[uuid.UUID]apd.Decimal{}
	for _, a := range r.s.Accounts {
		var v apd.Decimal
		v.SetInt64(1)
		if a.VotesPerShare != "" {
			if _, _, err := v.SetString(a.VotesPerShare); err != nil {
				return nil, err
			}
		}
		votes[account(a.Code)] = v
	}
	plain := func(d apd.Decimal) string {
		var x apd.Decimal
		x.Reduce(&d)
		return x.Text('f')
	}
	share := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}
	var out []CapTable
	for _, at := range r.s.CapTables {
		day, err := time.Parse(time.DateOnly, at)
		if err != nil {
			return nil, err
		}
		t, err := equity.TableAt(day, r.moves, func(k equity.HoldingKey) (apd.Decimal, bool, error) { return votes[k.Account], false, nil })
		if err != nil {
			return nil, err
		}
		c := CapTable{At: at, Issued: plain(t.Issued), Votes: plain(t.Votes), FullyDiluted: plain(t.FullyDiluted),
			Lines: []CapLine{}, Holders: []CapHolder{}}
		for _, l := range t.Lines {
			c.Lines = append(c.Lines, CapLine{Holder: r.personOf[l.Key.Holder], Class: r.codeOf[l.Key.Account], CapFigures: CapFigures{
				Issued: plain(l.Issued), Votes: plain(l.Votes), FullyDiluted: plain(l.FullyDiluted),
				Ownership: share(l.Ownership), Voting: share(l.Voting), Diluted: share(l.Diluted)}})
		}
		for _, h := range t.Holders {
			c.Holders = append(c.Holders, CapHolder{Holder: r.personOf[h.Holder], CapFigures: CapFigures{
				Issued: plain(h.Issued), Votes: plain(h.Votes), FullyDiluted: plain(h.FullyDiluted),
				Ownership: share(h.Ownership), Voting: share(h.Voting), Diluted: share(h.Diluted)}})
		}
		slices.SortFunc(c.Lines, func(a, b CapLine) int { return cmp.Or(cmp.Compare(a.Holder, b.Holder), cmp.Compare(a.Class, b.Class)) })
		slices.SortFunc(c.Holders, func(a, b CapHolder) int { return cmp.Compare(a.Holder, b.Holder) })
		out = append(out, c)
	}
	return out, nil
}
