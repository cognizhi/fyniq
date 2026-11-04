//go:build local

package api

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The founders' money in the statements (FR-EQ-10, FR-EQ-11; p1-founders-money design D14), worked by hand in October
// 2026. Ah Kow lends S$1,000.00 on 1 Oct and is paid S$1,500.00 on 2 Oct: he owes the company S$500.00. Mei Lin lends
// S$2,000.00 on 3 Oct, waives S$300.00 as a shareholder to 3300, and converts S$1,200.00 at S$1.20 a share (1,000
// shares), effective on 15 Oct: the company owes her S$500.00. 2500 nets to nothing, yet the balance sheet shows Ah
// Kow's S$500.00 under 1250 Staff & director advances and Mei Lin's S$500.00 under borrowings, with no posting: assets
// S$2,000.00 (the bank S$1,500.00 and the advance), borrowings S$500.00 and equity S$1,500.00. The cash flow's
// financing is the S$1,500.00 that moved; the conversion and the waiver are listed apart as "Converted to shares" and
// "Waived". The movement in equity shows S$1,200.00 of shares issued for non-cash consideration, none for cash, and
// the S$300.00 waived among the other movements. Mei Lin is also owed a director's fee of S$800.00 on 2120. The
// related-party schedule at 31 Oct: Ah Kow's loan −500.00 (he owes it); Mei Lin's loan 500.00, deferred pay and fees
// for the year 800.00, converted 1,200.00 and waived 300.00; the year from 1 Jan 2026, the financial years not being
// set; and as CSV.
// req: FR-EQ-10
// req: FR-EQ-11
// req: FR-RPT-01
// req: QA-RPT-01
func TestTheFoundersInTheStatements(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	loan, deferred := e.directorChart()
	e.advancesAccount()
	class := e.shareCapitalIn(e.books.Org)
	capital := e.capitalContribution()
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "6050", Name: "Directors' fees", Type: storage.Expense})
		return err
	})
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		for code, at := range map[string]storage.AccountAttributes{
			e.books.Bank.Code: {ReportGroup: "Cash & bank", CashFlowLine: "Cash & cash equivalents"},
			"1250":            {ReportGroup: "Staff & director advances", CashFlowLine: "Other current assets"},
			"2500":            {ReportGroup: "Director loans", CashFlowLine: "Borrowings"},
			"3010":            {ReportGroup: "Share capital", CashFlowLine: "Share capital"},
			"3300":            {ReportGroup: "Capital contribution", CashFlowLine: "Dividends & other equity"},
			"2120":            {ReportGroup: "Deferred pay", CashFlowLine: "Payroll liabilities"},
			"6050":            {ReportGroup: "Directors' fees", Function: "G&A"},
		} {
			a, err := u.Accounts().ByCode(ctx, code)
			if err != nil {
				return err
			}
			if _, err := u.Accounts().Update(ctx, a.ID, a.Version, storage.AccountChange{Name: a.Name, Active: true, AccountAttributes: at}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	bank := e.books.Bank.ID.String()
	var fees string
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		a, err := u.Accounts().ByCode(ctx, "6050")
		fees = a.ID.String()
		return err
	})
	kow, mei := e.directorContact("Ah Kow"), e.directorContact("Mei Lin")
	for _, j := range []struct {
		on    string
		lines [2]map[string]any
	}{
		{"2026-10-01", [2]map[string]any{founderLine(bank, "1000.00", "", ""), founderLine(loan, "-1000.00", kow, ledger.OriginLent)}},
		{"2026-10-02", [2]map[string]any{founderLine(loan, "1500.00", kow, ledger.OriginPaidOut), founderLine(bank, "-1500.00", "", "")}},
		{"2026-10-03", [2]map[string]any{founderLine(bank, "2000.00", "", ""), founderLine(loan, "-2000.00", mei, ledger.OriginLent)}},
		{"2026-10-04", [2]map[string]any{founderLine(fees, "800.00", "", ""), founderLine(deferred, "-800.00", mei, ledger.OriginFee)}},
	} {
		if r := e.postFounderJournal(j.on, j.lines[0], j.lines[1]); r.Status != http.StatusOK {
			t.Fatalf("posting on %s: %d %s", j.on, r.Status, r.Raw)
		}
	}

	weiling := c.as(c.weiling, authz.Owner, authz.Controller)
	settle := func(body map[string]any, evidence ...string) string {
		t.Helper()
		d := e.call(http.MethodPost, "/v1/director-settlements", weiling, body)
		if d.Status != http.StatusCreated {
			t.Fatalf("drafting: %d %s", d.Status, d.Raw)
		}
		path := "/v1/director-settlements/" + d.str("id")
		for _, kind := range evidence {
			g := e.call(http.MethodGet, path, weiling, nil)
			if f := e.upload(path+"/evidence", weiling, map[string]string{"evidence_type": kind}, "doc.pdf", []byte("%PDF-1.7 "+kind), "If-Match", g.Header.Get("ETag")); f.Status != http.StatusCreated {
				t.Fatalf("attaching the %s: %d %s", kind, f.Status, f.Raw)
			}
		}
		asked := e.call(http.MethodPost, path+"/approval", weiling, nil, "If-Match", d.Header.Get("ETag"))
		if a := e.call(http.MethodPost, "/v1/approvals/"+asked.str("id")+"/approve", c.as(c.ahkow, authz.Owner), hashOf(asked), "If-Match", `"1"`); a.Status != http.StatusOK {
			t.Fatalf("approving: %d %s", a.Status, a.Raw)
		}
		return path
	}
	sgd := func(a string) map[string]any { return map[string]any{"amount": a, "currency": "SGD"} }
	settle(map[string]any{"contact_id": mei, "account_id": loan, "amount": sgd("300.00"), "route": "waive",
		"waiver": map[string]any{"credit_account_id": capital, "capacity": "shareholder", "reason": "Waived ahead of the seed round"}}, "deed of waiver")
	path := settle(map[string]any{"contact_id": mei, "account_id": loan, "amount": sgd("1200.00"), "route": "convert",
		"conversion": map[string]any{"class_account_id": class, "price": "1.20", "capacity": "shareholder"}},
		"shareholders' approval", "board resolution", "conversion agreement", "balance confirmation")
	g := e.call(http.MethodGet, path, weiling, nil)
	if done := e.call(http.MethodPost, path+"/effective", weiling, map[string]any{"lodgement_ref": "AA2026101500777", "resolution_refs": "Written resolution of 15 Oct 2026",
		"effective_on": "2026-10-15"}, "If-Match", g.Header.Get("ETag")); done.Status != http.StatusOK || done.str("share_issue", "shares") != "1000" {
		t.Fatalf("recording the conversion effective: %d %s", done.Status, done.Raw)
	}

	bs := e.call(http.MethodGet, "/v1/reports/statements/bs?to=2026-10", e.as(authz.Accountant), nil)
	for _, want := range []string{
		`"name":"Staff & director advances","totals":[{"amount":"500.00","currency":"SGD"}]`,
		`"name":"Director loans","totals":[{"amount":"500.00","currency":"SGD"}]`,
		`"name":"Share capital","totals":[{"amount":"1200.00","currency":"SGD"}]`,
		`"name":"Capital contribution","totals":[{"amount":"300.00","currency":"SGD"}]`,
		`"name":"Shares issued for cash","totals":[{"amount":"0.00","currency":"SGD"}]`,
		`"name":"Shares issued for non-cash consideration","totals":[{"amount":"1200.00","currency":"SGD"}]`,
		`"name":"Other movements","totals":[{"amount":"300.00","currency":"SGD"}]`,
	} {
		if !strings.Contains(bs.Raw, want) {
			t.Errorf("31 October's balance sheet lacks %s: %s", want, bs.Raw)
		}
	}
	if bs.Status != 200 || strings.Contains(bs.Raw, `"ok":false`) {
		t.Errorf("31 October's balance sheet, its checks: %d %s", bs.Status, bs.Raw)
	}
	cf := e.call(http.MethodGet, "/v1/reports/statements/cf?from=2026-10&to=2026-10", e.as(authz.Accountant), nil)
	for _, want := range []string{`"label":"Waived"`, `"label":"Converted to shares"`, `"name":"Borrowings","totals":[{"amount":"1500.00","currency":"SGD"}]`} {
		if !strings.Contains(cf.Raw, want) {
			t.Errorf("October's cash flow lacks %s: %s", want, cf.Raw)
		}
	}
	if strings.Contains(cf.Raw, `"name":"Shares issued"`) || strings.Contains(cf.Raw, `"ok":false`) {
		t.Errorf("October's cash flow shows the conversion as shares issued for cash, or a failed check: %s", cf.Raw)
	}

	rp := e.call(http.MethodGet, "/v1/director-accounts/related-parties?as_at=2026-10-31", e.as(authz.Auditor), nil)
	if rp.Status != 200 || rp.str("year_from") != "2026-01-01" || len(rp.Body["rows"].([]any)) != 2 {
		t.Fatalf("the related-party schedule: %d %s", rp.Status, rp.Raw)
	}
	figures := func(i int) map[string]any {
		return rp.Body["rows"].([]any)[i].(map[string]any)["figures"].(map[string]any)
	}
	amount := func(f map[string]any, k string) string { return f[k].(map[string]any)["amount"].(string) }
	for i, want := range []map[string]string{
		{"loan": "-500.00", "deferred_pay": "0.00", "fees_for_year": "0.00", "converted": "0.00", "waived": "0.00"},
		{"loan": "500.00", "deferred_pay": "800.00", "fees_for_year": "800.00", "converted": "1200.00", "waived": "300.00"},
	} {
		for k, v := range want {
			if got := amount(figures(i), k); got != v {
				t.Errorf("row %d's %s: %s, want %s: %s", i, k, got, v, rp.Raw)
			}
		}
	}
	if tot := rp.Body["totals"].(map[string]any); amount(tot, "loan") != "0.00" || amount(tot, "converted") != "1200.00" {
		t.Errorf("the schedule's totals: %s", rp.Raw)
	}
	x := e.call(http.MethodGet, "/v1/director-accounts/related-parties/export?as_at=2026-10-31", e.as(authz.Accountant), nil)
	content, _ := base64.StdEncoding.DecodeString(x.str("content_base64"))
	if x.Status != 200 || x.str("file_name") != "related-parties-2026-10-31.csv" ||
		!strings.Contains(string(content), "\"Mei Lin\",\"Yes\",\"Yes\",500.00,800.00,800.00,1200.00,300.00\r\n") ||
		!strings.Contains(string(content), "\"Ah Kow\",\"Yes\",\"Yes\",-500.00,0.00,0.00,0.00,0.00\r\n") {
		t.Errorf("the schedule as CSV: %d %s\n%s", x.Status, x.Raw, content)
	}
	if trail := auditTrail(t, e); !strings.Contains(trail, `"report":"related_parties"`) {
		t.Errorf("the export is not audited: %s", trail)
	}
}
