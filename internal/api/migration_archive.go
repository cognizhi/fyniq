package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/migration"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the archive (§17.1 M6, FR-MIG-01; rev 0.269)

// MigrationArchiveCopy is a migration's file as M6 keeps it in the retention-locked store.
type MigrationArchiveCopy struct {
	FileID      string    `json:"file_id" format:"uuid" doc:"The file archived." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6a30"`
	Kind        string    `json:"kind" enum:"workbook,checks,pdf" doc:"workbook: a workbook as uploaded (M1's, or M5's); checks: the Checks export made from one; pdf: a PDF of the workbook's reports." example:"workbook"`
	FileName    string    `json:"file_name" doc:"As the uploader named it." example:"Cognizhi-Finance-Hub.xlsx"`
	SHA256      string    `json:"sha256" doc:"The copy's SHA-256, the file's as kept." example:"4d8057dc…"`
	RetainUntil time.Time `json:"retain_until" doc:"How long the store keeps it, never shortened: the end of the financial year it was archived in, plus the records' retention (FR-DOC-07, §11.9)." example:"2034-01-01T00:00:00+08:00"`
	ArchivedBy  string    `json:"archived_by" doc:"Who archived it." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4a11"`
	ArchivedAt  time.Time `json:"archived_at" doc:"When." example:"2026-11-30T09:00:00Z"`
}

// MigrationArchiveView is a migration's archive: its status and each file's copy.
type MigrationArchiveView struct {
	Status string                 `json:"status" enum:"open,closed" doc:"open until M6 archives the workbook and closes the migration; closed: read only." example:"closed"`
	Copies []MigrationArchiveCopy `json:"copies" nullable:"false" doc:"Each file's copy in the retention-locked store, in the order archived."`
}

type (
	migrationArchiveOutput struct {
		Body MigrationArchiveView
	}
	migrationArchiveInput struct {
		MigrationPath
		KeyHeader
		IfMatchHeader
		PreferHeader
	}
	migrationArchiveWriteOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"The migration's ETag."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     MigrationArchiveView
	}
)

// lockedKey is where M6 keeps a migration's file in the retention-locked store.
func lockedKey(org, run, file uuid.UUID) string {
	return "migrations/" + org.String() + "/" + run.String() + "/" + file.String()
}

// archiveOf is a migration's archive as kept.
func archiveOf(ctx context.Context, u storage.UnitOfWork, r storage.MigrationRun) (MigrationArchiveView, error) {
	v := MigrationArchiveView{Status: r.Status, Copies: []MigrationArchiveCopy{}}
	files, err := u.Migrations().Files(ctx, r.ID)
	if err != nil {
		return v, err
	}
	byID := map[uuid.UUID]storage.MigrationFile{}
	for _, f := range files {
		byID[f.ID] = f
	}
	copies, err := u.Migrations().Archives(ctx, r.ID)
	for _, c := range copies {
		f := byID[c.FileID]
		v.Copies = append(v.Copies, MigrationArchiveCopy{FileID: c.FileID.String(), Kind: f.Kind, FileName: f.FileName, SHA256: c.SHA256,
			RetainUntil: c.RetainUntil, ArchivedBy: c.ArchivedBy, ArchivedAt: c.ArchivedAt})
	}
	return v, err
}

// parallelSigned says whether the migration may be archived: its parallel month signed (M5).
func parallelSigned(ctx context.Context, u storage.UnitOfWork, run uuid.UUID) error {
	offs, err := u.Migrations().SignOffs(ctx, run)
	if err != nil {
		return err
	}
	for _, o := range offs {
		if o.Subject == storage.SignOffParallel {
			return nil
		}
	}
	return problem(migration.CodeParallel, "The parallel month is not signed yet, so the workbook cannot be archived.",
		"Reconcile the parallel month and sign it (M5) first.")
}

// copyLocked copies a migration's file write-once to the retention-locked store, checked against its hash on both
// sides, and keeps it until until: a copy made before, by an earlier attempt, is checked and kept.
func (a *API) copyLocked(ctx context.Context, f storage.MigrationFile, key string, until time.Time) error {
	read := func(b ports.BlobStore, key string) ([]byte, error) {
		rc, err := b.Get(ctx, key)
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(io.LimitReader(rc, migration.MaxBytes+1))
	}
	sum := func(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
	data, err := read(a.deps.Staging, f.StorageKey)
	if err != nil {
		return err
	}
	if sum(data) != f.SHA256 {
		return fmt.Errorf("migration file %s: %w", f.ID, evidence.ErrHashMismatch)
	}
	if err := a.deps.Evidence.Create(ctx, key, bytes.NewReader(data)); err != nil && !errors.Is(err, ports.ErrExists) {
		return err
	}
	locked, err := read(a.deps.Evidence, key)
	if err != nil {
		return err
	}
	if sum(locked) != f.SHA256 {
		return fmt.Errorf("migration file %s's locked copy: %w", f.ID, evidence.ErrHashMismatch)
	}
	if err := a.deps.Evidence.SetRetainUntil(ctx, key, until); err != nil && !errors.Is(err, ports.ErrRetentionShortened) {
		return err
	}
	return nil
}

func registerMigrationArchive(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeInvalid, CodeNotFound, CodeUnavailable,
		CodeIfMatchRequired, CodeStale, storage.CodeMigration}, locks...)

	register(a, authz.UploadMigrationArchivePDF, huma.Operation{
		OperationID: "uploadMigrationArchivePDF", Method: http.MethodPost, Path: "/v1/migrations/{migration_id}/archive/pdfs", Tags: []string{"Migration"},
		DefaultStatus: http.StatusCreated, Summary: "Add a PDF of the workbook's reports",
		Description: "M6 (§17.1, FR-MIG-01; rev 0.269): keeps a PDF of the workbook's reports (its statements, its GST return, its pay " +
			"summaries) on the migration, as multipart/form-data, for the archive, once the parity report is signed (MIGRATION-16; before the " +
			"commit, MIGRATION-14). An untrusted upload: its type is decided from its content and it is scanned for malware, as evidence is " +
			"(FR-DOC-08); it is kept as uploaded, hashed, as payroll data. Needs If-Match with the migration's ETag. Side effects: the file and " +
			"the audit event migration.pdf_received. Idempotent with Idempotency-Key; Prefer: return=preview checks the file and saves nothing.",
	}, append([]string{CodeMalformed, CodeTooLarge, CodeMediaType, migration.CodeCommit, migration.CodeParallel}, append(writes, EvidenceCodes...)...),
		func(ctx context.Context, in *migrationUploadInput) (*migrationUploadOutput, error) {
			id, err := parseID("migration_id", in.MigrationID)
			if err != nil {
				return nil, err
			}
			p, _ := authn.From(ctx)
			if a.deps.Staging == nil || a.deps.Scanner == nil {
				return nil, problem(CodeUnavailable, "Files cannot be uploaded here: no file store or malware scanner is configured.",
					"Ask your administrator to configure them (FR-DOC-01).")
			}
			up := in.RawBody.Data()
			if up == nil || up.File.File == nil {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.file", Message: "is missing", Fix: "Send the PDF in the form field file"}})
			}
			defer up.File.Close()
			data, err := io.ReadAll(io.LimitReader(up.File, migration.MaxBytes+1))
			if err != nil {
				return nil, problem(CodeMalformed, "The file could not be read in full.", "Send it again.")
			}
			file, problems, err := evidence.Check(ctx, a.deps.Scanner, up.File.Filename, data)
			if err != nil {
				logFailure(ctx, err)
				return nil, problem(CodeUnavailable, "The malware scan is not available, so the file was not kept.", "Try again in a few minutes.")
			}
			var bad []FieldProblem
			for _, x := range problems {
				bad = append(bad, FieldProblem{Code: x.Code, Field: "body." + x.Field, Message: x.Message, Fix: x.Fix})
			}
			if len(bad) == 0 && file.MediaType != evidence.PDF {
				bad = append(bad, FieldProblem{Code: evidence.CodeType, Field: "body.file", Message: "is not a PDF", Fix: "Upload the report as a PDF"})
			}
			if len(bad) > 0 {
				return nil, invalid(bad)
			}
			now := a.deps.Clock.Now()
			key := migration.StorageKey(p.Organisation, id, uuid.Must(uuid.NewV7()))
			if !wantsPreview(in.Prefer) {
				if err := a.deps.Staging.Put(ctx, key, bytes.NewReader(file.Data)); err != nil {
					logFailure(ctx, err)
					return nil, problem(CodeUnavailable, "The file could not be stored.", "Try again in a few minutes.")
				}
			}
			request := struct{ Migration, SHA256, Name string }{in.MigrationID, file.OriginalSHA256, file.Name}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, request, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (MigrationUploadView, error) {
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
				if err := parallelReady(ctx, u, r.ID); err != nil {
					return MigrationUploadView{}, err
				}
				kept, err := u.Migrations().AddPDF(ctx, storage.MigrationFile{RunID: r.ID, FileName: file.Name, MediaType: file.MediaType,
					Size: int64(len(file.Data)), SHA256: file.SHA256, OriginalSHA256: file.OriginalSHA256, StorageKey: key, DataClass: storage.ClassPayroll,
					AddedBy: uploader(ctx), AddedAt: now})
				return MigrationUploadView{File: migrationFileView(kept)}, err
			})
			if (err != nil || w.Replayed) && !wantsPreview(in.Prefer) {
				_ = a.deps.Staging.Delete(context.WithoutCancel(ctx), key) // no row names it
			}
			if err != nil {
				return nil, err
			}
			return &migrationUploadOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})
	if op := a.OpenAPI().Paths["/v1/migrations/{migration_id}/archive/pdfs"].Post; op != nil && op.RequestBody != nil {
		if mt := op.RequestBody.Content["multipart/form-data"]; mt != nil && mt.Schema != nil && mt.Schema.Properties["file"] != nil {
			mt.Schema.Properties["file"].Examples = []any{"%PDF-1.7… (the report's bytes)"}
		}
	}

	register(a, authz.GetMigrationArchive, huma.Operation{
		OperationID: "getMigrationArchive", Method: http.MethodGet, Path: "/v1/migrations/{migration_id}/archive", Tags: []string{"Migration"},
		Summary: "Get the archive",
		Description: "M6's archive (§17.1, FR-MIG-01; rev 0.269): the migration's status, and each of its files' copy in the retention-locked " +
			"store, with its hash and how long the store keeps it; none before the archive. Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *MigrationPath) (*migrationArchiveOutput, error) {
		id, err := parseID("migration_id", in.MigrationID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := &migrationArchiveOutput{}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			r, err := migrationRun(ctx, u, id)
			if err != nil {
				return err
			}
			out.Body, err = archiveOf(ctx, u, r)
			return err
		})
		return out, err
	})

	register(a, authz.ArchiveMigration, huma.Operation{
		OperationID: "archiveMigration", Method: http.MethodPost, Path: "/v1/migrations/{migration_id}/archive", Tags: []string{"Migration"},
		Summary: "Archive the workbook and close the migration",
		Description: "M6, go-live (§17.1, FR-MIG-01; rev 0.269): once the parallel month is signed (MIGRATION-16), each of the migration's " +
			"files (the workbooks of M1 and M5, their Checks exports and the PDFs of the workbook's reports) is copied write-once to the " +
			"retention-locked store, checked against its hash, and kept for the records' retention: until the end of the financial year it " +
			"is archived in, plus the years Settings › Data region and retention keeps records (FR-DOC-07, §11.9). Then the migration closes, " +
			"read only: nothing of it is added, decided or signed again. A person signed in, stepped up in the last 5 minutes. Needs If-Match " +
			"with the migration's ETag. Side effects: the copies, an audit event migration.file_archived for each, and migration.archived. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview checks it and copies and saves nothing.",
	}, append([]string{CodeSessionRequired, CodeStepUpRequired, migration.CodeParallel}, writes...),
		func(ctx context.Context, in *migrationArchiveInput) (*migrationArchiveWriteOutput, error) {
			id, err := parseID("migration_id", in.MigrationID)
			if err != nil {
				return nil, err
			}
			if _, err := a.steppedUpPerson(ctx, "Archiving the workbook"); err != nil {
				return nil, err
			}
			if a.deps.Staging == nil || a.deps.Evidence == nil {
				return nil, problem(CodeUnavailable, "The workbook cannot be archived here: no file store is configured.",
					"Ask your administrator to configure it (FR-DOC-01).")
			}
			p, _ := authn.From(ctx)
			now := a.deps.Clock.Now()
			// The files to copy, read first; the copies are made before the unit of work that records them, write-once, so a
			// repeat finds them made.
			var files []storage.MigrationFile
			var fyStart int
			if err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				r, err := migrationRun(ctx, u, id)
				if err != nil {
					return err
				}
				steps, err := u.Migrations().Steps(ctx, r.ID)
				if err != nil {
					return err
				}
				if err := checkIfMatch(in.IfMatch, migrationETag(r, steps)); err != nil {
					return err
				}
				if r.Status != "open" {
					return problem(storage.CodeMigration, "The migration is closed: its workbook is archived.", "Nothing to do.")
				}
				if err := parallelSigned(ctx, u, r.ID); err != nil {
					return err
				}
				o, err := u.Organisation(ctx)
				if err != nil {
					return err
				}
				fyStart = o.Profile.FYStartMonth
				files, err = u.Migrations().Files(ctx, r.ID)
				return err
			}); err != nil {
				return nil, err
			}
			until := evidence.RetainUntil(fyStart, evidence.RecordsYears(), now, clock.Singapore)
			if !wantsPreview(in.Prefer) {
				for _, f := range files {
					if err := a.copyLocked(ctx, f, lockedKey(p.Organisation, id, f.ID), until); err != nil {
						logFailure(ctx, err)
						return nil, problem(CodeUnavailable, "A file could not be copied to the archive.", "Try again in a few minutes.")
					}
				}
			}
			var tag string
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (MigrationArchiveView, error) {
				r, err := migrationRun(ctx, u, id)
				if err != nil {
					return MigrationArchiveView{}, err
				}
				done, err := u.Migrations().Archives(ctx, r.ID)
				if err != nil {
					return MigrationArchiveView{}, err
				}
				archived := map[uuid.UUID]bool{}
				for _, c := range done {
					archived[c.FileID] = true
				}
				for _, f := range files {
					if archived[f.ID] {
						continue
					}
					if _, err := u.Migrations().Archive(ctx, storage.MigrationArchive{RunID: r.ID, FileID: f.ID, LockedKey: lockedKey(p.Organisation, id, f.ID),
						SHA256: f.SHA256, RetainUntil: until, ArchivedBy: uploader(ctx), ArchivedAt: now}); err != nil {
						return MigrationArchiveView{}, err
					}
				}
				if r, err = u.Migrations().Close(ctx, r.ID); err != nil {
					return MigrationArchiveView{}, err
				}
				steps, err := u.Migrations().Steps(ctx, r.ID)
				if err != nil {
					return MigrationArchiveView{}, err
				}
				tag = migrationETag(r, steps)
				return archiveOf(ctx, u, r)
			})
			if err != nil {
				return nil, err
			}
			out := &migrationArchiveWriteOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}
			if !w.Previewed && !w.Replayed {
				out.ETag = tag
			}
			return out, nil
		})
}
