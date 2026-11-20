package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	capdom "github.com/cognizhi/fyniq/internal/domain/capture"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ receipt suggestions for a card line (FR-AP-08, p2-bank-feeds-payments task 3.1)
//
// A receipt mailed to the holder's own capture address and read by the model is suggested for a line of their own card
// when its total is the line's amount to the cent and its date is near the booking (capture.SuggestsFor). It is only a suggestion: the holder attaches it,
// once the line is completed, and the capture is then dismissed with the reason. Nothing of another member's or of the organisation's inbox is shown.

// CardReceiptSuggestion is a captured receipt that may be the receipt of a card line.
type CardReceiptSuggestion struct {
	CaptureID string `json:"capture_id" format:"uuid" doc:"The captured document." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d41"`
	FileName  string `json:"file_name" doc:"The name it was captured with: data, never a path." example:"taxi.pdf"`
	MediaType string `json:"media_type" doc:"Its type." example:"application/pdf"`
	Supplier  string `json:"supplier,omitempty" doc:"Who issued it, as the model read it (the model's reading, not a figure Fyniq computed)." example:"ComfortDelGro"`
	Date      string `json:"date" format:"date" doc:"The date it shows." example:"2026-10-19"`
	Total     string `json:"total" doc:"The total it shows, a decimal string." example:"27.30"`
}

// CardReceiptSuggestions are the suggestions for one card line.
type CardReceiptSuggestions struct {
	Items      []CardReceiptSuggestion `json:"items" nullable:"false" doc:"Captured receipts of the same amount near the booking date, newest first."`
	NextCursor string                  `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjEwMCJ9"`
}

type cardSuggestionsInput struct {
	CardLinePath
	PageQuery
}

type cardSuggestionsOutput struct{ Body CardReceiptSuggestions }

// CardReceiptAttach names the suggestion the holder attaches.
type CardReceiptAttach struct {
	CaptureID string `json:"capture_id" format:"uuid" doc:"A capture the line's suggestions name." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d41"`
}

type attachSuggestionInput struct {
	CardLinePath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body CardReceiptAttach
}

// theHoldersOwn is whether the document arrived at the holder's own capture address: only those are the holder's to be offered. (An upload or paste has no
// owner, and an Employee may not upload to the inbox.)
func theHoldersOwn(c storage.DocumentCapture, p authn.Principal) bool {
	return c.OwnerUserID != nil && *c.OwnerUserID == p.UserID
}

// suggestedFor is whether read capture c is a suggestion for line, and what it read.
func suggestedFor(c storage.DocumentCapture, p authn.Principal, line storage.BankLine) (capdom.Reading, bool) {
	var r capdom.Reading
	if c.Status != storage.CaptureRead || c.DraftID != nil || !theHoldersOwn(c, p) || json.Unmarshal(c.Reading, &r) != nil {
		return r, false
	}
	return r, capdom.SuggestsFor(r, string(line.Amount), line.Currency, line.BookingDate)
}

func registerCardReceiptSuggestions(a *API) {
	register(a, authz.ListCardReceiptSuggestions, huma.Operation{
		OperationID: "listCardReceiptSuggestions", Method: http.MethodGet, Path: "/v1/card-lines/{line_id}/receipt-suggestions", Tags: []string{"Expense claims"},
		Summary: "Suggest captured receipts for a line of your company card",
		Description: "The receipts the caller captured themselves, read by the model and not yet used, whose total is the line's amount to the cent and whose date " +
			"is within 5 days of the booking (FR-AP-08), newest first, a page at a time. Only the caller's own captures are considered, never the organisation's or another " +
			"member's. A suggestion is never attached by itself (attachCardReceiptSuggestion). Read-only; no side effects.",
	}, []string{CodeNotFound, CodeClaimNotYours, CodeCursor}, func(ctx context.Context, in *cardSuggestionsInput) (*cardSuggestionsOutput, error) {
		id, err := parseID("line_id", in.LineID)
		if err != nil {
			return nil, err
		}
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		skip, _ := strconv.Atoi(after) // a cursor is the number of suggestions on the pages before
		p, err := claimant(ctx)
		if err != nil {
			return nil, err
		}
		out := cardSuggestionsOutput{Body: CardReceiptSuggestions{Items: []CardReceiptSuggestion{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			l, _, err := cardLine(ctx, u, p, id)
			if err != nil {
				return err
			}
			caps, err := u.DocumentCaptures().List(ctx, p.UserID, storage.CaptureRead, uuid.Nil, 200)
			if err != nil {
				return err
			}
			n := 0
			for _, c := range caps {
				r, ok := suggestedFor(c, p, l)
				if !ok {
					continue
				}
				n++
				switch {
				case n <= skip:
					continue
				case len(out.Body.Items) == in.PageSize:
					out.Body.NextCursor = encodeCursor(strconv.Itoa(n - 1))
					return nil
				}
				s := CardReceiptSuggestion{CaptureID: c.ID.String(), FileName: c.FileName, MediaType: c.MediaType, Date: *r.Date.Value, Total: *r.Total.Value}
				if r.Supplier.Value != nil {
					s.Supplier = *r.Supplier.Value
				}
				out.Body.Items = append(out.Body.Items, s)
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.AttachCardReceiptSuggestion, huma.Operation{
		OperationID: "attachCardReceiptSuggestion", Method: http.MethodPost, Path: "/v1/card-lines/{line_id}/receipt-suggestions/attach", Tags: []string{"Evidence"},
		DefaultStatus: http.StatusCreated, Summary: "Attach a suggested receipt to a line of your company card",
		Description: "Attaches the captured receipt the caller names, which must be one of the line's suggestions (listCardReceiptSuggestions), to what completing the " +
			"line made (FR-AP-08, FR-DOC-05); the line is completed first (CLAIM-35). The capture is then dismissed with the reason that it became a card line's " +
			"receipt. Side effects: the evidence row and its audit event, the capture's status. Only the card's holder attaches, and only their own capture. Needs " +
			"If-Match with the line's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks everything and saves nothing.",
	}, []string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeUnavailable, CodeClaimNotYours,
		CodeCardNotDone, CodeInvalid, storage.CodeRetry, storage.CodeLockTimeout},
		func(ctx context.Context, in *attachSuggestionInput) (*cardEvidenceWriteOutput, error) {
			id, err := parseID("line_id", in.LineID)
			if err != nil {
				return nil, err
			}
			capID, err := uuid.Parse(strings.TrimSpace(in.Body.CaptureID))
			if err != nil {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.capture_id", Message: "is not an ID", Fix: "Send the capture_id of one of the line's suggestions"}})
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			p, err := claimant(ctx)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (CardEvidence, error) {
				l, _, err := cardLine(ctx, u, p, id)
				if err != nil {
					return CardEvidence{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(l.Version)); err != nil {
					return CardEvidence{}, err
				}
				doc, err := cardDocument(ctx, u, l.ID)
				if err != nil {
					return CardEvidence{}, err
				}
				if doc == nil {
					return CardEvidence{}, problem(CodeCardNotDone, "The line is not completed yet: its receipt is attached to what completing it makes.",
						"Complete the line first, then attach its receipt")
				}
				c, err := visibleCapture(ctx, u, p, capID, true)
				if err != nil {
					return CardEvidence{}, err
				}
				r, ok := suggestedFor(c, p, l)
				if !ok {
					return CardEvidence{}, problem(CodeNotFound, "That document is not a suggestion for this line.", "List the line's suggestions and attach one of them")
				}
				blob, err := a.deps.Staging.Get(ctx, c.BlobKey)
				if err != nil {
					logFailure(ctx, err)
					return CardEvidence{}, problem(CodeUnavailable, "The captured file is no longer kept.", "Capture it again.")
				}
				data, err := io.ReadAll(io.LimitReader(blob, evidence.MaxBytes+1))
				_ = blob.Close()
				if err != nil {
					return CardEvidence{}, err
				}
				evKey := evidence.StagingKey(p.Organisation, uuid.Must(uuid.NewV7()))
				if !storage.IsPreview(ctx) {
					if err := a.deps.Staging.Put(ctx, evKey, bytes.NewReader(data)); err != nil {
						logFailure(ctx, err)
						return CardEvidence{}, problem(CodeUnavailable, "The file could not be stored.", "Try again in a few minutes.")
					}
				}
				source := map[string]storage.EvidenceSource{storage.CaptureUpload: storage.SourceUpload, storage.CapturePaste: storage.SourcePaste, storage.CaptureEmail: storage.SourceEmail}[c.Source]
				e, err := u.Evidence().Attach(ctx, storage.NewEvidence{DocumentID: doc.ID, EvidenceType: "receipt", Note: "Captured in the document inbox, suggested for the card line",
					RelatesOn: nil, FileName: c.FileName, MediaType: c.MediaType, Size: int64(len(data)), SHA256: c.SHA256, OriginalSHA256: c.SHA256, NRICFlagged: c.NRICFlagged,
					Source: source, DataClass: storage.ClassGeneral, StagingKey: evKey, UploadedBy: uploader(ctx)})
				if err != nil {
					return CardEvidence{}, err
				}
				if _, err := u.DocumentCaptures().Dismiss(ctx, c.ID, c.Version, p.UserID, "Became the receipt of a card line ("+*r.Total.Value+")", a.deps.Clock.Now()); err != nil {
					return CardEvidence{}, err
				}
				if a.deps.Outbox != nil {
					if _, err := a.deps.Outbox.Enqueue(ctx, u, p.Organisation, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: EvidenceCommitKind, Payload: []byte(`{}`)}); err != nil {
						return CardEvidence{}, err
					}
				}
				return cardEvidenceOf(e, l.ID), nil
			})
			if err != nil {
				return nil, err
			}
			return &cardEvidenceWriteOutput{Status: w.Status, ETag: etag(w.Body.ID), Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})
}
