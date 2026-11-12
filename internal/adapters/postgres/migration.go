package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) Migrations() storage.Migrations { return migrations{u} }

type migrations struct{ u *unitOfWork }

func toMigrationRun(r sqlcdb.FyniqMigrationRun) storage.MigrationRun {
	return storage.MigrationRun{ID: r.ID, Status: r.Status, StartedBy: r.StartedBy, StartedAt: r.StartedAt}
}

func toMigrationFile(r sqlcdb.FyniqMigrationFile) storage.MigrationFile {
	return storage.MigrationFile{ID: r.ID, RunID: r.RunID, Kind: r.Kind, FileName: r.FileName, MediaType: r.MediaType, Size: r.SizeBytes,
		SHA256: r.Sha256, OriginalSHA256: r.OriginalSha256, StorageKey: r.StorageKey, DataClass: storage.DataClass(r.DataClass), AddedBy: r.AddedBy,
		AddedAt: r.AddedAt}
}

func toMigrationStep(r sqlcdb.FyniqMigrationStep) storage.MigrationStep {
	s := storage.MigrationStep{ID: r.ID, RunID: r.RunID, Step: r.Step, State: r.State, WorkbookFileID: r.WorkbookFileID, StartedBy: r.StartedBy,
		StartedAt: r.StartedAt, ChecksFileID: r.ChecksFileID, Report: r.Report, ValidatedAt: r.ValidatedAt}
	if r.ReportSha256 != nil {
		s.ReportSHA256 = *r.ReportSha256
	}
	if r.Errors != nil {
		s.Errors = int(*r.Errors)
	}
	if r.Warnings != nil {
		s.Warnings = int(*r.Warnings)
	}
	return s
}

func (m migrations) Posted(ctx context.Context) (bool, error) {
	if err := m.u.check(); err != nil {
		return false, err
	}
	posted, err := m.u.q.AnyJournalEntry(ctx)
	return posted, mapError(err)
}

func (m migrations) Start(ctx context.Context, by string, at time.Time) (storage.MigrationRun, error) {
	if err := m.u.check(); err != nil {
		return storage.MigrationRun{}, err
	}
	r, err := m.u.q.InsertMigrationRun(ctx, sqlcdb.InsertMigrationRunParams{StartedBy: by, StartedAt: at})
	if err != nil {
		return storage.MigrationRun{}, mapError(err)
	}
	return toMigrationRun(r), m.u.event(ctx, "migration.started", "migration_run", r.ID, nil, map[string][2]any{"status": {nil, r.Status}})
}

func (m migrations) Runs(ctx context.Context) ([]storage.MigrationRun, error) {
	if err := m.u.check(); err != nil {
		return nil, err
	}
	rows, err := m.u.q.ListMigrationRuns(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.MigrationRun, 0, len(rows))
	for _, r := range rows {
		out = append(out, toMigrationRun(r))
	}
	return out, nil
}

func (m migrations) Run(ctx context.Context, id uuid.UUID) (storage.MigrationRun, error) {
	if err := m.u.check(); err != nil {
		return storage.MigrationRun{}, err
	}
	r, err := m.u.q.GetMigrationRun(ctx, id)
	if err != nil {
		return storage.MigrationRun{}, mapError(err)
	}
	return toMigrationRun(r), nil
}

func (m migrations) Files(ctx context.Context, run uuid.UUID) ([]storage.MigrationFile, error) {
	if err := m.u.check(); err != nil {
		return nil, err
	}
	rows, err := m.u.q.ListMigrationFiles(ctx, run)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.MigrationFile, 0, len(rows))
	for _, r := range rows {
		out = append(out, toMigrationFile(r))
	}
	return out, nil
}

func (m migrations) Steps(ctx context.Context, run uuid.UUID) ([]storage.MigrationStep, error) {
	if err := m.u.check(); err != nil {
		return nil, err
	}
	rows, err := m.u.q.ListMigrationSteps(ctx, run)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.MigrationStep, 0, len(rows))
	for _, r := range rows {
		out = append(out, toMigrationStep(r))
	}
	return out, nil
}

func (m migrations) Step(ctx context.Context, id uuid.UUID) (storage.MigrationStep, error) {
	if err := m.u.check(); err != nil {
		return storage.MigrationStep{}, err
	}
	r, err := m.u.q.GetMigrationStep(ctx, id)
	if err != nil {
		return storage.MigrationStep{}, mapError(err)
	}
	return toMigrationStep(r), nil
}

// addFile keeps a file on its migration.
func (m migrations) addFile(ctx context.Context, f storage.MigrationFile) (storage.MigrationFile, error) {
	r, err := m.u.q.InsertMigrationFile(ctx, sqlcdb.InsertMigrationFileParams{RunID: f.RunID, Kind: f.Kind, FileName: f.FileName, MediaType: f.MediaType,
		SizeBytes: f.Size, Sha256: f.SHA256, OriginalSha256: f.OriginalSHA256, StorageKey: f.StorageKey, DataClass: string(f.DataClass),
		AddedBy: f.AddedBy, AddedAt: f.AddedAt})
	if err != nil {
		return storage.MigrationFile{}, mapError(err)
	}
	return toMigrationFile(r), nil
}

func (m migrations) AddWorkbook(ctx context.Context, step string, f storage.MigrationFile) (storage.MigrationStep, storage.MigrationFile, error) {
	if err := m.u.check(); err != nil {
		return storage.MigrationStep{}, storage.MigrationFile{}, err
	}
	f.Kind = storage.MigrationWorkbook
	file, err := m.addFile(ctx, f)
	if err != nil {
		return storage.MigrationStep{}, storage.MigrationFile{}, err
	}
	r, err := m.u.q.InsertMigrationStep(ctx, sqlcdb.InsertMigrationStepParams{RunID: f.RunID, Step: step, WorkbookFileID: file.ID, StartedBy: f.AddedBy,
		StartedAt: f.AddedAt})
	if err != nil {
		return storage.MigrationStep{}, storage.MigrationFile{}, mapError(err)
	}
	s := toMigrationStep(r)
	return s, file, m.u.event(ctx, "migration.workbook_received", "migration_run", f.RunID, nil, map[string][2]any{"step": {nil, step},
		"step_id": {nil, s.ID.String()}, "file_id": {nil, file.ID.String()}, "file_name": {nil, file.FileName}, "size_bytes": {nil, file.Size},
		"sha256": {nil, file.SHA256}, "original_sha256": {nil, file.OriginalSHA256}, "state": {nil, s.State}})
}

func (m migrations) Validated(ctx context.Context, id uuid.UUID, checks storage.MigrationFile, rep storage.MigrationReport) (storage.MigrationStep, error) {
	if err := m.u.check(); err != nil {
		return storage.MigrationStep{}, err
	}
	before, err := m.Step(ctx, id)
	if err != nil {
		return storage.MigrationStep{}, err
	}
	checks.RunID, checks.Kind = before.RunID, storage.MigrationChecks
	file, err := m.addFile(ctx, checks)
	if err != nil {
		return storage.MigrationStep{}, err
	}
	errs, warns := int32(min(max(rep.Errors, 0), 1<<30)), int32(min(max(rep.Warnings, 0), 1<<30)) // #nosec G115 -- bounded
	r, err := m.u.q.ValidateMigrationStep(ctx, sqlcdb.ValidateMigrationStepParams{ID: id, State: rep.State, ChecksFileID: &file.ID, Report: rep.Report,
		ReportSha256: &rep.ReportSHA256, Errors: &errs, Warnings: &warns, ValidatedAt: &rep.ValidatedAt})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.MigrationStep{}, &storage.Error{Code: storage.CodeMigration, Message: "the step is no longer validating"}
	}
	if err != nil {
		return storage.MigrationStep{}, mapError(err)
	}
	s := toMigrationStep(r)
	return s, m.u.event(ctx, "migration.validated", "migration_run", s.RunID, nil, map[string][2]any{"step": {nil, s.Step},
		"step_id": {nil, s.ID.String()}, "state": {before.State, s.State}, "errors": {nil, s.Errors}, "warnings": {nil, s.Warnings},
		"report_sha256": {nil, s.ReportSHA256}, "checks_file_id": {nil, file.ID.String()}, "checks_sha256": {nil, file.SHA256}})
}

func toMigrationMapping(r sqlcdb.FyniqMigrationMapping) storage.MigrationMapping {
	return storage.MigrationMapping{ID: r.ID, RunID: r.RunID, Version: int(r.Version), StepID: r.StepID, Mapping: r.Mapping, SHA256: r.MappingSha256,
		MadeBy: r.MadeBy, MadeAt: r.MadeAt}
}

func toMigrationSignOff(r sqlcdb.FyniqMigrationSignoff) storage.MigrationSignOff {
	return storage.MigrationSignOff{ID: r.ID, RunID: r.RunID, Subject: r.Subject, MappingID: r.MappingID, SubjectSHA256: r.SubjectSha256,
		SignedBy: r.SignedBy, SignedAt: r.SignedAt}
}

func (m migrations) Mapping(ctx context.Context, run uuid.UUID) (storage.MigrationMapping, error) {
	if err := m.u.check(); err != nil {
		return storage.MigrationMapping{}, err
	}
	r, err := m.u.q.LatestMigrationMapping(ctx, run)
	if err != nil {
		return storage.MigrationMapping{}, mapError(err)
	}
	return toMigrationMapping(r), nil
}

func (m migrations) Map(ctx context.Context, x storage.MigrationMapping) (storage.MigrationMapping, error) {
	if err := m.u.check(); err != nil {
		return storage.MigrationMapping{}, err
	}
	r, err := m.u.q.InsertMigrationMapping(ctx, sqlcdb.InsertMigrationMappingParams{RunID: x.RunID, Version: int32(min(max(x.Version, 1), 1<<30)), // #nosec G115 -- bounded
		StepID: x.StepID, Mapping: x.Mapping, MappingSha256: x.SHA256, MadeBy: x.MadeBy, MadeAt: x.MadeAt})
	if err != nil {
		return storage.MigrationMapping{}, mapError(err)
	}
	out := toMigrationMapping(r)
	return out, m.u.event(ctx, "migration.mapped", "migration_run", out.RunID, nil, map[string][2]any{"mapping_version": {nil, out.Version},
		"mapping_sha256": {nil, out.SHA256}, "step_id": {nil, out.StepID.String()}})
}

func (m migrations) SignOffs(ctx context.Context, run uuid.UUID) ([]storage.MigrationSignOff, error) {
	if err := m.u.check(); err != nil {
		return nil, err
	}
	rows, err := m.u.q.ListMigrationSignoffs(ctx, run)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.MigrationSignOff, 0, len(rows))
	for _, r := range rows {
		out = append(out, toMigrationSignOff(r))
	}
	return out, nil
}

func (m migrations) SignOff(ctx context.Context, s storage.MigrationSignOff) (storage.MigrationSignOff, error) {
	if err := m.u.check(); err != nil {
		return storage.MigrationSignOff{}, err
	}
	r, err := m.u.q.InsertMigrationSignoff(ctx, sqlcdb.InsertMigrationSignoffParams{RunID: s.RunID, Subject: s.Subject, MappingID: s.MappingID,
		SubjectSha256: s.SubjectSHA256, SignedBy: s.SignedBy, SignedAt: s.SignedAt})
	if err != nil {
		return storage.MigrationSignOff{}, mapError(err)
	}
	out := toMigrationSignOff(r)
	changes := map[string][2]any{"subject": {nil, out.Subject}, "subject_sha256": {nil, out.SubjectSHA256}, "signed_by": {nil, out.SignedBy}}
	if out.MappingID != nil {
		changes["mapping_id"] = [2]any{nil, out.MappingID.String()}
	}
	return out, m.u.event(ctx, "migration."+out.Subject+"_signed", "migration_run", out.RunID, nil, changes)
}
