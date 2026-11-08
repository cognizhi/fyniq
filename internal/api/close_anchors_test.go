//go:build local

package api

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/domain/closing"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A close records the books and the trail as they stood (AUD-04, PRD-001 rev 0.234): its snapshot holds the trial
// balance, every control account's subledger totals and the audit chain's head, read under the ledger lock (ENG-47);
// the hourly anchoring job signs it and writes it once to the write-once bucket, and a reopening's snapshot after the
// same way. Verification checks each anchor against the stored snapshot and the chain, so a changed anchor fails.
// req: AUD-04
// req: ENG-47
// req: CTL-AUD-02
func TestClosesAreSignedAndAnchored(t *testing.T) {
	ctx := context.Background()
	f := newFounders(t)
	e := f.e
	ahkow, weiling := f.as(f.ahkow, authz.Controller), f.as(f.weiling, authz.Owner, authz.Controller)
	path := func(id, op string) string { return "/v1/periods/" + id + "/" + op }
	etagOf := func(id string) string {
		return e.call(http.MethodGet, path(id, "close"), ahkow, nil).Header.Get("ETag")
	}
	oct := e.books.Oct.ID.String()

	e.issuedInvoice("2", "500.00")
	e.postedBill("300.00", "27.00")
	e.postedJournal("100.00")
	checklistDone(t, e.store, e.books.Org, e.books.Oct.ID)
	c := e.call(http.MethodPost, path(oct, "close-checks"), ahkow, nil, "If-Match", etagOf(oct))
	if r := e.call(http.MethodPost, path(oct, "close"), weiling, map[string]any{"checks_id": c.str("checks", "id"), "statement": "Reviewed."},
		"If-Match", etagOf(oct)); r.Status != 200 {
		t.Fatalf("closing October: %d %s", r.Status, r.Raw)
	}

	// The snapshot: version 2, with the receivables' and payables' subledger totals and the chain's head.
	var closed storage.PeriodClose
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		closed, err = u.Closes().Latest(ctx, e.books.Oct.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var snap closing.Snapshot
	if err := json.Unmarshal(closed.Snapshot, &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Schema != closing.SnapshotSchema || snap.Revision != closed.ClosedRevision || snap.Chain.Seq == 0 {
		t.Fatalf("the snapshot: %s", closed.Snapshot)
	}
	sub := map[string]string{}
	for _, x := range snap.Subledgers {
		sub[x.Code+" "+x.Source] = x.Base
	}
	// The invoice is 2 × 500.00 with no GST (the organisation is not registered): 1,000.00 owed; the bill 300.00 plus its
	// 27.00 of GST: 327.00 owing, a credit.
	if sub["1100 invoice"] != "1000.00" || sub["2010 bill"] != "-327.00" {
		t.Errorf("the subledger totals: %v", snap.Subledgers)
	}
	worker := workerStore(t)
	var stored []storage.AuditEvent
	if err := worker.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		stored, err = u.Audit().Events(ctx, snap.Chain.Seq-1, 1)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || hex.EncodeToString(stored[0].Hash) != snap.Chain.Hash {
		t.Errorf("the chain's head in the snapshot is event %d's hash: %v", snap.Chain.Seq, stored)
	}

	// Anchored once, by the hourly job; verification checks it.
	dir := t.TempDir()
	blobs, err := local.NewDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := local.NewEd25519(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	anchor := func() []string {
		t.Helper()
		if _, err := audit.AnchorAll(ctx, worker, blobs, signer, e.clock); err != nil {
			t.Fatal(err)
		}
		keys, err := blobs.List(ctx, audit.CloseAnchorPrefix(e.books.Org))
		if err != nil {
			t.Fatal(err)
		}
		return keys
	}
	verified := func() audit.Report {
		t.Helper()
		reports, _ := audit.VerifyAll(ctx, worker, blobs, signer, nil)
		for _, r := range reports {
			if r.Organisation == e.books.Org {
				return r
			}
		}
		t.Fatal("no report for the organisation")
		return audit.Report{}
	}
	if keys := anchor(); len(keys) != 1 || keys[0] != audit.CloseAnchorKey(e.books.Org, "close", closed.ID) {
		t.Fatalf("the close anchors: %v", keys)
	}
	if keys := anchor(); len(keys) != 1 {
		t.Errorf("a close is anchored once: %v", keys)
	}
	if r := verified(); !r.OK() || r.Closes != 1 {
		t.Fatalf("verification with the close anchor: %+v", r)
	}

	// A reopening's snapshot after is anchored the same way.
	ask := e.call(http.MethodPost, path(oct, "reopen"), ahkow, map[string]any{"reason": "A supplier's October invoice arrived late"}, "If-Match", etagOf(oct))
	view := e.call(http.MethodGet, "/v1/approvals/"+ask.str("id"), weiling, nil)
	if d := f.decide(weiling, ask.str("id"), "approve", hashOf(view)); d.Status != 200 {
		t.Fatalf("reopening: %d %s", d.Status, d.Raw)
	}
	keys := anchor()
	reopened := 0
	for _, k := range keys {
		if strings.Contains(k, "/reopen-") {
			reopened++
		}
	}
	if len(keys) != 2 || reopened != 1 {
		t.Fatalf("the close and reopening anchors: %v", keys)
	}
	if r := verified(); !r.OK() || r.Closes != 2 {
		t.Fatalf("verification with both anchors: %+v", r)
	}

	// An anchor changed in the bucket (as only someone past its retention lock could) fails verification.
	file := filepath.Join(dir, filepath.FromSlash(audit.CloseAnchorKey(e.books.Org, "close", closed.ID)))
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var sa audit.SignedCloseAnchor
	if err := json.Unmarshal(raw, &sa); err != nil {
		t.Fatal(err)
	}
	sa.Anchor.Revision++
	forged, _ := json.Marshal(sa)
	if err := os.WriteFile(file, forged, 0o600); err != nil {
		t.Fatal(err)
	}
	if r := verified(); r.OK() || !strings.Contains(r.Problem, "not signed") {
		t.Errorf("a changed close anchor must fail verification: %+v", r)
	}
}
