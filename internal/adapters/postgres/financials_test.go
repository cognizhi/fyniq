//go:build local

package postgres_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/closing"
	"github.com/cognizhi/fyniq/internal/domain/financials"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// financialBooks are porttest's books with a small chart laid out as the standard chart lays out its accounts, by
// code.
type financialBooks struct {
	porttest.Books
	acc map[string]storage.Account
}

func newFinancialBooks(t *testing.T, ctx context.Context, s storage.Store, name string) financialBooks {
	t.Helper()
	b := financialBooks{Books: porttest.NewBooks(t, s, name), acc: map[string]storage.Account{}}
	attrs := func(group, line, function string) storage.AccountAttributes {
		return storage.AccountAttributes{ReportGroup: group, CashFlowLine: line, Function: function}
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		for _, a := range []storage.NewAccount{
			{Code: "1010", Name: "DBS current", Type: storage.Asset, AccountAttributes: attrs("Cash & bank", "Cash & cash equivalents", "")},
			{Code: "1100", Name: "Accounts receivable", Type: storage.Asset, Role: storage.AccountRole(ledger.RoleAccountsReceivable), AccountAttributes: attrs("Receivables", "Receivables", "")},
			{Code: "1510", Name: "Computer equipment", Type: storage.Asset, AccountAttributes: attrs("Property, plant & equipment", "Capital expenditure", "")},
			{Code: "1590", Name: "Accumulated depreciation", Type: storage.Asset, Role: "accumulated-depreciation", AccountAttributes: attrs("Accumulated depreciation", "Depreciation & amortisation", "")},
			{Code: "1999", Name: "Unsorted", Type: storage.Asset, AccountAttributes: attrs("Other", "Elsewhere", "")},
			{Code: "2010", Name: "Accounts payable", Type: storage.Liability, Role: storage.AccountRole(ledger.RoleAccountsPayable), AccountAttributes: attrs("Payables & accruals", "Payables & accruals", "")},
			{Code: "2300", Name: "Income tax payable", Type: storage.Liability, Role: "income-tax-payable", AccountAttributes: attrs("Tax liabilities", "Tax liabilities", "")},
			{Code: "2500", Name: "Loan from director", Type: storage.Liability, AccountAttributes: attrs("Borrowings", "Borrowings", "")},
			{Code: "3010", Name: "Share capital", Type: storage.Equity, Role: storage.RoleShareCapital, AccountAttributes: attrs("Share capital", "Share capital", "")},
			{Code: "3100", Name: "Retained earnings", Type: storage.Equity, Role: storage.RoleRetainedEarnings, AccountAttributes: attrs("Retained earnings", "Dividends & other equity", "")},
			{Code: "4010", Name: "Subscription revenue", Type: storage.Revenue, AccountAttributes: attrs("Subscription revenue", "", "")},
			{Code: "5010", Name: "Hosting", Type: storage.Expense, AccountAttributes: attrs("Hosting & infrastructure", "", "COGS")},
			{Code: "6110", Name: "Advertising", Type: storage.Expense, AccountAttributes: attrs("Sales & marketing", "", "S&M")},
			{Code: "6610", Name: "Depreciation", Type: storage.Expense, Role: "depreciation", AccountAttributes: attrs("Depreciation & amortisation", "", "G&A")},
			{Code: "7020", Name: "Interest income", Type: storage.Revenue, AccountAttributes: attrs("Interest & other income", "", "Other")},
			{Code: "7110", Name: "Interest expense", Type: storage.Expense, AccountAttributes: attrs("Finance costs", "", "Other")},
			{Code: "8010", Name: "Income tax expense", Type: storage.Expense, Role: "income-tax-expense", AccountAttributes: attrs("Income tax", "", "Other")},
		} {
			x, err := u.Accounts().Create(ctx, a)
			if err != nil {
				return fmt.Errorf("%s: %w", a.Code, err)
			}
			b.acc[a.Code] = x
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return b
}

// post appends a balanced SGD entry on day of month, debiting dr and crediting cr by amount, from a source type.
func (b financialBooks) post(t *testing.T, ctx context.Context, s storage.Store, source string, month, day int, dr, cr, amount string) {
	t.Helper()
	id := uuid.NewString()
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Ledger().Append(ctx, storage.NewEntry{PostingDate: porttest.Day(10, 1).AddDate(0, month-10, day-1), PostingIdentity: source + ":" + id + ":v1:test@1",
			RuleVersion: "test@1", Source: storage.Source{Type: source, ID: id, Version: 1},
			Lines: []storage.NewLine{
				{AccountID: b.acc[dr].ID, Currency: "SGD", Amount: storage.Decimal(amount), FXRate: "1", FXRateSource: "base", BaseAmount: storage.Decimal(amount)},
				{AccountID: b.acc[cr].ID, Currency: "SGD", Amount: storage.Decimal("-" + amount), FXRate: "1", FXRateSource: "base", BaseAmount: storage.Decimal("-" + amount)},
			}})
		return err
	}); err != nil {
		t.Fatalf("%s %s/%s %s: %v", source, dr, cr, amount, err)
	}
}

// lay lays out a statement of the books' periods from first to last (0 for October, 1 for November).
func (b financialBooks) lay(t *testing.T, s storage.Store, k financials.Kind, first, last int, compare financials.Compare, revision int64) financials.Statement {
	t.Helper()
	var out financials.Statement
	if err := s.Do(context.Background(), b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.Periods().List(ctx)
		if err != nil {
			return err
		}
		if revision == 0 {
			if revision, err = u.Ledger().Revision(ctx); err != nil {
				return err
			}
		}
		out, err = financials.Lay(ctx, u, money.MustLookup("SGD"), financials.Request{Kind: k, All: all, First: first, Last: last, Compare: compare, Revision: revision})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// rendered is a statement as text: each block's key and totals, with each group's lines as "code amount" and the P&L's
// percentages, one block a line.
func rendered(s financials.Statement) string {
	var b strings.Builder
	for _, x := range s.Blocks {
		fmt.Fprintf(&b, "%s %s", x.Key, x.Totals)
		if len(x.Percents) > 0 && x.Percents[0] != "" {
			fmt.Fprintf(&b, " %s%%", x.Percents[0])
		}
		for _, g := range x.Groups {
			for _, l := range g.Lines {
				fmt.Fprintf(&b, " | %s %s", l.Account.Code, l.Amounts)
			}
			if len(g.Lines) == 0 {
				fmt.Fprintf(&b, " | %s %s", g.Name, g.Totals)
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

// The P&L and balance sheet laid out from the chart (FR-RPT-01, PRD-001 rev 0.159), worked by hand. October:
//   - opening balances: the bank 10,000.00 from share capital;
//   - sales of 5,000.00 on account; hosting of 1,000.00 (COGS) on account; advertising of 500.00 paid;
//   - a laptop of 2,400.00 paid, depreciated 100.00; interest of 20.00 received and 30.00 accrued to the director's
//     loan; income tax of 200.00; the director lends 1,000.00.
//
// The P&L: revenue 5,000.00; cost of sales 1,000.00; gross profit 4,000.00 (80.0%); operating expenses 500.00; EBITDA
// 3,500.00 (70.0%); depreciation 100.00; operating profit 3,400.00 (68.0%); other income 20.00; finance costs 30.00;
// profit before tax 3,390.00 (67.8%); tax 200.00; net profit 3,190.00 (63.8%).
//
// The balance sheet at 31 October: the bank 10,000 − 500 − 2,400 + 20 + 1,000 = 8,120.00 and receivables 5,000.00
// (13,120.00); equipment 2,400.00 less 100.00 (2,300.00); total assets 15,420.00. Payables 1,000.00 and tax 200.00
// (1,200.00); the director's loan 1,030.00; total liabilities 2,230.00. Share capital 10,000.00 and the profit to date
// 3,190.00: 13,190.00. Liabilities and equity 15,420.00, which balances.
// req: FR-RPT-01
// req: FR-RPT-02
// req: INV-10
func TestTheStatementsLaidOutFromTheChart(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "accountant"}})
	s := store(t, "fyniq_api")
	b := newFinancialBooks(t, ctx, s, "Statements Pte Ltd")
	mj := ledger.SourceManualJournal
	b.post(t, ctx, s, ledger.SourceOpeningBalance, 10, 1, "1010", "3010", "10000.00")
	for _, e := range []struct {
		day            int
		dr, cr, amount string
	}{
		{3, "1100", "4010", "5000.00"}, {4, "5010", "2010", "1000.00"}, {5, "6110", "1010", "500.00"}, {6, "1510", "1010", "2400.00"},
		{31, "6610", "1590", "100.00"}, {20, "1010", "7020", "20.00"}, {31, "7110", "2500", "30.00"}, {31, "8010", "2300", "200.00"},
		{10, "1010", "2500", "1000.00"},
	} {
		b.post(t, ctx, s, mj, 10, e.day, e.dr, e.cr, e.amount)
	}
	pl := b.lay(t, s, financials.ProfitAndLoss, 0, 0, financials.NoComparison, 0)
	want := `revenue [5000.00] 100.0% | 4010 [5000.00]
cost_of_sales [1000.00] 20.0% | 5010 [1000.00]
gross_profit [4000.00] 80.0%
operating_expenses [500.00] 10.0% | 6110 [500.00]
ebitda [3500.00] 70.0%
depreciation_amortisation [100.00] 2.0% | 6610 [100.00]
operating_profit [3400.00] 68.0%
other_income [20.00] 0.4% | 7020 [20.00]
finance_and_other [30.00] 0.6% | 7110 [30.00]
profit_before_tax [3390.00] 67.8%
income_tax [200.00] 4.0% | 8010 [200.00]
net_profit [3190.00] 63.8%
`
	if got := rendered(pl); got != want {
		t.Errorf("October's P&L:\n%s\nwant\n%s", got, want)
	}
	bs := b.lay(t, s, financials.BalanceSheet, 0, 0, financials.NoComparison, 0)
	want = `current_assets [13120.00] | 1010 [8120.00] | 1100 [5000.00]
non_current_assets [2300.00] | 1510 [2400.00] | 1590 [-100.00]
total_assets [15420.00]
current_liabilities [1200.00] | 2010 [1000.00] | 2300 [200.00]
borrowings [1030.00] | 2500 [1030.00]
total_liabilities [2230.00]
equity [13190.00] | 3010 [10000.00] | Profit for the year to date [3190.00]
liabilities_and_equity [15420.00]
`
	if got := rendered(bs); got != want {
		t.Errorf("31 October's balance sheet:\n%s\nwant\n%s", got, want)
	}
	if len(bs.Checks) != 2 || !bs.Checks[0].OK || !bs.Checks[1].OK {
		t.Errorf("its checks: %+v", bs.Checks)
	}

	// November, against October: sales of 3,000.00, hosting of 800.00, depreciation of 100.00.
	revision := func() int64 {
		var r int64
		if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			r, err = u.Ledger().Revision(ctx)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return r
	}
	for _, e := range []struct{ dr, cr, amount string }{{"1100", "4010", "3000.00"}, {"5010", "2010", "800.00"}, {"6610", "1590", "100.00"}} {
		b.post(t, ctx, s, mj, 11, 15, e.dr, e.cr, e.amount)
	}
	nov := b.lay(t, s, financials.ProfitAndLoss, 1, 1, financials.PriorPeriod, 0)
	if len(nov.Columns) != 2 || nov.Columns[1].Key != "prior_period" || !nov.Columns[1].From.Equal(porttest.Day(10, 1)) {
		t.Fatalf("November's columns: %+v", nov.Columns)
	}
	if got := strings.SplitN(rendered(nov), "\n", 4)[2]; got != "gross_profit [2200.00 4000.00] 73.3%" {
		t.Errorf("November's gross profit against October's: %s", got)
	}
	if yearAgo := b.lay(t, s, financials.ProfitAndLoss, 1, 1, financials.PriorYear, 0); len(yearAgo.Columns) != 1 {
		t.Errorf("a prior year the books do not have is left out: %+v", yearAgo.Columns)
	}

	// G4-051: as at the revision before a back-dated advertising bill of 50.00 dated 15 October, October's P&L is
	// unchanged; read now, it has it.
	before := revision()
	b.post(t, ctx, s, mj, 10, 15, "6110", "1010", "50.00")
	if got := rendered(b.lay(t, s, financials.ProfitAndLoss, 0, 0, financials.NoComparison, before)); got != rendered(pl) {
		t.Errorf("October as at revision %d after a back-dated posting:\n%s", before, got)
	}
	if got := b.lay(t, s, financials.ProfitAndLoss, 0, 0, financials.NoComparison, 0).Blocks[3].Totals[0].String(); got != "550.00" {
		t.Errorf("October's operating expenses now: %s", got)
	}

	// An account whose cash-flow line no section knows fails the balance sheet's check, by name.
	b.post(t, ctx, s, mj, 11, 20, "1999", "1010", "10.00")
	bs = b.lay(t, s, financials.BalanceSheet, 1, 1, financials.NoComparison, 0)
	if c := bs.Checks[1]; c.OK || c.Key != "placed" || !strings.HasPrefix(c.Detail, "1999 Unsorted: ") {
		t.Errorf("an account in no section: %+v", c)
	}
	if c := closeChecks(t, ctx, s, b.Org, b.Nov.ID)["statements"]; c.Severity != closing.Warning || c.Title != "Accounts are missing from the balance sheet" ||
		!strings.HasPrefix(c.Detail, "1999 Unsorted: ") {
		t.Errorf("November's close: %+v", c)
	}
	tb := b.lay(t, s, financials.TrialBalance, 1, 1, financials.NoComparison, 0)
	if last := tb.Blocks[len(tb.Blocks)-1]; last.Key != financials.KeyTotal || !last.Totals[0].IsZero() || !tb.Checks[0].OK {
		t.Errorf("the trial balance nets to zero: %+v %+v", last, tb.Checks)
	}
}
