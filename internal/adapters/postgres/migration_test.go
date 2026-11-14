//go:build local

package postgres_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func migrationFile(run uuid.UUID, org uuid.UUID, name string, now time.Time) storage.MigrationFile {
	return storage.MigrationFile{RunID: run, FileName: name, MediaType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", Size: 10,
		SHA256: strings.Repeat("a", 64), OriginalSHA256: strings.Repeat("a", 64), StorageKey: "migrations/" + org.String() + "/" + run.String() + "/" + uuid.NewString(),
		DataClass: storage.ClassPayroll, AddedBy: "user:x", AddedAt: now}
}

// The workbook migration's record keeps its rules (migration 000325, FY325): it starts only in an organisation without
// a posted entry, once; a step starts validating a workbook of its own migration, one at a time, and changes once, by
// the worker, to its report and Checks export; nothing changes otherwise or goes; the API cannot write a report nor
// the worker start a migration; another organisation sees none of it.
// req: FR-MIG-01
// req: AUD-01
// req: QA-ENG-05
// asvs: V8.2.2
func TestAttackTheMigrationRecordKeepsItsRules(t *testing.T) {
	ctx := context.Background()
	a, b, posted := books(t, "Migration A Pte Ltd"), books(t, "Migration B Pte Ltd"), books(t, "Migration Posted Pte Ltd")
	api, worker := store(t, "fyniq_api"), store(t, "fyniq_worker")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)

	// An organisation with a posted entry starts no migration: the database refuses it whoever asks.
	if err := api.Do(ctx, posted.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		id := uuid.NewString()
		_, err := u.Ledger().Append(ctx, storage.NewEntry{PostingDate: porttest.Day(10, 22), PostingIdentity: id, RuleVersion: "test@1",
			Source: storage.Source{Type: "manual_journal", ID: id, Version: 1}, Lines: []storage.NewLine{
				{AccountID: posted.Bank.ID, Currency: "SGD", Amount: "100.00", FXRate: "1", FXRateSource: "base", BaseAmount: "100.00"},
				{AccountID: posted.Sales.ID, Currency: "SGD", Amount: "-100.00", FXRate: "1", FXRateSource: "base", BaseAmount: "-100.00"},
			}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	err := api.Do(ctx, posted.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if p, err := u.Migrations().Posted(ctx); err != nil || !p {
			t.Errorf("the organisation with an entry: posted %v %v", p, err)
		}
		_, err := u.Migrations().Start(ctx, "user:x", now)
		return err
	})
	if !storageCode(err, storage.CodeMigration) {
		t.Errorf("a migration where an entry is posted: %v", err)
	}

	var run storage.MigrationRun
	var step storage.MigrationStep
	var workbook storage.MigrationFile
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if p, err := u.Migrations().Posted(ctx); err != nil || p {
			t.Errorf("a fresh organisation: posted %v %v", p, err)
		}
		if run, err = u.Migrations().Start(ctx, "user:x", now); err != nil {
			return err
		}
		step, workbook, err = u.Migrations().AddWorkbook(ctx, storage.StepWorkbook, migrationFile(run.ID, a.Org, "Hub.xlsx", now))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if step.State != storage.StepValidating || step.WorkbookFileID != workbook.ID || workbook.Kind != storage.MigrationWorkbook || run.Status != "open" {
		t.Errorf("the started migration: %+v %+v %+v", run, step, workbook)
	}
	for what, fn := range map[string]func(u storage.UnitOfWork) error{
		"a second migration": func(u storage.UnitOfWork) error { _, err := u.Migrations().Start(ctx, "user:x", now); return err },
		"a second workbook while one validates": func(u storage.UnitOfWork) error {
			_, _, err := u.Migrations().AddWorkbook(ctx, storage.StepWorkbook, migrationFile(run.ID, a.Org, "Again.xlsx", now))
			return err
		},
	} {
		err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error { return fn(u) })
		if !storageCode(err, storage.CodeDuplicate) {
			t.Errorf("%s: %v", what, err)
		}
	}

	report := storage.MigrationReport{State: storage.StepValid, Report: []byte(`{"format":"fyniq-migration-report@1"}`), ReportSHA256: strings.Repeat("b", 64),
		Warnings: 1, ValidatedAt: now}
	checks := migrationFile(run.ID, a.Org, "checks.csv", now)
	checks.MediaType, checks.DataClass, checks.AddedBy = "text/csv", storage.ClassGeneral, "system:migration.validate"
	if err := worker.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		s, err := u.Migrations().Validated(ctx, step.ID, checks, report)
		if err == nil && (s.State != storage.StepValid || s.ChecksFileID == nil || s.ReportSHA256 != report.ReportSHA256 || s.Warnings != 1 || s.Errors != 0) {
			t.Errorf("the validated step: %+v", s)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	err = worker.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Migrations().Validated(ctx, step.ID, checks, report)
		return err
	})
	if !storageCode(err, storage.CodeMigration) && !storageCode(err, storage.CodeInvalid) {
		t.Errorf("validating twice: %v", err)
	}

	for _, c := range []struct {
		what, role, stmt string
		args             []any
		states           []string
	}{
		{"the worker starting a migration", "fyniq_worker", `INSERT INTO fyniq.migration_run (started_by, started_at) VALUES ('user:x', now())`, nil,
			[]string{"42501"}},
		{"the API writing a report", "fyniq_api", `UPDATE fyniq.migration_step SET state = 'invalid'`, nil, []string{"42501"}},
		{"a validated step validating again", "fyniq_worker", `UPDATE fyniq.migration_step SET state = 'validating', report = NULL, report_sha256 = NULL,
		  errors = NULL, warnings = NULL, validated_at = NULL, checks_file_id = NULL`, nil, []string{"FY325"}},
		{"a step starting valid", "fyniq_api", `INSERT INTO fyniq.migration_step (run_id, step, state, workbook_file_id, started_by, started_at)
		  VALUES ($1, 'M1', 'valid', $2, 'user:x', now())`, []any{run.ID, workbook.ID}, []string{"FY325"}},
		{"a step validating a Checks export", "fyniq_api", `INSERT INTO fyniq.migration_step (run_id, step, state, workbook_file_id, started_by, started_at)
		  VALUES ($1, 'M1', 'validating', (SELECT id FROM fyniq.migration_file WHERE kind = 'checks'), 'user:x', now())`, []any{run.ID},
			[]string{"FY325"}},
		{"a file outside the migrations' keys", "fyniq_api", `INSERT INTO fyniq.migration_file (run_id, kind, file_name, media_type, size_bytes, sha256,
		  original_sha256, storage_key, data_class, added_by, added_at) VALUES ($1, 'workbook', 'x.xlsx', 'text/csv', 1, repeat('a', 64), repeat('a', 64),
		  'staging/x', 'payroll', 'user:x', now())`, []any{run.ID}, []string{"23514"}},
		{"the owner changing a file", "fyniq_migrator", `UPDATE fyniq.migration_file SET file_name = 'y.xlsx'`, nil, []string{"FY325"}},
		{"the owner reopening a step", "fyniq_migrator", `UPDATE fyniq.migration_step SET started_by = 'user:y'`, nil, []string{"FY325"}},
		{"the owner deleting a step", "fyniq_migrator", `DELETE FROM fyniq.migration_step`, nil, []string{"FY325"}},
		{"the owner deleting the migration", "fyniq_migrator", `DELETE FROM fyniq.migration_run`, nil, []string{"FY325", "23503"}},
		{"the owner emptying the files", "fyniq_migrator", `TRUNCATE fyniq.migration_file CASCADE`, nil, []string{"FY060", "FY061"}},
	} {
		err := asOrg(ctx, conn(t, c.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt, c.args...); return err })
		wantState(t, c.what, err, c.states...)
	}

	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		runs, err := u.Migrations().Runs(ctx)
		if err != nil {
			return err
		}
		steps, err := u.Migrations().Steps(ctx, run.ID)
		if err != nil {
			return err
		}
		files, err := u.Migrations().Files(ctx, run.ID)
		if len(runs) != 1 || len(steps) != 1 || steps[0].State != storage.StepValid || len(files) != 2 || files[1].Kind != storage.MigrationChecks {
			t.Errorf("the record: %+v %+v %+v", runs, steps, files)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		runs, err := u.Migrations().Runs(ctx)
		if err == nil && len(runs) != 0 {
			t.Errorf("another organisation sees %d migrations", len(runs))
		}
		if _, err := u.Migrations().Run(ctx, run.ID); err == nil {
			t.Error("another organisation reads the migration")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
