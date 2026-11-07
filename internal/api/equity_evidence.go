package api

import (
	"bytes"
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ a holder record's or share class's evidence (FR-DOC-05)
//
// A holder record rests on its identity document (the personal-data class "identity"), a share class on the
// constitution and the shareholders' agreement (FR-EQ-15, FR-EQ-16). While the record is a draft a file comes off
// again; once recorded its files are committed to the retention-locked store and are append-only. Whoever may draft
// records attaches and removes them, and whoever reads the cap table reads them; the DPO, who reads it masked, never
// opens a holder's identity document.

// EquityEvidence is a file on a holder record or share class version, as the API shows it.
type EquityEvidence struct {
	ID             string `json:"id" format:"uuid" doc:"The file's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e21"`
	RecordID       string `json:"record_id" format:"uuid" doc:"The holder record or share class version it is attached to." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e21"`
	EvidenceType   string `json:"evidence_type" doc:"What the file is." example:"Identity document"`
	Note           string `json:"note,omitempty" doc:"A short note." example:"Passport, front page"`
	RelatesOn      string `json:"relates_on,omitempty" format:"date" doc:"The date it relates to." example:"2026-01-02"`
	FileName       string `json:"file_name" doc:"The name it was uploaded with: data, never a path." example:"passport.pdf"`
	MediaType      string `json:"media_type" doc:"Its type, decided from its content." example:"application/pdf"`
	Size           int64  `json:"size_bytes" doc:"Its size as stored, in bytes." example:"48213"`
	SHA256         string `json:"sha256" doc:"The SHA-256 of the file as stored, in hexadecimal: image metadata such as location is stripped first." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	OriginalSHA256 string `json:"original_sha256" doc:"The SHA-256 of the file as received, in hexadecimal." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	UnmaskedNRIC   bool   `json:"unmasked_nric,omitempty" doc:"true when its text shows a Singapore NRIC or FIN in full (FR-DOC-09): it stays with the record and is never shared." example:"false"`
	Source         string `json:"source" enum:"upload,paste,email,api,generated" doc:"How it arrived." example:"upload"`
	DataClass      string `json:"data_class" enum:"general,personal,bank,payroll,identity" doc:"The personal data it holds (FR-DOC-09): an identity document is identity." example:"identity"`
	UploadedBy     string `json:"uploaded_by" doc:"Who uploaded it: an actor of the audit trail." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	UploadedAt     string `json:"uploaded_at" format:"date-time" doc:"When it was attached." example:"2026-11-07T09:15:00Z"`
	State          string `json:"state" enum:"attached,removed,final" doc:"Attached (in staging while the record is a draft), removed, or final: committed to the retention-locked store, append-only." example:"attached"`
	RetainUntil    string `json:"retain_until,omitempty" format:"date-time" doc:"Once final: kept, and never deletable, until then (FR-DOC-02)." example:"2034-01-01T00:00:00+08:00"`
	Version        int    `json:"version" doc:"The file's version; it makes the ETag." example:"1"`
}

// EquityEvidencePage is a page of a record's files, oldest first.
type EquityEvidencePage struct {
	Items      []EquityEvidence `json:"items" nullable:"false" doc:"The files on this page, removed ones included."`
	NextCursor string           `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTIifQ"`
}

// The evidence paths.
type (
	HolderRecordEvidencePath struct {
		HolderRecordPath
		EvidenceID string `path:"evidence_id" format:"uuid" doc:"The file's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e21"`
	}
	ShareClassEvidencePath struct {
		ShareClassPath
		EvidenceID string `path:"evidence_id" format:"uuid" doc:"The file's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e21"`
	}
)

type (
	addHolderEvidenceInput struct {
		HolderRecordPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		RawBody huma.MultipartFormFiles[EvidenceUpload]
	}
	listHolderEvidenceInput struct {
		HolderRecordPath
		PageQuery
	}
	removeHolderEvidenceInput struct {
		HolderRecordEvidencePath
		IfMatchHeader
		PreferHeader
	}
	addClassEvidenceInput struct {
		ShareClassPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		RawBody huma.MultipartFormFiles[EvidenceUpload]
	}
	listClassEvidenceInput struct {
		ShareClassPath
		PageQuery
	}
	removeClassEvidenceInput struct {
		ShareClassEvidencePath
		IfMatchHeader
		PreferHeader
	}
	equityEvidenceListOutput struct{ Body EquityEvidencePage }
	equityEvidenceReadOutput struct {
		ETag string `header:"ETag" doc:"The file's version, for If-Match."`
		Body EquityEvidence
	}
	equityEvidenceWriteOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"The file's version, for If-Match."`
		Location string `header:"Location" doc:"The file's URL."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     EquityEvidence
	}
)

func equityEvidenceOf(e storage.Evidence) EquityEvidence {
	j := evidenceOf(e)
	return EquityEvidence{ID: j.ID, RecordID: j.JournalID, EvidenceType: j.EvidenceType, Note: j.Note, RelatesOn: j.RelatesOn, FileName: j.FileName,
		MediaType: j.MediaType, Size: j.Size, SHA256: j.SHA256, OriginalSHA256: j.OriginalSHA256, UnmaskedNRIC: j.UnmaskedNRIC, Source: j.Source, DataClass: j.DataClass,
		UploadedBy: j.UploadedBy, UploadedAt: j.UploadedAt, State: j.State, RetainUntil: j.RetainUntil, Version: j.Version}
}

// equityRecordOfType reads a document of type t: not found for anything else.
func equityRecordOfType(ctx context.Context, u storage.UnitOfWork, t storage.DocumentType, id uuid.UUID) (storage.Document, error) {
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return doc, err
	}
	if doc.Type != t {
		return doc, problem(CodeNotFound, "There is no such record.", "List the holders or share classes to find it.")
	}
	return doc, nil
}

// equityFile is a file of a record the caller may read: not found when it is on another record.
func equityFile(ctx context.Context, u storage.UnitOfWork, t storage.DocumentType, recordID, id uuid.UUID) (storage.Evidence, error) {
	if _, err := equityRecordOfType(ctx, u, t, recordID); err != nil {
		return storage.Evidence{}, err
	}
	e, err := u.Evidence().Get(ctx, id)
	if err != nil {
		return e, err
	}
	if e.DocumentID != recordID {
		return e, problem(CodeNotFound, "That file is not attached to this record.", "List the record's evidence to find its files.")
	}
	return e, nil
}

// attachEquityEvidence attaches an uploaded file to a drafted record of type t, at its ETag.
func (a *API) attachEquityEvidence(ctx context.Context, t storage.DocumentType, recordPath, recordID, key, ifMatch, prefer string,
	raw *EvidenceUpload) (*equityEvidenceWriteOutput, error) {
	field := "record_id"
	if t == storage.DocumentShareClass {
		field = "version_id"
	}
	id, err := parseID(field, recordID)
	if err != nil {
		return nil, err
	}
	p, _ := authn.From(ctx)
	file, d, err := a.upload(ctx, raw)
	if err != nil {
		return nil, err
	}
	if t == storage.DocumentHolderRecord && d.DataClass == "general" {
		d.DataClass = "identity" // an identity document holds personal data of the identity class (FR-EQ-15)
	}
	request := struct {
		Record, IfMatch, SHA256, Name string
		Details                       evidence.Details
	}{recordID, ifMatch, file.OriginalSHA256, file.Name, d}
	staging := evidence.StagingKey(p.Organisation, uuid.Must(uuid.NewV7()))
	if !wantsPreview(prefer) {
		// Staged before the unit of work: if it rolls back, the sweeper deletes the file (FR-DOC-13).
		if err := a.deps.Staging.Put(ctx, staging, bytes.NewReader(file.Data)); err != nil {
			logFailure(ctx, err)
			return nil, problem(CodeUnavailable, "The file could not be stored.", "Try again in a few minutes.")
		}
	}
	w, err := write(ctx, a, key, prefer, request, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (EquityEvidence, error) {
		doc, err := equity.Drafted(ctx, u, t, id)
		if err != nil {
			return EquityEvidence{}, err
		}
		if err := checkIfMatch(ifMatch, recordETag(doc)); err != nil {
			return EquityEvidence{}, err
		}
		e, err := u.Evidence().Attach(ctx, storage.NewEvidence{DocumentID: id, EvidenceType: d.EvidenceType, Note: d.Note, RelatesOn: d.RelatesOn,
			FileName: file.Name, MediaType: file.MediaType, Size: int64(len(file.Data)), SHA256: file.SHA256, OriginalSHA256: file.OriginalSHA256, NRICFlagged: file.NRIC,
			Source: d.Source, DataClass: d.DataClass, StagingKey: staging, UploadedBy: uploader(ctx)})
		if err != nil {
			return EquityEvidence{}, err
		}
		return equityEvidenceOf(e), nil
	})
	if (err != nil || w.Replayed) && !wantsPreview(prefer) {
		_ = a.deps.Staging.Delete(context.WithoutCancel(ctx), staging) // no row uses it; the sweeper would delete it a day later
	}
	if err != nil {
		return nil, err
	}
	if w.Previewed {
		return &equityEvidenceWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
	}
	return &equityEvidenceWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Location: recordPath + "/" + recordID + "/evidence/" + w.Body.ID,
		Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
}

// listEquityEvidence lists a record's files, oldest first.
func (a *API) listEquityEvidence(ctx context.Context, t storage.DocumentType, field, recordID string, q PageQuery) (*equityEvidenceListOutput, error) {
	id, err := parseID(field, recordID)
	if err != nil {
		return nil, err
	}
	cur, err := decodeCursor(q.Cursor)
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
	out := equityEvidenceListOutput{Body: EquityEvidencePage{Items: []EquityEvidence{}}}
	err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := equityRecordOfType(ctx, u, t, id); err != nil {
			return err
		}
		page, err := u.Evidence().OfDocument(ctx, id, after, q.PageSize+1)
		if err != nil {
			return err
		}
		if len(page) > q.PageSize {
			page = page[:q.PageSize]
			out.Body.NextCursor = encodeCursor(page[len(page)-1].ID.String())
		}
		for _, e := range page {
			out.Body.Items = append(out.Body.Items, equityEvidenceOf(e))
		}
		return nil
	})
	return &out, err
}

// getEquityEvidence reads one file's details.
func (a *API) getEquityEvidence(ctx context.Context, t storage.DocumentType, field, recordID, evidenceID string) (*equityEvidenceReadOutput, error) {
	rid, err := parseID(field, recordID)
	if err != nil {
		return nil, err
	}
	id, err := parseID("evidence_id", evidenceID)
	if err != nil {
		return nil, err
	}
	p, _ := authn.From(ctx)
	var out equityEvidenceReadOutput
	err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		e, err := equityFile(ctx, u, t, rid, id)
		out.Body, out.ETag = equityEvidenceOf(e), etag(e.Version)
		return err
	})
	return &out, err
}

// removeEquityEvidence takes a file off a drafted record, at the file's ETag.
func (a *API) removeEquityEvidence(ctx context.Context, t storage.DocumentType, field, recordID, evidenceID, ifMatch, prefer string,
	in any) (*equityEvidenceWriteOutput, error) {
	rid, err := parseID(field, recordID)
	if err != nil {
		return nil, err
	}
	id, err := parseID("evidence_id", evidenceID)
	if err != nil {
		return nil, err
	}
	w, err := write(ctx, a, "", prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (EquityEvidence, error) {
		cur, err := equityFile(ctx, u, t, rid, id)
		if err != nil {
			return EquityEvidence{}, err
		}
		if _, err := equity.Drafted(ctx, u, t, rid); err != nil {
			return EquityEvidence{}, err
		}
		if err := checkIfMatch(ifMatch, etag(cur.Version)); err != nil {
			return EquityEvidence{}, err
		}
		e, err := u.Evidence().Remove(ctx, id, cur.Version)
		return equityEvidenceOf(e), err
	})
	if err != nil {
		return nil, err
	}
	if w.Previewed {
		return &equityEvidenceWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
	}
	return &equityEvidenceWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Body: w.Body}, nil
}

func registerEquityEvidence(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	addCodes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused,
		CodeIdempotencyBusy, CodeIdempotencyOld, CodeUnavailable, equity.CodeRecorded}, append(locks, EvidenceCodes...)...)
	removeCodes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, storage.CodeEvidence, storage.CodeVersion, equity.CodeRecorded}, locks...)
	addNote := " The file's type is decided from its content; it is scanned for malware and its image metadata, such as location, " +
		"is stripped before it is stored (FR-DOC-08). Side effects: the file in the staging store, its evidence row and its audit event " +
		"with the SHA-256 of the file as received and as stored (FR-DOC-07); once the record is recorded its files are committed to the " +
		"retention-locked store (FR-DOC-13). Needs If-Match with the record's ETag. Idempotent with Idempotency-Key; Prefer: " +
		"return=preview runs every check and saves nothing."
	removeNote := " Once recorded its evidence is append-only, and the database refuses. The row stays, marked removed, for the audit " +
		"trail; the staged file is deleted by the next sweep. Side effects: the removal and its audit event. Needs If-Match with the " +
		"file's ETag. Prefer: return=preview checks it and saves nothing."

	register(a, authz.AddHolderRecordEvidence, huma.Operation{
		OperationID: "addHolderRecordEvidence", Method: http.MethodPost, Path: "/v1/holder-records/{record_id}/evidence", Tags: []string{"Evidence"},
		DefaultStatus: http.StatusCreated, Summary: "Attach a holder's identity document",
		Description: "Attaches a file, such as the holder's identity document, to a drafted holder record (FR-EQ-15, FR-DOC-05), as " +
			"multipart/form-data: asking for approval needs one. It is kept with the personal-data class identity unless another is given." + addNote,
	}, addCodes, func(ctx context.Context, in *addHolderEvidenceInput) (*equityEvidenceWriteOutput, error) {
		return a.attachEquityEvidence(ctx, storage.DocumentHolderRecord, "/v1/holder-records", in.RecordID, in.IdempotencyKey, in.IfMatch, in.Prefer, in.RawBody.Data())
	})
	register(a, authz.ListHolderRecordEvidence, huma.Operation{
		OperationID: "listHolderRecordEvidence", Method: http.MethodGet, Path: "/v1/holder-records/{record_id}/evidence", Tags: []string{"Evidence"},
		Summary:     "List a holder record's files",
		Description: "The files attached to a holder record, oldest first, removed ones included, a page at a time (FR-DOC-05). Read-only; no side effects.",
	}, []string{CodeNotFound, CodeCursor}, func(ctx context.Context, in *listHolderEvidenceInput) (*equityEvidenceListOutput, error) {
		return a.listEquityEvidence(ctx, storage.DocumentHolderRecord, "record_id", in.RecordID, in.PageQuery)
	})
	register(a, authz.GetHolderRecordEvidence, huma.Operation{
		OperationID: "getHolderRecordEvidence", Method: http.MethodGet, Path: "/v1/holder-records/{record_id}/evidence/{evidence_id}", Tags: []string{"Evidence"},
		Summary:     "Get a holder record's file",
		Description: "Returns a file's details, hashes, state and retain-until, with its ETag (FR-DOC-05, FR-DOC-07). Read-only; no side effects.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *HolderRecordEvidencePath) (*equityEvidenceReadOutput, error) {
		return a.getEquityEvidence(ctx, storage.DocumentHolderRecord, "record_id", in.RecordID, in.EvidenceID)
	})
	register(a, authz.RemoveHolderRecordEvidence, huma.Operation{
		OperationID: "removeHolderRecordEvidence", Method: http.MethodDelete, Path: "/v1/holder-records/{record_id}/evidence/{evidence_id}", Tags: []string{"Evidence"},
		Summary:     "Remove a file from a drafted holder record",
		Description: "Takes a file off a holder record not recorded yet (FR-DOC-07)." + removeNote,
	}, removeCodes, func(ctx context.Context, in *removeHolderEvidenceInput) (*equityEvidenceWriteOutput, error) {
		return a.removeEquityEvidence(ctx, storage.DocumentHolderRecord, "record_id", in.RecordID, in.EvidenceID, in.IfMatch, in.Prefer, in)
	})

	register(a, authz.AddShareClassEvidence, huma.Operation{
		OperationID: "addShareClassEvidence", Method: http.MethodPost, Path: "/v1/share-classes/{version_id}/evidence", Tags: []string{"Evidence"},
		DefaultStatus: http.StatusCreated, Summary: "Attach a share class's documents",
		Description: "Attaches a file, such as the constitution or the shareholders' agreement, to a drafted share class version (FR-EQ-16, " +
			"FR-DOC-05), as multipart/form-data: asking for approval needs one." + addNote,
	}, addCodes, func(ctx context.Context, in *addClassEvidenceInput) (*equityEvidenceWriteOutput, error) {
		return a.attachEquityEvidence(ctx, storage.DocumentShareClass, "/v1/share-classes", in.VersionID, in.IdempotencyKey, in.IfMatch, in.Prefer, in.RawBody.Data())
	})
	register(a, authz.ListShareClassEvidence, huma.Operation{
		OperationID: "listShareClassEvidence", Method: http.MethodGet, Path: "/v1/share-classes/{version_id}/evidence", Tags: []string{"Evidence"},
		Summary:     "List a share class version's files",
		Description: "The files attached to a share class version, oldest first, removed ones included, a page at a time (FR-DOC-05). Read-only; no side effects.",
	}, []string{CodeNotFound, CodeCursor}, func(ctx context.Context, in *listClassEvidenceInput) (*equityEvidenceListOutput, error) {
		return a.listEquityEvidence(ctx, storage.DocumentShareClass, "version_id", in.VersionID, in.PageQuery)
	})
	register(a, authz.GetShareClassEvidence, huma.Operation{
		OperationID: "getShareClassEvidence", Method: http.MethodGet, Path: "/v1/share-classes/{version_id}/evidence/{evidence_id}", Tags: []string{"Evidence"},
		Summary:     "Get a share class version's file",
		Description: "Returns a file's details, hashes, state and retain-until, with its ETag (FR-DOC-05, FR-DOC-07). Read-only; no side effects.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *ShareClassEvidencePath) (*equityEvidenceReadOutput, error) {
		return a.getEquityEvidence(ctx, storage.DocumentShareClass, "version_id", in.VersionID, in.EvidenceID)
	})
	register(a, authz.RemoveShareClassEvidence, huma.Operation{
		OperationID: "removeShareClassEvidence", Method: http.MethodDelete, Path: "/v1/share-classes/{version_id}/evidence/{evidence_id}", Tags: []string{"Evidence"},
		Summary:     "Remove a file from a drafted share class version",
		Description: "Takes a file off a share class version not recorded yet (FR-DOC-07)." + removeNote,
	}, removeCodes, func(ctx context.Context, in *removeClassEvidenceInput) (*equityEvidenceWriteOutput, error) {
		return a.removeEquityEvidence(ctx, storage.DocumentShareClass, "version_id", in.VersionID, in.EvidenceID, in.IfMatch, in.Prefer, in)
	})

	for _, path := range []string{"/v1/holder-records/{record_id}/evidence", "/v1/share-classes/{version_id}/evidence"} {
		if op := a.OpenAPI().Paths[path].Post; op != nil && op.RequestBody != nil {
			if mt := op.RequestBody.Content["multipart/form-data"]; mt != nil && mt.Schema != nil && mt.Schema.Properties["file"] != nil {
				mt.Schema.Properties["file"].Examples = []any{"%PDF-1.7 … (the file's bytes)"}
			}
		}
	}
}
