package planning_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/differential"
	"github.com/cognizhi/fyniq/internal/domain/planning"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// req: FR-PLN-06
// req: RND-PLAN
// The calculator is the workbook's feasibility model (p1-planning design D8, D9): on the workbook's sample and its second
// data set, each year's cash flow and present value is within a cent per rounded line it adds (revenue, variable and
// fixed costs, depreciation, tax and two working-capital lines, and at the end the salvage's tax), and the NPV, each cell
// of the sensitivity grid and each NPV of the table within the sum of their years' allowances, since the reference adds
// unrounded values; the IRR, the paybacks, the profitability index, break-even and the margin of safety agree at the
// digits shown, and so does the verdict.
func TestTheWorkbooksFeasibility(t *testing.T) {
	b, err := os.ReadFile("../../../test/planning/workbook-sample.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx struct {
		Cases []struct {
			Name        string `json:"name"`
			Feasibility struct {
				Reference struct {
					FCF, PV   []string
					NPV       string     `json:"FE_NPV"`
					IRR       string     `json:"FE_IRR"`
					Payback   string     `json:"FE_Payback"`
					DPayback  string     `json:"FE_DPayback"`
					PI        string     `json:"FE_PI"`
					BEUnits   string     `json:"FE_BEUnits"`
					BERevenue string     `json:"FE_BERev"`
					Safety    string     `json:"FE_Safety"`
					Verdict   string     `json:"FE_Verdict"`
					Grid      [][]string `json:"grid"`
					NPVAt     []string   `json:"npv_at"`
				} `json:"reference"`
			} `json:"feasibility"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(b, &fx); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"sample": "workbook-sample", "worst-case": "workbook-worst-case"}
	at := func(s string, places int) string {
		x, err := rounding.PlanQuotient(parse(t, s), apd.New(1, 0), places)
		if err != nil {
			t.Fatal(err)
		}
		return x.Text('f')
	}
	verdicts := map[string]string{"⚠": "marginal", "✔": "feasible", "✖": "not_feasible"}
	for _, c := range fx.Cases {
		raw, err := os.ReadFile("../../../test/differential/feasibilities/" + files[c.Name] + ".json")
		if err != nil {
			t.Fatal(err)
		}
		doc, err := differential.ParseFeasibility(raw)
		if err != nil {
			t.Fatal(err)
		}
		r, err := feasible(doc)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		ref := c.Feasibility.Reference
		total := 0
		for i, y := range r.Years {
			lines := 0
			if i > 0 {
				lines = 7
			}
			if i == doc.Years {
				lines++
			}
			total += lines + 1
			if !within(y.CashFlow, parse(t, ref.FCF[i]), cents(lines)) || !within(y.PV, parse(t, ref.PV[i]), cents(lines+1)) {
				t.Errorf("%s year %d: flow %s, PV %s; the reference's %s, %s", c.Name, i, y.CashFlow.Text('f'), y.PV.Text('f'), ref.FCF[i], ref.PV[i])
			}
		}
		allowance := cents(total)
		if !within(r.NPV, parse(t, ref.NPV), allowance) {
			t.Errorf("%s: NPV %s, the reference's %s", c.Name, r.NPV.Text('f'), ref.NPV)
		}
		for i := range 5 {
			for j := range 5 {
				if !within(r.Grid[i][j], parse(t, ref.Grid[i][j]), allowance) {
					t.Errorf("%s: grid [%d][%d] %s, the reference's %s", c.Name, i, j, r.Grid[i][j].Text('f'), ref.Grid[i][j])
				}
			}
		}
		for i, x := range r.NPVAt {
			if !within(x, parse(t, ref.NPVAt[i]), allowance) {
				t.Errorf("%s: NPV at %s %s, the reference's %s", c.Name, planning.NPVRates[i], x.Text('f'), ref.NPVAt[i])
			}
		}
		pb, _ := r.Payback.Round(1)
		dpb, _ := r.DiscountedPayback.Round(1)
		be, _ := r.BreakEvenUnits.Round(1)
		safety, _ := r.Safety.Percent()
		hundred := new(apd.Decimal)
		if _, err := apd.BaseContext.WithPrecision(50).Mul(hundred, parse(t, ref.Safety), apd.New(100, 0)); err != nil {
			t.Fatal(err)
		}
		for _, x := range []struct{ what, got, want string }{
			{"IRR", r.IRR.Text('f'), at(ref.IRR, 4)},
			{"payback", pb.Text('f'), at(ref.Payback, 1)},
			{"discounted payback", dpb.Text('f'), at(ref.DPayback, 1)},
			{"PI", r.PI.Text('f'), at(ref.PI, 2)},
			{"break-even units", be.Text('f'), at(ref.BEUnits, 1)},
			{"break-even revenue", r.BreakEvenRevenue.Text('f'), at(ref.BERevenue, 2)},
			{"margin of safety", safety.Text('f'), at(hundred.Text('f'), 1)},
			{"verdict", r.Verdict, verdicts[string([]rune(ref.Verdict)[:1])]},
		} {
			if x.got != x.want {
				t.Errorf("%s: %s %s, the reference's %s", c.Name, x.what, x.got, x.want)
			}
		}
	}
}

// feasible works a differential document out through the calculator.
func feasible(doc differential.Feasibility) (planning.StudyResult, error) {
	return planning.Feasible(doc.Study())
}
