//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
)

// The overseas transfers (CMP-PD-06, CTL-DP-07; rev 0.244; p1-pdpa-retention D7): Fyniq's flows abroad, sign-in and
// email, are listed with nothing covering them, and the reviews list says they wait; the DPO records email's transfer,
// which covers it, and revises it; a flow Fyniq does not have, a review before the assessment, a revision for another
// flow and a stale version are refused; the company's own transfer past its review is overdue; a Controller sees none
// of it and an Owner records none.
// req: CMP-PD-06
// req: CTL-DP-07
// req: access/roles-and-segregation Permission matrix: Data protection (PDPA)
func TestTheTransferRegister(t *testing.T) {
	e := newEnv(t)
	dpo := e.as(authz.DPO)
	register := func() response { return e.call(http.MethodGet, "/v1/compliance/transfers", e.as(authz.Auditor), nil) }
	flow := func(r response, key string) map[string]any {
		for _, x := range r.Body["flows"].([]any) {
			if f := x.(map[string]any); f["key"] == key {
				return f
			}
		}
		t.Fatalf("no flow %s: %s", key, r.Raw)
		return nil
	}
	review := func() map[string]any {
		for _, x := range e.call(http.MethodGet, "/v1/compliance/reviews", e.as(authz.Auditor), nil).Body["items"].([]any) {
			if l := x.(map[string]any); l["kind"] == "transfers" {
				return l
			}
		}
		t.Fatal("no transfers review")
		return nil
	}
	r := register()
	if r.Status != 200 || flow(r, "email")["cross_border"] != true || flow(r, "sign-in")["covered_by"] != nil || len(r.Body["transfers"].([]any)) != 0 {
		t.Fatalf("the register at first: %d %s", r.Status, r.Raw)
	}
	if l := review(); l["status"] != "due" || l["waiting"] != float64(2) {
		t.Errorf("the reviews' transfers line at first: %v", l)
	}

	body := map[string]any{"flow": "email", "recipient": "Zoho Corporation (ZeptoMail)", "countries": "United States, India",
		"data": "Recipients' names and email addresses, and the messages", "purpose": "To deliver the company's email", "basis": "contract",
		"assessed_on": "2026-10-01", "assessed_by": "Ann Lim, Data Protection Officer", "evidence": "The provider's data processing addendum",
		"next_review_on": "2027-10-01"}
	c := e.call(http.MethodPost, "/v1/compliance/transfers", dpo, body, "Idempotency-Key", "t-1")
	if c.Status != 201 || c.Header.Get("ETag") != `"v1"` || c.str("flow") != "email" || c.Body["review_due"] != false || c.str("state") != "open" {
		t.Fatalf("email's transfer: %d %s", c.Status, c.Raw)
	}
	if f := flow(register(), "email"); f["covered_by"] != c.str("transfer_id") {
		t.Errorf("email is not covered: %v", f)
	}
	with := func(k string, v any) map[string]any {
		b := map[string]any{}
		for key, x := range body {
			b[key] = x
		}
		b[k] = v
		return b
	}
	e.problemIs(e.call(http.MethodPost, "/v1/compliance/transfers", dpo, with("flow", "fax"), "Idempotency-Key", "t-x1"), "createTransfer", 422, CodeInvalid)
	e.problemIs(e.call(http.MethodPost, "/v1/compliance/transfers", dpo, with("next_review_on", "2026-09-01"), "Idempotency-Key", "t-x2"),
		"createTransfer", 422, CodeInvalid)

	path := "/v1/compliance/transfers/" + c.str("transfer_id")
	e.problemIs(e.call(http.MethodPut, path, dpo, with("flow", "sign-in"), "If-Match", `"v1"`, "Idempotency-Key", "t-x3"), "reviseTransfer", 422, CodeInvalid)
	v := e.call(http.MethodPut, path, dpo, with("basis", "certification"), "If-Match", `"v1"`, "Idempotency-Key", "t-2")
	if v.Status != 200 || v.Header.Get("ETag") != `"v2"` || v.str("basis") != "certification" {
		t.Fatalf("revising: %d %s", v.Status, v.Raw)
	}
	e.problemIs(e.call(http.MethodPut, path, dpo, body, "If-Match", `"v1"`, "Idempotency-Key", "t-x4"), "reviseTransfer", 412, CodeStale)

	// The company's own transfer, past its review; sign-in still waits.
	own := e.call(http.MethodPost, "/v1/compliance/transfers", dpo, map[string]any{"recipient": "A payroll bureau", "countries": "Malaysia",
		"data": "Employees' names and pay", "purpose": "To run payroll", "basis": "contract", "assessed_on": "2025-01-01", "assessed_by": "Ann Lim",
		"evidence": "The bureau's contract", "next_review_on": "2026-01-01"}, "Idempotency-Key", "t-3")
	if own.Status != 201 || own.Body["overdue"] != true || own.str("flow") != "" {
		t.Fatalf("the company's own transfer: %d %s", own.Status, own.Raw)
	}
	if l := review(); l["status"] != "overdue" || l["waiting"] != float64(2) || l["overdue"] != float64(1) {
		t.Errorf("the reviews' transfers line: %v", l)
	}
	if trail := auditTrail(t, e); strings.Count(trail, "transfer.saved") < 3 {
		t.Errorf("the transfers are not in the audit trail")
	}
	e.problemIs(e.call(http.MethodGet, "/v1/compliance/transfers", e.as(authz.Controller), nil), "getTransfers", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodPost, "/v1/compliance/transfers", e.as(authz.Owner), body, "Idempotency-Key", "t-o"), "createTransfer", 403, CodeForbidden)
}

// matrixPDTransfers are the authorisation matrix's rows for the overseas transfers (CMP-PD-06, Appendix D "Data
// protection (PDPA)").
var matrixPDTransfers = map[string]func(e *env, tok string) (response, func() bool){
	"getTransfers": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/compliance/transfers", tok, nil), nil
	},
	"createTransfer": func(e *env, tok string) (response, func() bool) {
		was := matrixPDTransferState(e)
		r := e.call(http.MethodPost, "/v1/compliance/transfers", tok, matrixPDTransferBody, "Prefer", "return=preview")
		return r, func() bool { return matrixPDTransferState(e) == was }
	},
	"reviseTransfer": func(e *env, tok string) (response, func() bool) {
		id := matrixPDTransfer(e)
		was := matrixPDTransferState(e)
		r := e.call(http.MethodPut, "/v1/compliance/transfers/"+id, tok, matrixPDTransferBody, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixPDTransferState(e) == was }
	},
}

var (
	matrixPDTransferBody = map[string]any{"recipient": "x", "countries": "y", "data": "z", "purpose": "p", "basis": "contract", "assessed_on": "2026-10-01",
		"assessed_by": "a", "evidence": "e", "next_review_on": "2027-10-01"}
	matrixPDTransferOf = map[*env]string{}
)

// matrixPDTransfer is a transfer recorded once, so the matrix revises one that exists.
func matrixPDTransfer(e *env) string {
	e.t.Helper()
	if id, ok := matrixPDTransferOf[e]; ok {
		return id
	}
	r := e.call(http.MethodPost, "/v1/compliance/transfers", e.as(authz.DPO), matrixPDTransferBody, "Idempotency-Key", "matrix-transfer")
	if r.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's transfer: %d %s", r.Status, r.Raw)
	}
	matrixPDTransferOf[e] = r.str("transfer_id")
	return r.str("transfer_id")
}

func matrixPDTransferState(e *env) string {
	return e.call(http.MethodGet, "/v1/compliance/transfers", e.as(authz.DPO), nil).Raw
}
