//go:build local

package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/compliance"
	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The nightly comparison of the balances with the journal lines (CTL-FIN-01) finds drift and records
// it in the organisation's audit trail; the night's evidence records each scheduled check's latest run.
// req: INV-08
// req: CTL-FIN-01
func TestNightlyLedgerVerificationAndControlEvidence(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Nightly Checks Pte Ltd")
	api, worker := store(t, "fyniq_api"), store(t, "fyniq_worker")
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Ledger().Append(ctx, porttest.Sale("nightly-1", porttest.Day(10, 14), b, "250.00"))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	differencesOf := func(results []balance.Verification) int {
		for _, v := range results {
			if v.Organisation == b.Org {
				return v.Differences
			}
		}
		t.Fatal("the verification skipped the organisation")
		return 0
	}
	results, _ := balance.VerifyAll(ctx, worker) // other organisations of the shared database are not this test's
	if n := differencesOf(results); n != 0 {
		t.Fatalf("clean books: %d differences", n)
	}
	if err := asOrg(ctx, conn(t, "fyniq_migrator"), b.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE fyniq.balance SET base_amount = base_amount + 0.01 WHERE account_id = $1", b.Bank.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	results, err := balance.VerifyAll(ctx, worker)
	if err == nil || differencesOf(results) == 0 {
		t.Fatalf("a corrupted cache went unnoticed: %v", err)
	}
	found := false
	if err := store(t, "fyniq_readonly").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		events, err := u.Audit().Events(ctx, 0, 1000)
		for _, e := range events {
			found = found || (strings.Contains(e.Body, balance.ActionCheckFailed) && strings.Contains(e.Body, "CTL-FIN-01"))
		}
		return err
	}); err != nil || !found {
		t.Fatalf("no control.check.failed event in the organisation's trail (%v)", err)
	}
	if err := worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Balances().Rebuild(ctx)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// The evidence: after one night's runs, a check whose job succeeded and one whose job failed. The
	// job_slot table is installation-wide, so the checks name jobs of this test's own.
	c := clock.NewFrozen(time.Date(2026, 3, 4, 20, 0, 0, 0, time.UTC))
	slot := time.Date(2026, 3, 4, 18, 30, 0, 0, time.UTC)
	controls := []compliance.Control{
		{ID: "CTL-FIN-01", Checks: []compliance.Check{{Frequency: "N", Status: "running", How: "job evidence_test.ledger"}}},
		{ID: "CTL-AUD-02", Checks: []compliance.Check{{Frequency: "N", Status: "running", How: "job evidence_test.audit"},
			{Frequency: "D", Status: "planned", Arrives: "later"}}},
		{ID: "CTL-IAM-01", Checks: []compliance.Check{{Frequency: "N", Status: "running", How: "job evidence_test.never"}}},
	}
	for job, outcome := range map[string]error{"evidence_test.ledger": nil, "evidence_test.audit": errors.New("planted failure")} {
		if ok, err := api.ClaimSlot(ctx, job, slot, "test", c.Now().Add(time.Hour), c.Now()); err != nil || !ok {
			t.Fatalf("ClaimSlot(%s) = %v, %v", job, ok, err)
		}
		_ = outbox.RunSlot(ctx, worker, c, job, slot, func(context.Context) error { return outcome })
	}
	blobs, err := local.NewDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := compliance.RecordEvidenceFor(ctx, controls, worker, blobs, c.Now())
	if err == nil || !strings.Contains(err.Error(), "CTL-AUD-02") || !strings.Contains(err.Error(), "CTL-IAM-01") || strings.Contains(err.Error(), "CTL-FIN-01") {
		t.Fatalf("RecordEvidenceFor = %v; want CTL-AUD-02 (failed) and CTL-IAM-01 (never ran) failing", err)
	}
	byControl := map[string]compliance.Evidence{}
	for _, e := range evidence {
		byControl[e.Control] = e
	}
	if len(evidence) != 3 || !byControl["CTL-FIN-01"].OK || byControl["CTL-AUD-02"].OK || byControl["CTL-AUD-02"].Detail != "planted failure" ||
		byControl["CTL-IAM-01"].State != "never ran" {
		t.Fatalf("evidence = %+v", evidence)
	}
	rc, err := blobs.Get(ctx, "compliance/evidence/2026-03-04/CTL-FIN-01-N.json")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	var stored compliance.Evidence
	raw, _ := io.ReadAll(rc)
	if err := json.Unmarshal(raw, &stored); err != nil || stored.Job != "evidence_test.ledger" || !stored.OK || !stored.Slot.Equal(slot) {
		t.Fatalf("stored evidence %s: %v", raw, err)
	}
}
