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

// The heartbeat drill register keeps its rules (migration 000318, FY324): a drill is recorded through the unit of work
// with its audit event, with no alert or escalation when none came, and passes only when both came in time; its
// environment is one of the two and its minutes whole; nothing changes or goes; the worker records none.
// req: NFR-OBS-02
// req: CTL-RES-05
// req: AUD-01
// req: QA-ENG-05
// asvs: V8.2.2
func TestAttackTheHeartbeatDrillRegisterKeepsItsRules(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Heartbeat Drill Pte Ltd")
	api := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	held := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	first, escalation := 28, 58
	var recorded []storage.HeartbeatDrill
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		for _, d := range []storage.HeartbeatDrill{
			{HeldOn: held, Environment: "staging", PausedMinutes: 75, FirstAlertMinutes: &first, EscalationMinutes: &escalation, Findings: "f", RecordedBy: "user:x", RecordedAt: now},
			{HeldOn: held, Environment: "staging", PausedMinutes: 75, FirstAlertMinutes: &first, Findings: "no escalation", RecordedBy: "user:x", RecordedAt: now},
		} {
			r, err := u.Compliance().RecordHeartbeatDrill(ctx, d)
			if err != nil {
				return err
			}
			recorded = append(recorded, r)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !recorded[0].Passed() || recorded[1].Passed() || recorded[1].EscalationMinutes != nil || *recorded[0].FirstAlertMinutes != 28 {
		t.Fatalf("recorded: %+v", recorded)
	}
	insert := `INSERT INTO fyniq.tick_drill (held_on, environment, paused_minutes, first_alert_minutes, escalation_minutes, findings, recorded_by,
	  recorded_at) VALUES (current_date, $1, $2, $3, NULL, 'f', 'user:x', now())`
	for _, c := range []struct {
		what, role, stmt string
		args             []any
		states           []string
	}{
		{"the worker recording a drill", "fyniq_worker", insert, []any{"staging", 60, 30}, []string{"42501"}},
		{"an environment that is none", "fyniq_api", insert, []any{"laptop", 60, 30}, []string{"23514"}},
		{"a tick never paused", "fyniq_api", insert, []any{"staging", 0, 30}, []string{"23514"}},
		{"an alert before the pause", "fyniq_api", insert, []any{"staging", 60, -1}, []string{"23514"}},
		{"a drill with no audit event", "fyniq_api", insert, []any{"staging", 60, 30}, []string{"FY061"}},
		{"the API changing a drill", "fyniq_api", `UPDATE fyniq.tick_drill SET escalation_minutes = 30`, nil, []string{"42501"}},
		{"the owner passing a failed drill", "fyniq_migrator", `UPDATE fyniq.tick_drill SET escalation_minutes = 30`, nil, []string{"FY324"}},
		{"the owner deleting a drill", "fyniq_migrator", `DELETE FROM fyniq.tick_drill`, nil, []string{"FY324"}},
		{"the owner emptying the register", "fyniq_migrator", `TRUNCATE fyniq.tick_drill`, nil, []string{"FY060", "FY061"}},
	} {
		err := asOrg(ctx, conn(t, c.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt, c.args...); return err })
		wantState(t, c.what, err, c.states...)
	}
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.Compliance().HeartbeatDrills(ctx)
		if err == nil && len(all) != 2 {
			t.Errorf("the register: %d drills", len(all))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
