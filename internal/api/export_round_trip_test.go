//go:build local

package api

import (
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"math/big"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/pdftext"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/reportexport"
	"github.com/cognizhi/fyniq/internal/sheet"
)

// amounts are every money amount an API answer holds, as written: the "amount" of each object that has a currency.
func amounts(v any) []string {
	var out []string
	switch x := v.(type) {
	case map[string]any:
		if a, ok := x["amount"].(string); ok {
			if _, ok := x["currency"]; ok {
				out = append(out, a)
			}
		}
		for _, y := range x {
			out = append(out, amounts(y)...)
		}
	case []any:
		for _, y := range x {
			out = append(out, amounts(y)...)
		}
	}
	return out
}

// rat is a plain decimal, exactly; ok is false for anything else.
func rat(s string) (*big.Rat, bool) {
	if s == "" {
		return nil, false
	}
	return new(big.Rat).SetString(s)
}

// asPrinted is a plain decimal as a printed statement shows it: thousands grouped, a minus sign (U+2212) before a
// negative. It is written here from the rule, not from the code that prints.
func asPrinted(d string) string {
	neg := strings.HasPrefix(d, "-")
	whole, frac, _ := strings.Cut(strings.TrimPrefix(d, "-"), ".")
	var b strings.Builder
	for i, c := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	out := b.String()
	if frac != "" {
		out += "." + frac
	}
	if neg && strings.Trim(out, "0.,") != "" {
		out = "−" + out
	}
	return out
}

// exported downloads an export in a format and returns its bytes.
func exported(t *testing.T, e *env, path, format, token string) []byte {
	t.Helper()
	r := e.call(http.MethodGet, path+"&format="+format, token, nil)
	b, err := base64.StdEncoding.DecodeString(r.str("content_base64"))
	if r.Status != http.StatusOK || err != nil || len(b) == 0 {
		t.Fatalf("%s as %s: %d %.300s", path, format, r.Status, r.Raw)
	}
	return b
}

// QA-RPT-03, the export round trip: each financial statement and the trial balance, exported as CSV, XLSX and PDF, say
// what the API says.
// The books hold a loss, a six-figure balance and amounts with grouping, on four report groups. For the P&L, the
// balance sheet and the cash flow, every amount of the API's statement is read back from the workbook figure by figure
// (exactly, as a number cell), found in the CSV as a plain decimal, and found in the PDF's extracted text as printed;
// and the CSV's own figures reconcile: the P&L's lines add up to its net profit, the balance sheet's assets equal its
// equity and liabilities, and the trial balance's sections add up to their totals and to nothing in all.
// req: QA-RPT-03
// req: FR-RPT-05
func TestExportsRoundTripFigureByFigure(t *testing.T) {
	e := newEnv(t)
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	acc := map[string]storage.Account{}
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		for _, a := range []storage.NewAccount{
			{Code: "1010", Name: "DBS current", Type: storage.Asset, AccountAttributes: storage.AccountAttributes{ReportGroup: "Cash & bank", CashFlowLine: "Cash & cash equivalents"}},
			{Code: "2010", Name: "Trade payables", Type: storage.Liability, AccountAttributes: storage.AccountAttributes{ReportGroup: "Trade payables", CashFlowLine: "Payables & accruals"}},
			{Code: "3010", Name: "Share capital", Type: storage.Equity, Role: storage.RoleShareCapital, AccountAttributes: storage.AccountAttributes{ReportGroup: "Share capital", CashFlowLine: "Share capital"}},
			{Code: "4010", Name: "Subscription revenue", Type: storage.Revenue, AccountAttributes: storage.AccountAttributes{ReportGroup: "Subscription revenue"}},
			{Code: "4020", Name: "Services revenue", Type: storage.Revenue, AccountAttributes: storage.AccountAttributes{ReportGroup: "Services revenue"}},
			{Code: "6110", Name: "Advertising", Type: storage.Expense, AccountAttributes: storage.AccountAttributes{ReportGroup: "Sales & marketing", Function: "S&M"}},
			{Code: "6210", Name: "Rent", Type: storage.Expense, AccountAttributes: storage.AccountAttributes{ReportGroup: "Premises & office", Function: "G&A"}},
		} {
			x, err := u.Accounts().Create(ctx, a)
			if err != nil {
				return err
			}
			acc[a.Code] = x
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	post := func(source string, day int, dr, cr, amount string) {
		t.Helper()
		id := uuid.NewString()
		if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Ledger().Append(ctx, storage.NewEntry{PostingDate: porttest.Day(10, day), PostingIdentity: source + ":" + id + ":v1:test@1",
				RuleVersion: "test@1", Source: storage.Source{Type: source, ID: id, Version: 1}, Lines: []storage.NewLine{
					{AccountID: acc[dr].ID, Currency: "SGD", Amount: storage.Decimal(amount), FXRate: "1", FXRateSource: "base", BaseAmount: storage.Decimal(amount)},
					{AccountID: acc[cr].ID, Currency: "SGD", Amount: storage.Decimal("-" + amount), FXRate: "1", FXRateSource: "base", BaseAmount: storage.Decimal("-" + amount)},
				}})
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	post(ledger.SourceOpeningBalance, 1, "1010", "3010", "250000.00")
	post(ledger.SourceManualJournal, 5, "1010", "4010", "12345.67")
	post(ledger.SourceManualJournal, 6, "1010", "4020", "8900.10")
	post(ledger.SourceManualJournal, 9, "6110", "1010", "23456.78")
	post(ledger.SourceManualJournal, 12, "6210", "2010", "4500.00")

	accountant := e.as(authz.Accountant)
	for _, kind := range []string{"pl?from=2026-10&to=2026-10", "bs?to=2026-10", "cf?from=2026-10&to=2026-10", "tb?to=2026-10"} {
		path := "/v1/reports/statements/" + strings.Replace(kind, "?", "/export?", 1)
		statement := e.call(http.MethodGet, "/v1/reports/statements/"+kind, accountant, nil)
		want := amounts(statement.Body)
		if statement.Status != http.StatusOK || len(want) < 5 {
			t.Fatalf("%s: %d, %d amounts: %.300s", kind, statement.Status, len(want), statement.Raw)
		}

		// The workbook: its first sheet is the statement, every figure a number cell.
		g, err := sheet.ReadXLSX(exported(t, e, path, "xlsx", accountant), 1000)
		if err != nil {
			t.Fatalf("%s: the workbook does not read: %v", kind, err)
		}
		numbers := map[string]bool{}
		for i, row := range g.Rows {
			for j, c := range row {
				if g.Numeric[i][j] {
					if r, ok := rat(c); ok {
						numbers[r.RatString()] = true
					}
				}
			}
		}
		// The CSV: every figure a plain decimal.
		reader := csv.NewReader(strings.NewReader(string(exported(t, e, path, "csv", accountant))))
		reader.FieldsPerRecord = -1 // a section's title is a row of one cell
		records, err := reader.ReadAll()
		if err != nil {
			t.Fatalf("%s: the CSV does not read: %v", kind, err)
		}
		inCSV := map[string]bool{}
		byLabel := map[string]*big.Rat{}
		for _, rec := range records[1:] {
			if len(rec) > 1 {
				if r, ok := rat(rec[1]); ok {
					inCSV[r.RatString()] = true
					byLabel[rec[0]] = r
				}
			}
		}
		// The PDF: every figure as printed.
		text, err := pdftext.Text(exported(t, e, path, "pdf", accountant))
		if err != nil {
			t.Fatalf("%s: the PDF's text does not read: %v", kind, err)
		}
		for _, a := range want {
			r, ok := rat(a)
			if !ok {
				t.Errorf("%s: the API's amount %q is not a decimal", kind, a)
				continue
			}
			if !numbers[r.RatString()] {
				t.Errorf("%s: %s is not a number cell of the workbook", kind, a)
			}
			if !inCSV[r.RatString()] {
				t.Errorf("%s: %s is not in the CSV", kind, a)
			}
			if !strings.Contains(text, asPrinted(a)) {
				t.Errorf("%s: %s (printed %s) is not in the PDF's text:\n%s", kind, a, asPrinted(a), text)
			}
		}

		// The CSV's figures reconcile on their own.
		sum := func(labels ...string) *big.Rat {
			s := new(big.Rat)
			for _, l := range labels {
				v, ok := byLabel[l]
				if !ok {
					t.Errorf("%s: the CSV has no row %q: %v", kind, l, records)
					continue
				}
				s.Add(s, v)
			}
			return s
		}
		switch {
		case strings.HasPrefix(kind, "pl"):
			revenue := sum("4010 Subscription revenue", "4020 Services revenue")
			costs := sum("6110 Advertising", "6210 Rent")
			if net := new(big.Rat).Sub(revenue, costs); net.Cmp(sum("Net profit")) != 0 {
				t.Errorf("the P&L's CSV: revenue %s less costs %s is %s; its net profit says %s", revenue.FloatString(2), costs.FloatString(2),
					net.FloatString(2), sum("Net profit").FloatString(2))
			}
		case strings.HasPrefix(kind, "tb"):
			// Each section's accounts add up to its total, and every account together to the total of nothing.
			all, section, title := new(big.Rat), new(big.Rat), ""
			for _, rec := range records[1:] {
				switch {
				case len(rec) == 1 && rec[0] != "":
					title, section = rec[0], new(big.Rat)
				case len(rec) > 1 && len(rec[0]) > 5 && rec[0][4] == ' ' && strings.Trim(rec[0][:4], "0123456789") == "":
					v, _ := rat(rec[1])
					section.Add(section, v)
					all.Add(all, v)
				case len(rec) > 1 && rec[0] == "Total "+strings.ToLower(title):
					if v, _ := rat(rec[1]); v == nil || v.Cmp(section) != 0 {
						t.Errorf("the trial balance's CSV: %s's accounts add up to %s; %q says %s", title, section.FloatString(2), rec[0], rec[1])
					}
				}
			}
			if all.Sign() != 0 || sum("Total").Sign() != 0 {
				t.Errorf("the trial balance's CSV: its accounts add up to %s, its total says %s", all.FloatString(2), sum("Total").FloatString(2))
			}
		case strings.HasPrefix(kind, "bs"):
			assets := sum("1010 DBS current")
			if other := sum("2010 Trade payables", "3010 Share capital", "Profit for the year to date"); assets.Cmp(other) != 0 {
				t.Errorf("the balance sheet's CSV: assets %s, equity and liabilities %s", assets.FloatString(2), other.FloatString(2))
			}
		}
	}

	// The report catalogue's exports, prepared by the worker (FR-RPT-09): the trial balance as CSV and as XLSX holds every
	// balance of the trial balance statement, its debits equal to its credits; the general ledger for October holds each
	// account's movements.
	worker := workerStore(t)
	p := reportexport.Preparer{Store: worker, Files: e.blobs, Outbox: outbox.Dispatcher{Store: worker, Queue: local.NewMemory(), Clock: e.clock}, Clock: e.clock}
	prepared := func(body map[string]any) []byte {
		t.Helper()
		r := e.call(http.MethodPost, "/v1/report-exports", accountant, body)
		payload, _ := json.Marshal(map[string]string{"export_id": r.str("id")})
		if err := p.Work()(ctx, e.books.Org, storage.OutboxRow{Kind: reportexport.Kind, Payload: payload}); err != nil {
			t.Fatalf("preparing %v: %v", body, err)
		}
		f := e.call(http.MethodGet, "/v1/report-exports/"+r.str("id")+"/file", accountant, nil)
		b, err := base64.StdEncoding.DecodeString(f.str("content_base64"))
		if f.Status != http.StatusOK || err != nil {
			t.Fatalf("%v: %d %.300s", body, f.Status, f.Raw)
		}
		return b
	}
	var balances []string // the statement's account lines, not its section totals, which the catalogue's layout leaves out
	var lines func(v any)
	lines = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if ls, ok := x["lines"].([]any); ok {
				for _, l := range ls {
					balances = append(balances, amounts(l.(map[string]any)["amounts"])...)
				}
			}
			for k, y := range x {
				if k != "lines" {
					lines(y)
				}
			}
		case []any:
			for _, y := range x {
				lines(y)
			}
		}
	}
	lines(e.call(http.MethodGet, "/v1/reports/statements/tb?to=2026-10", accountant, nil).Body)
	if len(balances) != 7 {
		t.Fatalf("the trial balance statement has %d account lines; want 7: %v", len(balances), balances)
	}
	tbCSV := string(prepared(map[string]any{"report": "trial_balance", "to": "2026-10-31", "format": "csv"}))
	g, err := sheet.ReadXLSX(prepared(map[string]any{"report": "trial_balance", "to": "2026-10-31", "format": "xlsx"}), 1000)
	if err != nil {
		t.Fatal(err)
	}
	cells := map[string]bool{}
	for i, row := range g.Rows {
		for j, c := range row {
			if r, ok := rat(c); ok && g.Numeric[i][j] {
				cells[r.RatString()] = true
			}
		}
	}
	for _, a := range balances {
		r, _ := rat(a)
		if r.Sign() == 0 {
			continue
		}
		abs := new(big.Rat).Abs(r)
		if !cells[r.RatString()] && !cells[abs.RatString()] {
			t.Errorf("the catalogue's trial balance as XLSX lacks %s", a)
		}
		if !strings.Contains(tbCSV, a) && !strings.Contains(tbCSV, abs.FloatString(2)) {
			t.Errorf("the catalogue's trial balance as CSV lacks %s:\n%s", a, tbCSV)
		}
	}
	totals := regexp.MustCompile(`"Total, at 2026-10-31","",([0-9.]+),([0-9.]+)`).FindStringSubmatch(tbCSV)
	if totals == nil || totals[1] != totals[2] {
		t.Errorf("the catalogue's trial balance: its debits and credits differ, or have no total: %v", totals)
	}
	gl := string(prepared(map[string]any{"report": "general_ledger", "from": "2026-10-01", "to": "2026-10-31", "format": "csv"}))
	for _, a := range []string{"12345.67", "8900.10", "23456.78", "4500.00", "250000.00"} {
		if !strings.Contains(gl, a) {
			t.Errorf("the catalogue's general ledger lacks the movement %s:\n%s", a, gl)
		}
	}
}
