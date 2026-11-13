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

// The migration's mapping and its sign-offs keep their rules (migration 000327, FY326): a version follows the one
// before and maps a valid workbook of its own migration; a sign-off signs the latest version over its hash, once;
// nothing changes or goes; the worker proposes versions and signs none; another organisation sees none of it.
// req: data/migration Set-up import and mapping review (M2)
// req: AUD-01
// req: QA-ENG-05
// asvs: V8.2.2
func TestAttackTheMigrationMappingKeepsItsRules(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Mapping A Pte Ltd"), books(t, "Mapping B Pte Ltd")
	api, worker := store(t, "fyniq_api"), store(t, "fyniq_worker")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	hash := func(c string) string { return strings.Repeat(c, 64) }
	var run storage.MigrationRun
	var step, pending storage.MigrationStep
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if run, err = u.Migrations().Start(ctx, "user:x", now); err != nil {
			return err
		}
		step, _, err = u.Migrations().AddWorkbook(ctx, storage.StepWorkbook, migrationFile(run.ID, a.Org, "Hub.xlsx", now))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	checks := migrationFile(run.ID, a.Org, "checks.csv", now)
	checks.MediaType, checks.DataClass, checks.AddedBy = "text/csv", storage.ClassGeneral, "system:migration.validate"
	var v1 storage.MigrationMapping
	if err := worker.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if _, err = u.Migrations().Validated(ctx, step.ID, checks, storage.MigrationReport{State: storage.StepValid, Report: []byte(`{}`),
			ReportSHA256: hash("b"), ValidatedAt: now}); err != nil {
			return err
		}
		v1, err = u.Migrations().Map(ctx, storage.MigrationMapping{RunID: run.ID, Version: 1, StepID: step.ID, Mapping: []byte(`{"v":1}`), SHA256: hash("c"),
			MadeBy: "system:migration.validate", MadeAt: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var v2 storage.MigrationMapping
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if v2, err = u.Migrations().Map(ctx, storage.MigrationMapping{RunID: run.ID, Version: 2, StepID: step.ID, Mapping: []byte(`{"v":2}`), SHA256: hash("d"),
			MadeBy: "user:x", MadeAt: now}); err != nil {
			return err
		}
		if latest, err := u.Migrations().Mapping(ctx, run.ID); err != nil || latest.ID != v2.ID {
			t.Errorf("the latest mapping: %+v %v", latest, err)
		}
		_, err = u.Migrations().SignOff(ctx, storage.MigrationSignOff{RunID: run.ID, Subject: storage.SignOffMapping, MappingID: &v2.ID, SubjectSHA256: v2.SHA256,
			SignedBy: "user:y", SignedAt: now})
		if err != nil {
			return err
		}
		// A workbook validating: no mapping of it until it is valid.
		pending, _, err = u.Migrations().AddWorkbook(ctx, storage.StepWorkbook, migrationFile(run.ID, a.Org, "Again.xlsx", now))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for what, fn := range map[string]func(u storage.UnitOfWork) error{
		"a version out of order": func(u storage.UnitOfWork) error {
			_, err := u.Migrations().Map(ctx, storage.MigrationMapping{RunID: run.ID, Version: 4, StepID: step.ID, Mapping: []byte(`{}`), SHA256: hash("e"), MadeBy: "user:x", MadeAt: now})
			return err
		},
		"a version of a workbook still validating": func(u storage.UnitOfWork) error {
			_, err := u.Migrations().Map(ctx, storage.MigrationMapping{RunID: run.ID, Version: 3, StepID: pending.ID, Mapping: []byte(`{}`), SHA256: hash("e"), MadeBy: "user:x", MadeAt: now})
			return err
		},
		"signing an earlier version": func(u storage.UnitOfWork) error {
			_, err := u.Migrations().SignOff(ctx, storage.MigrationSignOff{RunID: run.ID, Subject: storage.SignOffMapping, MappingID: &v1.ID, SubjectSHA256: v1.SHA256,
				SignedBy: "user:y", SignedAt: now})
			return err
		},
		"signing another hash": func(u storage.UnitOfWork) error {
			_, err := u.Migrations().SignOff(ctx, storage.MigrationSignOff{RunID: run.ID, Subject: storage.SignOffMapping, MappingID: &v2.ID, SubjectSHA256: hash("f"),
				SignedBy: "user:y", SignedAt: now})
			return err
		},
	} {
		err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error { return fn(u) })
		if !storageCode(err, storage.CodeMigration) {
			t.Errorf("%s: %v", what, err)
		}
	}
	err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Migrations().SignOff(ctx, storage.MigrationSignOff{RunID: run.ID, Subject: storage.SignOffMapping, MappingID: &v2.ID, SubjectSHA256: v2.SHA256,
			SignedBy: "user:z", SignedAt: now})
		return err
	})
	if !storageCode(err, storage.CodeDuplicate) {
		t.Errorf("signing a version twice: %v", err)
	}
	for _, c := range []struct {
		what, role, stmt string
		args             []any
		states           []string
	}{
		{"the worker signing", "fyniq_worker", `INSERT INTO fyniq.migration_signoff (run_id, subject, mapping_id, subject_sha256, signed_by, signed_at)
		  VALUES ($1, 'mapping', $2, $3, 'system:x', now())`, []any{run.ID, v2.ID, v2.SHA256}, []string{"42501"}},
		{"a sign-off of nothing", "fyniq_api", `INSERT INTO fyniq.migration_signoff (run_id, subject, mapping_id, subject_sha256, signed_by, signed_at)
		  VALUES ($1, 'mapping', NULL, $2, 'user:x', now())`, []any{run.ID, v2.SHA256}, []string{"23514", "FY326"}},
		{"the API changing a version", "fyniq_api", `UPDATE fyniq.migration_mapping SET mapping = '{}'`, nil, []string{"42501"}},
		{"the owner changing a version", "fyniq_migrator", `UPDATE fyniq.migration_mapping SET mapping = '{}'`, nil, []string{"FY326"}},
		{"the owner withdrawing a sign-off", "fyniq_migrator", `DELETE FROM fyniq.migration_signoff`, nil, []string{"FY326"}},
		{"the owner emptying the mappings", "fyniq_migrator", `TRUNCATE fyniq.migration_mapping CASCADE`, nil, []string{"FY060", "FY061"}},
	} {
		err := asOrg(ctx, conn(t, c.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt, c.args...); return err })
		wantState(t, c.what, err, c.states...)
	}
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := u.Migrations().Mapping(ctx, run.ID); err == nil {
			t.Error("another organisation reads the mapping")
		}
		offs, err := u.Migrations().SignOffs(ctx, run.ID)
		if err == nil && len(offs) != 0 {
			t.Errorf("another organisation sees %d sign-offs", len(offs))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// The commit keeps its rules (migration 000330, FY327): it is recorded only under its own approved request, of the
// migration's latest valid workbook and its latest mapping, signed; once, as one per migration; a record joins only its
// running commit, each key once; the commit is committed only when it holds every record it planned, and then takes
// no record; from the commit on, no workbook or mapping version is added; nothing changes or goes; the worker writes
// none of it; another organisation sees none of it.
// req: FR-MIG-01
// req: AUD-01
// req: QA-ENG-05
// asvs: V8.2.2
func TestAttackTheMigrationCommitKeepsItsRules(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Commit A Pte Ltd"), books(t, "Commit B Pte Ltd")
	api, worker := store(t, "fyniq_api"), store(t, "fyniq_worker")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	hash := func(c string) string { return strings.Repeat(c, 64) }
	requester, owner := founder(t, api, a.Org, 1, now), founder(t, api, a.Org, 2, now)
	var run storage.MigrationRun
	var step storage.MigrationStep
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if run, err = u.Migrations().Start(ctx, "user:x", now); err != nil {
			return err
		}
		step, _, err = u.Migrations().AddWorkbook(ctx, storage.StepWorkbook, migrationFile(run.ID, a.Org, "Hub.xlsx", now))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	checks := migrationFile(run.ID, a.Org, "checks.csv", now)
	checks.MediaType, checks.DataClass, checks.AddedBy = "text/csv", storage.ClassGeneral, "system:migration.validate"
	var v1 storage.MigrationMapping
	if err := worker.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if _, err = u.Migrations().Validated(ctx, step.ID, checks, storage.MigrationReport{State: storage.StepValid, Report: []byte(`{}`),
			ReportSHA256: hash("b"), ValidatedAt: now}); err != nil {
			return err
		}
		v1, err = u.Migrations().Map(ctx, storage.MigrationMapping{RunID: run.ID, Version: 1, StepID: step.ID, Mapping: []byte(`{"v":1}`), SHA256: hash("c"),
			MadeBy: "system:migration.validate", MadeAt: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var v2 storage.MigrationMapping
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if _, err = u.Migrations().SignOff(ctx, storage.MigrationSignOff{RunID: run.ID, Subject: storage.SignOffMapping, MappingID: &v1.ID,
			SubjectSHA256: v1.SHA256, SignedBy: "user:y", SignedAt: now}); err != nil {
			return err
		}
		v2, err = u.Migrations().Map(ctx, storage.MigrationMapping{RunID: run.ID, Version: 2, StepID: step.ID, Mapping: []byte(`{"v":2}`), SHA256: hash("d"),
			MadeBy: "user:x", MadeAt: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	other := request(t, api, a.Org, requester, "budget", uuid.NewString(), "budget.record", `{"execute":{}}`, []storage.ApprovalStep{{Quorum: 1}}, now)
	approved := request(t, api, a.Org, requester, "migration_commit", run.ID.String(), "migration.commit", `{"execute":{}}`, []storage.ApprovalStep{{Quorum: 1}}, now)
	for _, r := range []storage.ApprovalRequest{other, approved} {
		if err := signs(api, a.Org, r, 1, owner, now); err != nil {
			t.Fatal(err)
		}
		if err := decides(api, a.Org, r, "approved", now); err != nil {
			t.Fatal(err)
		}
	}
	pending := request(t, api, a.Org, requester, "migration_commit", run.ID.String(), "migration.commit", `{"execute":{}}`, []storage.ApprovalStep{{Quorum: 1}}, now)
	commit := func(approval, mapping uuid.UUID, records int) storage.MigrationCommit {
		return storage.MigrationCommit{RunID: run.ID, ApprovalID: approval, Path: storage.PathHistory, StepID: step.ID, MappingID: mapping, Records: records,
			ApprovedBy: "user:" + owner.String(), ApprovedAt: now}
	}
	start := func(c storage.MigrationCommit) (storage.MigrationCommit, error) {
		var out storage.MigrationCommit
		err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = u.Migrations().StartCommit(ctx, c)
			return err
		})
		return out, err
	}
	for what, c := range map[string]storage.MigrationCommit{
		"a commit under a request still pending":       commit(pending.ID, v2.ID, 2),
		"a commit under another object's request":      commit(other.ID, v2.ID, 2),
		"a commit by a mapping that is not the latest": commit(approved.ID, v1.ID, 2),
		"a commit by a mapping not signed":             commit(approved.ID, v2.ID, 2),
	} {
		if _, err := start(c); !storageCode(err, storage.CodeMigration) {
			t.Errorf("%s: %v", what, err)
		}
	}
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Migrations().SignOff(ctx, storage.MigrationSignOff{RunID: run.ID, Subject: storage.SignOffMapping, MappingID: &v2.ID, SubjectSHA256: v2.SHA256,
			SignedBy: "user:y", SignedAt: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	c, err := start(commit(approved.ID, v2.ID, 2))
	if err != nil || c.Status != storage.CommitRunning || c.Records != 2 {
		t.Fatalf("the commit: %+v %v", c, err)
	}
	if _, err := start(commit(approved.ID, v2.ID, 2)); !storageCode(err, storage.CodeDuplicate) {
		t.Errorf("a second commit: %v", err)
	}

	record := func(key string) storage.MigrationRecord {
		return storage.MigrationRecord{RunID: run.ID, CommitID: c.ID, Key: key, Kind: "contact", Sheet: "Contacts", Row: 6, RecordType: "contact",
			RecordID: uuid.New(), ImportedBy: "user:x", ImportedAt: now}
	}
	in := func(fn func(ctx context.Context, u storage.UnitOfWork) error) error { return api.Do(ctx, a.Org, fn) }
	if err := in(func(ctx context.Context, u storage.UnitOfWork) error { _, err := u.Migrations().Import(ctx, record("contact:a")); return err }); err != nil {
		t.Fatal(err)
	}
	if err := in(func(ctx context.Context, u storage.UnitOfWork) error { _, err := u.Migrations().Import(ctx, record("contact:a")); return err }); !storageCode(err, storage.CodeDuplicate) {
		t.Errorf("a key imported twice: %v", err)
	}
	if err := in(func(ctx context.Context, u storage.UnitOfWork) error { _, err := u.Migrations().Committed(ctx, c.ID, now); return err }); !storageCode(err, storage.CodeMigration) {
		t.Errorf("committed with a record left: %v", err)
	}
	if err := in(func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := u.Migrations().Import(ctx, record("contact:b")); err != nil {
			return err
		}
		done, err := u.Migrations().Committed(ctx, c.ID, now)
		if err == nil && (done.Status != storage.CommitCommitted || done.CommittedAt == nil) {
			t.Errorf("the committed commit: %+v", done)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for what, fn := range map[string]func(ctx context.Context, u storage.UnitOfWork) error{
		"a record after the commit": func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Migrations().Import(ctx, record("contact:c"))
			return err
		},
		"committing twice": func(ctx context.Context, u storage.UnitOfWork) error { _, err := u.Migrations().Committed(ctx, c.ID, now); return err },
		"a workbook after the commit": func(ctx context.Context, u storage.UnitOfWork) error {
			_, _, err := u.Migrations().AddWorkbook(ctx, storage.StepWorkbook, migrationFile(run.ID, a.Org, "Late.xlsx", now))
			return err
		},
		"a mapping version after the commit": func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Migrations().Map(ctx, storage.MigrationMapping{RunID: run.ID, Version: 3, StepID: step.ID, Mapping: []byte(`{}`), SHA256: hash("e"),
				MadeBy: "user:x", MadeAt: now})
			return err
		},
	} {
		if err := in(fn); !storageCode(err, storage.CodeMigration) {
			t.Errorf("%s: %v", what, err)
		}
	}
	for _, x := range []struct {
		what, role, stmt string
		args             []any
		states           []string
	}{
		{"the worker recording a commit", "fyniq_worker", `INSERT INTO fyniq.migration_commit (run_id, approval_id, path, step_id, mapping_id, records, approved_by, approved_at)
		  VALUES ($1, $2, 'history', $3, $4, 1, 'user:x', now())`, []any{run.ID, approved.ID, step.ID, v2.ID}, []string{"42501"}},
		{"the API changing a record", "fyniq_api", `UPDATE fyniq.migration_record SET row_number = 7`, nil, []string{"42501"}},
		{"the API changing a commit's records", "fyniq_api", `UPDATE fyniq.migration_commit SET records = 1`, nil, []string{"42501"}},
		{"the owner reopening the commit", "fyniq_migrator", `UPDATE fyniq.migration_commit SET status = 'running', committed_at = NULL`, nil, []string{"FY327", "23514"}},
		{"the owner changing a record", "fyniq_migrator", `UPDATE fyniq.migration_record SET row_number = 7`, nil, []string{"FY327"}},
		{"the owner removing a record", "fyniq_migrator", `DELETE FROM fyniq.migration_record`, nil, []string{"FY327"}},
		{"the owner removing the commit", "fyniq_migrator", `DELETE FROM fyniq.migration_commit`, nil, []string{"FY327", "23503"}},
		{"the owner emptying the records", "fyniq_migrator", `TRUNCATE fyniq.migration_record`, nil, []string{"FY060", "FY061"}},
	} {
		err := asOrg(ctx, conn(t, x.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt, x.args...); return err })
		wantState(t, x.what, err, x.states...)
	}
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := u.Migrations().Commit(ctx, run.ID); err == nil {
			t.Error("another organisation reads the commit")
		}
		records, err := u.Migrations().Records(ctx, c.ID)
		if err == nil && len(records) != 0 {
			t.Errorf("another organisation sees %d records", len(records))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
