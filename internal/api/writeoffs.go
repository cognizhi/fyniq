package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/team"
	"github.com/cognizhi/fyniq/internal/domain/writeoff"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------- write-offs of invoices (FR-AR-17, SOD-9, FM-1; p1-sales)
//
// What is left of an invoice is written off for one reason (an agreed discount, bank charges, a bad debt, a cross-currency
// difference, a migrated residual) to the reason's account: drafted, and posted only when another person approves it,
// never one who recorded a receipt of the invoice (SOD-9) or prepared the write-off. A posted write-off is voided by a
// reversal, which reopens the invoice; in Founder mode a void needs a second person (FM-1).

// The API's codes for write-offs' approvals.
const (
	CodeWriteOffVoidApproval = "WRITEOFF-10" // in Founder mode a void needs a second person (FM-1)
	CodeOwnWriteOff          = "WRITEOFF-11" // whoever prepared the write-off or recorded a receipt of the invoice never approves it (SOD-9)
)

// WriteOffCodes are the codes the posting rule and the write-off's own checks answer.
var WriteOffCodes = []string{writeoff.CodeInvoice, writeoff.CodeTooMuch, writeoff.CodeReason, writeoff.CodeAccount, writeoff.CodeTooLong,
	ledger.CodePaymentAmount, ledger.CodeSystemRole, ledger.CodeRuleAccount}

// WriteOffApprovalCodes are the codes an approval of a write-off's post or void may answer: SOD-9, and the posting's own.
func WriteOffApprovalCodes() []string {
	return append([]string{CodeOwnWriteOff, writeoff.CodeNotWriteOff, writeoff.CodeBadDraft, writeoff.CodeNotDraft, writeoff.CodeNotPosted,
		writeoff.CodeVoidDate}, WriteOffCodes...)
}

// WriteOffDraft is a write-off as entered.
type WriteOffDraft struct {
	InvoiceID string `json:"invoice_id" format:"uuid" doc:"The invoice it writes off: an issued invoice with something open." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f30"`
	Date      string `json:"date,omitempty" format:"date" doc:"The day it is written off; the month it posts in." example:"2026-10-31"`
	Amount    Money  `json:"amount" doc:"What is written off, in the invoice's currency: at most what is open of it (WRITEOFF-04); all of it clears the invoice."`
	Reason    string `json:"reason" enum:"discount,charges,bad_debt,cross_currency,migrated" doc:"Why: discount, an agreed discount, to 4095 Discounts & refunds given unless another account is named; charges, bank or provider charges, and cross_currency, an unexplained cross-currency difference with the bank's advice, to 7030 FX & rounding unless named; bad_debt and migrated name their account (provisional until the chart-of-accounts review). A bad debt is tagged for the GST bad-debt relief review (FR-AR-11, P2)." example:"bad_debt"`
	AccountID string `json:"account_id,omitempty" format:"uuid" doc:"The account it writes off to; left out, the reason's. Required for bad_debt and migrated (WRITEOFF-06)." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d80"`
	Note      string `json:"note" minLength:"1" maxLength:"1000" doc:"Why it is written off, in plain words: the approver reads it, and the audit trail keeps it." example:"The customer went into liquidation; the liquidator's notice is attached to the invoice"`
}

// WriteOffInvoice is the invoice a write-off writes off.
type WriteOffInvoice struct {
	ID     string `json:"id" format:"uuid" doc:"The invoice." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f30"`
	Number string `json:"number,omitempty" doc:"Its number." example:"INV-0042"`
	Open   *Money `json:"open,omitempty" doc:"A draft write-off: what is open of the invoice now, in its currency."`
}

// WriteOffStates are a write-off's approval and ledger states.
type WriteOffStates struct {
	Approval string `json:"approval" enum:"draft,awaiting_approval,approved,rejected" doc:"draft until it is asked for; awaiting_approval while a request to post it waits; approved once posted; rejected when the latest request was rejected and it is still a draft." example:"draft"`
	Ledger   string `json:"ledger" enum:"not_posted,posted,voided" doc:"not_posted, posted, or voided by a reversal." example:"not_posted"`
}

// WriteOff is a write-off of an invoice.
type WriteOff struct {
	ID            string              `json:"id" format:"uuid" doc:"The write-off's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f70"`
	Number        string              `json:"number,omitempty" doc:"Its number from the write-off sequence, given when it posts." example:"WO-0001"`
	Status        string              `json:"status" enum:"draft,posted,reversed" doc:"Draft, posted (on approval), or reversed by a void." example:"draft"`
	Version       int                 `json:"version" doc:"The version of its content; with the status it makes the ETag." example:"1"`
	Invoice       WriteOffInvoice     `json:"invoice" doc:"The invoice it writes off."`
	Customer      *InvoiceCustomer    `json:"customer,omitempty" doc:"The invoice's customer."`
	Date          string              `json:"date,omitempty" format:"date" doc:"The day it is written off." example:"2026-10-31"`
	Amount        Money               `json:"amount" doc:"What is written off, in the invoice's currency."`
	Reason        string              `json:"reason" doc:"Why, as entered." example:"bad_debt"`
	Account       *BillPaymentAccount `json:"account,omitempty" doc:"The account it writes off to: as named, or the reason's."`
	Note          string              `json:"note" doc:"Why, in plain words." example:"The customer went into liquidation"`
	ReliefReview  bool                `json:"relief_review" doc:"true for a bad debt: tagged for the GST bad-debt relief review (FR-AR-11, P2)." example:"true"`
	Carrying      *Money              `json:"carrying,omitempty" doc:"The invoice's carrying amount it clears, in the base currency: as posted, or as posting would post it; absent while it cannot post."`
	States        WriteOffStates      `json:"states" doc:"Its approval and ledger states."`
	Checks        []InvoiceCheck      `json:"checks" nullable:"false" doc:"getWriteOff only: what stops a draft posting (✖)."`
	LedgerImpact  []EntryLine         `json:"ledger_impact" nullable:"false" doc:"getWriteOff only: the lines posting it writes (a draft), or wrote: debits positive."`
	PreparedBy    *Person             `json:"prepared_by,omitempty" doc:"Who saved the current version."`
	PostedBy      *Person             `json:"posted_by,omitempty" doc:"Who approved it, as whom it posted."`
	PostedAt      *time.Time          `json:"posted_at,omitempty" doc:"When it posted." example:"2026-10-31T09:30:00Z"`
	ApprovalAsked string              `json:"approval_asked,omitempty" format:"uuid" doc:"getWriteOff only: the pending request to post or void it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4dd4"`
}

// WriteOffPage is a page of write-offs, newest date first.
type WriteOffPage struct {
	Items      []WriteOff `json:"items" nullable:"false"`
	NextCursor string     `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjYtMTAtMzF8MDE5MiJ9"`
}

// WriteOffPosting is a write-off after a void, with the entry it made.
type WriteOffPosting struct {
	WriteOff WriteOff `json:"write_off" doc:"The write-off after the command."`
	Entry    Entry    `json:"entry" doc:"The reversing entry the void posted."`
}

// WriteOffPath names a write-off in the path.
type WriteOffPath struct {
	WriteOffID string `path:"write_off_id" format:"uuid" doc:"The write-off's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f70"`
}

type writeOffListInput struct {
	PageQuery
	Status string `query:"status" enum:"draft,posted,reversed" doc:"Only write-offs with this status." example:"posted"`
	From   string `query:"from" format:"date" doc:"Only write-offs dated on or after this day." example:"2026-10-01"`
	To     string `query:"to" format:"date" doc:"Only write-offs dated on or before this day." example:"2026-10-31"`
	Q      string `query:"q" maxLength:"100" doc:"Only write-offs whose number contains this text, ignoring case." example:"WO-0001"`
}

type writeOffListOutput struct{ Body WriteOffPage }

type createWriteOffInput struct {
	KeyHeader
	PreferHeader
	Body WriteOffDraft
}

type replaceWriteOffInput struct {
	WriteOffPath
	IfMatchHeader
	PreferHeader
	Body WriteOffDraft
}

type writeOffOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The write-off's version and status, for If-Match."`
	Location string `header:"Location" doc:"The write-off's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     WriteOff
}

type writeOffReadOutput struct {
	ETag string `header:"ETag" doc:"The write-off's version and status, for If-Match."`
	Body WriteOff
}

type writeOffCommandInput struct {
	WriteOffPath
	KeyHeader
	IfMatchHeader
	PreferHeader
}

type writeOffVoidInput struct {
	WriteOffPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body Reversal
}

type writeOffPostingOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The write-off's version and status, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved, and the entry shows what the void would post (FR-INT-14)."`
	Body     WriteOffPosting
}

type writeOffRequestOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     Approval
}

func writeOffETag(w WriteOff) string { return etag(w.Version, w.Status) }

// writeOffDraftOf reads a write-off from the request, with every invalid field at once.
func writeOffDraftOf(in WriteOffDraft) (writeoff.Draft, error) {
	var d writeoff.Draft
	var fields []FieldProblem
	id, err := uuid.Parse(in.InvoiceID)
	if err != nil {
		fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.invoice_id", Message: "is not an ID", Fix: "Send the invoice's ID"})
	}
	d.InvoiceID, d.Reason, d.Note = id, in.Reason, in.Note
	if in.AccountID != "" {
		if d.AccountID, err = uuid.Parse(in.AccountID); err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.account_id", Message: "is not an ID", Fix: "Send the account's ID, or leave it out"})
		}
	}
	if in.Date != "" {
		if d.Date, err = time.Parse(time.DateOnly, in.Date); err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.date", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"})
		}
	}
	m, bad := moneyIn(in.Amount, "body.amount")
	if bad != nil {
		fields = append(fields, *bad)
	}
	d.Amount = m
	if len(fields) > 0 {
		return d, invalid(fields)
	}
	return d, nil
}

// writeOffProblems turns the posting rule's problems into the API's, naming the write-off.
func writeOffProblems(err error) error {
	var lp ledger.Problems
	if !errors.As(err, &lp) || len(lp) == 0 {
		return err
	}
	fields := make([]FieldProblem, 0, len(lp))
	for _, x := range lp {
		fields = append(fields, FieldProblem{Code: x.Code, Field: x.Field, Message: x.Message, Fix: x.Fix})
	}
	return problem(lp[0].Code, fmt.Sprintf("The write-off cannot post: %d problem(s), each listed in errors.", len(lp)),
		"Correct the write-off and ask again.", fields...)
}

// writeOffView is what showing write-offs reads once per request.
type writeOffView struct {
	base     money.Currency
	accounts map[uuid.UUID]storage.Account
	asked    map[string]string
	rejected map[string]int
}

func (a *API) writeOffView(ctx context.Context, u storage.UnitOfWork) (writeOffView, error) {
	o, err := u.Organisation(ctx)
	if err != nil {
		return writeOffView{}, err
	}
	v := writeOffView{accounts: map[uuid.UUID]storage.Account{}, asked: map[string]string{}, rejected: map[string]int{}}
	if v.base, err = money.Lookup(o.BaseCurrency); err != nil {
		return writeOffView{}, err
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return writeOffView{}, err
	}
	for _, x := range accounts {
		v.accounts[x.ID] = x
	}
	requests, err := u.Approvals().List(ctx, "")
	if err != nil {
		return writeOffView{}, err
	}
	now := a.deps.Clock.Now()
	for _, r := range requests {
		if r.ObjectType != approval.ObjectWriteOff {
			continue
		}
		switch approval.StatusAt(requestOf(r), now) {
		case approval.Pending:
			v.asked[r.ObjectKey] = r.ID.String()
		case approval.Rejected:
			if r.Operation == approval.OpWriteOffPost {
				var c struct {
					Version string `json:"version"`
				}
				if json.Unmarshal(r.Change, &c) == nil {
					if n, err := strconv.Atoi(c.Version); err == nil && n >= v.rejected[r.ObjectKey] {
						v.rejected[r.ObjectKey] = n
					}
				}
			}
		}
	}
	return v, nil
}

// writeOffOf is a write-off as the API shows it. withChecks fills its checks and ledger impact (getWriteOff).
func (a *API) writeOffOf(ctx context.Context, u storage.UnitOfWork, v writeOffView, doc storage.Document, withChecks bool) (WriteOff, error) {
	d, err := writeoff.Read(doc)
	if err != nil {
		return WriteOff{}, err
	}
	out := WriteOff{ID: doc.ID.String(), Number: doc.Number, Status: string(doc.Status), Version: doc.Version, Invoice: WriteOffInvoice{ID: d.InvoiceID.String()},
		Date: dayText(d.Date), Amount: moneyOut(d.Amount), Reason: d.Reason, Note: d.Note, ReliefReview: d.Reason == writeoff.ReasonBadDebt,
		Checks: []InvoiceCheck{}, LedgerImpact: []EntryLine{}, PreparedBy: personOf(doc.SavedBy), PostedBy: personOf(doc.PostedBy), PostedAt: utc(doc.PostedAt),
		States: WriteOffStates{Approval: "draft", Ledger: "not_posted"}}
	if inv, err := u.Documents().Get(ctx, d.InvoiceID); err == nil {
		out.Invoice.Number = inv.Number
	}
	account := func(id uuid.UUID) {
		if id == uuid.Nil {
			return
		}
		out.Account = &BillPaymentAccount{ID: id.String()}
		if acc, ok := v.accounts[id]; ok {
			out.Account.Code, out.Account.Name = acc.Code, acc.Name
		}
	}
	switch doc.Status {
	case storage.DocumentPosted:
		out.States = WriteOffStates{Approval: "approved", Ledger: "posted"}
	case storage.DocumentReversed:
		out.States = WriteOffStates{Approval: "approved", Ledger: "voided"}
	default:
		if out.ApprovalAsked = v.asked[out.ID]; out.ApprovalAsked != "" {
			out.States.Approval = "awaiting_approval"
		} else if n, ok := v.rejected[out.ID]; ok && n == doc.Version {
			out.States.Approval = "rejected"
		}
	}
	if doc.Status == storage.DocumentDraft {
		c, err := journal.CheckWriteOff(ctx, u, doc)
		if err != nil {
			return WriteOff{}, err
		}
		if c.Prepared.Customer.LegalName != "" {
			out.Customer = &InvoiceCustomer{ID: c.Prepared.Customer.ID.String(), Name: c.Prepared.Customer.LegalName}
		}
		if c.Prepared.Invoice.Open.Currency().Code != "" {
			o := moneyOut(c.Prepared.Invoice.Open)
			out.Invoice.Open = &o
		}
		account(c.Prepared.WriteOff.Account)
		if withChecks {
			for _, x := range c.Problems {
				out.Checks = append(out.Checks, InvoiceCheck{Severity: "error", Code: x.Code, Field: x.Field, Message: x.Message, Fix: x.Fix})
			}
		}
		if len(c.Problems) == 0 {
			cr := moneyOut(c.Settlement.Base)
			out.Carrying = &cr
			if withChecks {
				out.LedgerImpact = impactOf(c.Entry)
			}
		}
		return out, a.name(ctx, u, out.PreparedBy, out.PostedBy)
	}
	if withChecks {
		out.ApprovalAsked = v.asked[out.ID]
	}
	e, err := u.Ledger().ByIdentity(ctx, ledger.WriteOffIdentity(doc.ID, doc.Version, ledger.RuleWriteOff))
	if err != nil {
		return WriteOff{}, err
	}
	for _, l := range entryOf(e).Lines {
		if withChecks {
			out.LedgerImpact = append(out.LedgerImpact, l)
		}
		id, _ := uuid.Parse(l.AccountID)
		if string(v.accounts[id].Role) != ledger.RoleAccountsReceivable {
			account(id)
			c := l.BaseAmount
			out.Carrying = &c
		}
	}
	if inv, err := u.Documents().Get(ctx, d.InvoiceID); err == nil {
		if id, err := invoiceCustomer(inv, v.base); err == nil {
			out.Customer = &InvoiceCustomer{ID: id.String()}
			if c, err := u.Contacts().Get(ctx, id); err == nil {
				out.Customer.Name = c.LegalName
			}
		}
	}
	return out, a.name(ctx, u, out.PreparedBy, out.PostedBy)
}

// gateWriteOffVoid refuses the void of a posted write-off in Founder mode: it needs a second person (FM-1).
func gateWriteOffVoid(ctx context.Context, u storage.UnitOfWork) error {
	mode, _, _, err := teamModeOf(ctx, u)
	if err != nil {
		return err
	}
	if mode == team.Founder {
		return problem(CodeWriteOffVoidApproval, "In Founder mode, voiding a posted write-off needs a second person (FM-1).", "Ask for another person's approval to void it")
	}
	return nil
}

// writeOffInvoiceName names a write-off's invoice and its customer; writeOffName the write-off itself.
func writeOffInvoiceName(w WriteOff) string {
	name := cmpText(w.Invoice.Number, "an invoice")
	if w.Customer != nil && w.Customer.Name != "" {
		name += " to " + w.Customer.Name
	}
	return name
}

func writeOffName(w WriteOff) string {
	return cmpText(w.Number, "the write-off") + " of " + writeOffInvoiceName(w)
}

// writeOffLocked is a write-off locked in u and shown, checked against If-Match.
func (a *API) writeOffLocked(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, ifMatch string) (storage.Document, WriteOff, writeOffView, error) {
	doc, err := u.Documents().Lock(ctx, id)
	if err != nil {
		return storage.Document{}, WriteOff{}, writeOffView{}, err
	}
	v, err := a.writeOffView(ctx, u)
	if err != nil {
		return storage.Document{}, WriteOff{}, writeOffView{}, err
	}
	w, err := a.writeOffOf(ctx, u, v, doc, false)
	if err != nil {
		return storage.Document{}, WriteOff{}, writeOffView{}, err
	}
	return doc, w, v, checkIfMatch(ifMatch, writeOffETag(w))
}

func registerWriteOffs(a *API) {
	formCodes := []string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, money.CodeNotDecimal, money.CodePrecision, money.CodeRange,
		money.CodeCurrency, writeoff.CodeTooLong, storage.CodeRetry, storage.CodeLockTimeout}
	readCodes := []string{CodeNotFound, writeoff.CodeNotWriteOff, writeoff.CodeBadDraft}

	register(a, authz.ListWriteOffs, huma.Operation{
		OperationID: "listWriteOffs", Method: http.MethodGet, Path: "/v1/write-offs", Tags: []string{"Sales"},
		Summary: "List the write-offs",
		Description: "The write-offs of invoices, newest date first (a draft without a date last), a page at a time, narrowed by status, " +
			"dates and text in the number (FR-AR-17). Read-only; no side effects.",
	}, append([]string{CodeCursor, CodeInvalid}, readCodes...), func(ctx context.Context, in *writeOffListInput) (*writeOffListOutput, error) {
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		afterDate, afterID, err := journalCursor(after)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := writeOffListOutput{Body: WriteOffPage{Items: []WriteOff{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentWriteOff, Status: storage.DocumentStatus(in.Status),
				From: in.From, To: in.To, Search: strings.TrimSpace(in.Q), AfterDate: afterDate, AfterID: afterID, Limit: in.PageSize + 1})
			if err != nil {
				return err
			}
			if len(docs) > in.PageSize {
				docs = docs[:in.PageSize]
				last := docs[len(docs)-1]
				out.Body.NextCursor = encodeCursor(last.Date + "|" + last.ID.String())
			}
			v, err := a.writeOffView(ctx, u)
			if err != nil {
				return err
			}
			for _, d := range docs {
				x, err := a.writeOffOf(ctx, u, v, d, false)
				if err != nil {
					return err
				}
				out.Body.Items = append(out.Body.Items, x)
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.GetWriteOff, huma.Operation{
		OperationID: "getWriteOff", Method: http.MethodGet, Path: "/v1/write-offs/{write_off_id}", Tags: []string{"Sales"},
		Summary: "Get a write-off",
		Description: "Returns a write-off with its ETag: the invoice and what is open of it, the reason, the account it writes off to, why, " +
			"the carrying amount it clears, its approval and ledger states, what stops a draft posting (✖), the lines posting it " +
			"writes or wrote (ledger_impact), and the pending request to post or void it. Read-only; no side effects.",
	}, readCodes, func(ctx context.Context, in *WriteOffPath) (*writeOffReadOutput, error) {
		id, err := parseID("write_off_id", in.WriteOffID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out WriteOff
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Get(ctx, id)
			if err != nil {
				return err
			}
			v, err := a.writeOffView(ctx, u)
			if err != nil {
				return err
			}
			out, err = a.writeOffOf(ctx, u, v, doc, true)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &writeOffReadOutput{ETag: writeOffETag(out), Body: out}, nil
	})

	register(a, authz.CreateWriteOff, huma.Operation{
		OperationID: "createWriteOff", Method: http.MethodPost, Path: "/v1/write-offs", Tags: []string{"Sales"}, DefaultStatus: http.StatusCreated,
		Summary: "Save a draft write-off",
		Description: "Saves the write-off of what is left of an invoice as a draft, at version 1 (FR-AR-17): the invoice, the amount in its " +
			"currency, the reason, the account (left out, the reason's) and why. Nothing posts: requestWriteOffPost asks another " +
			"person to approve it. getWriteOff shows what posting it would do. Side effects: a draft document and its audit event. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld}, formCodes...),
		func(ctx context.Context, in *createWriteOffInput) (*writeOffOutput, error) {
			p, _ := authn.From(ctx)
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (WriteOff, error) {
				d, err := writeOffDraftOf(in.Body)
				if err != nil {
					return WriteOff{}, err
				}
				doc, err := a.writeOffs.Create(ctx, p.Organisation, d)
				if err != nil {
					return WriteOff{}, err
				}
				v, err := a.writeOffView(ctx, u)
				if err != nil {
					return WriteOff{}, err
				}
				return a.writeOffOf(ctx, u, v, doc, false)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &writeOffOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &writeOffOutput{Status: w.Status, ETag: writeOffETag(w.Body), Location: "/v1/write-offs/" + w.Body.ID, Replayed: replayedHeader(w.Replayed),
				Body: w.Body}, nil
		})

	register(a, authz.ReplaceWriteOff, huma.Operation{
		OperationID: "replaceWriteOff", Method: http.MethodPut, Path: "/v1/write-offs/{write_off_id}", Tags: []string{"Sales"},
		Summary: "Change a draft write-off",
		Description: "Saves the draft's new content as its next version, recording who saved it; a pending request to post it is voided by " +
			"the change. Only a draft changes. Side effects: the new version and its audit event. Needs If-Match with the draft's ETag. " +
			"Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, writeoff.CodeNotWriteOff, writeoff.CodeNotDraft, writeoff.CodeBadDraft}, formCodes...),
		func(ctx context.Context, in *replaceWriteOffInput) (*writeOffOutput, error) {
			id, err := parseID("write_off_id", in.WriteOffID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			p, _ := authn.From(ctx)
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (WriteOff, error) {
				_, _, v, err := a.writeOffLocked(ctx, u, id, in.IfMatch)
				if err != nil {
					return WriteOff{}, err
				}
				d, err := writeOffDraftOf(in.Body)
				if err != nil {
					return WriteOff{}, err
				}
				doc, err := a.writeOffs.Revise(ctx, p.Organisation, id, d)
				if err != nil {
					return WriteOff{}, err
				}
				return a.writeOffOf(ctx, u, v, doc, false)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &writeOffOutput{Status: http.StatusOK, Applied: applied(w), Body: w.Body}, nil
			}
			return &writeOffOutput{Status: http.StatusOK, ETag: writeOffETag(w.Body), Body: w.Body}, nil
		})

	requestCodes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		CodeSessionRequired, CodeStepUpRequired, approval.CodePending, storage.CodeApproval, writeoff.CodeNotWriteOff, writeoff.CodeNotDraft,
		writeoff.CodeNotPosted, writeoff.CodeBadDraft, writeoff.CodeVoidDate, storage.CodeRetry, storage.CodeLockTimeout, CodeMalformed, CodeTooLarge,
		CodeMediaType, CodeInvalid}, append(WriteOffCodes, LedgerCodes...)...)

	register(a, authz.RequestWriteOffPost, huma.Operation{
		OperationID: "requestWriteOffPost", Method: http.MethodPost, Path: "/v1/write-offs/{write_off_id}/post-approval", Tags: []string{"Sales"},
		Summary: "Ask for approval to post a write-off", DefaultStatus: http.StatusAccepted,
		Description: "Asks another person to approve a draft write-off at the version read (FR-AR-17, FR-APR-01): every write-off posts only " +
			"this way, in any team mode. The draft must pass the posting rules. The answer is 202 with an approval request, which a " +
			"person who holds A on \"Invoices, credit notes\" approves, stepped up: never the one who asked, prepared the write-off, or " +
			"recorded a receipt of the invoice (WRITEOFF-11, SOD-9). On the approval it posts write_off@1 as the approver: Cr 1100 " +
			"with the amount at the invoice's stored rate, Dr the reason's account with the carrying amount, and an allocation that " +
			"settles the invoice. The request holds the write-off's version, status and carrying amount, so a change voids it. " +
			"Asking needs a person signed in who passed step-up, and If-Match with the write-off's ETag. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview checks it and asks nothing.",
	}, requestCodes, func(ctx context.Context, in *writeOffCommandInput) (*writeOffRequestOutput, error) {
		id, err := parseID("write_off_id", in.WriteOffID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
			doc, wo, _, err := a.writeOffLocked(ctx, u, id, in.IfMatch)
			if err != nil {
				return Approval{}, err
			}
			if doc.Status != storage.DocumentDraft {
				return Approval{}, problem(writeoff.CodeNotDraft, fmt.Sprintf("The write-off is %s.", doc.Status), "Ask for approval of a draft write-off")
			}
			c, err := journal.CheckWriteOff(ctx, u, doc)
			if err != nil {
				return Approval{}, err
			}
			if len(c.Problems) > 0 {
				return Approval{}, writeOffProblems(c.Problems)
			}
			carrying := c.Settlement.Base.String()
			return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectWriteOff, ObjectKey: doc.ID.String(), Operation: approval.OpWriteOffPost,
				Summary: fmt.Sprintf("Write off %s %s of %s (%s): %s", wo.Amount.Amount, wo.Amount.Currency, writeOffInvoiceName(wo),
					strings.ReplaceAll(wo.Reason, "_", " "), wo.Note),
				Execute: map[string]any{"write_off_id": doc.ID.String(), "version": fmt.Sprint(doc.Version)},
				Diff: []ApprovalDiff{{Field: "status", Before: "draft", After: "posted"}, {Field: "reason", After: wo.Reason},
					{Field: "carrying", After: carrying + " " + c.Settlement.Base.Currency().Code}}})
		})
		if err != nil {
			return nil, err
		}
		return &writeOffRequestOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	voidCodes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		team.CodeAction, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeWriteOffVoidApproval, writeoff.CodeNotPosted,
		writeoff.CodeVoidDate, storage.CodeAllocation, storage.CodeDuplicate}, append(postingCodes(), readCodes...)...)

	register(a, authz.VoidWriteOff, huma.Operation{
		OperationID: "voidWriteOff", Method: http.MethodPost, Path: "/v1/write-offs/{write_off_id}/void", Tags: []string{"Sales"},
		Summary: "Void a write-off",
		Description: "Voids a posted write-off with a reversing entry on the date given, with a reason, and a reversing allocation, which " +
			"reopens the invoice (FR-GL-12, FR-AR-18); posted history never changes. In Founder mode voiding needs a second person " +
			"(FM-1): it is refused (WRITEOFF-10), and requestWriteOffVoid asks for the approval. Side effects: the reversing entry and " +
			"allocation, a new ledger revision, balances, audit events with the reason. Needs If-Match with the write-off's ETag. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview voids nothing.",
	}, voidCodes, func(ctx context.Context, in *writeOffVoidInput) (*writeOffPostingOutput, error) {
		id, err := parseID("write_off_id", in.WriteOffID)
		if err != nil {
			return nil, err
		}
		date, err := time.Parse(time.DateOnly, in.Body.Date)
		if err != nil {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.date", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"}})
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		p, _ := authn.From(ctx)
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (WriteOffPosting, error) {
			_, _, v, err := a.writeOffLocked(ctx, u, id, in.IfMatch)
			if err != nil {
				return WriteOffPosting{}, err
			}
			if err := checkConflicts(ctx, u, p, "Posting"); err != nil {
				return WriteOffPosting{}, err
			}
			if err := gateWriteOffVoid(ctx, u); err != nil {
				return WriteOffPosting{}, err
			}
			r, err := a.writeOffs.Void(ctx, p.Organisation, id, date, strings.TrimSpace(in.Body.Reason))
			if err != nil {
				return WriteOffPosting{}, err
			}
			out, err := a.writeOffOf(ctx, u, v, r.WriteOff, false)
			return WriteOffPosting{WriteOff: out, Entry: entryOf(r.Entry)}, err
		})
		if err != nil {
			return nil, err
		}
		if w.Previewed {
			return &writeOffPostingOutput{Status: http.StatusOK, Applied: applied(w), Body: w.Body}, nil
		}
		return &writeOffPostingOutput{Status: http.StatusOK, ETag: writeOffETag(w.Body.WriteOff), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	register(a, authz.RequestWriteOffVoid, huma.Operation{
		OperationID: "requestWriteOffVoid", Method: http.MethodPost, Path: "/v1/write-offs/{write_off_id}/void-approval", Tags: []string{"Sales"},
		Summary: "Ask for approval to void a write-off", DefaultStatus: http.StatusAccepted,
		Description: "Asks for another person's approval to void a posted write-off with a reversing entry on the date given, with a reason " +
			"(FR-GL-12, FM-1): what voidWriteOff asks for in Founder mode (WRITEOFF-10). The answer is 202 with an approval request, " +
			"which a person who holds A on \"Invoices, credit notes\" and did not ask approves, stepped up; on the approval the void " +
			"posts and the invoice reopens. Asking needs a person signed in who passed step-up, and If-Match with the write-off's " +
			"ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.",
	}, requestCodes, func(ctx context.Context, in *writeOffVoidInput) (*writeOffRequestOutput, error) {
		id, err := parseID("write_off_id", in.WriteOffID)
		if err != nil {
			return nil, err
		}
		if _, err := time.Parse(time.DateOnly, in.Body.Date); err != nil {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.date", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"}})
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		reason := strings.TrimSpace(in.Body.Reason)
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
			doc, wo, _, err := a.writeOffLocked(ctx, u, id, in.IfMatch)
			if err != nil {
				return Approval{}, err
			}
			if doc.Status != storage.DocumentPosted {
				return Approval{}, problem(writeoff.CodeNotPosted, fmt.Sprintf("The write-off is %s.", doc.Status), "Ask for approval to void a posted write-off")
			}
			if wo.Date != "" && in.Body.Date < wo.Date {
				return Approval{}, problem(writeoff.CodeVoidDate, fmt.Sprintf("The void is dated %s, before the write-off's %s.", in.Body.Date, wo.Date),
					"Date the void on or after the write-off")
			}
			return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectWriteOff, ObjectKey: doc.ID.String(), Operation: approval.OpWriteOffVoid,
				Summary: fmt.Sprintf("Void %s on %s: %s", writeOffName(wo), in.Body.Date, reason),
				Execute: map[string]any{"write_off_id": doc.ID.String(), "date": in.Body.Date, "reason": reason},
				Diff:    []ApprovalDiff{{Field: "status", Before: "posted", After: "reversed"}, {Field: "void_date", After: in.Body.Date}}})
		})
		if err != nil {
			return nil, err
		}
		return &writeOffRequestOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})
}

// invoiceCustomer is the customer an invoice document is to.
func invoiceCustomer(doc storage.Document, base money.Currency) (uuid.UUID, error) {
	d, err := invoice.Read(doc, base)
	return d.CustomerID, err
}

// writeOffApprover refuses a signer who prepared the write-off, or recorded a receipt of its invoice that still settles
// it (SOD-9: a write-off never approved by the person who recorded the receipt).
func (a *API) writeOffApprover(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, signer uuid.UUID) error {
	id, err := uuid.Parse(r.ObjectKey)
	if err != nil {
		return err
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return err
	}
	me := (storage.Actor{Type: "user", ID: signer.String()}).String()
	if doc.SavedBy == me {
		return problem(CodeOwnWriteOff, "You prepared this write-off, so another person approves it (SOD-9).", "Ask another person who holds the approval right")
	}
	d, err := writeoff.Read(doc)
	if err != nil {
		return err
	}
	allocs, err := u.Allocations().OfDocument(ctx, d.InvoiceID)
	if err != nil {
		return err
	}
	for _, x := range allocs {
		src, err := u.Documents().Get(ctx, x.SourceID)
		if err != nil {
			return err
		}
		if src.Type == storage.DocumentCustomerReceipt && src.PostedBy == me {
			return problem(CodeOwnWriteOff, fmt.Sprintf("You recorded receipt %s of this invoice, so another person approves its write-off (SOD-9).", src.Number),
				"Ask another person who holds the approval right")
		}
	}
	return nil
}
