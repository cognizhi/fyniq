package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/paymentbatch"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ payment batches (FR-BNK-12, FR-AP-13, SOD-1, SOD-2, FM-1)
//
// A batch pays approved bills and expense claims whole, from one SGD money account on one date, by FAST or GIRO, to the
// payees' Singapore accounts in force: one payment per supplier, one reimbursement per claimant (PRD-001 rev 0.154 and rev
// 0.156; p1-payments-out design D1, D2). It is prepared under C on Appendix D's "Payment batches", approved by another
// person who holds A, and released once by the Payment releaser, which posts its payments and writes its payment list.
// Every check is run again at each read, ask and release, all at once (ENG-23).

// PaymentBatchItem is a bill or claim a batch pays, with what is open on it now.
type PaymentBatchItem struct {
	Kind       string      `json:"kind" enum:"bill,claim" doc:"A posted bill, or an approved expense claim." example:"bill"`
	DocumentID string      `json:"document_id" format:"uuid" doc:"The bill or claim." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b10"`
	Number     string      `json:"number,omitempty" doc:"Its number." example:"BILL-0042"`
	Date       string      `json:"date,omitempty" format:"date" doc:"Its date." example:"2026-10-02"`
	Open       Money       `json:"open" doc:"What the batch pays of it: what is open on it now, in SGD."`
	Checks     []BillCheck `json:"checks" nullable:"false" doc:"What stops it being paid in the batch (✖)."`
}

// PaymentBatchPayee is whom one of a batch's payments goes to.
type PaymentBatchPayee struct {
	Kind string `json:"kind" enum:"vendor,person" doc:"A supplier, paid its bills; or a claimant, paid back their claims." example:"vendor"`
	ID   string `json:"id" format:"uuid" doc:"The vendor's contact ID, or the person's user ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4db0"`
	Name string `json:"name" doc:"The vendor's legal name, or the person's name." example:"Northwind Analytics Pte. Ltd."`
}

// PaymentBatchPayment is one payee's part of a batch: one bill payment, or one reimbursement.
type PaymentBatchPayment struct {
	Payee   PaymentBatchPayee  `json:"payee" doc:"Whom it pays."`
	Account *PayeeAccount      `json:"account,omitempty" doc:"The payee's account in force it pays into, by its last four characters; absent when they have none."`
	Frozen  bool               `json:"payments_frozen" doc:"A change of the payee's bank account waits for approval: the payment is held (PAYEE-12)." example:"false"`
	Amount  Money              `json:"amount" doc:"What it pays: the sum of its items, in SGD."`
	Items   []PaymentBatchItem `json:"items" nullable:"false" doc:"The payee's bills or claims it pays, as the batch lists them."`
	// Once released (design D8):
	PaymentID  string `json:"payment_id,omitempty" format:"uuid" doc:"Released: the bill payment or reimbursement the release posted." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5d10"`
	Paid       bool   `json:"paid" doc:"Released: bank import matched the payment to its statement line." example:"false"`
	Rejected   bool   `json:"rejected" doc:"Released: the payment was voided, as when the bank rejected it; its bills or claims are open again." example:"false"`
	Remittance string `json:"remittance,omitempty" enum:"waiting,queued,sent,no_email" doc:"A supplier's remittance advice: waiting for the bank's statement line, queued once matched, sent, or no_email when the supplier has no remittance address (⚠). Absent for a claimant." example:"sent"`
}

// PaymentBatch is a batch of payments, as its checks read it now.
type PaymentBatch struct {
	ID             string                `json:"id" format:"uuid" doc:"The batch's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c10"`
	Number         string                `json:"number,omitempty" doc:"Its number, from the batch sequence, once approved (FR-ORG-04)." example:"PB-0003"`
	Status         string                `json:"status" enum:"draft,waiting,approved,released,paid,cancelled" doc:"draft: being prepared; waiting: asked for, waiting for approval; approved: ready to release; released: its payments posted and its payment list written; paid: bank import matched every payment to its statement line; cancelled: before release, with a reason." example:"draft"`
	MoneyAccountID string                `json:"money_account_id" format:"uuid" doc:"The SGD bank account it is paid from: the chart's money account, as a payment recorded by hand names it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d10"`
	MoneyAccount   string                `json:"money_account_name,omitempty" doc:"The bank account's name in Banking." example:"DBS SGD current"`
	PaymentDate    string                `json:"payment_date" format:"date" doc:"The day the bank pays it: each payment is posted on it." example:"2026-10-31"`
	Method         string                `json:"method" enum:"fast,giro" doc:"How the bank pays it: FAST, within minutes, or GIRO, over the next working days." example:"fast"`
	Notes          string                `json:"notes,omitempty" doc:"Its notes." example:"End of October suppliers and claims"`
	Payments       []PaymentBatchPayment `json:"payments" nullable:"false" doc:"One payment per payee: a supplier's bills as one bill payment, a claimant's claims as one reimbursement."`
	Total          Money                 `json:"total" doc:"What it pays in all, in SGD."`
	Checks         []BillCheck           `json:"checks" nullable:"false" doc:"Before release: what stops it being asked for, approved or released (✖), every problem at once. Empty in a list."`
	ApprovalID     string                `json:"approval_id,omitempty" format:"uuid" doc:"The approval request it waits for." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c20"`
	Reapproval     bool                  `json:"reapproval_needed" doc:"Approved: a payee's bank account, or what is open on an item, changed since its latest approval, so it is not released until it is approved again (G4-055)." example:"false"`
	Release        *PaymentBatchRelease  `json:"release,omitempty" doc:"A released batch: who released it, when, under which approval, its file's name and hash, and the payments it posted."`
	PreparedBy     string                `json:"prepared_by,omitempty" doc:"Who saved its current version: an actor of the audit trail." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	Version        int                   `json:"version" doc:"The batch's version; with its status it makes the ETag." example:"1"`
}

// PaymentBatchPage is a page of batches, newest payment date first.
type PaymentBatchPage struct {
	Items      []PaymentBatch `json:"items" nullable:"false"`
	NextCursor string         `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjYtMTAtMzF8MDE5MiJ9"`
}

// PaymentBatchItemRef names a bill or claim a batch pays.
type PaymentBatchItemRef struct {
	Kind       string `json:"kind" enum:"bill,claim" doc:"A posted bill, or an approved expense claim." example:"bill"`
	DocumentID string `json:"document_id" format:"uuid" doc:"The bill or claim." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b10"`
}

// PaymentBatchDraft is a batch as sent.
type PaymentBatchDraft struct {
	MoneyAccountID string                `json:"money_account_id" format:"uuid" doc:"The SGD bank account it is paid from: the chart's money account, as a payment recorded by hand names it, set up in Banking." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d10"`
	PaymentDate    string                `json:"payment_date" format:"date" doc:"The day the bank pays it." example:"2026-10-31"`
	Method         string                `json:"method" enum:"fast,giro" doc:"FAST or GIRO." example:"fast"`
	Items          []PaymentBatchItemRef `json:"items" maxItems:"500" nullable:"false" doc:"The bills and claims it pays, each whole, for what is open on it; at most 500."`
	Notes          string                `json:"notes,omitempty" maxLength:"500" doc:"Its notes." example:"End of October suppliers and claims"`
}

// PaymentBatchSuggestion is what a new batch for a payment date starts with (FR-AP-13).
type PaymentBatchSuggestion struct {
	PaymentDate string                `json:"payment_date" format:"date" doc:"The payment date asked for." example:"2026-10-31"`
	Suggested   []PaymentBatchItemRef `json:"suggested" nullable:"false" doc:"Every posted SGD bill with something open planned on or before the date, then every approved claim with something owed, none in an approved batch; at most the 500 items a batch holds, oldest first."`
}

// PaymentBatchPath names a batch in the path.
type PaymentBatchPath struct {
	BatchID string `path:"batch_id" format:"uuid" doc:"The batch's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c10"`
}

type batchListInput struct {
	PageQuery
	Status string `query:"status" enum:"draft,waiting,approved,released,paid,cancelled" doc:"Only batches with this status." example:"approved"`
}

type batchSuggestInput struct {
	PaymentDate string `query:"payment_date" required:"true" format:"date" doc:"The payment date of the batch to prepare." example:"2026-10-31"`
}

type batchCreateInput struct {
	KeyHeader
	PreferHeader
	Body PaymentBatchDraft
}

type batchReplaceInput struct {
	PaymentBatchPath
	IfMatchHeader
	PreferHeader
	Body PaymentBatchDraft
}

type batchListOutput struct{ Body PaymentBatchPage }

type batchSuggestOutput struct{ Body PaymentBatchSuggestion }

type batchReadOutput struct {
	ETag string `header:"ETag" doc:"The batch's version and status, for If-Match."`
	Body PaymentBatch
}

type batchWriteOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The batch's version and status, for If-Match."`
	Location string `header:"Location" doc:"The batch's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     PaymentBatch
}

// batchDraftOf reads a batch from the request, with every invalid field at once.
func batchDraftOf(in PaymentBatchDraft) (paymentbatch.Draft, error) {
	d := paymentbatch.Draft{Method: in.Method, Notes: in.Notes}
	var fields []FieldProblem
	if in.MoneyAccountID != "" {
		id, err := uuid.Parse(in.MoneyAccountID)
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.money_account_id", Message: "is not a money account's ID", Fix: "Send the account's ID"})
		}
		d.MoneyAccountID = id
	}
	if in.PaymentDate != "" {
		t, err := time.Parse(time.DateOnly, in.PaymentDate)
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.payment_date", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"})
		}
		d.PaymentDate = t
	}
	for i, x := range in.Items {
		id, err := uuid.Parse(x.DocumentID)
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: fmt.Sprintf("body.items[%d].document_id", i), Message: "is not a document's ID",
				Fix: "Send the bill's or claim's ID"})
		}
		d.Items = append(d.Items, paymentbatch.Item{Kind: x.Kind, DocumentID: id})
	}
	if len(fields) > 0 {
		return d, invalid(fields)
	}
	return d, nil
}

// batchProblems turns a batch's failed rules into one problem listing them all.
func batchProblems(err error) error {
	probs, ok := err.(paymentbatch.Problems)
	if !ok || len(probs) == 0 {
		return err
	}
	fields := make([]FieldProblem, len(probs))
	for i, x := range probs {
		fields[i] = FieldProblem{Code: x.Code, Field: "body." + x.Field, Message: x.Message, Fix: x.Fix}
	}
	return problem(probs[0].Code, fmt.Sprintf("The payment batch cannot be saved or asked for: %d problem(s), each listed in errors.", len(probs)),
		"Correct them and send it again.", fields...)
}

// batchChecksOf are a batch's problems as the API lists them.
func batchChecksOf(errs paymentbatch.Problems) []BillCheck {
	out := []BillCheck{}
	for _, x := range errs {
		out = append(out, BillCheck{Severity: "error", Code: x.Code, Field: x.Field, Message: x.Message, Fix: x.Fix})
	}
	return out
}

// batchView is what reading batches needs once per unit of work: the ledger's settings, today, and the items of every
// approved, unreleased batch.
type batchView struct {
	settings ledger.Settings
	approved map[uuid.UUID]uuid.UUID
	now      time.Time
}

func (a *API) batchView(ctx context.Context, u storage.UnitOfWork) (batchView, error) {
	s, err := journal.LoadSettings(ctx, u)
	if err != nil {
		return batchView{}, err
	}
	approved, err := paymentbatch.Approved(ctx, u)
	return batchView{settings: s, approved: approved, now: a.deps.Clock.Now()}, err
}

// batchStatus is a batch's status as people say it.
func batchStatus(ctx context.Context, u storage.UnitOfWork, doc storage.Document) (string, *storage.ApprovalRequest, error) {
	switch doc.Status {
	case storage.DocumentReversed:
		return "cancelled", nil, nil
	case storage.DocumentPosted:
		r, err := u.PaymentBatches().ReleaseOf(ctx, doc.ID)
		if err != nil {
			return "", nil, err
		}
		if r != nil {
			return "released", nil, nil
		}
		return "approved", nil, nil
	}
	pending, err := u.Approvals().List(ctx, string(approval.Pending))
	if err != nil {
		return "", nil, err
	}
	for _, r := range pending {
		if r.ObjectType == approval.ObjectPaymentBatch && r.ObjectKey == doc.ID.String() {
			return "waiting", &r, nil
		}
	}
	return "draft", nil, nil
}

// batchOf is a batch as the API shows it; with checks, what stops it.
func (a *API) batchOf(ctx context.Context, u storage.UnitOfWork, v batchView, doc storage.Document, checks bool) (PaymentBatch, error) {
	b, err := paymentbatch.Evaluate(ctx, u, doc, v.settings, v.approved, v.now)
	if err != nil {
		return PaymentBatch{}, err
	}
	status, r, err := batchStatus(ctx, u, doc)
	if err != nil {
		return PaymentBatch{}, err
	}
	out := PaymentBatch{ID: doc.ID.String(), Number: doc.Number, Status: status, MoneyAccountID: b.Stored.MoneyAccountID.String(), PaymentDate: b.Stored.PaymentDate,
		Method: b.Stored.Method, Notes: b.Stored.Notes, Payments: []PaymentBatchPayment{}, Total: moneyOut(b.Total), Checks: []BillCheck{},
		PreparedBy: doc.SavedBy, Version: doc.Version}
	if r != nil {
		out.ApprovalID = r.ID.String()
	}
	if b.Account != nil {
		out.MoneyAccount = b.Account.Name
	}
	if status == "approved" {
		latest, deps, err := latestApproval(ctx, u, doc.ID)
		if err != nil {
			return PaymentBatch{}, err
		}
		out.Reapproval = latest == nil || !sameAccounts(deps["accounts"], b.Accounts()) || fmt.Sprint(deps["total"]) != b.Total.String()
	}
	var warnings []BillCheck
	byItem := map[uuid.UUID][]BillCheck{}
	for _, x := range b.Errors {
		if x.DocumentID != nil {
			byItem[*x.DocumentID] = append(byItem[*x.DocumentID], BillCheck{Severity: "error", Code: x.Code, Field: x.Field, Message: x.Message, Fix: x.Fix})
		}
	}
	for _, p := range b.Payments {
		who := PaymentBatchPayee{Kind: "vendor", ID: p.Payee.ContactID.String()}
		if p.Payee.IsPerson() {
			who = PaymentBatchPayee{Kind: "person", ID: p.Payee.PersonID.String()}
			person := Person{Kind: "user", ID: who.ID}
			if err := a.name(ctx, u, &person); err != nil {
				return PaymentBatch{}, err
			}
			who.Name = person.Name
		} else {
			c, err := u.Contacts().Get(ctx, p.Payee.ContactID)
			if err != nil {
				return PaymentBatch{}, err
			}
			who.Name = c.LegalName
			if strings.TrimSpace(c.RemittanceEmail) == "" {
				warnings = append(warnings, BillCheck{Severity: "warning", Code: CodeBatchNoRemittance, Field: "payments",
					Message: c.LegalName + " has no remittance email, so no remittance advice is sent when it is paid",
					Fix:     "Add the supplier's remittance email to its contact"})
			}
		}
		pay := PaymentBatchPayment{Payee: who, Frozen: p.Frozen, Amount: moneyOut(p.Amount), Items: []PaymentBatchItem{}}
		if p.Bank != nil {
			acc := payeeAccountOf(*p.Bank)
			pay.Account = &acc
		}
		for _, l := range p.Lines {
			item := PaymentBatchItem{Kind: l.Kind, DocumentID: l.DocumentID.String(), Number: l.Number, Date: l.Date, Open: moneyOut(l.Open), Checks: []BillCheck{}}
			if c := byItem[l.DocumentID]; c != nil {
				item.Checks = c
			}
			pay.Items = append(pay.Items, item)
		}
		out.Payments = append(out.Payments, pay)
	}
	if status == "released" {
		if out.Release, err = a.releaseOf(ctx, u, doc.ID); err != nil {
			return PaymentBatch{}, err
		}
		// What the release paid, from the payments it posted: its items are no longer open.
		paid, total, all, err := a.releasedPayments(ctx, u, out.Release.Payments)
		if err != nil {
			return PaymentBatch{}, err
		}
		out.Payments, out.Total = paid, total
		if all {
			out.Status = "paid"
		}
	}
	if checks && status != "released" && status != "cancelled" {
		out.Checks = append(batchChecksOf(b.Errors), warnings...)
	}
	return out, nil
}

// batchDoc is the batch with id, refused for any other kind of document.
func batchDoc(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, lock bool) (storage.Document, error) {
	get := u.Documents().Get
	if lock {
		get = u.Documents().Lock
	}
	doc, err := get(ctx, id)
	if err != nil {
		return doc, err
	}
	if doc.Type != storage.DocumentPaymentBatch {
		return doc, problem(paymentbatch.CodeNotBatch, "That record is not a payment batch.", "Use the batch's ID.")
	}
	return doc, nil
}

// batchETag is a batch's ETag: its document's version and status.
func batchETag(doc storage.Document) string { return etag(doc.Version, string(doc.Status)) }

// BatchCodes are the refusals of a batch's commands.
var BatchCodes = []string{paymentbatch.CodeNotBatch, paymentbatch.CodeBadDraft, paymentbatch.CodeAccount, paymentbatch.CodeDate, paymentbatch.CodeMethod,
	paymentbatch.CodeEmpty, paymentbatch.CodeTwice, paymentbatch.CodeItem, paymentbatch.CodeTooMany, paymentbatch.CodeTooLong, paymentbatch.CodeNotDraft}

func registerPaymentBatches(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}

	register(a, authz.ListPaymentBatches, huma.Operation{
		OperationID: "listPaymentBatches", Method: http.MethodGet, Path: "/v1/payment-batches", Tags: []string{"Purchases"},
		Summary: "List payment batches",
		Description: "The payment batches, newest payment date first, a page at a time, narrowed by status (FR-BNK-12): each with its money " +
			"account, payment date, method, payments by payee and total. Read-only; no side effects.",
	}, []string{CodeCursor, CodeInvalid}, func(ctx context.Context, in *batchListInput) (*batchListOutput, error) {
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		afterDate, afterID, err := journalCursor(after)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := batchListOutput{Body: PaymentBatchPage{Items: []PaymentBatch{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			v, err := a.batchView(ctx, u)
			if err != nil {
				return err
			}
			for {
				docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentPaymentBatch, AfterDate: afterDate, AfterID: afterID, Limit: 200})
				if err != nil {
					return err
				}
				for _, doc := range docs {
					afterDate, afterID = doc.Date, &doc.ID
					if len(out.Body.Items) == in.PageSize {
						last := out.Body.Items[len(out.Body.Items)-1]
						out.Body.NextCursor = encodeCursor(last.PaymentDate + "|" + last.ID)
						return nil
					}
					b, err := a.batchOf(ctx, u, v, doc, false)
					if err != nil {
						return err
					}
					if in.Status == "" || b.Status == in.Status {
						out.Body.Items = append(out.Body.Items, b)
					}
				}
				if len(docs) < 200 {
					return nil
				}
			}
		})
		return &out, err
	})

	register(a, authz.GetPaymentBatchSuggestion, huma.Operation{
		OperationID: "getPaymentBatchSuggestion", Method: http.MethodGet, Path: "/v1/payment-batches/suggestion", Tags: []string{"Purchases"},
		Summary: "Suggest a payment batch's items for a date",
		Description: "What a new batch for a payment date starts with (FR-AP-13, FR-BNK-12): every posted SGD bill with something open whose " +
			"planned payment date is on or before it, then every approved expense claim with something owed, oldest first, none in an " +
			"approved batch. A suggestion never adds itself: send the items you keep with createPaymentBatch. Read-only; no side effects.",
	}, []string{CodeInvalid}, func(ctx context.Context, in *batchSuggestInput) (*batchSuggestOutput, error) {
		on, err := time.Parse(time.DateOnly, in.PaymentDate)
		if err != nil {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "query.payment_date", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"}})
		}
		p, _ := authn.From(ctx)
		out := batchSuggestOutput{Body: PaymentBatchSuggestion{PaymentDate: in.PaymentDate, Suggested: []PaymentBatchItemRef{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			s, err := journal.LoadSettings(ctx, u)
			if err != nil {
				return err
			}
			items, err := paymentbatch.Suggested(ctx, u, s, on)
			for _, x := range items[:min(len(items), paymentbatch.MaxItems)] {
				out.Body.Suggested = append(out.Body.Suggested, PaymentBatchItemRef{Kind: x.Kind, DocumentID: x.DocumentID.String()})
			}
			return err
		})
		return &out, err
	})

	register(a, authz.CreatePaymentBatch, huma.Operation{
		OperationID: "createPaymentBatch", Method: http.MethodPost, Path: "/v1/payment-batches", Tags: []string{"Purchases"},
		DefaultStatus: http.StatusCreated, Summary: "Prepare a payment batch",
		Description: "Saves a draft batch (FR-BNK-12; PRD-001 rev 0.154): an SGD bank account, a payment date, FAST or GIRO, and the approved " +
			"bills and expense claims it pays, each whole, for what is open on it. Saving checks its form only; getPaymentBatch shows " +
			"what stops it being asked for: an item not posted or not open, a bill not in SGD or waiting for withholding (FR-AP-19), an " +
			"item in another approved batch, a payee with no approved Singapore account, or one whose change of bank account waits " +
			"(PAYEE-12). Side effects: the batch and its audit event; nothing is paid or posted. Idempotent with Idempotency-Key; " +
			"Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeInvalid, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld},
		append(locks, BatchCodes...)...),
		func(ctx context.Context, in *batchCreateInput) (*batchWriteOutput, error) {
			d, err := batchDraftOf(in.Body)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (PaymentBatch, error) {
				if probs := paymentbatch.Check(d); len(probs) > 0 {
					return PaymentBatch{}, batchProblems(probs)
				}
				payload, err := paymentbatch.Encode(d)
				if err != nil {
					return PaymentBatch{}, err
				}
				doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentPaymentBatch, Payload: payload})
				if err != nil {
					return PaymentBatch{}, err
				}
				if doc, err = u.Documents().Get(ctx, doc.ID); err != nil {
					return PaymentBatch{}, err
				}
				v, err := a.batchView(ctx, u)
				if err != nil {
					return PaymentBatch{}, err
				}
				return a.batchOf(ctx, u, v, doc, true)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &batchWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &batchWriteOutput{Status: w.Status, ETag: etag(w.Body.Version, string(storage.DocumentDraft)), Location: "/v1/payment-batches/" + w.Body.ID,
				Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	register(a, authz.GetPaymentBatch, huma.Operation{
		OperationID: "getPaymentBatch", Method: http.MethodGet, Path: "/v1/payment-batches/{batch_id}", Tags: []string{"Purchases"},
		Summary: "Get a payment batch",
		Description: "Returns a batch with its status, its payments by payee (each payee's account in force by its last four characters, " +
			"and whether a change of it waits), each item with what is open on it now, its total, and before release every check " +
			"that stops it, with its ETag (FR-BNK-12). Read-only; no side effects.",
	}, []string{CodeNotFound, paymentbatch.CodeNotBatch, paymentbatch.CodeBadDraft}, func(ctx context.Context, in *PaymentBatchPath) (*batchReadOutput, error) {
		id, err := parseID("batch_id", in.BatchID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out batchReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := batchDoc(ctx, u, id, false)
			if err != nil {
				return err
			}
			v, err := a.batchView(ctx, u)
			if err != nil {
				return err
			}
			out.Body, err = a.batchOf(ctx, u, v, doc, true)
			out.ETag = batchETag(doc)
			return err
		})
		return &out, err
	})

	register(a, authz.ReplacePaymentBatch, huma.Operation{
		OperationID: "replacePaymentBatch", Method: http.MethodPut, Path: "/v1/payment-batches/{batch_id}", Tags: []string{"Purchases"},
		Summary: "Change a draft payment batch",
		Description: "Saves a draft batch's new content as its next version (FR-BNK-12). Saving voids an approval it waits for: ask again. " +
			"An approved batch never changes: cancel it and prepare another. Side effects: the new version and its audit event. Needs " +
			"If-Match with the batch's ETag. Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeInvalid, CodeMalformed, CodeTooLarge, CodeMediaType}, append(locks, BatchCodes...)...),
		func(ctx context.Context, in *batchReplaceInput) (*batchWriteOutput, error) {
			id, err := parseID("batch_id", in.BatchID)
			if err != nil {
				return nil, err
			}
			d, err := batchDraftOf(in.Body)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (PaymentBatch, error) {
				doc, err := batchDoc(ctx, u, id, true)
				if err != nil {
					return PaymentBatch{}, err
				}
				if err := checkIfMatch(in.IfMatch, batchETag(doc)); err != nil {
					return PaymentBatch{}, err
				}
				if doc.Status != storage.DocumentDraft {
					return PaymentBatch{}, problem(paymentbatch.CodeNotDraft, "The batch is approved, so it never changes.", "Cancel it and prepare another")
				}
				if probs := paymentbatch.Check(d); len(probs) > 0 {
					return PaymentBatch{}, batchProblems(probs)
				}
				payload, err := paymentbatch.Encode(d)
				if err != nil {
					return PaymentBatch{}, err
				}
				if doc, err = u.Documents().Revise(ctx, id, payload); err != nil {
					return PaymentBatch{}, err
				}
				if doc, err = u.Documents().Get(ctx, id); err != nil {
					return PaymentBatch{}, err
				}
				v, err := a.batchView(ctx, u)
				if err != nil {
					return PaymentBatch{}, err
				}
				return a.batchOf(ctx, u, v, doc, true)
			})
			if err != nil {
				return nil, err
			}
			return &batchWriteOutput{Status: w.Status, ETag: etag(w.Body.Version, string(storage.DocumentDraft)), Applied: applied(w), Body: w.Body}, nil
		})
}
