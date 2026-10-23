package api

import (
	"bytes"
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ a bill's evidence (FR-DOC-05, 07, 08, 11, 13)
//
// The supplier's invoice and anything else a bill or a vendor credit rests on, attached as a journal's evidence is: on a
// draft a file comes off again; once the bill is posted its files are committed to the retention-locked store and are
// append-only (p1-purchases tasks 1.5 and 2.2; PRD-001 rev 0.86). The evidence rules per activity, which require the
// invoice to post (FR-DOC-06), arrive later in P1.

// BillEvidence is a file on a bill or a vendor credit, as the API shows it.
type BillEvidence struct {
	ID             string `json:"id" format:"uuid" doc:"The file's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e21"`
	BillID         string `json:"bill_id" format:"uuid" doc:"The bill or vendor credit it is attached to." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e30"`
	EvidenceType   string `json:"evidence_type" doc:"What the file is." example:"invoice"`
	Note           string `json:"note,omitempty" doc:"A short note." example:"As emailed by the supplier"`
	RelatesOn      string `json:"relates_on,omitempty" format:"date" doc:"The date it relates to." example:"2026-10-14"`
	FileName       string `json:"file_name" doc:"The name it was uploaded with: data, never a path." example:"AU-1182.pdf"`
	MediaType      string `json:"media_type" doc:"Its type, decided from its content." example:"application/pdf"`
	Size           int64  `json:"size_bytes" doc:"Its size as stored, in bytes." example:"48213"`
	SHA256         string `json:"sha256" doc:"The SHA-256 of the file as stored, in hexadecimal: image metadata such as location is stripped first." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	OriginalSHA256 string `json:"original_sha256" doc:"The SHA-256 of the file as received, in hexadecimal." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	Source         string `json:"source" enum:"upload,paste,email,api" doc:"How it arrived." example:"upload"`
	DataClass      string `json:"data_class" enum:"general,personal,bank,payroll,identity" doc:"The personal data it holds (FR-DOC-09)." example:"general"`
	UploadedBy     string `json:"uploaded_by" doc:"Who uploaded it: an actor of the audit trail." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	UploadedAt     string `json:"uploaded_at" format:"date-time" doc:"When it was attached." example:"2026-10-14T09:15:00Z"`
	State          string `json:"state" enum:"attached,removed,final" doc:"Attached (in staging while the bill is a draft), removed from a draft, or final: committed to the retention-locked store, append-only." example:"attached"`
	RetainUntil    string `json:"retain_until,omitempty" format:"date-time" doc:"Once final: kept, and never deletable, until then (FR-DOC-02)." example:"2034-01-01T00:00:00+08:00"`
	Version        int    `json:"version" doc:"The file's version; it makes the ETag." example:"1"`
}

// BillEvidencePage is a page of a bill's files, oldest first.
type BillEvidencePage struct {
	Items      []BillEvidence `json:"items" nullable:"false" doc:"The files on this page, removed ones included."`
	NextCursor string         `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTIifQ"`
}

// BillEvidencePath names a bill's file.
type BillEvidencePath struct {
	BillPath
	EvidenceID string `path:"evidence_id" format:"uuid" doc:"The file's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e21"`
}

type addBillEvidenceInput struct {
	BillPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	RawBody huma.MultipartFormFiles[EvidenceUpload]
}

type listBillEvidenceInput struct {
	BillPath
	PageQuery
}

type removeBillEvidenceInput struct {
	BillEvidencePath
	IfMatchHeader
	PreferHeader
}

type billEvidenceListOutput struct{ Body BillEvidencePage }

type billEvidenceReadOutput struct {
	ETag string `header:"ETag" doc:"The file's version, for If-Match."`
	Body BillEvidence
}

type billEvidenceWriteOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The file's version, for If-Match."`
	Location string `header:"Location" doc:"The file's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     BillEvidence
}

func billEvidenceOf(e storage.Evidence) BillEvidence {
	j := evidenceOf(e)
	return BillEvidence{ID: j.ID, BillID: j.JournalID, EvidenceType: j.EvidenceType, Note: j.Note, RelatesOn: j.RelatesOn, FileName: j.FileName,
		MediaType: j.MediaType, Size: j.Size, SHA256: j.SHA256, OriginalSHA256: j.OriginalSHA256, Source: j.Source, DataClass: j.DataClass,
		UploadedBy: j.UploadedBy, UploadedAt: j.UploadedAt, State: j.State, RetainUntil: j.RetainUntil, Version: j.Version}
}

// billDocument returns the bill or vendor credit a file is under: refused for another kind of document.
func billDocument(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Document, error) {
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return doc, err
	}
	if !bill.IsBill(doc) {
		return doc, problem(bill.CodeNotBill, "That record is not a bill or a vendor credit.", "Use the bill's ID.")
	}
	return doc, nil
}

// billEvidence returns a file of the bill: not found when it is on another record.
func billEvidence(ctx context.Context, u storage.UnitOfWork, billID, id uuid.UUID) (storage.Evidence, error) {
	if _, err := billDocument(ctx, u, billID); err != nil {
		return storage.Evidence{}, err
	}
	e, err := u.Evidence().Get(ctx, id)
	if err != nil {
		return e, err
	}
	if e.DocumentID != billID {
		return e, problem(CodeNotFound, "That file is not attached to this bill.", "List the bill's evidence to find its files.")
	}
	return e, nil
}

func registerBillEvidence(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.AddBillEvidence, huma.Operation{
		OperationID: "addBillEvidence", Method: http.MethodPost, Path: "/v1/bills/{bill_id}/evidence", Tags: []string{"Evidence"},
		DefaultStatus: http.StatusCreated, Summary: "Attach evidence to a bill",
		Description: "Attaches a file, such as the supplier's invoice, to a bill or a vendor credit (FR-DOC-05, FR-DOC-11; PRD-001 rev 0.86), " +
			"as multipart/form-data. The file's type is decided from its content; it is scanned for malware and its image metadata, such as " +
			"location, is stripped before it is stored (FR-DOC-08). Side effects: the file in the staging store, its evidence row and its " +
			"audit event with the SHA-256 of the file as received and as stored, the uploader, the source and the bill (FR-DOC-07). On a " +
			"draft the file can be removed; on a posted or voided bill it is committed to the retention-locked store and is append-only " +
			"(FR-DOC-13). Needs If-Match with the bill's ETag, so the file goes on the bill as you read it. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview runs every check and saves nothing. AI and MCP clients cannot upload: they propose doc.attach.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused,
		CodeIdempotencyBusy, CodeIdempotencyOld, CodeUnavailable, bill.CodeNotBill}, append(locks, EvidenceCodes...)...),
		func(ctx context.Context, in *addBillEvidenceInput) (*billEvidenceWriteOutput, error) {
			id, err := parseID("bill_id", in.BillID)
			if err != nil {
				return nil, err
			}
			p, _ := authn.From(ctx)
			file, d, err := a.upload(ctx, in.RawBody.Data())
			if err != nil {
				return nil, err
			}
			// The request's identity for Idempotency-Key: the bill, the file's hash and what was said about it.
			request := struct {
				Bill, IfMatch, SHA256, Name string
				Details                     evidence.Details
			}{in.BillID, in.IfMatch, file.OriginalSHA256, file.Name, d}
			key := evidence.StagingKey(p.Organisation, uuid.Must(uuid.NewV7()))
			if !wantsPreview(in.Prefer) {
				// Staged before the unit of work: if it rolls back, the sweeper deletes the file (FR-DOC-13).
				if err := a.deps.Staging.Put(ctx, key, bytes.NewReader(file.Data)); err != nil {
					logFailure(ctx, err)
					return nil, problem(CodeUnavailable, "The file could not be stored.", "Try again in a few minutes.")
				}
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, request, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (BillEvidence, error) {
				doc, err := billDocument(ctx, u, id)
				if err != nil {
					return BillEvidence{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(doc.Version, string(doc.Status))); err != nil {
					return BillEvidence{}, err
				}
				e, err := u.Evidence().Attach(ctx, storage.NewEvidence{DocumentID: id, EvidenceType: d.EvidenceType, Note: d.Note, RelatesOn: d.RelatesOn,
					FileName: file.Name, MediaType: file.MediaType, Size: int64(len(file.Data)), SHA256: file.SHA256, OriginalSHA256: file.OriginalSHA256,
					Source: d.Source, DataClass: d.DataClass, StagingKey: key, UploadedBy: uploader(ctx)})
				if err != nil {
					return BillEvidence{}, err
				}
				if doc.Status != storage.DocumentDraft && a.deps.Outbox != nil { // committed now, not at the next sweep
					if _, err := a.deps.Outbox.Enqueue(ctx, u, p.Organisation, storage.NewOutboxItem{Class: storage.ClassWorker,
						Kind: EvidenceCommitKind, Payload: []byte(`{}`)}); err != nil {
						return BillEvidence{}, err
					}
				}
				return billEvidenceOf(e), nil
			})
			if (err != nil || w.Replayed) && !wantsPreview(in.Prefer) {
				_ = a.deps.Staging.Delete(context.WithoutCancel(ctx), key) // no row uses it; the sweeper would delete it a day later
			}
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &billEvidenceWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &billEvidenceWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Location: "/v1/bills/" + in.BillID + "/evidence/" + w.Body.ID,
				Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	// huma describes the file as binary but takes no example for it from a tag.
	if op := a.OpenAPI().Paths["/v1/bills/{bill_id}/evidence"].Post; op != nil && op.RequestBody != nil {
		if mt := op.RequestBody.Content["multipart/form-data"]; mt != nil && mt.Schema != nil && mt.Schema.Properties["file"] != nil {
			mt.Schema.Properties["file"].Examples = []any{"%PDF-1.7 … (the file's bytes)"}
		}
	}

	register(a, authz.ListBillEvidence, huma.Operation{
		OperationID: "listBillEvidence", Method: http.MethodGet, Path: "/v1/bills/{bill_id}/evidence", Tags: []string{"Evidence"},
		Summary:     "List a bill's evidence",
		Description: "The files attached to a bill or a vendor credit, oldest first, removed ones included, a page at a time (FR-DOC-05). Read-only; no side effects.",
	}, []string{CodeNotFound, CodeCursor, bill.CodeNotBill}, func(ctx context.Context, in *listBillEvidenceInput) (*billEvidenceListOutput, error) {
		id, err := parseID("bill_id", in.BillID)
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
		out := billEvidenceListOutput{Body: BillEvidencePage{Items: []BillEvidence{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if _, err := billDocument(ctx, u, id); err != nil {
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
				out.Body.Items = append(out.Body.Items, billEvidenceOf(e))
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.GetBillEvidence, huma.Operation{
		OperationID: "getBillEvidence", Method: http.MethodGet, Path: "/v1/bills/{bill_id}/evidence/{evidence_id}", Tags: []string{"Evidence"},
		Summary:     "Get a bill's evidence file",
		Description: "Returns a file's details, hashes, state and retain-until, with its ETag (FR-DOC-05, FR-DOC-07). Read-only; no side effects.",
	}, []string{CodeNotFound, bill.CodeNotBill}, func(ctx context.Context, in *BillEvidencePath) (*billEvidenceReadOutput, error) {
		bid, err := parseID("bill_id", in.BillID)
		if err != nil {
			return nil, err
		}
		id, err := parseID("evidence_id", in.EvidenceID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out billEvidenceReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			e, err := billEvidence(ctx, u, bid, id)
			out.Body, out.ETag = billEvidenceOf(e), etag(e.Version)
			return err
		})
		return &out, err
	})

	register(a, authz.RemoveBillEvidence, huma.Operation{
		OperationID: "removeBillEvidence", Method: http.MethodDelete, Path: "/v1/bills/{bill_id}/evidence/{evidence_id}", Tags: []string{"Evidence"},
		Summary: "Remove evidence from a draft bill",
		Description: "Takes a file off a draft bill or vendor credit (FR-DOC-07). Once it is posted its evidence is append-only, and the " +
			"database refuses. The row stays, marked removed, for the audit trail; the staged file is deleted by the next sweep. Side " +
			"effects: the removal and its audit event. Needs If-Match with the file's ETag. Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, storage.CodeEvidence, storage.CodeVersion, bill.CodeNotBill}, locks...),
		func(ctx context.Context, in *removeBillEvidenceInput) (*billEvidenceWriteOutput, error) {
			bid, err := parseID("bill_id", in.BillID)
			if err != nil {
				return nil, err
			}
			id, err := parseID("evidence_id", in.EvidenceID)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (BillEvidence, error) {
				cur, err := billEvidence(ctx, u, bid, id)
				if err != nil {
					return BillEvidence{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(cur.Version)); err != nil {
					return BillEvidence{}, err
				}
				e, err := u.Evidence().Remove(ctx, id, cur.Version)
				return billEvidenceOf(e), err
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &billEvidenceWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &billEvidenceWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Body: w.Body}, nil
		})
}
