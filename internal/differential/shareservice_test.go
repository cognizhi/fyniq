package differential

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	schemas "github.com/cognizhi/fyniq/test/differential"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/sharepay"
)

type shareServiceCase struct {
	Name         string `json:"name"`
	Currency     string `json:"currency"`
	MinorUnits   int    `json:"minor_units"`
	FairValue    string `json:"fair_value"`
	ServiceStart string `json:"service_start"`
	Tranches     []struct {
		Shares int64  `json:"shares"`
		VestOn string `json:"vest_on"`
	} `json:"tranches"`
	Want struct {
		Tranches []struct {
			Months int      `json:"months"`
			Total  string   `json:"total"`
			Parts  []string `json:"parts"`
		} `json:"tranches"`
		Months []struct {
			Month   string `json:"month"`
			Expense string `json:"expense"`
		} `json:"months"`
		Total string `json:"total"`
	} `json:"want"`
}

// The hand-worked grants and the 200 the oracle generated (FR-EQ-13, ALLOC-EVEN, RND-LINE) conform to their schema and the engine reproduces each: every tranche's months, total and part of each
// month, each month's expense and the grant's total, to the cent. The oracle's own module reproduces the same files (oracle/tests/test_share_based_payment.py), which is how the two
// implementations are held to one rule (QA-ENG-02).
// req: QA-ENG-02, FR-EQ-13
func TestTheShareServiceCasesConformAndTheEngineReproducesEach(t *testing.T) {
	schema := compile("share-service.schema.json", schemas.ShareServiceSchema)
	for file, least := range map[string]int{"hand.json": 10, "generated.json": 200} {
		raw, err := os.ReadFile("../../test/differential/share-services/" + file)
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
		var list []shareServiceCase
		if err := json.Unmarshal(raw, &list); err != nil {
			t.Fatal(err)
		}
		if len(list) < least {
			t.Fatalf("%s: only %d cases", file, len(list))
		}
		for _, c := range list {
			cur, err := money.Lookup(c.Currency)
			if err != nil || cur.MinorUnits != c.MinorUnits {
				t.Fatalf("%s: %s: currency %v (%v)", file, c.Name, c.Currency, err)
			}
			fv, err := money.ParseUnitPrice(c.FairValue, cur, "fair_value")
			if err != nil {
				t.Fatalf("%s: %s: %v", file, c.Name, err)
			}
			start, err := time.Parse(time.DateOnly, c.ServiceStart)
			if err != nil {
				t.Fatal(err)
			}
			var tranches []sharepay.Tranche
			for _, x := range c.Tranches {
				vest, err := time.Parse(time.DateOnly, x.VestOn)
				if err != nil {
					t.Fatal(err)
				}
				tranches = append(tranches, sharepay.Tranche{Shares: x.Shares, VestOn: vest})
			}
			got, err := sharepay.Work(fv, start, tranches)
			if err != nil {
				t.Errorf("%s: %s: %v", file, c.Name, err)
				continue
			}
			if got.Total.String() != c.Want.Total || len(got.Tranches) != len(c.Want.Tranches) || len(got.Months) != len(c.Want.Months) {
				t.Errorf("%s: %s: engine total %s over %d tranches and %d months, reference %s over %d and %d", file, c.Name, got.Total, len(got.Tranches), len(got.Months),
					c.Want.Total, len(c.Want.Tranches), len(c.Want.Months))
				continue
			}
			for i, w := range c.Want.Tranches {
				g := got.Tranches[i]
				parts := make([]string, len(g.Parts))
				for k, p := range g.Parts {
					parts[k] = p.String()
				}
				if g.Months != w.Months || g.Total.String() != w.Total || strings.Join(parts, " ") != strings.Join(w.Parts, " ") {
					t.Errorf("%s: %s: tranche %d: engine %d months, %s, %v; reference %d, %s, %v", file, c.Name, i+1, g.Months, g.Total, parts, w.Months, w.Total, w.Parts)
				}
			}
			for i, w := range c.Want.Months {
				if g := got.Months[i]; g.Month.Format("2006-01") != w.Month || g.Expense.String() != w.Expense {
					t.Errorf("%s: %s: month %d: engine %s %s, reference %s %s", file, c.Name, i+1, g.Month.Format("2006-01"), g.Expense, w.Month, w.Expense)
				}
			}
		}
	}
}
