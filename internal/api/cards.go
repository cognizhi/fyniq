package api

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/banktransaction"
	"github.com/cognizhi/fyniq/internal/domain/claim"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ company-card spend (FR-AP-18; p1-expense-claims)
//
// A card money account names its holder; the holder's My claims shows the card's lines, imported as any bank line is,
// and they complete each: spent for the company, with the expense policy's category and GST code, which categorises the
// line as Banking would; or personal spend, which they owe and refund (design D9). The receipt is attached to what the
// line made. Only the holder completes their card's lines, and only to the policy's categories.

// CodeCardNotDone is a receipt for a card line not completed yet: it is attached to what completing it makes.
const CodeCardNotDone = "CLAIM-35"

// CardLineCodes are the codes completing a card line answers.
var CardLineCodes = []string{journal.CodeNotYourCard, journal.CodeLineDone, claim.CodeCategory, claim.CodeAdvanceRefund, banktransaction.CodeGSTCode,
	banktransaction.CodeTooLong, claim.CodeTooLong, ledger.CodeAdvance, ledger.CodeNotMoneyAccount, ledger.CodePaymentAmount}

// CardLine is a line of a company card its holder completes from My claims.
type CardLine struct {
	ID             string `json:"id" format:"uuid" doc:"The bank line's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c01"`
	MoneyAccountID string `json:"money_account_id" format:"uuid" doc:"The card's money account." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b10"`
	Card           string `json:"card" doc:"The card's name." example:"Corporate Visa"`
	BookingDate    string `json:"booking_date" format:"date" doc:"The day it was booked on the card." example:"2026-10-14"`
	Amount         Money  `json:"amount" doc:"What moved, in the card's currency: spend is negative."`
	Description    string `json:"description,omitempty" doc:"What the statement says." example:"GRAB *TRIP SINGAPORE"`
	Counterparty   string `json:"counterparty,omitempty" doc:"Who was paid, as the statement says." example:"Grab"`
	Status         string `json:"status" enum:"unreconciled,possible_duplicate,categorised,matched,transferred,suspense,excluded" doc:"Where the line stands in Banking's queue: unreconciled lines wait to be completed." example:"unreconciled"`
	Completed      string `json:"completed,omitempty" enum:"spend,personal" doc:"How it was completed: spend for the company, or personal spend the holder owes." example:"spend"`
	DocumentID     string `json:"document_id,omitempty" format:"uuid" doc:"What completing it made: a bank transaction, or the personal spend recorded as an advance. Its receipt is attached to it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c09"`
	Version        int    `json:"version" doc:"The line's version; it makes the ETag." example:"1"`
}

// CardLinePage is a page of the caller's card lines, newest first.
type CardLinePage struct {
	Items      []CardLine `json:"items" nullable:"false"`
	NextCursor string     `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjEwMCJ9"`
}

// CardSpendDraft is how the holder completes a card line.
type CardSpendDraft struct {
	CategoryID  string `json:"category_id,omitempty" format:"uuid" doc:"Spend for the company: the expense policy's category it was for (listClaimCategories); its account is the cost." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c20"`
	GSTCode     string `json:"gst_code,omitempty" doc:"Spend for the company: the GST code of the receipt, such as TX when it shows GST; left out, the account's default." example:"TX"`
	Description string `json:"description,omitempty" maxLength:"140" doc:"What it was for." example:"Taxi to Acme"`
	Personal    bool   `json:"personal,omitempty" doc:"true when it was personal spend: the holder owes it, recorded as an advance to them, and refunds it (recordCashAdvanceRefund)." example:"false"`
}

// CardLinePath names a card line in the path.
type CardLinePath struct {
	LineID string `path:"line_id" format:"uuid" doc:"The card line's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c01"`
}

type cardLineListInput struct {
	PageQuery
	Waiting bool `query:"waiting" doc:"Only lines still waiting to be completed." example:"true"`
}

type cardLineListOutput struct{ Body CardLinePage }

type completeCardInput struct {
	CardLinePath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body CardSpendDraft
}

type cardLineOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The line's version, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     CardLine
}

// CardEvidence is a file on what completing a card line made, as the API shows it.
type CardEvidence struct {
	ID             string `json:"id" format:"uuid" doc:"The file's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e21"`
	LineID         string `json:"line_id" format:"uuid" doc:"The card line." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c01"`
	DocumentID     string `json:"document_id" format:"uuid" doc:"What completing it made, which the file is attached to." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c09"`
	EvidenceType   string `json:"evidence_type" doc:"What the file is." example:"receipt"`
	Note           string `json:"note,omitempty" doc:"A short note." example:"Taxi e-receipt"`
	RelatesOn      string `json:"relates_on,omitempty" format:"date" doc:"The date it relates to." example:"2026-10-14"`
	FileName       string `json:"file_name" doc:"The name it was uploaded with: data, never a path." example:"receipt.png"`
	MediaType      string `json:"media_type" doc:"Its type, decided from its content." example:"image/png"`
	Size           int64  `json:"size_bytes" doc:"Its size as stored, in bytes." example:"48213"`
	SHA256         string `json:"sha256" doc:"The SHA-256 of the file as stored, in hexadecimal: image metadata such as location is stripped first." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	OriginalSHA256 string `json:"original_sha256" doc:"The SHA-256 of the file as received, in hexadecimal." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	Source         string `json:"source" enum:"upload,paste,email,api,generated" doc:"How it arrived." example:"paste"`
	DataClass      string `json:"data_class" enum:"general,personal,bank,payroll,identity" doc:"The personal data it holds (FR-DOC-09)." example:"general"`
	UploadedBy     string `json:"uploaded_by" doc:"Who uploaded it: an actor of the audit trail." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	UploadedAt     string `json:"uploaded_at" format:"date-time" doc:"When it was attached." example:"2026-10-14T09:15:00Z"`
	State          string `json:"state" enum:"attached,removed,final" doc:"Attached until it is committed to the retention-locked store, then final: append-only." example:"final"`
	RetainUntil    string `json:"retain_until,omitempty" format:"date-time" doc:"Once final: kept, and never deletable, until then (FR-DOC-02)." example:"2034-01-01T00:00:00+08:00"`
	Version        int    `json:"version" doc:"The file's version; it makes the ETag." example:"1"`
}

// CardEvidencePage is a page of a card line's files, oldest first.
type CardEvidencePage struct {
	Items      []CardEvidence `json:"items" nullable:"false" doc:"The files on this page."`
	NextCursor string         `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTIifQ"`
}

type cardEvidenceListOutput struct{ Body CardEvidencePage }

type cardEvidenceWriteOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The file's version, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     CardEvidence
}

func cardEvidenceOf(e storage.Evidence, line uuid.UUID) CardEvidence {
	j := evidenceOf(e)
	return CardEvidence{ID: j.ID, LineID: line.String(), DocumentID: j.JournalID, EvidenceType: j.EvidenceType, Note: j.Note, RelatesOn: j.RelatesOn,
		FileName: j.FileName, MediaType: j.MediaType, Size: j.Size, SHA256: j.SHA256, OriginalSHA256: j.OriginalSHA256, Source: j.Source,
		DataClass: j.DataClass, UploadedBy: j.UploadedBy, UploadedAt: j.UploadedAt, State: j.State, RetainUntil: j.RetainUntil, Version: j.Version}
}

type addCardEvidenceInput struct {
	CardLinePath
	KeyHeader
	IfMatchHeader
	PreferHeader
	RawBody huma.MultipartFormFiles[EvidenceUpload]
}

type listCardEvidenceInput struct {
	CardLinePath
	PageQuery
}

// cardLineOf is a card line as its holder sees it, with what completing it made.
func cardLineOf(ctx context.Context, u storage.UnitOfWork, l storage.BankLine, card storage.MoneyAccount) (CardLine, error) {
	cur, err := money.Lookup(l.Currency)
	if err != nil {
		return CardLine{}, err
	}
	amount, err := money.ParseStored(string(l.Amount), cur)
	if err != nil {
		return CardLine{}, err
	}
	out := CardLine{ID: l.ID.String(), MoneyAccountID: card.ID.String(), Card: card.Name, BookingDate: dateOf(l.BookingDate), Amount: moneyOut(amount),
		Description: l.Description, Counterparty: l.Counterparty, Status: l.Status, Version: l.Version}
	doc, err := cardDocument(ctx, u, l.ID)
	if err != nil || doc == nil {
		return out, err
	}
	out.DocumentID = doc.ID.String()
	switch doc.Type {
	case storage.DocumentCashAdvance:
		out.Completed = "personal"
	case storage.DocumentBankTransaction:
		out.Completed = "spend"
	}
	return out, nil
}

// cardDocument is what a line's live match names, or nil while it has none.
func cardDocument(ctx context.Context, u storage.UnitOfWork, line uuid.UUID) (*storage.Document, error) {
	matches, err := u.BankMatching().Matches(ctx, storage.BankMatchFilter{LineID: &line})
	if err != nil {
		return nil, err
	}
	for _, m := range matches {
		if m.DocumentID == nil {
			continue
		}
		doc, err := u.Documents().Get(ctx, *m.DocumentID)
		if err != nil {
			return nil, err
		}
		return &doc, nil
	}
	return nil, nil
}

// cardLine is a line of a card the caller holds, as they see it: not found for anyone else's.
func cardLine(ctx context.Context, u storage.UnitOfWork, p authn.Principal, id uuid.UUID) (storage.BankLine, storage.MoneyAccount, error) {
	l, card, err := journal.CardLine(ctx, u, p.UserID, id)
	var ce *claim.Error
	if errors.As(err, &ce) && ce.Code == journal.CodeNotYourCard {
		return l, card, ports.ErrNotFound
	}
	return l, card, err
}

func registerCards(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}

	register(a, authz.ListCardLines, huma.Operation{
		OperationID: "listCardLines", Method: http.MethodGet, Path: "/v1/card-lines", Tags: []string{"Expense claims"},
		Summary: "List the lines of your company cards",
		Description: "The lines of the company cards the caller holds (FR-AP-18), newest first, a page at a time, or only those still " +
			"waiting to be completed: each with what completing it made. Nobody sees another holder's card lines here; finance sees " +
			"them in Banking. Read-only; no side effects.",
	}, []string{CodeClaimNotYours, CodeCursor}, func(ctx context.Context, in *cardLineListInput) (*cardLineListOutput, error) {
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		skip, _ := strconv.Atoi(after) // a cursor is the number of lines on the pages before
		p, err := claimant(ctx)
		if err != nil {
			return nil, err
		}
		out := cardLineListOutput{Body: CardLinePage{Items: []CardLine{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			accounts, err := u.Banking().MoneyAccounts(ctx)
			if err != nil {
				return err
			}
			type held struct {
				line storage.BankLine
				card storage.MoneyAccount
			}
			var lines []held
			for _, m := range accounts {
				if m.Kind != storage.MoneyCard || m.CardholderID == nil || *m.CardholderID != p.UserID {
					continue
				}
				f := storage.BankLineFilter{MoneyAccountID: &m.ID}
				if in.Waiting {
					f.Statuses = []string{storage.LineUnreconciled, storage.LinePossibleDuplicate}
				}
				ls, err := u.Banking().Lines(ctx, f)
				if err != nil {
					return err
				}
				for _, l := range ls {
					lines = append(lines, held{l, m})
				}
			}
			slices.SortStableFunc(lines, func(x, y held) int {
				return -strings.Compare(dateOf(x.line.BookingDate)+x.line.ID.String(), dateOf(y.line.BookingDate)+y.line.ID.String())
			})
			for i, x := range lines {
				switch {
				case i < skip:
					continue
				case len(out.Body.Items) == in.PageSize:
					out.Body.NextCursor = encodeCursor(strconv.Itoa(i))
					return nil
				}
				c, err := cardLineOf(ctx, u, x.line, x.card)
				if err != nil {
					return err
				}
				out.Body.Items = append(out.Body.Items, c)
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.CompleteCardLine, huma.Operation{
		OperationID: "completeCardLine", Method: http.MethodPost, Path: "/v1/card-lines/{line_id}/complete", Tags: []string{"Expense claims"},
		Summary: "Complete a line of your company card",
		Description: "Completes a waiting line of a company card the caller holds (FR-AP-18, design D9). Spent for the company: with a " +
			"category of the expense policy and its GST code, the line is categorised to the category's account as Banking categorises " +
			"it (bank_transaction@1). Personal spend: it is recorded as an advance to the caller from the card, Dr the advances account " +
			"· Cr the card (cash_advance@1), and the line matched to it; the caller owes it until they refund it, and no claim settles it. " +
			"Then attach the receipt (addCardLineEvidence). Only the card's holder completes its lines. Side effects: the bank " +
			"transaction or the advance, its entry, the line's match and status, audit events. Needs If-Match with the line's ETag. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview returns the line as it would be and saves nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeClaimNotYours, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}, postingCodes()...), CardLineCodes...),
		func(ctx context.Context, in *completeCardInput) (*cardLineOutput, error) {
			id, err := parseID("line_id", in.LineID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			spend := journal.CardSpend{LineID: id, GSTCode: strings.TrimSpace(in.Body.GSTCode), Description: strings.TrimSpace(in.Body.Description),
				Personal: in.Body.Personal}
			switch {
			case !in.Body.Personal && in.Body.CategoryID == "":
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.category_id", Message: "is required for spend for the company",
					Fix: "Send the expense policy's category, or personal: true"}})
			case !in.Body.Personal:
				if spend.CategoryID, err = uuid.Parse(in.Body.CategoryID); err != nil {
					return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.category_id", Message: "is not an ID", Fix: "Send the category's ID"}})
				}
			}
			p, err := claimant(ctx)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (CardLine, error) {
				l, _, err := cardLine(ctx, u, p, id)
				if err != nil {
					return CardLine{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(l.Version)); err != nil {
					return CardLine{}, err
				}
				spend.At = a.deps.Clock.Now()
				if _, err := a.bank.CompleteCard(ctx, p.Organisation, p.UserID, spend); err != nil {
					return CardLine{}, bankProblems("The card line cannot be completed", err)
				}
				l, card, err := cardLine(ctx, u, p, id)
				if err != nil {
					return CardLine{}, err
				}
				return cardLineOf(ctx, u, l, card)
			})
			if err != nil {
				return nil, err
			}
			return &cardLineOutput{Status: w.Status, ETag: etag(w.Body.Version), Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})

	register(a, authz.AddCardLineEvidence, huma.Operation{
		OperationID: "addCardLineEvidence", Method: http.MethodPost, Path: "/v1/card-lines/{line_id}/evidence", Tags: []string{"Evidence"},
		DefaultStatus: http.StatusCreated, Summary: "Attach the receipt of a line of your company card",
		Description: "Attaches a file, such as the receipt pasted or dropped, to what completing a line of the caller's own card made " +
			"(FR-AP-18, FR-DOC-05), as multipart/form-data: the bank transaction, or the personal spend. The line is completed first " +
			"(CLAIM-35). The file's type is decided from its content; it is scanned for malware and its image metadata, such as location, " +
			"is stripped before it is stored (FR-DOC-08); it is committed to the retention-locked store and is append-only (FR-DOC-13). " +
			"Side effects: the file, its evidence row and its audit event with the SHA-256 of the file as received and as stored " +
			"(FR-DOC-07). Only the card's holder attaches. Needs If-Match with the line's ETag. Idempotent with Idempotency-Key; Prefer: " +
			"return=preview runs every check and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused,
		CodeIdempotencyBusy, CodeIdempotencyOld, CodeUnavailable, CodeClaimNotYours, CodeCardNotDone}, append(locks, EvidenceCodes...)...),
		func(ctx context.Context, in *addCardEvidenceInput) (*cardEvidenceWriteOutput, error) {
			id, err := parseID("line_id", in.LineID)
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
			// The request's identity for Idempotency-Key: the line, the file's hash and what was said about it.
			request := struct {
				Line, IfMatch, SHA256, Name string
				Details                     evidence.Details
			}{in.LineID, in.IfMatch, file.OriginalSHA256, file.Name, d}
			key := evidence.StagingKey(p.Organisation, uuid.Must(uuid.NewV7()))
			if !wantsPreview(in.Prefer) {
				// Staged before the unit of work: if it rolls back, the sweeper deletes the file (FR-DOC-13).
				if err := a.deps.Staging.Put(ctx, key, bytes.NewReader(file.Data)); err != nil {
					logFailure(ctx, err)
					return nil, problem(CodeUnavailable, "The file could not be stored.", "Try again in a few minutes.")
				}
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, request, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (CardEvidence, error) {
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
				e, err := u.Evidence().Attach(ctx, storage.NewEvidence{DocumentID: doc.ID, EvidenceType: d.EvidenceType, Note: d.Note, RelatesOn: d.RelatesOn,
					FileName: file.Name, MediaType: file.MediaType, Size: int64(len(file.Data)), SHA256: file.SHA256, OriginalSHA256: file.OriginalSHA256,
					Source: d.Source, DataClass: d.DataClass, StagingKey: key, UploadedBy: uploader(ctx)})
				if err != nil {
					return CardEvidence{}, err
				}
				if a.deps.Outbox != nil { // committed now, not at the next sweep: what it is attached to is posted
					if _, err := a.deps.Outbox.Enqueue(ctx, u, p.Organisation, storage.NewOutboxItem{Class: storage.ClassWorker,
						Kind: EvidenceCommitKind, Payload: []byte(`{}`)}); err != nil {
						return CardEvidence{}, err
					}
				}
				return cardEvidenceOf(e, l.ID), nil
			})
			if (err != nil || w.Replayed) && !wantsPreview(in.Prefer) {
				_ = a.deps.Staging.Delete(context.WithoutCancel(ctx), key) // no row uses it; the sweeper would delete it a day later
			}
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &cardEvidenceWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &cardEvidenceWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	if op := a.OpenAPI().Paths["/v1/card-lines/{line_id}/evidence"].Post; op != nil && op.RequestBody != nil {
		if mt := op.RequestBody.Content["multipart/form-data"]; mt != nil && mt.Schema != nil && mt.Schema.Properties["file"] != nil {
			mt.Schema.Properties["file"].Examples = []any{"%PDF-1.7 … (the file's bytes)"}
		}
	}

	register(a, authz.ListCardLineEvidence, huma.Operation{
		OperationID: "listCardLineEvidence", Method: http.MethodGet, Path: "/v1/card-lines/{line_id}/evidence", Tags: []string{"Evidence"},
		Summary: "List the receipts of a line of your company card",
		Description: "The files attached to what completing a line of the caller's own card made, oldest first, a page at a time " +
			"(FR-AP-18, FR-DOC-05); none while it is not completed. Read-only; no side effects.",
	}, []string{CodeNotFound, CodeCursor, CodeClaimNotYours}, func(ctx context.Context, in *listCardEvidenceInput) (*cardEvidenceListOutput, error) {
		id, err := parseID("line_id", in.LineID)
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
		p, err := claimant(ctx)
		if err != nil {
			return nil, err
		}
		out := cardEvidenceListOutput{Body: CardEvidencePage{Items: []CardEvidence{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			l, _, err := cardLine(ctx, u, p, id)
			if err != nil {
				return err
			}
			doc, err := cardDocument(ctx, u, l.ID)
			if err != nil || doc == nil {
				return err
			}
			page, err := u.Evidence().OfDocument(ctx, doc.ID, after, in.PageSize+1)
			if err != nil {
				return err
			}
			if len(page) > in.PageSize {
				page = page[:in.PageSize]
				out.Body.NextCursor = encodeCursor(page[len(page)-1].ID.String())
			}
			for _, e := range page {
				out.Body.Items = append(out.Body.Items, cardEvidenceOf(e, l.ID))
			}
			return nil
		})
		return &out, err
	})
}
