//go:build local

package api

import (
	"net/http"
	"reflect"
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

// A filing's acknowledgement is evidence like any other (FR-DOC-10, FR-DOC-01): the search lists it under "filing" for
// whoever views the statutory calendar, and it opens through a download link as a bill's file does; the Bookkeeper, who
// does not view the calendar, finds none and opens none. Every kind of record a search covers is one its contract names,
// on the record found and in the filter.
// req: FR-DOC-10
// req: FR-DOC-01
// asvs: V8.2.2
func TestAFilingsAcknowledgementIsFoundAndOpens(t *testing.T) {
	e := newEnv(t)
	owner := e.as(authz.Owner)
	d := e.call(http.MethodPost, "/v1/compliance/filings", owner, map[string]any{"key": "eci:2026-12-31", "outcome": "filed", "filed_on": "2026-10-14",
		"reference": "ECI-2027-000123"})
	if d.Status != http.StatusCreated {
		t.Fatalf("the filing record: %d %s", d.Status, d.Raw)
	}
	up := e.upload("/v1/compliance/filings/"+d.str("id")+"/evidence", owner, map[string]string{"evidence_type": "IRAS acknowledgement"}, "ack.pdf",
		[]byte("%PDF-1.7 acknowledgement"), "If-Match", d.Header.Get("ETag"))
	if up.Status != http.StatusCreated {
		t.Fatalf("the acknowledgement: %d %s", up.Status, up.Raw)
	}
	auditor := e.as(authz.Auditor)
	r := e.call(http.MethodGet, "/v1/evidence?activity=filing", auditor, nil)
	if r.Status != http.StatusOK || len(r.Body["items"].([]any)) != 1 || !strings.Contains(r.Raw, `"activity":"filing"`) ||
		!strings.Contains(r.Raw, `"type":"statutory_filing"`) || !strings.Contains(r.Raw, `"filing"`) {
		t.Fatalf("the filings' files: %d %s", r.Status, r.Raw)
	}
	if l := e.call(http.MethodPost, "/v1/evidence/"+up.str("id")+"/download", auditor, nil); l.Status != http.StatusOK || l.str("file_name") != "ack.pdf" {
		t.Errorf("the Auditor opening the acknowledgement: %d %s", l.Status, l.Raw)
	}
	bk := e.as(authz.Bookkeeper)
	if r := e.call(http.MethodGet, "/v1/evidence?activity=filing", bk, nil); r.Status != http.StatusOK || len(r.Body["items"].([]any)) != 0 {
		t.Errorf("the Bookkeeper finds a filing's file: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/evidence/"+up.str("id")+"/download", bk, nil), "createEvidenceDownload", 404, CodeNotFound)

	var keys []string
	for _, a := range EvidenceActivities {
		keys = append(keys, a.Key)
	}
	want := strings.Join(keys, ",")
	for _, f := range []struct {
		of    any
		field string
	}{{EvidenceRecord{}, "Activity"}, {searchEvidenceInput{}, "Activity"}} {
		sf, _ := reflect.TypeOf(f.of).FieldByName(f.field)
		if got := sf.Tag.Get("enum"); got != want {
			t.Errorf("%T.%s names %s; the search covers %s", f.of, f.field, got, want)
		}
	}
}
