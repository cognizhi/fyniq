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

// The breach register keeps its rules (migration 000306, FY315): the database works out prescribed data and the
// assessment's 30 days; a breach of prescribed data, or of 500 or more people, is never decided not notifiable; the
// PDPC's day is 3 calendar days after a notifiable decision; steps come once and in order; nothing changes or goes; the
// worker writes none of it; another organisation sees none.
// req: CMP-PD-05
// req: CTL-DP-06
// req: AUD-01
// req: QA-ENG-05
// asvs: V8.2.2
func TestAttackTheBreachRegisterKeepsItsRules(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Breach A Pte Ltd"), books(t, "Breach B Pte Ltd")
	api := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	dpo := "user:" + founder(t, api, a.Org, 1, now).String()
	var pay, many, mild storage.Breach
	yes := true
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		if pay, err = u.Compliance().RecordBreach(ctx, storage.Breach{Title: "Pay", Description: "d", AwareAt: now, Individuals: 3,
			Classes: []string{"names", "pay"}, RecordedBy: dpo, RecordedAt: now}); err != nil {
			return err
		}
		if many, err = u.Compliance().RecordBreach(ctx, storage.Breach{Title: "Many", Description: "d", AwareAt: now, Individuals: 500,
			Classes: []string{"contact_details"}, RecordedBy: dpo, RecordedAt: now}); err != nil {
			return err
		}
		if mild, err = u.Compliance().RecordBreach(ctx, storage.Breach{Title: "Mild", Description: "d", AwareAt: now, Individuals: 2,
			Classes: []string{"contact_details"}, RecordedBy: dpo, RecordedAt: now}); err != nil {
			return err
		}
		for _, id := range []storage.Breach{pay, many, mild} {
			if err := u.Compliance().StepBreach(ctx, id.ID, storage.BreachStep{Step: "assessed", At: now, By: dpo}); err != nil {
				return err
			}
		}
		return u.Compliance().StepBreach(ctx, pay.ID, storage.BreachStep{Step: "decided", At: now, By: dpo, Notifiable: &yes})
	}); err != nil {
		t.Fatal(err)
	}
	sgt := time.FixedZone("SGT", 8*3600)
	day := func(t time.Time) time.Time {
		y, m, d := t.In(sgt).Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	if !pay.Prescribed || many.Prescribed || !pay.AssessmentDueOn.Equal(day(now).AddDate(0, 0, 30)) {
		t.Errorf("prescribed and the assessment's day: %+v", pay)
	}
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.Compliance().Breaches(ctx)
		for _, x := range all {
			if x.ID == pay.ID && (len(x.Steps) != 2 || x.Steps[1].PDPCDueOn == nil || !x.Steps[1].PDPCDueOn.Equal(day(now).AddDate(0, 0, 3))) {
				t.Errorf("the PDPC's day: %+v", x.Steps)
			}
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	step := `INSERT INTO fyniq.pd_breach_step (breach_id, step, at, by_actor, notifiable, pdpc_due_on, reference) VALUES ($1, $2, now(), 'user:x', $3, $4, $5)`
	for _, c := range []struct {
		what, role, stmt string
		args             []any
		states           []string
	}{
		{"the worker recording a breach", "fyniq_worker", `INSERT INTO fyniq.pd_breach (title, description, aware_at, assessment_due_on, individuals, classes, prescribed, recorded_by, recorded_at)
		  VALUES ('x', 'y', now(), (now() AT TIME ZONE 'Asia/Singapore')::date + 30, 1, ARRAY['other'], false, 'user:x', now())`, nil, []string{"42501"}},
		{"a prescribed flag that is not its data's", "fyniq_api", `INSERT INTO fyniq.pd_breach (title, description, aware_at, assessment_due_on, individuals, classes, prescribed, recorded_by, recorded_at)
		  VALUES ('x', 'y', now(), (now() AT TIME ZONE 'Asia/Singapore')::date + 30, 1, ARRAY['names', 'bank_accounts'], false, 'user:x', now())`, nil, []string{"23514"}},
		{"an assessment's day not 30 days on", "fyniq_api", `INSERT INTO fyniq.pd_breach (title, description, aware_at, assessment_due_on, individuals, classes, prescribed, recorded_by, recorded_at)
		  VALUES ('x', 'y', now(), current_date + 60, 1, ARRAY['other'], false, 'user:x', now())`, nil, []string{"23514"}},
		{"500 people decided not notifiable", "fyniq_api", step, []any{many.ID, "decided", false, nil, nil}, []string{"FY315"}},
		{"a PDPC day not 3 days on", "fyniq_api", step, []any{mild.ID, "decided", true, time.Now().AddDate(0, 0, 10), nil}, []string{"23514"}},
		{"the PDPC notified of a breach not decided", "fyniq_api", step, []any{mild.ID, "pdpc_notified", nil, nil, "DBN-1"}, []string{"FY315"}},
		{"closing a notifiable breach before the report", "fyniq_api", step, []any{pay.ID, "closed", nil, nil, nil}, []string{"FY315"}},
		{"assessing twice", "fyniq_api", step, []any{pay.ID, "assessed", nil, nil, nil}, []string{"FY315"}},
		{"the API changing a step", "fyniq_api", `UPDATE fyniq.pd_breach_step SET note = 'z'`, nil, []string{"42501"}},
		{"the owner deleting a breach's step", "fyniq_migrator", `DELETE FROM fyniq.pd_breach_step`, nil, []string{"FY315"}},
	} {
		err := asOrg(ctx, conn(t, c.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt, c.args...); return err })
		wantState(t, c.what, err, c.states...)
	}
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.Compliance().Breaches(ctx)
		if err == nil && len(all) != 0 {
			t.Errorf("another organisation sees %d breaches", len(all))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
