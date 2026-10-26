package api

import (
	"bytes"
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/claim"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ a claim's receipts (FR-AP-14, FR-AP-15, FR-DOC-05)
//
// The receipts a claim rests on, attached as a bill's files are: on a draft a file comes off again; once the claim is
// posted its files are committed to the retention-locked store and are append-only. Only the claimant attaches and
// removes them; whoever sees the claim sees them (design D10). A receipt line names its file (receipt_id), and the same
// file on another record is a duplicate (UC-EXP-01).

// ClaimEvidence is a file on an expense claim, as the API shows it.
type ClaimEvidence struct {
	ID             string `json:"id" format:"uuid" doc:"The file's ID: what a receipt line's receipt_id names." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e21"`
	ClaimID        string `json:"claim_id" format:"uuid" doc:"The claim it is attached to." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fd1"`
	EvidenceType   string `json:"evidence_type" doc:"What the file is." example:"receipt"`
	Note           string `json:"note,omitempty" doc:"A short note." example:"Taxi e-receipt"`
	RelatesOn      string `json:"relates_on,omitempty" format:"date" doc:"The date it relates to." example:"2026-10-14"`
	FileName       string `json:"file_name" doc:"The name it was uploaded with: data, never a path." example:"receipt.png"`
	MediaType      string `json:"media_type" doc:"Its type, decided from its content." example:"image/png"`
	Size           int64  `json:"size_bytes" doc:"Its size as stored, in bytes." example:"48213"`
	SHA256         string `json:"sha256" doc:"The SHA-256 of the file as stored, in hexadecimal: image metadata such as location is stripped first." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	OriginalSHA256 string `json:"original_sha256" doc:"The SHA-256 of the file as received, in hexadecimal: what a duplicate is found by." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	Source         string `json:"source" enum:"upload,paste,email,api,generated" doc:"How it arrived." example:"paste"`
	DataClass      string `json:"data_class" enum:"general,personal,bank,payroll,identity" doc:"The personal data it holds (FR-DOC-09)." example:"general"`
	UploadedBy     string `json:"uploaded_by" doc:"Who uploaded it: an actor of the audit trail." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	UploadedAt     string `json:"uploaded_at" format:"date-time" doc:"When it was attached." example:"2026-10-14T09:15:00Z"`
	State          string `json:"state" enum:"attached,removed,final" doc:"Attached (in staging while the claim is a draft), removed from a draft, or final: committed to the retention-locked store, append-only." example:"attached"`
	RetainUntil    string `json:"retain_until,omitempty" format:"date-time" doc:"Once final: kept, and never deletable, until then (FR-DOC-02)." example:"2034-01-01T00:00:00+08:00"`
	Version        int    `json:"version" doc:"The file's version; it makes the ETag." example:"1"`
}

// ClaimEvidencePage is a page of a claim's files, oldest first.
type ClaimEvidencePage struct {
	Items      []ClaimEvidence `json:"items" nullable:"false" doc:"The files on this page, removed ones included."`
	NextCursor string          `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTIifQ"`
}

// ClaimEvidencePath names a claim's file.
type ClaimEvidencePath struct {
	ExpenseClaimPath
	EvidenceID string `path:"evidence_id" format:"uuid" doc:"The file's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e21"`
}

type addClaimEvidenceInput struct {
	ExpenseClaimPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	RawBody huma.MultipartFormFiles[EvidenceUpload]
}

type listClaimEvidenceInput struct {
	ExpenseClaimPath
	PageQuery
}

type removeClaimEvidenceInput struct {
	ClaimEvidencePath
	IfMatchHeader
	PreferHeader
}

type claimEvidenceListOutput struct{ Body ClaimEvidencePage }

type claimEvidenceReadOutput struct {
	ETag string `header:"ETag" doc:"The file's version, for If-Match."`
	Body ClaimEvidence
}

type claimEvidenceWriteOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The file's version, for If-Match."`
	Location string `header:"Location" doc:"The file's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     ClaimEvidence
}

func claimEvidenceOf(e storage.Evidence) ClaimEvidence {
	j := evidenceOf(e)
	return ClaimEvidence{ID: j.ID, ClaimID: j.JournalID, EvidenceType: j.EvidenceType, Note: j.Note, RelatesOn: j.RelatesOn, FileName: j.FileName,
		MediaType: j.MediaType, Size: j.Size, SHA256: j.SHA256, OriginalSHA256: j.OriginalSHA256, Source: j.Source, DataClass: j.DataClass,
		UploadedBy: j.UploadedBy, UploadedAt: j.UploadedAt, State: j.State, RetainUntil: j.RetainUntil, Version: j.Version}
}

// visibleClaim is a claim p may see: their own, or anyone's for a person who sees everyone's claims; the claimant too.
func visibleClaim(ctx context.Context, u storage.UnitOfWork, p authn.Principal, id uuid.UUID) (storage.Document, claim.Draft, error) {
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return doc, claim.Draft{}, err
	}
	o, err := u.Organisation(ctx)
	if err != nil {
		return doc, claim.Draft{}, err
	}
	base, err := money.Lookup(o.BaseCurrency)
	if err != nil {
		return doc, claim.Draft{}, err
	}
	d, err := claim.Read(doc, base)
	if err != nil {
		return doc, d, err
	}
	if d.ClaimantID != p.UserID && ownClaimsOnly(p) {
		return doc, d, ports.ErrNotFound
	}
	return doc, d, nil
}

// claimFile is a file of a claim p may see: not found when it is on another record.
func claimFile(ctx context.Context, u storage.UnitOfWork, p authn.Principal, claimID, id uuid.UUID) (storage.Document, claim.Draft, storage.Evidence, error) {
	doc, d, err := visibleClaim(ctx, u, p, claimID)
	if err != nil {
		return doc, d, storage.Evidence{}, err
	}
	e, err := u.Evidence().Get(ctx, id)
	if err != nil {
		return doc, d, e, err
	}
	if e.DocumentID != claimID {
		return doc, d, e, problem(CodeNotFound, "That file is not attached to this claim.", "List the claim's evidence to find its files.")
	}
	return doc, d, e, nil
}

func registerClaimEvidence(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.AddExpenseClaimEvidence, huma.Operation{
		OperationID: "addExpenseClaimEvidence", Method: http.MethodPost, Path: "/v1/expense-claims/{claim_id}/evidence", Tags: []string{"Evidence"},
		DefaultStatus: http.StatusCreated, Summary: "Attach a receipt to your claim",
		Description: "Attaches a file, such as a receipt pasted or dropped, to the caller's own claim (FR-AP-14, FR-DOC-05), as " +
			"multipart/form-data: a receipt line then names it (receipt_id). The file's type is decided from its content; it is scanned for " +
			"malware and its image metadata, such as location, is stripped before it is stored (FR-DOC-08). Side effects: the file in the " +
			"staging store, its evidence row and its audit event with the SHA-256 of the file as received and as stored (FR-DOC-07). On a " +
			"draft the file can be removed; once the claim is posted it is committed to the retention-locked store and is append-only " +
			"(FR-DOC-13). Only the claimant attaches. Needs If-Match with the claim's ETag. Idempotent with Idempotency-Key; Prefer: " +
			"return=preview runs every check and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused,
		CodeIdempotencyBusy, CodeIdempotencyOld, CodeUnavailable, CodeClaimNotYours, claim.CodeNotClaim}, append(locks, EvidenceCodes...)...),
		func(ctx context.Context, in *addClaimEvidenceInput) (*claimEvidenceWriteOutput, error) {
			id, err := parseID("claim_id", in.ClaimID)
			if err != nil {
				return nil, err
			}
			p, err := claimant(ctx)
			if err != nil {
				return nil, err
			}
			file, d, err := a.upload(ctx, in.RawBody.Data())
			if err != nil {
				return nil, err
			}
			// The request's identity for Idempotency-Key: the claim, the file's hash and what was said about it.
			request := struct {
				Claim, IfMatch, SHA256, Name string
				Details                      evidence.Details
			}{in.ClaimID, in.IfMatch, file.OriginalSHA256, file.Name, d}
			key := evidence.StagingKey(p.Organisation, uuid.Must(uuid.NewV7()))
			if !wantsPreview(in.Prefer) {
				// Staged before the unit of work: if it rolls back, the sweeper deletes the file (FR-DOC-13).
				if err := a.deps.Staging.Put(ctx, key, bytes.NewReader(file.Data)); err != nil {
					logFailure(ctx, err)
					return nil, problem(CodeUnavailable, "The file could not be stored.", "Try again in a few minutes.")
				}
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, request, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (ClaimEvidence, error) {
				doc, draft, err := visibleClaim(ctx, u, p, id)
				if err != nil {
					return ClaimEvidence{}, err
				}
				if draft.ClaimantID != p.UserID {
					return ClaimEvidence{}, problem(CodeClaimNotYours, "Only the claimant attaches receipts to their claim.", "Ask them to attach it")
				}
				if err := checkIfMatch(in.IfMatch, etag(doc.Version, string(doc.Status))); err != nil {
					return ClaimEvidence{}, err
				}
				e, err := u.Evidence().Attach(ctx, storage.NewEvidence{DocumentID: id, EvidenceType: d.EvidenceType, Note: d.Note, RelatesOn: d.RelatesOn,
					FileName: file.Name, MediaType: file.MediaType, Size: int64(len(file.Data)), SHA256: file.SHA256, OriginalSHA256: file.OriginalSHA256,
					Source: d.Source, DataClass: d.DataClass, StagingKey: key, UploadedBy: uploader(ctx)})
				if err != nil {
					return ClaimEvidence{}, err
				}
				if doc.Status != storage.DocumentDraft && a.deps.Outbox != nil { // committed now, not at the next sweep
					if _, err := a.deps.Outbox.Enqueue(ctx, u, p.Organisation, storage.NewOutboxItem{Class: storage.ClassWorker,
						Kind: EvidenceCommitKind, Payload: []byte(`{}`)}); err != nil {
						return ClaimEvidence{}, err
					}
				}
				return claimEvidenceOf(e), nil
			})
			if (err != nil || w.Replayed) && !wantsPreview(in.Prefer) {
				_ = a.deps.Staging.Delete(context.WithoutCancel(ctx), key) // no row uses it; the sweeper would delete it a day later
			}
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &claimEvidenceWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &claimEvidenceWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Location: "/v1/expense-claims/" + in.ClaimID + "/evidence/" + w.Body.ID,
				Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	if op := a.OpenAPI().Paths["/v1/expense-claims/{claim_id}/evidence"].Post; op != nil && op.RequestBody != nil {
		if mt := op.RequestBody.Content["multipart/form-data"]; mt != nil && mt.Schema != nil && mt.Schema.Properties["file"] != nil {
			mt.Schema.Properties["file"].Examples = []any{"%PDF-1.7 … (the file's bytes)"}
		}
	}

	register(a, authz.ListExpenseClaimEvidence, huma.Operation{
		OperationID: "listExpenseClaimEvidence", Method: http.MethodGet, Path: "/v1/expense-claims/{claim_id}/evidence", Tags: []string{"Evidence"},
		Summary: "List a claim's receipts",
		Description: "The files attached to a claim, oldest first, removed ones included, a page at a time (FR-DOC-05): the caller's own " +
			"claim's, or anyone's for a person who sees everyone's claims. Read-only; no side effects.",
	}, []string{CodeNotFound, CodeCursor, claim.CodeNotClaim}, func(ctx context.Context, in *listClaimEvidenceInput) (*claimEvidenceListOutput, error) {
		id, err := parseID("claim_id", in.ClaimID)
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
		out := claimEvidenceListOutput{Body: ClaimEvidencePage{Items: []ClaimEvidence{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if _, _, err := visibleClaim(ctx, u, p, id); err != nil {
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
				out.Body.Items = append(out.Body.Items, claimEvidenceOf(e))
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.GetExpenseClaimEvidence, huma.Operation{
		OperationID: "getExpenseClaimEvidence", Method: http.MethodGet, Path: "/v1/expense-claims/{claim_id}/evidence/{evidence_id}", Tags: []string{"Evidence"},
		Summary:     "Get a claim's receipt file",
		Description: "Returns a file's details, hashes, state and retain-until, with its ETag (FR-DOC-05, FR-DOC-07). Read-only; no side effects.",
	}, []string{CodeNotFound, claim.CodeNotClaim}, func(ctx context.Context, in *ClaimEvidencePath) (*claimEvidenceReadOutput, error) {
		cid, err := parseID("claim_id", in.ClaimID)
		if err != nil {
			return nil, err
		}
		id, err := parseID("evidence_id", in.EvidenceID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out claimEvidenceReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			_, _, e, err := claimFile(ctx, u, p, cid, id)
			out.Body, out.ETag = claimEvidenceOf(e), etag(e.Version)
			return err
		})
		return &out, err
	})

	register(a, authz.RemoveExpenseClaimEvidence, huma.Operation{
		OperationID: "removeExpenseClaimEvidence", Method: http.MethodDelete, Path: "/v1/expense-claims/{claim_id}/evidence/{evidence_id}", Tags: []string{"Evidence"},
		Summary: "Remove a receipt from your draft claim",
		Description: "Takes a file off the caller's own draft claim (FR-DOC-07). Once it is posted its evidence is append-only, and the " +
			"database refuses. The row stays, marked removed, for the audit trail; the staged file is deleted by the next sweep. A line " +
			"that names it then needs another receipt where one is required. Side effects: the removal and its audit event. Needs " +
			"If-Match with the file's ETag. Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, storage.CodeEvidence, storage.CodeVersion, CodeClaimNotYours, claim.CodeNotClaim}, locks...),
		func(ctx context.Context, in *removeClaimEvidenceInput) (*claimEvidenceWriteOutput, error) {
			cid, err := parseID("claim_id", in.ClaimID)
			if err != nil {
				return nil, err
			}
			id, err := parseID("evidence_id", in.EvidenceID)
			if err != nil {
				return nil, err
			}
			p, err := claimant(ctx)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (ClaimEvidence, error) {
				_, draft, cur, err := claimFile(ctx, u, p, cid, id)
				if err != nil {
					return ClaimEvidence{}, err
				}
				if draft.ClaimantID != p.UserID {
					return ClaimEvidence{}, problem(CodeClaimNotYours, "Only the claimant removes receipts from their claim.", "Ask them to remove it")
				}
				if err := checkIfMatch(in.IfMatch, etag(cur.Version)); err != nil {
					return ClaimEvidence{}, err
				}
				e, err := u.Evidence().Remove(ctx, id, cur.Version)
				return claimEvidenceOf(e), err
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &claimEvidenceWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &claimEvidenceWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Body: w.Body}, nil
		})
}
