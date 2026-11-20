//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
)

// A bad debt that bore GST is in the relief queue with the GST in the part written off and the dates of the pack: an invoice of 14 Oct 2026 with 0.90 of GST on 10.90, written
// off for 10.00 on 31 Oct 2026, carries 0.83 of GST (10.00 x 0.90 / 10.90 = 0.8257, to the cent), may be claimed from 14 Oct 2027 and until 13 Oct 2031, and on 15 Oct 2026 is not yet
// eligible (FR-AR-11).
// req: FR-AR-11
func TestABadDebtIsInTheReliefQueueWithItsDates(t *testing.T) {
	e := newEnv(t)
	e.invoiceNowReady() // GST-registered from 1 Sep 2026
	if r := e.call(http.MethodGet, "/v1/tax/bad-debt-relief", e.as(authz.TaxAgent), nil); r.Status != 200 || strings.Contains(r.Raw, `"write_off_id"`) {
		t.Fatalf("an empty queue: %d %s", r.Status, r.Raw)
	}
	e.postedWriteOff()
	r := e.call(http.MethodGet, "/v1/tax/bad-debt-relief", e.as(authz.TaxAgent), nil)
	items, _ := r.Body["items"].([]any)
	if r.Status != 200 || len(items) != 1 {
		t.Fatalf("the queue: %d %s", r.Status, r.Raw)
	}
	it := items[0].(map[string]any)
	gst, _ := it["gst"].(map[string]any)
	if it["supply_date"] != "2026-10-14" || it["written_off_on"] != "2026-10-31" || it["eligible_from"] != "2027-10-14" || it["claim_by"] != "2031-10-13" || it["status"] != "not_yet" ||
		gst["amount"] != "0.83" || it["pack"] != "sg-tax-filings@2026.1" {
		t.Errorf("the item is %v", it)
	}
}

// The working paper takes the year's profit and revenue from the ledger and the rest from the packs: an invoice of S$1,000.00 is S$1,000.00 of profit and revenue, so under the partial
// exemption 750.00 is exempt (75% of the first 10,000), 250.00 is taxed at 17% (42.50), YA 2027 has no announced rebate in sg-cit (so none is given) and 42.50 is payable; the form is C-S (Lite), with C-S as the
// option; an adjustment of 400.00 added back makes the profit 1,400.00 (exempt 1,050.00, taxed 350.00, tax 59.50, no rebate). A reader of the books cannot ask for it, and what it
// cannot work it refuses with the field.
// req: FR-TAX-06
func TestTheTaxWorkingPaperIsWorkedFromTheLedger(t *testing.T) {
	e := newEnv(t)
	e.invoiceNowReady()
	e.issuedInvoice("1", "1000.00")
	ctl := e.as(authz.Controller)
	tag := e.call(http.MethodGet, "/v1/organisation/profile", ctl, nil).Header.Get("ETag")
	if p := e.call(http.MethodPatch, "/v1/organisation/profile", ctl, map[string]any{"fy_start_month": 10, "books_start": "2026-10-01", "first_year_end": "2027-09-30"}, "If-Match", tag); p.Status != 200 {
		t.Fatalf("the financial year: %d %s", p.Status, p.Raw)
	}
	tax := e.as(authz.TaxAgent)
	r := e.call(http.MethodPost, "/v1/tax/working-paper", tax, map[string]any{"financial_year_end": "2026-11-30", "scheme": "pte"})
	if r.Status != 200 {
		t.Fatalf("the paper: %d %s", r.Status, r.Raw)
	}
	if r.Body["ya"] != 2027.0 || r.str("revenue") != "1000.00" || r.str("profit_before_tax") != "1000.00" || r.str("exempt") != "750.00" || r.str("normal_chargeable") != "250.00" ||
		r.str("tax_before_rebate") != "42.50" || r.str("rebate") != "0.00" || r.str("tax_payable_rounded") != "42.50" || r.str("form") != "C-S (Lite)" || r.str("optional_form") != "C-S" ||
		r.str("cit_pack") != "sg-cit@2026.1" || r.str("forms_pack") != "sg-tax-filings@2026.1" {
		t.Errorf("the paper is %s", r.Raw)
	}
	r = e.call(http.MethodPost, "/v1/tax/working-paper", tax, map[string]any{"financial_year_end": "2026-11-30", "scheme": "pte",
		"adjustments": []map[string]any{{"kind": "non_deductible", "amount": "400.00", "reason": "Fines", "evidence": true}}})
	if r.Status != 200 || r.str("profit_before_tax") != "1000.00" || r.str("adjustments") != "400.00" || r.str("chargeable_income") != "1400.00" || r.str("exempt") != "1050.00" ||
		r.str("tax_before_rebate") != "59.50" || r.str("rebate") != "0.00" || r.str("tax_payable_rounded") != "59.50" {
		t.Errorf("with the adjustment: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/tax/working-paper", tax, map[string]any{"financial_year_end": "2026-11-30", "scheme": "pte",
		"adjustments": []map[string]any{{"kind": "non_deductible", "amount": "1.00", "reason": " "}}}); r.Status != 422 || !strings.Contains(r.Raw, "adjustments[0].reason") {
		t.Errorf("an adjustment with no reason: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/tax/working-paper", e.as(authz.Auditor), map[string]any{"financial_year_end": "2026-11-30", "scheme": "pte"}), "workTaxPaper", 403, CodeForbidden)
}

// With no pack activated the organisation has no update to accept, and the answer says so.
// req: ENG-33
func TestNoPackUpdateIsOfferedWhileNothingIsActivated(t *testing.T) {
	e := newEnv(t)
	r := e.call(http.MethodGet, "/v1/statutory-packs/updates", e.as(authz.Controller), nil)
	if r.Status != 200 || strings.Contains(r.Raw, `"available"`) {
		t.Fatalf("the updates: %d %s", r.Status, r.Raw)
	}
}

func init() {
	matrixTaxFilings["getBadDebtRelief"] = func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/tax/bad-debt-relief", tok, nil), nil
	}
	matrixTaxFilings["workTaxPaper"] = func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/tax/working-paper", tok, map[string]any{"financial_year_end": "2026-11-30", "scheme": "pte"}, "Prefer", "return=preview"), nil
	}
	matrixTaxFilings["listStatutoryPackUpdates"] = func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/statutory-packs/updates", tok, nil), nil
	}
}

// matrixTaxFilings are the authorisation matrix's rows for the tax filings (FR-AR-11, FR-TAX-06, ENG-33), filled by the init above.
var matrixTaxFilings = map[string]func(e *env, tok string) (response, func() bool){}
