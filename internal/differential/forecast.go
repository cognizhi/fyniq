package differential

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/planning"
	schemas "github.com/cognizhi/fyniq/test/differential"
)

// Forecasts (README, "Forecasts"; p1-planning design D9): a fyniq-forecast@1 document is the drivers, the plans, the
// books' figures at the start, the scenarios and the statutory figures, given directly; both sides run every scenario
// with and without its funding plan and write fyniq-forecast-result@1, compared to the cent and to the digit.

// The forecast formats.
const (
	ForecastSchema       = "fyniq-forecast@1"
	ForecastResultSchema = "fyniq-forecast-result@1"
)

// Forecast is a fyniq-forecast@1 document.
type Forecast struct {
	Schema         string `json:"schema"`
	Name           string `json:"name"`
	Currency       string `json:"currency"`
	MinorUnits     int    `json:"minor_units"`
	Start          string `json:"start"`
	Horizon        int    `json:"horizon"`
	CashAdjustment string `json:"cash_adjustment"`
	BufferMonths   string `json:"buffer_months"`
	Books          struct {
		Cash         string   `json:"cash"`
		Receivables  string   `json:"receivables"`
		Payables     string   `json:"payables"`
		Depreciation []string `json:"depreciation"`
	} `json:"books"`
	Revenue struct {
		Customers      string    `json:"customers"`
		NewCustomers   string    `json:"new_customers"`
		NewGrowth      string    `json:"new_growth"`
		Churn          string    `json:"churn"`
		ARPA           string    `json:"arpa"`
		PriceIncrease  string    `json:"price_increase"`
		Services       string    `json:"services"`
		ServicesGrowth string    `json:"services_growth"`
		Other          string    `json:"other"`
		Collection     [3]string `json:"collection"`
	} `json:"revenue"`
	Costs struct {
		Hosting        string `json:"hosting"`
		AI             string `json:"ai"`
		Contractors    string `json:"contractors"`
		Fees           string `json:"fees"`
		SalaryIncrease string `json:"salary_increase"`
	} `json:"costs"`
	Opex []struct {
		Label   string `json:"label"`
		Group   string `json:"group"`
		Base    string `json:"base"`
		Growth  string `json:"growth"`
		Percent string `json:"percent"`
	} `json:"opex"`
	Headcount []struct {
		Role     string  `json:"role"`
		Function string  `json:"function"`
		Start    string  `json:"start"`
		End      *string `json:"end"`
		Salary   string  `json:"salary"`
		CPF      bool    `json:"cpf"`
	} `json:"headcount"`
	Capex []struct {
		Amount string `json:"amount"`
		Month  string `json:"month"`
		Life   int    `json:"life"`
	} `json:"capex"`
	Funding []struct {
		Type   string `json:"type"`
		Month  string `json:"month"`
		Amount string `json:"amount"`
		Rate   string `json:"rate"`
		Tenor  int    `json:"tenor"`
	} `json:"funding"`
	Tax struct {
		StartUpYears         int    `json:"start_up_years"`
		LossesBroughtForward string `json:"losses_brought_forward"`
		PaymentLag           int    `json:"payment_lag"`
	} `json:"tax"`
	Scenarios []struct {
		Name   string `json:"name"`
		Levers struct {
			NewCustomers string `json:"new_customers"`
			Churn        string `json:"churn"`
			Price        string `json:"price"`
			Services     string `json:"services"`
			Opex         string `json:"opex"`
			DirectCosts  string `json:"direct_costs"`
			FundAmount   string `json:"fund_amount"`
			HireDelay    int    `json:"hire_delay"`
			FundDelay    int    `json:"fund_delay"`
		} `json:"levers"`
		BufferMonths *string `json:"buffer_months"`
	} `json:"scenarios"`
	Statutory FixedStatutory `json:"statutory"`
}

// FixedStatutory is a forecast document's statutory figures: the same for every month and YA it names.
type FixedStatutory struct {
	CPFRate   string `json:"employer_cpf"`
	OWCeiling string `json:"ow_ceiling"`
	SDLLevy   struct {
		Rate    string `json:"rate"`
		Minimum string `json:"minimum"`
		Maximum string `json:"maximum"`
	} `json:"sdl"`
	TaxRateText string                 `json:"tax_rate"`
	Exemptions  map[string][][2]string `json:"exemptions"`
	Rebates     map[string]struct {
		Percent string `json:"percent"`
		Cap     string `json:"cap"`
	} `json:"rebates"`
}

func num(s string) *apd.Decimal {
	if s == "" {
		return apd.New(0, 0)
	}
	x, _, err := apd.NewFromString(s)
	if err != nil {
		panic(&InvalidError{Reason: fmt.Sprintf("%q is not a decimal", s)})
	}
	return x
}

func (f FixedStatutory) EmployerCPF(time.Time) (*apd.Decimal, *apd.Decimal, error) {
	return num(f.CPFRate), num(f.OWCeiling), nil
}

func (f FixedStatutory) SDL(_ time.Time, wages *apd.Decimal) (*apd.Decimal, error) {
	if wages.Sign() <= 0 {
		return apd.New(0, 0), nil
	}
	levy := new(apd.Decimal)
	if _, err := apd.BaseContext.WithPrecision(200).Mul(levy, wages, num(f.SDLLevy.Rate)); err != nil {
		return nil, err
	}
	if lo := num(f.SDLLevy.Minimum); levy.Cmp(lo) < 0 {
		return lo, nil
	}
	if hi := num(f.SDLLevy.Maximum); levy.Cmp(hi) > 0 {
		return hi, nil
	}
	return levy, nil
}

func (f FixedStatutory) TaxRate(int) (*apd.Decimal, error) { return num(f.TaxRateText), nil }

func (f FixedStatutory) Exemption(scheme string, _ int, chargeable *apd.Decimal) (*apd.Decimal, error) {
	c := apd.BaseContext.WithPrecision(200)
	out, left := apd.New(0, 0), new(apd.Decimal).Set(chargeable)
	for _, t := range f.Exemptions[scheme] {
		band := num(t[0])
		part := band
		if left.Cmp(band) < 0 {
			part = new(apd.Decimal).Set(left)
		}
		var x apd.Decimal
		c.Mul(&x, part, num(t[1]))
		c.Add(out, out, &x)
		c.Sub(left, left, part)
	}
	return out, nil
}

func (f FixedStatutory) Rebate(ya int, tax *apd.Decimal) (*apd.Decimal, error) {
	r, ok := f.Rebates[strconv.Itoa(ya)]
	if !ok || tax.Sign() <= 0 {
		return apd.New(0, 0), nil
	}
	x := new(apd.Decimal)
	apd.BaseContext.WithPrecision(200).Mul(x, tax, num(r.Percent))
	if capped := num(r.Cap); x.Cmp(capped) > 0 {
		return capped, nil
	}
	return x, nil
}

func ym(s string) time.Time {
	t, err := time.ParseInLocation("2006-01", s, time.UTC)
	if err != nil {
		panic(&InvalidError{Reason: fmt.Sprintf("%q is not a month (YYYY-MM)", s)})
	}
	return t
}

var forecastSchema = compile("forecast.schema.json", schemas.ForecastSchema)

// ParseForecast reads a fyniq-forecast@1 document, checked against forecast.schema.json as the oracle checks it.
func ParseForecast(b []byte) (f Forecast, err error) {
	if err := noDuplicateKeys(b); err != nil {
		return f, &InvalidError{Reason: err.Error()}
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
	if err != nil {
		return f, &InvalidError{Reason: err.Error()}
	}
	if err := forecastSchema.Validate(doc); err != nil {
		return f, &InvalidError{Reason: err.Error()}
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return f, &InvalidError{Reason: err.Error()}
	}
	return f, nil
}

// inputs are the engine's inputs for a document.
func (f Forecast) inputs() (in planning.Inputs, err error) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(*InvalidError); ok {
				err = e
				return
			}
			panic(r)
		}
	}()
	in = planning.Inputs{
		Currency: money.Currency{Code: f.Currency, MinorUnits: f.MinorUnits}, Start: ym(f.Start), Horizon: f.Horizon,
		Books:          planning.Books{Cash: num(f.Books.Cash), Receivables: num(f.Books.Receivables), Payables: num(f.Books.Payables)},
		CashAdjustment: num(f.CashAdjustment), BufferMonths: num(f.BufferMonths),
		Revenue: planning.RevenueDrivers{Customers: num(f.Revenue.Customers), NewCustomers: num(f.Revenue.NewCustomers),
			NewGrowth: num(f.Revenue.NewGrowth), Churn: num(f.Revenue.Churn), ARPA: num(f.Revenue.ARPA),
			PriceIncrease: num(f.Revenue.PriceIncrease), Services: num(f.Revenue.Services),
			ServicesGrowth: num(f.Revenue.ServicesGrowth), Other: num(f.Revenue.Other),
			Collection: [3]*apd.Decimal{num(f.Revenue.Collection[0]), num(f.Revenue.Collection[1]), num(f.Revenue.Collection[2])}},
		Costs: planning.CostDrivers{Hosting: num(f.Costs.Hosting), AI: num(f.Costs.AI), Contractors: num(f.Costs.Contractors),
			Fees: num(f.Costs.Fees), SalaryIncrease: num(f.Costs.SalaryIncrease)},
		Tax: planning.TaxInputs{StartUpYears: f.Tax.StartUpYears, LossesBroughtForward: num(f.Tax.LossesBroughtForward),
			PaymentLag: f.Tax.PaymentLag},
	}
	for _, x := range f.Books.Depreciation {
		in.Books.Depreciation = append(in.Books.Depreciation, num(x))
	}
	for _, o := range f.Opex {
		in.Opex = append(in.Opex, planning.OpexLine{Label: o.Label, Group: o.Group, Base: num(o.Base), Growth: num(o.Growth), Percent: num(o.Percent)})
	}
	for _, h := range f.Headcount {
		hire := planning.Hire{Role: h.Role, Function: h.Function, Start: ym(h.Start), Salary: num(h.Salary), CPF: h.CPF}
		if h.End != nil {
			e := ym(*h.End)
			hire.End = &e
		}
		in.Headcount = append(in.Headcount, hire)
	}
	for _, c := range f.Capex {
		in.Capex = append(in.Capex, planning.Capex{Amount: num(c.Amount), Month: ym(c.Month), Life: c.Life})
	}
	for _, x := range f.Funding {
		in.Funding = append(in.Funding, planning.Funding{Type: x.Type, Month: ym(x.Month), Amount: num(x.Amount), Rate: num(x.Rate), Tenor: x.Tenor})
	}
	return in, nil
}

// RunForecast runs every scenario of the document through the engine, each with and without its funding plan.
func RunForecast(f Forecast) (map[string]any, error) {
	in, err := f.inputs()
	if err != nil {
		return nil, err
	}
	var runs []any
	for _, s := range f.Scenarios {
		l := s.Levers
		sc := planning.Scenario{Name: s.Name, Levers: planning.Levers{NewCustomers: num(l.NewCustomers), Churn: num(l.Churn),
			Price: num(l.Price), Services: num(l.Services), Opex: num(l.Opex), DirectCosts: num(l.DirectCosts),
			FundAmount: num(l.FundAmount), HireDelay: l.HireDelay, FundDelay: l.FundDelay}}
		if s.BufferMonths != nil {
			sc.BufferMonths = num(*s.BufferMonths)
		}
		for _, withFunding := range []bool{true, false} {
			x := in
			if !withFunding {
				x = planning.WithoutFunding(in)
			}
			r, err := planning.Run(x, sc, f.Statutory)
			if err != nil {
				return nil, err
			}
			out, err := renderForecast(r, withFunding)
			if err != nil {
				return nil, err
			}
			runs = append(runs, out)
		}
	}
	return map[string]any{"schema": ForecastResultSchema, "name": f.Name, "runs": runs}, nil
}

func renderForecast(r planning.Result, withFunding bool) (map[string]any, error) {
	var ferr error
	t := func(x *apd.Decimal) any {
		if x == nil {
			return nil
		}
		return x.Text('f')
	}
	ratio := func(f *planning.Fraction, places int) any {
		if f == nil {
			return nil
		}
		x, err := f.Round(places)
		if err != nil && ferr == nil {
			ferr = err
		}
		return t(x)
	}
	month := func(d time.Time) string { return d.Format("2006-01") }
	optMonth := func(d *time.Time) any {
		if d == nil {
			return nil
		}
		return month(*d)
	}
	var months []any
	for _, m := range r.Months {
		opex := make([]any, len(m.Opex))
		for i, o := range m.Opex {
			opex[i] = t(o)
		}
		var gm any
		if m.GrossMargin != nil {
			x, err := m.GrossMargin.Percent()
			if err != nil {
				return nil, err
			}
			gm = t(x)
		}
		months = append(months, map[string]any{
			"n": m.N, "date": month(m.Date), "year": m.Year,
			"customers_open": t(m.CustomersOpen), "new_customers": t(m.NewCustomers), "lost_customers": t(m.LostCustomers),
			"customers_close": t(m.CustomersClose), "arpa": t(m.ARPA),
			"subscription": t(m.Subscription), "services": t(m.Services), "other": t(m.Other), "revenue": t(m.Revenue),
			"hosting": t(m.Hosting), "ai": t(m.AI), "contractors": t(m.Contractors), "fees": t(m.Fees),
			"payroll_cogs": t(m.PayrollCOGS), "direct_costs": t(m.DirectCosts), "gross_profit": t(m.GrossProfit),
			"payroll_rd": t(m.PayrollRD), "payroll_sm": t(m.PayrollSM), "payroll_ga": t(m.PayrollGA), "payroll": t(m.Payroll),
			"headcount": m.Headcount, "opex": opex, "opex_total": t(m.OpexTotal), "ebitda": t(m.EBITDA),
			"depreciation": t(m.Depreciation), "grants": t(m.Grants), "interest": t(m.Interest),
			"profit_before_tax": t(m.ProfitBeforeTax), "tax": t(m.Tax), "net_profit": t(m.NetProfit),
			"collections": t(m.Collections), "suppliers": t(m.Suppliers), "payroll_paid": t(m.PayrollPaid),
			"grants_received": t(m.GrantsReceived), "interest_paid": t(m.InterestPaid), "tax_paid": t(m.TaxPaid),
			"operating": t(m.Operating), "capex_paid": t(m.CapexPaid), "equity": t(m.Equity), "loans_drawn": t(m.LoansDrawn),
			"loan_repayments": t(m.LoanRepayments), "net_cash_flow": t(m.NetCashFlow), "cash_open": t(m.CashOpen),
			"cash_close": t(m.CashClose), "net_burn": t(m.NetBurn), "arr": t(m.ARR), "sales_marketing": t(m.SalesMarketing),
			"gross_margin": gm, "cac": ratio(m.CAC, 2), "ltv": ratio(m.LTV, 2), "ltv_to_cac": ratio(m.LTVToCAC, 2),
			"burn_multiple": ratio(m.BurnMultiple, 2),
		})
	}
	var taxes []any
	for _, x := range r.Taxes {
		taxes = append(taxes, map[string]any{"year": x.Year, "ya": x.YA, "scheme": x.Scheme,
			"profit_before_tax": t(x.ProfitBeforeTax), "losses_brought_forward": t(x.LossesBroughtForward),
			"chargeable": t(x.Chargeable), "exempt": t(x.Exempt), "taxable": t(x.Taxable), "gross_tax": t(x.GrossTax),
			"rebate": t(x.Rebate), "tax": t(x.Tax), "losses_carried_forward": t(x.LossesCarriedForward)})
	}
	var years []any
	for _, y := range r.Years {
		var hc any
		if y.Headcount != nil {
			hc = *y.Headcount
		}
		years = append(years, map[string]any{"year": y.Year, "revenue": t(y.Revenue), "gross_profit": t(y.GrossProfit),
			"ebitda": t(y.EBITDA), "net_profit": t(y.NetProfit), "net_burn": t(y.NetBurn), "cash": t(y.Cash),
			"customers": t(y.Customers), "arr": t(y.ARR), "headcount": hc})
	}
	k := r.Key
	key := map[string]any{"cash_out_month": optMonth(k.CashOutMonth), "runway_months": k.RunwayMonths,
		"runway_beyond": k.RunwayBeyond, "minimum_cash": t(k.MinimumCash), "minimum_month": month(k.MinimumMonth),
		"break_even": optMonth(k.BreakEven), "average_burn": t(k.AverageBurn), "funding_gap": t(k.FundingGap),
		"revenue_next_12": t(k.RevenueNext12), "arr_end": t(k.ARREnd), "customers_end": t(k.CustomersEnd)}
	return map[string]any{"scenario": r.Scenario, "funding": withFunding, "opening_cash": t(r.OpeningCash),
		"collections_ok": r.CollectionsOK, "months": months, "taxes": taxes, "years": years, "key": key}, ferr
}

// ForecastDifference is the first difference between two fyniq-forecast-result@1 results: numbers compared exactly
// as decimals, everything else as written; "" when they agree.
func ForecastDifference(engine, oracle []byte) string {
	var a, b any
	if err := json.Unmarshal(engine, &a); err != nil {
		return "the engine's result does not read: " + err.Error()
	}
	if err := json.Unmarshal(oracle, &b); err != nil {
		return "the oracle's result does not read: " + err.Error()
	}
	return diffValue("", a, b)
}

func diffValue(path string, a, b any) string {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok {
			return fmt.Sprintf("%s: engine an object, oracle %v", path, b)
		}
		keys := map[string]bool{}
		for k := range x {
			keys[k] = true
		}
		for k := range y {
			keys[k] = true
		}
		names := make([]string, 0, len(keys))
		for k := range keys {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			av, aok := x[k]
			bv, bok := y[k]
			if aok != bok {
				return fmt.Sprintf("%s.%s: present on one side only (engine %v, oracle %v)", path, k, aok, bok)
			}
			if d := diffValue(path+"."+k, av, bv); d != "" {
				return d
			}
		}
		return ""
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return fmt.Sprintf("%s: engine %d items, oracle %v", path, len(x), b)
		}
		for i := range x {
			if d := diffValue(fmt.Sprintf("%s[%d]", path, i), x[i], y[i]); d != "" {
				return d
			}
		}
		return ""
	case string:
		y, ok := b.(string)
		if !ok {
			return fmt.Sprintf("%s: engine %q, oracle %v", path, x, b)
		}
		p, _, e1 := apd.NewFromString(x)
		q, _, e2 := apd.NewFromString(y)
		if e1 == nil && e2 == nil {
			if p.Cmp(q) != 0 {
				return fmt.Sprintf("%s: engine %s, oracle %s", path, x, y)
			}
			return ""
		}
		if x != y {
			return fmt.Sprintf("%s: engine %q, oracle %q", path, x, y)
		}
		return ""
	default:
		if a != b {
			return fmt.Sprintf("%s: engine %v, oracle %v", path, a, b)
		}
		return ""
	}
}
