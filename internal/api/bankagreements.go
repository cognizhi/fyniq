package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A money account's bank agreements as evidence (FR-DOC-05, FR-BNK-17; p1-bank-import-reconcile task 8.5, design D13):
// the files of a source switch, such as DBS's agreement for RAPID, on a document of their own, bank_agreement, one per
// money account, made with its first file and posted at once, so every file is committed to the retention-locked store
// and is append-only (FR-DOC-13).

type moneyAccountEvidenceInput struct {
	MoneyAccountPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	RawBody huma.MultipartFormFiles[EvidenceUpload]
}

type moneyAccountEvidenceListInput struct {
	MoneyAccountPath
	PageQuery
}

type agreementPayload struct {
	MoneyAccountID uuid.UUID `json:"money_account_id"`
}

// agreements is a money account's bank agreement document, made and posted when create is true and it has none; ok is
// false when it has none and create is false.
func agreements(ctx context.Context, u storage.UnitOfWork, account uuid.UUID, create bool) (storage.Document, bool, error) {
	all, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentBankAgreement, Limit: 1000})
	if err != nil {
		return storage.Document{}, false, err
	}
	for _, d := range all {
		var p agreementPayload
		if json.Unmarshal(d.Payload, &p) == nil && p.MoneyAccountID == account {
			return d, true, nil
		}
	}
	if !create {
		return storage.Document{}, false, nil
	}
	b, err := json.Marshal(agreementPayload{MoneyAccountID: account})
	if err != nil {
		return storage.Document{}, false, err
	}
	d, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentBankAgreement, Payload: b})
	if err != nil {
		return d, false, err
	}
	if err := u.Documents().SetStatus(ctx, d.ID, storage.DocumentPosted); err != nil {
		return d, false, err
	}
	d.Status = storage.DocumentPosted
	return d, true, nil
}

func registerBankAgreements(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}

	register(a, authz.AddMoneyAccountEvidence, huma.Operation{
		OperationID: "addMoneyAccountEvidence", Method: http.MethodPost, Path: "/v1/money-accounts/{money_account_id}/evidence", Tags: []string{"Banking"},
		DefaultStatus: http.StatusCreated, Summary: "Attach a bank agreement to a money account",
		Description: "Attaches a file to a money account, as multipart/form-data: the bank's agreements of a source switch, such as DBS's for " +
			"RAPID (FR-DOC-05, FR-BNK-17). The file is checked and scanned as any evidence is (FR-DOC-08) and committed to the " +
			"retention-locked store at once: it is append-only (FR-DOC-13). Side effects: the file in the staging store, its evidence row and " +
			"audit event, and, with the account's first file, the account's bank agreement record. Needs If-Match with the account's ETag, " +
			"so the file goes on the account as you read it. Idempotent with Idempotency-Key; Prefer: return=preview runs every check and " +
			"saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused,
		CodeIdempotencyBusy, CodeIdempotencyOld, CodeUnavailable}, append(locks, EvidenceCodes...)...),
		func(ctx context.Context, in *moneyAccountEvidenceInput) (*evidenceWriteOutput, error) {
			id, err := parseID("money_account_id", in.MoneyAccountID)
			if err != nil {
				return nil, err
			}
			p, _ := authn.From(ctx)
			file, d, err := a.upload(ctx, in.RawBody.Data())
			if err != nil {
				return nil, err
			}
			request := struct {
				Account, IfMatch, SHA256, Name string
				Details                        evidence.Details
			}{in.MoneyAccountID, in.IfMatch, file.OriginalSHA256, file.Name, d}
			key := evidence.StagingKey(p.Organisation, uuid.Must(uuid.NewV7()))
			if !wantsPreview(in.Prefer) {
				// Staged before the unit of work: if it rolls back, the sweeper deletes the file (FR-DOC-13).
				if err := a.deps.Staging.Put(ctx, key, bytes.NewReader(file.Data)); err != nil {
					logFailure(ctx, err)
					return nil, problem(CodeUnavailable, "The file could not be stored.", "Try again in a few minutes.")
				}
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, request, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (Evidence, error) {
				m, err := u.Banking().MoneyAccount(ctx, id)
				if err != nil {
					return Evidence{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(m.Version)); err != nil {
					return Evidence{}, err
				}
				doc, _, err := agreements(ctx, u, id, true)
				if err != nil {
					return Evidence{}, err
				}
				e, err := u.Evidence().Attach(ctx, storage.NewEvidence{DocumentID: doc.ID, EvidenceType: d.EvidenceType, Note: d.Note, RelatesOn: d.RelatesOn,
					FileName: file.Name, MediaType: file.MediaType, Size: int64(len(file.Data)), SHA256: file.SHA256, OriginalSHA256: file.OriginalSHA256,
					Source: d.Source, DataClass: d.DataClass, StagingKey: key, UploadedBy: uploader(ctx)})
				if err != nil {
					return Evidence{}, err
				}
				if a.deps.Outbox != nil { // committed now, not at the next sweep
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
			return &evidenceWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})
	if op := a.OpenAPI().Paths["/v1/money-accounts/{money_account_id}/evidence"].Post; op != nil && op.RequestBody != nil {
		if mt := op.RequestBody.Content["multipart/form-data"]; mt != nil && mt.Schema != nil && mt.Schema.Properties["file"] != nil {
			mt.Schema.Properties["file"].Examples = []any{"%PDF-1.7 … (the file's bytes)"}
		}
	}

	register(a, authz.ListMoneyAccountEvidence, huma.Operation{
		OperationID: "listMoneyAccountEvidence", Method: http.MethodGet, Path: "/v1/money-accounts/{money_account_id}/evidence", Tags: []string{"Banking"},
		Summary:     "List a money account's bank agreements",
		Description: "The files attached to a money account, oldest first, a page at a time (FR-DOC-05). Read-only; no side effects.",
	}, []string{CodeNotFound, CodeCursor}, func(ctx context.Context, in *moneyAccountEvidenceListInput) (*evidenceListOutput, error) {
		id, err := parseID("money_account_id", in.MoneyAccountID)
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
			if _, err := u.Banking().MoneyAccount(ctx, id); err != nil {
				return err
			}
			doc, ok, err := agreements(ctx, u, id, false)
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
