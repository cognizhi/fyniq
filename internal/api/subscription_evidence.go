package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A subscription contract's evidence (FR-AR-15, FR-DOC-05): the signed order form, the customer's email or an amendment letter, as files on a document of their own, subscription_file, one per
// contract, made with its first file and posted at once so every file is committed to the retention-locked store and is append-only (FR-DOC-13). A file may name the amendment it supports.

type subscriptionEvidenceInput struct {
	SubscriptionContractPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	AmendmentID string `query:"amendment_id" format:"uuid" doc:"The amendment of this contract the file supports; left out, it supports the contract." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5a02"`
	RawBody     huma.MultipartFormFiles[EvidenceUpload]
}

type subscriptionEvidenceListInput struct {
	SubscriptionContractPath
	PageQuery
}

type subscriptionFilePayload struct {
	ContractID uuid.UUID `json:"contract_id"`
}

// subscriptionFile is a contract's file document, made and posted when create is true and it has none; ok is false when it has none and create is false.
func subscriptionFile(ctx context.Context, u storage.UnitOfWork, contract uuid.UUID, create bool) (storage.Document, bool, error) {
	all, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentSubscriptionFile, Limit: 1000})
	if err != nil {
		return storage.Document{}, false, err
	}
	for _, d := range all {
		var p subscriptionFilePayload
		if json.Unmarshal(d.Payload, &p) == nil && p.ContractID == contract {
			return d, true, nil
		}
	}
	if !create {
		return storage.Document{}, false, nil
	}
	b, err := json.Marshal(subscriptionFilePayload{ContractID: contract})
	if err != nil {
		return storage.Document{}, false, err
	}
	d, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentSubscriptionFile, Payload: b})
	if err != nil {
		return d, false, err
	}
	if err := u.Documents().SetStatus(ctx, d.ID, storage.DocumentPosted); err != nil {
		return d, false, err
	}
	d.Status = storage.DocumentPosted
	return d, true, nil
}

func registerSubscriptionEvidence(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}

	register(a, authz.AddSubscriptionContractEvidence, huma.Operation{
		OperationID: "addSubscriptionContractEvidence", Method: http.MethodPost, Path: "/v1/subscription-contracts/{contract_id}/evidence", Tags: []string{"Sales"},
		DefaultStatus: http.StatusCreated, Summary: "Attach a file to a subscription contract or one of its amendments",
		Description: "Attaches a file, as multipart/form-data, to a subscription contract (FR-AR-15, FR-DOC-05): the signed order form, the customer's email or an amendment letter, optionally " +
			"naming the amendment it supports with amendment_id. The file is checked and scanned as any evidence is (FR-DOC-08) and committed to the retention-locked store at once: it is " +
			"append-only (FR-DOC-13). Side effects: the file in the staging store, its evidence row and audit event, and, with the contract's first file, the contract's file record. Needs " +
			"If-Match with the contract's ETag. Idempotent with Idempotency-Key; Prefer: return=preview runs every check and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused,
		CodeIdempotencyBusy, CodeIdempotencyOld, CodeUnavailable, CodeInvalid}, append(locks, EvidenceCodes...)...),
		func(ctx context.Context, in *subscriptionEvidenceInput) (*evidenceWriteOutput, error) {
			id, err := parseID("contract_id", in.ContractID)
			if err != nil {
				return nil, err
			}
			var amendment uuid.UUID
			if in.AmendmentID != "" {
				if amendment, err = parseID("amendment_id", in.AmendmentID); err != nil {
					return nil, err
				}
			}
			p, _ := authn.From(ctx)
			file, d, err := a.upload(ctx, in.RawBody.Data())
			if err != nil {
				return nil, err
			}
			request := struct {
				Contract, Amendment, IfMatch, SHA256, Name string
				Details                                    evidence.Details
			}{in.ContractID, in.AmendmentID, in.IfMatch, file.OriginalSHA256, file.Name, d}
			key := evidence.StagingKey(p.Organisation, uuid.Must(uuid.NewV7()))
			if !wantsPreview(in.Prefer) {
				if err := a.deps.Staging.Put(ctx, key, bytes.NewReader(file.Data)); err != nil {
					logFailure(ctx, err)
					return nil, problem(CodeUnavailable, "The file could not be stored.", "Try again in a few minutes.")
				}
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, request, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (Evidence, error) {
				c, err := u.Subscriptions().Get(ctx, id)
				if err != nil {
					return Evidence{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(c.Version)); err != nil {
					return Evidence{}, err
				}
				note := d.Note
				if amendment != uuid.Nil {
					ams, err := u.Subscriptions().Amendments(ctx, id)
					if err != nil {
						return Evidence{}, err
					}
					found := false
					for _, am := range ams {
						if am.ID == amendment {
							found = true
							note = strings.TrimSpace("Supports the " + am.Kind + " from " + dateOf(am.EffectiveOn) + " (amendment " + am.ID.String() + "). " + d.Note)
						}
					}
					if !found {
						return Evidence{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "amendment_id", Message: "is not an amendment of this contract", Fix: "Send an amendment from getSubscriptionContract"}})
					}
				}
				doc, _, err := subscriptionFile(ctx, u, id, true)
				if err != nil {
					return Evidence{}, err
				}
				e, err := u.Evidence().Attach(ctx, storage.NewEvidence{DocumentID: doc.ID, EvidenceType: d.EvidenceType, Note: note, RelatesOn: d.RelatesOn,
					FileName: file.Name, MediaType: file.MediaType, Size: int64(len(file.Data)), SHA256: file.SHA256, OriginalSHA256: file.OriginalSHA256, NRICFlagged: file.NRIC,
					Source: d.Source, DataClass: d.DataClass, StagingKey: key, UploadedBy: uploader(ctx)})
				if err != nil {
					return Evidence{}, err
				}
				if a.deps.Outbox != nil {
					if _, err := a.deps.Outbox.Enqueue(ctx, u, p.Organisation, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: EvidenceCommitKind, Payload: []byte(`{}`)}); err != nil {
						return Evidence{}, err
					}
				}
				return evidenceOf(e), nil
			})
			if (err != nil || w.Replayed) && !wantsPreview(in.Prefer) {
				_ = a.deps.Staging.Delete(context.WithoutCancel(ctx), key)
			}
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &evidenceWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &evidenceWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})
	if op := a.OpenAPI().Paths["/v1/subscription-contracts/{contract_id}/evidence"].Post; op != nil && op.RequestBody != nil {
		if mt := op.RequestBody.Content["multipart/form-data"]; mt != nil && mt.Schema != nil && mt.Schema.Properties["file"] != nil {
			mt.Schema.Properties["file"].Examples = []any{"%PDF-1.7 … (the file's bytes)"}
		}
	}

	register(a, authz.ListSubscriptionContractEvidence, huma.Operation{
		OperationID: "listSubscriptionContractEvidence", Method: http.MethodGet, Path: "/v1/subscription-contracts/{contract_id}/evidence", Tags: []string{"Sales"},
		Summary:     "List a subscription contract's files",
		Description: "The files attached to a subscription contract and its amendments, oldest first, a page at a time (FR-AR-15, FR-DOC-05). Read-only; no side effects.",
	}, []string{CodeNotFound, CodeCursor}, func(ctx context.Context, in *subscriptionEvidenceListInput) (*evidenceListOutput, error) {
		id, err := parseID("contract_id", in.ContractID)
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
		size := in.PageSize
		if size <= 0 {
			size = 100
		}
		p, _ := authn.From(ctx)
		out := evidenceListOutput{Body: EvidencePage{Items: []Evidence{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if _, err := u.Subscriptions().Get(ctx, id); err != nil {
				return err
			}
			doc, ok, err := subscriptionFile(ctx, u, id, false)
			if err != nil || !ok {
				return err
			}
			page, err := u.Evidence().OfDocument(ctx, doc.ID, after, size+1)
			if err != nil {
				return err
			}
			if len(page) > size {
				page = page[:size]
				out.Body.NextCursor = encodeCursor(page[len(page)-1].ID.String())
			}
			for _, e := range page {
				out.Body.Items = append(out.Body.Items, evidenceOf(e))
			}
			return nil
		})
		return &out, err
	})
}
