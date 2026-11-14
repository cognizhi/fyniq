//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// setEvidence sets an evidence rule's level in e's organisation from 2026, as the version after the ones before.
func (e *env) setEvidence(key, was, level string, version int) {
	e.t.Helper()
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Settings().Record(ctx, key, was, level, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), version)
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
}

// The evidence rules as shipped (FR-DOC-06; PRD-001 rev 0.182, the owner's decisions of 6 Nov 2026): a bill posts only
// with a file attached, and so does a manual journal whose debits are above S$1,000; one of S$1,000.00 posts without.
// Asking for a post's approval is refused the same way. Once the file is attached, it posts. With the bill's rule
// recommended, a bill posts without a file, and Checks lists it (C.2 #30); with it optional, nothing is listed. With the
// journals' rule recommended, a journal above the threshold posted without a file is listed by its debits.
// req: FR-DOC-06
// req: documents/attachments-and-evidence Default evidence for bills and manual journals (rev 0.182)
// req: C.2 #30
func TestEvidenceRulesHoldBillsAndJournals(t *testing.T) {
	e := newEnv(t)
	e.setEvidence("evidence.bill", "optional", "required", 2)
	e.setEvidence("evidence.manual_journal", "optional", "required", 2)
	acc := e.as(authz.Accountant)
	file := func(path, tag string) {
		t.Helper()
		if r := e.upload(path, acc, map[string]string{"evidence_type": "invoice"}, "doc.pdf", []byte("%PDF-1.7 "+path), "If-Match", tag); r.Status != http.StatusCreated {
			t.Fatalf("attaching to %s: %d %s", path, r.Status, r.Raw)
		}
	}

	// A bill: refused without its invoice, posted with it.
	bill, tag := e.createBill("100.00", "9.00")
	e.problemIs(e.call(http.MethodPost, "/v1/bills/"+bill+"/post", acc, nil, "If-Match", tag), "postBill", 409, evidence.CodeMissing)
	big, bigTag := e.createBill("3000.00", "270.00")
	e.problemIs(e.call(http.MethodPost, "/v1/bills/"+big+"/post-approval", steppedUp(e, acc), nil, "If-Match", bigTag), "requestBillPost", 409, evidence.CodeMissing)
	file("/v1/bills/"+bill+"/evidence", tag)
	if p := e.call(http.MethodPost, "/v1/bills/"+bill+"/post", acc, nil, "If-Match", tag); p.Status != http.StatusOK {
		t.Fatalf("posting the bill with its invoice: %d %s", p.Status, p.Raw)
	}

	// Manual journals: at the threshold none is needed; above it, refused without a file, posted with one.
	if id, tag := e.createDraft("1000.00"); e.call(http.MethodPost, "/v1/journals/"+id+"/post", acc, nil, "If-Match", tag).Status != http.StatusOK {
		t.Fatal("a journal of S$1,000.00 needs no evidence")
	}
	j, jTag := e.createDraft("1500.00")
	r := e.call(http.MethodPost, "/v1/journals/"+j+"/post", acc, nil, "If-Match", jTag)
	e.problemIs(r, "postJournal", 409, evidence.CodeMissing)
	if !strings.Contains(r.str("detail"), "a supporting calculation or document") {
		t.Errorf("the refusal: %s", r.Raw)
	}
	bigJ, bigJTag := e.createDraft("2500.00")
	e.problemIs(e.call(http.MethodPost, "/v1/journals/"+bigJ+"/post-approval", steppedUp(e, acc), nil, "If-Match", bigJTag), "requestJournalPost", 409, evidence.CodeMissing)
	file("/v1/journals/"+j+"/evidence", jTag)
	if p := e.call(http.MethodPost, "/v1/journals/"+j+"/post", acc, nil, "If-Match", jTag); p.Status != http.StatusOK {
		t.Fatalf("posting the journal with its file: %d %s", p.Status, p.Raw)
	}

	// Recommended: posts, and Checks lists it; optional: nothing listed.
	e.setEvidence("evidence.bill", "required", "recommended", 3)
	bare, bareTag := e.createBill("50.00", "4.50")
	if p := e.call(http.MethodPost, "/v1/bills/"+bare+"/post", acc, nil, "If-Match", bareTag); p.Status != http.StatusOK {
		t.Fatalf("a bill whose evidence is recommended: %d %s", p.Status, p.Raw)
	}
	c := e.checked("evidence.missing")
	wantStatus(t, c, "warning")
	if itemNamed(c, bare) == nil || itemNamed(c, bill) != nil {
		t.Errorf("C.2 #30 lists the wrong records: %s", c.Raw)
	}
	e.setEvidence("evidence.bill", "recommended", "optional", 4)
	if c := e.checked("evidence.missing"); itemNamed(c, bare) != nil {
		t.Errorf("C.2 #30 lists a bill whose evidence is optional: %s", c.Raw)
	}

	// With the journals' rule recommended, one above the threshold posts without a file, and Checks lists it by its
	// debits, read with a page of journals' entries at once (p1-operations-dr task 13.13); the one at the threshold and
	// the one with its file are not listed.
	e.setEvidence("evidence.manual_journal", "required", "recommended", 3)
	over, overTag := e.createDraft("1200.00")
	if p := e.call(http.MethodPost, "/v1/journals/"+over+"/post", acc, nil, "If-Match", overTag); p.Status != http.StatusOK {
		t.Fatalf("a journal whose evidence is recommended: %d %s", p.Status, p.Raw)
	}
	c = e.checked("evidence.missing")
	if it := itemNamed(c, over); it == nil || !strings.Contains(it["detail"].(string), "Debits of 1200.00 SGD") || itemNamed(c, j) != nil ||
		len(itemsOf(c)) != 1 {
		t.Errorf("C.2 #30 with journals recommended: %s", c.Raw)
	}
}
