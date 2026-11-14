package migration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/sheet"
)

// ValidateKind is the job that validates an uploaded workbook in the background (design D2).
const ValidateKind = "migration.validate"

// MaxBytes is the largest workbook the import keeps: an evidence file's limit (FR-DOC-08).
const MaxBytes = 25 << 20

// Actor is who records a validation.
var Actor = storage.Actor{Type: "system", ID: ValidateKind}

// ChecksFileName is the name the Checks export is kept under.
const ChecksFileName = "checks.csv"

// StorageKey is where a migration's file is kept: in the staging store, under a prefix of its own that the evidence
// sweeper leaves alone, until M6 archives the workbook under retention.
func StorageKey(org, run, upload uuid.UUID) string {
	return "migrations/" + org.String() + "/" + run.String() + "/" + upload.String()
}

// ValidateItem is the job that validates a step's workbook.
func ValidateItem(step uuid.UUID) (storage.NewOutboxItem, error) {
	payload, err := json.Marshal(map[string]string{"step_id": step.String()})
	return storage.NewOutboxItem{Class: storage.ClassWorker, Kind: ValidateKind, Payload: payload, BusinessKey: "migration-step:" + step.String()}, err
}

// Validator validates uploaded workbooks, in the worker.
type Validator struct {
	Store storage.Store
	Files ports.BlobStore
	Clock ports.Clock
}

// Work validates one step's workbook: it reads the workbook as kept, checks it against the organisation inside a
// transaction that rolls back, so validating commits nothing of the import (FR-MIG-01), and keeps the report and the
// Checks export made from the workbook on the step. A step no longer validating needs nothing.
func (v Validator) Work() func(ctx context.Context, org uuid.UUID, row storage.OutboxRow) error {
	return func(ctx context.Context, org uuid.UUID, row storage.OutboxRow) error {
		var job struct {
			ID uuid.UUID `json:"step_id"`
		}
		if err := json.Unmarshal(row.Payload, &job); err != nil {
			return err
		}
		if v.Files == nil {
			return errors.New("migration: no file store is configured")
		}
		ctx = storage.WithAudit(ctx, storage.AuditContext{Actor: &Actor})
		var step storage.MigrationStep
		var file storage.MigrationFile
		err := v.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			if step, err = u.Migrations().Step(ctx, job.ID); err != nil || step.State != storage.StepValidating {
				return err
			}
			files, err := u.Migrations().Files(ctx, step.RunID)
			for _, f := range files {
				if f.ID == step.WorkbookFileID {
					file = f
				}
			}
			if err == nil && file.ID == uuid.Nil {
				err = fmt.Errorf("migration: step %s names no workbook of its migration", step.ID)
			}
			return err
		})
		if err != nil || step.State != storage.StepValidating {
			return err
		}
		report, checks, err := v.validate(ctx, org, file)
		if err != nil {
			return err
		}
		sealed, sum, err := report.Sealed()
		if err != nil {
			return err
		}
		key := StorageKey(org, step.RunID, uuid.Must(uuid.NewV7()))
		if err := v.Files.Put(ctx, key, bytes.NewReader(checks)); err != nil {
			return err
		}
		hash := hashOf(checks)
		state := storage.StepValid
		if report.Errors > 0 {
			state = storage.StepInvalid
		}
		now := v.Clock.Now()
		err = v.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Migrations().Validated(ctx, step.ID, storage.MigrationFile{FileName: ChecksFileName, MediaType: "text/csv",
				Size: int64(len(checks)), SHA256: hash, OriginalSHA256: hash, StorageKey: key, DataClass: storage.ClassGeneral,
				AddedBy: Actor.Type + ":" + Actor.ID, AddedAt: now},
				storage.MigrationReport{State: state, Report: sealed, ReportSHA256: sum, Errors: report.Errors, Warnings: report.Warnings, ValidatedAt: now})
			return err
		})
		if err != nil {
			_ = v.Files.Delete(context.WithoutCancel(ctx), key) // no row names it
			var se *storage.Error
			if errors.As(err, &se) && se.Code == storage.CodeMigration {
				return nil // validated meanwhile
			}
		}
		return err
	}
}

func hashOf(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// validate reads the workbook as kept and checks it: its report, and the Checks export made from it.
func (v Validator) validate(ctx context.Context, org uuid.UUID, file storage.MigrationFile) (Report, []byte, error) {
	empty := (&Workbook{}).ChecksExport()
	rc, err := v.Files.Get(ctx, file.StorageKey)
	if err != nil {
		return Report{}, nil, err
	}
	data, err := io.ReadAll(io.LimitReader(rc, MaxBytes+1))
	_ = rc.Close()
	if err != nil {
		return Report{}, nil, err
	}
	if hashOf(data) != file.SHA256 {
		return Unreadable(file.FileName, file.SHA256, Problem{Severity: Error, Code: CodeWorkbook,
			Message: "the workbook kept is not the one uploaded: its SHA-256 differs",
			Fix:     "Upload the workbook again"}), empty, nil
	}
	w, err := Read(data)
	if re := (*sheet.ReadError)(nil); errors.As(err, &re) {
		return Unreadable(file.FileName, file.SHA256, Problem{Severity: Error, Code: CodeWorkbook, Message: re.Message, Fix: re.Fix}), empty, nil
	}
	if err != nil {
		return Report{}, nil, err
	}
	w.Freeze()
	// Checked against the organisation inside a transaction that rolls back: validating commits nothing (design D2).
	err = v.Store.Do(storage.WithPreview(ctx), org, func(ctx context.Context, u storage.UnitOfWork) error { return w.Against(ctx, u) })
	if err != nil && !errors.Is(err, storage.ErrPreviewed) {
		return Report{}, nil, err
	}
	return w.Report(file.FileName, file.SHA256), w.ChecksExport(), nil
}

// Against checks the workbook against the organisation it goes into: one without a posted entry (rev 0.254), with the
// workbook's base currency. The mapping (M2) and each record's command preview add their checks here as they are built.
func (w *Workbook) Against(ctx context.Context, u storage.UnitOfWork) error {
	posted, err := u.Migrations().Posted(ctx)
	if err != nil {
		return err
	}
	if posted {
		w.Problems = append(w.Problems, Problem{Severity: Error, Code: CodePosted, Message: "the organisation holds posted entries",
			Fix: "Import the workbook into a new organisation, never into live books"})
	}
	o, err := u.Organisation(ctx)
	if err != nil {
		return err
	}
	if b := w.Settings.BaseCurrency; b != "" && o.BaseCurrency != b {
		w.Problems = append(w.Problems, Problem{Sheet: "Settings", Column: "Base (reporting) currency", Severity: Error, Code: CodeOrganisation,
			Message: fmt.Sprintf("the workbook's base currency is %s; the organisation's is %s", b, o.BaseCurrency),
			Fix:     fmt.Sprintf("Import the workbook into an organisation whose base currency is %s", b)})
	}
	return nil
}
