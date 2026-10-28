package financials

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The cash flow (FR-RPT-01, INV-11; PRD-001 rev 0.159, provisional) [accountant to confirm]. Each entry of a run is
// operating, investing (it moves a capital-expenditure account) or financing (it moves borrowings or equity), and its
// cash is what it put on the cash accounts. Investing and financing show their entries' cash; operating shows the
// indirect reconciliation, which equals its entries' cash exactly: net profit, the depreciation added back, the profit
// and loss of investing and financing entries taken out, and the change in each working-capital line. An investing or
// financing entry that moved no cash (a cost a director paid personally, a conversion to shares) is listed apart. The
// opening balances are where the books start, not a flow.

// CashFlow is the cash flow statement, indirect method.
const CashFlow Kind = "cf"

// The cash flow's and the movement in equity's keys.
const (
	KeyOperating      = "operating"
	KeyInvesting      = "investing"
	KeyFinancing      = "financing"
	KeyNetOperating   = "net_operating"
	KeyNetInvesting   = "net_investing"
	KeyNetFinancing   = "net_financing"
	KeyNetChange      = "net_change"
	KeyCashAtStart    = "cash_at_start"
	KeyCashAtEnd      = "cash_at_end"
	KeyEquityMovement = "equity_movement"
	cashLine          = "Cash & cash equivalents"
	depreciationLine  = "Depreciation & amortisation"
	capitalLine       = "Capital expenditure"
	shareCapitalLine  = "Share capital"
	otherEquityLine   = "Dividends & other equity"
	borrowingsLine    = "Borrowings"
)

// NonCash is an investing or financing transaction that moved no cash, listed beside the cash flow.
type NonCash struct {
	Date   time.Time
	Source storage.Source
	Memo   string
	Amount money.Money // its debits
}

// activity is what an entry is: operating, investing or financing.
type activity int

const (
	operating activity = iota
	investing
	financing
)

// role is what an account is to the cash flow.
type role int

const (
	other role = iota
	cash
	profitOrLoss
	depreciation
	workingCapital
	capital
	borrowingOrEquity
)

// cashRole places an account for the cash flow by its type and cash-flow line.
func cashRole(a storage.Account) role {
	line := strings.TrimSpace(a.CashFlowLine)
	switch {
	case profitAndLoss(a):
		return profitOrLoss
	case a.Type == storage.Asset && line == cashLine:
		return cash
	case a.Type == storage.Asset && line == depreciationLine:
		return depreciation
	case a.Type == storage.Asset && line == capitalLine:
		return capital
	case (a.Type == storage.Asset || a.Type == storage.Liability) && (slices.Contains(currentAssets, line) || slices.Contains(currentLiabilities, line)):
		return workingCapital
	case a.Type == storage.Liability && line == borrowingsLine, a.Type == storage.Equity && (a.Role == storage.RoleRetainedEarnings || slices.Contains(equityLines, line)):
		return borrowingOrEquity
	}
	return other
}

// flow is what one column's entries did, beside its opening and closing positions. Amounts are as the cash flow shows
// them: cash in and profit positive.
type flow struct {
	profit, depreciation, reclassified, other money.Money
	working                                   map[string]money.Money // by cash-flow line
	paidOut, paidIn                           money.Money            // investing
	financed                                  map[string]money.Money // by cash-flow line
	nonCash                                   []NonCash
	others                                    map[string]bool // the accounts behind other
	cashStart, cashEnd                        money.Money
	equityStart, equityEnd                    money.Money
	shares, otherEquity                       money.Money
}

func newFlow(base money.Currency) *flow {
	z := money.Zero(base)
	return &flow{profit: z, depreciation: z, reclassified: z, other: z, working: map[string]money.Money{}, paidOut: z, paidIn: z,
		financed: map[string]money.Money{}, others: map[string]bool{}, cashStart: z, cashEnd: z, equityStart: z, equityEnd: z, shares: z, otherEquity: z}
}

// accrue adds b into *a.
func accrue(a *money.Money, b money.Money) error {
	x, err := a.Add(b)
	*a = x
	return err
}

// addKey adds b into m[key].
func addKey(m map[string]money.Money, key string, b money.Money) error {
	x, ok := m[key]
	if !ok {
		x = money.Zero(b.Currency())
	}
	sum, err := x.Add(b)
	m[key] = sum
	return err
}

// flows reads each column's entries, as at the request's revision, and its opening and closing positions.
func flows(ctx context.Context, u storage.UnitOfWork, base money.Currency, r Request, cols []Column, accounts []storage.Account) ([]*flow, error) {
	byID := map[uuid.UUID]storage.Account{}
	for _, a := range accounts {
		byID[a.ID] = a
	}
	index := map[uuid.UUID]int{}
	for i, p := range r.All {
		index[p.ID] = i
	}
	inColumn := make([]map[uuid.UUID]bool, len(cols))
	for i, c := range cols {
		inColumn[i] = map[uuid.UUID]bool{}
		for _, p := range c.Periods {
			inColumn[i][p.ID] = true
		}
	}
	out := make([]*flow, len(cols))
	for i := range cols {
		out[i] = newFlow(base)
	}
	err := balance.EachEntry(ctx, u, r.Revision, func(e storage.Entry) error {
		for i := range cols {
			if !inColumn[i][e.PeriodID] {
				continue
			}
			if err := out[i].read(e, byID, base); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	at := balance.At(r.Revision)
	for i, c := range cols {
		first := index[c.Periods[0].ID]
		if first > 0 {
			start, err := balance.PositionAt(ctx, u, base, at, r.All, r.All[first-1])
			if err != nil {
				return nil, err
			}
			if err := out[i].position(start, byID, true); err != nil {
				return nil, err
			}
		}
		end, err := balance.PositionAt(ctx, u, base, at, r.All, c.Periods[len(c.Periods)-1])
		if err != nil {
			return nil, err
		}
		if err := out[i].position(end, byID, false); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// position adds a position's cash and equity to the flow's start or end.
func (f *flow) position(tb balance.TrialBalance, byID map[uuid.UUID]storage.Account, start bool) error {
	cashTo, equityTo := &f.cashEnd, &f.equityEnd
	if start {
		cashTo, equityTo = &f.cashStart, &f.equityStart
	}
	for _, row := range tb.Rows {
		a := byID[row.AccountID]
		switch {
		case cashRole(a) == cash:
			if err := accrue(cashTo, row.Base); err != nil {
				return err
			}
		case a.Type == storage.Equity || profitAndLoss(a):
			if err := accrue(equityTo, row.Base.Neg()); err != nil {
				return err
			}
		}
	}
	return nil
}

// read adds one entry of the column to the flow.
func (f *flow) read(e storage.Entry, byID map[uuid.UUID]storage.Account, base money.Currency) error {
	type line struct {
		a    storage.Account
		r    role
		base money.Money
	}
	lines := make([]line, 0, len(e.Lines))
	act, moved, cashOf := operating, false, money.Zero(base)
	for _, l := range e.Lines {
		b, err := money.ParseStored(string(l.BaseAmount), base)
		if err != nil {
			return err
		}
		a := byID[l.AccountID]
		x := line{a: a, r: cashRole(a), base: b}
		switch x.r {
		case capital:
			act = investing
		case borrowingOrEquity:
			if act == operating {
				act = financing
			}
		case cash:
			moved = true
			if err := accrue(&cashOf, b); err != nil {
				return err
			}
		}
		lines = append(lines, x)
	}
	if e.Source.Type == ledger.SourceOpeningBalance {
		// Where the books start: its cash and equity open the run.
		for _, x := range lines {
			switch {
			case x.r == cash:
				if err := accrue(&f.cashStart, x.base); err != nil {
					return err
				}
			case x.a.Type == storage.Equity || x.r == profitOrLoss:
				if err := accrue(&f.equityStart, x.base.Neg()); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, x := range lines {
		var err error
		switch {
		case x.r == profitOrLoss:
			if err = accrue(&f.profit, x.base.Neg()); err == nil && act != operating {
				err = accrue(&f.reclassified, x.base) // not operating cash: taken back out of profit
			}
		case act != operating || x.r == cash:
		case x.r == depreciation:
			err = accrue(&f.depreciation, x.base.Neg())
		case x.r == workingCapital:
			err = addKey(f.working, strings.TrimSpace(x.a.CashFlowLine), x.base.Neg())
		default:
			f.others[x.a.Code+" "+x.a.Name] = true
			err = accrue(&f.other, x.base.Neg())
		}
		if err != nil {
			return err
		}
		if x.a.Type == storage.Equity {
			to := &f.otherEquity
			if strings.TrimSpace(x.a.CashFlowLine) == shareCapitalLine {
				to = &f.shares
			}
			if err := accrue(to, x.base.Neg()); err != nil {
				return err
			}
		}
	}
	switch {
	case act == operating:
	case !moved:
		debits := money.Zero(base)
		for _, x := range lines {
			if x.base.Sign() > 0 {
				if err := accrue(&debits, x.base); err != nil {
					return err
				}
			}
		}
		f.nonCash = append(f.nonCash, NonCash{Date: e.PostingDate, Source: e.Source, Memo: e.Memo, Amount: debits})
	case act == investing && cashOf.Sign() < 0:
		return accrue(&f.paidOut, cashOf)
	case act == investing:
		return accrue(&f.paidIn, cashOf)
	default:
		for _, x := range lines {
			if x.r == borrowingOrEquity {
				line := strings.TrimSpace(x.a.CashFlowLine)
				if x.a.Role == storage.RoleRetainedEarnings {
					line = otherEquityLine
				}
				return addKey(f.financed, line, cashOf)
			}
		}
	}
	return nil
}

// row is a cash flow's computed line: one amount per column.
func row(name string, n int, base money.Currency, of func(*flow) money.Money, fs []*flow) Group {
	g := Group{Name: name, Totals: make([]money.Money, n)}
	for i := range fs {
		g.Totals[i] = of(fs[i])
	}
	return g
}

// nonZero says whether any of a row's amounts is not zero.
func nonZero(g Group) bool {
	return slices.ContainsFunc(g.Totals, func(m money.Money) bool { return !m.IsZero() })
}

// total is a block's total: its groups summed, column by column.
func total(groups []Group, n int, base money.Currency) ([]money.Money, error) {
	out := make([]money.Money, n)
	for i := range out {
		out[i] = money.Zero(base)
	}
	for _, g := range groups {
		if err := addTo(out, g.Totals); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// cashFlow lays out the cash flow from the columns' flows, and its checks (INV-11).
func cashFlow(fs []*flow, n int, base money.Currency) ([]Block, []Check, []NonCash, error) {
	operatingRows := []Group{row("Net profit", n, base, func(f *flow) money.Money { return f.profit }, fs)}
	for _, x := range []Group{
		row("Depreciation and amortisation", n, base, func(f *flow) money.Money { return f.depreciation }, fs),
		row("Profit and loss of investing and financing", n, base, func(f *flow) money.Money { return f.reclassified }, fs),
	} {
		if nonZero(x) {
			operatingRows = append(operatingRows, x)
		}
	}
	for _, line := range append(slices.Clone(currentAssets[1:]), currentLiabilities...) {
		x := row("Change in "+strings.ToLower(line), n, base, func(f *flow) money.Money {
			if m, ok := f.working[line]; ok {
				return m
			}
			return money.Zero(base)
		}, fs)
		if nonZero(x) {
			operatingRows = append(operatingRows, x)
		}
	}
	if x := row("Other accounts", n, base, func(f *flow) money.Money { return f.other }, fs); nonZero(x) {
		operatingRows = append(operatingRows, x)
	}
	var investingRows []Group
	for _, x := range []Group{
		row("Paid for property, plant and equipment", n, base, func(f *flow) money.Money { return f.paidOut }, fs),
		row("Received from disposals", n, base, func(f *flow) money.Money { return f.paidIn }, fs),
	} {
		if nonZero(x) {
			investingRows = append(investingRows, x)
		}
	}
	var financingRows []Group
	for _, x := range [][2]string{{borrowingsLine, "Borrowings"}, {shareCapitalLine, "Shares issued"}, {otherEquityLine, "Dividends and other equity"}} {
		g := row(x[1], n, base, func(f *flow) money.Money {
			if m, ok := f.financed[x[0]]; ok {
				return m
			}
			return money.Zero(base)
		}, fs)
		if nonZero(g) {
			financingRows = append(financingRows, g)
		}
	}
	op, err := total(operatingRows, n, base)
	if err != nil {
		return nil, nil, nil, err
	}
	inv, err := total(investingRows, n, base)
	if err != nil {
		return nil, nil, nil, err
	}
	fin, err := total(financingRows, n, base)
	if err != nil {
		return nil, nil, nil, err
	}
	change, err := combine(op, plus(inv), plus(fin))
	if err != nil {
		return nil, nil, nil, err
	}
	start := row("", n, base, func(f *flow) money.Money { return f.cashStart }, fs).Totals
	end := row("", n, base, func(f *flow) money.Money { return f.cashEnd }, fs).Totals
	blocks := []Block{
		{Key: KeyOperating, Title: "Operating activities", Groups: operatingRows, Totals: op},
		measure(KeyNetOperating, "Net cash from operating activities", op),
		{Key: KeyInvesting, Title: "Investing activities", Groups: investingRows, Totals: inv},
		measure(KeyNetInvesting, "Net cash from investing activities", inv),
		{Key: KeyFinancing, Title: "Financing activities", Groups: financingRows, Totals: fin},
		measure(KeyNetFinancing, "Net cash from financing activities", fin),
		measure(KeyNetChange, "Net change in cash", change),
		measure(KeyCashAtStart, "Cash at the start", start),
		measure(KeyCashAtEnd, "Cash at the end", end),
	}
	var checks []Check
	var off []string
	for i := range fs {
		want, err := start[i].Add(change[i])
		if err != nil {
			return nil, nil, nil, err
		}
		if !want.Equal(end[i]) {
			diff, err := end[i].Sub(want)
			if err != nil {
				return nil, nil, nil, err
			}
			off = append(off, fmt.Sprintf("%s %s", diff, base))
		}
	}
	if len(off) > 0 {
		checks = append(checks, Check{Key: "ties", Title: "The cash flow does not tie to the cash accounts",
			Detail: "The cash at the start and the net change differ from the cash at the end by " + strings.Join(off, ", ") + ": report it"})
	} else {
		checks = append(checks, Check{Key: "ties", OK: true, Title: "The cash flow ties to the cash accounts"})
	}
	var others []string
	for _, f := range fs {
		for name := range f.others {
			if !slices.Contains(others, name) {
				others = append(others, name)
			}
		}
	}
	slices.Sort(others)
	if len(others) > 0 {
		checks = append(checks, Check{Key: "placed", Title: "Accounts sit in no line of the cash flow",
			Detail: strings.Join(others, ", ") + ": give each a cash-flow line of the standard chart in the chart of accounts"})
	} else {
		checks = append(checks, Check{Key: "placed", OK: true, Title: "Every account sits in a line"})
	}
	var nonCash []NonCash
	if len(fs) > 0 {
		nonCash = fs[0].nonCash
	}
	return blocks, checks, nonCash, nil
}

// equityMovement is the balance sheet's movement in equity over each column's run, and its check (BS_EqChk): equity
// at the start, the profit, the shares issued and the other movements add up to equity at the end.
func equityMovement(fs []*flow, n int, base money.Currency) (Block, Check, error) {
	groups := []Group{
		row("Equity at the start", n, base, func(f *flow) money.Money { return f.equityStart }, fs),
		row("Profit for the period", n, base, func(f *flow) money.Money { return f.profit }, fs),
		row("Shares issued", n, base, func(f *flow) money.Money { return f.shares }, fs),
		row("Other movements", n, base, func(f *flow) money.Money { return f.otherEquity }, fs),
	}
	totals, err := total(groups, n, base)
	if err != nil {
		return Block{}, Check{}, err
	}
	var off []string
	for i, f := range fs {
		if !totals[i].Equal(f.equityEnd) {
			diff, err := f.equityEnd.Sub(totals[i])
			if err != nil {
				return Block{}, Check{}, err
			}
			off = append(off, fmt.Sprintf("%s %s", diff, base))
		}
	}
	c := Check{Key: "equity", OK: true, Title: "The movement in equity reconciles"}
	if len(off) > 0 {
		c = Check{Key: "equity", Title: "The movement in equity does not reconcile",
			Detail: "Equity at the end differs from its movement by " + strings.Join(off, ", ") + ": report it"}
	}
	return Block{Key: KeyEquityMovement, Title: "Movement in equity", Groups: groups, Totals: totals}, c, nil
}
