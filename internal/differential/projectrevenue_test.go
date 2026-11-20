package differential

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	schemas "github.com/cognizhi/fyniq/test/differential"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/projectrevenue"
)

type projectRevenueCase struct {
	Name       string `json:"name"`
	Currency   string `json:"currency"`
	MinorUnits int    `json:"minor_units"`
	Price      string `json:"price"`
	Estimate   string `json:"estimate"`
	Cost       string `json:"cost"`
	Billed     string `json:"billed"`
	Held       string `json:"held"`
	Want       struct {
		Progress        string `json:"progress"`
		Overrun         bool   `json:"overrun"`
		Earned          string `json:"earned"`
		Adjustment      string `json:"adjustment"`
		Asset           string `json:"asset"`
		Liability       string `json:"liability"`
		AssetChange     string `json:"asset_change"`
		LiabilityChange string `json:"liability_change"`
	} `json:"want"`
}

// The hand-worked project-revenue cases (FR-PRJ-11, RND-RECOGNISE) conform to their schema and the engine reproduces each: the progress, what is earned to date, the revenue the
// journal books, and the contract asset and liability with what the journal moves them by. The oracle's own module reproduces the same file (oracle/tests/test_project_revenue.py),
// which is how the two implementations are held to one rule (QA-ENG-02).
// req: QA-ENG-02, FR-PRJ-11
func TestTheProjectRevenueCasesConformAndTheEngineReproducesEach(t *testing.T) {
	schema := compile("project-revenue.schema.json", schemas.ProjectRevenueSchema)
	raw, err := os.ReadFile("../../test/differential/project-revenue/project-revenue.json")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	for i, d := range doc.([]any) {
		if err := schema.Validate(d); err != nil {
			t.Errorf("case %d: %v", i, err)
		}
	}
	var list []projectRevenueCase
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	if len(list) < 10 {
		t.Fatalf("only %d cases", len(list))
	}
	for _, c := range list {
		cur, err := money.Lookup(c.Currency)
		if err != nil {
			t.Fatal(err)
		}
		if cur.MinorUnits != c.MinorUnits {
			t.Errorf("%s: %s has %d minor units, the case says %d", c.Name, c.Currency, cur.MinorUnits, c.MinorUnits)
		}
		m := func(s string) money.Money {
			v, err := money.Parse(s, cur)
			if err != nil {
				t.Fatalf("%s: %q: %v", c.Name, s, err)
			}
			return v
		}
		got, err := projectrevenue.Work(m(c.Price), m(c.Estimate), m(c.Cost), m(c.Billed), m(c.Held))
		if err != nil {
			t.Errorf("%s: %v", c.Name, err)
			continue
		}
		for _, x := range []struct {
			what      string
			got, want string
		}{
			{"progress", got.Progress, c.Want.Progress}, {"earned", got.Earned.String(), c.Want.Earned}, {"adjustment", got.Adjustment.String(), c.Want.Adjustment},
			{"asset", got.Asset.String(), c.Want.Asset}, {"liability", got.Liability.String(), c.Want.Liability},
			{"asset change", got.AssetChange.String(), c.Want.AssetChange}, {"liability change", got.LiabilityChange.String(), c.Want.LiabilityChange},
		} {
			if x.got != x.want {
				t.Errorf("%s: %s is %s, want %s", c.Name, x.what, x.got, x.want)
			}
		}
		if got.Overrun != c.Want.Overrun {
			t.Errorf("%s: overrun is %v, want %v", c.Name, got.Overrun, c.Want.Overrun)
		}
	}
}

// Refusals: a negative fee or cost and an estimate of nothing are refused in plain words, and two currencies are never mixed.
// req: FR-PRJ-11
func TestProjectRevenueRefusesWhatCannotBeWorked(t *testing.T) {
	sgd, _ := money.Lookup("SGD")
	usd, _ := money.Lookup("USD")
	m := func(c money.Currency, s string) money.Money { v, _ := money.Parse(s, c); return v }
	for name, args := range map[string][5]money.Money{
		"an estimate of nothing": {m(sgd, "100"), m(sgd, "0"), m(sgd, "10"), m(sgd, "0"), m(sgd, "0")},
		"a negative fee":         {m(sgd, "-1"), m(sgd, "10"), m(sgd, "1"), m(sgd, "0"), m(sgd, "0")},
		"a negative cost":        {m(sgd, "100"), m(sgd, "10"), m(sgd, "-1"), m(sgd, "0"), m(sgd, "0")},
		"two currencies":         {m(sgd, "100"), m(usd, "10"), m(sgd, "1"), m(sgd, "0"), m(sgd, "0")},
	} {
		if _, err := projectrevenue.Work(args[0], args[1], args[2], args[3], args[4]); err == nil {
			t.Errorf("%s was worked", name)
		}
	}
}
