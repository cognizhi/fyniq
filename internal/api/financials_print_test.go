package api

import (
	"slices"
	"strings"
	"testing"
)

// A statement prints as it reads (FR-RPT-05): its sections, accounts with their codes, group and section totals and
// measures in order, with the % of revenue beside a P&L's column; its period, title and revision; and as notes the
// checks that failed, the restated presentation and the cash flow's transactions that moved no money. Worked by hand.
// req: FR-RPT-05
// req: FR-RPT-08
func TestAStatementPrintsAsItReads(t *testing.T) {
	sgd := func(s string) Money { return Money{Amount: s, Currency: "SGD"} }
	pl := FinancialStatement{Kind: "pl", StatementTitle: "Management statements", Currency: "SGD", From: "2026-10-01", To: "2026-10-31", Revision: 42,
		Columns: []FinancialColumn{{Key: "current", From: "2026-10-01", To: "2026-10-31"}},
		Blocks: []FinancialBlock{
			{Key: "revenue", Title: "Revenue", Totals: []Money{sgd("500.00")}, Percents: []Percent{"100.0"}, Groups: []FinancialGroup{{Name: "Subscriptions",
				Totals: []Money{sgd("500.00")}, Percents: []Percent{"100.0"}, Lines: []FinancialLine{{Code: "4010", Name: "Subscription revenue",
					Amounts: []Money{sgd("500.00")}, Percents: []Percent{"100.0"}}}}}},
			{Key: "operating_expenses", Title: "Operating expenses", Totals: []Money{sgd("1600.00")}, Percents: []Percent{"320.0"}, Groups: []FinancialGroup{
				{Name: "Sales & marketing", Totals: []Money{sgd("1600.00")}, Percents: []Percent{"320.0"}, Lines: []FinancialLine{
					{Code: "6110", Name: "Advertising", Amounts: []Money{sgd("100.00")}, Percents: []Percent{"20.0"}},
					{Code: "6120", Name: "Events", Amounts: []Money{sgd("1500.00")}, Percents: []Percent{"300.0"}}}},
				{Name: "General & administrative", Totals: []Money{sgd("0.00")}, Percents: []Percent{""}, Lines: []FinancialLine{}}}},
			{Key: "net_profit", Title: "Net profit", Measure: true, Totals: []Money{sgd("-1100.00")}, Percents: []Percent{"-220.0"}},
		},
		Checks: []FinancialCheck{{Key: "balances", OK: true, Title: "Debits equal credits"}, {Key: "placed", Title: "1999 Unsorted is in no section",
			Detail: "give it a type in the chart of accounts"}},
		Restated: true}
	r := statementReport(pl, "Cognizhi Pte Ltd", "0.30.0")
	if err := r.Check(); err != nil {
		t.Fatal(err)
	}
	if r.Title != "Profit and loss" || r.Author != "Cognizhi Pte Ltd" || !slices.Equal(r.Lines, []string{"Management statements",
		"1 October 2026 to 31 October 2026", "Amounts in SGD"}) || !slices.Equal(r.Columns, []string{"Oct 2026", "% of revenue"}) || r.Label != "SGD" {
		t.Errorf("the head: %q %q %q %q", r.Title, r.Lines, r.Columns, r.Label)
	}
	var rows []string
	for _, x := range r.Rows {
		rows = append(rows, strings.Join(strings.Fields(x.Kind+" "+x.Code+" "+x.Label+" "+strings.Join(x.Cells, " ")), " "))
	}
	want := []string{
		"section Revenue",
		"line 4010 Subscription revenue 500.00 100.0%",
		"total Total revenue 500.00 100.0%",
		"section Operating expenses",
		"line 6110 Advertising 100.00 20.0%",
		"line 6120 Events 1,500.00 300.0%",
		"subtotal Sales & marketing 1,600.00 320.0%",
		"subtotal General & administrative 0.00",
		"total Total operating expenses 1,600.00 320.0%",
		"measure Net profit −1,100.00 −220.0%",
	}
	if !slices.Equal(rows, want) {
		t.Errorf("the rows:\n%s\nwant:\n%s", strings.Join(rows, "\n"), strings.Join(want, "\n"))
	}
	if len(r.Notes) != 2 || r.Notes[0].Title != "Checks that need attention" || !slices.Equal(r.Notes[0].Items, []string{"1999 Unsorted is in no section: give it a type in the chart of accounts"}) ||
		r.Notes[1].Title != "Restated presentation" || !strings.Contains(r.Notes[1].Items[0], "after ledger revision 42") {
		t.Errorf("the notes: %+v", r.Notes)
	}
	if r.Footer != "Management statements · read at ledger revision 42 · Fyniq 0.30.0 · restated presentation" {
		t.Errorf("the footer: %q", r.Footer)
	}

	// A balance sheet is at a day; a cash flow lists what moved no money; a P&L by month has no % columns.
	bs := FinancialStatement{Kind: "bs", StatementTitle: "Financial statements", Basis: "Prepared under SFRS for Small Entities", Currency: "SGD",
		From: "2026-10-01", To: "2026-10-31", Revision: 7, Columns: []FinancialColumn{{Key: "current", From: "2026-10-01", To: "2026-10-31"},
			{Key: "prior_year", From: "2025-10-01", To: "2025-10-31"}}}
	if r := statementReport(bs, "x", "1"); r.Lines[0] != "Financial statements. Prepared under SFRS for Small Entities" || r.Lines[1] != "At 31 October 2026" ||
		!slices.Equal(r.Columns, []string{"Oct 2026", "Oct 2025"}) || r.Notes != nil {
		t.Errorf("a balance sheet: %q %q %v", r.Lines, r.Columns, r.Notes)
	}
	cf := FinancialStatement{Kind: "cf", StatementTitle: "Management statements", Currency: "SGD", From: "2026-10-01", To: "2026-10-31",
		Columns: []FinancialColumn{{Key: "current", From: "2026-10-01", To: "2026-10-31"}},
		NonCash: []FinancialNonCash{{Date: "2026-10-15", Memo: "Software paid by a director", Amount: sgd("240.00")}}}
	if r := statementReport(cf, "x", "1"); len(r.Notes) != 1 || r.Notes[0].Items[0] != "15 October 2026 Software paid by a director: 240.00" {
		t.Errorf("a cash flow's non-cash transactions: %+v", r.Notes)
	}
	byMonth := pl
	byMonth.Columns = []FinancialColumn{{Key: "2026-09", From: "2026-09-01", To: "2026-09-30"}, {Key: "2026-10", From: "2026-10-01", To: "2026-10-31"},
		{Key: "total", From: "2026-09-01", To: "2026-10-31"}}
	byMonth.Blocks = []FinancialBlock{{Key: "net_profit", Title: "Net profit", Measure: true, Totals: []Money{sgd("1.00"), sgd("2.00"), sgd("3.00")}}}
	if r := statementReport(byMonth, "x", "1"); !slices.Equal(r.Columns, []string{"Sep 2026", "Oct 2026", "Total"}) || len(r.Rows[0].Cells) != 3 {
		t.Errorf("a P&L by month: %q %q", r.Columns, r.Rows)
	}
}
