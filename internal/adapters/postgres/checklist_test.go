//go:build local

package postgres_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/closing"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-GL-07 (rev 0.68): a checklist template version and a checklist mark never change or go, and a mark is written only
// while its month is open, whatever the API does; a skipped task names its reason.
// req: FR-GL-07
func TestAttackTheCloseChecklistIsAppendOnlyAndOnlyInAnOpenMonth(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "accountant"}})
	b := books(t, "Checklist Pte Ltd")
	s := store(t, "fyniq_api")
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		first, err := u.Closes().SaveChecklistTemplate(ctx, []byte(`[{"key":"bank","title":"Bank reconciled","due_days":7}]`))
		if err != nil {
			return err
		}
		second, err := u.Closes().SaveChecklistTemplate(ctx, []byte(`[{"key":"bank","title":"Bank reconciled","due_days":5}]`))
		if err == nil && (first.Version != 1 || second.Version != 2 || second.WrittenBy != "user:accountant") {
			t.Errorf("the versions: %+v %+v", first, second)
		}
		if err != nil {
			return err
		}
		latest, err := u.Closes().ChecklistTemplate(ctx)
		if err == nil && latest.Version != 2 {
			t.Errorf("the latest version: %+v", latest)
		}
		for _, state := range []string{storage.ChecklistDone, storage.ChecklistOpen} {
			if _, err := u.Closes().MarkChecklistTask(ctx, storage.NewChecklistEntry{PeriodID: b.Oct.ID, TaskKey: "bank", TemplateVersion: 2, State: state}); err != nil {
				return err
			}
		}
		es, err := u.Closes().ChecklistEntries(ctx, b.Oct.ID)
		if err == nil && (len(es) != 1 || es[0].State != storage.ChecklistOpen) {
			t.Errorf("the latest mark: %+v", es)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if n := count(t, b.Org, "close_checklist_entry"); n != 2 {
		t.Errorf("%d marks kept, want both", n)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Periods().Close(ctx, b.Oct.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Closes().MarkChecklistTask(ctx, storage.NewChecklistEntry{PeriodID: b.Oct.ID, TaskKey: "bank", State: storage.ChecklistDone})
		return err
	}); !refusedAs(err, storage.CodeChecklist) {
		t.Errorf("a mark in a closed month: %v", err)
	}

	insert := func(period, key, state, note string) string {
		n := "NULL"
		if note != "" {
			n = "'" + note + "'"
		}
		return "INSERT INTO fyniq.close_checklist_entry (period_id, task_key, template_version, state, note, written_by, written_at) VALUES ('" +
			period + "', '" + key + "', 0, '" + state + "', " + n + ", 'user:a', now())"
	}
	nov := b.Nov.ID.String()
	for _, c := range []struct{ role, stmt, state string }{
		// The API's role only adds.
		{"fyniq_api", "UPDATE fyniq.close_checklist_entry SET state = 'done'", "42501"},
		{"fyniq_api", "DELETE FROM fyniq.close_checklist_template", "42501"},
		// Not even the owner of the tables rewrites or removes one.
		{"fyniq_migrator", "UPDATE fyniq.close_checklist_entry SET state = 'done'", "FY165"},
		{"fyniq_migrator", "DELETE FROM fyniq.close_checklist_entry", "FY165"},
		{"fyniq_migrator", "UPDATE fyniq.close_checklist_template SET tasks = '[]'", "FY165"},
		{"fyniq_migrator", "DELETE FROM fyniq.close_checklist_template", "FY165"},
		{"fyniq_migrator", "TRUNCATE fyniq.close_checklist_entry", "FY002"},
		// A closed month's checklist does not change, by any path.
		{"fyniq_api", insert(b.Oct.ID.String(), "bank", "done", ""), "FY164"},
		// A skipped task names why; keys and states are known; a template is a list of one to 40 tasks.
		{"fyniq_api", insert(nov, "bank", "skipped", ""), "23514"},
		{"fyniq_api", insert(nov, "Bank Task", "done", ""), "23514"},
		{"fyniq_api", insert(nov, "bank", "waived", "x"), "23514"},
		{"fyniq_api", "INSERT INTO fyniq.close_checklist_template (version, tasks, written_by, written_at) VALUES (9, '[]', 'user:a', now())", "23514"},
		{"fyniq_api", "INSERT INTO fyniq.close_checklist_template (version, tasks, written_by, written_at) VALUES (9, '{}', 'user:a', now())", "23514"},
	} {
		err := asOrg(ctx, conn(t, c.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt); return err })
		wantState(t, c.role+": "+c.stmt, err, c.state)
	}
}

// checklistDone marks every task of a period's close checklist done, so its close checks see the checklist finished
// (FR-GL-07, C.2 #58).
func checklistDone(t *testing.T, s storage.Store, org, period uuid.UUID) {
	t.Helper()
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "checklist-preparer"}})
	if err := s.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		c, err := closing.ChecklistOf(ctx, u, period)
		if err != nil {
			return err
		}
		for _, it := range c.Items {
			if _, err := closing.MarkTask(ctx, u, period, it.Key, storage.ChecklistDone, ""); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
