package api

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/claim"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ a receipt forwarded to the claimant's own address (FR-AP-14, FR-DOC-12; rev 0.322)

// CodeMessageFile: the mail or its file is not one the caller may attach.
const CodeMessageFile = "EML-10"

// ClaimEvidenceFromMessage names a file of the caller's own mail to attach to a claim as a receipt.
type ClaimEvidenceFromMessage struct {
	MessageID    string `json:"message_id" format:"uuid" doc:"The message, from listMyInboundMessages: accepted mail at the caller's own email-in address." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f30"`
	FileID       string `json:"file_id" format:"uuid" doc:"Its attachment's id, from the same list." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f31"`
	EvidenceType string `json:"evidence_type,omitempty" maxLength:"100" doc:"What the file is; receipt by default." example:"receipt"`
	Note         string `json:"note,omitempty" maxLength:"1000" doc:"A short note." example:"Forwarded from the hotel"`
}

type addClaimEvidenceFromMessageInput struct {
	ExpenseClaimPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body ClaimEvidenceFromMessage
}

// fileCloser lets bytes read stand in for an uploaded file.
type fileCloser struct{ *bytes.Reader }

func (fileCloser) Close() error { return nil }

func registerClaimEvidenceFromMessage(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.AddExpenseClaimEvidenceFromMessage, huma.Operation{
		OperationID: "addExpenseClaimEvidenceFromMessage", Method: http.MethodPost, Path: "/v1/expense-claims/{claim_id}/evidence:from-message", Tags: []string{"Evidence"},
		DefaultStatus: http.StatusCreated, Summary: "Attach a receipt you forwarded to your claim",
		Description: "Attaches an attachment of mail at the caller's own email-in address, which they forwarded as a receipt (FR-AP-14, FR-DOC-12; " +
			"PRD-001 rev 0.322), to the caller's own draft claim. The mail must be accepted: from the caller's own verified sign-in email, or from " +
			"a sender they accepted. The file is checked again as any upload is (type from its content, malware scan, image metadata stripped, " +
			"FR-DOC-08) and its source is recorded as email (FR-DOC-07). A message or file that is not the caller's, not accepted or not an " +
			"attachment is EML-10. Side effects: the file in the staging store, its evidence row and its audit event. Only the claimant attaches. " +
			"Needs If-Match with the claim's ETag. Idempotent with Idempotency-Key; Prefer: return=preview runs every check and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeUnavailable,
		CodeClaimNotYours, claim.CodeNotClaim, CodeMessageFile, CodeInvalid}, append(locks, EvidenceCodes...)...),
		func(ctx context.Context, in *addClaimEvidenceFromMessageInput) (*claimEvidenceWriteOutput, error) {
			id, err := parseID("claim_id", in.ClaimID)
			if err != nil {
				return nil, err
			}
			messageID, err := parseID("body.message_id", in.Body.MessageID)
			if err != nil {
				return nil, err
			}
			fileID, err := parseID("body.file_id", in.Body.FileID)
			if err != nil {
				return nil, err
			}
			p, err := claimant(ctx)
			if err != nil {
				return nil, err
			}
			if a.deps.Staging == nil || a.deps.Scanner == nil {
				return nil, problem(CodeUnavailable, "Files cannot be attached here: no evidence store or malware scanner is configured.",
					"Ask your administrator to configure them (FR-DOC-01).")
			}
			// The mail's file, from the caller's own address only.
			var data []byte
			var name string
			if err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				f, err := ownMessageFile(ctx, u, p.UserID, messageID, fileID)
				if err != nil {
					return err
				}
				r, err := a.deps.Staging.Get(ctx, f.BlobKey)
				if err != nil {
					logFailure(ctx, err)
					return problem(CodeMessageFile, "The mail's file is no longer kept.", "Forward the receipt again, or attach it from your device.")
				}
				defer r.Close()
				if data, err = io.ReadAll(io.LimitReader(r, evidence.MaxBytes+1)); err != nil {
					return err
				}
				name = f.FileName
				return nil
			}); err != nil {
				return nil, err
			}
			kind := in.Body.EvidenceType
			if kind == "" {
				kind = "receipt"
			}
			file, d, err := a.upload(ctx, &EvidenceUpload{File: huma.FormFile{File: fileCloser{bytes.NewReader(data)}, Filename: name, Size: int64(len(data))},
				EvidenceType: kind, Note: in.Body.Note}) // its source is email below: a person's own form may only say upload or paste
			if err != nil {
				return nil, err
			}
			request := struct {
				Claim, IfMatch, Message, File, Type, Note string
			}{in.ClaimID, in.IfMatch, in.Body.MessageID, in.Body.FileID, kind, in.Body.Note}
			key := evidence.StagingKey(p.Organisation, uuid.Must(uuid.NewV7()))
			if !wantsPreview(in.Prefer) {
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
					FileName: file.Name, MediaType: file.MediaType, Size: int64(len(file.Data)), SHA256: file.SHA256, OriginalSHA256: file.OriginalSHA256, NRICFlagged: file.NRIC,
					Source: storage.SourceEmail, DataClass: d.DataClass, StagingKey: key, UploadedBy: uploader(ctx)})
				if err != nil {
					return ClaimEvidence{}, err
				}
				if doc.Status != storage.DocumentDraft && a.deps.Outbox != nil {
					if _, err := a.deps.Outbox.Enqueue(ctx, u, p.Organisation, storage.NewOutboxItem{Class: storage.ClassWorker,
						Kind: EvidenceCommitKind, Payload: []byte(`{}`)}); err != nil {
						return ClaimEvidence{}, err
					}
				}
				return claimEvidenceOf(e), nil
			})
			if (err != nil || w.Replayed) && !wantsPreview(in.Prefer) {
				_ = a.deps.Staging.Delete(context.WithoutCancel(ctx), key)
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
}

// ownMessageFile is the attachment fileID of message messageID, when the message is accepted mail at user's own email-in
// address for documents and the file is an attachment still kept: any other combination is EML-10, and says no more, so
// nobody learns whether another member's mail exists.
func ownMessageFile(ctx context.Context, u storage.UnitOfWork, user, messageID, fileID uuid.UUID) (storage.InboundFile, error) {
	refused := problem(CodeMessageFile, "That is not an attachment of accepted mail at your own email-in address.",
		"List your mail, accept its sender if it waits, and choose one of its attachments.")
	m, err := u.InboundMessages().Get(ctx, messageID)
	if err != nil || m.Status != storage.InboundAccepted {
		return storage.InboundFile{}, refused
	}
	addr, err := u.CaptureAddresses().Get(ctx, m.AddressID)
	if err != nil || addr.UserID == nil || *addr.UserID != user || addr.Kind != storage.CaptureDocuments {
		return storage.InboundFile{}, refused
	}
	files, err := u.InboundMessages().Files(ctx, messageID)
	if err != nil {
		return storage.InboundFile{}, err
	}
	for _, f := range files {
		if f.ID == fileID && f.Kind == storage.InboundAttachment && f.RemovedAt == nil {
			return f, nil
		}
	}
	return storage.InboundFile{}, refused
}
