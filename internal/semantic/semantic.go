// Package semantic is the curated semantic layer the AI's read tools query (PRD-001 AI-03, AI-68; p1-ai-assistant
// design D6): named measures and dimensions, compiled by platform code to the balance service and the reports that
// already compute them, so a measure is the statement line, KPI or sub-ledger total it names. The model never writes a
// query. Every figure it gives is a typed fact: an ID, the measure, its dimension and member, the period or date, the
// value as exact decimal text, the currency and unit, the sign convention, the ledger revision (ENG-46) and a link to
// the report it comes from.
package semantic

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/financials"
	"github.com/cognizhi/fyniq/internal/domain/kpi"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/payables"
	"github.com/cognizhi/fyniq/internal/domain/receivables"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The kinds of a measure: a flow over months (summed), a stock at a month's end, or a KPI, one value a month.
const (
	Flow    = "flow"
	Stock   = "stock"
	Monthly = "monthly"
)

// The units of a fact.
const (
	Amount  = "amount"
	Percent = "percent"
	Months  = "months"
	Ratio   = "ratio"
	Days    = "days"
)

// The grains of a query: one fact a month, or one over the months asked.
const (
	ByMonth = "month"
	Total   = "total"
)

// Dimension is what a measure may be split by.
type Dimension string

// The dimensions.
const (
	None       Dimension = ""
	Account    Dimension = "account"
	Customer   Dimension = "customer"
	Vendor     Dimension = "vendor"
	Department Dimension = "department"
)

// The codes of a query's refusal.
const (
	CodeMeasure   = "SEMANTIC-01" // no such measure
	CodeDimension = "SEMANTIC-02" // the measure is not split by that dimension, or must be
	CodeGrain     = "SEMANTIC-03" // the measure is not given at that grain
	CodeMember    = "SEMANTIC-04" // no such member of the dimension
)

// Error is a query the semantic layer refuses, with the field it is about.
type Error struct{ Code, Field, Message, Fix string }

func (e *Error) Error() string { return e.Code + ": " + e.Field + " " + e.Message }

func refuse(code, field, message, fix string) *Error {
	return &Error{Code: code, Field: field, Message: message, Fix: fix}
}

// Measure is a named measure: what it is, its unit and kind, what it comes from, and the dimensions it may be split by.
// Needs is a dimension it must be split by.
type Measure struct {
	Key, Name, Description string
	Unit, Kind             string
	Source                 string // the report or statement it is, in words
	Sign                   string
	By                     []Dimension
	Needs                  Dimension
	kpi                    string // the KPI it is, for a Monthly measure
}

const (
	statementSign = "as the statements show it: revenue, profit and expenses positive; assets, liabilities and equity positive"
	debitsSign    = "as the ledger signs it: debits positive, credits negative"
	owedSign      = "what is owed, positive; a credit with the party, negative"
)

// Measures are the semantic layer's measures, in the order the registry lists them.
var Measures = []Measure{
	{Key: "revenue", Name: "Revenue", Description: "Revenue recognised", Unit: Amount, Kind: Flow, Source: "the profit and loss's revenue", Sign: statementSign, By: []Dimension{Department}},
	{Key: "gross_profit", Name: "Gross profit", Description: "Revenue less cost of sales", Unit: Amount, Kind: Flow, Source: "the profit and loss's gross profit", Sign: statementSign, By: []Dimension{Department}},
	{Key: "ebitda", Name: "EBITDA", Description: "Operating profit before depreciation and amortisation, without other income", Unit: Amount, Kind: Flow, Source: "the profit and loss's EBITDA", Sign: statementSign, By: []Dimension{Department}},
	{Key: "net_profit", Name: "Net profit", Description: "Profit after every expense", Unit: Amount, Kind: Flow, Source: "the profit and loss's net profit", Sign: statementSign, By: []Dimension{Department}},
	{Key: "costs", Name: "Cost of sales and operating expenses", Description: "Cost of sales and operating expenses together", Unit: Amount, Kind: Flow, Source: "the profit and loss's cost of sales and operating expenses", Sign: statementSign, By: []Dimension{Department}},
	{Key: "spend_sm", Name: "Spend on sales & marketing", Description: "Expenses of accounts whose function is S&M", Unit: Amount, Kind: Flow, Source: "the profit and loss's S&M expenses", Sign: statementSign},
	{Key: "spend_rd", Name: "Spend on research & development", Description: "Expenses of accounts whose function is R&D", Unit: Amount, Kind: Flow, Source: "the profit and loss's R&D expenses", Sign: statementSign},
	{Key: "spend_ga", Name: "Spend on general & administrative", Description: "Expenses of accounts whose function is G&A", Unit: Amount, Kind: Flow, Source: "the profit and loss's G&A expenses", Sign: statementSign},
	{Key: "net_burn", Name: "Net burn", Description: "−(operating + investing cash flow); below zero when the business generates cash", Unit: Amount, Kind: Flow, Source: "the cash flow statement", Sign: "cash used, positive"},
	{Key: "cash", Name: "Cash", Description: "Cash in the money accounts", Unit: Amount, Kind: Stock, Source: "the balance sheet's cash", Sign: statementSign},
	{Key: "receivables", Name: "Owed to us", Description: "Receivables; by customer, each customer's open documents", Unit: Amount, Kind: Stock, Source: "the balance sheet's receivables; by customer, the receivables sub-ledger", Sign: owedSign, By: []Dimension{Customer}},
	{Key: "payables", Name: "We owe", Description: "Payables and accruals; by vendor, each vendor's open bills", Unit: Amount, Kind: Stock, Source: "the balance sheet's payables and accruals; by vendor, the payables sub-ledger", Sign: owedSign, By: []Dimension{Vendor}},
	{Key: "current_assets", Name: "Current assets", Unit: Amount, Kind: Stock, Source: "the balance sheet's current assets", Sign: statementSign},
	{Key: "current_liabilities", Name: "Current liabilities", Unit: Amount, Kind: Stock, Source: "the balance sheet's current liabilities", Sign: statementSign},
	{Key: "total_liabilities", Name: "Total liabilities", Unit: Amount, Kind: Stock, Source: "the balance sheet's total liabilities", Sign: statementSign},
	{Key: "equity", Name: "Equity", Unit: Amount, Kind: Stock, Source: "the balance sheet's equity", Sign: statementSign},
	{Key: "account_movement", Name: "Account movement", Description: "Each account's net movement", Unit: Amount, Kind: Flow, Source: "the trial balance of the months", Sign: debitsSign, By: []Dimension{Account}, Needs: Account},
	{Key: "account_balance", Name: "Account balance", Description: "Each account's balance at the month's end", Unit: Amount, Kind: Stock, Source: "the trial balance from the books' first month", Sign: debitsSign, By: []Dimension{Account}, Needs: Account},
	{Key: "gross_margin", Name: "Gross margin", Unit: Percent, Kind: Monthly, Source: "the KPI gross_margin@1", kpi: "gross_margin"},
	{Key: "ebitda_margin", Name: "EBITDA margin", Unit: Percent, Kind: Monthly, Source: "the KPI ebitda_margin@1", kpi: "ebitda_margin"},
	{Key: "net_margin", Name: "Net margin", Unit: Percent, Kind: Monthly, Source: "the KPI net_margin@1", kpi: "net_margin"},
	{Key: "revenue_growth", Name: "Revenue growth", Unit: Percent, Kind: Monthly, Source: "the KPI revenue_growth@1", kpi: "revenue_growth"},
	{Key: "booked_mrr", Name: "Booked MRR", Unit: Amount, Kind: Monthly, Source: "the KPI booked_mrr@1", kpi: "booked_mrr"},
	{Key: "arr", Name: "ARR (booked)", Unit: Amount, Kind: Monthly, Source: "the KPI arr@1", kpi: "arr"},
	{Key: "runway", Name: "Runway", Unit: Months, Kind: Monthly, Source: "the KPI runway@1", kpi: "runway"},
	{Key: "burn_multiple", Name: "Burn multiple", Unit: Ratio, Kind: Monthly, Source: "the KPI burn_multiple@1", kpi: "burn_multiple"},
	{Key: "current_ratio", Name: "Current ratio", Unit: Ratio, Kind: Monthly, Source: "the KPI current_ratio@1", kpi: "current_ratio"},
	{Key: "quick_ratio", Name: "Quick ratio", Unit: Ratio, Kind: Monthly, Source: "the KPI quick_ratio@1", kpi: "quick_ratio"},
	{Key: "working_capital", Name: "Working capital", Unit: Amount, Kind: Monthly, Source: "the KPI working_capital@1", kpi: "working_capital"},
	{Key: "dso", Name: "DSO", Unit: Days, Kind: Monthly, Source: "the KPI dso@1", kpi: "dso"},
	{Key: "dpo", Name: "DPO", Unit: Days, Kind: Monthly, Source: "the KPI dpo@1", kpi: "dpo"},
}

// MeasureOf is the measure named key.
func MeasureOf(key string) (Measure, bool) {
	i := slices.IndexFunc(Measures, func(m Measure) bool { return m.Key == key })
	if i < 0 {
		return Measure{}, false
	}
	return Measures[i], true
}

// Books are the books a query reads: the base currency, every period earliest first, the run of months asked (indexes
// into Periods, inclusive), the ledger revision every figure is read at, and the financial year a month is in.
type Books struct {
	Base        money.Currency
	Periods     []storage.Period
	First, Last int
	Revision    int64
	YearStart   func(day time.Time) (time.Time, error)
}

// Query asks for a measure over the books' months, at a grain, split by a dimension, kept to one member of it or not.
type Query struct {
	Measure string
	Grain   string
	By      Dimension
	Member  string // a member's ID (an account may be named by its code too); "" for every member
}

// Fact is one figure of the semantic layer (AI-68).
type Fact struct {
	ID         string
	Measure    string
	Name       string
	Value      string // exact decimal text; "" when the fact has no value
	Words      string // what stands for the value, such as "Not burning"
	Undefined  string // why it has no value
	Unit       string
	Currency   string // for an amount
	From, To   time.Time
	Dimension  Dimension
	Member     string
	MemberName string
	Sign       string
	Revision   int64
	Source     string
	Lineage    string // the report it comes from, as an API path
}

// Run answers q from the books in u, read at b.Revision.
func Run(ctx context.Context, u storage.UnitOfWork, b Books, q Query) ([]Fact, error) {
	m, ok := MeasureOf(q.Measure)
	switch {
	case !ok:
		return nil, refuse(CodeMeasure, "measure", "is not a measure of the semantic layer", "Choose one the registry lists")
	case q.By != None && !slices.Contains(m.By, q.By):
		return nil, refuse(CodeDimension, "by", fmt.Sprintf("is not a dimension %s is split by", m.Key), "Split it by one the registry lists for it, or by none")
	case m.Needs != None && q.By != m.Needs:
		return nil, refuse(CodeDimension, "by", fmt.Sprintf("must be %s for %s", m.Needs, m.Key), "Split it by "+string(m.Needs))
	case q.Member != "" && q.By == None:
		return nil, refuse(CodeMember, "member", "needs a dimension to be a member of", "Name the dimension too")
	case q.Grain != ByMonth && q.Grain != Total:
		return nil, refuse(CodeGrain, "grain", "is not month or total", "Ask by month, or for the total of the months")
	case m.Kind == Monthly && q.Grain == Total && b.First != b.Last:
		return nil, refuse(CodeGrain, "grain", fmt.Sprintf("is total, but %s is a KPI of one month", m.Key), "Ask for it by month")
	case (q.By == Customer || q.By == Vendor || q.By == Department) && q.Grain == ByMonth && b.First != b.Last:
		return nil, refuse(CodeGrain, "grain", fmt.Sprintf("is month, but %s by %s is given over the months asked", m.Key, q.By), "Ask for the total")
	}
	r := run{u: u, b: b, m: m, q: q}
	switch q.By {
	case Account:
		return r.accounts(ctx)
	case Customer, Vendor:
		return r.parties(ctx)
	case Department:
		return r.departments(ctx)
	}
	return r.organisation(ctx)
}

type run struct {
	u storage.UnitOfWork
	b Books
	m Measure
	q Query
}

// fact is a fact of the run's measure over [from, to].
func (r run) fact(from, to time.Time, member, name string) Fact {
	sign := r.m.Sign
	if sign == "" {
		sign = "as the KPI page shows it, rounded by RND-KPI"
	}
	f := Fact{Measure: r.m.Key, Name: r.m.Name, Unit: r.m.Unit, From: from, To: to, Dimension: r.q.By, Member: member, MemberName: name, Sign: sign,
		Revision: r.b.Revision, Source: r.m.Source, Lineage: r.lineage(from, to, member)}
	if r.m.Unit == Amount {
		f.Currency = r.b.Base.Code
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{r.m.Key, from.Format(time.DateOnly), to.Format(time.DateOnly), string(r.q.By), member,
		fmt.Sprint(r.b.Revision)}, "|")))
	f.ID = "F" + hex.EncodeToString(sum[:8])
	return f
}

// lineage is the report a fact comes from, as the API serves it, read at the fact's revision.
func (r run) lineage(from, to time.Time, member string) string {
	revision := ""
	if r.b.Revision > 0 {
		revision = fmt.Sprintf("&as_at_revision=%d", r.b.Revision)
	}
	months := fmt.Sprintf("from=%s&to=%s", from.Format("2006-01"), to.Format("2006-01")) + revision
	switch {
	case r.q.By == Account:
		return "/v1/reports/general-ledger?account_id=" + member + "&from=" + from.Format(time.DateOnly) + "&to=" + to.Format(time.DateOnly) + revision
	case r.q.By == Customer:
		return "/v1/receivables?as_at=" + to.Format(time.DateOnly)
	case r.q.By == Vendor:
		return "/v1/payables?as_at=" + to.Format(time.DateOnly)
	case r.m.Kind == Monthly:
		return "/v1/kpis?month=" + to.Format("2006-01") + revision
	case r.m.Key == "net_burn":
		return "/v1/reports/statements/cf?" + months
	case r.m.Kind == Stock:
		return "/v1/reports/statements/bs?" + months
	}
	return "/v1/reports/statements/pl?" + months
}

// months are the run's months, or its whole span for the total.
func (r run) spans() [][2]int {
	if r.q.Grain == Total {
		return [][2]int{{r.b.First, r.b.Last}}
	}
	var out [][2]int
	for i := r.b.First; i <= r.b.Last; i++ {
		out = append(out, [2]int{i, i})
	}
	return out
}

func (r run) from(i int) time.Time { return r.b.Periods[i].StartsOn }
func (r run) to(i int) time.Time   { return r.b.Periods[i].EndsOn }

// month reads a month's KPI figures and values at the run's revision.
func (r run) month(ctx context.Context, i int) (kpi.Month, []kpi.Value, error) {
	start := time.Time{}
	if r.b.YearStart != nil {
		var err error
		if start, err = r.b.YearStart(r.b.Periods[i].StartsOn); err != nil {
			return kpi.Month{}, nil, err
		}
	}
	months, before, err := kpi.Read(ctx, r.u, r.b.Base, r.b.Periods, i, start, r.b.Revision)
	if err != nil {
		return kpi.Month{}, nil, err
	}
	return months[len(months)-1], kpi.Compute(months, before, r.b.Base), nil
}

func figure(m kpi.Month, key string) money.Money {
	switch key {
	case "revenue":
		return m.Revenue
	case "gross_profit":
		return m.GrossProfit
	case "ebitda":
		return m.EBITDA
	case "net_profit":
		return m.NetProfit
	case "costs":
		return m.Costs
	case "spend_sm":
		return m.SM
	case "spend_rd":
		return m.RD
	case "spend_ga":
		return m.GA
	case "net_burn":
		return m.NetBurn
	case "cash":
		return m.Cash
	case "receivables":
		return m.Receivables
	case "payables":
		return m.Payables
	case "current_assets":
		return m.CurrentAssets
	case "current_liabilities":
		return m.CurrentLiabilities
	case "total_liabilities":
		return m.TotalLiabilities
	}
	return m.Equity
}

// organisation answers a measure of the whole organisation.
func (r run) organisation(ctx context.Context) ([]Fact, error) {
	var out []Fact
	for _, s := range r.spans() {
		f := r.fact(r.from(s[0]), r.to(s[1]), "", "")
		switch r.m.Kind {
		case Monthly:
			_, values, err := r.month(ctx, s[1])
			if err != nil {
				return nil, err
			}
			for _, v := range values {
				if v.Key == r.m.kpi {
					f.Value, f.Words, f.Undefined = v.Figure, v.Words, v.Undefined
				}
			}
		case Stock:
			m, _, err := r.month(ctx, s[1])
			if err != nil {
				return nil, err
			}
			f.From, f.Value = f.To, figure(m, r.m.Key).String()
		default:
			total := money.Zero(r.b.Base)
			for i := s[0]; i <= s[1]; i++ {
				m, _, err := r.month(ctx, i)
				if err != nil {
					return nil, err
				}
				if total, err = total.Add(figure(m, r.m.Key)); err != nil {
					return nil, err
				}
			}
			f.Value = total.String()
		}
		out = append(out, f)
	}
	return out, nil
}

// accounts answers an account's movement or balance from the trial balance (the balance service, ENG-04).
func (r run) accounts(ctx context.Context) ([]Fact, error) {
	list, err := r.u.Accounts().List(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[uuid.UUID]storage.Account{}
	for _, a := range list {
		byID[a.ID] = a
	}
	keep := func(a storage.Account) bool {
		return r.q.Member == "" || a.ID.String() == r.q.Member || a.Code == r.q.Member
	}
	if r.q.Member != "" && !slices.ContainsFunc(list, keep) {
		return nil, refuse(CodeMember, "member", "is not an account of the chart", "Name an account by its ID or code")
	}
	var out []Fact
	for _, s := range r.spans() {
		first := s[0]
		if r.m.Kind == Stock {
			first = 0
		}
		var periods []uuid.UUID
		for i := first; i <= s[1]; i++ {
			periods = append(periods, r.b.Periods[i].ID)
		}
		tb, err := balance.TrialAt(ctx, r.u, r.b.Base, balance.At(r.b.Revision), periods...)
		if err != nil {
			return nil, err
		}
		for _, row := range tb.Rows {
			a, ok := byID[row.AccountID]
			if !ok || !keep(a) {
				continue
			}
			f := r.fact(r.from(s[0]), r.to(s[1]), a.ID.String(), a.Code+" "+a.Name)
			if r.m.Kind == Stock {
				f.From = f.To
			}
			f.Value = row.Base.String()
			out = append(out, f)
		}
	}
	return out, nil
}

// parties answers what each customer owes the organisation, or the organisation each vendor, at the run's last month
// end, from the sub-ledgers in the base currency.
func (r run) parties(ctx context.Context) ([]Fact, error) {
	list, err := r.u.Accounts().List(ctx)
	if err != nil {
		return nil, err
	}
	asAt := r.to(r.b.Last)
	type party struct {
		name string
		owed money.Money
	}
	owed := map[uuid.UUID]*party{}
	add := func(id uuid.UUID, name string, items []receivables.Item) error {
		p := owed[id]
		if p == nil {
			p = &party{name: name, owed: money.Zero(r.b.Base)}
			owed[id] = p
		}
		for _, it := range items {
			v, err := p.owed.Add(it.Carrying)
			if err != nil {
				return err
			}
			p.owed = v
		}
		return nil
	}
	for _, a := range list {
		switch {
		case r.q.By == Customer && string(a.Role) == ledger.RoleAccountsReceivable:
			l, err := receivables.Open(ctx, r.u, r.b.Base, a.ID, asAt)
			if err != nil {
				return nil, err
			}
			for _, c := range l.Customers {
				if err := add(c.ID, c.Name, c.Items); err != nil {
					return nil, err
				}
			}
		case r.q.By == Vendor && string(a.Role) == ledger.RoleAccountsPayable:
			l, err := payables.Open(ctx, r.u, r.b.Base, a.ID, asAt)
			if err != nil {
				return nil, err
			}
			for _, s := range l.Suppliers {
				if err := add(s.ID, s.Name, s.Items); err != nil {
					return nil, err
				}
			}
		}
	}
	var out []Fact
	for id, p := range owed {
		if r.q.Member != "" && id.String() != r.q.Member {
			continue
		}
		f := r.fact(asAt, asAt, id.String(), p.name)
		f.Value = p.owed.String()
		out = append(out, f)
	}
	slices.SortFunc(out, func(a, b Fact) int { return strings.Compare(a.MemberName+a.Member, b.MemberName+b.Member) })
	return out, nil
}

// departments answers a profit and loss line by the department dimension's values, over the months asked: one fact
// for each value, and one for the lines with none.
func (r run) departments(ctx context.Context) ([]Fact, error) {
	dims, err := r.u.Dimensions().List(ctx)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(dims, func(d storage.Dimension) bool { return d.Kind == storage.DimensionDepartment })
	if i < 0 {
		return nil, refuse(CodeDimension, "by", "is department, but the organisation has no department dimension", "Ask for the organisation's total")
	}
	pl, err := financials.Lay(ctx, r.u, r.b.Base, financials.Request{Kind: financials.ProfitAndLoss, All: r.b.Periods, First: r.b.First, Last: r.b.Last,
		Revision: r.b.Revision, Pivot: dims[i].ID})
	if err != nil {
		return nil, err
	}
	var out []Fact
	for c, col := range pl.Columns {
		member, ok := strings.CutPrefix(col.Key, "value:")
		if !ok && col.Key != "none" {
			continue
		}
		if r.q.Member != "" && member != r.q.Member {
			continue
		}
		total := money.Zero(r.b.Base)
		for _, blk := range pl.Blocks {
			if blk.Key == r.m.Key || (r.m.Key == "costs" && (blk.Key == "cost_of_sales" || blk.Key == "operating_expenses")) {
				if total, err = total.Add(blk.Totals[c]); err != nil {
					return nil, err
				}
			}
		}
		f := r.fact(r.from(r.b.First), r.to(r.b.Last), member, col.Label)
		f.Value = total.String()
		out = append(out, f)
	}
	return out, nil
}
