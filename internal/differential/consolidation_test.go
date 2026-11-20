package differential

import (
	"bytes"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	schemas "github.com/cognizhi/fyniq/test/differential"

	"github.com/cognizhi/fyniq/internal/domain/consolidation"
	"github.com/cognizhi/fyniq/internal/domain/money"
)

type consolidationCase struct {
	Name       string `json:"name"`
	Group      string `json:"group_currency"`
	GroupUnits int    `json:"group_minor_units"`
	From       string `json:"from"`
	To         string `json:"to"`
	Rates      []struct {
		Currency string `json:"currency"`
		Month    string `json:"month"`
		Closing  string `json:"closing"`
		Average  string `json:"average"`
	} `json:"rates"`
	Entities []struct {
		ID         string `json:"id"`
		Currency   string `json:"currency"`
		MinorUnits int    `json:"minor_units"`
		Lines      []struct {
			Account      string `json:"account"`
			Type         string `json:"type"`
			Counterparty string `json:"counterparty"`
			Month        string `json:"month"`
			Amount       string `json:"amount"`
		} `json:"lines"`
	} `json:"entities"`
	Pairs []struct {
		A string `json:"a"`
		B string `json:"b"`
	} `json:"pairs"`
	Want struct {
		Entities []struct {
			ID      string   `json:"id"`
			Lines   []string `json:"lines"`
			Reserve string   `json:"reserve"`
		} `json:"entities"`
		Eliminations []string `json:"eliminations"`
		Differences  []string `json:"differences"`
		Consolidated []string `json:"consolidated"`
		Reserve      string   `json:"reserve"`
	} `json:"want"`
}

// input turns a case into the engine's input.
func (c consolidationCase) input(t *testing.T) consolidation.Input {
	t.Helper()
	group, err := money.Lookup(c.Group)
	if err != nil {
		t.Fatal(err)
	}
	if group.MinorUnits != c.GroupUnits {
		t.Fatalf("%s: %s has %d minor units, the case says %d", c.Name, c.Group, group.MinorUnits, c.GroupUnits)
	}
	in := consolidation.Input{Group: group, From: c.From, To: c.To}
	for _, r := range c.Rates {
		cur, err := money.Lookup(r.Currency)
		if err != nil {
			t.Fatal(err)
		}
		closing, err := money.ParseRate(r.Closing, cur, group, "the group's table")
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		average, err := money.ParseRate(r.Average, cur, group, "the group's table")
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		in.Rates = append(in.Rates, consolidation.Rate{Currency: r.Currency, Month: r.Month, Closing: closing, Average: average})
	}
	for _, e := range c.Entities {
		cur, err := money.Lookup(e.Currency)
		if err != nil {
			t.Fatal(err)
		}
		if cur.MinorUnits != e.MinorUnits {
			t.Fatalf("%s: %s has %d minor units, the case says %d", c.Name, e.Currency, cur.MinorUnits, e.MinorUnits)
		}
		ent := consolidation.Entity{ID: e.ID, Currency: cur}
		for _, l := range e.Lines {
			m, err := money.Parse(l.Amount, cur)
			if err != nil {
				t.Fatalf("%s: %v", c.Name, err)
			}
			ent.Lines = append(ent.Lines, consolidation.Line{Account: l.Account, Type: l.Type, Counterparty: l.Counterparty, Month: l.Month, Amount: m})
		}
		in.Entities = append(in.Entities, ent)
	}
	for _, p := range c.Pairs {
		in.Pairs = append(in.Pairs, consolidation.Pair{A: p.A, B: p.B})
	}
	return in
}

// flat writes a result as the case's flat texts (consolidation.schema.json).
func flat(r consolidation.Result) (entities [][]string, elim, diff, cons []string, reserve string) {
	for _, e := range r.Entities {
		var lines []string
		for _, l := range e.Lines {
			lines = append(lines, strings.Join([]string{l.Account, l.Counterparty, l.Section, l.Amount.String()}, "|"))
		}
		sort.Strings(lines)
		entities = append(entities, append([]string{e.ID, e.Reserve.String()}, lines...))
	}
	for _, e := range r.Eliminations {
		elim = append(elim, strings.Join([]string{e.Entity, e.Account, e.Counterparty, e.Section, e.Amount.String()}, "|"))
	}
	for _, d := range r.Differences {
		diff = append(diff, strings.Join([]string{d.DebitEntity, d.CreditEntity, d.A, d.B, d.Debit.String(), d.Credit.String(), d.Matched.String(), d.Difference.String()}, "|"))
	}
	for _, c := range r.Consolidated {
		cons = append(cons, strings.Join([]string{c.Account, c.Section, c.Amount.String()}, "|"))
	}
	sort.Strings(elim)
	sort.Strings(diff)
	sort.Strings(cons)
	return entities, elim, diff, cons, r.Reserve.String()
}

// The hand-worked consolidation cases and the 240 the oracle generated (FR-ENT-06, RND-TRANSLATE) conform to their schema and the engine reproduces each: every entity's translated lines and
// reserve, the eliminations, the differences and the consolidated figures, to the cent. The oracle's own module reproduces the same files (oracle/tests/test_consolidation.py), which is how
// the two implementations are held to one rule (QA-ENG-02).
// req: QA-ENG-02, FR-ENT-06, RND-TRANSLATE
func TestTheConsolidationCasesConformAndTheEngineReproducesEach(t *testing.T) {
	schema := compile("consolidation.schema.json", schemas.ConsolidationSchema)
	for file, least := range map[string]int{"hand.json": 11, "generated.json": 240} {
		raw, err := os.ReadFile("../../test/differential/consolidations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		for i, d := range doc.([]any) {
			if err := schema.Validate(d); err != nil {
				t.Errorf("%s case %d: %v", file, i, err)
			}
		}
		var list []consolidationCase
		if err := json.Unmarshal(raw, &list); err != nil {
			t.Fatal(err)
		}
		if len(list) < least {
			t.Fatalf("%s: only %d cases", file, len(list))
		}
		for _, c := range list {
			res, err := consolidation.Work(c.input(t))
			if err != nil {
				t.Errorf("%s: %s: %v", file, c.Name, err)
				continue
			}
			ents, elim, diff, cons, reserve := flat(res)
			if len(ents) != len(c.Want.Entities) {
				t.Errorf("%s: %s: %d entities, reference %d", file, c.Name, len(ents), len(c.Want.Entities))
				continue
			}
			for i, w := range c.Want.Entities {
				want := append([]string{w.ID, w.Reserve}, w.Lines...)
				if strings.Join(ents[i], "\n") != strings.Join(want, "\n") {
					t.Errorf("%s: %s: entity %s: engine %v, reference %v", file, c.Name, w.ID, ents[i], want)
				}
			}
			for what, pair := range map[string][2][]string{"eliminations": {elim, c.Want.Eliminations}, "differences": {diff, c.Want.Differences}, "consolidated": {cons, c.Want.Consolidated}} {
				if strings.Join(pair[0], "\n") != strings.Join(pair[1], "\n") {
					t.Errorf("%s: %s: %s: engine %v, reference %v", file, c.Name, what, pair[0], pair[1])
				}
			}
			if reserve != c.Want.Reserve {
				t.Errorf("%s: %s: reserve %s, reference %s", file, c.Name, reserve, c.Want.Reserve)
			}
		}
	}
}

// A run that cannot be worked is refused in plain words: a month with no rate, an account with two types, a counterparty that is no entity of the run, and a month after the run's last.
// req: FR-ENT-05, FR-ENT-06
func TestAConsolidationThatCannotBeWorkedIsRefusedInPlainWords(t *testing.T) {
	sgd, myr := money.MustLookup("SGD"), money.MustLookup("MYR")
	amount := func(s string, c money.Currency) money.Money { m, _ := money.Parse(s, c); return m }
	rate := func(s string) money.Rate { r, _ := money.ParseRate(s, myr, sgd, "test"); return r }
	good := func() consolidation.Input {
		return consolidation.Input{Group: sgd, From: "2026-07", To: "2026-07",
			Rates:    []consolidation.Rate{{Currency: "MYR", Month: "2026-07", Closing: rate("0.3"), Average: rate("0.29")}},
			Entities: []consolidation.Entity{{ID: "my", Currency: myr, Lines: []consolidation.Line{{Account: "1000", Type: "asset", Month: "2026-07", Amount: amount("10.00", myr)}}}}}
	}
	if _, err := consolidation.Work(good()); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*consolidation.Input){
		"no rate": func(in *consolidation.Input) { in.Rates = nil },
		"two types": func(in *consolidation.Input) {
			in.Entities[0].Lines = append(in.Entities[0].Lines, in.Entities[0].Lines[0])
			in.Entities[0].Lines[1].Type = "liability"
		},
		"a stranger":            func(in *consolidation.Input) { in.Entities[0].Lines[0].Counterparty = "xx" },
		"itself":                func(in *consolidation.Input) { in.Entities[0].Lines[0].Counterparty = "my" },
		"a late month":          func(in *consolidation.Input) { in.Entities[0].Lines[0].Month = "2026-08" },
		"reversed months":       func(in *consolidation.Input) { in.From = "2026-08" },
		"an unknown type":       func(in *consolidation.Input) { in.Entities[0].Lines[0].Type = "income" },
		"another currency":      func(in *consolidation.Input) { in.Entities[0].Lines[0].Amount = amount("10.00", sgd) },
		"the same entity twice": func(in *consolidation.Input) { in.Entities = append(in.Entities, in.Entities[0]) },
	} {
		in := good()
		mutate(&in)
		_, err := consolidation.Work(in)
		var ce *consolidation.Error
		if err == nil || !asConsolidation(err, &ce) || ce.Fix == "" {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func asConsolidation(err error, target **consolidation.Error) bool {
	e, ok := err.(*consolidation.Error)
	if ok {
		*target = e
	}
	return ok
}
