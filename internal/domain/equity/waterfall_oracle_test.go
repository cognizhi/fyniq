package equity

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// The engine's waterfall and the independent reference's agree to the cent on the 60 structures test/differential/waterfalls/cases.json holds, which
// oracle/src/oracle/waterfall.py worked out by trying every set of conversions (the engine converts one class at a time, lowest preference per share
// first): each class's total, preference and residual, whether it converts, each holder's part, and the as-converted split.
// req: FR-EQ-29
// req: QA-ENG-02
func TestTheWaterfallAgreesWithTheOracleToTheCent(t *testing.T) {
	raw, err := os.ReadFile("../../../test/differential/waterfalls/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	type holder struct {
		ID       string `json:"id"`
		Shares   int64  `json:"shares"`
		Invested string `json:"invested"`
	}
	type class struct {
		ID            string   `json:"id"`
		Name          string   `json:"name"`
		Preference    bool     `json:"preference"`
		Participating bool     `json:"participating"`
		Multiple      string   `json:"multiple"`
		Seniority     int      `json:"seniority"`
		Holders       []holder `json:"holders"`
	}
	type paid struct {
		ID         string `json:"id"`
		Total      string `json:"total"`
		Preference string `json:"preference"`
		Residual   string `json:"residual"`
		Converts   bool   `json:"converts"`
		Holders    []struct {
			ID    string `json:"id"`
			Total string `json:"total"`
		} `json:"holders"`
	}
	var cases []struct {
		Exit    string  `json:"exit"`
		Classes []class `json:"classes"`
		Result  struct {
			Classes     []paid `json:"classes"`
			AsConverted []paid `json:"as_converted"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 50 {
		t.Fatalf("only %d cases", len(cases))
	}
	converted := 0
	for n, c := range cases {
		var in []WaterfallClass
		for _, x := range c.Classes {
			wc := WaterfallClass{ID: x.ID, Name: x.Name, Preference: x.Preference, Participating: x.Participating, Multiple: x.Multiple, Seniority: x.Seniority}
			for _, h := range x.Holders {
				wc.Holders = append(wc.Holders, WaterfallHolder{ID: h.ID, Shares: h.Shares, Invested: sg(t, h.Invested)})
			}
			in = append(in, wc)
		}
		got, err := ModelWaterfall(sg(t, c.Exit), in)
		if err != nil {
			t.Fatalf("case %d: %v", n, err)
		}
		check := func(what string, have []WaterfallPaid, want []paid) {
			for i, w := range want {
				g := have[i]
				if g.Total.String() != w.Total || g.Preference.String() != w.Preference || g.Residual.String() != w.Residual {
					t.Errorf("case %d (exit %s) %s %s: engine %s (%s + %s), reference %s (%s + %s)", n, c.Exit, what, w.ID, g.Total, g.Preference, g.Residual, w.Total, w.Preference, w.Residual)
				}
				for j, h := range w.Holders {
					if g.Holders[j].Total.String() != h.Total {
						t.Errorf("case %d %s %s holder %s: engine %s, reference %s", n, what, w.ID, h.ID, g.Holders[j].Total, h.Total)
					}
				}
			}
		}
		check("classes", got.Classes, c.Result.Classes)
		check("as converted", got.AsConverted, c.Result.AsConverted)
		for i, w := range c.Result.Classes {
			if got.Classes[i].Converts != w.Converts {
				t.Errorf("case %d %s: engine converts %v, reference %v", n, w.ID, got.Classes[i].Converts, w.Converts)
			}
			if w.Converts {
				converted++
			}
		}
		if _, err := money.Parse(c.Exit, sgd); err != nil {
			t.Fatal(err)
		}
	}
	if converted == 0 {
		t.Error("the cases never convert a class: they prove only half of the rule")
	}
}
