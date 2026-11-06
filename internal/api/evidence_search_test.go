//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
)

// Evidence search (FR-DOC-10; PRD-001 rev 0.184): the files on a bill and a journal, found by activity, evidence type,
// the record's date and uploader, a page at a time; the Data protection officer, who views none of those records,
// finds none of them, and an Employee, whose evidence right covers their own files only, cannot search.
// req: FR-DOC-10
// req: documents/attachments-and-evidence Search the evidence (rev 0.184)
func TestEvidenceIsFoundByActivityTypeDateAndUploader(t *testing.T) {
	e := newEnv(t)
	acc := e.as(authz.Accountant)
	bill, billTag := e.createBill("100.00", "9.00")
	journal, journalTag := e.createDraft("100.00")
	attach := func(path, tag, kind string) {
		t.Helper()
		if r := e.upload(path, acc, map[string]string{"evidence_type": kind}, kind+".pdf", []byte("%PDF-1.7 "+kind+path), "If-Match", tag); r.Status != http.StatusCreated {
			t.Fatalf("attaching %s: %d %s", kind, r.Status, r.Raw)
		}
	}
	attach("/v1/bills/"+bill+"/evidence", billTag, "supplier invoice")
	attach("/v1/journals/"+journal+"/evidence", journalTag, "calculation")
	search := func(tok, query string) response {
		t.Helper()
		r := e.call(http.MethodGet, "/v1/evidence"+query, tok, nil)
		if r.Status != http.StatusOK {
			t.Fatalf("searching %s: %d %s", query, r.Status, r.Raw)
		}
		return r
	}
	ids := func(r response) string {
		var out []string
		for _, x := range r.Body["items"].([]any) {
			out = append(out, x.(map[string]any)["record"].(map[string]any)["id"].(string))
		}
		return strings.Join(out, ",")
	}
	auditor := e.as(authz.Auditor)
	all := search(auditor, "")
	if n := len(all.Body["items"].([]any)); n != 2 || !strings.Contains(all.Raw, `"activity":"bill"`) || !strings.Contains(all.Raw, `"activity":"journal"`) {
		t.Fatalf("every file: %s", all.Raw)
	}
	if got := ids(search(auditor, "?activity=bill")); got != bill {
		t.Errorf("the bills' files: %s", got)
	}
	if got := ids(search(auditor, "?evidence_type=CALC")); got != journal {
		t.Errorf("by evidence type: %s", got)
	}
	if got := ids(search(auditor, "?from=2099-01-01")); got != "" {
		t.Errorf("by date: %s", got)
	}
	if got := ids(search(auditor, "?uploaded_by=user:nobody")); got != "" {
		t.Errorf("by uploader: %s", got)
	}
	first := search(auditor, "?page_size=1")
	next := first.str("next_cursor")
	if len(first.Body["items"].([]any)) != 1 || next == "" {
		t.Fatalf("a page of one: %s", first.Raw)
	}
	if second := search(auditor, "?page_size=1&cursor="+next); ids(second) == ids(first) || len(second.Body["items"].([]any)) != 1 {
		t.Errorf("the next page: %s", second.Raw)
	}
	if dpo := search(e.as(authz.DPO), ""); len(dpo.Body["items"].([]any)) != 0 || strings.Contains(dpo.Raw, `"bill"`) {
		t.Errorf("the DPO sees records they do not view: %s", dpo.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/evidence", e.as(authz.Employee), nil), "searchEvidence", 403, CodeForbidden)
}
