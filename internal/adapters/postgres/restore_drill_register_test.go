//go:build local

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The restore drill register keeps its rules (migration 000317, FY323): a drill is recorded through the unit of work with
// its audit event, and counts as recovered only when every one of DR-06's checks passed; its scenario is one of the three
// and its minutes are whole; nothing changes or goes; the worker records none; another organisation sees none.
// req: DR-06
// req: CTL-RES-02
// req: AUD-01
// req: QA-ENG-05
// asvs: V8.2.2
func TestAttackTheRestoreDrillRegisterKeepsItsRules(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Drill Register A Pte Ltd"), books(t, "Drill Register B Pte Ltd")
	api := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	held := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	full := storage.RestoreDrill{HeldOn: held, Scenario: "project_loss", RestoredFrom: "The weekly export", DataLostMinutes: 10080, TookMinutes: 185,
		Ledger: true, AuditChain: true, Documents: true, Keys: true, SignIn: true, Tick: true, Outbox: true, Findings: "f", RecordedBy: "user:x", RecordedAt: now}
	partial := full
	partial.Outbox = false
	var recorded []storage.RestoreDrill
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		for _, d := range []storage.RestoreDrill{full, partial} {
			r, err := u.Compliance().RecordRestoreDrill(ctx, d)
			if err != nil {
				return err
			}
			recorded = append(recorded, r)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !recorded[0].Recovered() || recorded[1].Recovered() || recorded[0].DataLostMinutes != 10080 || recorded[0].TookMinutes != 185 {
		t.Fatalf("recorded: %+v", recorded)
	}
	insert := `INSERT INTO fyniq.restore_drill (held_on, scenario, restored_from, data_lost_minutes, took_minutes, ledger, audit_chain, documents, keys,
	  sign_in, tick, outbox, findings, recorded_by, recorded_at) VALUES (current_date, $1, 'x', $2, $3, true, true, true, true, true, true, true, 'f', 'user:x', now())`
	for _, c := range []struct {
		what, role, stmt string
		args             []any
		states           []string
	}{
		{"the worker recording a drill", "fyniq_worker", insert, []any{"project_loss", 0, 1}, []string{"42501"}},
		{"a scenario that is none", "fyniq_api", insert, []any{"meteor", 0, 1}, []string{"23514"}},
		{"data lost below zero", "fyniq_api", insert, []any{"corruption", -1, 1}, []string{"23514"}},
		{"a drill that took no time", "fyniq_api", insert, []any{"corruption", 0, 0}, []string{"23514"}},
		{"a drill with no audit event", "fyniq_api", insert, []any{"corruption", 0, 1}, []string{"FY061"}},
		{"the API changing a drill", "fyniq_api", `UPDATE fyniq.restore_drill SET outbox = true`, nil, []string{"42501"}},
		{"the owner passing a failed check", "fyniq_migrator", `UPDATE fyniq.restore_drill SET outbox = true`, nil, []string{"FY323"}},
		{"the owner deleting a drill", "fyniq_migrator", `DELETE FROM fyniq.restore_drill`, nil, []string{"FY323"}},
		{"the owner emptying the register", "fyniq_migrator", `TRUNCATE fyniq.restore_drill`, nil, []string{"FY060", "FY061"}},
	} {
		err := asOrg(ctx, conn(t, c.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt, c.args...); return err })
		wantState(t, c.what, err, c.states...)
	}
	for _, c := range []struct {
		books porttest.Books
		want  int
	}{{a, 2}, {b, 0}} {
		if err := api.Do(ctx, c.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.Compliance().RestoreDrills(ctx)
			if err == nil && len(all) != c.want {
				t.Errorf("%s sees %d drills, want %d", c.books.Org, len(all), c.want)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	var events int
	if err := asOrg(ctx, conn(t, "fyniq_readonly"), a.Org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM fyniq.audit_event WHERE action = 'restore_drill.recorded'
		  AND body::jsonb->'changes'->'recovered'->>1 IN ('true', 'false')`).Scan(&events)
	}); err != nil || events != 2 {
		t.Errorf("the drills' audit events: %d, %v", events, err)
	}
}
