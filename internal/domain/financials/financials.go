// Package financials lays out the financial statements from the balance service (FR-RPT-01, ENG-04): the P&L, the
// balance sheet and the trial balance, for a run of whole periods, with comparatives or a column per month
// (FR-RPT-02), read as at one ledger revision (FR-RPT-08). The layout comes from each account's chart attributes
// (PRD-001 rev 0.159, provisional) [accountant to confirm]; p1-reports design D1 to D4. A customer's statement of
// account is package statement.
package financials

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Kind is a statement.
type Kind string

// The statements this package lays out; the cash flow is in flows.go.
const (
	ProfitAndLoss Kind = "pl"
	BalanceSheet  Kind = "bs"
	TrialBalance  Kind = "tb"
)

// Compare is the comparative column a statement adds (FR-RPT-02). Budget and forecast arrive with p1-planning.
type Compare string

// The comparatives.
const (
	NoComparison Compare = ""
	PriorPeriod  Compare = "prior_period" // the run of the same length just before
	PriorYear    Compare = "prior_year"   // the same months a year earlier
)

// Request is what a statement is laid out for.
type Request struct {
	Kind        Kind
	All         []storage.Period // every period, earliest first
	First, Last int              // the run: indexes into All, inclusive
	Compare     Compare
	ByMonth     bool  // a column per period of the run and, for the P&L, their total
	Revision    int64 // the ledger revision every figure is read as at
}

// Column is one column of figures: a run of whole periods. A P&L column holds the run's movements; a balance sheet's
// and a trial balance's hold the positions at the run's end.
type Column struct {
	Key     string // "current", "prior_period", "prior_year", a month such as "2026-10", or "total"
	From    time.Time
	To      time.Time
	Periods []storage.Period
}

// Line is one account's figures, one amount per column, signed as the statement shows them.
type Line struct {
	Account  storage.Account
	Amounts  []money.Money
	Percents []string // the P&L's % of revenue (RND-PERCENT); "" where the column's revenue is zero
}

// Group is a report group's accounts and their total. A group without lines is a computed line, such as the balance
// sheet's profit for the year.
type Group struct {
	Name     string
	Lines    []Line
	Totals   []money.Money
	Percents []string
}

// Block is a section of accounts by group, or a measure computed from the sections above it.
type Block struct {
	Key      string
	Title    string
	Measure  bool
	Groups   []Group
	Totals   []money.Money
	Percents []string
}

// Check is one of a statement's checks (INV-10): OK, or failed with what is wrong and how to fix it.
type Check struct {
	Key    string
	OK     bool
	Title  string
	Detail string
}

// Statement is a laid-out statement: its columns, its blocks in order and its checks, read as at Revision. A cash flow
// lists its run's non-cash transactions apart.
type Statement struct {
	Kind     Kind
	Columns  []Column
	Blocks   []Block
	Checks   []Check
	NonCash  []NonCash
	Revision int64
}

// The cash-flow lines of the standard chart (FR-COA-02) that place a balance-sheet account (design D2).
var (
	currentAssets      = []string{"Cash & cash equivalents", "Receivables", "Other current assets"}
	nonCurrentAssets   = []string{"Capital expenditure", "Depreciation & amortisation"}
	currentLiabilities = []string{"Payables & accruals", "Payroll liabilities", "Tax liabilities", "Deferred revenue"}
	borrowingLines     = []string{"Borrowings"}
	equityLines        = []string{"Share capital", "Dividends & other equity"}
)

// The blocks' keys, which the API returns.
const (
	KeyRevenue           = "revenue"
	KeyCostOfSales       = "cost_of_sales"
	KeyGrossProfit       = "gross_profit"
	KeyOperatingExpenses = "operating_expenses"
	KeyEBITDA            = "ebitda"
	KeyDepreciation      = "depreciation_amortisation"
	KeyOperatingProfit   = "operating_profit"
	KeyOtherIncome       = "other_income"
	KeyFinanceAndOther   = "finance_and_other"
	KeyProfitBeforeTax   = "profit_before_tax"
	KeyIncomeTax         = "income_tax"
	KeyNetProfit         = "net_profit"

	KeyCurrentAssets      = "current_assets"
	KeyNonCurrentAssets   = "non_current_assets"
	KeyTotalAssets        = "total_assets"
	KeyCurrentLiabilities = "current_liabilities"
	KeyBorrowings         = "borrowings"
	KeyTotalLiabilities   = "total_liabilities"
	KeyEquity             = "equity"
	KeyLiabilitiesEquity  = "liabilities_and_equity"
	KeyTotal              = "total"
)

// ProfitForTheYear is the balance sheet's computed line in equity: the current financial year's result to date, which
// the year end folds into retained earnings (FR-GL-10).
const ProfitForTheYear = "Profit for the year to date"

// plSection places a P&L account (design D2): other income by function Other; the income tax account; the report group
// of the depreciation account; cost of sales by function COGS; finance costs and other expenses by function Other;
// revenue and operating expenses otherwise.
func plSection(a storage.Account, depreciationGroup string) string {
	switch {
	case a.Type == storage.Revenue && a.Function == "Other":
		return KeyOtherIncome
	case a.Type == storage.Revenue:
		return KeyRevenue
	case a.Role == "income-tax-expense":
		return KeyIncomeTax
	case depreciationGroup != "" && a.ReportGroup == depreciationGroup:
		return KeyDepreciation
	case a.Function == "COGS":
		return KeyCostOfSales
	case a.Function == "Other":
		return KeyFinanceAndOther
	}
	return KeyOperatingExpenses
}

// bsSection places a balance-sheet account by its type and cash-flow line (design D2); "" when it fits no section,
// which fails the statement's check instead of leaving the account out quietly.
func bsSection(a storage.Account) string {
	line := strings.TrimSpace(a.CashFlowLine)
	switch {
	case a.Type == storage.Equity && (a.Role == storage.RoleRetainedEarnings || slices.Contains(equityLines, line)):
		return KeyEquity
	case a.Type == storage.Asset && slices.Contains(currentAssets, line):
		return KeyCurrentAssets
	case a.Type == storage.Asset && slices.Contains(nonCurrentAssets, line):
		return KeyNonCurrentAssets
	case a.Type == storage.Liability && slices.Contains(currentLiabilities, line):
		return KeyCurrentLiabilities
	case a.Type == storage.Liability && slices.Contains(borrowingLines, line):
		return KeyBorrowings
	}
	return ""
}

// profitAndLoss says whether an account's balance belongs to the P&L.
func profitAndLoss(a storage.Account) bool {
	return a.Type == storage.Revenue || a.Type == storage.Expense
}

// shown is an account's base amount as a statement shows it: income, liabilities and equity as positive credits,
// expenses and assets as positive debits. The trial balance keeps the ledger's sign, debits positive.
func shown(k Kind, a storage.Account, m money.Money) money.Money {
	if k == TrialBalance {
		return m
	}
	switch a.Type {
	case storage.Revenue, storage.Liability, storage.Equity:
		return m.Neg()
	}
	return m
}

// Columns are a request's columns: the run and its comparative, or a column per month and, for the P&L, their total.
// A comparative whose periods are not all in the books is left out.
func Columns(r Request) ([]Column, error) {
	if r.First < 0 || r.Last >= len(r.All) || r.First > r.Last {
		return nil, fmt.Errorf("financials: the run %d..%d is outside the %d periods", r.First, r.Last, len(r.All))
	}
	run := func(key string, ps []storage.Period) Column {
		return Column{Key: key, From: ps[0].StartsOn, To: ps[len(ps)-1].EndsOn, Periods: ps}
	}
	current := r.All[r.First : r.Last+1]
	if r.ByMonth {
		var out []Column
		for _, p := range current {
			out = append(out, run(p.StartsOn.Format("2006-01"), []storage.Period{p}))
		}
		if (r.Kind == ProfitAndLoss || r.Kind == CashFlow) && len(current) > 1 {
			out = append(out, run(KeyTotal, current))
		}
		return out, nil
	}
	out := []Column{run("current", current)}
	switch r.Compare {
	case PriorPeriod:
		if n := len(current); r.First-n >= 0 {
			out = append(out, run(string(PriorPeriod), r.All[r.First-n:r.First]))
		}
	case PriorYear:
		var earlier []storage.Period
		for _, p := range current {
			for _, q := range r.All {
				if q.StartsOn.Equal(p.StartsOn.AddDate(-1, 0, 0)) && q.EndsOn.Equal(p.EndsOn.AddDate(-1, 0, 0)) {
					earlier = append(earlier, q)
				}
			}
		}
		if len(earlier) == len(current) {
			out = append(out, run(string(PriorYear), earlier))
		}
	}
	return out, nil
}

// figures are each account's base amount in each column, and each column's total, read as at the request's revision.
func figures(ctx context.Context, u storage.UnitOfWork, base money.Currency, r Request, cols []Column) ([]map[uuid.UUID]money.Money, []money.Money, error) {
	at := balance.At(r.Revision)
	out := make([]map[uuid.UUID]money.Money, len(cols))
	totals := make([]money.Money, len(cols))
	for i, c := range cols {
		var tb balance.TrialBalance
		var err error
		if r.Kind == ProfitAndLoss {
			ids := make([]uuid.UUID, len(c.Periods))
			for j, p := range c.Periods {
				ids[j] = p.ID
			}
			tb, err = balance.TrialAt(ctx, u, base, at, ids...)
		} else {
			tb, err = balance.PositionAt(ctx, u, base, at, r.All, c.Periods[len(c.Periods)-1])
		}
		if err != nil {
			return nil, nil, err
		}
		out[i] = map[uuid.UUID]money.Money{}
		for _, row := range tb.Rows {
			out[i][row.AccountID] = row.Base
		}
		totals[i] = tb.Total
	}
	return out, totals, nil
}

// Lay reads a statement as at the request's revision and lays it out.
func Lay(ctx context.Context, u storage.UnitOfWork, base money.Currency, r Request) (Statement, error) {
	cols, err := Columns(r)
	if err != nil {
		return Statement{}, err
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return Statement{}, err
	}
	s := Statement{Kind: r.Kind, Columns: cols, Revision: r.Revision}
	if r.Kind == CashFlow {
		fs, err := flows(ctx, u, base, r, cols, accounts)
		if err != nil {
			return Statement{}, err
		}
		s.Blocks, s.Checks, s.NonCash, err = cashFlow(fs, len(cols), base)
		return s, err
	}
	figs, totals, err := figures(ctx, u, base, r, cols)
	if err != nil {
		return Statement{}, err
	}
	l := layout{kind: r.Kind, base: base, n: len(cols), figs: figs, accounts: accounts}
	switch r.Kind {
	case ProfitAndLoss:
		s.Blocks, err = l.profitAndLoss()
	case BalanceSheet:
		s.Blocks, err = l.balanceSheet()
	case TrialBalance:
		s.Blocks, err = l.trialBalance()
	default:
		err = fmt.Errorf("financials: %q is not a statement", r.Kind)
	}
	if err != nil {
		return Statement{}, err
	}
	s.Checks = l.checks(totals)
	if r.Kind == BalanceSheet {
		// The movement in equity over each column's run, and that it reconciles (INV-11, BS_EqChk).
		fs, err := flows(ctx, u, base, r, cols, accounts)
		if err != nil {
			return Statement{}, err
		}
		b, c, err := equityMovement(fs, len(cols), base)
		if err != nil {
			return Statement{}, err
		}
		s.Blocks = append(s.Blocks, b)
		s.Checks = append(s.Checks, c)
	}
	return s, nil
}

// layout is what laying out one statement reads: the figures by column and the chart.
type layout struct {
	kind     Kind
	base     money.Currency
	n        int
	figs     []map[uuid.UUID]money.Money
	accounts []storage.Account
}

func (l layout) zeros() []money.Money {
	z := make([]money.Money, l.n)
	for i := range z {
		z[i] = money.Zero(l.base)
	}
	return z
}

// lines are the accounts with figures that place in a section, by the section's key, in code order. Group headings
// never carry figures.
func (l layout) lines(place func(storage.Account) string) map[string][]Line {
	sorted := slices.Clone(l.accounts)
	slices.SortFunc(sorted, func(a, b storage.Account) int { return strings.Compare(a.Code, b.Code) })
	out := map[string][]Line{}
	for _, a := range sorted {
		key := place(a)
		if a.Group || key == "" {
			continue
		}
		line, any := Line{Account: a, Amounts: l.zeros()}, false
		for i := range l.figs {
			if m, ok := l.figs[i][a.ID]; ok {
				line.Amounts[i] = shown(l.kind, a, m)
				any = any || !m.IsZero()
			}
		}
		if any {
			out[key] = append(out[key], line)
		}
	}
	return out
}

// section groups a section's lines by report group, in the order the groups first appear, with each group's and the
// section's totals.
func (l layout) section(key, title string, lines []Line) (Block, error) {
	b := Block{Key: key, Title: title, Totals: l.zeros()}
	var order []string
	byName := map[string]*Group{}
	for _, line := range lines {
		name := strings.TrimSpace(line.Account.ReportGroup)
		if name == "" {
			name = title
		}
		g := byName[name]
		if g == nil {
			g = &Group{Name: name, Totals: l.zeros()}
			byName[name] = g
			order = append(order, name)
		}
		g.Lines = append(g.Lines, line)
		if err := addTo(g.Totals, line.Amounts); err != nil {
			return Block{}, err
		}
		if err := addTo(b.Totals, line.Amounts); err != nil {
			return Block{}, err
		}
	}
	for _, name := range order {
		b.Groups = append(b.Groups, *byName[name])
	}
	return b, nil
}

// sections lays out the named sections from the placed lines.
func (l layout) sections(lines map[string][]Line, names ...[2]string) (map[string]Block, error) {
	out := map[string]Block{}
	for _, x := range names {
		b, err := l.section(x[0], x[1], lines[x[0]])
		if err != nil {
			return nil, err
		}
		out[x[0]] = b
	}
	return out, nil
}

// addTo adds b into a, column by column.
func addTo(a, b []money.Money) error {
	for i := range a {
		var err error
		if a[i], err = a[i].Add(b[i]); err != nil {
			return err
		}
	}
	return nil
}

// term is amounts to add (sign 1) or take off (sign -1).
type term struct {
	sign    int
	amounts []money.Money
}

func plus(a []money.Money) term  { return term{1, a} }
func minus(a []money.Money) term { return term{-1, a} }

// combine is first with each term added or taken off, column by column.
func combine(first []money.Money, rest ...term) ([]money.Money, error) {
	out := slices.Clone(first)
	for _, t := range rest {
		for i := range out {
			var err error
			if t.sign < 0 {
				out[i], err = out[i].Sub(t.amounts[i])
			} else {
				out[i], err = out[i].Add(t.amounts[i])
			}
			if err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func measure(key, title string, totals []money.Money) Block {
	return Block{Key: key, Title: title, Measure: true, Totals: totals}
}

// profitAndLoss is the P&L (design D2): revenue, cost of sales, gross profit, operating expenses, EBITDA, depreciation
// and amortisation, operating profit, other income, finance costs and other expenses, profit before tax, income tax and
// net profit, each with its % of revenue.
func (l layout) profitAndLoss() ([]Block, error) {
	depreciationGroup := ""
	for _, a := range l.accounts {
		if a.Role == "depreciation" {
			depreciationGroup = a.ReportGroup
		}
	}
	s, err := l.sections(l.lines(func(a storage.Account) string {
		if !profitAndLoss(a) {
			return ""
		}
		return plSection(a, depreciationGroup)
	}), [2]string{KeyRevenue, "Revenue"}, [2]string{KeyCostOfSales, "Cost of sales"}, [2]string{KeyOperatingExpenses, "Operating expenses"},
		[2]string{KeyDepreciation, "Depreciation and amortisation"}, [2]string{KeyOtherIncome, "Other income"},
		[2]string{KeyFinanceAndOther, "Finance costs and other expenses"}, [2]string{KeyIncomeTax, "Income tax"})
	if err != nil {
		return nil, err
	}
	gp, err := combine(s[KeyRevenue].Totals, minus(s[KeyCostOfSales].Totals))
	if err != nil {
		return nil, err
	}
	ebitda, err := combine(gp, minus(s[KeyOperatingExpenses].Totals))
	if err != nil {
		return nil, err
	}
	op, err := combine(ebitda, minus(s[KeyDepreciation].Totals))
	if err != nil {
		return nil, err
	}
	pbt, err := combine(op, plus(s[KeyOtherIncome].Totals), minus(s[KeyFinanceAndOther].Totals))
	if err != nil {
		return nil, err
	}
	net, err := combine(pbt, minus(s[KeyIncomeTax].Totals))
	if err != nil {
		return nil, err
	}
	blocks := []Block{
		s[KeyRevenue], s[KeyCostOfSales], measure(KeyGrossProfit, "Gross profit", gp),
		s[KeyOperatingExpenses], measure(KeyEBITDA, "EBITDA", ebitda),
		s[KeyDepreciation], measure(KeyOperatingProfit, "Operating profit", op),
		s[KeyOtherIncome], s[KeyFinanceAndOther], measure(KeyProfitBeforeTax, "Profit before tax", pbt),
		s[KeyIncomeTax], measure(KeyNetProfit, "Net profit", net),
	}
	return blocks, percents(blocks, s[KeyRevenue].Totals)
}

// percents gives every P&L figure its % of its column's revenue (RND-PERCENT); none where the revenue is zero.
func percents(blocks []Block, revenue []money.Money) error {
	of := func(amounts []money.Money) ([]string, error) {
		out := make([]string, len(amounts))
		for i, m := range amounts {
			if revenue[i].IsZero() {
				continue
			}
			var err error
			if out[i], err = rounding.Percent(m, revenue[i]); err != nil {
				return nil, err
			}
		}
		return out, nil
	}
	var err error
	for i := range blocks {
		b := &blocks[i]
		if b.Percents, err = of(b.Totals); err != nil {
			return err
		}
		for j := range b.Groups {
			g := &b.Groups[j]
			if g.Percents, err = of(g.Totals); err != nil {
				return err
			}
			for k := range g.Lines {
				if g.Lines[k].Percents, err = of(g.Lines[k].Amounts); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// balanceSheet is the balance sheet at each column's end (design D2): current and non-current assets, current
// liabilities, borrowings apart, and equity with the year's profit to date.
func (l layout) balanceSheet() ([]Block, error) {
	s, err := l.sections(l.lines(func(a storage.Account) string {
		if profitAndLoss(a) {
			return ""
		}
		return bsSection(a)
	}), [2]string{KeyCurrentAssets, "Current assets"}, [2]string{KeyNonCurrentAssets, "Non-current assets"},
		[2]string{KeyCurrentLiabilities, "Current liabilities"}, [2]string{KeyBorrowings, "Borrowings"}, [2]string{KeyEquity, "Equity"})
	if err != nil {
		return nil, err
	}
	// The year's result to date sits in equity until the year end folds it into retained earnings (FR-GL-10).
	profit := l.zeros()
	for _, a := range l.accounts {
		if !profitAndLoss(a) {
			continue
		}
		for i := range l.figs {
			if m, ok := l.figs[i][a.ID]; ok {
				if profit[i], err = profit[i].Sub(m); err != nil {
					return nil, err
				}
			}
		}
	}
	eq := s[KeyEquity]
	eq.Groups = append(eq.Groups, Group{Name: ProfitForTheYear, Totals: profit})
	if err := addTo(eq.Totals, profit); err != nil {
		return nil, err
	}
	assets, err := combine(s[KeyCurrentAssets].Totals, plus(s[KeyNonCurrentAssets].Totals))
	if err != nil {
		return nil, err
	}
	liabilities, err := combine(s[KeyCurrentLiabilities].Totals, plus(s[KeyBorrowings].Totals))
	if err != nil {
		return nil, err
	}
	both, err := combine(liabilities, plus(eq.Totals))
	if err != nil {
		return nil, err
	}
	return []Block{
		s[KeyCurrentAssets], s[KeyNonCurrentAssets], measure(KeyTotalAssets, "Total assets", assets),
		s[KeyCurrentLiabilities], s[KeyBorrowings], measure(KeyTotalLiabilities, "Total liabilities", liabilities),
		eq, measure(KeyLiabilitiesEquity, "Total liabilities and equity", both),
	}, nil
}

// trialBalance is every account's balance at each column's end by type, debits positive, and the total, which nets to
// zero (INV-09).
func (l layout) trialBalance() ([]Block, error) {
	types := []storage.AccountType{storage.Asset, storage.Liability, storage.Equity, storage.Revenue, storage.Expense}
	titles := []string{"Assets", "Liabilities", "Equity", "Revenue", "Expenses"}
	lines := l.lines(func(a storage.Account) string { return string(a.Type) })
	var blocks []Block
	total := l.zeros()
	for i, t := range types {
		b, err := l.section(string(t), titles[i], lines[string(t)])
		if err != nil {
			return nil, err
		}
		if err := addTo(total, b.Totals); err != nil {
			return nil, err
		}
		blocks = append(blocks, b)
	}
	return append(blocks, measure(KeyTotal, "Total", total)), nil
}

// checks are a statement's checks (INV-10): the books balance in every column (BS_Chk, and INV-09 for the trial
// balance), and every balance-sheet account with figures sits in a section (BS_Uncl). Every P&L account always does
// (PL_Chk).
func (l layout) checks(totals []money.Money) []Check {
	var out []Check
	if l.kind == BalanceSheet || l.kind == TrialBalance {
		var off []string
		for _, t := range totals {
			if !t.IsZero() {
				off = append(off, fmt.Sprintf("%s %s", t, l.base))
			}
		}
		if len(off) > 0 {
			out = append(out, Check{Key: "balances", Title: "The books do not balance",
				Detail: "Debits and credits differ by " + strings.Join(off, ", ") + ": the ledger's own checks should have refused this; report it"})
		} else {
			out = append(out, Check{Key: "balances", OK: true, Title: "Debits equal credits"})
		}
	}
	if l.kind == BalanceSheet {
		var loose []string
		for _, a := range l.accounts {
			if a.Group || profitAndLoss(a) || bsSection(a) != "" {
				continue
			}
			for i := range l.figs {
				if m, ok := l.figs[i][a.ID]; ok && !m.IsZero() {
					loose = append(loose, a.Code+" "+a.Name)
					break
				}
			}
		}
		slices.Sort(loose)
		if len(loose) > 0 {
			out = append(out, Check{Key: "placed", Title: "Accounts sit in no section of the balance sheet",
				Detail: strings.Join(loose, ", ") + ": give each a cash-flow line of the standard chart in the chart of accounts"})
		} else {
			out = append(out, Check{Key: "placed", OK: true, Title: "Every account sits in a section"})
		}
	}
	return out
}
