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
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ an invoice's evidence (FR-DOC-05, 07, 08, 11, 13)
//
// The customer's purchase order, a signed delivery note and anything else an invoice rests on (FR-AR-03's attachments),
// attached as a bill's evidence is: on a draft a file comes off again; once the invoice is issued its files are committed
// to the retention-locked store and are append-only (p1-sales task 1.4; PRD-001 rev 0.117). The evidence rules per
// activity (FR-DOC-06) arrive later in P1.

// InvoiceEvidence is a file on an invoice, as the API shows it.
type InvoiceEvidence struct {
	ID             string `json:"id" format:"uuid" doc:"The file's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e21"`
	InvoiceID      string `json:"invoice_id" format:"uuid" doc:"The invoice it is attached to." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e30"`
	EvidenceType   string `json:"evidence_type" doc:"What the file is." example:"invoice"`
	Note           string `json:"note,omitempty" doc:"A short note." example:"The customer's purchase order"`
	RelatesOn      string `json:"relates_on,omitempty" format:"date" doc:"The date it relates to." example:"2026-10-14"`
	FileName       string `json:"file_name" doc:"The name it was uploaded with: data, never a path." example:"AU-1182.pdf"`
	MediaType      string `json:"media_type" doc:"Its type, decided from its content." example:"application/pdf"`
	Size           int64  `json:"size_bytes" doc:"Its size as stored, in bytes." example:"48213"`
	SHA256         string `json:"sha256" doc:"The SHA-256 of the file as stored, in hexadecimal: image metadata such as location is stripped first." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	OriginalSHA256 string `json:"original_sha256" doc:"The SHA-256 of the file as received, in hexadecimal." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	Source         string `json:"source" enum:"upload,paste,email,api,generated" doc:"How it arrived: generated for a file Fyniq made, such as the PDF of a quote or invoice it emailed (FR-AR-06)." example:"upload"`
	DataClass      string `json:"data_class" enum:"general,personal,bank,payroll,identity" doc:"The personal data it holds (FR-DOC-09)." example:"general"`
	UploadedBy     string `json:"uploaded_by" doc:"Who uploaded it: an actor of the audit trail." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	UploadedAt     string `json:"uploaded_at" format:"date-time" doc:"When it was attached." example:"2026-10-14T09:15:00Z"`
	State          string `json:"state" enum:"attached,removed,final" doc:"Attached (in staging while the invoice is a draft), removed from a draft, or final: committed to the retention-locked store, append-only." example:"attached"`
	RetainUntil    string `json:"retain_until,omitempty" format:"date-time" doc:"Once final: kept, and never deletable, until then (FR-DOC-02)." example:"2034-01-01T00:00:00+08:00"`
	Version        int    `json:"version" doc:"The file's version; it makes the ETag." example:"1"`
}

// InvoiceEvidencePage is a page of an invoice's files, oldest first.
type InvoiceEvidencePage struct {
	Items      []InvoiceEvidence `json:"items" nullable:"false" doc:"The files on this page, removed ones included."`
	NextCursor string            `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTIifQ"`
}

// InvoiceEvidencePath names an invoice's file.
type InvoiceEvidencePath struct {
	InvoicePath
	EvidenceID string `path:"evidence_id" format:"uuid" doc:"The file's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e21"`
}

type addInvoiceEvidenceInput struct {
	InvoicePath
	KeyHeader
	IfMatchHeader
	PreferHeader
	RawBody huma.MultipartFormFiles[EvidenceUpload]
}

type listInvoiceEvidenceInput struct {
	InvoicePath
	PageQuery
}

type removeInvoiceEvidenceInput struct {
	InvoiceEvidencePath
	IfMatchHeader
	PreferHeader
}

type invoiceEvidenceListOutput struct{ Body InvoiceEvidencePage }

type invoiceEvidenceReadOutput struct {
	ETag string `header:"ETag" doc:"The file's version, for If-Match."`
	Body InvoiceEvidence
}

type invoiceEvidenceWriteOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The file's version, for If-Match."`
	Location string `header:"Location" doc:"The file's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     InvoiceEvidence
}

func invoiceEvidenceOf(e storage.Evidence) InvoiceEvidence {
	j := evidenceOf(e)
	return InvoiceEvidence{ID: j.ID, InvoiceID: j.JournalID, EvidenceType: j.EvidenceType, Note: j.Note, RelatesOn: j.RelatesOn, FileName: j.FileName,
		MediaType: j.MediaType, Size: j.Size, SHA256: j.SHA256, OriginalSHA256: j.OriginalSHA256, Source: j.Source, DataClass: j.DataClass,
		UploadedBy: j.UploadedBy, UploadedAt: j.UploadedAt, State: j.State, RetainUntil: j.RetainUntil, Version: j.Version}
}

// invoiceDocument returns the invoice a file is under: refused for another kind of document.
func invoiceDocument(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Document, error) {
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return doc, err
	}
	if !invoice.IsInvoice(doc) {
		return doc, problem(invoice.CodeNotInvoice, "That record is not an invoice.", "Use the invoice's ID.")
	}
	return doc, nil
}

// invoiceEvidence returns a file of the invoice: not found when it is on another record.
func invoiceEvidence(ctx context.Context, u storage.UnitOfWork, invoiceID, id uuid.UUID) (storage.Evidence, error) {
	if _, err := invoiceDocument(ctx, u, invoiceID); err != nil {
		return storage.Evidence{}, err
	}
	e, err := u.Evidence().Get(ctx, id)
	if err != nil {
		return e, err
	}
	if e.DocumentID != invoiceID {
		return e, problem(CodeNotFound, "That file is not attached to this invoice.", "List the invoice's evidence to find its files.")
	}
	return e, nil
}

func registerInvoiceEvidence(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.AddInvoiceEvidence, huma.Operation{
		OperationID: "addInvoiceEvidence", Method: http.MethodPost, Path: "/v1/invoices/{invoice_id}/evidence", Tags: []string{"Evidence"},
		DefaultStatus: http.StatusCreated, Summary: "Attach evidence to an invoice",
		Description: "Attaches a file, such as the customer's purchase order, to an invoice (FR-AR-03, FR-DOC-05, FR-DOC-11; PRD-001 rev 0.117), " +
			"as multipart/form-data. The file's type is decided from its content; it is scanned for malware and its image metadata, such as " +
			"location, is stripped before it is stored (FR-DOC-08). Side effects: the file in the staging store, its evidence row and its " +
			"audit event with the SHA-256 of the file as received and as stored, the uploader, the source and the invoice (FR-DOC-07). On a " +
			"draft the file can be removed; on an issued or voided invoice it is committed to the retention-locked store and is append-only " +
			"(FR-DOC-13). Needs If-Match with the invoice's ETag, so the file goes on the invoice as you read it. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview runs every check and saves nothing. AI and MCP clients cannot upload: they propose doc.attach.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused,
		CodeIdempotencyBusy, CodeIdempotencyOld, CodeUnavailable, invoice.CodeNotInvoice}, append(locks, EvidenceCodes...)...),
		func(ctx context.Context, in *addInvoiceEvidenceInput) (*invoiceEvidenceWriteOutput, error) {
			id, err := parseID("invoice_id", in.InvoiceID)
			if err != nil {
				return nil, err
			}
			p, _ := authn.From(ctx)
			file, d, err := a.upload(ctx, in.RawBody.Data())
			if err != nil {
				return nil, err
			}
			// The request's identity for Idempotency-Key: the invoice, the file's hash and what was said about it.
			request := struct {
				Invoice, IfMatch, SHA256, Name string
				Details                        evidence.Details
			}{in.InvoiceID, in.IfMatch, file.OriginalSHA256, file.Name, d}
			key := evidence.StagingKey(p.Organisation, uuid.Must(uuid.NewV7()))
			if !wantsPreview(in.Prefer) {
				// Staged before the unit of work: if it rolls back, the sweeper deletes the file (FR-DOC-13).
				if err := a.deps.Staging.Put(ctx, key, bytes.NewReader(file.Data)); err != nil {
					logFailure(ctx, err)
					return nil, problem(CodeUnavailable, "The file could not be stored.", "Try again in a few minutes.")
				}
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, request, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (InvoiceEvidence, error) {
				doc, err := invoiceDocument(ctx, u, id)
				if err != nil {
					return InvoiceEvidence{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(doc.Version, string(doc.Status))); err != nil {
					return InvoiceEvidence{}, err
				}
				e, err := u.Evidence().Attach(ctx, storage.NewEvidence{DocumentID: id, EvidenceType: d.EvidenceType, Note: d.Note, RelatesOn: d.RelatesOn,
					FileName: file.Name, MediaType: file.MediaType, Size: int64(len(file.Data)), SHA256: file.SHA256, OriginalSHA256: file.OriginalSHA256,
					Source: d.Source, DataClass: d.DataClass, StagingKey: key, UploadedBy: uploader(ctx)})
				if err != nil {
					return InvoiceEvidence{}, err
				}
				if doc.Status != storage.DocumentDraft && a.deps.Outbox != nil { // committed now, not at the next sweep
					if _, err := a.deps.Outbox.Enqueue(ctx, u, p.Organisation, storage.NewOutboxItem{Class: storage.ClassWorker,
						Kind: EvidenceCommitKind, Payload: []byte(`{}`)}); err != nil {
						return InvoiceEvidence{}, err
					}
				}
				return invoiceEvidenceOf(e), nil
			})
			if (err != nil || w.Replayed) && !wantsPreview(in.Prefer) {
				_ = a.deps.Staging.Delete(context.WithoutCancel(ctx), key) // no row uses it; the sweeper would delete it a day later
			}
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &invoiceEvidenceWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &invoiceEvidenceWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Location: "/v1/invoices/" + in.InvoiceID + "/evidence/" + w.Body.ID,
				Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	// huma describes the file as binary but takes no example for it from a tag.
	if op := a.OpenAPI().Paths["/v1/invoices/{invoice_id}/evidence"].Post; op != nil && op.RequestBody != nil {
		if mt := op.RequestBody.Content["multipart/form-data"]; mt != nil && mt.Schema != nil && mt.Schema.Properties["file"] != nil {
			mt.Schema.Properties["file"].Examples = []any{"%PDF-1.7 … (the file's bytes)"}
		}
	}

	register(a, authz.ListInvoiceEvidence, huma.Operation{
		OperationID: "listInvoiceEvidence", Method: http.MethodGet, Path: "/v1/invoices/{invoice_id}/evidence", Tags: []string{"Evidence"},
		Summary:     "List an invoice's evidence",
		Description: "The files attached to an invoice, oldest first, removed ones included, a page at a time (FR-DOC-05). Read-only; no side effects.",
	}, []string{CodeNotFound, CodeCursor, invoice.CodeNotInvoice}, func(ctx context.Context, in *listInvoiceEvidenceInput) (*invoiceEvidenceListOutput, error) {
		id, err := parseID("invoice_id", in.InvoiceID)
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
		out := invoiceEvidenceListOutput{Body: InvoiceEvidencePage{Items: []InvoiceEvidence{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if _, err := invoiceDocument(ctx, u, id); err != nil {
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
				out.Body.Items = append(out.Body.Items, invoiceEvidenceOf(e))
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.GetInvoiceEvidence, huma.Operation{
		OperationID: "getInvoiceEvidence", Method: http.MethodGet, Path: "/v1/invoices/{invoice_id}/evidence/{evidence_id}", Tags: []string{"Evidence"},
		Summary:     "Get an invoice's evidence file",
		Description: "Returns a file's details, hashes, state and retain-until, with its ETag (FR-DOC-05, FR-DOC-07). Read-only; no side effects.",
	}, []string{CodeNotFound, invoice.CodeNotInvoice}, func(ctx context.Context, in *InvoiceEvidencePath) (*invoiceEvidenceReadOutput, error) {
		bid, err := parseID("invoice_id", in.InvoiceID)
		if err != nil {
			return nil, err
		}
		id, err := parseID("evidence_id", in.EvidenceID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out invoiceEvidenceReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			e, err := invoiceEvidence(ctx, u, bid, id)
			out.Body, out.ETag = invoiceEvidenceOf(e), etag(e.Version)
			return err
		})
		return &out, err
	})

	register(a, authz.RemoveInvoiceEvidence, huma.Operation{
		OperationID: "removeInvoiceEvidence", Method: http.MethodDelete, Path: "/v1/invoices/{invoice_id}/evidence/{evidence_id}", Tags: []string{"Evidence"},
		Summary: "Remove evidence from a draft invoice",
		Description: "Takes a file off a draft invoice (FR-DOC-07). Once it is issued its evidence is append-only, and the " +
			"database refuses. The row stays, marked removed, for the audit trail; the staged file is deleted by the next sweep. Side " +
			"effects: the removal and its audit event. Needs If-Match with the file's ETag. Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, storage.CodeEvidence, storage.CodeVersion, invoice.CodeNotInvoice}, locks...),
		func(ctx context.Context, in *removeInvoiceEvidenceInput) (*invoiceEvidenceWriteOutput, error) {
			bid, err := parseID("invoice_id", in.InvoiceID)
			if err != nil {
				return nil, err
			}
			id, err := parseID("evidence_id", in.EvidenceID)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (InvoiceEvidence, error) {
				cur, err := invoiceEvidence(ctx, u, bid, id)
				if err != nil {
					return InvoiceEvidence{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(cur.Version)); err != nil {
					return InvoiceEvidence{}, err
				}
				e, err := u.Evidence().Remove(ctx, id, cur.Version)
				return invoiceEvidenceOf(e), err
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &invoiceEvidenceWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &invoiceEvidenceWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Body: w.Body}, nil
		})
}
