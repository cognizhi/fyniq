package postgres

import (
	"context"
	"errors"
	"fmt"
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

func (m migrations) MappingVersion(ctx context.Context, id uuid.UUID) (storage.MigrationMapping, error) {
	if err := m.u.check(); err != nil {
		return storage.MigrationMapping{}, err
	}
	r, err := m.u.q.GetMigrationMapping(ctx, id)
	if err != nil {
		return storage.MigrationMapping{}, mapError(err)
	}
	return toMigrationMapping(r), nil
}

func toMigrationCommit(r sqlcdb.FyniqMigrationCommit) storage.MigrationCommit {
	return storage.MigrationCommit{ID: r.ID, RunID: r.RunID, ApprovalID: r.ApprovalID, Path: r.Path, StepID: r.StepID, MappingID: r.MappingID,
		Records: int(r.Records), Status: r.Status, ApprovedBy: r.ApprovedBy, ApprovedAt: r.ApprovedAt, CommittedAt: r.CommittedAt}
}

func toMigrationRecord(r sqlcdb.FyniqMigrationRecord) storage.MigrationRecord {
	return storage.MigrationRecord{ID: r.ID, RunID: r.RunID, CommitID: r.CommitID, Key: r.Key, Kind: r.Kind, Sheet: r.Sheet, Row: int(r.RowNumber),
		RecordType: r.RecordType, RecordID: r.RecordID, ImportedBy: r.ImportedBy, ImportedAt: r.ImportedAt}
}

func (m migrations) Commit(ctx context.Context, run uuid.UUID) (storage.MigrationCommit, error) {
	if err := m.u.check(); err != nil {
		return storage.MigrationCommit{}, err
	}
	r, err := m.u.q.GetMigrationCommit(ctx, run)
	if err != nil {
		return storage.MigrationCommit{}, mapError(err)
	}
	return toMigrationCommit(r), nil
}

func (m migrations) StartCommit(ctx context.Context, c storage.MigrationCommit) (storage.MigrationCommit, error) {
	if err := m.u.check(); err != nil {
		return storage.MigrationCommit{}, err
	}
	r, err := m.u.q.InsertMigrationCommit(ctx, sqlcdb.InsertMigrationCommitParams{RunID: c.RunID, ApprovalID: c.ApprovalID, Path: c.Path, StepID: c.StepID,
		MappingID: c.MappingID, Records: int32(min(max(c.Records, 0), 1<<30)), // #nosec G115 -- bounded
		ApprovedBy: c.ApprovedBy, ApprovedAt: c.ApprovedAt})
	if err != nil {
		return storage.MigrationCommit{}, mapError(err)
	}
	out := toMigrationCommit(r)
	return out, m.u.event(ctx, "migration.commit_approved", "migration_run", out.RunID, nil, map[string][2]any{"commit_id": {nil, out.ID.String()},
		"approval_id": {nil, out.ApprovalID.String()}, "path": {nil, out.Path}, "step_id": {nil, out.StepID.String()},
		"mapping_id": {nil, out.MappingID.String()}, "records": {nil, out.Records}, "status": {nil, out.Status}})
}

func (m migrations) Records(ctx context.Context, commit uuid.UUID) ([]storage.MigrationRecord, error) {
	if err := m.u.check(); err != nil {
		return nil, err
	}
	rows, err := m.u.q.ListMigrationRecords(ctx, commit)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.MigrationRecord, 0, len(rows))
	for _, r := range rows {
		out = append(out, toMigrationRecord(r))
	}
	return out, nil
}

func (m migrations) Import(ctx context.Context, x storage.MigrationRecord) (storage.MigrationRecord, error) {
	if err := m.u.check(); err != nil {
		return storage.MigrationRecord{}, err
	}
	r, err := m.u.q.InsertMigrationRecord(ctx, sqlcdb.InsertMigrationRecordParams{RunID: x.RunID, CommitID: x.CommitID, Key: x.Key, Kind: x.Kind,
		Sheet: x.Sheet, RowNumber: int32(min(max(x.Row, 0), 1<<30)), // #nosec G115 -- bounded
		RecordType: x.RecordType, RecordID: x.RecordID, ImportedBy: x.ImportedBy, ImportedAt: x.ImportedAt})
	if err != nil {
		return storage.MigrationRecord{}, mapError(err)
	}
	out := toMigrationRecord(r)
	return out, m.u.event(ctx, "migration.record_imported", "migration_run", out.RunID, nil, map[string][2]any{"commit_id": {nil, out.CommitID.String()},
		"key": {nil, out.Key}, "kind": {nil, out.Kind}, "sheet": {nil, out.Sheet}, "row": {nil, out.Row}, "record_type": {nil, out.RecordType},
		"record_id": {nil, out.RecordID.String()}})
}

func (m migrations) Committed(ctx context.Context, id uuid.UUID, at time.Time) (storage.MigrationCommit, error) {
	if err := m.u.check(); err != nil {
		return storage.MigrationCommit{}, err
	}
	r, err := m.u.q.CommitMigrationCommit(ctx, sqlcdb.CommitMigrationCommitParams{ID: id, CommittedAt: &at})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.MigrationCommit{}, &storage.Error{Code: storage.CodeMigration, Message: "the commit is not running",
			Hint: "A commit is committed once, when its last record is imported"}
	}
	if err != nil {
		return storage.MigrationCommit{}, mapError(err)
	}
	out := toMigrationCommit(r)
	return out, m.u.event(ctx, "migration.committed", "migration_run", out.RunID, nil, map[string][2]any{"commit_id": {nil, out.ID.String()},
		"status": {storage.CommitRunning, out.Status}, "records": {nil, out.Records}})
}

func (m migrations) RecordOf(ctx context.Context, recordType string, id uuid.UUID) (storage.MigrationRecord, error) {
	if err := m.u.check(); err != nil {
		return storage.MigrationRecord{}, err
	}
	r, err := m.u.q.MigrationRecordOf(ctx, sqlcdb.MigrationRecordOfParams{RecordType: recordType, RecordID: id})
	if err != nil {
		return storage.MigrationRecord{}, mapError(err)
	}
	return toMigrationRecord(r), nil
}

func (m migrations) LockCommit(ctx context.Context, id uuid.UUID) (storage.MigrationCommit, error) {
	if err := m.u.check(); err != nil {
		return storage.MigrationCommit{}, err
	}
	r, err := m.u.q.LockMigrationCommit(ctx, id)
	if err != nil {
		return storage.MigrationCommit{}, mapError(err)
	}
	return toMigrationCommit(r), nil
}

func (m migrations) RecordByKey(ctx context.Context, commit uuid.UUID, key string) (storage.MigrationRecord, error) {
	if err := m.u.check(); err != nil {
		return storage.MigrationRecord{}, err
	}
	r, err := m.u.q.GetMigrationRecordByKey(ctx, sqlcdb.GetMigrationRecordByKeyParams{CommitID: commit, Key: key})
	if err != nil {
		return storage.MigrationRecord{}, mapError(err)
	}
	return toMigrationRecord(r), nil
}

func toMigrationDecision(r sqlcdb.FyniqMigrationDifference) storage.MigrationDecision {
	return storage.MigrationDecision{ID: r.ID, RunID: r.RunID, Line: r.LineKey, Class: r.Class, Note: r.Note, Fyniq: r.FyniqFigure, Workbook: r.WorkbookFigure,
		Version: int(r.Version), DecidedBy: r.DecidedBy, DecidedAt: r.DecidedAt}
}

func (m migrations) Decisions(ctx context.Context, run uuid.UUID) ([]storage.MigrationDecision, error) {
	if err := m.u.check(); err != nil {
		return nil, err
	}
	rows, err := m.u.q.ListMigrationDifferences(ctx, run)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.MigrationDecision, 0, len(rows))
	for _, r := range rows {
		out = append(out, toMigrationDecision(r))
	}
	return out, nil
}

func (m migrations) Decide(ctx context.Context, d storage.MigrationDecision) (storage.MigrationDecision, error) {
	if err := m.u.check(); err != nil {
		return storage.MigrationDecision{}, err
	}
	changed := func(was string) *storage.Error {
		return &storage.Error{Code: storage.CodeVersion, Message: fmt.Sprintf("the line %s was decided %s since it was read", d.Line, was)}
	}
	var before *storage.MigrationDecision
	var r sqlcdb.FyniqMigrationDifference
	var err error
	if d.Version == 0 {
		r, err = m.u.q.InsertMigrationDifference(ctx, sqlcdb.InsertMigrationDifferenceParams{RunID: d.RunID, LineKey: d.Line, Class: d.Class, Note: d.Note,
			FyniqFigure: d.Fyniq, WorkbookFigure: d.Workbook, DecidedBy: d.DecidedBy, DecidedAt: d.DecidedAt})
		if errors.Is(err, pgx.ErrNoRows) {
			return storage.MigrationDecision{}, changed("already")
		}
	} else {
		was, gerr := m.u.q.GetMigrationDifference(ctx, sqlcdb.GetMigrationDifferenceParams{RunID: d.RunID, LineKey: d.Line})
		if gerr != nil {
			return storage.MigrationDecision{}, mapError(gerr)
		}
		w := toMigrationDecision(was)
		before = &w
		r, err = m.u.q.UpdateMigrationDifference(ctx, sqlcdb.UpdateMigrationDifferenceParams{RunID: d.RunID, LineKey: d.Line, Version: int32(d.Version), // #nosec G115 -- a version
			Class: d.Class, Note: d.Note, FyniqFigure: d.Fyniq, WorkbookFigure: d.Workbook, DecidedBy: d.DecidedBy, DecidedAt: d.DecidedAt})
		if errors.Is(err, pgx.ErrNoRows) {
			return storage.MigrationDecision{}, changed("again")
		}
	}
	if err != nil {
		return storage.MigrationDecision{}, mapError(err)
	}
	out := toMigrationDecision(r)
	field := func(was, now string) [2]any {
		if before == nil {
			return [2]any{nil, now}
		}
		return [2]any{was, now}
	}
	var b storage.MigrationDecision
	if before != nil {
		b = *before
	}
	changes := map[string][2]any{"line": {nil, out.Line}, "class": field(b.Class, out.Class), "note": field(b.Note, out.Note),
		"fyniq": field(b.Fyniq, out.Fyniq), "workbook": field(b.Workbook, out.Workbook)}
	return out, m.u.event(ctx, "migration.difference_decided", "migration_run", out.RunID, nil, changes)
}
