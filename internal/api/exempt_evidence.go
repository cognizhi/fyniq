package api

import (
	"bytes"
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/director"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ an exempt status's evidence (FR-EQ-11, FR-DOC-05)
//
// The evidence an exempt-private-company status rests on, the ACRA business profile, attached as a pay choice's
// decision is: while the status is a draft a file comes off again; once recorded its files are committed to the
// retention-locked store and are append-only. Whoever may record statuses attaches and removes them, and whoever reads
// every director's account reads them.

// ExemptStatusEvidence is a file on an exempt-private-company status, as the API shows it.
type ExemptStatusEvidence struct {
	ID             string `json:"id" format:"uuid" doc:"The file's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e21"`
	StatusID       string `json:"status_id" format:"uuid" doc:"The status it is attached to." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fe1"`
	EvidenceType   string `json:"evidence_type" doc:"What the file is." example:"ACRA business profile"`
	Note           string `json:"note,omitempty" doc:"A short note." example:"Business profile of 14 Mar 2025"`
	RelatesOn      string `json:"relates_on,omitempty" format:"date" doc:"The date it relates to." example:"2026-01-02"`
	FileName       string `json:"file_name" doc:"The name it was uploaded with: data, never a path." example:"bizfile.pdf"`
	MediaType      string `json:"media_type" doc:"Its type, decided from its content." example:"application/pdf"`
	Size           int64  `json:"size_bytes" doc:"Its size as stored, in bytes." example:"48213"`
	SHA256         string `json:"sha256" doc:"The SHA-256 of the file as stored, in hexadecimal: image metadata such as location is stripped first." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	OriginalSHA256 string `json:"original_sha256" doc:"The SHA-256 of the file as received, in hexadecimal." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	Source         string `json:"source" enum:"upload,paste,email,api,generated" doc:"How it arrived." example:"upload"`
	DataClass      string `json:"data_class" enum:"general,personal,bank,payroll,identity" doc:"The personal data it holds (FR-DOC-09)." example:"general"`
	UploadedBy     string `json:"uploaded_by" doc:"Who uploaded it: an actor of the audit trail." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	UploadedAt     string `json:"uploaded_at" format:"date-time" doc:"When it was attached." example:"2026-11-03T09:15:00Z"`
	State          string `json:"state" enum:"attached,removed,final" doc:"Attached (in staging while the status is a draft), removed, or final: committed to the retention-locked store, append-only." example:"attached"`
	RetainUntil    string `json:"retain_until,omitempty" format:"date-time" doc:"Once final: kept, and never deletable, until then (FR-DOC-02)." example:"2034-01-01T00:00:00+08:00"`
	Version        int    `json:"version" doc:"The file's version; it makes the ETag." example:"1"`
}

// ExemptStatusEvidencePage is a page of a status's files, oldest first.
type ExemptStatusEvidencePage struct {
	Items      []ExemptStatusEvidence `json:"items" nullable:"false" doc:"The files on this page, removed ones included."`
	NextCursor string                 `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTIifQ"`
}

// ExemptStatusEvidencePath names a status's file.
type ExemptStatusEvidencePath struct {
	ExemptStatusPath
	EvidenceID string `path:"evidence_id" format:"uuid" doc:"The file's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e21"`
}

type addExemptEvidenceInput struct {
	ExemptStatusPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	RawBody huma.MultipartFormFiles[EvidenceUpload]
}

type listExemptEvidenceInput struct {
	ExemptStatusPath
	PageQuery
}

type removeExemptEvidenceInput struct {
	ExemptStatusEvidencePath
	IfMatchHeader
	PreferHeader
}

type exemptEvidenceListOutput struct {
	Body ExemptStatusEvidencePage
}

type exemptEvidenceReadOutput struct {
	ETag string `header:"ETag" doc:"The file's version, for If-Match."`
	Body ExemptStatusEvidence
}

type exemptEvidenceWriteOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The file's version, for If-Match."`
	Location string `header:"Location" doc:"The file's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     ExemptStatusEvidence
}

func exemptEvidenceOf(e storage.Evidence) ExemptStatusEvidence {
	j := evidenceOf(e)
	return ExemptStatusEvidence{ID: j.ID, StatusID: j.JournalID, EvidenceType: j.EvidenceType, Note: j.Note, RelatesOn: j.RelatesOn, FileName: j.FileName,
		MediaType: j.MediaType, Size: j.Size, SHA256: j.SHA256, OriginalSHA256: j.OriginalSHA256, Source: j.Source, DataClass: j.DataClass,
		UploadedBy: j.UploadedBy, UploadedAt: j.UploadedAt, State: j.State, RetainUntil: j.RetainUntil, Version: j.Version}
}

// exemptFile is a file of a status the caller may read: not found when it is on another record.
func exemptFile(ctx context.Context, u storage.UnitOfWork, p authn.Principal, statusID, id uuid.UUID) (storage.Evidence, error) {
	if _, err := readableExemptStatus(ctx, u, p, statusID); err != nil {
		return storage.Evidence{}, err
	}
	e, err := u.Evidence().Get(ctx, id)
	if err != nil {
		return e, err
	}
	if e.DocumentID != statusID {
		return e, problem(CodeNotFound, "That file is not attached to this status.", "List the status's evidence to find its files.")
	}
	return e, nil
}

func registerExemptEvidence(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.AddExemptStatusEvidence, huma.Operation{
		OperationID: "addExemptStatusEvidence", Method: http.MethodPost, Path: "/v1/exempt-statuses/{status_id}/evidence", Tags: []string{"Evidence"},
		DefaultStatus: http.StatusCreated, Summary: "Attach the evidence of an exempt-private-company status",
		Description: "Attaches a file, such as the ACRA business profile, to a drafted exempt-private-company status (FR-EQ-11, FR-DOC-05), as " +
			"multipart/form-data: recording it needs one. The file's type is decided from its content; it is scanned for " +
			"malware and its image metadata, such as location, is stripped before it is stored (FR-DOC-08). Side effects: the file in " +
			"the staging store, its evidence row and its audit event with the SHA-256 of the file as received and as stored (FR-DOC-07); " +
			"once the status is recorded its files are committed to the retention-locked store (FR-DOC-13). An Owner or the Controller " +
			"attaches. Needs If-Match with the status's ETag. Idempotent with Idempotency-Key; Prefer: return=preview runs every check and " +
			"saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused,
		CodeIdempotencyBusy, CodeIdempotencyOld, CodeUnavailable, director.CodeNotExemptStatus, director.CodeStatusRecorded}, append(locks, EvidenceCodes...)...),
		func(ctx context.Context, in *addExemptEvidenceInput) (*exemptEvidenceWriteOutput, error) {
			id, err := parseID("status_id", in.StatusID)
			if err != nil {
				return nil, err
			}
			p, _ := authn.From(ctx)
			file, d, err := a.upload(ctx, in.RawBody.Data())
			if err != nil {
				return nil, err
			}
			// The request's identity for Idempotency-Key: the status, the file's hash and what was said about it.
			request := struct {
				Status, IfMatch, SHA256, Name string
				Details                       evidence.Details
			}{in.StatusID, in.IfMatch, file.OriginalSHA256, file.Name, d}
			key := evidence.StagingKey(p.Organisation, uuid.Must(uuid.NewV7()))
			if !wantsPreview(in.Prefer) {
				// Staged before the unit of work: if it rolls back, the sweeper deletes the file (FR-DOC-13).
				if err := a.deps.Staging.Put(ctx, key, bytes.NewReader(file.Data)); err != nil {
					logFailure(ctx, err)
					return nil, problem(CodeUnavailable, "The file could not be stored.", "Try again in a few minutes.")
				}
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, request, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (ExemptStatusEvidence, error) {
				if _, err := readableExemptStatus(ctx, u, p, id); err != nil {
					return ExemptStatusEvidence{}, err
				}
				doc, _, err := director.DraftedExemptStatus(ctx, u, id)
				if err != nil {
					return ExemptStatusEvidence{}, err
				}
				if err := checkIfMatch(in.IfMatch, exemptETag(doc)); err != nil {
					return ExemptStatusEvidence{}, err
				}
				e, err := u.Evidence().Attach(ctx, storage.NewEvidence{DocumentID: id, EvidenceType: d.EvidenceType, Note: d.Note, RelatesOn: d.RelatesOn,
					FileName: file.Name, MediaType: file.MediaType, Size: int64(len(file.Data)), SHA256: file.SHA256, OriginalSHA256: file.OriginalSHA256,
					Source: d.Source, DataClass: d.DataClass, StagingKey: key, UploadedBy: uploader(ctx)})
				if err != nil {
					return ExemptStatusEvidence{}, err
				}
				return exemptEvidenceOf(e), nil
			})
			if (err != nil || w.Replayed) && !wantsPreview(in.Prefer) {
				_ = a.deps.Staging.Delete(context.WithoutCancel(ctx), key) // no row uses it; the sweeper would delete it a day later
			}
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &exemptEvidenceWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &exemptEvidenceWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Location: "/v1/exempt-statuses/" + in.StatusID + "/evidence/" + w.Body.ID,
				Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	if op := a.OpenAPI().Paths["/v1/exempt-statuses/{status_id}/evidence"].Post; op != nil && op.RequestBody != nil {
		if mt := op.RequestBody.Content["multipart/form-data"]; mt != nil && mt.Schema != nil && mt.Schema.Properties["file"] != nil {
			mt.Schema.Properties["file"].Examples = []any{"%PDF-1.7 … (the file's bytes)"}
		}
	}

	register(a, authz.ListExemptStatusEvidence, huma.Operation{
		OperationID: "listExemptStatusEvidence", Method: http.MethodGet, Path: "/v1/exempt-statuses/{status_id}/evidence", Tags: []string{"Evidence"},
		Summary:     "List an exempt status's files",
		Description: "The files attached to an exempt-private-company status, oldest first, removed ones included, a page at a time (FR-DOC-05). Read-only; no side effects.",
	}, []string{CodeNotFound, CodeCursor, director.CodeNotExemptStatus}, func(ctx context.Context, in *listExemptEvidenceInput) (*exemptEvidenceListOutput, error) {
		id, err := parseID("status_id", in.StatusID)
		if err != nil {
			return nil, err
		}
		cur, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		after := uuid.Nil
		if cur != "" {
			if after, err = uuid.Parse(cur); err != nil {
				return nil, problem(CodeCursor, "The cursor is not one this list gave.", "Start again without a cursor.")
			}
		}
		p, _ := authn.From(ctx)
		out := exemptEvidenceListOutput{Body: ExemptStatusEvidencePage{Items: []ExemptStatusEvidence{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if _, err := readableExemptStatus(ctx, u, p, id); err != nil {
				return err
			}
			page, err := u.Evidence().OfDocument(ctx, id, after, in.PageSize+1)
			if err != nil {
				return err
			}
			if len(page) > in.PageSize {
				page = page[:in.PageSize]
				out.Body.NextCursor = encodeCursor(page[len(page)-1].ID.String())
			}
			for _, e := range page {
				out.Body.Items = append(out.Body.Items, exemptEvidenceOf(e))
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.GetExemptStatusEvidence, huma.Operation{
		OperationID: "getExemptStatusEvidence", Method: http.MethodGet, Path: "/v1/exempt-statuses/{status_id}/evidence/{evidence_id}", Tags: []string{"Evidence"},
		Summary:     "Get an exempt status's file",
		Description: "Returns a file's details, hashes, state and retain-until, with its ETag (FR-DOC-05, FR-DOC-07). Read-only; no side effects.",
	}, []string{CodeNotFound, director.CodeNotExemptStatus}, func(ctx context.Context, in *ExemptStatusEvidencePath) (*exemptEvidenceReadOutput, error) {
		cid, err := parseID("status_id", in.StatusID)
		if err != nil {
			return nil, err
		}
		id, err := parseID("evidence_id", in.EvidenceID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out exemptEvidenceReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			e, err := exemptFile(ctx, u, p, cid, id)
			out.Body, out.ETag = exemptEvidenceOf(e), etag(e.Version)
			return err
		})
		return &out, err
	})

	register(a, authz.RemoveExemptStatusEvidence, huma.Operation{
		OperationID: "removeExemptStatusEvidence", Method: http.MethodDelete, Path: "/v1/exempt-statuses/{status_id}/evidence/{evidence_id}", Tags: []string{"Evidence"},
		Summary: "Remove a file from a drafted exempt status",
		Description: "Takes a file off a status not recorded yet (FR-DOC-07). Once recorded its evidence is " +
			"append-only, and the database refuses. The row stays, marked removed, for the audit trail; the staged file is deleted by " +
			"the next sweep. Side effects: the removal and its audit event. Needs If-Match with the file's ETag. Prefer: return=preview " +
			"checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, storage.CodeEvidence, storage.CodeVersion, director.CodeNotExemptStatus,
		director.CodeStatusRecorded}, locks...),
		func(ctx context.Context, in *removeExemptEvidenceInput) (*exemptEvidenceWriteOutput, error) {
			cid, err := parseID("status_id", in.StatusID)
			if err != nil {
				return nil, err
			}
			id, err := parseID("evidence_id", in.EvidenceID)
			if err != nil {
				return nil, err
			}
			p, _ := authn.From(ctx)
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (ExemptStatusEvidence, error) {
				cur, err := exemptFile(ctx, u, p, cid, id)
				if err != nil {
					return ExemptStatusEvidence{}, err
				}
				if _, _, err := director.DraftedExemptStatus(ctx, u, cid); err != nil {
					return ExemptStatusEvidence{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(cur.Version)); err != nil {
					return ExemptStatusEvidence{}, err
				}
				e, err := u.Evidence().Remove(ctx, id, cur.Version)
				return exemptEvidenceOf(e), err
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &exemptEvidenceWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &exemptEvidenceWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Body: w.Body}, nil
		})
}
