package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ evidence (FR-DOC-01, 05, 07, 08, 11, 13)

// Evidence is a file on a record, as the API shows it. Its bytes are never in the API's answers: a
// download, with a safe name in its headers, arrives with the Evidence panel (FR-DOC-08).
type Evidence struct {
	ID             string `json:"id" format:"uuid" doc:"The file's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e20"`
	JournalID      string `json:"journal_id" format:"uuid" doc:"The journal it is attached to." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d70"`
	EvidenceType   string `json:"evidence_type" doc:"What the file is." example:"receipt"`
	Note           string `json:"note,omitempty" doc:"A short note." example:"Paid by PayNow"`
	RelatesOn      string `json:"relates_on,omitempty" format:"date" doc:"The date it relates to." example:"2026-10-20"`
	FileName       string `json:"file_name" doc:"The name it was uploaded with: data, never a path." example:"receipt-october.pdf"`
	MediaType      string `json:"media_type" enum:"application/pdf,image/png,image/jpeg,image/webp,text/plain,text/csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,application/vnd.openxmlformats-officedocument.wordprocessingml.document,message/rfc822,application/vnd.ms-outlook" doc:"Its type, decided from its content." example:"application/pdf"`
	Size           int64  `json:"size_bytes" doc:"Its size as stored, in bytes." example:"48213"`
	SHA256         string `json:"sha256" doc:"The SHA-256 of the file as stored, in hexadecimal: image metadata such as location is stripped first." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	OriginalSHA256 string `json:"original_sha256" doc:"The SHA-256 of the file as received, in hexadecimal." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	Source         string `json:"source" enum:"upload,paste,email,api,generated" doc:"How it arrived: generated for a file Fyniq made, such as the PDF of a quote or invoice it emailed (FR-AR-06)." example:"upload"`
	DataClass      string `json:"data_class" enum:"general,personal,bank,payroll,identity" doc:"The personal data it holds (FR-DOC-09)." example:"general"`
	UploadedBy     string `json:"uploaded_by" doc:"Who uploaded it: an actor of the audit trail." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	UploadedAt     string `json:"uploaded_at" format:"date-time" doc:"When it was attached." example:"2026-10-20T09:15:00Z"`
	State          string `json:"state" enum:"attached,removed,final" doc:"Attached (in staging while the journal is a draft), removed from a draft, or final: committed to the retention-locked store, append-only." example:"attached"`
	RetainUntil    string `json:"retain_until,omitempty" format:"date-time" doc:"Once final: kept, and never deletable, until then (FR-DOC-02)." example:"2034-01-01T00:00:00+08:00"`
	Version        int    `json:"version" doc:"The file's version; it makes the ETag." example:"1"`
}

// EvidenceUpload is a file and what the uploader says about it.
type EvidenceUpload struct {
	File         huma.FormFile `form:"file" contentType:"application/octet-stream" required:"true" doc:"The file: PDF, PNG, JPEG, WebP, CSV, plain text, XLSX, DOCX (no macros), EML or MSG, up to 25 MB. Its type is decided from its content, never from its name or Content-Type."`
	EvidenceType string        `form:"evidence_type" required:"true" minLength:"1" maxLength:"100" doc:"What the file is, such as receipt, board resolution or screenshot." example:"receipt"`
	Note         string        `form:"note" required:"false" maxLength:"1000" doc:"A short note." example:"Paid by PayNow"`
	RelatesOn    string        `form:"relates_on" required:"false" format:"date" doc:"The date it relates to." example:"2026-10-20"`
	Source       string        `form:"source" required:"false" enum:"upload,paste" doc:"How a person added it: upload (the default) or paste. An integration's file is always api." example:"upload"`
	DataClass    string        `form:"data_class" required:"false" enum:"general,personal,bank" doc:"The personal data it holds (FR-DOC-09); general by default. Payroll and identity files wait for payroll permissions." example:"general"`
}

// EvidencePage is a page of a record's files, oldest first.
type EvidencePage struct {
	Items      []Evidence `json:"items" doc:"The files on this page, removed ones included."`
	NextCursor string     `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTIifQ"`
}

// EvidencePath names a journal's file.
type EvidencePath struct {
	JournalPath
	EvidenceID string `path:"evidence_id" format:"uuid" doc:"The file's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e20"`
}

type addEvidenceInput struct {
	JournalPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	RawBody huma.MultipartFormFiles[EvidenceUpload]
}

type listEvidenceInput struct {
	JournalPath
	PageQuery
}

type removeEvidenceInput struct {
	EvidencePath
	IfMatchHeader
	PreferHeader
}

type evidenceListOutput struct{ Body EvidencePage }

type evidenceReadOutput struct {
	ETag string `header:"ETag" doc:"The file's version, for If-Match."`
	Body Evidence
}

type evidenceWriteOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The file's version, for If-Match."`
	Location string `header:"Location" doc:"The file's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     Evidence
}

// EvidenceCodes are an upload's rules (internal/domain/evidence).
var EvidenceCodes = []string{evidence.CodeType, evidence.CodeSize, evidence.CodeMalware, evidence.CodeText, evidence.CodeClass, evidence.CodeMacro, evidence.CodeArchive,
	evidence.CodeSource, evidence.CodeImage}

// EvidenceCommitKind is the outbox work that commits a record's files to the locked store (evidence.CommitKind).
const EvidenceCommitKind = evidence.CommitKind

// maxUpload is the largest multipart request: the file's limit and room for the form around it.
const maxUpload = evidence.MaxBytes + 1<<20

func evidenceOf(e storage.Evidence) Evidence {
	out := Evidence{ID: e.ID.String(), JournalID: e.DocumentID.String(), EvidenceType: e.EvidenceType, Note: e.Note, FileName: e.FileName,
		MediaType: e.MediaType, Size: e.Size, SHA256: e.SHA256, OriginalSHA256: e.OriginalSHA256, Source: string(e.Source),
		DataClass: string(e.DataClass), UploadedBy: e.UploadedBy, UploadedAt: e.UploadedAt.UTC().Format(time.RFC3339),
		State: string(e.State), Version: e.Version}
	if e.RelatesOn != nil {
		out.RelatesOn = e.RelatesOn.Format(time.DateOnly)
	}
	if e.RetainUntil != nil {
		out.RetainUntil = e.RetainUntil.UTC().Format(time.RFC3339)
	}
	return out
}

// journalDocument returns the journal a file is under: not found for another kind of document.
func journalDocument(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Document, error) {
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return doc, err
	}
	if doc.Type != storage.DocumentJournal {
		return doc, problem(journal.CodeNotJournal, "That record is not a manual journal.", "Use the journal's ID.")
	}
	return doc, nil
}

// journalEvidence returns a file of the journal: not found when it is on another record.
func journalEvidence(ctx context.Context, u storage.UnitOfWork, journalID, id uuid.UUID) (storage.Evidence, error) {
	if _, err := journalDocument(ctx, u, journalID); err != nil {
		return storage.Evidence{}, err
	}
	e, err := u.Evidence().Get(ctx, id)
	if err != nil {
		return e, err
	}
	if e.DocumentID != journalID {
		return e, problem(CodeNotFound, "That file is not attached to this journal.", "List the journal's evidence to find its files.")
	}
	return e, nil
}

// uploader is the caller as the audit trail names them.
func uploader(ctx context.Context) string {
	if a := storage.AuditFrom(ctx).Actor; a != nil {
		return a.Type + ":" + a.ID
	}
	p, _ := authn.From(ctx)
	return "user:" + p.Subject
}

// commitEvidence queues the commit of a posted journal's attached files to the locked store, in the posting's
// own unit of work (FR-DOC-13 step 4); the hourly evidence.commit job catches anything the queue misses.
func (a *API) commitEvidence(ctx context.Context, u storage.UnitOfWork, doc storage.Document, _ storage.Entry) error {
	if a.deps.Outbox == nil {
		return nil
	}
	files, err := u.Evidence().OfDocument(ctx, doc.ID, uuid.Nil, 1000)
	if err != nil {
		return err
	}
	for _, f := range files {
		if f.State == storage.EvidenceAttached {
			org, err := u.Organisation(ctx)
			if err != nil {
				return err
			}
			_, err = a.deps.Outbox.Enqueue(ctx, u, org.ID, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: EvidenceCommitKind,
				Payload: []byte(`{}`)})
			return err
		}
	}
	return nil
}

// upload reads and checks an uploaded file and what was said about it (FR-DOC-08, FR-DOC-11): the file as it would be
// stored, and its details; an invalid field, an unreadable file or a scan that is not available is a refusal.
func (a *API) upload(ctx context.Context, up *EvidenceUpload) (evidence.File, evidence.Details, error) {
	if a.deps.Staging == nil || a.deps.Scanner == nil {
		return evidence.File{}, evidence.Details{}, problem(CodeUnavailable, "Files cannot be attached here: no evidence store or malware scanner is configured.",
			"Ask your administrator to configure them (FR-DOC-01).")
	}
	if up == nil || up.File.File == nil {
		return evidence.File{}, evidence.Details{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.file", Message: "is missing", Fix: "Send the file in the form field file"}})
	}
	defer up.File.Close()
	data, err := io.ReadAll(io.LimitReader(up.File, evidence.MaxBytes+1))
	if err != nil {
		return evidence.File{}, evidence.Details{}, problem(CodeMalformed, "The file could not be read in full.", "Send it again.")
	}
	p, _ := authn.From(ctx)
	d := evidence.Details{EvidenceType: strings.TrimSpace(up.EvidenceType), Note: strings.TrimSpace(up.Note),
		Source: storage.EvidenceSource(up.Source), DataClass: storage.DataClass(up.DataClass)}
	if d.Source == "" {
		d.Source = storage.SourceUpload
	}
	if p.Class == authn.ClassIntegration {
		d.Source = storage.SourceAPI
	}
	if d.DataClass == "" {
		d.DataClass = storage.ClassGeneral
	}
	var bad []FieldProblem
	if up.RelatesOn != "" {
		t, err := time.Parse(time.DateOnly, up.RelatesOn)
		if err != nil {
			bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.relates_on", Message: "is not a date", Fix: "Send YYYY-MM-DD"})
		}
		d.RelatesOn = &t
	}
	file, problems, err := evidence.Check(ctx, a.deps.Scanner, up.File.Filename, data)
	if err != nil {
		logFailure(ctx, err)
		return evidence.File{}, evidence.Details{}, problem(CodeUnavailable, "The malware scan is not available, so the file was not attached.", "Try again in a few minutes.")
	}
	for _, x := range append(problems, evidence.CheckDetails(d)...) {
		bad = append(bad, FieldProblem{Code: x.Code, Field: "body." + x.Field, Message: x.Message, Fix: x.Fix})
	}
	if len(bad) > 0 {
		return evidence.File{}, evidence.Details{}, invalid(bad)
	}
	return file, d, nil
}

// limitUploads caps a multipart request's body (FR-DOC-08): huma applies an operation's MaxBodyBytes only to
// JSON bodies, and parses a multipart form before the handler sees it.
type limitUploads struct{ humago.Mux }

func (m limitUploads) HandleFunc(pattern string, h func(http.ResponseWriter, *http.Request)) {
	m.Mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/") {
			if r.ContentLength > maxUpload {
				p := problem(CodeTooLarge, "The request is larger than 26 MB; a file may be at most 25 MB.", "Compress the file, or split it into smaller files.")
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(http.StatusRequestEntityTooLarge)
				_ = json.NewEncoder(w).Encode(p)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
		}
		h(w, r)
	})
}

func registerEvidence(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.AddJournalEvidence, huma.Operation{
		OperationID: "addJournalEvidence", Method: http.MethodPost, Path: "/v1/journals/{journal_id}/evidence", Tags: []string{"Evidence"},
		DefaultStatus: http.StatusCreated, Summary: "Attach evidence to a journal",
		Description: "Attaches a file to a manual journal (FR-DOC-05, FR-DOC-11), as multipart/form-data. The file's type is decided " +
			"from its content; it is scanned for malware and its image metadata, such as location, is stripped before it is stored " +
			"(FR-DOC-08). Side effects: the file in the staging store, its evidence row and its audit event with the SHA-256 of the " +
			"file as received and as stored, the uploader, the source and the journal (FR-DOC-07). On a draft the file can be removed; " +
			"on a posted or reversed journal it is committed to the retention-locked store and is append-only (FR-DOC-13). Needs " +
			"If-Match with the journal's ETag, so the file goes on the journal as you read it. Idempotent with Idempotency-Key; " +
			"Prefer: return=preview runs every check and saves nothing. AI and MCP clients cannot upload: they propose doc.attach.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused,
		CodeIdempotencyBusy, CodeIdempotencyOld, CodeUnavailable, journal.CodeNotJournal}, append(locks, EvidenceCodes...)...),
		func(ctx context.Context, in *addEvidenceInput) (*evidenceWriteOutput, error) {
			id, err := parseID("journal_id", in.JournalID)
			if err != nil {
				return nil, err
			}
			p, _ := authn.From(ctx)
			file, d, err := a.upload(ctx, in.RawBody.Data())
			if err != nil {
				return nil, err
			}
			// The request's identity for Idempotency-Key: the journal, the file's hash and what was said about it.
			request := struct {
				Journal, IfMatch, SHA256, Name string
				Details                        evidence.Details
			}{in.JournalID, in.IfMatch, file.OriginalSHA256, file.Name, d}
			key := evidence.StagingKey(p.Organisation, uuid.Must(uuid.NewV7()))
			if !wantsPreview(in.Prefer) {
				// Staged before the unit of work: if it rolls back, the sweeper deletes the file (FR-DOC-13).
				if err := a.deps.Staging.Put(ctx, key, bytes.NewReader(file.Data)); err != nil {
					logFailure(ctx, err)
					return nil, problem(CodeUnavailable, "The file could not be stored.", "Try again in a few minutes.")
				}
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, request, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (Evidence, error) {
				doc, err := journalDocument(ctx, u, id)
				if err != nil {
					return Evidence{}, err
				}
				j, err := journalOf(doc)
				if err != nil {
					return Evidence{}, err
				}
				if err := checkIfMatch(in.IfMatch, journalETag(j)); err != nil {
					return Evidence{}, err
				}
				e, err := u.Evidence().Attach(ctx, storage.NewEvidence{DocumentID: id, EvidenceType: d.EvidenceType, Note: d.Note, RelatesOn: d.RelatesOn,
					FileName: file.Name, MediaType: file.MediaType, Size: int64(len(file.Data)), SHA256: file.SHA256, OriginalSHA256: file.OriginalSHA256,
					Source: d.Source, DataClass: d.DataClass, StagingKey: key, UploadedBy: uploader(ctx)})
				if err != nil {
					return Evidence{}, err
				}
				if doc.Status != storage.DocumentDraft && a.deps.Outbox != nil { // committed now, not at the next sweep
					if _, err := a.deps.Outbox.Enqueue(ctx, u, p.Organisation, storage.NewOutboxItem{Class: storage.ClassWorker,
						Kind: EvidenceCommitKind, Payload: []byte(`{}`)}); err != nil {
						return Evidence{}, err
					}
				}
				return evidenceOf(e), nil
			})
			if (err != nil || w.Replayed) && !wantsPreview(in.Prefer) {
				_ = a.deps.Staging.Delete(context.WithoutCancel(ctx), key) // no row uses it; the sweeper would delete it a day later
			}
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &evidenceWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &evidenceWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Location: "/v1/journals/" + in.JournalID + "/evidence/" + w.Body.ID,
				Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	// huma describes the file as binary but takes no example for it from a tag.
	if op := a.OpenAPI().Paths["/v1/journals/{journal_id}/evidence"].Post; op != nil && op.RequestBody != nil {
		if mt := op.RequestBody.Content["multipart/form-data"]; mt != nil && mt.Schema != nil && mt.Schema.Properties["file"] != nil {
			mt.Schema.Properties["file"].Examples = []any{"%PDF-1.7 … (the file's bytes)"}
		}
	}

	register(a, authz.ListJournalEvidence, huma.Operation{
		OperationID: "listJournalEvidence", Method: http.MethodGet, Path: "/v1/journals/{journal_id}/evidence", Tags: []string{"Evidence"},
		Summary:     "List a journal's evidence",
		Description: "The files attached to a manual journal, oldest first, removed ones included, a page at a time (FR-DOC-05). Read-only; no side effects.",
	}, []string{CodeNotFound, CodeCursor, journal.CodeNotJournal}, func(ctx context.Context, in *listEvidenceInput) (*evidenceListOutput, error) {
		id, err := parseID("journal_id", in.JournalID)
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
		out := evidenceListOutput{Body: EvidencePage{Items: []Evidence{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if _, err := journalDocument(ctx, u, id); err != nil {
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
				out.Body.Items = append(out.Body.Items, evidenceOf(e))
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.GetJournalEvidence, huma.Operation{
		OperationID: "getJournalEvidence", Method: http.MethodGet, Path: "/v1/journals/{journal_id}/evidence/{evidence_id}", Tags: []string{"Evidence"},
		Summary:     "Get a journal's evidence file",
		Description: "Returns a file's details, hashes, state and retain-until, with its ETag (FR-DOC-05, FR-DOC-07). Read-only; no side effects.",
	}, []string{CodeNotFound, journal.CodeNotJournal}, func(ctx context.Context, in *EvidencePath) (*evidenceReadOutput, error) {
		jid, err := parseID("journal_id", in.JournalID)
		if err != nil {
			return nil, err
		}
		id, err := parseID("evidence_id", in.EvidenceID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out evidenceReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			e, err := journalEvidence(ctx, u, jid, id)
			out.Body, out.ETag = evidenceOf(e), etag(e.Version)
			return err
		})
		return &out, err
	})

	register(a, authz.RemoveJournalEvidence, huma.Operation{
		OperationID: "removeJournalEvidence", Method: http.MethodDelete, Path: "/v1/journals/{journal_id}/evidence/{evidence_id}", Tags: []string{"Evidence"},
		Summary: "Remove evidence from a draft journal",
		Description: "Takes a file off a draft journal (FR-DOC-07). Once the journal is posted its evidence is append-only, and the " +
			"database refuses. The row stays, marked removed, for the audit trail; the staged file is deleted by the next sweep. Side " +
			"effects: the removal and its audit event. Needs If-Match with the file's ETag. Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, storage.CodeEvidence, storage.CodeVersion, journal.CodeNotJournal}, locks...),
		func(ctx context.Context, in *removeEvidenceInput) (*evidenceWriteOutput, error) {
			jid, err := parseID("journal_id", in.JournalID)
			if err != nil {
				return nil, err
			}
			id, err := parseID("evidence_id", in.EvidenceID)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (Evidence, error) {
				cur, err := journalEvidence(ctx, u, jid, id)
				if err != nil {
					return Evidence{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(cur.Version)); err != nil {
					return Evidence{}, err
				}
				e, err := u.Evidence().Remove(ctx, id, cur.Version)
				return evidenceOf(e), err
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &evidenceWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &evidenceWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Body: w.Body}, nil
		})
}
