//go:build local

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Impact assessments keep every version and approval (migration 000304, FY313): a version saved and approved, each
// audited; a version out of order, an approval of an earlier version, a key that is not one, a text whose SHA-256 is not
// its own, and anything changed or deleted are refused; the worker writes none of it; another organisation sees none.
// req: CMP-PD-08
// req: AUD-01
// req: QA-ENG-05
// asvs: V8.2.2
func TestAttackImpactAssessmentsKeepTheirHistory(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "DPIA A Pte Ltd"), books(t, "DPIA B Pte Ltd")
	api := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	dpo := "user:" + founder(t, api, a.Org, 1, now).String()
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		for v := 1; v <= 2; v++ {
			if _, err := u.Compliance().SaveDPIA(ctx, storage.DPIAVersion{Key: "payroll", Version: v, Title: "Payroll", Body: "# Payroll\n", From: "organisation",
				SavedBy: dpo, SavedAt: now}); err != nil {
				return err
			}
		}
		return u.Compliance().ApproveDPIA(ctx, storage.DPIAApproval{Key: "payroll", Version: 2, ApprovedBy: dpo, ApprovedAt: now})
	}); err != nil {
		t.Fatal(err)
	}
	var actions []string
	if err := asOrg(ctx, conn(t, "fyniq_api"), a.Org, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT action FROM fyniq.audit_event WHERE object_type IN ('dpia_version', 'dpia_approval') ORDER BY seq`)
		if err != nil {
			return err
		}
		actions, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	}); err != nil || len(actions) != 3 || actions[2] != "dpia.approved" {
		t.Errorf("the assessments' audit events: %v %v", actions, err)
	}
	version := `INSERT INTO fyniq.dpia_version (key, version, title, body, sha256, from_draft, saved_by, saved_at)
	            VALUES ($1, $2, 't', 'b', sha256('b'), 'organisation', 'user:x', now())`
	for _, c := range []struct {
		what, role, stmt string
		args             []any
		states           []string
	}{
		{"the worker saving a version", "fyniq_worker", version, []any{"ai-assistant", 1}, []string{"42501"}},
		{"a version skipping one", "fyniq_api", version, []any{"payroll", 4}, []string{"FY313"}},
		{"a key that is not one", "fyniq_api", version, []any{"hr", 1}, []string{"23514"}},
		{"an approval of an earlier version", "fyniq_api", `INSERT INTO fyniq.dpia_approval (key, version, approved_by, approved_at) VALUES ('payroll', 1, 'user:x', now())`, nil, []string{"FY313"}},
		{"a text whose SHA-256 is not its own", "fyniq_api", `INSERT INTO fyniq.dpia_version (key, version, title, body, sha256, from_draft, saved_by, saved_at)
		  VALUES ('ai-assistant', 1, 't', 'b', sha256('c'), 'organisation', 'user:x', now())`, nil, []string{"23514"}},
		{"the API changing a version", "fyniq_api", `UPDATE fyniq.dpia_version SET title = 'z'`, nil, []string{"42501"}},
		{"the owner changing a version", "fyniq_migrator", `UPDATE fyniq.dpia_version SET title = 'z' WHERE key = 'payroll'`, nil, []string{"FY313"}},
		{"the owner deleting an approval", "fyniq_migrator", `DELETE FROM fyniq.dpia_approval`, nil, []string{"FY313"}},
	} {
		err := asOrg(ctx, conn(t, c.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt, c.args...); return err })
		wantState(t, c.what, err, c.states...)
	}
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		vs, as, err := u.Compliance().DPIAs(ctx)
		if err == nil && (len(vs) != 0 || len(as) != 0) {
			t.Errorf("another organisation sees %d versions and %d approvals", len(vs), len(as))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
