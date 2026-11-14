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
	"strings"
	"time"

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
		report, checks, proposal, err := v.validate(ctx, org, step.RunID, file)
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
			if err != nil || state != storage.StepValid || proposal == nil {
				return err
			}
			// A valid workbook's mapping is proposed as the mapping's next version, for the Accountant to review and sign (M2).
			return remap(ctx, u, step.RunID, step.ID, *proposal, Actor.Type+":"+Actor.ID, now)
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

// validate reads the workbook as kept and checks it: its report, the Checks export made from it, and the mapping it
// proposes.
func (v Validator) validate(ctx context.Context, org, run uuid.UUID, file storage.MigrationFile) (Report, []byte, *Mapping, error) {
	empty := (&Workbook{}).ChecksExport()
	rc, err := v.Files.Get(ctx, file.StorageKey)
	if err != nil {
		return Report{}, nil, nil, err
	}
	data, err := io.ReadAll(io.LimitReader(rc, MaxBytes+1))
	_ = rc.Close()
	if err != nil {
		return Report{}, nil, nil, err
	}
	if hashOf(data) != file.SHA256 {
		return Unreadable(file.FileName, file.SHA256, Problem{Severity: Error, Code: CodeWorkbook,
			Message: "the workbook kept is not the one uploaded: its SHA-256 differs",
			Fix:     "Upload the workbook again"}), empty, nil, nil
	}
	w, err := Read(data)
	if re := (*sheet.ReadError)(nil); errors.As(err, &re) {
		return Unreadable(file.FileName, file.SHA256, Problem{Severity: Error, Code: CodeWorkbook, Message: re.Message, Fix: re.Fix}), empty, nil, nil
	}
	if err != nil {
		return Report{}, nil, nil, err
	}
	w.Freeze()
	// Checked against the organisation inside a transaction that rolls back: validating commits nothing (design D2).
	var proposal *Mapping
	err = v.Store.Do(storage.WithPreview(ctx), org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		proposal, err = w.Against(ctx, u, run)
		return err
	})
	if err != nil && !errors.Is(err, storage.ErrPreviewed) {
		return Report{}, nil, nil, err
	}
	return w.Report(file.FileName, file.SHA256), w.ChecksExport(), proposal, nil
}

// remap keeps m as the migration's next mapping version, of the workbook step validated.
func remap(ctx context.Context, u storage.UnitOfWork, run, step uuid.UUID, m Mapping, by string, at time.Time) error {
	version := 1
	latest, err := u.Migrations().Mapping(ctx, run)
	switch {
	case err == nil:
		version = latest.Version + 1
	case !errors.Is(err, ports.ErrNotFound):
		return err
	}
	sealed, sum, err := m.Sealed()
	if err != nil {
		return err
	}
	_, err = u.Migrations().Map(ctx, storage.MigrationMapping{RunID: run, Version: version, StepID: step, Mapping: sealed, SHA256: sum, MadeBy: by, MadeAt: at})
	return err
}

// LatestMapping is a migration's latest mapping and its version, or nil when it has none yet.
func LatestMapping(ctx context.Context, u storage.UnitOfWork, run uuid.UUID) (*Mapping, storage.MigrationMapping, error) {
	latest, err := u.Migrations().Mapping(ctx, run)
	if errors.Is(err, ports.ErrNotFound) {
		return nil, latest, nil
	}
	if err != nil {
		return nil, latest, err
	}
	var m Mapping
	if err := json.Unmarshal(latest.Mapping, &m); err != nil {
		return nil, latest, err
	}
	return &m, latest, nil
}

// Against checks the workbook against the organisation it goes into: one without a posted entry (rev 0.254), with the
// workbook's base currency; and proposes its mapping onto the organisation's set-up (M2), keeping the choices of the
// migration's mapping before it. What the mapping gets wrong is reported as a warning, fixed in the mapping before
// the Accountant signs it. A UEN not in ACRA's form and a bank rule to a money account are warnings, left out of the
// import; two rows the commit would import as one record are an error.
func (w *Workbook) Against(ctx context.Context, u storage.UnitOfWork, run uuid.UUID) (*Mapping, error) {
	posted, err := u.Migrations().Posted(ctx)
	if err != nil {
		return nil, err
	}
	if posted {
		w.Problems = append(w.Problems, Problem{Severity: Error, Code: CodePosted, Message: "the organisation holds posted entries",
			Fix: "Import the workbook into a new organisation, never into live books"})
	}
	o, err := u.Organisation(ctx)
	if err != nil {
		return nil, err
	}
	if b := w.Settings.BaseCurrency; b != "" && o.BaseCurrency != b {
		w.Problems = append(w.Problems, Problem{Sheet: "Settings", Column: "Base (reporting) currency", Severity: Error, Code: CodeOrganisation,
			Message: fmt.Sprintf("the workbook's base currency is %s; the organisation's is %s", b, o.BaseCurrency),
			Fix:     fmt.Sprintf("Import the workbook into an organisation whose base currency is %s", b)})
	}
	if uen := strings.TrimSpace(w.Settings.UEN); uen != "" && !validUEN(uen) {
		w.Problems = append(w.Problems, Problem{Sheet: "Settings", Column: "UEN", Severity: Warning, Code: CodeValue,
			Message: fmt.Sprintf("%q is not a UEN in ACRA's form: the import leaves it out", uen),
			Fix:     "Enter the UEN in Settings › Organisation once the import is committed, or correct it in the workbook"})
	}
	for i, r := range w.BankRules {
		if w.transferRule(i + 1) {
			w.Problems = append(w.Problems, Problem{Sheet: "Lists", Row: r.Row, Column: "…use category", Severity: Warning, Code: CodeArchived,
				Message: fmt.Sprintf("bank rule %d (%q) gives the money account %s: Fyniq records money moving between accounts as a transfer, so the rule is kept in the archive", i+1, r.Contains, codeOf(r.Account)),
				Fix:     "Nothing to do: the bank import matches both sides of a transfer"})
		}
	}
	target, err := TargetOf(ctx, u, w.Settings.BooksStart)
	if err != nil {
		return nil, err
	}
	previous, _, err := LatestMapping(ctx, u, run)
	if err != nil {
		return nil, err
	}
	m := w.Propose(target, previous)
	for _, p := range m.Check(target) {
		p.Severity, p.Fix = Warning, p.Fix+", in the mapping before it is signed"
		w.Problems = append(w.Problems, p)
	}
	// Two rows the commit would import as one record (rev 0.258).
	_, dup := w.Plan(m, storage.PathHistory)
	w.Problems = append(w.Problems, dup...)
	return &m, nil
}
