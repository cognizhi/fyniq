package differential

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/deposit"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/packs"
)

// The P1 sources (test/differential/README.md, "P1 sources"): each document posts through its own rule set in package
// ledger, the same pure functions the domain services call. What a service reads from the database before it calls the
// engine (the accounts' roles, the money accounts, the GST registration and rate, what a document still has open) comes
// from the scenario's settings and from the documents posted before it.

// Settings are the organisation's settings a P1 scenario declares (README, "Settings and as_of").
type Settings struct {
	BooksStart string `json:"books_start,omitempty"`
	// FirstYearEnd is the first financial year's last day (FR-ORG-02): with BooksStart, the years an income or expense
	// account's merge reads (README, "Account merges").
	FirstYearEnd  string            `json:"first_year_end,omitempty"`
	Accounts      map[string]string `json:"accounts,omitempty"`
	MoneyAccounts map[string]string `json:"money_accounts,omitempty"`
	MileageRates  []MileageRate     `json:"mileage_rates,omitempty"`
	Tolerance     map[string]string `json:"tolerance,omitempty"`
	GST           *GSTSettings      `json:"gst,omitempty"`
	// PeriodEndRates are the currencies' rates for months' last days, which the month-end revaluation reads (FR-GL-04).
	PeriodEndRates []PeriodEndRate `json:"period_end_rates,omitempty"`
}

// MileageRate is the rate per km for a vehicle type from a date (FR-AP-16).
type MileageRate struct {
	Vehicle string `json:"vehicle"`
	From    string `json:"from"`
	Rate    string `json:"rate"`
}

// GSTSettings are the GST registration, both dates inclusive, and the standard rate from each date (ENG-31).
type GSTSettings struct {
	RegisteredFrom string    `json:"registered_from,omitempty"`
	RegisteredTo   string    `json:"registered_to,omitempty"`
	PerLine        bool      `json:"per_line,omitempty"`
	Rates          []GSTRate `json:"rates"`
}

// GSTRate is the standard rate as a percentage ("9") from a date.
type GSTRate struct {
	From    string `json:"from"`
	Percent string `json:"percent"`
}

// systemRoles are the settings keys that give an account a system role the rules find it by (Appendix B.5). The other
// keys (bank_charges, advances, disposal, disposal_clearing) are settings a document's rule is handed directly.
var systemRoles = map[string]string{
	"receivable":               ledger.RoleAccountsReceivable,
	"payable":                  ledger.RoleAccountsPayable,
	"gst":                      ledger.RoleGSTControl,
	"prepayments":              ledger.RolePrepayments,
	"deferred_revenue":         ledger.RoleDeferredRevenue,
	"fx":                       ledger.RoleFXRounding,
	"withholding_tax":          ledger.RoleWithholdingTax,
	"customer_deposits":        ledger.RoleCustomerDeposits,
	"claims_payable":           ledger.RoleExpenseClaimsPayable,
	"director_loan":            ledger.RoleDirectorLoan,
	"deferred_remuneration":    ledger.RoleDeferredRemuneration,
	"depreciation":             ledger.RoleDepreciation,
	"accumulated_depreciation": ledger.RoleAccumulatedDepreciation,
}

// applySettings gives the chart the P1 settings: the system roles by key, and each money account its currency.
func applySettings(s Scenario, settings ledger.Settings) {
	if s.Settings == nil {
		return
	}
	keys := make([]string, 0, len(s.Settings.Accounts))
	for k := range s.Settings.Accounts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		role, ok := systemRoles[k]
		if !ok {
			continue
		}
		id := stableID("account", s.Settings.Accounts[k])
		if a, known := settings.Accounts[id]; known {
			// The rules find each system account by its role, so the settings' account holds it alone: another account
			// the chart gave the same role (the statements' depreciation account) yields it for posting.
			for other, b := range settings.Accounts {
				if b.Role == role && other != id {
					b.Role = ""
					settings.Accounts[other] = b
				}
			}
			a.Role = role
			settings.Accounts[id] = a
		}
	}
	if code, ok := s.Settings.Accounts["unrealised_fx"]; ok { // the month-end revaluation's 7040 (B.5, FR-GL-04)
		id := stableID("account", code)
		if a, known := settings.Accounts[id]; known {
			a.UnrealisedFX = true
			settings.Accounts[id] = a
		}
	}
	for code, cur := range s.Settings.MoneyAccounts {
		id := stableID("account", code)
		if a, known := settings.Accounts[id]; known {
			a.Money, a.Currency = true, cur
			settings.Accounts[id] = a
		}
	}
}

// p1 posts a scenario's P1 documents and keeps what later documents and the scheduler read.
type p1 struct {
	s        Scenario
	base     money.Currency
	settings ledger.Settings
	asOf     time.Time
	codes    pack.Version // the sg-gst codes (Appendix B.6); the scenario's own settings give the rates
	codeOf   map[uuid.UUID]string
	personOf map[uuid.UUID]string
	valueOf  valueNames
	named    map[[2]uuid.UUID][]named // the posted lines naming a person, by person and account
	held     map[string]*held         // the receivable's and payable's documents in other currencies, for the revaluation
	pending  []scheduled
	open     map[string]*openDocument // the posted documents settlements settle, by the scenario's ID
	used     map[string]money.Money   // the settlement differences cleared each month (YYYY-MM), against its cap
	issued   map[string]*issued       // the posted invoices credit notes credit, by the scenario's ID
	deposits map[string]*deposit.Open // the posted customer deposits, as invoices and refunds use them
	advances map[string]*advance      // the posted cash advances, as claims and refunds settle them
	assets   map[string]*registered   // the assets in the register, by the scenario's ID
	posted   []postedLine             // every line of the entries posted so far, for what an account merge moves
	toCome   []toCome                 // what spreads release to accounts, as_of or not, for an account merge's rule 5
	journals []reversing              // the manual journals that reverse by themselves, as posted
}

// scheduled is one entry the scheduler posts after every document: a spread's release or an asset's depreciation.
type scheduled struct {
	index int // the document's place in the scenario
	doc   string
	kind  string
	entry ledger.Entry
	// asStored keeps the entry's lines as they are, an automatic reversal's negating its journal's line by line,
	// instead of the canonical form.
	asStored bool
}

func newP1(s Scenario, base money.Currency, settings ledger.Settings) (*p1, error) {
	r := &p1{s: s, base: base, settings: settings, open: map[string]*openDocument{}, used: map[string]money.Money{},
		issued: map[string]*issued{}, deposits: map[string]*deposit.Open{}, advances: map[string]*advance{},
		assets: map[string]*registered{}, codeOf: map[uuid.UUID]string{}, personOf: map[uuid.UUID]string{}, named: map[[2]uuid.UUID][]named{}, held: map[string]*held{}}
	for id, a := range settings.Accounts {
		r.codeOf[id] = a.Code
	}
	for _, p := range append(slices.Clone(s.People), s.Subscribers...) {
		r.personOf[stableID("party", p)] = p
	}
	r.valueOf = dimensionNames(settings)
	if s.AsOf != "" {
		r.asOf, _ = time.Parse(time.DateOnly, s.AsOf)
	}
	all, err := pack.LoadAll(packs.FS)
	if err != nil {
		return nil, err
	}
	// The codes are the same in every sg-gst version: the newest this release carries lists them. Their rates come from
	// the scenario, which stands in for the sg-gst pack (README, "Settings and as_of").
	for _, v := range all {
		if v.Pack == "sg-gst" && !v.EffectiveFrom.Before(r.codes.EffectiveFrom) {
			r.codes = v
		}
	}
	if r.codes.Pack == "" {
		return nil, fmt.Errorf("this release carries no sg-gst pack")
	}
	return r, nil
}

// post posts document i, or reports it rejected. A document that posts nothing of its own (an asset in the register)
// comes back accepted with an entry of no lines.
func (r *p1) post(i int, d Document) (ledger.Entry, bool) {
	date, err := time.Parse(time.DateOnly, d.Date)
	if err != nil {
		return ledger.Entry{}, false
	}
	switch d.Type {
	case ledger.SourceBankTransaction:
		return r.bankTransaction(i, d, date)
	case ledger.SourceBankTransfer:
		return r.bankTransfer(d, date)
	case ledger.SourceBill:
		return r.bill(i, d, date, false)
	case ledger.SourceVendorCredit:
		return r.bill(i, d, date, true)
	case ledger.SourceBillPayment:
		return r.billPayment(d, date)
	case "invoice":
		return r.invoice(i, d, date, false)
	case "credit_note":
		return r.invoice(i, d, date, true)
	case "customer_deposit":
		return r.customerDeposit(d, date)
	case "customer_receipt":
		return r.customerReceipt(d, date)
	case "deposit_refund":
		return r.depositRefund(d, date)
	case "write_off":
		return r.writeOff(d, date)
	case "expense_claim":
		return r.expenseClaim(d, date)
	case "claim_reimbursement":
		return r.claimReimbursement(d, date)
	case "cash_advance":
		return r.cashAdvance(d, date)
	case "fixed_asset":
		return ledger.Entry{}, r.fixedAsset(i, d, date) // a registered asset posts no entry of its own
	case "asset_disposal":
		return r.assetDisposal(d, date)
	case "pre_incorporation_cost":
		return r.preIncorporationCost(d, date)
	case "director_fee":
		return r.directorFee(d, date)
	case "director_waiver":
		return r.directorWaiver(d, date)
	case "director_repayment":
		return r.directorRepayment(d, date)
	case "director_conversion":
		return r.directorConversion(d, date)
	case "share_issue_cash":
		return r.shareIssueCash(d, date)
	case ledger.SourceAccountMerge:
		return r.accountMerge(d, date)
	}
	return ledger.Entry{}, false
}

// amount reads an amount in a currency the scenario declares, under ENG-44's strict rules.
func (r *p1) amount(s, code string) (money.Money, bool) {
	if _, ok := r.s.Currencies[code]; !ok {
		return money.Money{}, false
	}
	cur, err := money.Lookup(code)
	if err != nil {
		return money.Money{}, false
	}
	m, err := money.Parse(s, cur)
	return m, err == nil
}

// rate reads the rate of an amount in cur: none for the base currency, and a rate with its source for any other
// (the P1 rules every source shares, rule 2).
func (r *p1) rate(cur money.Currency, rate, source string) (*money.Rate, bool) {
	isBase, hasRate := cur == r.base, rate != "" || source != ""
	if isBase && hasRate || !isBase && (rate == "" || source == "") {
		return nil, false
	}
	if isBase {
		return nil, true
	}
	x, err := money.ParseRate(rate, cur, r.base, source)
	if err != nil {
		return nil, false
	}
	return &x, true
}

// account is the engine's ID for an account code.
func account(code string) uuid.UUID { return stableID("account", code) }

// setting is the account a settings key names, if the organisation set it.
func (r *p1) setting(key string) (uuid.UUID, bool) {
	if r.s.Settings == nil || r.s.Settings.Accounts[key] == "" {
		return uuid.Nil, false
	}
	return account(r.s.Settings.Accounts[key]), true
}

// registered says whether the organisation is GST-registered on day.
func (r *p1) registered(day time.Time) bool {
	g := r.s.Settings.GST
	if g == nil || g.RegisteredFrom == "" {
		return false
	}
	from, _ := time.Parse(time.DateOnly, g.RegisteredFrom)
	if day.Before(from) {
		return false
	}
	if g.RegisteredTo != "" {
		to, _ := time.Parse(time.DateOnly, g.RegisteredTo)
		return !day.After(to)
	}
	return true
}

// standardRate is the standard rate in force on day as a fraction ("0.09"), or false when none is.
func (r *p1) standardRate(day time.Time) (string, bool) {
	g := r.s.Settings.GST
	if g == nil {
		return "", false
	}
	var in GSTRate
	var since time.Time
	for _, x := range g.Rates {
		from, err := time.Parse(time.DateOnly, x.From)
		if err != nil || from.After(day) || in.Percent != "" && from.Before(since) {
			continue
		}
		in, since = x, from
	}
	if in.Percent == "" {
		return "", false
	}
	var d apd.Decimal
	if _, _, err := d.SetString(in.Percent); err != nil {
		return "", false
	}
	d.Exponent -= 2 // a percentage as a fraction, exactly
	return d.Text('f'), true
}

// gstRate is the GST rate a line coded code bears on day, as the domain decides it before it calls the engine: the
// standard rate for a standard-rated code while registered, "0" for a code the return reports at no GST, and none
// otherwise. An unknown code, or a registered day with no rate in force, rejects the document (the P1 rules, rule 4).
func (r *p1) gstRate(code string, day time.Time) (string, bool) {
	c, ok := pack.Code(r.codes, code)
	if !ok {
		return "", false
	}
	if !r.registered(day) {
		return "", true
	}
	standard, ok := r.standardRate(day)
	switch {
	case !ok:
		return "", false
	case c.StandardRated:
		return standard, true
	case c.ValueBox != 0:
		return "0", true
	}
	return "", true
}

// schedule keeps the entries of releases dated on or before as_of (ENG-05); a month whose part is zero releases
// nothing (the P1 rules, rule 5).
func (r *p1) schedule(i int, doc string, releases []ledger.Release, post func(ledger.Release, ledger.Settings) (ledger.Entry, error)) bool {
	for _, x := range releases {
		if !x.Amount.IsZero() {
			r.toCome = append(r.toCome, toCome{date: x.Date, account: x.AccountID})
		}
	}
	for _, x := range releases {
		if x.Date.After(r.asOf) || x.Amount.IsZero() {
			continue
		}
		e, err := post(x, r.settings)
		if err != nil {
			return false
		}
		r.pending = append(r.pending, scheduled{index: i, doc: doc, kind: "release", entry: e})
	}
	return true
}

// scheduledEntries are the scheduler's entries: one per document, kind and date, its lines together, ordered by date
// and then by the document's place in the scenario (README, "P1 entries").
func (r *p1) scheduledEntries() []scheduled {
	r.depreciation()
	type key struct {
		index int
		kind  string
		date  time.Time
	}
	merged := map[key]*scheduled{}
	var order []key
	for _, x := range r.pending {
		k := key{x.index, x.kind, x.entry.PostingDate}
		if m, ok := merged[k]; ok {
			m.entry.Lines = append(m.entry.Lines, x.entry.Lines...)
			continue
		}
		c := x
		c.entry.Lines = slices.Clone(x.entry.Lines)
		merged[k] = &c
		order = append(order, k)
	}
	slices.SortStableFunc(order, func(a, b key) int {
		return cmp.Or(a.date.Compare(b.date), cmp.Compare(a.index, b.index), cmp.Compare(a.kind, b.kind))
	})
	out := make([]scheduled, len(order))
	for i, k := range order {
		out[i] = *merged[k]
	}
	return out
}

// named is a posted line on a director's account naming its person: its day and its base amount.
type named struct {
	day  time.Time
	base money.Money
}

// note keeps the lines of an entry that name a person, for what a later settlement may settle, and every line, for
// what a later account merge moves.
func (r *p1) note(e ledger.Entry) {
	for _, l := range e.Lines {
		r.posted = append(r.posted, postedLine{day: e.PostingDate, line: l})
	}
	for _, l := range e.Lines {
		if l.Party != uuid.Nil {
			k := [2]uuid.UUID{l.Party, l.AccountID}
			r.named[k] = append(r.named[k], named{e.PostingDate, l.Base})
		}
	}
}

// owed is what the company owes person on account at the end of day (README, "P1 readings"; FR-EQ-05): the credit,
// positive, of the lines naming them there in the entries posted before, dated on or before day.
func (r *p1) owed(person string, account uuid.UUID, day time.Time) money.Money {
	sum := money.Zero(r.base)
	for _, x := range r.named[[2]uuid.UUID{stableID("party", person), account}] {
		if !x.day.After(day) {
			sum, _ = sum.Sub(x.base)
		}
	}
	return sum
}

// within reports whether amount is at most what is owed person on account at the end of day.
func (r *p1) within(amount money.Money, person string, account uuid.UUID, day time.Time) bool {
	c, err := amount.Cmp(r.owed(person, account, day))
	return err == nil && c <= 0
}

// fits reports whether an entry can be stored. The database refuses an entry of fewer than two lines (INV-01), and in
// canonical form a P1 entry whose other legs round to nothing in base has one or none (README, "P1 readings"): such as
// a claim of USD 0.02 at 0.1, which owes S$0.00. A document that posts nothing of its own fits. Each source asks before
// it changes what later documents read, so a refused document leaves no trace, as in the oracle.
func (r *p1) fits(e ledger.Entry) bool {
	if len(e.Lines) == 0 {
		return true
	}
	// The database also refuses an entry all in one currency that does not balance in it (INV-01), as when every
	// base-currency leg rounds to nothing: a USD invoice of 0.10 at 0.1 whose GST is S$0.00 leaves USD 0.11 against 0.10.
	cur, sum := e.Lines[0].Amount.Currency(), money.Zero(e.Lines[0].Amount.Currency())
	for _, l := range e.Lines {
		if l.Amount.Currency() != cur {
			return len(canonical(e.Lines, r.codeOf, r.personOf, r.valueOf, r.base)) >= 2
		}
		sum, _ = sum.Add(l.Amount)
	}
	return sum.IsZero() && len(canonical(e.Lines, r.codeOf, r.personOf, r.valueOf, r.base)) >= 2
}

// fitsColumns reports whether every line's amount and base amount fit NUMERIC(20,4) (§11.3, ENG-44).
func fitsColumns(lines []ledger.Line) bool {
	for _, l := range lines {
		for _, x := range []money.Money{l.Amount, l.Base} {
			if _, err := money.ParseStored(x.String(), x.Currency()); err != nil {
				return false
			}
		}
	}
	return true
}

// canonical is a P1 entry's lines in canonical form (README, "P1 entries"): one line per account, currency, person and
// dimension set with the sums of its amounts and base amounts, the lines whose sums are both zero left out, sorted by
// account, then currency, then person (a line naming none first), then set (the lines with none first).
func canonical(lines []ledger.Line, codeOf, personOf map[uuid.UUID]string, valueOf valueNames, base money.Currency) []ledger.Line {
	type key struct{ code, cur, person, set string }
	sums := map[key]*ledger.Line{}
	var keys []key
	for _, l := range lines {
		k := key{codeOf[l.AccountID], l.Amount.Currency().Code, personOf[l.Party], valueOf.set(l.Values)}
		s, ok := sums[k]
		if !ok {
			s = &ledger.Line{AccountID: l.AccountID, Amount: money.Zero(l.Amount.Currency()), Base: money.Zero(base), Party: l.Party, Values: l.Values}
			sums[k] = s
			keys = append(keys, k)
		}
		s.Amount, _ = s.Amount.Add(l.Amount)
		s.Base, _ = s.Base.Add(l.Base)
	}
	slices.SortFunc(keys, func(a, b key) int {
		return cmp.Or(cmp.Compare(a.code, b.code), cmp.Compare(a.cur, b.cur), cmp.Compare(a.person, b.person), cmp.Compare(a.set, b.set))
	})
	out := make([]ledger.Line, 0, len(keys))
	for _, k := range keys {
		if s := sums[k]; !s.Amount.IsZero() || !s.Base.IsZero() {
			out = append(out, *s)
		}
	}
	return out
}

type bankTransactionDoc struct {
	Account      string `json:"account"`
	Currency     string `json:"currency"`
	Amount       string `json:"amount"`
	FXRate       string `json:"fx_rate"`
	FXRateSource string `json:"fx_rate_source"`
	Person       string `json:"person"` // the subscriber a part on share application monies names (B.7)
	Parts        []struct {
		Account      string `json:"account"`
		Amount       string `json:"amount"`
		GSTCode      string `json:"gst_code"`
		SpreadMonths int    `json:"spread_months"`
	} `json:"parts"`
}

// bankTransaction posts a categorised bank line (bank_transaction@1) and schedules its spread parts' releases.
func (r *p1) bankTransaction(i int, d Document, date time.Time) (ledger.Entry, bool) {
	var x bankTransactionDoc
	if json.Unmarshal(d.Raw, &x) != nil {
		return ledger.Entry{}, false
	}
	amount, ok := r.amount(x.Amount, x.Currency)
	if !ok {
		return ledger.Entry{}, false
	}
	rate, ok := r.rate(amount.Currency(), x.FXRate, x.FXRateSource)
	if !ok {
		return ledger.Entry{}, false
	}
	t := ledger.BankTransaction{ID: stableID("document", d.ID), Version: max(d.Version, 1), Date: date, Account: account(x.Account), Amount: amount, Rate: rate}
	if x.Person != "" { // the subscriber of the line's parts on share application monies, and only of them (rev 0.206)
		monies := false
		for _, p := range x.Parts {
			monies = monies || r.settings.Accounts[account(p.Account)].SubscriberAccount()
		}
		if !monies {
			return ledger.Entry{}, false
		}
		t.Party = stableID("party", x.Person)
	}
	for _, p := range x.Parts {
		a, ok := r.amount(p.Amount, x.Currency)
		if !ok {
			return ledger.Entry{}, false
		}
		gst, ok := r.gstRate(p.GSTCode, date)
		if !ok {
			return ledger.Entry{}, false
		}
		t.Parts = append(t.Parts, ledger.BankPart{AccountID: account(p.Account), Amount: a, GSTRate: gst, Months: p.SpreadMonths})
	}
	e, err := ledger.PostBankTransaction(t, r.settings)
	if err != nil || !r.fits(e) {
		return ledger.Entry{}, false
	}
	releases, err := ledger.BankTransactionReleases(t, e, r.settings)
	if err != nil {
		return ledger.Entry{}, false
	}
	post := ledger.PostBankOutRelease
	if amount.Sign() > 0 {
		post = ledger.PostBankInRelease
	}
	if !r.schedule(i, d.ID, releases, post) {
		return ledger.Entry{}, false
	}
	return e, true
}

type transferLeg struct {
	Account      string `json:"account"`
	Currency     string `json:"currency"`
	Amount       string `json:"amount"`
	FXRate       string `json:"fx_rate"`
	FXRateSource string `json:"fx_rate_source"`
}

type bankTransferDoc struct {
	From transferLeg `json:"from"`
	To   transferLeg `json:"to"`
	Fee  *string     `json:"fee"`
}

// bankTransfer posts a transfer between money accounts (bank_transfer@1), its fee to the bank charges setting.
func (r *p1) bankTransfer(d Document, date time.Time) (ledger.Entry, bool) {
	var x bankTransferDoc
	if json.Unmarshal(d.Raw, &x) != nil {
		return ledger.Entry{}, false
	}
	leg := func(l transferLeg) (money.Money, *money.Rate, bool) {
		a, ok := r.amount(l.Amount, l.Currency)
		if !ok {
			return money.Money{}, nil, false
		}
		rate, ok := r.rate(a.Currency(), l.FXRate, l.FXRateSource)
		return a, rate, ok
	}
	paid, fromRate, ok1 := leg(x.From)
	received, toRate, ok2 := leg(x.To)
	if !ok1 || !ok2 {
		return ledger.Entry{}, false
	}
	t := ledger.BankTransfer{ID: stableID("document", d.ID), Date: date, From: account(x.From.Account), Paid: paid, FromRate: fromRate,
		To: account(x.To.Account), Received: received, ToRate: toRate}
	t.Charges, _ = r.setting("bank_charges")
	if x.Fee != nil {
		fee, ok := r.amount(*x.Fee, x.From.Currency)
		if !ok {
			return ledger.Entry{}, false
		}
		t.Fee = &fee
	}
	e, err := ledger.PostBankTransfer(t, r.settings)
	return e, err == nil
}
