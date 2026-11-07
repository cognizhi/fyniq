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
	"github.com/cognizhi/fyniq/internal/domain/money"
)

// stage keeps a movement of a document being posted; the runner commits a document's movements only once it is
// accepted, so a rejected document moves no holding (README, "Cap tables").
func (r *p1) stage(person, class string, shares apd.Decimal, date time.Time) {
	r.staged = append(r.staged, equity.Move{ID: uuid.New(), On: date, Shares: shares,
		Key: equity.HoldingKey{Holder: stableID("party", person), Account: account(class)}})
}

// commit records what the document just accepted staged: its movements, the SAFE it records, the SAFEs it converts and
// the round it sets.
func (r *p1) commit() {
	r.moves = append(r.moves, r.staged...)
	if r.stagedSAFE != nil {
		r.safes = append(r.safes, *r.stagedSAFE)
		r.settings.Instruments[r.stagedSAFE.id] = true // a line on the instruments account may name it from now on
	}
	for _, id := range r.stagedConverted {
		r.converted[id] = true
	}
	if r.stagedRound != nil {
		r.rounds[r.stagedRound.doc] = r.stagedRound.round
	}
	if r.stagedPool != nil {
		r.optionBook.Pools = append(r.optionBook.Pools, *r.stagedPool)
	}
	if g := r.stagedGrant; g != nil {
		r.optionBook.Grants = append(r.optionBook.Grants, g.terms)
		o := equity.Option{Key: g.key(), On: g.terms.GrantedOn}
		if _, _, err := o.Number.SetString(g.terms.Number); err == nil {
			r.options = append(r.options, o)
		}
		if r.grants == nil {
			r.grants = map[string]*recordedGrant{}
		}
		r.grants[g.doc] = g
	}
	if x := r.stagedEvent; x != nil {
		x.grant.events = append(x.grant.events, x.event)
		r.optionBook.Returns = append(r.optionBook.Returns, equity.Return{On: x.event.On, Number: x.event.Number})
		gone := equity.Option{Key: x.grant.key(), On: x.event.On}
		if _, _, err := gone.Number.SetString("-" + x.event.Number); err == nil {
			r.options = append(r.options, gone)
		}
	}
	r.unstage()
}

// unstage forgets what the document being posted staged.
func (r *p1) unstage() {
	r.staged, r.stagedSAFE, r.stagedConverted, r.stagedRound, r.stagedPool, r.stagedGrant, r.stagedEvent = nil, nil, nil, nil, nil, nil, nil
}

// recordedGrant is a grant an option_grant document recorded: its document's ID, its terms and its recorded events.
type recordedGrant struct {
	doc    string
	terms  equity.Grant
	events []equity.GrantEvent
}

func (g *recordedGrant) key() equity.HoldingKey {
	return equity.HoldingKey{Holder: g.terms.HolderID, Account: g.terms.ClassAccountID}
}

// stagedEvent is a forfeiture or expiry being posted, and its grant.
type stagedEvent struct {
	grant *recordedGrant
	event equity.GrantEvent
}

// optionEvent records a forfeiture or expiry of a recorded grant's options, as recording one does (FR-EQ-19): the
// product's checks on its own terms (equity.CheckEvent) and against its grant and the grant's recorded events
// (equity.CheckEventOf: at most what they leave, and a forfeiture at most what is not vested on its day). It posts
// nothing; from its day the options leave the holder's line and are back in the pool.
func (r *p1) optionEvent(d Document, date time.Time) bool {
	var x struct {
		Grant  string `json:"grant"`
		Kind   string `json:"kind"`
		Number string `json:"number"`
	}
	if json.Unmarshal(d.Raw, &x) != nil {
		return false
	}
	g, ok := r.grants[x.Grant]
	if !ok {
		return false
	}
	e := equity.GrantEvent{Date: date, GrantID: stableID("document", x.Grant), Kind: x.Kind, Number: x.Number, On: date}
	if len(equity.CheckEvent(e, time.Time{})) > 0 {
		return false
	}
	if probs, err := equity.CheckEventOf(e, g.terms, g.events); err != nil || len(probs) > 0 {
		return false
	}
	r.stagedEvent = &stagedEvent{grant: g, event: e}
	return true
}

// optionPool records a resolution setting the option pool's size from its date, as recording one does (FR-EQ-19): the
// product's checks on its terms (equity.CheckPool), then the pool covering the grants on every day from its date
// (equity.OptionBook.ShortFrom, FY293). It posts nothing.
func (r *p1) optionPool(d Document, date time.Time) bool {
	var x struct {
		Size  string `json:"size"`
		Class string `json:"class"`
	}
	if json.Unmarshal(d.Raw, &x) != nil {
		return false
	}
	p := equity.PoolResolution{Date: date, Size: x.Size, ClassAccountID: account(x.Class), ResolutionRef: d.ID, ResolvedOn: date}
	if len(equity.CheckPool(p, r.settings, time.Time{})) > 0 {
		return false
	}
	book := equity.OptionBook{Pools: append(slices.Clip(r.optionBook.Pools), p), Grants: r.optionBook.Grants, Returns: r.optionBook.Returns}
	if short, err := book.ShortFrom(date); err != nil || short != nil {
		return false
	}
	r.stagedPool = &p
	return true
}

// optionGrant records a grant of options, as recording one does (FR-EQ-19): its holder one of the people or subscribers,
// the product's checks on its terms (equity.CheckGrant; the format has no exercise price, vesting or expiry, so they are
// ones that pass), a pool of its class in force on its date, and the pool covering the grants on every day from it
// (equity.OptionBook.ShortFrom, FY293). It posts nothing.
func (r *p1) optionGrant(d Document, date time.Time) bool {
	var x struct {
		Person        string `json:"person"`
		Number        string `json:"number"`
		Class         string `json:"class"`
		CliffMonths   int    `json:"cliff_months"`
		VestingMonths int    `json:"vesting_months"`
	}
	if json.Unmarshal(d.Raw, &x) != nil || !r.settings.Subscribers[stableID("party", x.Person)] {
		return false
	}
	g := equity.Grant{Date: date, HolderID: stableID("party", x.Person), Number: x.Number, ExercisePrice: "0", GrantedOn: date,
		Vesting: equity.Vesting{CliffMonths: x.CliffMonths, Months: x.VestingMonths, Acceleration: "none"}, ExpiresOn: date.AddDate(0, 0, 1),
		ClassAccountID: account(x.Class)}
	if len(equity.CheckGrant(g, r.settings, time.Time{})) > 0 {
		return false
	}
	pool, in, err := r.optionBook.At(date)
	if err != nil || in < 0 || pool.ClassID != g.ClassAccountID {
		return false
	}
	book := equity.OptionBook{Pools: r.optionBook.Pools, Grants: append(slices.Clip(r.optionBook.Grants), g), Returns: r.optionBook.Returns}
	if short, err := book.ShortFrom(date); err != nil || short != nil {
		return false
	}
	r.stagedGrant = &recordedGrant{doc: d.ID, terms: g}
	return true
}

// recordedSAFE is a SAFE an instrument document recorded: its document's ID, its holder's and class's names in the
// scenario, and its terms as the product records them.
type recordedSAFE struct {
	doc, holder, class string
	id                 uuid.UUID
	terms              equity.Instrument
}

// round is the priced round a cash issue sets: its price per share and the day it took effect.
type round struct {
	price money.UnitPrice
	day   time.Time
}

type namedRound struct {
	doc   string
	round round
}

// safeForms are the format's forms of SAFE (README, "SAFEs and their conversion"): a convertible note is not in it yet.
var safeForms = []string{equity.FormPostMoneySAFE, equity.FormPreMoneySAFE, equity.FormDiscountSAFE, equity.FormMFNSAFE}

// instrument records a SAFE on its date, as recording an instrument does (FR-EQ-18): the product's checks
// (equity.CheckInstrument) on its terms, its holder one of the people or subscribers. It posts nothing.
func (r *p1) instrument(d Document, date time.Time) bool {
	var x struct {
		Person        string `json:"person"`
		Form          string `json:"form"`
		Amount        string `json:"amount"`
		ValuationCap  string `json:"valuation_cap"`
		Discount      string `json:"discount"`
		Class         string `json:"class"`
		PriceDecimals int    `json:"price_decimals"`
	}
	if json.Unmarshal(d.Raw, &x) != nil || !r.settings.Subscribers[stableID("party", x.Person)] || !slices.Contains(safeForms, x.Form) {
		return false
	}
	amount, ok := r.amount(x.Amount, r.base.Code)
	if !ok {
		return false
	}
	i := equity.Instrument{Form: x.Form, HolderID: stableID("party", x.Person), Amount: amount, SignedOn: date, ValuationCap: x.ValuationCap,
		Discount: x.Discount, MFN: x.Form == equity.FormMFNSAFE, Trigger: "priced_round", ClassAccountID: account(x.Class),
		Denominator: "pre_money_shares", PriceDecimals: x.PriceDecimals}
	if x.Form == equity.FormPostMoneySAFE {
		i.Denominator = "post_money_capitalisation"
	}
	if len(equity.CheckInstrument(i, r.settings)) > 0 {
		return false
	}
	r.stagedSAFE = &recordedSAFE{doc: d.ID, holder: x.Person, class: x.Class, id: stableID("instrument", d.ID), terms: i}
	return true
}

// heldFor is what the instruments account holds for a SAFE at the end of day: the credit of the lines naming it there in
// the entries posted before, dated on or before day (README, "SAFEs and their conversion").
func (r *p1) heldFor(holding, safe uuid.UUID, day time.Time) money.Money {
	sum := money.Zero(r.base)
	for _, p := range r.posted {
		if p.line.AccountID == holding && p.line.Instrument == safe && !p.day.After(day) {
			sum, _ = sum.Sub(p.line.Base)
		}
	}
	return sum
}

// safeConversion converts every SAFE recorded and not converted yet at a priced round, as the drafts of
// draftInstrumentConversions do once each is effective (FR-EQ-18, design D6): equity.Convert at the round's price over
// the shares issued the day before the round, then instrument_conversion@1 for each, their lines in one entry.
func (r *p1) safeConversion(d Document, date time.Time) (ledger.Entry, bool) {
	var x struct {
		Round string `json:"round"`
	}
	if json.Unmarshal(d.Raw, &x) != nil {
		return ledger.Entry{}, false
	}
	rd, ok := r.rounds[x.Round]
	if !ok || rd.day.After(date) {
		return ledger.Entry{}, false
	}
	var holding uuid.UUID
	accounts := 0
	for id, a := range r.settings.Accounts {
		if a.InstrumentAccount() && a.Active && !a.Group {
			holding, accounts = id, accounts+1
		}
	}
	if accounts != 1 {
		return ledger.Entry{}, false
	}
	var safes []recordedSAFE
	var converting []equity.Converting
	for _, s := range r.safes {
		if !r.converted[s.id] {
			safes, converting = append(safes, s), append(converting, equity.Converting{ID: s.id, Instrument: s.terms})
		}
	}
	if len(converting) == 0 {
		return ledger.Entry{}, false
	}
	one := func(equity.HoldingKey) (apd.Decimal, bool, error) { return *apd.New(1, 0), false, nil }
	before, err := equity.TableAt(rd.day.AddDate(0, 0, -1), r.moves, one)
	if err != nil {
		return ledger.Entry{}, false
	}
	conversions, err := equity.Convert(equity.Round{Price: rd.price, Before: before.Issued}, converting, func(string) bool { return false })
	if err != nil {
		return ledger.Entry{}, false
	}
	e := ledger.Entry{PostingDate: date, RuleVersion: ledger.RuleInstrumentConversion,
		Source: ledger.Source{Type: ledger.SourceShareIssue, ID: stableID("document", d.ID).String(), Version: max(d.Version, 1)}}
	for k, c := range conversions {
		if c.Shares.Sign() <= 0 || c.Converted.Sign() <= 0 {
			return ledger.Entry{}, false
		}
		if held, err := r.heldFor(holding, c.ID, date).Cmp(c.Converted); err != nil || held < 0 {
			return ledger.Entry{}, false
		}
		posted, err := ledger.PostInstrumentConversion(ledger.InstrumentConversion{ID: stableID("issue", d.ID+"/"+safes[k].doc), PostOn: date,
			Instrument: c.ID, Holding: holding, Class: safes[k].terms.ClassAccountID, Amount: c.Converted}, r.settings)
		if err != nil {
			return ledger.Entry{}, false
		}
		e.Lines = append(e.Lines, posted.Lines...)
		r.stage(safes[k].holder, safes[k].class, c.Shares, date)
		r.stagedConverted = append(r.stagedConverted, c.ID)
	}
	return e, true
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
		dilution := equity.Dilution{Options: r.options}
		if len(r.optionBook.Pools) > 0 {
			dilution.Pool = func(day time.Time) (apd.Decimal, error) {
				p, _, err := r.optionBook.At(day)
				return p.Unallocated, err
			}
		}
		t, err := equity.DilutedAt(day, r.moves, func(k equity.HoldingKey) (apd.Decimal, bool, error) { return votes[k.Account], false, nil },
			dilution)
		if err != nil {
			return nil, err
		}
		c := CapTable{At: at, Issued: plain(t.Issued), Votes: plain(t.Votes), Options: plain(t.Options), Unallocated: plain(t.Unallocated),
			FullyDiluted: plain(t.FullyDiluted), Lines: []CapLine{}, Holders: []CapHolder{}}
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

// directorSAFE posts a founder's balance turned into a SAFE they hold (director_safe@1, FR-EQ-05), as approving the
// settlement's safe route does: at most what is owed on the account, into a recorded SAFE the person holds, never past
// its amount with what 2700 holds for it already.
func (r *p1) directorSAFE(d Document, date time.Time) (ledger.Entry, bool) {
	var x struct {
		Person     string `json:"person"`
		Account    string `json:"account"`
		Instrument string `json:"instrument"`
		Amount     string `json:"amount"`
	}
	if json.Unmarshal(d.Raw, &x) != nil {
		return ledger.Entry{}, false
	}
	amount, ok := r.amount(x.Amount, r.base.Code)
	if !ok || !r.within(amount, x.Person, account(x.Account), date) { // at most what is owed (FR-EQ-05)
		return ledger.Entry{}, false
	}
	var safe *recordedSAFE
	for k := range r.safes {
		if r.safes[k].doc == x.Instrument {
			safe = &r.safes[k]
		}
	}
	if safe == nil || safe.holder != x.Person {
		return ledger.Entry{}, false
	}
	var holding uuid.UUID
	accounts := 0
	for id, a := range r.settings.Accounts {
		if a.InstrumentAccount() && a.Active && !a.Group {
			holding, accounts = id, accounts+1
		}
	}
	if accounts != 1 {
		return ledger.Entry{}, false
	}
	total, err := r.heldFor(holding, safe.id, date).Add(amount)
	if err != nil {
		return ledger.Entry{}, false
	}
	if c, err := total.Cmp(safe.terms.Amount); err != nil || c > 0 {
		return ledger.Entry{}, false
	}
	e, err := ledger.PostDirectorSAFE(ledger.DirectorSAFE{ID: stableID("document", d.ID), Version: max(d.Version, 1), PostOn: date,
		Party: stableID("party", x.Person), Account: account(x.Account), Holding: holding, Instrument: safe.id, Amount: amount}, r.settings)
	return e, err == nil
}
