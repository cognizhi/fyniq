//go:build local

package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	registry "github.com/cognizhi/fyniq/compliance"
	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/compliance"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// G4-068 (FR-DOC-02, CTL-AUD-03): a contract on an invoice whose revenue is spread over three years is kept until the
// end of the financial year its transactions complete in, plus the retention, in its row and in the locked store; when
// the spread is lengthened, the nightly extension keeps it longer; nothing ever shortens it; and the records check of
// CTL-AUD-03 passes.
// req: G4-068
// req: FR-DOC-02
func TestAContractIsKeptUntilItsTransactionsComplete(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ctx := context.Background()
	acc := f.as(f.ahkow, authz.Accountant, authz.Controller)
	weiling := f.as(f.weiling, authz.Accountant, authz.Controller)
	e.customer()
	d := e.invoiceDraft("1", "1800.00")
	d["lines"].([]map[string]any)[0]["spread_months"], d["lines"].([]map[string]any)[0]["spread_from"] = 36, "2026-10-01"
	r := e.call(http.MethodPost, "/v1/invoices", acc, d)
	if r.Status != http.StatusCreated {
		t.Fatalf("the spread invoice: %d %s", r.Status, r.Raw)
	}
	id, tag := r.str("id"), r.Header.Get("ETag")
	path := "/v1/invoices/" + id + "/evidence"
	contract := e.upload(path, acc, map[string]string{"evidence_type": "contract"}, "contract.pdf", []byte("%PDF-1.7 a three-year subscription"), "If-Match", tag)
	if contract.Status != http.StatusCreated {
		t.Fatalf("the contract: %d %s", contract.Status, contract.Raw)
	}
	tag = e.call(http.MethodGet, "/v1/invoices/"+id, acc, nil).Header.Get("ETag")
	if p := e.call(http.MethodPost, "/v1/invoices/"+id+"/issue", acc, nil, "If-Match", tag); p.Status != http.StatusOK {
		t.Fatalf("issuing: %d %s", p.Status, p.Raw)
	}

	locked, err := local.NewDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	locked.Clock = e.clock
	ws := workerStore(t)
	c := evidence.Committer{Store: ws, Staging: e.blobs, Locked: locked, Clock: e.clock, Zone: clock.Singapore}
	if n, err := c.Commit(ctx, e.books.Org); err != nil || n != 1 {
		t.Fatalf("Commit = %d, %v", n, err)
	}
	var fyStart int
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := u.Organisation(ctx)
		fyStart = o.Profile.FYStartMonth
		return err
	}); err != nil {
		t.Fatal(err)
	}
	kept := func() (time.Time, time.Time) {
		t.Helper()
		g := e.call(http.MethodGet, path+"/"+contract.str("id"), acc, nil)
		row, err := time.Parse(time.RFC3339, g.str("retain_until"))
		if err != nil {
			t.Fatalf("the contract's retain-until: %s", g.Raw)
		}
		b, err := locked.RetentionOf(ctx, evidence.LockedKey(e.books.Org, uuid.MustParse(contract.str("id"))))
		if err != nil {
			t.Fatal(err)
		}
		return row, b.RetainUntil
	}
	// Its transactions complete in September 2029, the spread's last month: kept until that year's end plus 7 years.
	want := evidence.RetainUntil(fyStart, 7, time.Date(2029, 9, 30, 0, 0, 0, 0, clock.Singapore), clock.Singapore)
	if row, blob := kept(); !row.Equal(want) || blob.Before(want) {
		t.Fatalf("the contract is kept until %s (the store: %s); want %s", row, blob, want)
	}
	if n, err := c.Extend(ctx, e.books.Org); err != nil || n != 0 {
		t.Fatalf("Extend with nothing changed = %d, %v", n, err)
	}

	// The contract runs a year longer: its record now completes in September 2030, and the nightly extension keeps it so.
	if err := ws.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Get(ctx, uuid.MustParse(id))
		if err != nil {
			return err
		}
		if c := evidence.Completion(doc); !c.Equal(time.Date(2029, 9, 30, 0, 0, 0, 0, time.UTC)) {
			t.Errorf("the record completes on %s", c)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	longer := e.invoiceEdit("1", "1800.00", "The customer renewed for a fourth year")
	longer["lines"].([]map[string]any)[0]["spread_months"], longer["lines"].([]map[string]any)[0]["spread_from"] = 48, "2026-10-01"
	tag = e.call(http.MethodGet, "/v1/invoices/"+id, acc, nil).Header.Get("ETag")
	asked := e.call(http.MethodPost, "/v1/invoices/"+id+"/edit-approval", acc, longer, "If-Match", tag)
	if a := f.decide(weiling, asked.str("id"), "approve", hashOf(asked)); a.Status != http.StatusOK || a.str("status") != "approved" {
		t.Fatalf("lengthening the contract's spread: %d %s / %d %s", asked.Status, asked.Raw, a.Status, a.Raw)
	}
	if n, err := c.Extend(ctx, e.books.Org); err != nil || n != 1 {
		t.Fatalf("Extend = %d, %v; want the contract", n, err)
	}
	later := evidence.RetainUntil(fyStart, 7, time.Date(2030, 9, 30, 0, 0, 0, 0, clock.Singapore), clock.Singapore)
	if row, blob := kept(); !row.Equal(later) || blob.Before(later) {
		t.Fatalf("after the extension the contract is kept until %s (the store: %s); want %s", row, blob, later)
	}

	// Never shortened: neither its row nor its locked copy takes an earlier date.
	cid := uuid.MustParse(contract.str("id"))
	if err := ws.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Evidence().Extend(ctx, cid, want)
		return err
	}); err == nil {
		t.Error("the contract's row took an earlier retain-until")
	}
	if err := locked.SetRetainUntil(ctx, evidence.LockedKey(e.books.Org, cid), want); !errors.Is(err, ports.ErrRetentionShortened) {
		t.Errorf("the locked copy took an earlier retain-until: %v", err)
	}

	// CTL-AUD-03's records check passes: the committed file is locked at least until its record's retain-until.
	controls, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	rep, err := compliance.Runner{Store: ws, Clock: e.clock, Controls: controls, Evidence: locked}.Verify(ctx, e.books.Org)
	i := slices.IndexFunc(rep.Runs, func(r storage.ControlRun) bool {
		return r.Control == "CTL-AUD-03" && r.Check == "verify records.retention"
	})
	if err != nil || i < 0 || !rep.Runs[i].OK {
		t.Fatalf("CTL-AUD-03's records check: %v %+v", err, rep.Runs)
	}
}
