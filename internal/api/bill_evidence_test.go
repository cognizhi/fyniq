//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A bill's evidence (p1-purchases tasks 1.5, 2.2; rev 0.86), as a journal's: the supplier's invoice attached to a draft
// bill is checked, staged, listed and read with its ETag, and comes off again while the bill is a draft; posting the
// bill queues the commit of its files to the locked store in the posting's own transaction, after which a file never
// comes off. A journal's file is not found under a bill, nor a bill's under another bill.
// req: FR-DOC-05
// req: FR-DOC-07
// req: FR-DOC-11
// req: FR-DOC-13
// asvs: V5.2.1
func TestEvidenceOnABill(t *testing.T) {
	e := newEnv(t)
	id, tag := e.createBill("100.00", "9.00")
	path := "/v1/bills/" + id + "/evidence"
	acc := e.as(authz.Accountant)
	invoice := []byte("%PDF-1.7 Atlassian AU-100.00")

	pre := e.upload(path, acc, map[string]string{"evidence_type": "invoice"}, "AU.pdf", invoice, "If-Match", tag, "Prefer", "return=preview")
	if pre.Status != http.StatusOK || pre.Header.Get("Preference-Applied") != "return=preview" {
		t.Fatalf("a preview = %d: %s", pre.Status, pre.Raw)
	}
	r := e.upload(path, acc, map[string]string{"evidence_type": "invoice", "note": "As emailed"}, "AU.pdf", invoice, "If-Match", tag, "Idempotency-Key", "bill-ev-1")
	if r.Status != http.StatusCreated || r.str("bill_id") != id || r.str("media_type") != evidence.PDF || r.str("state") != "attached" ||
		r.Header.Get("Location") != path+"/"+r.str("id") {
		t.Fatalf("addBillEvidence = %d: %s", r.Status, r.Raw)
	}
	if l := e.call(http.MethodGet, path, e.as(authz.Auditor), nil); l.Status != http.StatusOK || len(l.Body["items"].([]any)) != 1 {
		t.Fatalf("listBillEvidence = %d: %s", l.Status, l.Raw)
	}
	if g := e.call(http.MethodGet, path+"/"+r.str("id"), acc, nil); g.Status != http.StatusOK || g.Header.Get("ETag") != `"1"` {
		t.Fatalf("getBillEvidence = %d: %s", g.Status, g.Raw)
	}
	if stale := e.upload(path, acc, map[string]string{"evidence_type": "invoice"}, "AU.pdf", invoice, "If-Match", `"9-draft"`); stale.Status != http.StatusPreconditionFailed {
		t.Errorf("a stale If-Match = %d: %s", stale.Status, stale.Raw)
	}
	if rm := e.call(http.MethodDelete, path+"/"+r.str("id"), acc, nil, "If-Match", `"1"`); rm.Status != http.StatusOK || rm.str("state") != "removed" {
		t.Fatalf("removeBillEvidence = %d: %s", rm.Status, rm.Raw)
	}

	// Posted: its file is committed, and never comes off.
	kept := e.upload(path, acc, map[string]string{"evidence_type": "invoice"}, "AU-2.pdf", []byte("%PDF-1.7 the corrected invoice"), "If-Match", tag)
	if kept.Status != http.StatusCreated {
		t.Fatalf("a second file = %d: %s", kept.Status, kept.Raw)
	}
	drainWork(e.t, e)
	if p := e.call(http.MethodPost, "/v1/bills/"+id+"/post", acc, nil, "If-Match", tag); p.Status != http.StatusOK {
		t.Fatalf("posting = %d: %s", p.Status, p.Raw)
	}
	if kind := nextWork(t, e); kind != evidence.CommitKind {
		t.Fatalf("posting a bill with files queued %q; want %s", kind, evidence.CommitKind)
	}
	if rm := e.call(http.MethodDelete, path+"/"+kept.str("id"), acc, nil, "If-Match", "*"); rm.Status != http.StatusConflict || rm.problemCode() != storage.CodeEvidence {
		t.Errorf("removing from a posted bill = %d: %s", rm.Status, rm.Raw)
	}

	// A journal is not a bill; another bill's file is not this bill's.
	journalID, _ := e.createDraft("10.00")
	e.problemIs(e.call(http.MethodGet, "/v1/bills/"+journalID+"/evidence", acc, nil), "listBillEvidence", 404, bill.CodeNotBill)
	other, _ := e.createBill("20.00", "1.80")
	if g := e.call(http.MethodGet, "/v1/bills/"+other+"/evidence/"+kept.str("id"), acc, nil); g.Status != http.StatusNotFound ||
		!strings.Contains(g.Raw, "not attached to this bill") {
		t.Errorf("another bill's file = %d: %s", g.Status, g.Raw)
	}
}
