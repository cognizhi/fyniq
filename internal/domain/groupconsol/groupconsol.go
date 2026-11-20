// Package groupconsol prepares a consolidation run (PRD-001 FR-ENT-05, FR-ENT-06; p3-multi-entity design D4 to D6): it turns what the parent holds (its own figures, the figures it
// read from each linked entity, its chart, the account mappings, the translation rates and the elimination pairs) into the input of the consolidation engine, names what blocks the
// run in plain words (an account with a balance and no mapping, a counterparty that is no entity of the run), and freezes the input and the result as the documents a run keeps.
// Everything here works in exact decimals; the calculation itself is package consolidation, which has an oracle half.
package groupconsol

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/consolidation"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Codes of the plain refusals (ENG-23).
const (
	CodeUnmapped     = "CONSOL-03"
	CodeCounterparty = "CONSOL-04"
	CodeNothing      = "CONSOL-05"
)

// ParentKey is the parent's own key in a run.
const ParentKey = "parent"

// Account is an account of the parent's chart, which is the group's chart.
type Account struct {
	ID         uuid.UUID
	Code, Name string
	Type       string
}

// Source is one organisation's figures in a run: the parent's own, or an entity's as read through its link.
type Source struct {
	Key      string // ParentKey, or the entity row's ID
	Label    string
	Row      uuid.UUID // the entity row; uuid.Nil for the parent
	Currency money.Currency
	Revision int64
	Periods  []storage.EntityPeriod
	Lines    []storage.EntityFigureLine
}

// Setup is the parent's set-up.
type Setup struct {
	Chart    []Account
	Mappings []storage.GroupMapping
	Rates    []storage.GroupRate // newest entry of a month and currency first, as the store lists them
	Pairs    []storage.GroupPair
}

// Problem is one thing that blocks a run, with the words that say how to fix it.
type Problem struct {
	Code, Entity, Account, Message, Fix string
}

// Prepared is a run's input with the documents it keeps.
type Prepared struct {
	Input       consolidation.Input
	Problems    []Problem
	Provisional bool
	Inputs      Inputs
}

// Inputs is the document a run keeps of what it read: each organisation's figures as read, the rates and the pairs. It is the whole of what the result is worked from.
type Inputs struct {
	GroupCurrency string        `json:"group_currency"`
	From          string        `json:"from"`
	To            string        `json:"to"`
	Sources       []InputSource `json:"sources"`
	Rates         []InputRate   `json:"rates"`
	Pairs         []InputPair   `json:"pairs"`
}

// InputSource is one organisation as the run read it.
type InputSource struct {
	Key      string        `json:"key"`
	Label    string        `json:"label"`
	Currency string        `json:"currency"`
	Revision int64         `json:"ledger_revision"`
	Periods  []InputPeriod `json:"periods"`
	Lines    []InputLine   `json:"lines"`
}

// InputPeriod is the state of a period when the run read it.
type InputPeriod struct {
	Month  string `json:"month"`
	Status string `json:"status"`
}

// InputLine is an account's net movement in a month, on the parent's chart, in the organisation's currency.
type InputLine struct {
	Account      string `json:"account"`
	Type         string `json:"type"`
	Counterparty string `json:"counterparty,omitempty"`
	Month        string `json:"month"`
	Amount       string `json:"amount"`
}

// InputRate is a rate used.
type InputRate struct {
	Currency string `json:"currency"`
	Month    string `json:"month"`
	Closing  string `json:"closing"`
	Average  string `json:"average"`
	Note     string `json:"note"`
}

// InputPair is a pair of mirror accounts, by code.
type InputPair struct {
	A string `json:"a"`
	B string `json:"b"`
}

func monthKey(t interface{ Format(string) string }) string { return t.Format("2006-01") }

// Prepare maps, collects the rates and builds the engine's input for the months from through to, naming every problem it finds instead of stopping at the first.
func Prepare(group money.Currency, from, to string, sources []Source, set Setup) (Prepared, error) {
	out := Prepared{Input: consolidation.Input{Group: group, From: from, To: to}, Inputs: Inputs{GroupCurrency: group.Code, From: from, To: to}}
	chart := map[uuid.UUID]Account{}
	byCode := map[string]Account{}
	for _, a := range set.Chart {
		chart[a.ID] = a
		byCode[a.Code] = a
	}
	keys := map[uuid.UUID]string{}
	live := map[string]bool{}
	for _, s := range sources {
		live[s.Key] = true
		if s.Row != uuid.Nil {
			keys[s.Row] = s.Key
		}
	}
	type mapKey struct {
		row  uuid.UUID
		code string
	}
	maps := map[mapKey]storage.GroupMapping{}
	for _, m := range set.Mappings {
		maps[mapKey{m.EntityRow, m.EntityAccountCode}] = m
	}
	for _, s := range sources {
		is := InputSource{Key: s.Key, Label: s.Label, Currency: s.Currency.Code, Revision: s.Revision}
		for _, p := range s.Periods {
			is.Periods = append(is.Periods, InputPeriod{Month: monthKey(p.Month), Status: p.Status})
			if m := monthKey(p.Month); m >= from && m <= to && p.Status != "closed" {
				out.Provisional = true
			}
		}
		ent := consolidation.Entity{ID: s.Key, Currency: s.Currency}
		unmapped := map[string]bool{} // an account with any movement and no mapping, by code
		for _, l := range s.Lines {
			net, err := money.ParseStored(string(l.Net), s.Currency)
			if err != nil {
				return out, err
			}
			code, typ, cp := l.Code, "", ""
			mp, mapped := maps[mapKey{s.Row, l.Code}]
			if s.Row == uuid.Nil { // the parent's own accounts are the group's: no mapping unless it names a counterparty
				a, ok := byCode[l.Code]
				if !ok {
					return out, fmt.Errorf("the parent's account %s is not in its own chart", l.Code)
				}
				typ = a.Type
				if mapped {
					g := chart[mp.GroupAccountID]
					code, typ = g.Code, g.Type
				}
			} else if mapped {
				g, ok := chart[mp.GroupAccountID]
				if !ok {
					out.Problems = append(out.Problems, Problem{Code: CodeUnmapped, Entity: s.Label, Account: l.Code, Message: "its mapping names an account that is no longer in the parent's chart",
						Fix: "Map the account to an account of the parent's chart"})
					continue
				}
				code, typ = g.Code, g.Type
			} else {
				if !net.IsZero() {
					unmapped[l.Code] = true
				}
				continue
			}
			if mapped {
				switch {
				case mp.CounterpartyParent:
					cp = ParentKey
				case mp.CounterpartyRow != uuid.Nil:
					cp = keys[mp.CounterpartyRow]
				}
				if cp != "" && !live[cp] {
					out.Problems = append(out.Problems, Problem{Code: CodeCounterparty, Entity: s.Label, Account: l.Code,
						Message: "its mapping names a counterparty that is not an entity of this run: its link has ended or it has not accepted",
						Fix:     "Map the account again, naming an entity that is linked, or none"})
					continue
				}
				if cp == s.Key {
					cp = ""
				}
			}
			ent.Lines = append(ent.Lines, consolidation.Line{Account: code, Type: typ, Counterparty: cp, Month: monthKey(l.Month), Amount: net})
			is.Lines = append(is.Lines, InputLine{Account: code, Type: typ, Counterparty: cp, Month: monthKey(l.Month), Amount: net.String()})
		}
		codes := make([]string, 0, len(unmapped))
		for code := range unmapped {
			codes = append(codes, code)
		}
		sort.Strings(codes)
		for _, code := range codes { // an account with a balance and no mapping blocks the run (FR-ENT-05)
			out.Problems = append(out.Problems, Problem{Code: CodeUnmapped, Entity: s.Label, Account: code, Message: "it has figures and is not mapped to an account of the parent's chart",
				Fix: "Map it to an account of the parent's chart, in the group's set-up"})
		}
		out.Input.Entities = append(out.Input.Entities, ent)
		out.Inputs.Sources = append(out.Inputs.Sources, is)
	}
	// The newest entry of each month and currency, in the order the store lists them.
	got := map[string]bool{}
	for _, r := range set.Rates {
		k := r.Currency + "|" + monthKey(r.Month)
		if got[k] || monthKey(r.Month) > to {
			continue
		}
		got[k] = true
		cur, err := money.Lookup(r.Currency)
		if err != nil {
			continue
		}
		closing, err1 := money.ParseRate(string(r.Closing), cur, group, r.Note)
		average, err2 := money.ParseRate(string(r.Average), cur, group, r.Note)
		if err1 != nil || err2 != nil {
			continue
		}
		out.Input.Rates = append(out.Input.Rates, consolidation.Rate{Currency: r.Currency, Month: monthKey(r.Month), Closing: closing, Average: average})
		out.Inputs.Rates = append(out.Inputs.Rates, InputRate{Currency: r.Currency, Month: monthKey(r.Month), Closing: closing.String(), Average: average.String(), Note: r.Note})
	}
	sort.Slice(out.Inputs.Rates, func(i, j int) bool {
		a, b := out.Inputs.Rates[i], out.Inputs.Rates[j]
		return a.Currency+a.Month < b.Currency+b.Month
	})
	for _, p := range set.Pairs {
		a, b := chart[p.A], chart[p.B]
		if a.Code == "" || b.Code == "" {
			continue
		}
		out.Input.Pairs = append(out.Input.Pairs, consolidation.Pair{A: a.Code, B: b.Code})
		out.Inputs.Pairs = append(out.Inputs.Pairs, InputPair{A: a.Code, B: b.Code})
	}
	sort.SliceStable(out.Problems, func(i, j int) bool {
		a, b := out.Problems[i], out.Problems[j]
		return a.Entity+"|"+a.Account < b.Entity+"|"+b.Account
	})
	if len(out.Input.Entities) == 0 {
		out.Problems = append(out.Problems, Problem{Code: CodeNothing, Message: "there is nothing to consolidate", Fix: "Invite an entity and have its Owner accept first"})
	}
	return out, nil
}

// Result is the document a run keeps of its result: every figure as exact decimal text.
type Result struct {
	Entities     []ResultEntity      `json:"entities"`
	Eliminations []ResultElimination `json:"eliminations"`
	Differences  []ResultDifference  `json:"differences"`
	Consolidated []ResultTotal       `json:"consolidated"`
	Reserve      string              `json:"reserve"`
}

// ResultEntity is an entity's translated lines and its translation reserve.
type ResultEntity struct {
	Key     string       `json:"key"`
	Reserve string       `json:"reserve"`
	Lines   []ResultLine `json:"lines"`
}

// ResultLine is a translated line.
type ResultLine struct {
	Account      string `json:"account"`
	Counterparty string `json:"counterparty,omitempty"`
	Section      string `json:"section"`
	Amount       string `json:"amount"`
}

// ResultElimination is an amount taken off an entity's line.
type ResultElimination struct {
	Entity string `json:"entity"`
	ResultLine
}

// ResultDifference is what two entities do not agree on for one pair.
type ResultDifference struct {
	DebitEntity  string `json:"debit_entity"`
	CreditEntity string `json:"credit_entity"`
	A            string `json:"a"`
	B            string `json:"b"`
	Debit        string `json:"debit"`
	Credit       string `json:"credit"`
	Matched      string `json:"matched"`
	Difference   string `json:"difference"`
}

// ResultTotal is a consolidated account in a section.
type ResultTotal struct {
	Account string `json:"account"`
	Section string `json:"section"`
	Amount  string `json:"amount"`
}

// Freeze writes a run's two documents.
func Freeze(p Prepared, r consolidation.Result) (inputs, result []byte, err error) {
	if inputs, err = json.Marshal(p.Inputs); err != nil {
		return nil, nil, err
	}
	out := Result{Entities: []ResultEntity{}, Eliminations: []ResultElimination{}, Differences: []ResultDifference{}, Consolidated: []ResultTotal{}, Reserve: r.Reserve.String()}
	for _, e := range r.Entities {
		re := ResultEntity{Key: e.ID, Reserve: e.Reserve.String(), Lines: []ResultLine{}}
		for _, l := range e.Lines {
			re.Lines = append(re.Lines, ResultLine{Account: l.Account, Counterparty: l.Counterparty, Section: l.Section, Amount: l.Amount.String()})
		}
		out.Entities = append(out.Entities, re)
	}
	for _, e := range r.Eliminations {
		out.Eliminations = append(out.Eliminations, ResultElimination{Entity: e.Entity, ResultLine: ResultLine{Account: e.Account, Counterparty: e.Counterparty, Section: e.Section, Amount: e.Amount.String()}})
	}
	for _, d := range r.Differences {
		out.Differences = append(out.Differences, ResultDifference{DebitEntity: d.DebitEntity, CreditEntity: d.CreditEntity, A: d.A, B: d.B, Debit: d.Debit.String(), Credit: d.Credit.String(),
			Matched: d.Matched.String(), Difference: d.Difference.String()})
	}
	for _, t := range r.Consolidated {
		out.Consolidated = append(out.Consolidated, ResultTotal{Account: t.Account, Section: t.Section, Amount: t.Amount.String()})
	}
	result, err = json.Marshal(out)
	return inputs, result, err
}
