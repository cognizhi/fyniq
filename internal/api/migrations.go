package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/migration"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the workbook migration (FR-MIG-01, §17.1)

// MigrationView is the organisation's move from the workbook (p1-workbook-migration design D3, D11): its steps and the
// files they keep.
type MigrationView struct {
	ID        string              `json:"id" format:"uuid" doc:"The migration." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6a10"`
	Status    string              `json:"status" enum:"open,closed" doc:"open until M6 archives the workbook and closes it." example:"open"`
	StartedBy string              `json:"started_by" doc:"Who started it, as records name actors." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	StartedAt string              `json:"started_at" format:"date-time" doc:"When it started." example:"2026-11-13T09:00:00Z"`
	Steps     []MigrationStepView `json:"steps" nullable:"false" doc:"Each attempt at a step, oldest first: M1's are the workbooks uploaded and validated."`
	Files     []MigrationFileView `json:"files" nullable:"false" doc:"The files the migration keeps, oldest first: each workbook as uploaded and each Checks export made from one."`
}

// MigrationStepView is an attempt at one of §17.1's steps.
type MigrationStepView struct {
	ID             string `json:"id" format:"uuid" doc:"The step." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6a20"`
	Step           string `json:"step" enum:"M1,M2,M3,M4,M5,M6" doc:"§17.1's step: M1 is the frozen workbook uploaded and validated." example:"M1"`
	State          string `json:"state" enum:"validating,valid,invalid" doc:"validating in the background; then valid, or invalid when its report holds an error." example:"valid"`
	WorkbookFileID string `json:"workbook_file_id" format:"uuid" doc:"The workbook it validates." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6a30"`
	ChecksFileID   string `json:"checks_file_id,omitempty" format:"uuid" doc:"The Checks export made from the workbook, once validated: M1's evidence with the workbook." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6a40"`
	ReportSHA256   string `json:"report_sha256,omitempty" doc:"The SHA-256 of the validation report as kept, once validated: what a sign-off names." example:"9f2c4e…"`
	Errors         *int   `json:"errors,omitempty" doc:"The report's errors, once validated: any stops the commit." example:"0"`
	Warnings       *int   `json:"warnings,omitempty" doc:"The report's warnings, once validated." example:"1"`
	StartedBy      string `json:"started_by" doc:"Who uploaded the workbook." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	StartedAt      string `json:"started_at" format:"date-time" doc:"When." example:"2026-11-13T09:05:00Z"`
	ValidatedAt    string `json:"validated_at,omitempty" format:"date-time" doc:"When it was validated; absent while validating." example:"2026-11-13T09:05:20Z"`
}

// MigrationFileView is a file a migration keeps.
type MigrationFileView struct {
	ID             string `json:"id" format:"uuid" doc:"The file." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6a30"`
	Kind           string `json:"kind" enum:"workbook,checks" doc:"workbook: as uploaded; checks: the Checks export made from one." example:"workbook"`
	FileName       string `json:"file_name" doc:"As the uploader named it, or checks.csv." example:"Cognizhi-Finance-Hub.xlsx"`
	MediaType      string `json:"media_type" doc:"Decided from the content." example:"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"`
	Size           int64  `json:"size_bytes" doc:"Its size in bytes." example:"6684378"`
	SHA256         string `json:"sha256" doc:"The SHA-256 of the file as kept." example:"4d8057dc…"`
	OriginalSHA256 string `json:"original_sha256" doc:"The SHA-256 of the file as received." example:"4d8057dc…"`
	DataClass      string `json:"data_class" enum:"general,payroll" doc:"payroll for a workbook, which holds the pay of each employee; general for a Checks export." example:"payroll"`
	AddedBy        string `json:"added_by" doc:"Who added it: a person, or the validation." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	AddedAt        string `json:"added_at" format:"date-time" doc:"When." example:"2026-11-13T09:05:00Z"`
}

// MigrationUpload is a workbook uploaded to a migration.
type MigrationUpload struct {
	File huma.FormFile `form:"file" contentType:"application/octet-stream" required:"true" doc:"The frozen workbook: the published .xlsx, saved after make verify, up to 25 MB. Its type is decided from its content and it is scanned for malware, as evidence is."`
}

// MigrationUploadView is the step that validates an uploaded workbook in the background.
type MigrationUploadView struct {
	Step        MigrationStepView `json:"step" doc:"The step, validating."`
	File        MigrationFileView `json:"file" doc:"The workbook as kept."`
	OperationID string            `json:"operation_id,omitempty" format:"uuid" doc:"The operation that validates it: poll it, then read the report. Absent for a preview." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e10"`
}

// MigrationProblem is something the validation report says about the workbook.
type MigrationProblem struct {
	Sheet    string `json:"sheet,omitempty" doc:"The sheet; absent for the workbook or the organisation as a whole." example:"Transactions"`
	Row      int    `json:"row,omitempty" doc:"The sheet's row; absent for the sheet as a whole." example:"8"`
	Column   string `json:"column,omitempty" doc:"The column, by its header." example:"Money out"`
	Severity string `json:"severity" enum:"error,warning" doc:"An error stops the commit; a warning does not." example:"error"`
	Code     string `json:"code" doc:"The problem's code: MIGRATION-01 not the workbook; -02 a sheet missing; -03 a column missing; -04 a value not what its column holds; -05 saved without its calculated values; -06 a row the workbook did not post; -07 kept in the archive; -08 a check showing ✖; -09 the organisation holds posted entries; -10 the organisation is not set up as the workbook is; -11 the mapping does not fit the set-up; -13 two rows would import as one record." example:"MIGRATION-04"`
	Message  string `json:"message" doc:"What is wrong." example:"98.125 has more than 2 decimals"`
	Fix      string `json:"fix" doc:"How to fix it." example:"Enter it with at most 2 decimals, as the currency has"`
}

// MigrationSheetCount is a sheet's rows the import reads, and its problems.
type MigrationSheetCount struct {
	Sheet    string `json:"sheet" doc:"The sheet." example:"Transactions"`
	Rows     int    `json:"rows" doc:"Its rows the import reads." example:"178"`
	Errors   int    `json:"errors" doc:"Its errors." example:"0"`
	Warnings int    `json:"warnings" doc:"Its warnings." example:"1"`
}

// MigrationReportView is M1's latest validation report (FR-MIG-01, design D2): what the workbook holds and every
// problem found, before anything is committed.
type MigrationReportView struct {
	StepID       string                `json:"step_id" format:"uuid" doc:"The step that validated it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6a20"`
	State        string                `json:"state" enum:"validating,valid,invalid" doc:"validating: poll the operation, then read again; valid or invalid with the report." example:"valid"`
	ReportSHA256 string                `json:"report_sha256,omitempty" doc:"The SHA-256 of the report as kept." example:"9f2c4e…"`
	ValidatedAt  string                `json:"validated_at,omitempty" format:"date-time" doc:"When it was validated." example:"2026-11-13T09:05:20Z"`
	Format       string                `json:"format,omitempty" doc:"The report's format." example:"fyniq-migration-report@1"`
	FileName     string                `json:"file_name,omitempty" doc:"The workbook's name as uploaded." example:"Cognizhi-Finance-Hub.xlsx"`
	SHA256       string                `json:"sha256,omitempty" doc:"The workbook's SHA-256." example:"4d8057dc…"`
	Company      string                `json:"company,omitempty" doc:"The company the workbook keeps, from its Settings." example:"Cognizhi Pte. Ltd."`
	BaseCurrency string                `json:"base_currency,omitempty" doc:"Its base currency." example:"SGD"`
	BooksStart   string                `json:"books_start,omitempty" format:"date" doc:"Its books start." example:"2026-01-01"`
	YearStart    string                `json:"year_start,omitempty" format:"date" doc:"Its financial year's start." example:"2026-01-01"`
	AsOf         string                `json:"as_of,omitempty" format:"date" doc:"Its reports' as-of date: the cut-over a clean start carries forward to." example:"2026-09-30"`
	ClosedTo     string                `json:"closed_to,omitempty" format:"date" doc:"The day its books are closed up to." example:"2026-08-31"`
	Sheets       []MigrationSheetCount `json:"sheets,omitempty" doc:"Each sheet the import reads, in order."`
	Problems     []MigrationProblem    `json:"problems,omitempty" doc:"Every problem, by sheet and row."`
	Errors       int                   `json:"errors" doc:"The errors: any stops the commit." example:"0"`
	Warnings     int                   `json:"warnings" doc:"The warnings." example:"1"`
}

// MigrationPath names a migration.
type MigrationPath struct {
	MigrationID string `path:"migration_id" format:"uuid" doc:"The migration." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6a10"`
}

type (
	migrationsOutput struct {
		Body struct {
			Items []MigrationView `json:"items" nullable:"false" doc:"The organisation's migration: at most one."`
		}
	}
	migrationStartInput struct {
		KeyHeader
		PreferHeader
	}
	migrationOutput struct {
		Status   int
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Location string `header:"Location" doc:"The migration."`
		ETag     string `header:"ETag" doc:"Its version and status, for If-Match."`
		Body     MigrationView
	}
	migrationReadOutput struct {
		ETag string `header:"ETag" doc:"Its version and status, for If-Match."`
		Body MigrationView
	}
	migrationUploadInput struct {
		MigrationPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		RawBody huma.MultipartFormFiles[MigrationUpload]
	}
	migrationUploadOutput struct {
		Status   int
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Location string `header:"Location" doc:"The operation that validates the workbook: poll it."`
		Body     MigrationUploadView
	}
	migrationReportOutput struct{ Body MigrationReportView }
)

func at(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func migrationStepView(s storage.MigrationStep) MigrationStepView {
	v := MigrationStepView{ID: s.ID.String(), Step: s.Step, State: s.State, WorkbookFileID: s.WorkbookFileID.String(), StartedBy: s.StartedBy,
		StartedAt: at(s.StartedAt)}
	if s.ValidatedAt != nil {
		errs, warns := s.Errors, s.Warnings
		v.ReportSHA256, v.Errors, v.Warnings, v.ValidatedAt = s.ReportSHA256, &errs, &warns, at(*s.ValidatedAt)
	}
	if s.ChecksFileID != nil {
		v.ChecksFileID = s.ChecksFileID.String()
	}
	return v
}

func migrationFileView(f storage.MigrationFile) MigrationFileView {
	return MigrationFileView{ID: f.ID.String(), Kind: f.Kind, FileName: f.FileName, MediaType: f.MediaType, Size: f.Size, SHA256: f.SHA256,
		OriginalSHA256: f.OriginalSHA256, DataClass: string(f.DataClass), AddedBy: f.AddedBy, AddedAt: at(f.AddedAt)}
}

// migrationOf is a migration with its steps and files, and its ETag.
func migrationOf(ctx context.Context, u storage.UnitOfWork, r storage.MigrationRun) (MigrationView, string, error) {
	v := MigrationView{ID: r.ID.String(), Status: r.Status, StartedBy: r.StartedBy, StartedAt: at(r.StartedAt), Steps: []MigrationStepView{},
		Files: []MigrationFileView{}}
	steps, err := u.Migrations().Steps(ctx, r.ID)
	if err != nil {
		return v, "", err
	}
	for _, s := range steps {
		v.Steps = append(v.Steps, migrationStepView(s))
	}
	files, err := u.Migrations().Files(ctx, r.ID)
	for _, f := range files {
		v.Files = append(v.Files, migrationFileView(f))
	}
	return v, migrationETag(r, steps), err
}

// migrationETag is a migration's entity tag: its version counts each step started and each validated, so it moves with
// every change a command makes, and its status.
func migrationETag(r storage.MigrationRun, steps []storage.MigrationStep) string {
	v := 1
	for _, s := range steps {
		v++
		if s.ValidatedAt != nil {
			v++
		}
	}
	return etag(v, r.Status)
}

// migrationRun is the migration a request names: CodeNotFound when the organisation has no such one.
func migrationRun(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.MigrationRun, error) {
	r, err := u.Migrations().Run(ctx, id)
	if errors.Is(err, ports.ErrNotFound) {
		return r, problem(CodeNotFound, "The organisation has no such migration.", "List the migrations with listMigrations.")
	}
	return r, err
}

func registerMigrations(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, storage.CodeMigration, storage.CodeDuplicate}, locks...)

	register(a, authz.ListMigrations, exempt(huma.Operation{
		OperationID: "listMigrations", Method: http.MethodGet, Path: "/v1/migrations", Tags: []string{"Migration"},
		Summary: "List the migrations",
		Description: "The organisation's move from the workbook (FR-MIG-01, §17.1): at most one, with each attempt at its steps and the files " +
			"they keep. Read-only; no side effects.",
	}, "cursor", "An organisation has at most one migration (rev 0.254), read whole."), locks,
		func(ctx context.Context, _ *struct{}) (*migrationsOutput, error) {
			p, _ := authn.From(ctx)
			out := &migrationsOutput{}
			out.Body.Items = []MigrationView{}
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				runs, err := u.Migrations().Runs(ctx)
				for _, r := range runs {
					v, _, err := migrationOf(ctx, u, r)
					if err != nil {
						return err
					}
					out.Body.Items = append(out.Body.Items, v)
				}
				return err
			})
			return out, err
		})

	register(a, authz.StartMigration, huma.Operation{
		OperationID: "startMigration", Method: http.MethodPost, Path: "/v1/migrations", Tags: []string{"Migration"},
		DefaultStatus: http.StatusCreated, Summary: "Start the migration from the workbook",
		Description: "Starts the organisation's move from the workbook (FR-MIG-01, §17.1), whose first step, M1, uploads the frozen workbook. " +
			"An organisation has one migration: once started, this answers 200 with it. Only an organisation without a posted entry " +
			"starts one (MIGRATION-09, rev 0.254): a failed or rejected import starts again in a new organisation, never in live books. " +
			"Side effects: the migration, the audit event migration.started. Idempotent with Idempotency-Key; Prefer: return=preview " +
			"saves nothing.",
	}, append([]string{migration.CodePosted}, writes...), func(ctx context.Context, in *migrationStartInput) (*migrationOutput, error) {
		now := a.deps.Clock.Now()
		p, _ := authn.From(ctx)
		var started *MigrationView
		var tag string
		if err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			runs, err := u.Migrations().Runs(ctx)
			if err != nil || len(runs) == 0 {
				return err
			}
			v, t, err := migrationOf(ctx, u, runs[0])
			started, tag = &v, t
			return err
		}); err != nil {
			return nil, err
		}
		if started != nil {
			return &migrationOutput{Status: http.StatusOK, Location: "/v1/migrations/" + started.ID, ETag: tag, Body: *started}, nil
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, struct{}{}, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (MigrationView, error) {
			posted, err := u.Migrations().Posted(ctx)
			if err != nil {
				return MigrationView{}, err
			}
			if posted {
				return MigrationView{}, problem(migration.CodePosted, "The organisation holds posted entries, so the workbook cannot be imported into it.",
					"Import the workbook into a new organisation, never into live books.")
			}
			r, err := u.Migrations().Start(ctx, uploader(ctx), now)
			if err != nil {
				return MigrationView{}, err
			}
			v, _, err := migrationOf(ctx, u, r)
			return v, err
		})
		if err != nil {
			return nil, err
		}
		out := &migrationOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}
		if !w.Previewed {
			out.Location, out.ETag = "/v1/migrations/"+w.Body.ID, etag(1, w.Body.Status)
		}
		return out, nil
	})

	register(a, authz.GetMigration, huma.Operation{
		OperationID: "getMigration", Method: http.MethodGet, Path: "/v1/migrations/{migration_id}", Tags: []string{"Migration"},
		Summary:     "Get the migration",
		Description: "The migration with each attempt at its steps and the files they keep (FR-MIG-01, §17.1). Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *MigrationPath) (*migrationReadOutput, error) {
		id, err := parseID("migration_id", in.MigrationID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := &migrationReadOutput{}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			r, err := migrationRun(ctx, u, id)
			if err != nil {
				return err
			}
			out.Body, out.ETag, err = migrationOf(ctx, u, r)
			return err
		})
		return out, err
	})

	register(a, authz.UploadMigrationWorkbook, huma.Operation{
		OperationID: "uploadMigrationWorkbook", Method: http.MethodPost, Path: "/v1/migrations/{migration_id}/workbook", Tags: []string{"Migration"},
		DefaultStatus: http.StatusAccepted, Summary: "Upload the frozen workbook",
		Description: "M1 (§17.1, FR-MIG-01): uploads the frozen workbook, as multipart/form-data, and validates it in the background. The " +
			"file is an untrusted upload: its type is decided from its content and it is scanned for malware, as evidence is (FR-DOC-08), " +
			"then kept as uploaded, hashed, as M1's evidence (personal data: the workbook holds each employee's pay). The validation reads " +
			"every sheet the import uses within the workbook's bounds and checks it against the organisation inside a transaction that " +
			"rolls back, so it commits nothing of the import; its report and the Checks export made from the workbook are kept on the " +
			"step. The answer is 202 with the step and the operation to poll; then read getMigrationReport. Uploading again, once the " +
			"last validation is done, validates the new workbook as a new attempt, until the import is committed (MIGRATION-14). Needs If-Match with the migration's ETag, so the " +
			"workbook goes on the migration as you read it. Side effects: the file, the step, the job, the audit " +
			"event migration.workbook_received, and later migration.validated. Idempotent with Idempotency-Key; Prefer: return=preview " +
			"checks the file and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeUnavailable,
		migration.CodeCommit}, append(writes, EvidenceCodes...)...),
		func(ctx context.Context, in *migrationUploadInput) (*migrationUploadOutput, error) {
			return a.uploadWorkbook(ctx, in, storage.StepWorkbook, migrationFixed)
		})

	if op := a.OpenAPI().Paths["/v1/migrations/{migration_id}/workbook"].Post; op != nil && op.RequestBody != nil {
		if mt := op.RequestBody.Content["multipart/form-data"]; mt != nil && mt.Schema != nil && mt.Schema.Properties["file"] != nil {
			mt.Schema.Properties["file"].Examples = []any{"PK… (the workbook's bytes)"}
		}
	}

	register(a, authz.GetMigrationReport, huma.Operation{
		OperationID: "getMigrationReport", Method: http.MethodGet, Path: "/v1/migrations/{migration_id}/report", Tags: []string{"Migration"},
		Summary: "Get the validation report",
		Description: "M1's latest validation report (FR-MIG-01, design D2): the workbook's company, base currency and dates, each sheet's rows " +
			"the import reads, and every problem with its sheet, row and column, what is wrong and how to fix it. An error stops the " +
			"commit; a row the workbook did not post (✖) is a warning and is not imported. While the workbook is validating, the state " +
			"says so: poll the operation, then read again. Before a workbook is uploaded there is none to read. Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *MigrationPath) (*migrationReportOutput, error) {
		id, err := parseID("migration_id", in.MigrationID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := &migrationReportOutput{}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			r, err := migrationRun(ctx, u, id)
			if err != nil {
				return err
			}
			steps, err := u.Migrations().Steps(ctx, r.ID)
			if err != nil {
				return err
			}
			var last *storage.MigrationStep
			for i := range steps {
				if steps[i].Step == storage.StepWorkbook {
					last = &steps[i]
				}
			}
			if last == nil {
				return problem(CodeNotFound, "No workbook has been uploaded to the migration yet.", "Upload the frozen workbook with uploadMigrationWorkbook.")
			}
			out.Body, err = migrationReportOf(*last)
			return err
		})
		return out, err
	})
}

// migrationReportOf is a step's report as the API shows it.
func migrationReportOf(s storage.MigrationStep) (MigrationReportView, error) {
	v := MigrationReportView{StepID: s.ID.String(), State: s.State, ReportSHA256: s.ReportSHA256}
	if s.ValidatedAt == nil {
		return v, nil
	}
	v.ValidatedAt = at(*s.ValidatedAt)
	var r migration.Report
	if err := json.Unmarshal(s.Report, &r); err != nil {
		return v, err
	}
	f := r.Workbook
	v.Format, v.FileName, v.SHA256, v.Company, v.BaseCurrency = r.Format, f.FileName, f.SHA256, f.Company, f.BaseCurrency
	v.BooksStart, v.YearStart, v.AsOf, v.ClosedTo, v.Errors, v.Warnings = f.BooksStart, f.YearStart, f.AsOf, f.ClosedTo, r.Errors, r.Warnings
	for _, c := range r.Sheets {
		v.Sheets = append(v.Sheets, MigrationSheetCount{Sheet: c.Sheet, Rows: c.Rows, Errors: c.Errors, Warnings: c.Warnings})
	}
	for _, p := range r.Problems {
		v.Problems = append(v.Problems, MigrationProblem{Sheet: p.Sheet, Row: p.Row, Column: p.Column, Severity: string(p.Severity), Code: p.Code,
			Message: p.Message, Fix: p.Fix})
	}
	return v, nil
}

// uploadWorkbook keeps an uploaded workbook as a step's evidence and validates it in the background: M1's frozen
// workbook, or M5's at the end of the parallel month (rev 0.268), each once ready says the migration may take it.
func (a *API) uploadWorkbook(ctx context.Context, in *migrationUploadInput, step string, ready func(ctx context.Context, u storage.UnitOfWork, run uuid.UUID) error) (*migrationUploadOutput, error) {
	id, err := parseID("migration_id", in.MigrationID)
	if err != nil {
		return nil, err
	}
	p, _ := authn.From(ctx)
	if a.deps.Staging == nil || a.deps.Scanner == nil {
		return nil, problem(CodeUnavailable, "Workbooks cannot be uploaded here: no file store or malware scanner is configured.",
			"Ask your administrator to configure them (FR-DOC-01).")
	}
	up := in.RawBody.Data()
	if up == nil || up.File.File == nil {
		return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.file", Message: "is missing", Fix: "Send the workbook in the form field file"}})
	}
	defer up.File.Close()
	data, err := io.ReadAll(io.LimitReader(up.File, migration.MaxBytes+1))
	if err != nil {
		return nil, problem(CodeMalformed, "The file could not be read in full.", "Send it again.")
	}
	file, problems, err := evidence.Check(ctx, a.deps.Scanner, up.File.Filename, data)
	if err != nil {
		logFailure(ctx, err)
		return nil, problem(CodeUnavailable, "The malware scan is not available, so the workbook was not kept.", "Try again in a few minutes.")
	}
	var bad []FieldProblem
	for _, x := range problems {
		bad = append(bad, FieldProblem{Code: x.Code, Field: "body." + x.Field, Message: x.Message, Fix: x.Fix})
	}
	if len(bad) == 0 && file.MediaType != evidence.XLSX {
		bad = append(bad, FieldProblem{Code: evidence.CodeType, Field: "body.file", Message: "is not an .xlsx workbook",
			Fix: "Upload the workbook as the published .xlsx, saved after make verify"})
	}
	if len(bad) > 0 {
		return nil, invalid(bad)
	}
	now := a.deps.Clock.Now()
	key := migration.StorageKey(p.Organisation, id, uuid.Must(uuid.NewV7()))
	if !wantsPreview(in.Prefer) {
		if err := a.deps.Staging.Put(ctx, key, bytes.NewReader(file.Data)); err != nil {
			logFailure(ctx, err)
			return nil, problem(CodeUnavailable, "The workbook could not be stored.", "Try again in a few minutes.")
		}
	}
	request := struct{ Migration, SHA256, Name string }{in.MigrationID, file.OriginalSHA256, file.Name}
	w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, request, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (MigrationUploadView, error) {
		r, err := migrationRun(ctx, u, id)
		if err != nil {
			return MigrationUploadView{}, err
		}
		steps, err := u.Migrations().Steps(ctx, r.ID)
		if err != nil {
			return MigrationUploadView{}, err
		}
		if err := checkIfMatch(in.IfMatch, migrationETag(r, steps)); err != nil {
			return MigrationUploadView{}, err
		}
		if err := ready(ctx, u, r.ID); err != nil {
			return MigrationUploadView{}, err
		}
		step, kept, err := u.Migrations().AddWorkbook(ctx, step, storage.MigrationFile{RunID: r.ID, FileName: file.Name,
			MediaType: file.MediaType, Size: int64(len(file.Data)), SHA256: file.SHA256, OriginalSHA256: file.OriginalSHA256, StorageKey: key,
			DataClass: storage.ClassPayroll, AddedBy: uploader(ctx), AddedAt: now})
		if err != nil {
			return MigrationUploadView{}, err
		}
		item, err := migration.ValidateItem(step.ID)
		if err != nil {
			return MigrationUploadView{}, err
		}
		var job storage.OutboxRow
		if a.deps.Outbox != nil {
			job, err = a.deps.Outbox.Enqueue(ctx, u, p.Organisation, item)
		} else { // no dispatcher here: the tick sends it
			job, err = u.Outbox().Enqueue(ctx, item)
		}
		if err != nil {
			return MigrationUploadView{}, err
		}
		report := "/v1/migrations/" + r.ID.String() + "/report"
		if step.Step == storage.StepParallel {
			report = "/v1/migrations/" + r.ID.String() + "/parallel/report"
		}
		op, err := startOperation(ctx, u, "migration_validation", job.ID, report, now)
		return MigrationUploadView{Step: migrationStepView(step), File: migrationFileView(kept), OperationID: op.ID.String()}, err
	})
	if (err != nil || w.Replayed) && !wantsPreview(in.Prefer) {
		_ = a.deps.Staging.Delete(context.WithoutCancel(ctx), key) // no row names it
	}
	if err != nil {
		return nil, err
	}
	out := &migrationUploadOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}
	if w.Previewed {
		out.Body.OperationID = ""
	} else {
		out.Location = operationPath(w.Body.OperationID)
	}
	return out, nil
}
