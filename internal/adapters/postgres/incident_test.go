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

// The incident register keeps its rules (migration 000316, FY322): steps come once and in the runbook's order
// (notified after assessed, recovered after contained, reviewed after recovered, nothing after the review); a breach is
// named by the assessment only, and only one of the organisation's own; nothing changes or goes; the worker writes
// none of it; another organisation sees none.
// req: SEC-32
// req: AUD-01
// req: QA-ENG-05
// asvs: V8.2.2
func TestAttackTheIncidentRegisterKeepsItsRules(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Incident A Pte Ltd"), books(t, "Incident B Pte Ltd")
	api := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	dpo := "user:" + founder(t, api, a.Org, 1, now).String()
	var open, done storage.Incident
	var theirs storage.Breach
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		theirs, err = u.Compliance().RecordBreach(ctx, storage.Breach{Title: "Theirs", Description: "d", AwareAt: now, Individuals: 1,
			Classes: []string{"other"}, RecordedBy: "user:x", RecordedAt: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		if open, err = u.Compliance().OpenIncident(ctx, storage.Incident{Title: "Open", Summary: "s", Severity: "high", DetectedAt: now.Add(-time.Hour),
			OpenedBy: dpo, OpenedAt: now}); err != nil {
			return err
		}
		if done, err = u.Compliance().OpenIncident(ctx, storage.Incident{Title: "Done", Summary: "s", Severity: "low", DetectedAt: now, OpenedBy: dpo,
			OpenedAt: now}); err != nil {
			return err
		}
		if err := u.Compliance().StepIncident(ctx, open.ID, storage.IncidentStep{Step: "contained", At: now, By: dpo, Note: "n"}); err != nil {
			return err
		}
		for _, s := range []string{"contained", "recovered", "reviewed"} {
			if err := u.Compliance().StepIncident(ctx, done.ID, storage.IncidentStep{Step: s, At: now, By: dpo, Note: "n"}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	step := `INSERT INTO fyniq.security_incident_step (incident_id, step, at, by_actor, note, breach_id) VALUES ($1, $2, now(), 'user:x', 'n', $3)`
	for _, c := range []struct {
		what, role, stmt string
		args             []any
		states           []string
	}{
		{"the worker opening an incident", "fyniq_worker", `INSERT INTO fyniq.security_incident (title, summary, severity, detected_at, opened_by, opened_at)
		  VALUES ('x', 'y', 'low', now(), 'user:x', now())`, nil, []string{"42501"}},
		{"a severity that is none", "fyniq_api", `INSERT INTO fyniq.security_incident (title, summary, severity, detected_at, opened_by, opened_at)
		  VALUES ('x', 'y', 'sev1', now(), 'user:x', now())`, nil, []string{"23514"}},
		{"an incident seen after it was opened", "fyniq_api", `INSERT INTO fyniq.security_incident (title, summary, severity, detected_at, opened_by, opened_at)
		  VALUES ('x', 'y', 'low', now() + interval '1 hour', 'user:x', now())`, nil, []string{"23514"}},
		{"notifying before the assessment", "fyniq_api", step, []any{open.ID, "notified", nil}, []string{"FY322"}},
		{"reviewing before the recovery", "fyniq_api", step, []any{open.ID, "reviewed", nil}, []string{"FY322"}},
		{"containing twice", "fyniq_api", step, []any{open.ID, "contained", nil}, []string{"FY322"}},
		{"a step after the review", "fyniq_api", step, []any{done.ID, "assessed", nil}, []string{"FY322"}},
		{"a breach on another step", "fyniq_api", step, []any{open.ID, "recovered", theirs.ID}, []string{"23514"}},
		{"another organisation's breach", "fyniq_api", step, []any{open.ID, "assessed", theirs.ID}, []string{"23503"}},
		{"no breach at all", "fyniq_api", step, []any{open.ID, "assessed", uuid.New()}, []string{"23503"}},
		{"the API changing a step", "fyniq_api", `UPDATE fyniq.security_incident_step SET note = 'z'`, nil, []string{"42501"}},
		{"the owner changing an incident", "fyniq_migrator", `UPDATE fyniq.security_incident SET severity = 'low'`, nil, []string{"FY322"}},
		{"the owner deleting a step", "fyniq_migrator", `DELETE FROM fyniq.security_incident_step`, nil, []string{"FY322"}},
		{"the owner emptying the register", "fyniq_migrator", `TRUNCATE fyniq.security_incident_step`, nil, []string{"FY060", "FY061"}},
	} {
		err := asOrg(ctx, conn(t, c.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt, c.args...); return err })
		wantState(t, c.what, err, c.states...)
	}
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.Compliance().Incidents(ctx)
		if err == nil && (len(all) != 2 || len(all[0].Steps) != 1 || all[0].Steps[0].Step != "contained" || len(all[1].Steps) != 3) {
			t.Errorf("the register: %+v", all)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.Compliance().Incidents(ctx)
		if err == nil && len(all) != 0 {
			t.Errorf("another organisation sees %d incidents", len(all))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
