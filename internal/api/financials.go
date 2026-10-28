package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/financials"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------- the financial statements (FR-RPT-01, FR-RPT-02, FR-RPT-08)

// The statements' error codes.
const (
	CodeNoPeriod       = "REPORT-03" // a month the books have no period for
	CodeLaterRevision  = "REPORT-04" // a revision later than the ledger's
	statementMonthForm = "2006-01"
)

// Percent is a figure as a % of its column's revenue, to one decimal (RND-PERCENT), or "" for none.
type Percent string

// Schema implements huma.SchemaProvider.
func (Percent) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Description: "A % of revenue to one decimal, such as 12.5; empty for none.", Examples: []any{"12.5"}}
}

// FinancialLine is one account's figures in a statement.
type FinancialLine struct {
	AccountID string    `json:"account_id" format:"uuid" doc:"The account, to trace its figures in the general ledger." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d60"`
	Code      string    `json:"code" doc:"Its code." example:"4010"`
	Name      string    `json:"name" doc:"Its name, as the chart has it today (FR-RPT-08)." example:"Subscription revenue"`
	Function  string    `json:"function,omitempty" doc:"Its function: COGS, R&D, S&M, G&A or Other." example:"S&M"`
	Amounts   []Money   `json:"amounts" nullable:"false" doc:"One per column, signed as the statement shows them: income, liabilities and equity as positive credits, expenses and assets as positive debits; the trial balance keeps the ledger's sign, debits positive."`
	Percents  []Percent `json:"percents" nullable:"false" doc:"The P&L only: each column's % of that column's revenue (RND-PERCENT); empty where the revenue is zero, and for other statements."`
}

// FinancialGroup is a report group's accounts and their total.
type FinancialGroup struct {
	Name     string          `json:"name" doc:"The report group; or a computed line such as \"Profit for the year to date\", which has no lines." example:"Sales & marketing"`
	Totals   []Money         `json:"totals" nullable:"false" doc:"One per column."`
	Percents []Percent       `json:"percents" nullable:"false" doc:"As a line's."`
	Lines    []FinancialLine `json:"lines" nullable:"false" doc:"Its accounts with figures, by code."`
}

// FinancialBlock is a section of a statement, or a measure computed from those above it.
type FinancialBlock struct {
	Key      string           `json:"key" doc:"The block: for the P&L revenue, cost_of_sales, gross_profit, operating_expenses, ebitda, depreciation_amortisation, operating_profit, other_income, finance_and_other, profit_before_tax, income_tax, net_profit; for the balance sheet current_assets, non_current_assets, total_assets, current_liabilities, borrowings, total_liabilities, equity, liabilities_and_equity, equity_movement; for the cash flow operating, net_operating, investing, net_investing, financing, net_financing, net_change, cash_at_start, cash_at_end; for the trial balance asset, liability, equity, revenue, expense and total." example:"gross_profit"`
	Title    string           `json:"title" doc:"Its title." example:"Gross profit"`
	Measure  bool             `json:"measure" doc:"true for a line computed from the sections above it." example:"true"`
	Totals   []Money          `json:"totals" nullable:"false" doc:"One per column."`
	Percents []Percent        `json:"percents" nullable:"false" doc:"As a line's."`
	Groups   []FinancialGroup `json:"groups" nullable:"false" doc:"A section's report groups, in the order of their first account's code; none for a measure."`
}

// FinancialColumn is a column of a statement: a run of whole months.
type FinancialColumn struct {
	Key  string `json:"key" doc:"current, prior_period or prior_year; a month such as 2026-10 when the columns are by month; total, the run's total of a P&L by month." example:"current"`
	From string `json:"from" format:"date" doc:"The run's first day." example:"2026-10-01"`
	To   string `json:"to" format:"date" doc:"The run's last day: a balance sheet's or a trial balance's figures are at its end." example:"2026-10-31"`
}

// FinancialCheck is one of a statement's checks (INV-10).
type FinancialCheck struct {
	Key    string `json:"key" doc:"balances: debits equal credits in every column; placed: every account with figures sits in a section or a line; equity: the balance sheet's movement in equity reconciles; ties: the cash flow ties to the cash accounts." example:"balances"`
	OK     bool   `json:"ok" doc:"true when it holds." example:"true"`
	Title  string `json:"title" doc:"What it found." example:"Debits equal credits"`
	Detail string `json:"detail,omitempty" doc:"What is wrong and how to fix it." example:"1999 Unsorted: give each a cash-flow line of the standard chart in the chart of accounts"`
}

// FinancialNonCash is an investing or financing transaction that moved no money, listed beside the cash flow.
type FinancialNonCash struct {
	Date       string `json:"date" format:"date" doc:"Its date." example:"2026-10-31"`
	SourceType string `json:"source_type" doc:"Where it comes from (ENG-03)." example:"manual_journal"`
	SourceID   string `json:"source_id" doc:"The source document, to open it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d70"`
	Memo       string `json:"memo,omitempty" doc:"The entry's memo." example:"Interest on the director's loan"`
	Amount     Money  `json:"amount" doc:"Its debits, in the base currency."`
}

// FinancialStatement is a statement laid out from the chart (FR-RPT-01; PRD-001 rev 0.159, provisional), read as at
// one ledger revision (FR-RPT-08).
type FinancialStatement struct {
	Kind            string             `json:"kind" enum:"pl,bs,cf,tb" doc:"pl: the profit and loss; bs: the balance sheet; cf: the cash flow; tb: the trial balance." example:"pl"`
	StatementTitle  string             `json:"statement_title" enum:"Management statements,Financial statements" doc:"How the financial year's statements are titled: management statements until its framework and policies are approved (FR-ORG-10)." example:"Management statements"`
	Basis           string             `json:"basis,omitempty" doc:"What they are prepared under, once approved." example:"Prepared under SFRS for Small Entities (2nd edition)"`
	Currency        string             `json:"currency" doc:"The base currency every amount is in." example:"SGD"`
	From            string             `json:"from" format:"date" doc:"The run's first day." example:"2026-10-01"`
	To              string             `json:"to" format:"date" doc:"The run's last day." example:"2026-10-31"`
	Revision        int64              `json:"ledger_revision" doc:"The ledger revision every figure is read as at (ENG-46): asking again as at it gives the same figures." example:"42"`
	Restated        bool               `json:"restated_presentation" doc:"true when an account was created or changed after the revision, so the names and groups are today's, not those in force at it (rev 0.159)." example:"false"`
	PlatformVersion string             `json:"platform_version" doc:"The platform version that laid it out (REL-05)." example:"0.1.0"`
	Columns         []FinancialColumn  `json:"columns" nullable:"false" doc:"The columns, in order: the run, then its comparative; or a month each, then for a P&L the total."`
	Blocks          []FinancialBlock   `json:"blocks" nullable:"false" doc:"The statement, block by block in order."`
	Checks          []FinancialCheck   `json:"checks" nullable:"false" doc:"Its checks: the balance sheet and trial balance balance, and every balance-sheet account sits in a section (INV-10); the movement in equity reconciles, and the cash flow ties to the cash accounts with every account in a line (INV-11)."`
	NonCash         []FinancialNonCash `json:"non_cash" nullable:"false" doc:"The cash flow only: the run's investing and financing transactions that moved no money, such as a cost a director paid personally or a conversion to shares, left out of its figures and listed apart."`
}

type financialStatementInput struct {
	Kind     string `path:"kind" enum:"pl,bs,cf,tb" doc:"pl: the profit and loss; bs: the balance sheet; cf: the cash flow; tb: the trial balance." example:"pl"`
	From     string `query:"from" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"The run's first month, YYYY-MM; left out, the last month's. A balance sheet is at the last month's end, with the movement in equity over the run; a trial balance is at the last month's end." example:"2026-10"`
	To       string `query:"to" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"The run's last month, YYYY-MM; left out, the month of the organisation's today." example:"2026-10"`
	Compare  string `query:"compare" enum:"prior_period,prior_year" doc:"A comparative column: the run of the same length just before, or the same months a year earlier. Left out when the books do not have all its months. Budget and forecast arrive with planning." example:"prior_period"`
	Columns  string `query:"columns" enum:"total,month" default:"total" doc:"total: one column for the run, and its comparative; month: a column per month and, for a P&L, the total, without a comparative." example:"total"`
	Revision int64  `query:"as_at_revision" minimum:"1" doc:"Read as at this ledger revision (FR-RPT-08), as an earlier statement recorded it; left out, the ledger as it stands." example:"42"`
}

type financialStatementOutput struct{ Body FinancialStatement }

func moneysOut(ms []money.Money) []Money {
	out := make([]Money, len(ms))
	for i, m := range ms {
		out[i] = moneyOut(m)
	}
	return out
}

// percentsOut is a statement's percentages, one per column.
func percentsOut(ps []string, n int) []Percent {
	out := make([]Percent, n)
	for i := range min(len(ps), n) {
		out[i] = Percent(ps[i])
	}
	return out
}

func registerFinancials(a *API) {
	register(a, authz.GetFinancialStatement, huma.Operation{
		OperationID: "getFinancialStatement", Method: http.MethodGet, Path: "/v1/reports/statements/{kind}", Tags: []string{"Reports"},
		Summary: "Get a financial statement",
		Description: "The profit and loss, the balance sheet, the cash flow or the trial balance (FR-RPT-01) for a run of whole months (FR-RPT-02), with " +
			"the prior period or the prior year beside it, or a column per month. It is laid out from each account's chart " +
			"attributes (PRD-001 rev 0.159, provisional until the accountant confirms): the P&L from revenue by stream through " +
			"gross profit, EBITDA and operating profit to net profit, each with its % of revenue; the balance sheet by current " +
			"and non-current groups with borrowings apart, the year's profit to date in equity, and the movement in equity over the " +
			"run; the cash flow by the indirect method, each entry operating, investing or financing by the accounts it moves, " +
			"tied to the cash accounts, with the transactions that moved no money listed apart. Every figure is read as at one " +
			"ledger revision (FR-RPT-08), which it returns: asking again with as_at_revision gives the same figures, whatever " +
			"was posted since, with today's account names, flagged restated_presentation when the chart changed after it. Its " +
			"checks (INV-10, INV-11) say whether it balances, ties and reconciles, and whether every account sits in a section. A month the books have no " +
			"period for is refused (REPORT-03), as is a revision later than the ledger's (REPORT-04). Read-only; no side effects.",
	}, []string{CodeInvalid, CodeNoPeriod, CodeLaterRevision}, func(ctx context.Context, in *financialStatementInput) (*financialStatementOutput, error) {
		p, _ := authn.From(ctx)
		var out FinancialStatement
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			org, err := u.Organisation(ctx)
			if err != nil {
				return err
			}
			base, err := money.Lookup(org.BaseCurrency)
			if err != nil {
				return err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return err
			}
			to := in.To
			if to == "" {
				to = today.Format(statementMonthForm)
			}
			from := in.From
			if from == "" || in.Kind == string(financials.TrialBalance) {
				from = to
			}
			all, err := u.Periods().List(ctx)
			if err != nil {
				return err
			}
			index := func(field, month string) (int, *FieldProblem) {
				for i, x := range all {
					if x.StartsOn.Format(statementMonthForm) == month {
						return i, nil
					}
				}
				return 0, &FieldProblem{Code: CodeNoPeriod, Field: field, Message: "the books have no period for " + month,
					Fix: "Choose a month of an open financial year, or open the year in Accounting › Periods and close"}
			}
			var bad []FieldProblem
			first, fp := index("from", from)
			if fp != nil {
				bad = append(bad, *fp)
			}
			last, tp := index("to", to)
			if tp != nil {
				bad = append(bad, *tp)
			}
			if len(bad) == 0 && first > last {
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "to", Message: "is before from", Fix: "Send a last month on or after the first"})
			}
			current, err := u.Ledger().Revision(ctx)
			if err != nil {
				return err
			}
			revision := current
			if in.Revision != 0 {
				if in.Revision > current {
					bad = append(bad, FieldProblem{Code: CodeLaterRevision, Field: "as_at_revision", Message: "is later than the ledger's revision",
						Fix: "Send a revision a statement recorded, or leave it out to read the ledger as it stands"})
				}
				revision = in.Revision
			}
			if len(bad) > 0 {
				return invalid(bad)
			}
			s, err := financials.Lay(ctx, u, base, financials.Request{Kind: financials.Kind(in.Kind), All: all, First: first, Last: last,
				Compare: financials.Compare(in.Compare), ByMonth: in.Columns == "month", Revision: revision})
			if err != nil {
				return err
			}
			restated := false
			if revision < current {
				if restated, err = u.Accounts().ChangedAfter(ctx, revision); err != nil {
					return err
				}
			}
			// Without its financial years set, the books have no framework to title them by: management statements.
			year := AccountingYear{StatementTitle: "Management statements"}
			if y, err := a.accountingYear(ctx, u, all[last].EndsOn.Format(time.DateOnly)); err == nil {
				year = y
			} else if pe := (*period.Error)(nil); !errors.As(err, &pe) || pe.Code != period.CodeNoYear {
				return err
			}
			out = FinancialStatement{Kind: in.Kind, StatementTitle: year.StatementTitle, Basis: year.StatementBasis, Currency: base.Code,
				From: dateOf(all[first].StartsOn), To: dateOf(all[last].EndsOn), Revision: revision, Restated: restated,
				PlatformVersion: a.deps.Version, Columns: []FinancialColumn{}, Blocks: []FinancialBlock{}, Checks: []FinancialCheck{}, NonCash: []FinancialNonCash{}}
			n := len(s.Columns)
			for _, c := range s.Columns {
				out.Columns = append(out.Columns, FinancialColumn{Key: c.Key, From: dateOf(c.From), To: dateOf(c.To)})
			}
			for _, b := range s.Blocks {
				x := FinancialBlock{Key: b.Key, Title: b.Title, Measure: b.Measure, Totals: moneysOut(b.Totals), Percents: percentsOut(b.Percents, n),
					Groups: []FinancialGroup{}}
				for _, g := range b.Groups {
					y := FinancialGroup{Name: g.Name, Totals: moneysOut(g.Totals), Percents: percentsOut(g.Percents, n), Lines: []FinancialLine{}}
					for _, l := range g.Lines {
						y.Lines = append(y.Lines, FinancialLine{AccountID: l.Account.ID.String(), Code: l.Account.Code, Name: l.Account.Name,
							Function: l.Account.Function, Amounts: moneysOut(l.Amounts), Percents: percentsOut(l.Percents, n)})
					}
					x.Groups = append(x.Groups, y)
				}
				out.Blocks = append(out.Blocks, x)
			}
			for _, c := range s.Checks {
				out.Checks = append(out.Checks, FinancialCheck{Key: c.Key, OK: c.OK, Title: c.Title, Detail: c.Detail})
			}
			for _, x := range s.NonCash {
				out.NonCash = append(out.NonCash, FinancialNonCash{Date: dateOf(x.Date), SourceType: x.Source.Type, SourceID: x.Source.ID, Memo: x.Memo, Amount: moneyOut(x.Amount)})
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return &financialStatementOutput{Body: out}, nil
	})
}
