package migration

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/forecasts"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/sheet"
)

// KindForecast is the Forecast sheet's drivers as the company's forecast (design D5, task 4.6; rev 0.264).
const KindForecast = "forecast"

// Forecast is the Forecast sheet: its controls, scenario levers, drivers and plans, each as the workbook holds it, and
// the tax settings the forecast reads from the Settings sheet.
type Forecast struct {
	Origin
	Horizon                *apd.Decimal
	CashAdjustment, Buffer *apd.Decimal
	// What the workbook's books held at its last actual month, as the sheet shows them: what Fyniq's books must give the
	// forecast too (FR-MIG-02).
	Cash, Receivables, Payables *apd.Decimal
	Levers                      []Lever
	Customers, NewCustomers     *apd.Decimal
	NewGrowth, Churn            *apd.Decimal
	ARPA, PriceIncrease         *apd.Decimal
	Services, ServicesGrowth    *apd.Decimal
	Other                       *apd.Decimal
	Collected                   [3]*apd.Decimal // in the month of sale, one month later, two months later
	Hosting, AI, Contractors    *apd.Decimal
	Fees, SalaryIncrease        *apd.Decimal
	Opex                        []OpexRow
	Hires                       []Hire
	Capex                       []CapexRow
	Funding                     []FundingRow
}

// Lever is a scenario lever: its Base, Best and Worst values.
type Lever struct {
	Label  string
	Values [3]*apd.Decimal
}

// OpexRow, Hire, CapexRow and FundingRow are the rows of the Forecast sheet's plans.
type OpexRow struct {
	Origin
	Label                 string
	Base, Growth, Percent *apd.Decimal
}

type Hire struct {
	Origin
	Role, Function string
	Start, End     time.Time
	Salary         *apd.Decimal
	CPF            bool
}

type CapexRow struct {
	Origin
	Item         string
	Month        time.Time
	Amount, Life *apd.Decimal
}

type FundingRow struct {
	Origin
	Type         string
	Month        time.Time
	Amount, Rate *apd.Decimal
	Tenor        *apd.Decimal
	Notes        string
}

// readForecast reads the Forecast sheet (task 4.6): the drivers by their labels in column B, each value in column C,
// the levers' Base, Best and Worst in C to E, and the plans as the tables under their titles, each to the next title.
func (w *Workbook) readForecast(g sheet.Grid) {
	const labels, values = 1, 2 // columns B and C
	f := &w.Forecast
	f.Origin = Origin{Sheet: "Forecast", Row: 1}
	if len(g.Rows) == 0 {
		return // a missing sheet is a problem already
	}
	find := func(prefix string, from int) int {
		for r := max(from, 1); r <= len(g.Rows); r++ {
			if strings.HasPrefix(strings.TrimSpace(g.Cell(r, labels)), prefix) {
				return r
			}
		}
		w.Problems = append(w.Problems, Problem{Sheet: "Forecast", Column: prefix, Severity: Error, Code: CodeColumn,
			Message: fmt.Sprintf("the Forecast sheet has no %q", prefix), Fix: "Restore the row from the published workbook"})
		return 0
	}
	number := func(r, c int, label string) *apd.Decimal {
		v := strings.TrimSpace(g.Cell(r, c))
		if r == 0 || v == "" {
			return nil
		}
		d, _, err := apd.NewFromString(v)
		if err != nil || d.Form != apd.Finite {
			w.Problems = append(w.Problems, Problem{Sheet: "Forecast", Row: r, Column: label, Severity: Error, Code: CodeValue,
				Message: fmt.Sprintf("%q is not a number", v), Fix: "Enter a number"})
			return nil
		}
		return d
	}
	driver := func(prefix string) *apd.Decimal { return number(find(prefix, 1), values, prefix) }
	f.Horizon = driver("Months to forecast")
	f.CashAdjustment = driver("Adjustment to starting cash")
	f.Buffer = driver("Minimum cash buffer to keep")
	f.Cash = driver("Cash at the start (actual, from your books)")
	f.Receivables = driver("Customers owe us at the start")
	f.Payables = driver("We owe suppliers at the start")
	if top := find("SCENARIO LEVERS", 1); top > 0 {
		for _, l := range []string{"New customers won", "Customer churn (×", "Price / ARPA", "Services & project revenue (×", "Operating costs",
			"Direct cost percentages", "Hiring delay", "Fundraising delay", "Fundraising amount"} {
			r := find(l, top)
			f.Levers = append(f.Levers, Lever{Label: l, Values: [3]*apd.Decimal{number(r, values, l), number(r, values+1, l), number(r, values+2, l)}})
		}
	}
	f.Customers = driver("Paying customers at the start")
	f.NewCustomers = driver("New customers per month")
	f.NewGrowth = driver("Monthly growth in new customers")
	f.Churn = driver("Monthly customer churn")
	f.ARPA = driver("Revenue per customer per month")
	f.PriceIncrease = driver("Price increase each year")
	f.Services = driver("Services & project revenue per month")
	f.ServicesGrowth = driver("Monthly growth in services revenue")
	f.Other = driver("Other revenue per month")
	f.Collected = [3]*apd.Decimal{driver("Customers pay in the month of sale"), driver("…one month later"), driver("…two months later")}
	f.Hosting = driver("Hosting & infrastructure (% of subscription revenue)")
	f.AI = driver("AI / API & third-party costs")
	f.Contractors = driver("Contractors (% of services revenue)")
	f.Fees = driver("Payment fees (% of total revenue)")
	f.SalaryIncrease = driver("Salary increase each year")
	// A plan's table: its headers on the row after its title, its rows up to the next section's title, a row without
	// its first column left out.
	plan := func(title, next, first string, each func(t table, r int)) {
		top, end := find(title, 1), find(next, 1)
		if top == 0 || end == 0 {
			return
		}
		t := w.headed("Forecast", g, top+1)
		for r := top + 2; r < end; r++ {
			if t.text(r, first) != "" {
				each(t, r)
			}
		}
	}
	plan("OTHER OPERATING EXPENSES", "HEADCOUNT PLAN", "Expense line", func(t table, r int) {
		f.Opex = append(f.Opex, OpexRow{Origin: Origin{"Forecast", r}, Label: t.text(r, "Expense line"), Base: t.number(r, "Monthly amount now"),
			Growth: t.number(r, "Monthly growth"), Percent: t.number(r, "Plus % of revenue")})
	})
	plan("HEADCOUNT PLAN", "CAPITAL EXPENDITURE PLAN", "Role / name", func(t table, r int) {
		f.Hires = append(f.Hires, Hire{Origin: Origin{"Forecast", r}, Role: t.text(r, "Role / name"), Function: t.text(r, "Function"),
			Start: t.date(r, "Start month"), End: t.date(r, "End month (optional)"), Salary: t.number(r, "Monthly salary"), CPF: t.yes(r, "CPF? (SC / PR)")})
	})
	plan("CAPITAL EXPENDITURE PLAN", "FUNDING PLAN", "Item", func(t table, r int) {
		f.Capex = append(f.Capex, CapexRow{Origin: Origin{"Forecast", r}, Item: t.text(r, "Item"), Month: t.date(r, "Month"),
			Amount: t.number(r, "Amount"), Life: t.number(r, "Useful life (months)")})
	})
	plan("FUNDING PLAN", "SUMMARY BY FORECAST YEAR", "Type", func(t table, r int) {
		f.Funding = append(f.Funding, FundingRow{Origin: Origin{"Forecast", r}, Type: t.text(r, "Type"), Month: t.date(r, "Month"),
			Amount: t.number(r, "Amount"), Rate: t.number(r, "Loan interest (yearly)"), Tenor: t.number(r, "Loan repaid over (months)"),
			Notes: t.text(r, "Notes")})
	})
}

// fraction is a rate as the forecast keeps it, a decimal fraction ("0.04"); zero for an empty cell.
func fraction(d *apd.Decimal) string {
	if d == nil {
		return "0"
	}
	x := new(apd.Decimal).Set(d)
	x.Reduce(x)
	return x.Text('f')
}

// money2 is an amount of the base currency with its two decimals; zero for an empty cell.
func money2(d *apd.Decimal) string {
	x := new(apd.Decimal)
	if d != nil {
		if _, err := apd.BaseContext.WithPrecision(40).Quantize(x, d, -2); err != nil {
			return d.Text('f')
		}
	}
	return x.Text('f')
}

// whole is a count the forecast keeps as an int.
func whole(d *apd.Decimal) int {
	if d == nil {
		return 0
	}
	n, err := d.Int64()
	if err != nil {
		return 0
	}
	return int(n)
}

// monthOf is a month as the forecast names it (YYYY-MM).
func monthOf(t time.Time) string { return t.Format("2006-01") }

// forecastRecord is the Forecast sheet as the company's forecast (design D5, task 4.6; rev 0.264): its drivers, plans
// and the Base, Best and Worst levers as a recorded forecast, which never posts (FR-PLN-08); the opex lines whose label
// names a report group of the chart plan it. The history keeps the workbook's last month of actuals, so the forecast
// starts from the same books as the workbook's; a clean start leaves it to the last closed month, as its books begin
// at the cut-over [accountant to confirm].
func (w *Workbook) forecastRecord(path string) Record {
	f := w.Forecast
	return Record{Key: "forecast", Kind: KindForecast, Origin: f.Origin,
		make: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Made, error) {
			base, err := baseOf(ctx, u)
			if err != nil {
				return Made{}, err
			}
			accounts, err := u.Accounts().List(ctx)
			if err != nil {
				return Made{}, err
			}
			groups := map[string]string{}
			for _, a := range accounts {
				if !a.Group && (a.Type == storage.Expense || a.Type == storage.Revenue) && a.ReportGroup != "" {
					groups[strings.ToLower(strings.TrimSpace(a.ReportGroup))] = strings.TrimSpace(a.ReportGroup)
				}
			}
			buffer := whole(f.Buffer)
			p := forecasts.ForecastPlan{Name: "Forecast from the workbook", Horizon: whole(f.Horizon), CashAdjustment: money2(f.CashAdjustment),
				BufferMonths: &buffer,
				Revenue: forecasts.ForecastRevenue{Customers: fraction(f.Customers), NewCustomers: fraction(f.NewCustomers), NewGrowth: fraction(f.NewGrowth),
					Churn: fraction(f.Churn), ARPA: money2(f.ARPA), PriceIncrease: fraction(f.PriceIncrease), Services: money2(f.Services),
					ServicesGrowth: fraction(f.ServicesGrowth), Other: money2(f.Other),
					Collection: forecasts.ForecastCollection{ThisMonth: fraction(f.Collected[0]), NextMonth: fraction(f.Collected[1]), MonthAfter: fraction(f.Collected[2])}},
				Costs: forecasts.ForecastCosts{Hosting: fraction(f.Hosting), AI: fraction(f.AI), Contractors: fraction(f.Contractors), Fees: fraction(f.Fees),
					SalaryIncrease: fraction(f.SalaryIncrease)},
				Opex: []forecasts.ForecastOpex{}, Headcount: []forecasts.ForecastHire{}, Capex: []forecasts.ForecastCapex{}, Funding: []forecasts.ForecastFunding{},
				Tax: forecasts.ForecastTax{StartUpYears: whole(w.Settings.StartUpYears), LossesBroughtForward: money2(w.Settings.LossesBroughtForward),
					PaymentLag: whole(w.Settings.TaxLag)}}
			if path == storage.PathHistory && !w.Settings.LastActual.IsZero() {
				p.LastActual = monthOf(w.Settings.LastActual)
			}
			for _, o := range f.Opex {
				p.Opex = append(p.Opex, forecasts.ForecastOpex{Label: o.Label, Group: groups[strings.ToLower(o.Label)], Base: money2(o.Base),
					Growth: fraction(o.Growth), Percent: fraction(o.Percent)})
			}
			for _, h := range f.Hires {
				x := forecasts.ForecastHire{Role: h.Role, Function: h.Function, Start: monthOf(h.Start), Salary: money2(h.Salary), CPF: h.CPF}
				if !h.End.IsZero() {
					x.End = monthOf(h.End)
				}
				p.Headcount = append(p.Headcount, x)
			}
			for _, c := range f.Capex {
				p.Capex = append(p.Capex, forecasts.ForecastCapex{Item: c.Item, Amount: money2(c.Amount), Month: monthOf(c.Month), Life: whole(c.Life)})
			}
			for _, x := range f.Funding {
				fu := forecasts.ForecastFunding{Type: x.Type, Month: monthOf(x.Month), Amount: money2(x.Amount), Tenor: whole(x.Tenor), Notes: x.Notes}
				if x.Rate != nil && !x.Rate.IsZero() {
					fu.Rate = fraction(x.Rate)
				}
				p.Funding = append(p.Funding, fu)
			}
			for i, name := range []string{"Base", "Best", "Worst"} {
				lv := func(k int) *apd.Decimal {
					if k < len(f.Levers) {
						return f.Levers[k].Values[i]
					}
					return nil
				}
				times := func(k int) string { // a multiplier the sheet leaves empty is the plan's (× 1)
					if lv(k) == nil {
						return "1"
					}
					return fraction(lv(k))
				}
				p.Scenarios = append(p.Scenarios, forecasts.ForecastScenario{Name: name, Levers: forecasts.ForecastLevers{NewCustomers: times(0),
					Churn: times(1), Price: times(2), Services: times(3), Opex: times(4), DirectCosts: times(5),
					HireDelay: whole(lv(6)), FundDelay: whole(lv(7)), FundAmount: times(8)}})
			}
			doc, err := forecasts.DraftIn(ctx, u, p, base, e.Now)
			if err != nil {
				return Made{}, err
			}
			doc, err = forecasts.RecordIn(ctx, u, doc.ID, base, e.Now)
			return Made{Type: "document", ID: doc.ID}, err
		}}
}
