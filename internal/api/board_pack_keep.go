package api

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A published board pack's PDF kept as evidence (FR-RPT-06, FR-DOC-05, FR-DOC-06; p2-reporting-planning task 3.1, rev 0.607). The PDF is a file Fyniq made: keeping it is a request
// another person approves (the object board_pack_pdf; in Founder mode the other founder, as a request is never signed by its requester). Until then the file waits attached to a draft
// document of its own, board_pack_pdf, one per pack; the approval posts it, and the worker commits the file to the retention-locked store, append-only (FR-DOC-13). The database refuses a
// kept PDF without the approval, a second one for a pack, and a reversal (migration 001110).

// More stable codes of the board pack (ENG-23).
const (
	CodePackKept = "BPACK-06" // the pack's PDF is kept already
)

// BoardPackKeeping is where a published pack's PDF is as evidence.
type BoardPackKeeping struct {
	PackID      string `json:"pack_id" format:"uuid" doc:"The published pack." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6a10"`
	State       string `json:"state" enum:"not_kept,waiting,kept" doc:"not_kept: no one has asked; waiting: another person's approval is awaited, or was refused or lapsed and may be asked again; kept: the PDF is evidence, append-only." example:"kept"`
	DocumentID  string `json:"document_id,omitempty" format:"uuid" doc:"The document holding the file, once asked." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6a40"`
	FileName    string `json:"file_name,omitempty" doc:"The file's name." example:"board-pack-2026-10-v1.pdf"`
	SHA256      string `json:"sha256,omitempty" doc:"The SHA-256 of the file, in hexadecimal: the Download the PDF file of the pack has the same." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	Size        int64  `json:"size_bytes,omitempty" doc:"Its size in bytes." example:"48213"`
	EvidenceID  string `json:"evidence_id,omitempty" format:"uuid" doc:"The evidence file." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e20"`
	FileState   string `json:"file_state,omitempty" enum:"attached,final" doc:"attached while it waits; final once committed to the retention-locked store." example:"final"`
	RetainUntil string `json:"retain_until,omitempty" format:"date-time" doc:"Once final: kept until then (FR-DOC-02)." example:"2034-01-01T00:00:00+08:00"`
	KeptAt      string `json:"kept_at,omitempty" format:"date-time" doc:"When it was approved." example:"2026-11-05T03:00:00Z"`
}

type boardPackKeepingInput struct{ BoardPackPath }

type boardPackKeepRequestInput struct {
	BoardPackPath
	KeyHeader
	PreferHeader
}

type boardPackKeepingOutput struct {
	Body BoardPackKeeping
}

type boardPackPDFPayload struct {
	PackID      string `json:"pack_id"`
	PackVersion int    `json:"pack_version"`
	Month       string `json:"month"`
	Date        string `json:"date"`
	SHA256      string `json:"sha256"`
	FileName    string `json:"file_name"`
}

// keptPDFOf is a pack's document of its kept PDF, if one was asked for: the posted one if there is one, else the draft.
func keptPDFOf(ctx context.Context, u storage.UnitOfWork, pack uuid.UUID) (storage.Document, boardPackPDFPayload, bool, error) {
	all, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentBoardPackPDF, Limit: 1000})
	if err != nil {
		return storage.Document{}, boardPackPDFPayload{}, false, err
	}
	var found storage.Document
	var fp boardPackPDFPayload
	ok := false
	for _, d := range all {
		var p boardPackPDFPayload
		if json.Unmarshal(d.Payload, &p) != nil || p.PackID != pack.String() {
			continue
		}
		if !ok || d.Status == storage.DocumentPosted {
			found, fp, ok = d, p, true
		}
	}
	return found, fp, ok, nil
}

// boardPackPDFDependencies are what a request to keep a pack's PDF is signed over: the document's version and status, the pack and its version, and the file's hash.
func boardPackPDFDependencies(ctx context.Context, u storage.UnitOfWork, key string) (int, map[string]string, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return 0, nil, err
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return 0, nil, err
	}
	if doc.Type != storage.DocumentBoardPackPDF {
		return 0, nil, fmt.Errorf("document %s is a %s, not a kept board pack PDF", doc.ID, doc.Type)
	}
	var p boardPackPDFPayload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return 0, nil, err
	}
	return doc.Version, map[string]string{"status": string(doc.Status), "pack": p.PackID, "pack_version": strconv.Itoa(p.PackVersion), "sha256": p.SHA256}, nil
}

// executeBoardPackPDFKeep is what the approval does: the document posts with its file, and the worker commits the file at once rather than at the next sweep (FR-DOC-13).
func (a *API) executeBoardPackPDFKeep(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest) error {
	id, err := uuid.Parse(r.ObjectKey)
	if err != nil {
		return err
	}
	doc, err := equity.RecordIn(ctx, u, storage.DocumentBoardPackPDF, id)
	if err != nil {
		return err
	}
	var p boardPackPDFPayload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return err
	}
	if err := u.Audit().Record(ctx, storage.AuditRecord{Action: "board_pack.pdf_kept", ObjectType: "document", ObjectID: doc.ID,
		Changes: map[string][2]any{"kept": {nil, map[string]any{"pack": p.PackID, "month": p.Month, "version": p.PackVersion, "file": p.FileName, "sha256": p.SHA256,
			"approval": r.ID.String()}}}}); err != nil {
		return err
	}
	if a.deps.Outbox != nil {
		pr, _ := authn.From(ctx)
		if _, err := a.deps.Outbox.Enqueue(ctx, u, pr.Organisation, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: EvidenceCommitKind, Payload: []byte(`{}`)}); err != nil {
			return err
		}
	}
	return nil
}

func (a *API) boardPackKeepingOf(ctx context.Context, u storage.UnitOfWork, pack uuid.UUID) (BoardPackKeeping, error) {
	out := BoardPackKeeping{PackID: pack.String(), State: "not_kept"}
	doc, p, ok, err := keptPDFOf(ctx, u, pack)
	if err != nil || !ok {
		return out, err
	}
	out.DocumentID, out.FileName, out.SHA256, out.State = doc.ID.String(), p.FileName, p.SHA256, "waiting"
	files, err := u.Evidence().OfDocument(ctx, doc.ID, uuid.Nil, 10)
	if err != nil {
		return out, err
	}
	for _, e := range files {
		if e.State == storage.EvidenceRemoved {
			continue
		}
		out.EvidenceID, out.Size, out.FileState = e.ID.String(), e.Size, string(e.State)
		if e.RetainUntil != nil {
			out.RetainUntil = e.RetainUntil.UTC().Format("2006-01-02T15:04:05Z07:00")
		}
		break
	}
	if doc.Status == storage.DocumentPosted {
		out.State = "kept"
		if doc.PostedAt != nil {
			out.KeptAt = doc.PostedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
		}
	}
	return out, nil
}

func registerBoardPackKeeping(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeNotFound}, locks...)

	register(a, authz.GetBoardPackKeeping, huma.Operation{
		OperationID: "getBoardPackKeeping", Method: http.MethodGet, Path: "/v1/board-packs/{id}/pdf/keeping", Tags: []string{"Reports"},
		Summary: "See whether a board pack's PDF is kept as evidence",
		Description: "Where a published pack's PDF is as evidence (FR-RPT-06, FR-DOC-05): not kept, waiting for another person's approval, or kept (the file's name, hash and size, and once committed to " +
			"the retention-locked store, until when it is kept). A draft pack is not found. Read as exportBoardPack is, by a Board viewer as well. Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *boardPackKeepingInput) (*boardPackKeepingOutput, error) {
		p, _ := authn.From(ctx)
		id, err := parseID("id", in.ID)
		if err != nil {
			return nil, err
		}
		var out BoardPackKeeping
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			b, err := u.BoardPacks().Get(ctx, id)
			if errors.Is(err, ports.ErrNotFound) || err == nil && b.Status != storage.PackPublished {
				return problem(CodeNotFound, "No published board pack with that ID is open to you.", "List the board packs")
			}
			if err != nil {
				return err
			}
			out, err = a.boardPackKeepingOf(ctx, u, id)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &boardPackKeepingOutput{Body: out}, nil
	})

	register(a, authz.RequestBoardPackKeeping, huma.Operation{
		OperationID: "requestBoardPackKeeping", Method: http.MethodPost, Path: "/v1/board-packs/{id}/pdf/keeping", Tags: []string{"Reports"}, DefaultStatus: http.StatusAccepted,
		Summary: "Ask to keep a published board pack's PDF as evidence",
		Description: "Lays the published pack out as its PDF, attaches the file as generated evidence to a document of its own and asks another person to approve keeping it (FR-RPT-06, FR-DOC-05, " +
			"FR-DOC-06): an Owner or Controller who did not ask, in Founder mode the other founder. The answer is 202 with an approval request. The file is the same bytes as exportBoardPack's, and " +
			"its hash is in the request, so an approver signs for exactly that file. Approved, the file is committed to the retention-locked store and is append-only (FR-DOC-13); refused or " +
			"lapsed, it may be asked for again. A pack keeps one PDF (BPACK-06); a correction is the next version of the pack. Only the Owner and Controller ask (BPACK-01), stepped up. " +
			"Side effects: the draft document, the evidence row and file in staging, the approval request and its notification. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.",
	}, append([]string{CodeNotFound, CodePackRole, CodePackKept, CodeReportRender, CodeStepUpRequired, CodeSessionRequired, CodeUnavailable, approval.CodePending, storage.CodeApproval}, writes...),
		func(ctx context.Context, in *boardPackKeepRequestInput) (*equityAskOutput, error) {
			p, _ := authn.From(ctx)
			if !mayPublishPacks(p) {
				return nil, problem(CodePackRole, "Only an Owner or Controller asks to keep a board pack's PDF.", "Ask one of them")
			}
			f, err := a.renderPublishedPack(ctx, p, in.ID)
			if err != nil {
				return nil, err
			}
			packID, err := parseID("id", in.ID)
			if err != nil {
				return nil, err
			}
			hash := hex.EncodeToString(f.sum[:])
			staging := evidence.StagingKey(p.Organisation, uuid.Must(uuid.NewV7()))
			used := false // a row of the unit of work keeps the staged file
			if !wantsPreview(in.Prefer) {
				if err := a.deps.Staging.Put(ctx, staging, bytes.NewReader(f.data)); err != nil {
					logFailure(ctx, err)
					return nil, problem(CodeUnavailable, "The file could not be stored.", "Try again in a few minutes.")
				}
			}
			request := struct{ Pack, SHA256 string }{in.ID, hash}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, request, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
				doc, payload, ok, err := keptPDFOf(ctx, u, packID)
				if err != nil {
					return Approval{}, err
				}
				if ok && doc.Status == storage.DocumentPosted {
					return Approval{}, problem(CodePackKept, "The PDF of that pack is kept already.", "Open the pack: a correction is the next version of the pack, with its own PDF")
				}
				if !ok {
					date := f.pack.Month + "-01"
					if f.pack.PublishedAt != nil {
						date = f.pack.PublishedAt.UTC().Format("2006-01-02")
					}
					payload = boardPackPDFPayload{PackID: packID.String(), PackVersion: f.pack.Version, Month: f.pack.Month, Date: date, SHA256: hash, FileName: f.name}
					b, err := json.Marshal(payload)
					if err != nil {
						return Approval{}, err
					}
					if doc, err = u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentBoardPackPDF, Payload: b}); err != nil {
						return Approval{}, err
					}
					if _, err = u.Evidence().Attach(ctx, storage.NewEvidence{DocumentID: doc.ID, EvidenceType: "board pack PDF", Note: "The PDF of the board pack for " + f.pack.Month + ", version " + strconv.Itoa(f.pack.Version),
						FileName: f.name, MediaType: "application/pdf", Size: int64(len(f.data)), SHA256: hash, OriginalSHA256: hash, Source: storage.SourceGenerated, DataClass: storage.ClassGeneral,
						StagingKey: staging, UploadedBy: uploader(ctx)}); err != nil {
						return Approval{}, err
					}
					used = true
				} else if payload.SHA256 != hash {
					return Approval{}, problem(CodeInvalid, "The PDF laid out now differs from the one waiting for approval.", "Open the pack and contact support: a published pack's PDF is the same each time")
				}
				diff := []ApprovalDiff{{Field: "board pack", After: f.pack.Month + ", version " + strconv.Itoa(f.pack.Version)}, {Field: "file", After: f.name}, {Field: "sha256", After: hash}}
				return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectBoardPackPDF, ObjectKey: doc.ID.String(), Operation: approval.OpBoardPackPDFKeep,
					Summary: "Keep the PDF of the " + f.pack.Month + " board pack (version " + strconv.Itoa(f.pack.Version) + ") as evidence",
					Execute: map[string]any{"document_id": doc.ID.String(), "pack_id": packID.String(), "sha256": hash}, Diff: diff})
			})
			if !wantsPreview(in.Prefer) && (!used || err != nil || w.Replayed) { // no row keeps it (a replay, a refusal, or a file already waiting): the sweeper would delete it a day later
				_ = a.deps.Staging.Delete(context.WithoutCancel(ctx), staging)
			}
			if err != nil {
				return nil, err
			}
			return &equityAskOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})
}
