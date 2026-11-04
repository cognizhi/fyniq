// Package differential runs the posting engine on a scenario and compares its results with the
// independent oracle's, to the cent (PRD-001 §16.1, QA-ENG-01, QA-ENG-02). The scenario and result
// formats are the only interface between the two implementations: the oracle (Python, `oracle/`)
// reads the same scenario files and writes the same result format, and neither side reads the
// other's code. The formats are described in test/differential/ (schemas from the oracle's change).
package differential

import (
	"cmp"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
)

// The format names.
const (
	ScenarioSchema = "fyniq-scenario@1"
	ResultSchema   = "fyniq-scenario-result@1"
)

// Scenario is one input: accounts, documents in posting order, and reversals.
type Scenario struct {
	Schema       string         `json:"schema"`
	Name         string         `json:"name"`
	BaseCurrency string         `json:"base_currency"`
	Currencies   map[string]int `json:"currencies"`
	Accounts     []Account      `json:"accounts"`
	// People are the directors and shareholders a line on a director's account may name (FR-EQ-01).
	People []string `json:"people,omitempty"`
	// Statements and KPIs ask for the statements and the KPIs (README, "Statements" and "KPIs").
	Statements bool            `json:"statements,omitempty"`
	KPIs       json.RawMessage `json:"kpis,omitempty"`
	// AsOf is the business date and Settings the organisation's settings, which a scenario with a P1 document has
	// (README, "Settings and as_of").
	AsOf      string     `json:"as_of,omitempty"`
	Settings  *Settings  `json:"settings,omitempty"`
	Documents []Document `json:"documents"`
	Reversals []Reversal `json:"reversals"`
}

// Account is one account of the scenario's chart. Role "share-capital" marks an account a manual
// journal may not post to (INV-13); "director-loan" and "deferred-remuneration" a director's account,
// whose lines name their person and origin (FR-EQ-01, B.7).
type Account struct {
	Code   string `json:"code"`
	Type   string `json:"type"`
	Group  bool   `json:"group"`
	Active bool   `json:"active"`
	Role   string `json:"role,omitempty"`
}

// Document is one document version: a manual journal, or a P1 source's document, whose own shape Raw holds.
type Document struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Version int    `json:"version"`
	Date    string `json:"date"`
	Memo    string `json:"memo"`
	Lines   []Line `json:"lines"`
	// Raw is a P1 document as written, which its source reads its own shape from (README, "P1 sources").
	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON reads a manual journal into the fields above; a P1 document fills only its type, ID, version and date
// and keeps the rest as written in Raw, since its lines have shapes of their own.
func (d *Document) UnmarshalJSON(b []byte) error {
	var head struct {
		Type    string `json:"type"`
		ID      string `json:"id"`
		Version int    `json:"version"`
		Date    string `json:"date"`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return err
	}
	if head.Type == ledger.SourceManualJournal {
		type plain Document
		var p plain
		if err := json.Unmarshal(b, &p); err != nil {
			return err
		}
		*d = Document(p)
		return nil
	}
	*d = Document{Type: head.Type, ID: head.ID, Version: head.Version, Date: head.Date, Raw: slices.Clone(b)}
	return nil
}

// Line is one journal line: a signed amount, and a rate when not in the base currency.
type Line struct {
	Account      string `json:"account"`
	Currency     string `json:"currency"`
	Amount       string `json:"amount"`
	FXRate       string `json:"fx_rate,omitempty"`
	FXRateSource string `json:"fx_rate_source,omitempty"`
	Party        string `json:"party,omitempty"`  // a director's account's person, one of the scenario's people
	Origin       string `json:"origin,omitempty"` // where that amount came from
}

// Reversal reverses the posted entry of a document on a date.
type Reversal struct {
	Document string `json:"document"`
	Date     string `json:"date"`
}

// Result is what an implementation produced from a scenario.
type Result struct {
	Schema       string                       `json:"schema"`
	Name         string                       `json:"name"`
	Entries      []Entry                      `json:"entries"`
	Rejected     []string                     `json:"rejected"`
	TrialBalance map[string]map[string]string `json:"trial_balance"` // month → account → base total
	// People is each person's base total on each director's account (FR-EQ-01); absent when no line names one.
	People map[string]map[string]string `json:"people,omitempty"`
}

// Entry is one posted entry.
type Entry struct {
	Document    string      `json:"document"`
	Kind        string      `json:"kind"` // post or reversal
	PostingDate string      `json:"posting_date"`
	Lines       []EntryLine `json:"lines"`
}

// EntryLine is one posted line.
type EntryLine struct {
	Account    string `json:"account"`
	Currency   string `json:"currency"`
	Amount     string `json:"amount"`
	BaseAmount string `json:"base_amount"`
	Party      string `json:"party,omitempty"`
}

// stableID turns a scenario's name for something into a fixed UUID.
func stableID(kind, name string) uuid.UUID {
	return uuid.NewHash(sha1.New(), uuid.NameSpaceURL, []byte("fyniq-scenario:"+kind+":"+name), 5)
}

// RunEngine posts a scenario through the Go engine (package ledger), with no database: the same
// pure functions the journal service and the P1 sources' services call. A document the rules refuse
// goes to Rejected. The scheduler's entries (a spread's releases) follow every document and reversal.
func RunEngine(s Scenario) (Result, error) {
	if s.Schema != ScenarioSchema {
		return Result{}, fmt.Errorf("scenario %q has schema %q; want %q", s.Name, s.Schema, ScenarioSchema)
	}
	for code, units := range s.Currencies {
		c, err := money.Lookup(code)
		if err != nil {
			return Result{}, fmt.Errorf("scenario %q: %w", s.Name, err)
		}
		if c.MinorUnits != units {
			return Result{}, fmt.Errorf("scenario %q declares %s with %d minor units; ISO 4217 has %d", s.Name, code, units, c.MinorUnits)
		}
	}
	base, err := money.Lookup(s.BaseCurrency)
	if err != nil {
		return Result{}, fmt.Errorf("scenario %q: base currency: %w", s.Name, err)
	}
	settings := ledger.Settings{Base: base, Accounts: map[uuid.UUID]ledger.Account{}, People: map[uuid.UUID]bool{}}
	codeOf, personOf := map[uuid.UUID]string{}, map[uuid.UUID]string{}
	for _, a := range s.Accounts {
		id := stableID("account", a.Code)
		settings.Accounts[id] = ledger.Account{ID: id, Code: a.Code, Group: a.Group, Active: a.Active, ShareCapital: a.Role == "share-capital",
			Role: a.Role, Type: a.Type}
		codeOf[id] = a.Code
	}
	for _, p := range s.People {
		id := stableID("party", p)
		settings.People[id], personOf[id] = true, p
	}
	applySettings(s, settings)
	sources, err := newP1(s, base, settings)
	if err != nil {
		return Result{}, fmt.Errorf("scenario %q: %w", s.Name, err)
	}

	res := Result{Schema: ResultSchema, Name: s.Name, Entries: []Entry{}, Rejected: []string{}, TrialBalance: map[string]map[string]string{}}
	totals := map[string]map[string]money.Money{}
	people := map[string]map[string]money.Money{}
	// record writes an entry: a manual journal's lines as entered, any other in canonical form (README, "P1 entries").
	record := func(doc, kind string, e ledger.Entry, p1 bool) {
		out := Entry{Document: doc, Kind: kind, PostingDate: e.PostingDate.Format(time.DateOnly)}
		month := e.PostingDate.Format("2006-01")
		if totals[month] == nil {
			totals[month] = map[string]money.Money{}
		}
		lines := e.Lines
		if p1 {
			lines = canonical(lines, codeOf, base)
		}
		for _, l := range lines {
			code := codeOf[l.AccountID]
			line := EntryLine{Account: code, Currency: l.Amount.Currency().Code, Amount: l.Amount.String(), BaseAmount: l.Base.String()}
			if l.Party != uuid.Nil {
				line.Party = personOf[l.Party]
				if people[line.Party] == nil {
					people[line.Party] = map[string]money.Money{}
				}
				sum, ok := people[line.Party][code]
				if !ok {
					sum = money.Zero(base)
				}
				people[line.Party][code], _ = sum.Add(l.Base)
			}
			out.Lines = append(out.Lines, line)
			cur, ok := totals[month][code]
			if !ok {
				cur = money.Zero(base)
			}
			totals[month][code], _ = cur.Add(l.Base) // both in the base currency
		}
		res.Entries = append(res.Entries, out)
	}

	posted := map[string]ledger.Entry{}
	for i, d := range s.Documents {
		if d.Type != ledger.SourceManualJournal {
			e, ok := sources.post(i, d)
			// The database refuses an entry of fewer than two lines (db/migrations/000004, INV-01), such as one whose
			// lines but one round to nothing in base (README, "P1 readings").
			if !ok || len(e.Lines) == 1 {
				res.Rejected = append(res.Rejected, d.ID)
				continue
			}
			if len(e.Lines) > 0 {
				record(d.ID, "post", e, true)
			}
			continue
		}
		j, ok := journalOf(d, base, s.Currencies)
		if !ok {
			res.Rejected = append(res.Rejected, d.ID)
			continue
		}
		e, err := ledger.PostJournal(j, settings)
		if err != nil {
			res.Rejected = append(res.Rejected, d.ID)
			continue
		}
		posted[d.ID] = e
		record(d.ID, "post", e, false)
	}
	for _, r := range s.Reversals {
		e, ok := posted[r.Document]
		date, err := time.Parse(time.DateOnly, r.Date)
		if !ok || err != nil {
			res.Rejected = append(res.Rejected, r.Document+"/reversal")
			continue
		}
		rev, err := ledger.Reverse(e, date, "")
		if err != nil {
			res.Rejected = append(res.Rejected, r.Document+"/reversal")
			continue
		}
		delete(posted, r.Document) // a document is reversed once
		record(r.Document, "reversal", rev, false)
	}
	for _, x := range sources.scheduledEntries() {
		record(x.doc, x.kind, x.entry, true)
	}
	for month, accounts := range totals {
		res.TrialBalance[month] = map[string]string{}
		for code, total := range accounts {
			if !total.IsZero() {
				res.TrialBalance[month][code] = total.String()
			}
		}
	}
	for person, accounts := range people {
		for code, total := range accounts {
			if total.IsZero() {
				continue
			}
			if res.People == nil {
				res.People = map[string]map[string]string{}
			}
			if res.People[person] == nil {
				res.People[person] = map[string]string{}
			}
			res.People[person][code] = total.String()
		}
	}
	return res, nil
}

// journalOf reads a scenario document into the engine's form, with the format's rules for lines
// (test/differential/README.md): each line's currency is one the scenario declares; a base-currency
// line has no rate, and any other line has a rate and its source; amounts and rates are parsed with
// the strict rules of ENG-44. A line that breaks them makes the document rejected, as the API would
// refuse it.
func journalOf(d Document, base money.Currency, declared map[string]int) (ledger.Journal, bool) {
	date, err := time.Parse(time.DateOnly, d.Date)
	if err != nil {
		return ledger.Journal{}, false
	}
	j := ledger.Journal{ID: stableID("document", d.ID), Version: max(d.Version, 1), Date: date, Memo: d.Memo}
	for _, l := range d.Lines {
		if _, ok := declared[l.Currency]; !ok {
			return ledger.Journal{}, false
		}
		cur, err := money.Lookup(l.Currency)
		if err != nil {
			return ledger.Journal{}, false
		}
		isBase, hasRate := l.Currency == base.Code, l.FXRate != "" || l.FXRateSource != ""
		if isBase && hasRate || !isBase && (l.FXRate == "" || l.FXRateSource == "") {
			return ledger.Journal{}, false
		}
		amount, err := money.Parse(l.Amount, cur)
		if err != nil {
			return ledger.Journal{}, false
		}
		line := ledger.JournalLine{AccountID: stableID("account", l.Account), Amount: amount, Origin: l.Origin}
		if l.Party != "" {
			line.Party = stableID("party", l.Party)
		}
		if l.FXRate != "" {
			r, err := money.ParseRate(l.FXRate, cur, base, l.FXRateSource)
			if err != nil {
				return ledger.Journal{}, false
			}
			line.Rate = &r
		}
		j.Lines = append(j.Lines, line)
	}
	return j, true
}

// Compare returns nil when two results agree to the cent, or an error naming the first difference:
// rejected documents first, then each entry's lines, then the trial balance month by month. A
// trial-balance account that is missing counts as zero.
func Compare(engine, oracle Result) error {
	if !slices.Equal(engine.Rejected, oracle.Rejected) {
		return fmt.Errorf("rejected documents differ: engine %v, oracle %v", engine.Rejected, oracle.Rejected)
	}
	if len(engine.Entries) != len(oracle.Entries) {
		return fmt.Errorf("entries differ in number: engine %d, oracle %d", len(engine.Entries), len(oracle.Entries))
	}
	for i, e := range engine.Entries {
		o := oracle.Entries[i]
		at := fmt.Sprintf("entry %d (%s %s)", i+1, e.Kind, e.Document)
		if e.Document != o.Document || e.Kind != o.Kind || e.PostingDate != o.PostingDate {
			return fmt.Errorf("%s: engine %s %s on %s, oracle %s %s on %s", at, e.Kind, e.Document, e.PostingDate, o.Kind, o.Document, o.PostingDate)
		}
		if len(e.Lines) != len(o.Lines) {
			return fmt.Errorf("%s: engine has %d lines, oracle %d", at, len(e.Lines), len(o.Lines))
		}
		for k, l := range e.Lines {
			if ol := o.Lines[k]; !sameLine(l, ol) {
				return fmt.Errorf("%s line %d: engine %s %s %s base %s person %q, oracle %s %s %s base %s person %q",
					at, k+1, l.Account, l.Currency, l.Amount, l.BaseAmount, l.Party, ol.Account, ol.Currency, ol.Amount, ol.BaseAmount, ol.Party)
			}
		}
	}
	months := map[string]bool{}
	for m := range engine.TrialBalance {
		months[m] = true
	}
	for m := range oracle.TrialBalance {
		months[m] = true
	}
	for _, m := range sortedKeys(months) {
		accounts := map[string]bool{}
		for a := range engine.TrialBalance[m] {
			accounts[a] = true
		}
		for a := range oracle.TrialBalance[m] {
			accounts[a] = true
		}
		for _, a := range sortedKeys(accounts) {
			ev, ov := engine.TrialBalance[m][a], oracle.TrialBalance[m][a]
			if !sameAmount(ev, ov) {
				return fmt.Errorf("trial balance %s account %s: engine %s, oracle %s", m, a, cmp.Or(ev, "0"), cmp.Or(ov, "0"))
			}
		}
	}
	// Each person's totals on the director's accounts (FR-EQ-01): a missing person or account reads as zero.
	persons := map[string]bool{}
	for p := range engine.People {
		persons[p] = true
	}
	for p := range oracle.People {
		persons[p] = true
	}
	for _, p := range sortedKeys(persons) {
		accounts := map[string]bool{}
		for a := range engine.People[p] {
			accounts[a] = true
		}
		for a := range oracle.People[p] {
			accounts[a] = true
		}
		for _, a := range sortedKeys(accounts) {
			if ev, ov := engine.People[p][a], oracle.People[p][a]; !sameAmount(ev, ov) {
				return fmt.Errorf("person %s account %s: engine %s, oracle %s", p, a, cmp.Or(ev, "0"), cmp.Or(ov, "0"))
			}
		}
	}
	return nil
}

func sameLine(a, b EntryLine) bool {
	return a.Account == b.Account && a.Currency == b.Currency && sameAmount(a.Amount, b.Amount) && sameAmount(a.BaseAmount, b.BaseAmount) &&
		a.Party == b.Party
}

// sameAmount compares two decimal strings exactly, so "1.50" equals "1.5" and "" equals "0".
func sameAmount(a, b string) bool {
	da, _, err1 := apd.NewFromString(cmp.Or(a, "0"))
	db, _, err2 := apd.NewFromString(cmp.Or(b, "0"))
	if err1 != nil || err2 != nil || da.Form != apd.Finite || db.Form != apd.Finite {
		return a == b
	}
	return da.Cmp(db) == 0
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
