package differential

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/claim"
	"github.com/cognizhi/fyniq/internal/domain/financials"
	"github.com/cognizhi/fyniq/internal/domain/kpi"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The statements and the KPIs (README, "Statements" and "KPIs"): the engine's own financials.Lay and kpi.Read read the
// entries the engine posted, through memStore, and their blocks are written in the result's shape. Only presentation
// is mapped here: every figure is the engine's.

// booked is an entry the engine posted, with its lines as the result writes them.
type booked struct {
	doc, kind string
	entry     ledger.Entry
	lines     []ledger.Line
}

// kpiInput is the scenario's KPI request: the financial year's first month and each month's entered counts.
type kpiInput struct {
	YearStartMonth int `json:"year_start_month"`
	Counts         map[string]struct {
		Paying    *int64 `json:"paying_customers"`
		New       *int64 `json:"new_customers"`
		Lost      *int64 `json:"customers_lost"`
		Headcount *int64 `json:"headcount"`
	} `json:"counts"`
}

// month is the first day of t's month.
func month(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC) }

// newMemStore is the store of a scenario's chart and the entries the engine posted, a period per calendar month from
// the first posting month to the last.
func newMemStore(s Scenario, settings ledger.Settings, base money.Currency, book []booked) (*memStore, map[uuid.UUID]booked, error) {
	m := &memStore{base: base, byCode: map[string]storage.Account{}, settings: map[string][]storage.SettingChange{}}
	// The statements' roles are the chart's own (README, "Statements"); a settings key that gave another account the
	// same role (the P1 sources' depreciation account) yields to it, as a chart holds each role once.
	statementRoles := map[string]bool{}
	for _, a := range s.Accounts {
		switch a.Role {
		case "retained-earnings", "depreciation", "income-tax-expense":
			statementRoles[a.Role] = true
		}
	}
	for _, a := range s.Accounts {
		id := stableID("account", a.Code)
		role := settings.Accounts[id].Role
		switch {
		case a.Role == "retained-earnings" || a.Role == "depreciation" || a.Role == "income-tax-expense":
			role = a.Role
		case statementRoles[role]:
			role = ""
		}
		x := storage.Account{ID: id, Code: a.Code, Name: a.Code, Type: storage.AccountType(a.Type), Group: a.Group, Active: a.Active,
			Role: storage.AccountRole(role), AccountAttributes: storage.AccountAttributes{ReportGroup: a.ReportGroup,
				CashFlowLine: a.CashFlowLine, Function: a.Function, MoneyAccount: settings.Accounts[id].Money}}
		m.accounts = append(m.accounts, x)
		m.byCode[a.Code] = x
	}
	advances := ""
	if s.Settings != nil {
		advances = s.Settings.Accounts["advances"]
	}
	// The advances account is the organisation's setting: none unless the scenario names it.
	m.settings[claim.AdvancesSetting] = []storage.SettingChange{{Key: claim.AdvancesSetting, Value: advances, Version: 1}}
	if len(book) == 0 {
		return m, nil, nil
	}
	first, last := month(book[0].entry.PostingDate), month(book[0].entry.PostingDate)
	for _, b := range book {
		d := month(b.entry.PostingDate)
		if d.Before(first) {
			first = d
		}
		if d.After(last) {
			last = d
		}
	}
	periodOf := map[time.Time]uuid.UUID{}
	for d := first; !d.After(last); d = d.AddDate(0, 1, 0) {
		p := storage.Period{ID: stableID("period", d.Format("2006-01")), StartsOn: d, EndsOn: d.AddDate(0, 1, -1), Status: storage.PeriodOpen, Version: 1}
		m.periods = append(m.periods, p)
		periodOf[d] = p.ID
	}
	// The entries in the order they posted, as the ledger's revisions run: the result's own order.
	byEntry := map[uuid.UUID]booked{}
	for i, b := range book {
		rev := i
		e := storage.Entry{ID: stableID("entry", fmt.Sprint(i)), LedgerRevision: int64(rev + 1), PostingDate: b.entry.PostingDate,
			PeriodID: periodOf[month(b.entry.PostingDate)], Source: storage.Source{Type: b.entry.Source.Type, ID: b.entry.Source.ID, Version: b.entry.Source.Version}}
		for k, l := range b.lines {
			line := storage.Line{LineNo: k + 1, AccountID: l.AccountID, Currency: l.Amount.Currency().Code, Amount: storage.Decimal(l.Amount.String()),
				BaseCurrency: base.Code, BaseAmount: storage.Decimal(l.Base.String())}
			if l.Party != uuid.Nil {
				p := l.Party
				line.PartyID = &p
			}
			e.Lines = append(e.Lines, line)
		}
		m.entries = append(m.entries, e)
		byEntry[e.ID] = b
		if b.entry.Source.Type == ledger.SourceShareIssue && b.kind == "post" { // a conversion: shares for debt, not cash
			if id, err := uuid.Parse(b.entry.Source.ID); err == nil {
				m.issues = append(m.issues, storage.ShareIssue{ID: id, Consideration: storage.ConsiderationDebtCapitalised})
			}
		}
	}
	if len(s.KPIs) > 0 {
		var k kpiInput
		if err := json.Unmarshal(s.KPIs, &k); err != nil {
			return nil, nil, err
		}
		for key, c := range k.Counts {
			d, err := time.Parse("2006-01", key)
			if err != nil {
				return nil, nil, err
			}
			m.inputs = append(m.inputs, storage.KPIInput{Month: d, Counts: storage.KPICounts{Paying: c.Paying, New: c.New, Lost: c.Lost, Headcount: c.Headcount}})
		}
	}
	return m, byEntry, nil
}

// state lays out every month's statements, and its KPIs when the scenario asks for them.
func state(s Scenario, settings ledger.Settings, base money.Currency, book []booked) (map[string]any, map[string]any, error) {
	m, byEntry, err := newMemStore(s, settings, base, book)
	if err != nil {
		return nil, nil, err
	}
	statements := map[string]any{}
	var kpis map[string]any
	var input kpiInput
	if len(s.KPIs) > 0 {
		kpis = map[string]any{}
		if err := json.Unmarshal(s.KPIs, &input); err != nil {
			return nil, nil, err
		}
	}
	ctx := context.Background()
	revision := int64(len(m.entries))
	for i, p := range m.periods {
		lay := func(k financials.Kind) (financials.Statement, error) {
			return financials.Lay(ctx, m, base, financials.Request{Kind: k, All: m.periods, First: i, Last: i, Revision: revision})
		}
		pl, err := lay(financials.ProfitAndLoss)
		if err != nil {
			return nil, nil, err
		}
		bs, err := lay(financials.BalanceSheet)
		if err != nil {
			return nil, nil, err
		}
		cf, err := lay(financials.CashFlow)
		if err != nil {
			return nil, nil, err
		}
		key := p.StartsOn.Format("2006-01")
		statements[key] = map[string]any{
			"profit_and_loss": profitAndLoss(pl),
			"balance_sheet":   balanceSheet(bs, m.accounts),
			"cash_flow":       cashFlow(cf, byEntry),
			"equity_movement": equityMovement(bs),
			"checks":          checks(bs, cf),
		}
		if kpis != nil {
			start := time.Date(p.StartsOn.Year(), time.Month(max(input.YearStartMonth, 1)), 1, 0, 0, 0, 0, time.UTC)
			if start.After(p.StartsOn) {
				start = start.AddDate(-1, 0, 0)
			}
			months, before, err := kpi.Read(ctx, m, base, m.periods, i, start, revision)
			if err != nil {
				return nil, nil, err
			}
			kpis[key] = kpiValues(kpi.Compute(months, before, base))
		}
	}
	return statements, kpis, nil
}

// blockOf is the statement's block with key, if it has one.
func blockOf(s financials.Statement, key string) (financials.Block, bool) {
	for _, b := range s.Blocks {
		if b.Key == key {
			return b, true
		}
	}
	return financials.Block{}, false
}

// total is a block's figure in the statement's one column, "0" when it has none.
func total(s financials.Statement, key string) string {
	if b, ok := blockOf(s, key); ok && len(b.Totals) > 0 {
		return signed(s, key, b.Totals[0]).String()
	}
	return "0"
}

// groupName is the line an account is shown under in the result: its report group, or its code (README, "Statements").
func groupName(a storage.Account) string {
	if g := strings.TrimSpace(a.ReportGroup); g != "" {
		return g
	}
	return a.Code
}

// costs are the P&L's blocks of costs: the result shows each P&L figure as what it adds to profit, so costs are
// negative where the statement shows them as positive debits (README, "Statements").
var costs = map[string]bool{financials.KeyCostOfSales: true, financials.KeyOperatingExpenses: true, financials.KeyDepreciation: true,
	financials.KeyFinanceAndOther: true, financials.KeyIncomeTax: true}

// signed is a figure as the result shows it: negated for a P&L cost.
func signed(s financials.Statement, key string, m money.Money) money.Money {
	if s.Kind == financials.ProfitAndLoss && costs[key] {
		return m.Neg()
	}
	return m
}

// section is a block's total and its figure per report group, regrouped from the block's own lines.
func section(s financials.Statement, key string) map[string]any {
	groups := map[string]money.Money{}
	b, _ := blockOf(s, key)
	for _, g := range b.Groups {
		for _, l := range g.Lines {
			name, amount := groupName(l.Account), signed(s, key, l.Amounts[0])
			if x, ok := groups[name]; ok {
				groups[name], _ = x.Add(amount)
			} else {
				groups[name] = amount
			}
		}
	}
	out := map[string]any{}
	for name, x := range groups {
		out[name] = x.String()
	}
	return map[string]any{"total": total(s, key), "groups": out}
}

// plKeys are the P&L's blocks, by the result's names.
var plKeys = [][2]string{{"revenue", financials.KeyRevenue}, {"cost_of_sales", financials.KeyCostOfSales}, {"gross_profit", financials.KeyGrossProfit},
	{"operating_expenses", financials.KeyOperatingExpenses}, {"ebitda", financials.KeyEBITDA},
	{"depreciation_and_amortisation", financials.KeyDepreciation}, {"operating_profit", financials.KeyOperatingProfit},
	{"other_income", financials.KeyOtherIncome}, {"finance_costs_and_other_expenses", financials.KeyFinanceAndOther},
	{"profit_before_tax", financials.KeyProfitBeforeTax}, {"income_tax", financials.KeyIncomeTax}, {"net_profit", financials.KeyNetProfit}}

func profitAndLoss(pl financials.Statement) map[string]any {
	out := map[string]any{}
	pct := map[string]any{}
	for _, k := range plKeys {
		switch k[0] {
		case "revenue", "cost_of_sales", "depreciation_and_amortisation", "other_income", "finance_costs_and_other_expenses":
			out[k[0]] = section(pl, k[1])
		case "operating_expenses":
			opex := section(pl, k[1])
			functions := map[string]money.Money{}
			if b, ok := blockOf(pl, k[1]); ok {
				for _, g := range b.Groups {
					for _, l := range g.Lines {
						f, amount := l.Account.Function, l.Amounts[0].Neg()
						if f == "" || f == "COGS" {
							f = "Other"
						}
						if x, ok := functions[f]; ok {
							functions[f], _ = x.Add(amount)
						} else {
							functions[f] = amount
						}
					}
				}
			}
			fs := map[string]any{}
			for f, x := range functions {
				fs[f] = x.String()
			}
			opex["functions"] = fs
			out[k[0]] = opex
		default:
			out[k[0]] = total(pl, k[1])
		}
		if b, ok := blockOf(pl, k[1]); ok && len(b.Percents) > 0 && b.Percents[0] != "" {
			x := b.Percents[0]
			if costs[k[1]] {
				x = negated(x)
			}
			pct[k[0]] = x
		}
	}
	if len(pct) > 0 {
		out["percent_of_revenue"] = pct
	}
	return out
}

// negated is a decimal's text negated: "-" added or taken off, and zero left as it is.
func negated(x string) string {
	switch {
	case strings.HasPrefix(x, "-"):
		return x[1:]
	case strings.Trim(x, "0.") == "":
		return x
	}
	return "-" + x
}

func balanceSheet(bs financials.Statement, accounts []storage.Account) map[string]any {
	out := map[string]any{}
	for _, k := range [][2]string{{"current_assets", financials.KeyCurrentAssets}, {"non_current_assets", financials.KeyNonCurrentAssets},
		{"current_liabilities", financials.KeyCurrentLiabilities}, {"borrowings", financials.KeyBorrowings}, {"equity", financials.KeyEquity}} {
		out[k[0]] = section(bs, k[1])
	}
	for _, k := range [][2]string{{"total_assets", financials.KeyTotalAssets}, {"total_liabilities", financials.KeyTotalLiabilities},
		{"total_liabilities_and_equity", financials.KeyLiabilitiesEquity}} {
		out[k[0]] = total(bs, k[1])
	}
	// The profit to date sits inside the retained-earnings account's group (README, "Statements").
	if b, ok := blockOf(bs, financials.KeyEquity); ok {
		for _, g := range b.Groups {
			if g.Name != financials.ProfitForTheYear || len(g.Totals) == 0 {
				continue
			}
			name := financials.ProfitForTheYear
			for _, a := range accounts {
				if a.Role == storage.RoleRetainedEarnings {
					name = groupName(a)
				}
			}
			groups := out["equity"].(map[string]any)["groups"].(map[string]any)
			sum := g.Totals[0]
			if was, ok := groups[name].(string); ok {
				prev, _ := money.ParseStored(was, sum.Currency())
				sum, _ = sum.Add(prev)
			}
			groups[name] = sum.String()
		}
	}
	return out
}

// workingLines are the cash flow's working-capital lines, by the row each is shown on.
var workingLines = []string{"Receivables", "Other current assets", "Payables & accruals", "Payroll liabilities", "Tax liabilities", "Deferred revenue"}

// row is a block's row named name, "0" when it is not shown.
func row(s financials.Statement, key, name string) string {
	if b, ok := blockOf(s, key); ok {
		for _, g := range b.Groups {
			if g.Name == name && len(g.Totals) > 0 {
				return g.Totals[0].String()
			}
		}
	}
	return "0"
}

func cashFlow(cf financials.Statement, byEntry map[uuid.UUID]booked) map[string]any {
	working := map[string]any{}
	for _, line := range workingLines {
		if x := row(cf, financials.KeyOperating, "Change in "+strings.ToLower(line)); x != "0" {
			working[line] = x
		}
	}
	nonCash := []any{}
	for _, x := range cf.NonCash {
		b := byEntry[x.Entry]
		item := map[string]any{"document": b.doc, "kind": b.kind, "posting_date": x.Date.Format(time.DateOnly), "section": x.Section}
		if x.Label != "" {
			item["label"] = x.Label
		}
		nonCash = append(nonCash, item)
	}
	return map[string]any{
		"operating": map[string]any{
			"net_profit":                      row(cf, financials.KeyOperating, "Net profit"),
			"depreciation_and_amortisation":   row(cf, financials.KeyOperating, "Depreciation and amortisation"),
			"investing_and_financing_results": row(cf, financials.KeyOperating, "Profit and loss of investing and financing"),
			"working_capital":                 working,
			"other_accounts":                  row(cf, financials.KeyOperating, "Other accounts"),
			"total":                           total(cf, financials.KeyOperating),
		},
		"investing":    total(cf, financials.KeyInvesting),
		"financing":    total(cf, financials.KeyFinancing),
		"net_change":   total(cf, financials.KeyNetChange),
		"opening_cash": total(cf, financials.KeyCashAtStart),
		"closing_cash": total(cf, financials.KeyCashAtEnd),
		"non_cash":     nonCash,
	}
}

func equityMovement(bs financials.Statement) map[string]any {
	return map[string]any{
		"opening":                row(bs, financials.KeyEquityMovement, "Equity at the start"),
		"profit":                 row(bs, financials.KeyEquityMovement, "Profit for the period"),
		"share_capital":          row(bs, financials.KeyEquityMovement, "Shares issued for cash"),
		"share_capital_non_cash": row(bs, financials.KeyEquityMovement, "Shares issued for non-cash consideration"),
		"other":                  row(bs, financials.KeyEquityMovement, "Other movements"),
		"closing":                total(bs, financials.KeyEquityMovement),
	}
}

// checkOf is a statement's check with key: whether it passed, and the accounts its detail names ("code name, ...").
func checkOf(s financials.Statement, key string) (bool, []string) {
	for _, c := range s.Checks {
		if c.Key != key {
			continue
		}
		var codes []string
		if !c.OK {
			names, _, _ := strings.Cut(c.Detail, ": ")
			for _, n := range strings.Split(names, ", ") {
				code, _, _ := strings.Cut(strings.TrimSpace(n), " ")
				codes = append(codes, code)
			}
		}
		return c.OK, codes
	}
	return true, nil
}

func checks(bs, cf financials.Statement) map[string]any {
	balances, _ := checkOf(bs, "balances")
	equity, _ := checkOf(bs, "equity")
	ties, _ := checkOf(cf, "ties")
	_, unplacedBS := checkOf(bs, "placed")
	_, unplacedCF := checkOf(cf, "placed")
	unplaced := append(unplacedBS, unplacedCF...)
	slices.Sort(unplaced)
	unplaced = slices.Compact(unplaced)
	list := make([]any, 0, len(unplaced))
	for _, c := range unplaced {
		list = append(list, c)
	}
	return map[string]any{"balances": balances, "ties": ties, "equity": equity, "unplaced": list}
}

// kpiKeys are the result's KPIs, by the product's keys.
var kpiKeys = map[string]string{"booked_mrr": "booked_mrr", "arr": "arr", "revenue_growth": "revenue_growth", "gross_margin": "gross_margin",
	"net_burn": "net_burn", "runway": "runway", "arpa": "arpa", "churn": "churn", "cac": "cac", "ltv": "ltv", "ltv_cac": "ltv_to_cac",
	"cac_payback": "cac_payback", "burn_multiple": "burn_multiple", "current_ratio": "current_ratio", "quick_ratio": "quick_ratio",
	"working_capital": "working_capital", "debt_equity": "debt_to_equity", "dso": "dso", "dpo": "dpo", "ebitda_margin": "ebitda_margin",
	"net_margin": "net_margin", "headcount": "headcount", "revenue_per_employee": "revenue_per_employee"}

// kpiValues are a month's KPIs in the result's shape: a figure, or null when undefined (FR-KPI-03).
func kpiValues(values []kpi.Value) map[string]any {
	out := map[string]any{"contracted_mrr": nil}
	spend := map[string]any{}
	for _, v := range values {
		var x any
		switch {
		case v.Figure != "":
			x = v.Figure
		case v.Key == "runway" && v.Words != "":
			x = "not-burning"
		}
		switch v.Key {
		case "spend_rd":
			spend["R&D"] = x
		case "spend_sm":
			spend["S&M"] = x
		case "spend_ga":
			spend["G&A"] = x
		default:
			if k, ok := kpiKeys[v.Key]; ok {
				out[k] = x
			}
		}
	}
	out["spend_by_function"] = spend
	return out
}
