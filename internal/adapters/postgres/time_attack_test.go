//go:build local

package postgres_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A week of time is decided by a manager of its project or the Controller and never by the person; an entry follows its
// submission, an approved entry never changes or goes, a reversing entry is the opposite of an approved one, once, and time is a
// member's of the project, on a task in use (FR-PRJ-05, FR-PRJ-07; FY602, FY603, FY604).
// req: FR-PRJ-05
// req: FR-PRJ-07
// req: QA-ENG-05
// asvs: V8.2.2, V13.2.2
func TestAttackTimeIsDecidedByTheProjectsManagerAndApprovedTimeNeverChanges(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "attacker"}})
	api := store(t, "fyniq_api")
	b := books(t, "Attack time Pte Ltd")
	now := time.Date(2099, 10, 1, 9, 0, 0, 0, time.UTC)
	priya := joinAs(t, api, b.Org, "priya-"+uuid.NewString()[:8], []string{"employee"}, nil, 1, now).UserID
	wei := joinAs(t, api, b.Org, "wei-"+uuid.NewString()[:8], []string{"approver"}, nil, 2, now).UserID
	otto := joinAs(t, api, b.Org, "otto-"+uuid.NewString()[:8], []string{"approver"}, nil, 3, now).UserID
	una := joinAs(t, api, b.Org, "una-"+uuid.NewString()[:8], []string{"controller"}, nil, 4, now).UserID
	week := time.Date(2099, 10, 5, 0, 0, 0, 0, time.UTC) // a Monday
	day := week.AddDate(0, 0, 1)
	var project, task uuid.UUID
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		d, err := u.Dimensions().Create(ctx, storage.NewDimension{Kind: storage.DimensionProject, Name: "Project"})
		if err != nil {
			return err
		}
		v, err := u.Dimensions().CreateValue(ctx, storage.NewDimensionValue{DimensionID: d.ID, Code: "ATLAS", Name: "Atlas"})
		if err != nil {
			return err
		}
		project = v.ID
		if _, err := u.Projects().Create(ctx, storage.NewProject{ID: v.ID, Currency: "SGD", Status: storage.ProjectActive, BillingMethod: storage.BillingTimeAndMaterials}); err != nil {
			return err
		}
		if err := u.Projects().SetMembers(ctx, v.ID, []storage.ProjectPerson{{UserID: priya}, {UserID: wei, Manager: true}}); err != nil {
			return err
		}
		tk, err := u.Projects().CreateTask(ctx, v.ID, "Build", true)
		task = tk.ID
		return err
	}); err != nil {
		t.Fatal(err)
	}
	do := func(who string, fn func(ctx context.Context, u storage.UnitOfWork) error) error {
		return api.Do(ctx, b.Org, fn)
	}
	entry := func(user uuid.UUID, tk uuid.UUID, minutes int) error {
		return do("", func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Timesheets().SaveWeek(ctx, user, week, storage.TimeWeekEdits{Add: []storage.NewTimeEntry{{UserID: user, ProjectID: project, TaskID: tk, WorkDate: day, Minutes: minutes, Billable: true}}})
			return err
		})
	}
	// Time is a member's: Otto is not on the project.
	if err := entry(otto, task, 60); storage.CodeOf(err) != storage.CodeTime {
		t.Errorf("a person not on the project logs time: %v", err)
	}
	if err := entry(priya, task, 135); err != nil {
		t.Fatalf("an entry: %v", err)
	}
	var sub storage.Submission
	if err := do("", func(ctx context.Context, u storage.UnitOfWork) (err error) {
		sub, _, err = u.Timesheets().Submit(ctx, priya, project, week, now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	decide := func(by uuid.UUID, approve bool) error {
		return do("", func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Timesheets().Decide(ctx, sub.ID, 1, by, approve, "Fix it", now)
			return err
		})
	}
	for name, by := range map[string]uuid.UUID{"the person": priya, "an Approver who does not manage the project": otto} {
		if err := decide(by, true); storage.CodeOf(err) != storage.CodeTime {
			t.Errorf("%s approves the week: %v", name, err)
		}
	}
	if err := decide(wei, true); err != nil {
		t.Fatalf("the project's manager approves: %v", err)
	}
	if err := decide(una, true); storage.CodeOf(err) != storage.CodeVersion && storage.CodeOf(err) != storage.CodeTime {
		t.Errorf("a week is decided once: %v", err)
	}

	// An approved entry never changes or goes, in any role; the entries follow their week.
	var approved storage.TimeEntry
	if err := do("", func(ctx context.Context, u storage.UnitOfWork) error {
		es, err := u.Timesheets().Entries(ctx, storage.TimeFilter{UserID: priya, From: week, To: week.AddDate(0, 0, 6)})
		if len(es) == 1 {
			approved = es[0]
		}
		return err
	}); err != nil || approved.Status != storage.TimeApproved {
		t.Fatalf("the approved entry: %+v %v", approved, err)
	}
	ctx0 := context.Background()
	for _, c := range []struct{ name, role, sql, code string }{
		{"changing approved minutes as the owner", "fyniq_migrator", `UPDATE fyniq.time_entry SET minutes = 1, version = version + 1`, "FY603"},
		{"changing the status of an approved entry", "fyniq_migrator", `UPDATE fyniq.time_entry SET status = 'draft', submission_id = NULL, version = version + 1`, "FY603"},
		{"deleting an approved entry as the owner", "fyniq_migrator", `DELETE FROM fyniq.time_entry`, "FY603"},
		{"deleting an approved entry as the application", "fyniq_api", `DELETE FROM fyniq.time_entry`, "FY603"},
		{"deciding a decided week", "fyniq_migrator", `UPDATE fyniq.timesheet_submission SET status = 'returned', comment = 'x', version = version + 1`, "FY602"},
		{"deleting a submission as the owner", "fyniq_migrator", `DELETE FROM fyniq.timesheet_submission`, "FY602"},
		{"a reversal that is not the opposite", "fyniq_api", `INSERT INTO fyniq.time_entry (user_id, project_id, task_id, work_date, minutes, billable, reverses_id)
			SELECT user_id, project_id, task_id, work_date, -5, billable, id FROM fyniq.time_entry`, "FY603"},
	} {
		err := asOrg(ctx0, conn(t, c.role), b.Org, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx0, c.sql); err != nil {
				return err
			}
			return auditRaw(ctx0, tx, "time.week_saved", "time_week", uuid.New())
		})
		if err == nil || !strings.Contains(err.Error(), c.code) {
			t.Errorf("%s: %v, want %s", c.name, err, c.code)
		}
	}
	// The exact opposite is allowed, once.
	reverse := func() error {
		return do("", func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Timesheets().Reverse(ctx, approved.ID)
			return err
		})
	}
	if err := reverse(); err != nil {
		t.Fatalf("a reversing entry: %v", err)
	}
	if err := reverse(); err == nil {
		t.Error("an entry was reversed twice")
	}
}

// A rate never changes or goes, in any role, and one starts on a day once for its person (FR-PRJ-06; FY605).
// req: FR-PRJ-06
// req: QA-ENG-05
// asvs: V13.2.2
func TestAttackARateNeverChangesOrGoes(t *testing.T) {
	ctx0 := context.Background()
	b := books(t, "Attack rates Pte Ltd")
	enter := `INSERT INTO fyniq.person_cost_rate (role_name, effective_from, hourly_rate, currency, minor_units, created_by) VALUES ('Standard', '2030-01-01', 70, 'SGD', 2, 'user:x')`
	// Rows exist only through the unit of work; the guards are tried on the owner's role with a committed rate.
	if err := asOrg(ctx0, conn(t, "fyniq_migrator"), b.Org, func(tx pgx.Tx) error {
		var id uuid.UUID
		if err := tx.QueryRow(ctx0, enter+` RETURNING id`).Scan(&id); err != nil {
			return err
		}
		if err := auditRaw(ctx0, tx, "cost_rate.entered", "person_cost_rate", id); err != nil {
			return err
		}
		for name, sql := range map[string]string{"changing": `UPDATE fyniq.person_cost_rate SET hourly_rate = 1`, "deleting": `DELETE FROM fyniq.person_cost_rate`} {
			if _, err := tx.Exec(ctx0, "SAVEPOINT x"); err != nil {
				return err
			}
			_, err := tx.Exec(ctx0, sql)
			if err == nil || !strings.Contains(err.Error(), "FY605") {
				t.Errorf("%s a rate: %v", name, err)
			}
			if _, err := tx.Exec(ctx0, "ROLLBACK TO SAVEPOINT x"); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx0, "SAVEPOINT y"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx0, enter); err == nil || !strings.Contains(err.Error(), "person_cost_rate_day_key") {
			t.Errorf("a day twice: %v", err)
		}
		_, err := tx.Exec(ctx0, "ROLLBACK TO SAVEPOINT y")
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
