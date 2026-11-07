//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/banktransaction"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The GST registration monitor through the API (FR-TAX-05, C.1 #37; p1-gst design D3, D4), worked by hand. In October
// 2026 the company, not registered, issues: an SR invoice of 500,000.00; a ZR one of 200,000.00; a ZR one of USD
// 80,000.00 at the day's rate of 1.25, so 100,000.00; one of ES 50,000.00 and OS 10,000.00, which count for nothing; one
// of SR 30,000.00 on an asset account, a sale of a capital asset, which counts for nothing; an SR one of 100,000.00 that
// is voided; and a credit note of SR 20,000.00. A bank receipt of 5,000.00 is coded SR to sales. Taxable supplies so far:
// 500,000.00 + 200,000.00 + 100,000.00 + 100,000.00 − 100,000.00 − 20,000.00 + 5,000.00 = 785,000.00, 78.5% of the
// S$1,000,000.00 threshold, below the warning at 80%, in 7 entries. Another SR invoice of 20,000.00 brings it to
// 805,000.00, 80.5%: near. A forecast of 1,200,000.00 made on 20 Nov 2026 is liable: apply by 20 Dec 2026, registered
// from 20 Jan 2027. In January 2027, with one more SR invoice of 300,000.00 in October, 2026's 1,105,000.00 is more than
// the threshold: apply by 30 Jan 2027, registered from 1 Mar 2027. Before 7 Nov 2026 the pack holds no threshold, and a
// registered organisation is not tested.
// req: FR-TAX-05
// req: C.1 #37
// req: QA-RPT-01
// req: QA-RPT-02
func TestTheGSTRegistrationMonitorThroughTheAPI(t *testing.T) {
	f := newFounders(t)
	e := f.e
	accountant := f.as(f.ahkow, authz.Accountant, authz.Controller)
	weiling := f.as(f.weiling, authz.Accountant, authz.Controller)
	customer := e.customer()
	var asset string
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		a, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "1160", Name: "Disposal clearing", Type: storage.Asset})
		asset = a.ID.String()
		if err != nil {
			return err
		}
		// One person issues the invoices under a raised self-approval threshold: the approval of larger invoices has its own tests.
		_, err = u.Settings().Record(ctx, "approvals.self_approval_threshold", "2000.00", "1000000.00", porttest.Day(10, 1), 1)
		return err
	})

	monitor := func(tok string) response {
		t.Helper()
		r := e.call(http.MethodGet, "/v1/compliance/gst-monitor", tok, nil)
		if r.Status != http.StatusOK {
			t.Fatalf("the monitor: %d %s", r.Status, r.Raw)
		}
		return r
	}
	// Before 7 Nov 2026 the pack holds no threshold: not assessed, never below.
	if r := monitor(accountant); r.str("status") != "not_assessed" || !strings.Contains(r.str("reason"), "no registration threshold in force on 15 Oct 2026") {
		t.Errorf("on 15 Oct 2026: %s", r.Raw)
	}
	wantStatus(t, e.checked("gst.registration-threshold"), "not_assessed")

	line := func(account, code, amount string) map[string]any {
		return map[string]any{"account_id": account, "description": "Platform", "gst_code": code, "quantity": "1", "unit_price": amount}
	}
	sales := e.books.Sales.ID.String()
	issue := func(body map[string]any) (string, string) {
		t.Helper()
		r := e.call(http.MethodPost, "/v1/invoices", accountant, body)
		if r.Status != http.StatusCreated {
			t.Fatalf("a draft: %d %s", r.Status, r.Raw)
		}
		p := e.call(http.MethodPost, "/v1/invoices/"+r.str("id")+"/issue", accountant, nil, "If-Match", r.Header.Get("ETag"))
		if p.Status != http.StatusOK {
			t.Fatalf("issuing it: %d %s", p.Status, p.Raw)
		}
		return r.str("id"), p.Header.Get("ETag")
	}
	invoice := func(currency string, lines ...map[string]any) map[string]any {
		return map[string]any{"customer_id": customer, "date": "2026-10-14", "currency": currency, "lines": lines}
	}
	if r := e.call(http.MethodPost, "/v1/fx/rates", accountant, map[string]any{"kind": "day", "currency": "USD", "on": "2026-10-14", "quote": "0.8",
		"quote_form": "foreign-per-base", "note": "Bank's rate"}); r.Status != http.StatusCreated {
		t.Fatalf("the USD rate: %d %s", r.Status, r.Raw)
	}
	first, _ := issue(invoice("SGD", line(sales, "SR", "500000.00")))
	issue(invoice("SGD", line(sales, "ZR", "200000.00")))
	issue(invoice("USD", line(sales, "ZR", "80000.00")))
	issue(invoice("SGD", line(sales, "ES", "50000.00"), line(sales, "OS", "10000.00")))
	issue(invoice("SGD", line(asset, "SR", "30000.00")))
	voided, tag := issue(invoice("SGD", line(sales, "SR", "100000.00")))
	void := e.call(http.MethodPost, "/v1/invoices/"+voided+"/void-approval", accountant, map[string]any{"date": "2026-10-22", "reason": "Sent twice"}, "If-Match", tag)
	if a := f.decide(weiling, void.str("id"), "approve", hashOf(void)); a.Status != http.StatusOK {
		t.Fatalf("voiding the invoice: %d %s", a.Status, a.Raw)
	}
	credit := invoice("SGD", line(sales, "SR", "20000.00"))
	credit["kind"], credit["date"], credit["invoice_id"] = "credit_note", "2026-10-21", first
	issue(credit)
	_, l := e.bankLine("5000.00")
	if _, err := e.bank().Categorise(e.bankCtx(), e.books.Org, journal.Categorisation{Basis: storage.BasisPerson, At: e.clock.Now(),
		Draft: banktransaction.Draft{LineID: l.ID, Parts: []banktransaction.Part{{AccountID: e.books.Sales.ID, GSTCode: "SR"}}}}); err != nil {
		t.Fatal(err)
	}

	e.clock.Set(time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC))
	r := monitor(accountant)
	if r.str("status") != "below" || r.str("this_year", "taxable_supplies", "amount") != "785000.00" || r.str("share") != "78.5" ||
		r.str("threshold", "amount") != "1000000.00" || r.str("warning_percent") != "80" || r.Body["this_year"].(map[string]any)["entries"] != 7.0 ||
		r.str("this_year", "from") != "2026-01-01" || r.str("this_year", "to") != "2026-11-20" || r.str("last_year", "taxable_supplies", "amount") != "0.00" {
		t.Errorf("below the warning: %s", r.Raw)
	}
	wantStatus(t, e.checked("gst.registration-threshold"), "ok")

	issue(invoice("SGD", line(sales, "SR", "20000.00")))
	r = monitor(accountant)
	if r.str("status") != "near" || r.str("this_year", "taxable_supplies", "amount") != "805000.00" || r.str("share") != "80.5" {
		t.Errorf("near the threshold: %s", r.Raw)
	}
	c := e.checked("gst.registration-threshold")
	wantStatus(t, c, "warning")
	if items := itemsOf(c); len(items) != 1 || items[0]["id"] != "this_year" || !strings.Contains(items[0]["detail"].(string), "80.5% of the S$1000000.00 threshold") {
		t.Errorf("C.1 #37 near: %s", c.Raw)
	}

	// A forecast: an Owner, who approves GST returns, may not prepare one; it needs its evidence to be recorded.
	body := map[string]any{"forecast_on": "2026-11-20", "value": map[string]any{"amount": "1200000.00", "currency": "SGD"},
		"basis": "Signed contracts with Lumen Health and Acme Analytics"}
	e.problemIs(e.call(http.MethodPost, "/v1/gst-forecasts", e.as(authz.Owner), body), "createGSTForecast", 403, CodeForbidden)
	bad := e.call(http.MethodPost, "/v1/gst-forecasts", accountant, map[string]any{"forecast_on": "2026-11-21", "value": map[string]any{"amount": "0.00", "currency": "SGD"}, "basis": " "})
	if bad.Status != 422 || strings.Count(bad.Raw, `"code":"GST-30"`) != 3 {
		t.Errorf("a forecast dated tomorrow, of nothing, without a basis: %d %s", bad.Status, bad.Raw)
	}
	d := e.call(http.MethodPost, "/v1/gst-forecasts", accountant, body)
	if d.Status != http.StatusCreated || d.str("status") != "draft" || d.str("until") != "2027-11-19" || !strings.Contains(d.Raw, `"code":"GST-31"`) {
		t.Fatalf("the draft: %d %s", d.Status, d.Raw)
	}
	id := d.str("id")
	e.problemIs(e.call(http.MethodPost, "/v1/gst-forecasts/"+id+"/record", accountant, nil, "If-Match", d.Header.Get("ETag")), "recordGSTForecast", 422, "GST-31")
	up := e.upload("/v1/gst-forecasts/"+id+"/evidence", accountant, map[string]string{"evidence_type": "Signed contract"}, "contract.pdf",
		[]byte("%PDF-1.7 contract"), "If-Match", d.Header.Get("ETag"))
	if up.Status != http.StatusCreated {
		t.Fatalf("the contract: %d %s", up.Status, up.Raw)
	}
	cur := e.call(http.MethodGet, "/v1/gst-forecasts/"+id, accountant, nil)
	rec := e.call(http.MethodPost, "/v1/gst-forecasts/"+id+"/record", accountant, nil, "If-Match", cur.Header.Get("ETag"))
	if rec.Status != http.StatusOK || rec.str("status") != "recorded" || rec.Body["evidence"] != 1.0 {
		t.Fatalf("recording it: %d %s", rec.Status, rec.Raw)
	}
	e.problemIs(e.call(http.MethodPut, "/v1/gst-forecasts/"+id, accountant, body, "If-Match", rec.Header.Get("ETag")), "replaceGSTForecast", 409, "GST-32")
	r = monitor(e.as(authz.TaxAgent))
	if r.str("status") != "liable" || r.str("forecast", "forecast", "id") != id || r.Body["forecast"].(map[string]any)["exceeded"] != true ||
		r.str("forecast", "apply_by") != "2026-12-20" || r.str("forecast", "registered_from") != "2027-01-20" {
		t.Errorf("liable by the forecast: %s", r.Raw)
	}
	c = e.checked("gst.registration-threshold")
	if items := itemsOf(c); len(items) != 2 || items[1]["id"] != "forecast" || !strings.Contains(items[1]["detail"].(string), "apply to register by 20 Dec 2026; registered from 20 Jan 2027") {
		t.Errorf("C.1 #37 by the forecast: %s", c.Raw)
	}

	// In January 2027, 2026 is more than the threshold.
	issue(invoice("SGD", line(sales, "SR", "300000.00")))
	e.clock.Set(time.Date(2027, 1, 10, 9, 0, 0, 0, time.UTC))
	r = monitor(accountant)
	if r.str("status") != "liable" || r.str("last_year", "taxable_supplies", "amount") != "1105000.00" || r.Body["last_year"].(map[string]any)["exceeded"] != true ||
		r.str("last_year", "apply_by") != "2027-01-30" || r.str("last_year", "registered_from") != "2027-03-01" || r.str("this_year", "taxable_supplies", "amount") != "0.00" {
		t.Errorf("liable by 2026: %s", r.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/gst-forecasts", accountant, nil); len(l.Body["items"].([]any)) != 1 {
		t.Errorf("the forecasts: %s", l.Raw)
	}

	// Registered from 1 Mar 2027, the monitor has nothing to test.
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.GSTRegistrations().Create(ctx, storage.NewGSTRegistration{RegisteredFrom: time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC), GSTNumber: "M90000001X",
			ReturnFrequency: storage.ReturnQuarterly, QuarterStartMonth: 1, AccountingBasis: storage.BasisInvoice, RecordedAt: e.clock.Now()})
		return err
	})
	e.clock.Set(time.Date(2027, 3, 5, 9, 0, 0, 0, time.UTC))
	if r := monitor(accountant); r.str("status") != "registered" || r.str("reason") != "Registered for GST from 1 Mar 2027" || r.str("registration", "gst_number") != "M90000001X" {
		t.Errorf("registered: %s", r.Raw)
	}
	wantStatus(t, e.checked("gst.registration-threshold"), "ok")
}

// gstForecastFixture is the matrix's forecast: a draft with a contract attached.
type gstForecastFixture struct{ forecast, file string }

var matrixGSTForecastOf = map[*env]gstForecastFixture{}

func matrixGSTForecast(e *env) gstForecastFixture {
	e.t.Helper()
	if f, ok := matrixGSTForecastOf[e]; ok {
		return f
	}
	acc := e.as(authz.Accountant)
	d := e.call(http.MethodPost, "/v1/gst-forecasts", acc, map[string]any{"forecast_on": "2026-10-15", "value": map[string]any{"amount": "400000.00", "currency": "SGD"},
		"basis": "A signed contract"})
	up := e.upload("/v1/gst-forecasts/"+d.str("id")+"/evidence", acc, map[string]string{"evidence_type": "Signed contract"}, "contract.pdf",
		[]byte("%PDF-1.7 matrix contract"), "If-Match", d.Header.Get("ETag"))
	if d.Status != http.StatusCreated || up.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's forecast: %d %d %s", d.Status, up.Status, d.Raw)
	}
	out := gstForecastFixture{forecast: d.str("id"), file: up.str("id")}
	matrixGSTForecastOf[e] = out
	return out
}

// matrixGSTForecastState is what a refused call must leave as it was: the draft, its files and the list.
func matrixGSTForecastState(e *env, f gstForecastFixture) string {
	acc := e.as(authz.Accountant)
	var out []string
	for _, p := range []string{"/v1/gst-forecasts/" + f.forecast, "/v1/gst-forecasts/" + f.forecast + "/evidence", "/v1/gst-forecasts"} {
		r := e.call(http.MethodGet, p, acc, nil)
		out = append(out, r.Header.Get("ETag")+r.Raw)
	}
	return strings.Join(out, "|")
}

var matrixGSTForecastBody = map[string]any{"forecast_on": "2026-10-15", "value": map[string]any{"amount": "400000.00", "currency": "SGD"}, "basis": "A signed contract"}

// matrixGSTMonitor are the authorisation matrix's rows for the GST registration monitor (FR-TAX-05).
var matrixGSTMonitor = map[string]func(e *env, tok string) (response, func() bool){
	"getGSTMonitor": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/compliance/gst-monitor", tok, nil), nil
	},
	"listGSTForecasts": func(e *env, tok string) (response, func() bool) {
		matrixGSTForecast(e)
		return e.call(http.MethodGet, "/v1/gst-forecasts", tok, nil), nil
	},
	"createGSTForecast": func(e *env, tok string) (response, func() bool) {
		f := matrixGSTForecast(e)
		was := matrixGSTForecastState(e, f)
		r := e.call(http.MethodPost, "/v1/gst-forecasts", tok, matrixGSTForecastBody, "Prefer", "return=preview")
		return r, func() bool { return matrixGSTForecastState(e, f) == was }
	},
	"getGSTForecast": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/gst-forecasts/"+matrixGSTForecast(e).forecast, tok, nil), nil
	},
	"replaceGSTForecast": func(e *env, tok string) (response, func() bool) {
		f := matrixGSTForecast(e)
		was := matrixGSTForecastState(e, f)
		r := e.call(http.MethodPut, "/v1/gst-forecasts/"+f.forecast, tok, matrixGSTForecastBody, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixGSTForecastState(e, f) == was }
	},
	"recordGSTForecast": func(e *env, tok string) (response, func() bool) {
		f := matrixGSTForecast(e)
		was := matrixGSTForecastState(e, f)
		r := e.call(http.MethodPost, "/v1/gst-forecasts/"+f.forecast+"/record", tok, nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixGSTForecastState(e, f) == was }
	},
	"addGSTForecastEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixGSTForecast(e)
		was := matrixGSTForecastState(e, f)
		r := e.upload("/v1/gst-forecasts/"+f.forecast+"/evidence", tok, map[string]string{"evidence_type": "Signed contract"}, "again.pdf",
			[]byte("%PDF-1.7 again"), "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixGSTForecastState(e, f) == was }
	},
	"listGSTForecastEvidence": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/gst-forecasts/"+matrixGSTForecast(e).forecast+"/evidence", tok, nil), nil
	},
	"getGSTForecastEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixGSTForecast(e)
		return e.call(http.MethodGet, "/v1/gst-forecasts/"+f.forecast+"/evidence/"+f.file, tok, nil), nil
	},
	"removeGSTForecastEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixGSTForecast(e)
		was := matrixGSTForecastState(e, f)
		r := e.call(http.MethodDelete, "/v1/gst-forecasts/"+f.forecast+"/evidence/"+f.file, tok, nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixGSTForecastState(e, f) == was }
	},
}
