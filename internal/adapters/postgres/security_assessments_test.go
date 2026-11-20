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

// The register of security assessments is only ever added to (001301, FY362): an assessment with a finding and its steps are recorded
// and audited; a step out of order (a retest before a fix, a second fix, an acceptance after the retest, one before the finding or the step
// before it), an unknown severity, a due day before the day found or on an informational finding, an acceptance with no reason, a hash that
// is not a SHA-256, and anything changed or deleted are refused, whoever asks; the worker writes none of it; another organisation sees none.
// req: SEC-25
// req: AUD-01
// req: QA-ENG-05
// asvs: V8.2.2
func TestAttackTheRegisterOfSecurityAssessmentsIsOnlyAddedTo(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Assess A Pte Ltd"), books(t, "Assess B Pte Ltd")
	api := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	by := "user:" + founder(t, api, a.Org, 1, now).String()
	found := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	var finding storage.Finding
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		saved, err := u.Compliance().RecordAssessment(ctx, storage.Assessment{Kind: "penetration_test", Tester: "Example Security", Independent: true, Scope: "The web app",
			StartedOn: found.AddDate(0, 0, -14), EndedOn: found, Summary: "One high", ReportSHA256: "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
			RecordedBy: by, RecordedAt: now, Findings: []storage.Finding{{Ref: "PT-1", Title: "Reflected script", Severity: "high", FoundOn: found,
				DueOn: new(found.AddDate(0, 0, 30)), RecordedBy: by, RecordedAt: now}}})
		if err != nil {
			return err
		}
		finding = saved.Findings[0]
		for _, s := range []storage.FindingStep{{Step: "fixed", OnDate: found.AddDate(0, 0, 3), RecordedBy: by, RecordedAt: now},
			{Step: "retested", OnDate: found.AddDate(0, 0, 5), RecordedBy: by, RecordedAt: now.Add(time.Second)}} {
			if _, err := u.Compliance().StepFinding(ctx, finding.ID, s); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var list []storage.Assessment
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		list, err = u.Compliance().Assessments(ctx)
		return err
	}); err != nil ||
		len(list) != 1 || len(list[0].Findings) != 1 || len(list[0].Findings[0].Steps) != 2 || list[0].Findings[0].Steps[1].Step != "retested" {
		t.Fatalf("the register: %+v %v", list, err)
	}
	var actions []string
	if err := asOrg(ctx, conn(t, "fyniq_api"), a.Org, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT action FROM fyniq.audit_event WHERE action LIKE 'security_%' ORDER BY seq`)
		if err != nil {
			return err
		}
		actions, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	}); err != nil || len(actions) != 4 || actions[0] != "security_assessment.recorded" || actions[3] != "security_finding.stepped" {
		t.Errorf("the register's audit events: %v %v", actions, err)
	}
	assessment := list[0].ID
	newFinding := `INSERT INTO fyniq.security_finding (assessment_id, ref, title, severity, found_on, due_on, recorded_by, recorded_at)
	               VALUES ($1, $2, 't', $3, DATE '2026-10-05', $4::date, 'user:x', now())`
	step := `INSERT INTO fyniq.security_finding_step (finding_id, step, on_date, reason, recorded_by, recorded_at) VALUES ($1, $2, $3::date, $4, 'user:x', now())`
	for _, c := range []struct {
		what, role, stmt string
		args             []any
		states           []string
	}{
		{"the worker adding a finding", "fyniq_worker", newFinding, []any{assessment, "W-1", "low", "2027-04-01"}, []string{"42501"}},
		{"a second finding with a reference used", "fyniq_api", newFinding, []any{assessment, "PT-1", "low", "2027-04-01"}, []string{"23505"}},
		{"an unknown severity", "fyniq_api", newFinding, []any{assessment, "X-1", "grave", "2027-04-01"}, []string{"23514"}},
		{"a due day before the day found", "fyniq_api", newFinding, []any{assessment, "X-2", "low", "2026-10-01"}, []string{"23514"}},
		{"an informational finding with a due day", "fyniq_api", newFinding, []any{assessment, "X-3", "info", "2027-04-01"}, []string{"23514"}},
		{"a second fix", "fyniq_api", step, []any{finding.ID, "fixed", "2026-10-20", nil}, []string{"FY362", "23505"}},
		{"an acceptance after the retest", "fyniq_api", step, []any{finding.ID, "accepted", "2026-10-20", "why not"}, []string{"FY362"}},
		{"an acceptance with no reason", "fyniq_api", step, []any{finding.ID, "accepted", "2026-10-20", nil}, []string{"23514", "FY362"}},
		{"a hash that is not a SHA-256", "fyniq_api", `INSERT INTO fyniq.security_assessment (kind, tester, independent, scope, started_on, ended_on, summary, report_sha256, recorded_by,
		   recorded_at) VALUES ('other', 't', false, 's', DATE '2026-10-01', DATE '2026-10-02', 'm', 'abc', 'user:x', now())`, nil, []string{"23514"}},
		{"the API changing an assessment", "fyniq_api", `UPDATE fyniq.security_assessment SET summary = 'z'`, nil, []string{"42501"}},
		{"the owner changing an assessment", "fyniq_migrator", `UPDATE fyniq.security_assessment SET summary = 'z'`, nil, []string{"FY362"}},
		{"the owner changing a finding", "fyniq_migrator", `UPDATE fyniq.security_finding SET severity = 'low'`, nil, []string{"FY362"}},
		{"the owner deleting a step", "fyniq_migrator", `DELETE FROM fyniq.security_finding_step`, nil, []string{"FY362"}},
		{"the owner deleting an assessment", "fyniq_migrator", `DELETE FROM fyniq.security_assessment`, nil, []string{"FY362"}},
	} {
		err := asOrg(ctx, conn(t, c.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt, c.args...); return err })
		wantState(t, c.what, err, c.states...)
	}
	// A step dated before the finding was found, on a finding with no step yet.
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		s, err := u.Compliance().RecordAssessment(ctx, storage.Assessment{Kind: "other", Tester: "t", Scope: "s", StartedOn: found, EndedOn: found, Summary: "m", RecordedBy: by, RecordedAt: now,
			Findings: []storage.Finding{{Ref: "N-1", Title: "n", Severity: "low", FoundOn: found, DueOn: new(found.AddDate(0, 0, 180)), RecordedBy: by, RecordedAt: now}}})
		if err != nil {
			return err
		}
		_, err = u.Compliance().StepFinding(ctx, s.Findings[0].ID, storage.FindingStep{Step: "fixed", OnDate: found.AddDate(0, 0, -1), RecordedBy: by, RecordedAt: now})
		return err
	}); err == nil {
		t.Error("a step dated before the finding was recorded")
	}
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		l, err := u.Compliance().Assessments(ctx)
		if err == nil && len(l) != 0 {
			t.Errorf("another organisation sees %d assessments", len(l))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
