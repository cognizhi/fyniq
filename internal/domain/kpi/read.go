package kpi

import (
	"context"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/financials"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The balance sheet's lines the KPIs read, by each account's cash-flow line (the chart's vocabulary).
const (
	cashLine        = "Cash & cash equivalents"
	receivablesLine = "Receivables"
	payablesLine    = "Payables & accruals"
)

// Subscription says whether an account earns subscription revenue: a revenue account whose report group names a
// subscription stream (design D1; [accountant to confirm]).
func Subscription(a storage.Account) bool {
	return a.Type == storage.Revenue && strings.Contains(strings.ToLower(a.ReportGroup), "subscription")
}

// Read gathers the figures the KPIs of the month at index last of all need, all read at revision (design D2): the last
// three months of the financial year that starts on yearStart (fewer in its first two months), earliest first, and the
// month before the last, from whichever year, nil when the books have none. Each month carries its entered counts.
func Read(ctx context.Context, u storage.UnitOfWork, base money.Currency, all []storage.Period, last int, yearStart time.Time, revision int64) ([]Month, *Month, error) {
	first := last
	for first > 0 && last-first < 2 && !all[first-1].StartsOn.Before(yearStart) {
		first--
	}
	from := first
	if last > 0 && last-1 < from {
		from = last - 1
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return nil, nil, err
	}
	subscriptions := false
	for _, a := range accounts {
		subscriptions = subscriptions || (Subscription(a) && !a.Group)
	}
	lay := func(k financials.Kind) (financials.Statement, error) {
		return financials.Lay(ctx, u, base, financials.Request{Kind: k, All: all, First: from, Last: last, ByMonth: true, Revision: revision})
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
	inputs, err := u.KPIInputs().Range(ctx, all[from].StartsOn, all[last].StartsOn)
	if err != nil {
		return nil, nil, err
	}
	entered := map[string]storage.KPIInput{}
	for _, x := range inputs {
		entered[x.Month.Format("2006-01")] = x
	}
	zero := money.Zero(base)
	month := func(i int) Month {
		p := all[i]
		key := p.StartsOn.Format("2006-01")
		m := Month{Start: p.StartsOn, Revenue: zero, GrossProfit: zero, EBITDA: zero, NetProfit: zero, NetProfitYTD: zero, SM: zero, RD: zero, GA: zero, Costs: zero,
			NetBurn: zero, Cash: zero, Receivables: zero, Payables: zero, CurrentAssets: zero, CurrentLiabilities: zero, TotalLiabilities: zero, Equity: zero}
		if subscriptions {
			m.Subscription = &zero
		}
		col := func(s financials.Statement) int {
			for c, x := range s.Columns {
				if x.Key == key {
					return c
				}
			}
			return -1
		}
		plus := func(into *money.Money, x money.Money) {
			if v, err := into.Add(x); err == nil {
				*into = v
			}
		}
		if c := col(pl); c >= 0 {
			for _, b := range pl.Blocks {
				t := b.Totals[c]
				switch b.Key {
				case "revenue":
					m.Revenue = t
				case "gross_profit":
					m.GrossProfit = t
				case "ebitda":
					m.EBITDA = t
				case "net_profit":
					m.NetProfit = t
				case "cost_of_sales", "operating_expenses":
					plus(&m.Costs, t)
				}
				for _, g := range b.Groups {
					for _, l := range g.Lines {
						switch {
						case b.Key == "revenue" && Subscription(l.Account) && m.Subscription != nil:
							s := *m.Subscription
							plus(&s, l.Amounts[c])
							m.Subscription = &s
						case l.Account.Type != storage.Expense:
						case l.Account.Function == "S&M":
							plus(&m.SM, l.Amounts[c])
						case l.Account.Function == "R&D":
							plus(&m.RD, l.Amounts[c])
						case l.Account.Function == "G&A":
							plus(&m.GA, l.Amounts[c])
						}
					}
				}
			}
		}
		if c := col(bs); c >= 0 {
			for _, b := range bs.Blocks {
				switch b.Key {
				case "current_assets":
					m.CurrentAssets = b.Totals[c]
				case "current_liabilities":
					m.CurrentLiabilities = b.Totals[c]
				case "total_liabilities":
					m.TotalLiabilities = b.Totals[c]
				case "equity":
					m.Equity = b.Totals[c]
				}
				for _, g := range b.Groups {
					for _, l := range g.Lines {
						switch strings.TrimSpace(l.Account.CashFlowLine) {
						case cashLine:
							plus(&m.Cash, l.Amounts[c])
						case receivablesLine:
							plus(&m.Receivables, l.Amounts[c])
						case payablesLine:
							plus(&m.Payables, l.Amounts[c])
						}
					}
				}
			}
		}
		if c := col(cf); c >= 0 {
			flow := zero
			for _, b := range cf.Blocks {
				if b.Key == "net_operating" || b.Key == "net_investing" {
					plus(&flow, b.Totals[c])
				}
			}
			m.NetBurn = flow.Neg()
		}
		if x, ok := entered[key]; ok {
			c := &Counts{Headcount: x.Counts.Headcount, EnteredBy: x.EnteredBy, EnteredAt: x.EnteredAt}
			if x.Counts.Paying != nil && x.Counts.New != nil && x.Counts.Lost != nil {
				c.Paying, c.New, c.Lost, c.HasCustomers = *x.Counts.Paying, *x.Counts.New, *x.Counts.Lost, true
			}
			m.Counts = c
		}
		return m
	}
	var months []Month
	for i := first; i <= last; i++ {
		months = append(months, month(i))
	}
	// The year to date: the P&L from the financial year's first month to the last, one column.
	ytd := last
	for ytd > 0 && !all[ytd-1].StartsOn.Before(yearStart) {
		ytd--
	}
	year, err := financials.Lay(ctx, u, base, financials.Request{Kind: financials.ProfitAndLoss, All: all, First: ytd, Last: last, Revision: revision})
	if err != nil {
		return nil, nil, err
	}
	months[len(months)-1].NetProfitYTD = zero
	for _, b := range year.Blocks {
		if b.Key == "net_profit" {
			months[len(months)-1].NetProfitYTD = b.Totals[0]
		}
	}
	var before *Month
	if last > 0 {
		b := month(last - 1)
		before = &b
	}
	return months, before, nil
}
