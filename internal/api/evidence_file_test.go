//go:build local

package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// An evidence file opens with its own content (FR-DOC-01; PRD-001 rev 0.188): a bill's invoice, for the Accountant, from
// staging while the bill is a draft and from the locked store once it is posted and committed, checked against its
// hash, audited at each opening, its name only as data. A role that does not view bills, the AI and another
// organisation's file are not found. An Employee opens the receipt of their own claim, never a colleague's; a removed
// file is gone.
// req: FR-DOC-01
// req: FR-DOC-08
// req: documents/attachments-and-evidence Open a file from its record (rev 0.188)
// asvs: V8.2.2, V5.4.1
func TestAnEvidenceFileOpensForWhoeverSeesItsRecord(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	acc := e.as(authz.Accountant)
	open := func(id, tok string) response {
		t.Helper()
		return e.call(http.MethodGet, "/v1/evidence/"+id+"/file", tok, nil)
	}
	content := func(r response) []byte {
		t.Helper()
		b, err := base64.StdEncoding.DecodeString(r.str("content_base64"))
		if err != nil {
			t.Fatalf("the content: %v", err)
		}
		return b
	}
	pdf := []byte("%PDF-1.7 AU-100, Amazon Web Services, S$218.00")
	bill, tag := e.createBill("200.00", "18.00")
	up := e.upload("/v1/bills/"+bill+"/evidence", acc, map[string]string{"evidence_type": "invoice"}, "../../etc/AU-100.pdf", pdf, "If-Match", tag)
	if up.Status != http.StatusCreated {
		t.Fatalf("attaching: %d %s", up.Status, up.Raw)
	}
	file := up.str("id")
	sum := sha256.Sum256(pdf)
	got := open(file, acc)
	if got.Status != http.StatusOK || !bytes.Equal(content(got), pdf) || got.str("sha256") != hex.EncodeToString(sum[:]) ||
		got.str("media_type") != evidence.PDF || got.str("file_name") != "AU-100.pdf" || got.Body["size_bytes"] != float64(len(pdf)) {
		t.Fatalf("opening a draft's file: %d %s", got.Status, got.Raw)
	}
	if c := got.Header.Get("Cache-Control"); c != "no-store" {
		t.Errorf("an opened file may be cached: %q", c)
	}

	// Posted and committed: it opens from the locked store, its staging copy gone.
	if p := e.call(http.MethodPost, "/v1/bills/"+bill+"/post", acc, nil, "If-Match", e.call(http.MethodGet, "/v1/bills/"+bill, acc, nil).Header.Get("ETag")); p.Status != http.StatusOK {
		t.Fatalf("posting the bill: %d %s", p.Status, p.Raw)
	}
	committer := evidence.Committer{Store: workerStore(t), Staging: e.blobs, Locked: e.blobs, Clock: e.clock, Zone: clock.Singapore}
	if n, err := committer.Commit(context.Background(), e.books.Org); err != nil || n != 1 {
		t.Fatalf("committing: %d, %v", n, err)
	}
	var staged string
	if err := e.store.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		f, err := u.Evidence().Get(ctx, uuid.MustParse(file))
		staged = f.StagingKey
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.blobs.Delete(context.Background(), staged); err != nil {
		t.Fatal(err)
	}
	if r := open(file, acc); r.Status != http.StatusOK || !bytes.Equal(content(r), pdf) {
		t.Fatalf("opening a committed file: %d %s", r.Status, r.Raw)
	}
	opened := 0
	for _, ev := range strings.Split(auditTrail(t, e), "\n") {
		if strings.Contains(ev, `"action":"evidence.opened"`) && strings.Contains(ev, file) && strings.Contains(ev, hex.EncodeToString(sum[:])) {
			opened++
		}
	}
	if opened != 2 {
		t.Errorf("%d openings audited with the file's hash; want 2", opened)
	}

	// Who does not see bills, the AI, and an unknown file: not found.
	for _, tok := range []string{e.as(authz.ARSpecialist), c.as(c.mei, authz.Employee)} {
		e.problemIs(open(file, tok), "getEvidenceFile", 404, CodeNotFound)
	}
	if r := open(file, e.as(authz.AI)); r.Status != http.StatusForbidden {
		t.Errorf("the AI opened a file: %d %s", r.Status, r.Raw)
	}
	e.problemIs(open(uuid.NewString(), acc), "getEvidenceFile", 404, CodeNotFound)

	// An Employee's own receipt opens for them, not for a colleague; removed, it is gone.
	travel, _ := e.claimSetup()
	ahkow, mei := c.as(c.ahkow, authz.Employee), c.as(c.mei, authz.Employee)
	claim := e.call(http.MethodPost, "/v1/expense-claims", ahkow, map[string]any{"date": "2026-10-15", "lines": []any{taxi(travel)}})
	if claim.Status != http.StatusCreated {
		t.Fatalf("a claim: %d %s", claim.Status, claim.Raw)
	}
	receipt := e.receipt(claim.str("id"), ahkow, []byte("%PDF-1.7 ComfortDelGro, S$27.30"))
	if r := open(receipt, ahkow); r.Status != http.StatusOK || string(content(r)) != "%PDF-1.7 ComfortDelGro, S$27.30" {
		t.Fatalf("the claimant opening their receipt: %d %s", r.Status, r.Raw)
	}
	e.problemIs(open(receipt, mei), "getEvidenceFile", 404, CodeNotFound)
	if r := e.call(http.MethodDelete, "/v1/expense-claims/"+claim.str("id")+"/evidence/"+receipt, ahkow, nil, "If-Match", "*"); r.Status != http.StatusOK && r.Status != http.StatusNoContent {
		t.Fatalf("removing the receipt: %d %s", r.Status, r.Raw)
	}
	e.problemIs(open(receipt, ahkow), "getEvidenceFile", 404, CodeNotFound)
}
