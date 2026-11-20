//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
)

var paperBody = map[string]any{"financial_year_end": "2026-11-30", "scheme": "pte", "adjustments": []map[string]any{
	{"kind": "non_deductible", "amount": "400.00", "reason": "Fines"}, {"kind": "capital_allowance_difference", "amount": "-100.00", "reason": "Allowances less depreciation"}}}

// The tax agent keeps a financial year's adjustments with their reasons as a draft that evidence attaches to, then records it with the figures it worked out (FR-TAX-06, FR-DOC-05): an
// invoice of S$1,000.00 is S$1,000.00 of profit, the adjustments net 300.00, so the chargeable income is 1,300.00 (exempt 975.00, taxed 325.00 at 17% is 55.25 and no rebate). A paper with
// adjustments cannot be recorded without evidence (TAXPAPER-02); recorded, it changes no more (TAXPAPER-03) and keeps the figures as they were, a later recorded paper of the year
// supersedes it, and an Auditor reads but cannot keep one.
// req: FR-TAX-06
// req: FR-DOC-05
// req: QA-SEC-03
// asvs: V4.1.3, V8.2.1
func TestATaxWorkingPaperKeepsItsAdjustmentsWithEvidenceAndIsRecorded(t *testing.T) {
	e := newEnv(t)
	e.invoiceNowReady()
	e.issuedInvoice("1", "1000.00")
	ctl := e.as(authz.Controller)
	tag := e.call(http.MethodGet, "/v1/organisation/profile", ctl, nil).Header.Get("ETag")
	if p := e.call(http.MethodPatch, "/v1/organisation/profile", ctl, map[string]any{"fy_start_month": 10, "books_start": "2026-10-01", "first_year_end": "2027-09-30"}, "If-Match", tag); p.Status != 200 {
		t.Fatalf("the financial year: %d %s", p.Status, p.Raw)
	}
	tax := e.as(authz.TaxAgent)
	e.problemIs(e.call(http.MethodPost, "/v1/tax/working-papers", e.as(authz.Auditor), paperBody), "createTaxWorkingPaper", 403, CodeForbidden)
	bad := e.call(http.MethodPost, "/v1/tax/working-papers", tax, map[string]any{"financial_year_end": "2026-11-30", "scheme": "pte",
		"adjustments": []map[string]any{{"kind": "other", "amount": "1.00", "reason": " "}, {"kind": "other", "amount": "2.00", "reason": "  "}}})
	if bad.Status != 422 || strings.Count(bad.Raw, `"code":"TAXPAPER-01"`) != 3 { // the summary and each blank reason
		t.Errorf("a paper with two reasons missing: %d %s", bad.Status, bad.Raw)
	}
	if bad := e.call(http.MethodPost, "/v1/tax/working-papers", tax, map[string]any{"financial_year_end": "2026-11-30", "scheme": "pte",
		"adjustments": []map[string]any{{"kind": "other", "amount": "1.005", "reason": "x"}}}); bad.Status != 422 || !strings.Contains(bad.Raw, "adjustments[0].amount") {
		t.Errorf("an amount in fractions of a cent: %d %s", bad.Status, bad.Raw)
	}
	d := e.call(http.MethodPost, "/v1/tax/working-papers", tax, paperBody)
	if d.Status != http.StatusCreated || d.str("status") != "draft" || nested(d, "figures", "chargeable_income") != "1300.00" || nested(d, "figures", "exempt") != "975.00" ||
		nested(d, "figures", "tax_payable_rounded") != "55.25" || !strings.Contains(d.Raw, `"code":"TAXPAPER-02"`) {
		t.Fatalf("the draft: %d %s", d.Status, d.Raw)
	}
	id := d.str("id")
	e.problemIs(e.call(http.MethodPost, "/v1/tax/working-papers/"+id+"/record", tax, nil, "If-Match", d.Header.Get("ETag")), "recordTaxWorkingPaper", 422, "TAXPAPER-02")
	up := e.upload("/v1/tax/working-papers/"+id+"/evidence", tax, map[string]string{"evidence_type": "Tax computation"}, "fines.pdf", []byte("%PDF-1.7 the fines"), "If-Match", d.Header.Get("ETag"))
	if up.Status != http.StatusCreated {
		t.Fatalf("the evidence: %d %s", up.Status, up.Raw)
	}
	cur := e.call(http.MethodGet, "/v1/tax/working-papers/"+id, tax, nil)
	rec := e.call(http.MethodPost, "/v1/tax/working-papers/"+id+"/record", tax, nil, "If-Match", cur.Header.Get("ETag"))
	if rec.Status != http.StatusOK || rec.str("status") != "recorded" || rec.Body["evidence"] != 1.0 || nested(rec, "figures", "tax_payable_rounded") != "55.25" || rec.Body["recorded_by"] == nil {
		t.Fatalf("recording it: %d %s", rec.Status, rec.Raw)
	}
	e.problemIs(e.call(http.MethodPut, "/v1/tax/working-papers/"+id, tax, paperBody, "If-Match", rec.Header.Get("ETag")), "replaceTaxWorkingPaper", 409, "TAXPAPER-03")
	// the figures are kept as they were signed, whatever the ledger does later
	e.issuedInvoice("1", "500.00")
	if g := e.call(http.MethodGet, "/v1/tax/working-papers/"+id, tax, nil); nested(g, "figures", "revenue") != "1000.00" {
		t.Errorf("the recorded figures: %s", g.Raw)
	}
	// a later paper of the year supersedes it; the list shows the latest recorded first
	body := map[string]any{"financial_year_end": "2026-11-30", "scheme": "pte", "adjustments": []map[string]any{}}
	d2 := e.call(http.MethodPost, "/v1/tax/working-papers", tax, body)
	rec2 := e.call(http.MethodPost, "/v1/tax/working-papers/"+d2.str("id")+"/record", tax, nil, "If-Match", d2.Header.Get("ETag"))
	if d2.Status != http.StatusCreated || rec2.Status != http.StatusOK || rec2.str("status") != "recorded" {
		t.Fatalf("a paper with no adjustments records without evidence: %d %s / %d %s", d2.Status, d2.Raw, rec2.Status, rec2.Raw)
	}
	l := e.call(http.MethodGet, "/v1/tax/working-papers?financial_year_end=2026-11-30", e.as(authz.Auditor), nil)
	items, _ := l.Body["items"].([]any)
	if l.Status != 200 || len(items) != 2 || items[0].(map[string]any)["id"] != d2.str("id") || items[0].(map[string]any)["status"] != "recorded" || items[1].(map[string]any)["status"] != "superseded" {
		t.Errorf("the list: %d %s", l.Status, l.Raw)
	}
	if ev := e.call(http.MethodGet, "/v1/tax/working-papers/"+id+"/evidence", e.as(authz.Auditor), nil); ev.Status != 200 || !strings.Contains(ev.Raw, "fines.pdf") {
		t.Errorf("the files: %d %s", ev.Status, ev.Raw)
	}
}

type taxPaperFixture struct{ paper, file string }

var matrixTaxPaperOf = map[*env]taxPaperFixture{}

// matrixTaxPaper is a drafted paper with one file in e's organisation, made once.
func matrixTaxPaper(e *env) taxPaperFixture {
	e.t.Helper()
	if f, ok := matrixTaxPaperOf[e]; ok {
		return f
	}
	acc := e.as(authz.TaxAgent)
	d := e.call(http.MethodPost, "/v1/tax/working-papers", acc, paperBody)
	up := e.upload("/v1/tax/working-papers/"+d.str("id")+"/evidence", acc, map[string]string{"evidence_type": "Tax computation"}, "matrix.pdf", []byte("%PDF-1.7 matrix"), "If-Match", d.Header.Get("ETag"))
	if d.Status != http.StatusCreated || up.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's paper: %d %d %s %s", d.Status, up.Status, d.Raw, up.Raw)
	}
	f := taxPaperFixture{paper: d.str("id"), file: up.str("id")}
	matrixTaxPaperOf[e] = f
	return f
}

func matrixTaxPaperState(e *env, f taxPaperFixture) string {
	acc := e.as(authz.TaxAgent)
	var out []string
	for _, p := range []string{"/v1/tax/working-papers/" + f.paper, "/v1/tax/working-papers/" + f.paper + "/evidence", "/v1/tax/working-papers"} {
		r := e.call(http.MethodGet, p, acc, nil)
		out = append(out, r.Header.Get("ETag")+r.Raw)
	}
	return strings.Join(out, "|")
}

func init() {
	matrixTaxFilings["listTaxWorkingPapers"] = func(e *env, tok string) (response, func() bool) {
		matrixTaxPaper(e)
		return e.call(http.MethodGet, "/v1/tax/working-papers", tok, nil), nil
	}
	matrixTaxFilings["createTaxWorkingPaper"] = func(e *env, tok string) (response, func() bool) {
		f := matrixTaxPaper(e)
		was := matrixTaxPaperState(e, f)
		r := e.call(http.MethodPost, "/v1/tax/working-papers", tok, paperBody, "Prefer", "return=preview")
		return r, func() bool { return matrixTaxPaperState(e, f) == was }
	}
	matrixTaxFilings["getTaxWorkingPaper"] = func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/tax/working-papers/"+matrixTaxPaper(e).paper, tok, nil), nil
	}
	matrixTaxFilings["replaceTaxWorkingPaper"] = func(e *env, tok string) (response, func() bool) {
		f := matrixTaxPaper(e)
		was := matrixTaxPaperState(e, f)
		r := e.call(http.MethodPut, "/v1/tax/working-papers/"+f.paper, tok, paperBody, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixTaxPaperState(e, f) == was }
	}
	matrixTaxFilings["recordTaxWorkingPaper"] = func(e *env, tok string) (response, func() bool) {
		f := matrixTaxPaper(e)
		was := matrixTaxPaperState(e, f)
		r := e.call(http.MethodPost, "/v1/tax/working-papers/"+f.paper+"/record", tok, nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixTaxPaperState(e, f) == was }
	}
	matrixTaxFilings["addTaxWorkingPaperEvidence"] = func(e *env, tok string) (response, func() bool) {
		f := matrixTaxPaper(e)
		was := matrixTaxPaperState(e, f)
		r := e.upload("/v1/tax/working-papers/"+f.paper+"/evidence", tok, map[string]string{"evidence_type": "Tax computation"}, "again.pdf", []byte("%PDF-1.7 again"), "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixTaxPaperState(e, f) == was }
	}
	matrixTaxFilings["listTaxWorkingPaperEvidence"] = func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/tax/working-papers/"+matrixTaxPaper(e).paper+"/evidence", tok, nil), nil
	}
	matrixTaxFilings["getTaxWorkingPaperEvidence"] = func(e *env, tok string) (response, func() bool) {
		f := matrixTaxPaper(e)
		return e.call(http.MethodGet, "/v1/tax/working-papers/"+f.paper+"/evidence/"+f.file, tok, nil), nil
	}
	matrixTaxFilings["removeTaxWorkingPaperEvidence"] = func(e *env, tok string) (response, func() bool) {
		f := matrixTaxPaper(e)
		was := matrixTaxPaperState(e, f)
		r := e.call(http.MethodDelete, "/v1/tax/working-papers/"+f.paper+"/evidence/"+f.file, tok, nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixTaxPaperState(e, f) == was }
	}
}
