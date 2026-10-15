// Package differential runs the posting engine on a scenario and compares its results with the
// independent oracle's, to the cent (PRD-001 §16.1, QA-ENG-01, QA-ENG-02). The scenario and result
// formats are the only interface between the two implementations: the oracle (Python, `oracle/`)
// reads the same scenario files and writes the same result format, and neither side reads the
// other's code. The formats are described in test/differential/ (schemas from the oracle's change).
package differential

import (
	"cmp"
	"crypto/sha1"
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
	Documents    []Document     `json:"documents"`
	Reversals    []Reversal     `json:"reversals"`
}

// Account is one account of the scenario's chart.
type Account struct {
	Code   string `json:"code"`
	Type   string `json:"type"`
	Group  bool   `json:"group"`
	Active bool   `json:"active"`
}

// Document is one manual journal version.
type Document struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Version int    `json:"version"`
	Date    string `json:"date"`
	Memo    string `json:"memo"`
	Lines   []Line `json:"lines"`
}

// Line is one journal line: a signed amount, and a rate when not in the base currency.
type Line struct {
	Account      string `json:"account"`
	Currency     string `json:"currency"`
	Amount       string `json:"amount"`
	FXRate       string `json:"fx_rate,omitempty"`
	FXRateSource string `json:"fx_rate_source,omitempty"`
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
}

// stableID turns a scenario's name for something into a fixed UUID.
func stableID(kind, name string) uuid.UUID {
	return uuid.NewHash(sha1.New(), uuid.NameSpaceURL, []byte("fyniq-scenario:"+kind+":"+name), 5)
}

// RunEngine posts a scenario through the Go engine (package ledger), with no database: the same
// pure functions the journal service calls. A document the rules refuse goes to Rejected.
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
	settings := ledger.Settings{Base: base, Accounts: map[uuid.UUID]ledger.Account{}}
	codeOf := map[uuid.UUID]string{}
	for _, a := range s.Accounts {
		id := stableID("account", a.Code)
		settings.Accounts[id] = ledger.Account{ID: id, Code: a.Code, Group: a.Group, Active: a.Active}
		codeOf[id] = a.Code
	}

	res := Result{Schema: ResultSchema, Name: s.Name, Rejected: []string{}, TrialBalance: map[string]map[string]string{}}
	totals := map[string]map[string]money.Money{}
	record := func(doc, kind string, e ledger.Entry) {
		out := Entry{Document: doc, Kind: kind, PostingDate: e.PostingDate.Format(time.DateOnly)}
		month := e.PostingDate.Format("2006-01")
		if totals[month] == nil {
			totals[month] = map[string]money.Money{}
		}
		for _, l := range e.Lines {
			code := codeOf[l.AccountID]
			out.Lines = append(out.Lines, EntryLine{Account: code, Currency: l.Amount.Currency().Code, Amount: l.Amount.String(), BaseAmount: l.Base.String()})
			cur, ok := totals[month][code]
			if !ok {
				cur = money.Zero(base)
			}
			totals[month][code], _ = cur.Add(l.Base) // both in the base currency
		}
		res.Entries = append(res.Entries, out)
	}

	posted := map[string]ledger.Entry{}
	for _, d := range s.Documents {
		j, ok := journalOf(d, base)
		if !ok || d.Type != "manual_journal" {
			res.Rejected = append(res.Rejected, d.ID)
			continue
		}
		e, err := ledger.PostJournal(j, settings)
		if err != nil {
			res.Rejected = append(res.Rejected, d.ID)
			continue
		}
		posted[d.ID] = e
		record(d.ID, "post", e)
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
		record(r.Document, "reversal", rev)
	}
	for month, accounts := range totals {
		res.TrialBalance[month] = map[string]string{}
		for code, total := range accounts {
			if !total.IsZero() {
				res.TrialBalance[month][code] = total.String()
			}
		}
	}
	return res, nil
}

// journalOf reads a scenario document into the engine's form. Amounts and rates are parsed with the
// strict rules of ENG-44; one that fails makes the document rejected, as the API would refuse it.
func journalOf(d Document, base money.Currency) (ledger.Journal, bool) {
	date, err := time.Parse(time.DateOnly, d.Date)
	if err != nil {
		return ledger.Journal{}, false
	}
	j := ledger.Journal{ID: stableID("document", d.ID), Version: max(d.Version, 1), Date: date, Memo: d.Memo}
	for _, l := range d.Lines {
		cur, err := money.Lookup(l.Currency)
		if err != nil {
			return ledger.Journal{}, false
		}
		amount, err := money.Parse(l.Amount, cur)
		if err != nil {
			return ledger.Journal{}, false
		}
		line := ledger.JournalLine{AccountID: stableID("account", l.Account), Amount: amount}
		if l.FXRate != "" {
			r, err := money.ParseRate(l.FXRate, cur, base, cmp.Or(l.FXRateSource, "scenario"))
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
				return fmt.Errorf("%s line %d: engine %s %s %s base %s, oracle %s %s %s base %s",
					at, k+1, l.Account, l.Currency, l.Amount, l.BaseAmount, ol.Account, ol.Currency, ol.Amount, ol.BaseAmount)
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
	return nil
}

func sameLine(a, b EntryLine) bool {
	return a.Account == b.Account && a.Currency == b.Currency && sameAmount(a.Amount, b.Amount) && sameAmount(a.BaseAmount, b.BaseAmount)
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
