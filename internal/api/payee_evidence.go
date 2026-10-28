package api

import (
	"bytes"
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/domain/payee"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ a change of bank details' evidence (FR-CT-03, FR-AP-22, FR-DOC-06)
//
// The files a change of bank details rests on, which it needs before it is asked for: for a vendor's, the vendor's letter
// or a screenshot of their confirmation through a channel known before; for a person's own, a screenshot of their banking
// app or their statement's header. Attached as a bill's evidence is, always of the data class bank. While the change is a
// draft a file comes off again; once approved its files are committed to the retention-locked store and are append-only.

// PayeeBankChangeEvidence is a file on a change of bank details, as the API shows it.
type PayeeBankChangeEvidence struct {
	ID             string `json:"id" format:"uuid" doc:"The file's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e21"`
	ChangeID       string `json:"change_id" format:"uuid" doc:"The change it is attached to." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5a10"`
	EvidenceType   string `json:"evidence_type" doc:"What the file is: bank_letter or vendor_confirmation meet a vendor's change's required evidence; bank_app_screenshot or bank_statement a person's own." example:"bank_letter"`
	Note           string `json:"note,omitempty" doc:"A short note." example:"Letter on Northwind's letterhead, signed by its CFO"`
	RelatesOn      string `json:"relates_on,omitempty" format:"date" doc:"The date it relates to." example:"2026-10-14"`
	FileName       string `json:"file_name" doc:"The name it was uploaded with: data, never a path." example:"northwind-bank-letter.pdf"`
	MediaType      string `json:"media_type" doc:"Its type, decided from its content." example:"application/pdf"`
	Size           int64  `json:"size_bytes" doc:"Its size as stored, in bytes." example:"48213"`
	SHA256         string `json:"sha256" doc:"The SHA-256 of the file as stored, in hexadecimal: image metadata such as location is stripped first." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	OriginalSHA256 string `json:"original_sha256" doc:"The SHA-256 of the file as received, in hexadecimal." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	Source         string `json:"source" enum:"upload,paste,email,api,generated" doc:"How it arrived: generated for a file Fyniq made, such as the PDF of a quote or invoice it emailed (FR-AR-06)." example:"upload"`
	DataClass      string `json:"data_class" enum:"general,personal,bank,payroll,identity" doc:"The personal data it holds (FR-DOC-09)." example:"general"`
	UploadedBy     string `json:"uploaded_by" doc:"Who uploaded it: an actor of the audit trail." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	UploadedAt     string `json:"uploaded_at" format:"date-time" doc:"When it was attached." example:"2026-10-14T09:15:00Z"`
	State          string `json:"state" enum:"attached,removed,final" doc:"Attached (in staging while the change is a draft), removed from a draft, or final: committed to the retention-locked store, append-only." example:"attached"`
	RetainUntil    string `json:"retain_until,omitempty" format:"date-time" doc:"Once final: kept, and never deletable, until then (FR-DOC-02)." example:"2034-01-01T00:00:00+08:00"`
	Version        int    `json:"version" doc:"The file's version; it makes the ETag." example:"1"`
}

// PayeeBankChangeEvidencePage is a page of a change's files, oldest first.
type PayeeBankChangeEvidencePage struct {
	Items      []PayeeBankChangeEvidence `json:"items" nullable:"false" doc:"The files on this page, removed ones included."`
	NextCursor string                    `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTIifQ"`
}

// PayeeBankChangeEvidencePath names a change's file.
type PayeeBankChangeEvidencePath struct {
	PayeeBankChangePath
	EvidenceID string `path:"evidence_id" format:"uuid" doc:"The file's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e21"`
}

type addPayeeBankChangeEvidenceInput struct {
	PayeeBankChangePath
	KeyHeader
	IfMatchHeader
	PreferHeader
	RawBody huma.MultipartFormFiles[EvidenceUpload]
}

type listPayeeBankChangeEvidenceInput struct {
	PayeeBankChangePath
	PageQuery
}

type removePayeeBankChangeEvidenceInput struct {
	PayeeBankChangeEvidencePath
	IfMatchHeader
	PreferHeader
}

type payeeEvidenceListOutput struct{ Body PayeeBankChangeEvidencePage }

type payeeEvidenceReadOutput struct {
	ETag string `header:"ETag" doc:"The file's version, for If-Match."`
	Body PayeeBankChangeEvidence
}

type payeeEvidenceWriteOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The file's version, for If-Match."`
	Location string `header:"Location" doc:"The file's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     PayeeBankChangeEvidence
}

func payeeEvidenceOf(e storage.Evidence) PayeeBankChangeEvidence {
	j := evidenceOf(e)
	return PayeeBankChangeEvidence{ID: j.ID, ChangeID: j.JournalID, EvidenceType: j.EvidenceType, Note: j.Note, RelatesOn: j.RelatesOn, FileName: j.FileName,
		MediaType: j.MediaType, Size: j.Size, SHA256: j.SHA256, OriginalSHA256: j.OriginalSHA256, Source: j.Source, DataClass: j.DataClass,
		UploadedBy: j.UploadedBy, UploadedAt: j.UploadedAt, State: j.State, RetainUntil: j.RetainUntil, Version: j.Version}
}

// findChange finds the change a file request is about, refusing what the caller may not reach: another kind of record,
// or (for a person's own) another person's change.
type findChange func(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Document, error)

// payeeEvidence returns a file of the change: not found when it is on another record.
func payeeEvidence(ctx context.Context, u storage.UnitOfWork, find findChange, changeID, id uuid.UUID) (storage.Evidence, error) {
	if _, err := find(ctx, u, changeID); err != nil {
		return storage.Evidence{}, err
	}
	e, err := u.Evidence().Get(ctx, id)
	if err != nil {
		return e, err
	}
	if e.DocumentID != changeID {
		return e, problem(CodeNotFound, "That file is not attached to this change.", "List the change's evidence to find its files.")
	}
	return e, nil
}

// addChangeEvidence attaches a file to the change find reaches; base is the change's URL, for Location.
func (a *API) addChangeEvidence(ctx context.Context, in *addPayeeBankChangeEvidenceInput, find findChange, base string) (*payeeEvidenceWriteOutput, error) {
	id, err := parseID("change_id", in.ChangeID)
	if err != nil {
		return nil, err
	}
	p, _ := authn.From(ctx)
	file, d, err := a.upload(ctx, in.RawBody.Data())
	if err != nil {
		return nil, err
	}
	// The request's identity for Idempotency-Key: the change, the file's hash and what was said about it.
	request := struct {
		Change, IfMatch, SHA256, Name string
		Details                       evidence.Details
	}{in.ChangeID, in.IfMatch, file.OriginalSHA256, file.Name, d}
	key := evidence.StagingKey(p.Organisation, uuid.Must(uuid.NewV7()))
	if !wantsPreview(in.Prefer) {
		// Staged before the unit of work: if it rolls back, the sweeper deletes the file (FR-DOC-13).
		if err := a.deps.Staging.Put(ctx, key, bytes.NewReader(file.Data)); err != nil {
			logFailure(ctx, err)
			return nil, problem(CodeUnavailable, "The file could not be stored.", "Try again in a few minutes.")
		}
	}
	w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, request, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (PayeeBankChangeEvidence, error) {
		doc, err := find(ctx, u, id)
		if err != nil {
			return PayeeBankChangeEvidence{}, err
		}
		if err := checkIfMatch(in.IfMatch, etag(doc.Version, string(doc.Status))); err != nil {
			return PayeeBankChangeEvidence{}, err
		}
		e, err := u.Evidence().Attach(ctx, storage.NewEvidence{DocumentID: id, EvidenceType: d.EvidenceType, Note: d.Note, RelatesOn: d.RelatesOn,
			FileName: file.Name, MediaType: file.MediaType, Size: int64(len(file.Data)), SHA256: file.SHA256, OriginalSHA256: file.OriginalSHA256,
			Source: d.Source, DataClass: storage.ClassBank, StagingKey: key, UploadedBy: uploader(ctx)})
		if err != nil {
			return PayeeBankChangeEvidence{}, err
		}
		if doc.Status != storage.DocumentDraft && a.deps.Outbox != nil { // committed now, not at the next sweep
			if _, err := a.deps.Outbox.Enqueue(ctx, u, p.Organisation, storage.NewOutboxItem{Class: storage.ClassWorker,
				Kind: EvidenceCommitKind, Payload: []byte(`{}`)}); err != nil {
				return PayeeBankChangeEvidence{}, err
			}
		}
		return payeeEvidenceOf(e), nil
	})
	if (err != nil || w.Replayed) && !wantsPreview(in.Prefer) {
		_ = a.deps.Staging.Delete(context.WithoutCancel(ctx), key) // no row uses it; the sweeper would delete it a day later
	}
	if err != nil {
		return nil, err
	}
	if w.Previewed {
		return &payeeEvidenceWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
	}
	return &payeeEvidenceWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Location: base + "/" + in.ChangeID + "/evidence/" + w.Body.ID,
		Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
}

// listChangeEvidence lists the files of the change find reaches.
func (a *API) listChangeEvidence(ctx context.Context, in *listPayeeBankChangeEvidenceInput, find findChange) (*payeeEvidenceListOutput, error) {
	id, err := parseID("change_id", in.ChangeID)
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
	out := payeeEvidenceListOutput{Body: PayeeBankChangeEvidencePage{Items: []PayeeBankChangeEvidence{}}}
	err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := find(ctx, u, id); err != nil {
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
			out.Body.Items = append(out.Body.Items, payeeEvidenceOf(e))
		}
		return nil
	})
	return &out, err
}

// getChangeEvidence reads a file of the change find reaches.
func (a *API) getChangeEvidence(ctx context.Context, in *PayeeBankChangeEvidencePath, find findChange) (*payeeEvidenceReadOutput, error) {
	bid, err := parseID("change_id", in.ChangeID)
	if err != nil {
		return nil, err
	}
	id, err := parseID("evidence_id", in.EvidenceID)
	if err != nil {
		return nil, err
	}
	p, _ := authn.From(ctx)
	var out payeeEvidenceReadOutput
	err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		e, err := payeeEvidence(ctx, u, find, bid, id)
		out.Body, out.ETag = payeeEvidenceOf(e), etag(e.Version)
		return err
	})
	return &out, err
}

// removeChangeEvidence takes a file off the draft change find reaches.
func (a *API) removeChangeEvidence(ctx context.Context, in *removePayeeBankChangeEvidenceInput, find findChange) (*payeeEvidenceWriteOutput, error) {
	bid, err := parseID("change_id", in.ChangeID)
	if err != nil {
		return nil, err
	}
	id, err := parseID("evidence_id", in.EvidenceID)
	if err != nil {
		return nil, err
	}
	w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (PayeeBankChangeEvidence, error) {
		cur, err := payeeEvidence(ctx, u, find, bid, id)
		if err != nil {
			return PayeeBankChangeEvidence{}, err
		}
		if err := checkIfMatch(in.IfMatch, etag(cur.Version)); err != nil {
			return PayeeBankChangeEvidence{}, err
		}
		e, err := u.Evidence().Remove(ctx, id, cur.Version)
		return payeeEvidenceOf(e), err
	})
	if err != nil {
		return nil, err
	}
	if w.Previewed {
		return &payeeEvidenceWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
	}
	return &payeeEvidenceWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Body: w.Body}, nil
}

// fileExample gives a multipart upload's file an example, which huma does not take from a tag.
func fileExample(a *API, path string) {
	if op := a.OpenAPI().Paths[path].Post; op != nil && op.RequestBody != nil {
		if mt := op.RequestBody.Content["multipart/form-data"]; mt != nil && mt.Schema != nil && mt.Schema.Properties["file"] != nil {
			mt.Schema.Properties["file"].Examples = []any{"%PDF-1.7 … (the file's bytes)"}
		}
	}
}

func registerPayeeBankChangeEvidence(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	vendors := func(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Document, error) {
		return payeeChange(ctx, u, id, false)
	}
	register(a, authz.AddPayeeBankChangeEvidence, huma.Operation{
		OperationID: "addPayeeBankChangeEvidence", Method: http.MethodPost, Path: "/v1/bank-changes/{change_id}/evidence", Tags: []string{"Evidence"},
		DefaultStatus: http.StatusCreated, Summary: "Attach evidence to a change of bank details",
		Description: "Attaches a file to a change of a vendor's bank details (FR-CT-03, FR-DOC-05, FR-DOC-06; PRD-001 rev 0.152), as " +
			"multipart/form-data: the vendor's letter (evidence type bank_letter) or a screenshot of their confirmation through a channel " +
			"known before (vendor_confirmation), one of which the change needs before it is asked for, and anything else that supports it. " +
			"Its data class is bank, whatever is sent (FR-DOC-09). The file's type is decided from its content; it is scanned for malware " +
			"and its image metadata, such as location, is stripped before it is stored (FR-DOC-08). Side effects: the file in the staging " +
			"store, its evidence row and its audit event with the SHA-256 of the file as received and as stored (FR-DOC-07). While the " +
			"change is a draft the file can be removed; once approved it is committed to the retention-locked store and is append-only " +
			"(FR-DOC-13). Needs If-Match with the change's ETag. Idempotent with Idempotency-Key; Prefer: return=preview runs every " +
			"check and saves nothing. AI and MCP clients cannot upload.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused,
		CodeIdempotencyBusy, CodeIdempotencyOld, CodeUnavailable, payee.CodeNotChange}, append(locks, EvidenceCodes...)...),
		func(ctx context.Context, in *addPayeeBankChangeEvidenceInput) (*payeeEvidenceWriteOutput, error) {
			return a.addChangeEvidence(ctx, in, vendors, "/v1/bank-changes")
		})
	fileExample(a, "/v1/bank-changes/{change_id}/evidence")

	register(a, authz.ListPayeeBankChangeEvidence, huma.Operation{
		OperationID: "listPayeeBankChangeEvidence", Method: http.MethodGet, Path: "/v1/bank-changes/{change_id}/evidence", Tags: []string{"Evidence"},
		Summary:     "List a change's evidence",
		Description: "The files attached to a change of a vendor's bank details, oldest first, removed ones included, a page at a time (FR-DOC-05). Read-only; no side effects.",
	}, []string{CodeNotFound, CodeCursor, payee.CodeNotChange}, func(ctx context.Context, in *listPayeeBankChangeEvidenceInput) (*payeeEvidenceListOutput, error) {
		return a.listChangeEvidence(ctx, in, vendors)
	})

	register(a, authz.GetPayeeBankChangeEvidence, huma.Operation{
		OperationID: "getPayeeBankChangeEvidence", Method: http.MethodGet, Path: "/v1/bank-changes/{change_id}/evidence/{evidence_id}", Tags: []string{"Evidence"},
		Summary:     "Get a change's evidence file",
		Description: "Returns a file's details, hashes, state and retain-until, with its ETag (FR-DOC-05, FR-DOC-07). Read-only; no side effects.",
	}, []string{CodeNotFound, payee.CodeNotChange}, func(ctx context.Context, in *PayeeBankChangeEvidencePath) (*payeeEvidenceReadOutput, error) {
		return a.getChangeEvidence(ctx, in, vendors)
	})

	register(a, authz.RemovePayeeBankChangeEvidence, huma.Operation{
		OperationID: "removePayeeBankChangeEvidence", Method: http.MethodDelete, Path: "/v1/bank-changes/{change_id}/evidence/{evidence_id}", Tags: []string{"Evidence"},
		Summary: "Remove evidence from a draft change of bank details",
		Description: "Takes a file off a draft change of bank details (FR-DOC-07). Once it is approved its evidence is append-only, and the " +
			"database refuses. The row stays, marked removed, for the audit trail; the staged file is deleted by the next sweep. Side " +
			"effects: the removal and its audit event. Needs If-Match with the file's ETag. Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, storage.CodeEvidence, storage.CodeVersion, payee.CodeNotChange}, locks...),
		func(ctx context.Context, in *removePayeeBankChangeEvidenceInput) (*payeeEvidenceWriteOutput, error) {
			return a.removeChangeEvidence(ctx, in, vendors)
		})
}

func registerClaimantBankChangeEvidence(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	// mine reaches the caller's own changes only; anyone's reaches every person's change, for its approver.
	mine := func(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Document, error) {
		p, err := me(ctx)
		if err != nil {
			return storage.Document{}, err
		}
		return claimantChange(ctx, u, id, p.UserID, false)
	}
	anyone := func(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Document, error) {
		return claimantChange(ctx, u, id, uuid.Nil, false)
	}
	register(a, authz.AddMyBankChangeEvidence, huma.Operation{
		OperationID: "addMyBankChangeEvidence", Method: http.MethodPost, Path: "/v1/me/bank-changes/{change_id}/evidence", Tags: []string{"Evidence"},
		DefaultStatus: http.StatusCreated, Summary: "Attach evidence to a change of your bank account",
		Description: "Attaches a file to a change of your own bank account for reimbursements (FR-AP-22, FR-DOC-05, FR-DOC-06; PRD-001 rev " +
			"0.154), as multipart/form-data: a screenshot of your banking app (evidence type bank_app_screenshot) or your statement's " +
			"header (bank_statement), showing your name and the account number, one of which the change needs before it is asked for. Its " +
			"data class is bank, whatever is sent (FR-DOC-09). The file's type is decided from its content; it is scanned for malware and " +
			"its image metadata is stripped before it is stored (FR-DOC-08). Side effects: the file in the staging store, its evidence " +
			"row and its audit event (FR-DOC-07). While the change is a draft the file can be removed; once approved it is append-only " +
			"(FR-DOC-13). Another person's change is not found. Needs If-Match with the change's ETag. Idempotent with Idempotency-Key; " +
			"Prefer: return=preview runs every check and saves nothing. AI and MCP clients cannot upload.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused,
		CodeIdempotencyBusy, CodeIdempotencyOld, CodeUnavailable, payee.CodeNotChange, payee.CodeOwnOnly}, append(locks, EvidenceCodes...)...),
		func(ctx context.Context, in *addPayeeBankChangeEvidenceInput) (*payeeEvidenceWriteOutput, error) {
			return a.addChangeEvidence(ctx, in, mine, "/v1/me/bank-changes")
		})
	fileExample(a, "/v1/me/bank-changes/{change_id}/evidence")

	register(a, authz.ListMyBankChangeEvidence, huma.Operation{
		OperationID: "listMyBankChangeEvidence", Method: http.MethodGet, Path: "/v1/me/bank-changes/{change_id}/evidence", Tags: []string{"Evidence"},
		Summary: "List a change of your bank account's evidence",
		Description: "The files attached to a change of your own bank account, oldest first, removed ones included, a page at a time " +
			"(FR-DOC-05). Another person's change is not found. Read-only; no side effects.",
	}, []string{CodeNotFound, CodeCursor, payee.CodeNotChange, payee.CodeOwnOnly}, func(ctx context.Context, in *listPayeeBankChangeEvidenceInput) (*payeeEvidenceListOutput, error) {
		return a.listChangeEvidence(ctx, in, mine)
	})

	register(a, authz.GetMyBankChangeEvidence, huma.Operation{
		OperationID: "getMyBankChangeEvidence", Method: http.MethodGet, Path: "/v1/me/bank-changes/{change_id}/evidence/{evidence_id}", Tags: []string{"Evidence"},
		Summary:     "Get a change of your bank account's evidence file",
		Description: "Returns a file's details, hashes, state and retain-until, with its ETag (FR-DOC-05, FR-DOC-07). Read-only; no side effects.",
	}, []string{CodeNotFound, payee.CodeNotChange, payee.CodeOwnOnly}, func(ctx context.Context, in *PayeeBankChangeEvidencePath) (*payeeEvidenceReadOutput, error) {
		return a.getChangeEvidence(ctx, in, mine)
	})

	register(a, authz.RemoveMyBankChangeEvidence, huma.Operation{
		OperationID: "removeMyBankChangeEvidence", Method: http.MethodDelete, Path: "/v1/me/bank-changes/{change_id}/evidence/{evidence_id}", Tags: []string{"Evidence"},
		Summary: "Remove evidence from a draft change of your bank account",
		Description: "Takes a file off a draft change of your own bank account (FR-DOC-07). Once it is approved its evidence is append-only, " +
			"and the database refuses. The row stays, marked removed, for the audit trail. Side effects: the removal and its audit event. " +
			"Needs If-Match with the file's ETag. Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, storage.CodeEvidence, storage.CodeVersion, payee.CodeNotChange, payee.CodeOwnOnly}, locks...),
		func(ctx context.Context, in *removePayeeBankChangeEvidenceInput) (*payeeEvidenceWriteOutput, error) {
			return a.removeChangeEvidence(ctx, in, mine)
		})

	register(a, authz.ListClaimantBankChangeEvidence, huma.Operation{
		OperationID: "listClaimantBankChangeEvidence", Method: http.MethodGet, Path: "/v1/claimant-bank-changes/{change_id}/evidence", Tags: []string{"Evidence"},
		Summary: "List a change of a person's bank account's evidence",
		Description: "The files attached to a change of a person's bank account for reimbursements, for its approver to compare with the " +
			"number (FR-AP-22, FR-DOC-05): oldest first, removed ones included, a page at a time. Read-only; no side effects.",
	}, []string{CodeNotFound, CodeCursor, payee.CodeNotChange}, func(ctx context.Context, in *listPayeeBankChangeEvidenceInput) (*payeeEvidenceListOutput, error) {
		return a.listChangeEvidence(ctx, in, anyone)
	})

	register(a, authz.GetClaimantBankChangeEvidence, huma.Operation{
		OperationID: "getClaimantBankChangeEvidence", Method: http.MethodGet, Path: "/v1/claimant-bank-changes/{change_id}/evidence/{evidence_id}", Tags: []string{"Evidence"},
		Summary:     "Get a change of a person's bank account's evidence file",
		Description: "Returns a file's details, hashes, state and retain-until, with its ETag (FR-DOC-05, FR-DOC-07). Read-only; no side effects.",
	}, []string{CodeNotFound, payee.CodeNotChange}, func(ctx context.Context, in *PayeeBankChangeEvidencePath) (*payeeEvidenceReadOutput, error) {
		return a.getChangeEvidence(ctx, in, anyone)
	})
}
