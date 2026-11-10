package differential

import (
	"bytes"
	"encoding/json"

	"github.com/cockroachdb/apd/v3"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/planning"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	schemas "github.com/cognizhi/fyniq/test/differential"
)

// Feasibility studies (README, "Feasibility studies"; p1-planning design D8, D9): a fyniq-feasibility@1 document is one
// idea's inputs and the tax rate, given directly; both sides work it out and write fyniq-feasibility-result@1, compared
// to the cent and to the digit.

// The feasibility formats.
const (
	FeasibilitySchema       = "fyniq-feasibility@1"
	FeasibilityResultSchema = "fyniq-feasibility-result@1"
)

// Feasibility is a fyniq-feasibility@1 document.
type Feasibility struct {
	Schema             string `json:"schema"`
	Name               string `json:"name"`
	Currency           string `json:"currency"`
	MinorUnits         int    `json:"minor_units"`
	Years              int    `json:"years"`
	Rate               string `json:"rate"`
	MaxPayback         string `json:"max_payback"`
	Investment         string `json:"investment"`
	Life               int    `json:"life"`
	Salvage            string `json:"salvage"`
	WorkingCapital     string `json:"working_capital"`
	Units              string `json:"units"`
	UnitGrowth         string `json:"unit_growth"`
	Price              string `json:"price"`
	PriceGrowth        string `json:"price_growth"`
	VariableCost       string `json:"variable_cost"`
	VariableCostGrowth string `json:"variable_cost_growth"`
	Fixed              string `json:"fixed"`
	FixedGrowth        string `json:"fixed_growth"`
	TaxRate            string `json:"tax_rate"`
}

var feasibilitySchema = compile("feasibility.schema.json", schemas.FeasibilitySchema)

// ParseFeasibility reads a fyniq-feasibility@1 document, checked against feasibility.schema.json as the oracle checks
// it. A salvage above the investment is refused, as the guard refuses it.
func ParseFeasibility(b []byte) (f Feasibility, err error) {
	if err := noDuplicateKeys(b); err != nil {
		return f, &InvalidError{Reason: err.Error()}
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
	if err != nil {
		return f, &InvalidError{Reason: err.Error()}
	}
	if err := feasibilitySchema.Validate(doc); err != nil {
		return f, &InvalidError{Reason: err.Error()}
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return f, &InvalidError{Reason: err.Error()}
	}
	if num(f.Salvage).Cmp(num(f.Investment)) > 0 {
		return f, &InvalidError{Reason: "the salvage is above the investment"}
	}
	return f, nil
}

// Study is the engine's study for a document.
func (f Feasibility) Study() planning.Study {
	return planning.Study{Currency: money.Currency{Code: f.Currency, MinorUnits: f.MinorUnits}, Years: f.Years, Rate: num(f.Rate),
		MaxPayback: num(f.MaxPayback), Investment: num(f.Investment), Life: f.Life, Salvage: num(f.Salvage),
		WorkingCap: num(f.WorkingCapital), Units: num(f.Units), UnitGrowth: num(f.UnitGrowth), Price: num(f.Price),
		PriceGrowth: num(f.PriceGrowth), VarCost: num(f.VariableCost), VarCostGrowth: num(f.VariableCostGrowth),
		Fixed: num(f.Fixed), FixedGrowth: num(f.FixedGrowth), TaxRate: num(f.TaxRate)}
}

// RunFeasibility works a document out through the engine.
func RunFeasibility(f Feasibility) (map[string]any, error) {
	r, err := planning.Feasible(f.Study())
	if err != nil {
		return nil, err
	}
	t := func(x *apd.Decimal) any { return x.Text('f') }
	shown := func(x *planning.Fraction, places int) (any, error) {
		if x == nil {
			return nil, nil
		}
		v, err := x.Round(places)
		if err != nil {
			return nil, err
		}
		return v.Text('f'), nil
	}
	var years []any
	for _, y := range r.Years {
		units, err := rounding.PlanQuotient(y.Units, apd.New(1, 0), 1)
		if err != nil {
			return nil, err
		}
		years = append(years, map[string]any{"year": y.Year, "units": units.Text('f'), "revenue": t(y.Revenue), "variable": t(y.Variable),
			"fixed": t(y.Fixed), "depreciation": t(y.Depreciation), "ebit": t(y.EBIT), "tax": t(y.Tax), "nopat": t(y.NOPAT),
			"working_capital": t(y.WorkingCap), "working_capital_change": t(y.WorkingCapChange), "salvage": t(y.Salvage),
			"release": t(y.Relief), "cash_flow": t(y.CashFlow), "pv": t(y.PV), "cumulative": t(y.Cumulative),
			"cumulative_pv": t(y.CumulativePV)})
	}
	out := map[string]any{"schema": FeasibilityResultSchema, "name": f.Name, "years": years, "npv": t(r.NPV), "verdict": r.Verdict,
		"irr": nil, "profitability_index": nil, "break_even_revenue": nil, "margin_of_safety": nil}
	if r.IRR != nil {
		out["irr"] = t(r.IRR)
	}
	if r.PI != nil {
		out["profitability_index"] = t(r.PI)
	}
	if r.BreakEvenRevenue != nil {
		out["break_even_revenue"] = t(r.BreakEvenRevenue)
	}
	if out["payback"], err = shown(r.Payback, 1); err != nil {
		return nil, err
	}
	if out["discounted_payback"], err = shown(r.DiscountedPayback, 1); err != nil {
		return nil, err
	}
	if out["break_even_units"], err = shown(r.BreakEvenUnits, 1); err != nil {
		return nil, err
	}
	if r.Safety != nil {
		p, err := r.Safety.Percent()
		if err != nil {
			return nil, err
		}
		out["margin_of_safety"] = p.Text('f')
	}
	var grid []any
	for _, row := range r.Grid {
		var cells []any
		for _, x := range row {
			cells = append(cells, t(x))
		}
		grid = append(grid, cells)
	}
	out["grid"] = grid
	var at []any
	for _, x := range r.NPVAt {
		at = append(at, t(x))
	}
	out["npv_at"] = at
	return out, nil
}

// FeasibilityDifference is the first difference between two fyniq-feasibility-result@1 results, as ForecastDifference
// compares; "" when they agree.
func FeasibilityDifference(engine, oracle []byte) string { return ForecastDifference(engine, oracle) }
