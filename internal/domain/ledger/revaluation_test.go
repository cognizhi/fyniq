package ledger

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

var (
	ar         = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000001100"), Code: "1100", Active: true, Revalue: true}
	ap         = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000002010"), Code: "2010", Active: true, Revalue: true}
	wise       = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000001030"), Code: "1030", Active: true, Revalue: true}
	unrealised = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000007040"), Code: "7040", Active: true, UnrealisedFX: true}
	revaluing  = Settings{Base: sgd, Accounts: map[uuid.UUID]Account{ar.ID: ar, ap.ID: ap, wise.ID: wise, unrealised.ID: unrealised, bank.ID: bank}}
	runID      = uuid.MustParse("0192f1a0-0000-7000-8000-00000000f0c1")
)

func item(t *testing.T, acc Account, open, carrying, rate string) RevaluationItem {
	t.Helper()
	r, err := money.ParseRate(rate, usd, sgd, "MAS daily feed of 2026-03-31")
	if err != nil {
		t.Fatal(err)
	}
	return RevaluationItem{AccountID: acc.ID, Open: amt(t, open, usd), Carrying: amt(t, carrying, sgd), Rate: r}
}

func lineText(l Line) string {
	return l.AccountID.String()[30:] + " " + l.Amount.String() + " " + l.Amount.Currency().Code + " " + l.Base.String() + " " + l.Description
}

// FR-GL-04 at account level, worked by hand (G4-039 and the Currencies screen's figures): per item, unrealised =
// RND-FX(open × rate) − carrying. G4-039's USD 10,000 receivable carried at S$13,500.00 is S$13,000.00 at 1.30 on 31
// March: Cr 1100 S$500.00 as a base-only line, Dr 7040 S$500.00. On 30 April USD 6,000 carried at S$8,100.00 is
// S$7,980.00 at 1.33: S$120.00 more. USD 16,240.00 in Wise carried at S$20,803.44 is 16,240 × 1.2867 = 20,896.008 →
// S$20,896.01, a S$92.57 gain; USD 1,050.00 owed carried at S$1,345.05 is 1,351.035 → S$1,351.04 under RND-FX (half away
// from zero), a S$5.99 loss.
// req: FR-GL-04
// req: ENG-41
// req: INV-05
// req: ledger/posting-rules FX revaluation
func TestRevalueTakesEachItemToThePeriodEndRate(t *testing.T) {
	march := Revaluation{ID: runID, Version: 1, Date: time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC), Unrealised: unrealised.ID,
		Items: []RevaluationItem{item(t, ar, "10000.00", "13500.00", "1.30")}}
	e, err := Revalue(march, revaluing)
	if err != nil {
		t.Fatal(err)
	}
	if e.PostingDate.Format(time.DateOnly) != "2026-03-31" || e.Source.Type != SourceFXRevaluation || e.RuleVersion != RuleFXRevaluation ||
		e.PostingIdentity != "fx_revaluation:"+runID.String()+":v1:fx_revaluation@1" || e.Memo != "FX revaluation at 31 Mar 2026" {
		t.Errorf("the entry: %+v", e)
	}
	got := make([]string, len(e.Lines))
	for i, l := range e.Lines {
		got[i] = lineText(l)
	}
	want := []string{"001100 0.00 USD -500.00 10000.00 USD revalued at 1.30", "007040 500.00 SGD 500.00 Unrealised FX loss"}
	if !slices.Equal(got, want) {
		t.Errorf("31 March:\n got %q\nwant %q", got, want)
	}
	if e.Lines[0].Rate.String() != "1.30" || e.Lines[0].Rate.Source != "MAS daily feed of 2026-03-31" {
		t.Errorf("the base-only line keeps its rate and source: %+v", e.Lines[0].Rate)
	}

	april := march
	april.Date, april.Items = time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC), []RevaluationItem{item(t, ar, "6000.00", "8100.00", "1.33")}
	if e, err = Revalue(april, revaluing); err != nil || len(e.Lines) != 2 || e.Lines[0].Base.String() != "-120.00" || e.Lines[1].Base.String() != "120.00" {
		t.Errorf("30 April: %v %+v", err, e.Lines)
	}

	both := march
	both.Items = []RevaluationItem{item(t, wise, "16240.00", "20803.44", "1.2867"), item(t, ap, "-1050.00", "-1345.05", "1.2867"),
		item(t, ar, "100.00", "128.67", "1.2867")}
	e, err = Revalue(both, revaluing)
	if err != nil {
		t.Fatal(err)
	}
	got = got[:0]
	for _, l := range e.Lines {
		got = append(got, lineText(l))
	}
	want = []string{"001030 0.00 USD 92.57 16240.00 USD revalued at 1.2867", "002010 0.00 USD -5.99 -1050.00 USD revalued at 1.2867",
		"007040 5.99 SGD 5.99 Unrealised FX loss", "007040 -92.57 SGD -92.57 Unrealised FX gain"}
	if !slices.Equal(got, want) {
		t.Errorf("a gain, a loss and an item already at the rate:\n got %q\nwant %q", got, want)
	}
	sum := money.Zero(sgd)
	for _, l := range e.Lines {
		sum, _ = sum.Add(l.Base)
	}
	if !sum.IsZero() {
		t.Errorf("the entry does not balance in base: %s", sum)
	}

	none := march
	none.Items = []RevaluationItem{item(t, ar, "100.00", "130.00", "1.30")}
	if e, err := Revalue(none, revaluing); err != nil || len(e.Lines) != 0 {
		t.Errorf("nothing to revalue: %v %+v", err, e.Lines)
	}
}

// What the revaluation refuses: an account not marked for it, no unrealised-FX account, a base-currency item, a rate of
// another currency, no date.
// req: FR-GL-04
func TestRevalueRefusesWhatIsNotAMonetaryForeignItem(t *testing.T) {
	ok := Revaluation{ID: runID, Version: 1, Date: time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC), Unrealised: unrealised.ID}
	for name, c := range map[string]struct {
		r    func(Revaluation) Revaluation
		want string
	}{
		"an account not marked": {func(r Revaluation) Revaluation {
			r.Items = []RevaluationItem{item(t, bank, "100.00", "130.00", "1.35")}
			return r
		}, CodeNotRevalued},
		"no unrealised-FX account": {func(r Revaluation) Revaluation { r.Unrealised = bank.ID; return r }, CodeNotRevalued},
		"no date":                  {func(r Revaluation) Revaluation { r.Date = time.Time{}; return r }, CodeNoDate},
		"a base-currency item": {func(r Revaluation) Revaluation {
			it := item(t, ar, "100.00", "130.00", "1.35")
			it.Open = amt(t, "100.00", sgd)
			r.Items = []RevaluationItem{it}
			return r
		}, CodeItem},
		"a rate of another currency": {func(r Revaluation) Revaluation {
			it := item(t, ar, "100.00", "130.00", "1.35")
			jr, _ := money.ParseRate("0.0091", jpy, sgd, "feed")
			it.Rate = jr
			r.Items = []RevaluationItem{it}
			return r
		}, CodeItem},
	} {
		if _, err := Revalue(c.r(ok), revaluing); !slices.Contains(codes(err), c.want) {
			t.Errorf("%s: want %s, got %v", name, c.want, err)
		}
	}
	if _, err := ReplayRevaluation("fx_revaluation@0", ok, revaluing); !slices.Contains(codes(err), CodeUnknownRule) {
		t.Errorf("an unknown rule version: %v", err)
	}
}

// Each revaluation rule version reproduces exactly what it produced over the same runs (ENG-06), as the journal rules
// do: its golden file in testdata/replay changes only by review, and a changed rule is a new version.
// req: ENG-06
// req: QA-ENG-06
func TestEveryRevaluationRuleVersionReplaysItsGoldenEntries(t *testing.T) {
	march := Revaluation{ID: runID, Version: 1, Date: time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC), Unrealised: unrealised.ID,
		Items: []RevaluationItem{item(t, ar, "10000.00", "13500.00", "1.30")}}
	both := march
	both.Version, both.Items = 2, []RevaluationItem{item(t, wise, "16240.00", "20803.44", "1.2867"), item(t, ap, "-1050.00", "-1345.05", "1.2867")}
	one, two := item(t, ap, "-1000.37", "-1346.10", "1.3333"), item(t, ap, "-200.01", "-270.03", "1.3333")
	one.Of, two.Of = "BILL-0001", "BILL-0002"
	bills := march
	bills.Version, bills.Items = 3, []RevaluationItem{one, two}
	cases := map[string][]struct {
		name string
		r    Revaluation
	}{
		RuleFXRevaluation: {{"a receivable at a lower rate (G4-039, 31 March)", march}, {"a gain and a loss", both},
			// rev 0.92: an item may be a document of a control account's sub-ledger, and its line names it; no earlier item
			// named one, so every stored entry replays.
			{"two bills revalued one by one", bills}},
	}
	for rule := range RevaluationRules {
		if len(cases[rule]) == 0 {
			t.Errorf("rule version %s has no replay cases: add them, and its golden file", rule)
		}
	}
	for rule, cs := range cases {
		var b strings.Builder
		fmt.Fprintf(&b, "# What %s posts for each replay case. Generated by TestEveryRevaluationRuleVersionReplaysItsGoldenEntries;\n"+
			"# a rule version's output never changes (ENG-06): a different output is a new rule version.\n", rule)
		for _, c := range cs {
			e, err := ReplayRevaluation(rule, c.r, revaluing)
			if err != nil {
				t.Fatalf("%s, %s: %v", rule, c.name, err)
			}
			b.WriteString(render(c.name, e))
		}
		path := filepath.Join("testdata", "replay", rule+".golden")
		if *update {
			if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%v (run `go test ./internal/domain/ledger -run %s -update` for a new rule version, and review it)", err, t.Name())
		}
		if string(want) != b.String() {
			t.Errorf("%s no longer reproduces its entries (ENG-06): make the change a new rule version.\n--- want\n%s\n--- got\n%s", rule, want, b.String())
		}
	}
}
