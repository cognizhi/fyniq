//go:build local

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The risk register keeps every version and review (migration 000296, FY309): a risk is saved, revised and reviewed,
// each audited on the risk; a version out of order, a review of an earlier version or setting no later date, and a
// version, review or risk changed or deleted are refused; the worker writes none of it; another organisation sees none.
// req: CMP-06
// req: AUD-01
// req: QA-ENG-05
// asvs: V8.2.2
func TestAttackTheRiskRegisterKeepsItsHistory(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Risks A Pte Ltd"), books(t, "Risks B Pte Ltd")
	api := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	reviewer := founder(t, api, a.Org, 1, now)
	fields := storage.RiskFields{Title: "A supplier's bank details are changed by a fraudster", Owner: "controller", Likelihood: "medium",
		Impact: "high", Mitigation: "A second person approves every bank change", ReviewMonths: 3, NextReviewOn: now.AddDate(0, 3, 0).Truncate(24 * time.Hour), State: "open"}
	var risk storage.Risk
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		r, err := u.Compliance().CreateRisk(ctx, storage.NewRisk{RiskFields: fields, SavedBy: "user:" + reviewer.String(), At: now})
		if err != nil {
			return err
		}
		f := fields
		f.Likelihood = "low"
		if r, err = u.Compliance().ReviseRisk(ctx, r.ID, f, "user:"+reviewer.String(), now, 1); err != nil {
			return err
		}
		if _, err := u.Compliance().ReviseRisk(ctx, r.ID, f, "user:"+reviewer.String(), now, 1); storage.CodeOf(err) != storage.CodeVersion {
			t.Errorf("a revision of a stale version: %v", err)
		}
		risk, err = u.Compliance().ReviewRisk(ctx, r.ID, storage.RiskReview{Version: 2, ReviewedBy: "user:" + reviewer.String(), ReviewedAt: now, Note: "Holds",
			NextReviewOn: now.AddDate(0, 3, 0)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if risk.Ref != "R1" || risk.Version != 2 || risk.Likelihood != "low" || risk.LastReview == nil || risk.LastReview.Version != 2 {
		t.Fatalf("the risk: %+v", risk)
	}
	var actions []string
	if err := asOrg(ctx, conn(t, "fyniq_api"), a.Org, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT action FROM fyniq.audit_event WHERE object_type = 'risk' AND object_id = $1 ORDER BY seq`, risk.ID)
		if err != nil {
			return err
		}
		actions, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	}); err != nil || len(actions) != 3 || actions[0] != "risk.saved" || actions[1] != "risk.saved" || actions[2] != "risk.reviewed" {
		t.Errorf("the risk's audit events: %v %v", actions, err)
	}
	version := `INSERT INTO fyniq.risk_version (risk_id, version, title, owner_role, likelihood, impact, mitigation, next_review_on, saved_by, saved_at)
	            VALUES ($1, $2, 'x', 'owner', 'low', 'low', 'y', $3, 'user:x', $4)`
	review := `INSERT INTO fyniq.risk_review (risk_id, version, reviewed_by, reviewed_at, next_review_on) VALUES ($1, $2, $3, $4, $5)`
	later := now.AddDate(0, 3, 0)
	for _, c := range []struct {
		what, role, stmt string
		args             []any
		states           []string
	}{
		{"the worker adding a risk", "fyniq_worker", `INSERT INTO fyniq.risk (ref, created_at) VALUES ('R9', now())`, nil, []string{"42501"}},
		{"a version skipping one", "fyniq_api", version, []any{risk.ID, 4, later, now}, []string{"FY309"}},
		{"a review of an earlier version", "fyniq_api", review, []any{risk.ID, 1, "user:" + reviewer.String(), now, later}, []string{"FY309"}},
		{"a review setting no later date", "fyniq_api", review, []any{risk.ID, 2, "user:" + reviewer.String(), now, now.AddDate(0, 0, -1)}, []string{"FY309"}},
		{"a risk's reference that is not R and a number", "fyniq_api", `INSERT INTO fyniq.risk (ref, created_at) VALUES ('Risk 1', now())`, nil, []string{"23514"}},
		{"the API changing a version", "fyniq_api", `UPDATE fyniq.risk_version SET title = 'z' WHERE risk_id = $1`, []any{risk.ID}, []string{"42501"}},
		{"the owner changing a version", "fyniq_migrator", `UPDATE fyniq.risk_version SET title = 'z' WHERE risk_id = $1`, []any{risk.ID}, []string{"FY309"}},
		{"the owner deleting a review", "fyniq_migrator", `DELETE FROM fyniq.risk_review WHERE risk_id = $1`, []any{risk.ID}, []string{"FY309"}},
		{"the owner deleting a risk", "fyniq_migrator", `DELETE FROM fyniq.risk WHERE id = $1`, []any{risk.ID}, []string{"FY060", "FY061"}},
	} {
		err := asOrg(ctx, conn(t, c.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt, c.args...); return err })
		wantState(t, c.what, err, c.states...)
	}
	for org, want := range map[uuid.UUID]int{a.Org: 1, b.Org: 0} {
		if err := api.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			risks, err := u.Compliance().Risks(ctx)
			if err == nil && len(risks) != want {
				t.Errorf("organisation %s sees %d risks, want %d", org, len(risks), want)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
}
