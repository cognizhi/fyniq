package planning_test

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/differential"
)

// fixture is test/planning/workbook-sample.json: the workbook's sample and its second data set, each as a
// fyniq-forecast@1 document with the reference model's outputs (scripts/workbook_planning_fixture.py).
type fixture struct {
	Source map[string]string `json:"source"`
	Cases  []struct {
		Name      string          `json:"name"`
		Forecast  json.RawMessage `json:"forecast"`
		Reference struct {
			Rows    map[string][]*string `json:"rows"`
			Summary map[string][]*string `json:"summary"`
			Key     map[string]*string   `json:"key"`
		} `json:"reference"`
	} `json:"cases"`
}

var tolCtx = apd.BaseContext.WithPrecision(100)

func parse(t *testing.T, s string) *apd.Decimal {
	t.Helper()
	x, _, err := apd.NewFromString(s)
	if err != nil {
		t.Fatalf("%q: %v", s, err)
	}
	return x
}

// within reports whether |a − b| ≤ tol.
func within(a, b, tol *apd.Decimal) bool {
	var d apd.Decimal
	tolCtx.Sub(&d, a, b)
	d.Abs(&d)
	return d.Cmp(tol) <= 0
}

func cents(n int) *apd.Decimal { return apd.New(int64(n), 2) } // n cents

// req: FR-PLN-02
// req: FR-PLN-03
// req: RND-PLAN
// The engine is the workbook's model (rev 0.214): on the workbook's sample and on its second data set, each driver
// line is within a cent of the reference model's own figure (its one rounding, and the rounding of the lines it is a
// percentage of); a total, a profit line, a cash flow and the cash balance are within a cent per rounded line they add
// up; customers and ARPA agree but for the reference's binary floating point; the key results' months and runway
// agree exactly (p1-planning design D9).
func TestTheWorkbooksSample(t *testing.T) {
	b, err := os.ReadFile("../../../test/planning/workbook-sample.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx fixture
	if err := json.Unmarshal(b, &fx); err != nil {
		t.Fatal(err)
	}
	if fx.Source["commit"] == "" || len(fx.Cases) != 2 {
		t.Fatalf("the fixture names no workbook commit, or has %d cases", len(fx.Cases))
	}
	driver := map[string]string{"r_sub": "subscription", "r_serv": "services", "r_oth": "other", "k_host": "hosting",
		"k_ai": "ai", "k_con": "contractors", "k_fee": "fees", "k_pay": "payroll_cogs", "p_rd": "payroll_rd",
		"p_sm": "payroll_sm", "p_ga": "payroll_ga", "dep": "depreciation", "grants": "grants", "interest": "interest",
		"cf_coll": "collections", "cf_capex": "capex_paid", "cf_eq": "equity", "cf_loan": "loans_drawn",
		"cf_repay": "loan_repayments", "cf_grant": "grants_received", "cf_int": "interest_paid"}
	for _, c := range fx.Cases {
		doc, err := differential.ParseForecast(c.Forecast)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		res, err := differential.RunForecast(doc)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		raw, _ := json.Marshal(res)
		var out struct {
			Runs []struct {
				Months []map[string]any `json:"months"`
				Years  []map[string]any `json:"years"`
				Key    map[string]any   `json:"key"`
			} `json:"runs"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		run := out.Runs[0] // the data set's scenario, with its funding plan: what the workbook shows
		ref := c.Reference.Rows
		at := func(key string, n int) *apd.Decimal {
			v := ref[key][n-1]
			if v == nil {
				return apd.New(0, 0)
			}
			return parse(t, *v)
		}
		ours := func(key string, n int) *apd.Decimal {
			s, ok := run.Months[n-1][key].(string)
			if !ok {
				t.Fatalf("%s month %d: %s is %v", c.Name, n, key, run.Months[n-1][key])
			}
			return parse(t, s)
		}
		check := func(what string, n int, got, want *apd.Decimal, tol *apd.Decimal) {
			if !within(got, want, tol) {
				t.Errorf("%s month %d %s: engine %s, workbook %s (allowed %s)", c.Name, n, what, got.Text('f'), want.Text('f'), tol.Text('f'))
			}
		}
		opex := len(run.Months[0]["opex"].([]any))
		cash := 0 // the cash balance's allowance in cents, accumulated month by month
		var taxTol []int
		for y := 0; y*12 < len(run.Months); y++ {
			// a year's tax moves by at most its rate (17%) of its profit's allowance, plus its own cent
			taxTol = append(taxTol, (12*(14+opex)*17+99)/100+1)
		}
		for n := 1; n <= len(run.Months); n++ {
			for k, o := range driver {
				check(k, n, ours(o, n), at(k, n), cents(1))
			}
			for i := 1; i <= opex; i++ {
				got := parse(t, run.Months[n-1]["opex"].([]any)[i-1].(string))
				check(fmt.Sprintf("o_%d", i), n, got, at(fmt.Sprintf("o_%d", i), n), cents(1))
			}
			year := (n - 1) / 12
			tax := 0
			if s := run.Months[n-1]["tax"].(string); s != "0" && s != "0.00" {
				tax = taxTol[year]
			}
			paid := 0
			if s := run.Months[n-1]["tax_paid"].(string); s != "0" && s != "0.00" {
				paid = taxTol[0]
				for _, x := range taxTol {
					paid = max(paid, x)
				}
			}
			ebitda := 11 + opex
			operating := 1 + (4 + opex) + 4 + 1 + 1 + paid
			net := operating + 4
			cash += net
			for _, x := range []struct {
				ref, ours string
				tol       int
			}{
				{"r_tot", "revenue", 3}, {"k_tot", "direct_costs", 5}, {"gp", "gross_profit", 8}, {"p_tot", "payroll", 3},
				{"o_tot", "opex_total", opex}, {"ebitda", "ebitda", ebitda}, {"pbt", "profit_before_tax", ebitda + 3},
				{"tax", "tax", tax}, {"np", "net_profit", ebitda + 3 + tax}, {"cf_sup", "suppliers", 4 + opex},
				{"cf_pay", "payroll_paid", 4}, {"cf_tax", "tax_paid", paid}, {"cf_op", "operating", operating},
				{"cf_net", "net_cash_flow", net}, {"cash_close", "cash_close", cash}, {"burn", "net_burn", operating + 1},
				{"arr", "arr", 12}, {"sm", "sales_marketing", 2},
			} {
				check(x.ref, n, ours(x.ours, n), at(x.ref, n), cents(x.tol))
			}
			for k, o := range map[string]string{"c_open": "customers_open", "c_new": "new_customers", "c_lost": "lost_customers",
				"c_close": "customers_close", "arpa": "arpa"} {
				check(k, n, ours(o, n), at(k, n), parse(t, "0.000000001"))
			}
			if hc := ref["hc"][n-1]; hc == nil || fmt.Sprint(run.Months[n-1]["headcount"]) != *hc {
				t.Errorf("%s month %d headcount: engine %v, workbook %v", c.Name, n, run.Months[n-1]["headcount"], hc)
			}
		}
		key := c.Reference.Key
		k := run.Key
		month := func(v any) string {
			if v == nil {
				return ""
			}
			return v.(string)
		}
		refMonth := func(name string) string {
			v := key[name]
			if v == nil || len(*v) != 7 || (*v)[4] != '-' {
				return "" // "Not within 36 months", "Not within forecast"
			}
			return *v
		}
		if month(k["cash_out_month"]) != refMonth("FC_ZeroMonth") || month(k["break_even"]) != refMonth("FC_BreakEven") ||
			month(k["minimum_month"]) != refMonth("FC_MinMonth") {
			t.Errorf("%s: cash out %v, break-even %v, minimum %v; workbook %v, %v, %v", c.Name, k["cash_out_month"],
				k["break_even"], k["minimum_month"], *key["FC_ZeroMonth"], *key["FC_BreakEven"], *key["FC_MinMonth"])
		}
		runway := fmt.Sprint(k["runway_months"])
		if k["runway_beyond"] == true {
			runway = "> " + runway
		}
		if runway != *key["FC_Runway"] {
			t.Errorf("%s: runway %s, workbook %s", c.Name, runway, *key["FC_Runway"])
		}
		for _, x := range []struct {
			ours, ref string
			tol       int
		}{
			{"arr_end", "FC_ARREnd", 12}, {"revenue_next_12", "FC_RevNext12", 36}, {"average_burn", "FC_AvgBurn", 30},
			{"minimum_cash", "FC_MinCash", cash}, {"funding_gap", "FC_FundGap", cash + 6*30},
		} {
			if got := parse(t, k[x.ours].(string)); !within(got, parse(t, *key[x.ref]), cents(x.tol)) {
				t.Errorf("%s: %s engine %s, workbook %s", c.Name, x.ours, got.Text('f'), *key[x.ref])
			}
		}
		if got := parse(t, k["customers_end"].(string)); !within(got, parse(t, *key["FC_CustEnd"]), parse(t, "0.000000001")) {
			t.Errorf("%s: customers at the end %s, workbook %s", c.Name, got.Text('f'), *key["FC_CustEnd"])
		}
	}
}
