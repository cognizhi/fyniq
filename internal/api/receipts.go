package api

import (
	"context"
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
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/receipt"
	"github.com/cognizhi/fyniq/internal/domain/team"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// --------------------------------------------- customer receipts recorded by hand (FR-AR-18, ENG-41, FM-1; p1-sales)
//
// Until bank import matches receipts, money a customer paid is recorded by hand (the owner's decision of 24 Oct 2026):
// the money account it came into, the date, and what it settled of each of the customer's invoices, in their currency.
// Recording it posts customer_receipt@1 at once, as the money has come in already: Cr 1100 at each invoice's stored
// rate, Dr the money account, and the realised FX to 7030 (ENG-41). Voiding it reverses the entry and its allocations,
// which reopens the invoices; in Founder mode a void needs a second person (FM-1).

// CodeReceiptVoidApproval is the API's code for a void that needs a second person in Founder mode (FM-1).
const CodeReceiptVoidApproval = "RECEIPT-10"

// ReceiptCodes are the codes the posting rule and the receipt's own checks answer.
var ReceiptCodes = []string{receipt.CodeCustomer, receipt.CodeInvoice, receipt.CodeOverpaid, receipt.CodeTooLong, receipt.CodeTooMany,
	ledger.CodeNotMoneyAccount, ledger.CodePaymentAmount, ledger.CodePaidAmount, ledger.CodeSystemRole, ledger.CodeLineCurrency}

// ReceiptApprovalCodes are the codes an approval of a receipt's void may answer.
func ReceiptApprovalCodes() []string {
	return []string{receipt.CodeNotReceipt, receipt.CodeNotPosted, receipt.CodeVoidDate, receipt.CodeBadDraft}
}

// CustomerReceiptPart is what a receipt settles of one invoice, as entered.
type CustomerReceiptPart struct {
	InvoiceID string `json:"invoice_id" format:"uuid" doc:"The invoice: one of the customer's issued invoices, in the receipt's currency." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f30"`
	Amount    Money  `json:"amount" doc:"What the receipt settles of it, in the invoice's currency: at most what is open of it (RECEIPT-05)."`
}

// CustomerReceiptDraft is a receipt as recorded.
type CustomerReceiptDraft struct {
	CustomerID     string                `json:"customer_id,omitempty" format:"uuid" doc:"The customer who paid; left out, the first invoice's customer. Every invoice is the customer's." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f20"`
	Date           string                `json:"date" format:"date" doc:"The day the money came into the account." example:"2026-10-28"`
	MoneyAccountID string                `json:"money_account_id" format:"uuid" doc:"The bank, card or wallet account it came into: an active money account in the invoices' currency or in the base currency." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d10"`
	Currency       string                `json:"currency" pattern:"^[A-Z]{3}$" doc:"The invoices' currency." example:"SGD"`
	FXRate         string                `json:"fx_rate,omitempty" doc:"For invoices in another currency received into an account in that currency: base currency per one unit, as on the bank's advice. Left out, the cascade's rate for the date (ENG-40)." example:"1.36"`
	Received       *Money                `json:"received,omitempty" doc:"For invoices in another currency received into a base-currency account: what the account received, in the base currency, as the statement shows it; the receipt settles at the rate it implies. Required then, and only then (LEDGER-27)."`
	Reference      string                `json:"reference,omitempty" maxLength:"140" doc:"The bank's reference for it." example:"FAST 7731"`
	Invoices       []CustomerReceiptPart `json:"invoices" maxItems:"100" doc:"What it settles of each invoice."`
	Unapplied      *Money                `json:"unapplied,omitempty" doc:"What it received beyond the invoices, in their currency: kept as a credit with the customer (FR-AR-18). Left out, nothing."`
	Notes          string                `json:"notes,omitempty" maxLength:"2000" doc:"Notes kept with it." example:"Paid by FAST from the customer's DBS account"`
}

// InvoiceSettled is what a receipt settled of one invoice: its allocation (FR-AR-18).
type InvoiceSettled struct {
	InvoiceID     string `json:"invoice_id" format:"uuid" doc:"The invoice." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f30"`
	InvoiceNumber string `json:"invoice_number,omitempty" doc:"The invoice's number." example:"INV-0042"`
	Amount        Money  `json:"amount" doc:"What it settled, in the invoice's currency."`
	Base          Money  `json:"base" doc:"The invoice's carrying amount it cleared, in the base currency: the amount at the invoice's stored rate, and for the part that settles what is left of an invoice, what was left (ENG-14)."`
	RealisedFX    Money  `json:"realised_fx" doc:"The realised gain (positive) or loss (negative) in the base currency: the amount at the receipt's rate against the invoice's stored rate, with no tolerance (ENG-41)."`
}

// CustomerReceipt is money a customer paid, recorded by hand.
type CustomerReceipt struct {
	ID            string              `json:"id" format:"uuid" doc:"The receipt's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f60"`
	Number        string              `json:"number,omitempty" doc:"Its number from the receipt sequence (FR-ORG-04)." example:"RC-0003"`
	Status        string              `json:"status" enum:"posted,reversed" doc:"posted once recorded; reversed by a void." example:"posted"`
	Version       int                 `json:"version" doc:"The version of its content; with the status it makes the ETag." example:"1"`
	Customer      *InvoiceCustomer    `json:"customer,omitempty" doc:"The customer who paid."`
	Date          string              `json:"date,omitempty" format:"date" doc:"The day the money came in." example:"2026-10-28"`
	MoneyAccount  *BillPaymentAccount `json:"money_account,omitempty" doc:"The account it came into."`
	Currency      string              `json:"currency" doc:"The invoices' currency." example:"SGD"`
	FXRate        string              `json:"fx_rate,omitempty" doc:"The rate typed, if one was." example:"1.36"`
	Received      *Money              `json:"received,omitempty" doc:"What a base-currency account received for invoices in another currency."`
	Reference     string              `json:"reference,omitempty" doc:"The bank's reference." example:"FAST 7731"`
	Total         Money               `json:"total" doc:"What it received in the invoices' currency: every part and the unapplied amount."`
	Unapplied     Money               `json:"unapplied" doc:"What it received beyond the invoices: a credit with the customer."`
	Invoices      []InvoiceSettled    `json:"invoices" nullable:"false" doc:"What it settled of each invoice, as recorded."`
	Notes         string              `json:"notes,omitempty" doc:"Its notes." example:"Paid by FAST"`
	LedgerImpact  []EntryLine         `json:"ledger_impact" nullable:"false" doc:"getCustomerReceipt only: the lines its entry wrote: debits positive."`
	RecordedBy    *Person             `json:"recorded_by,omitempty" doc:"Who recorded it."`
	RecordedAt    *time.Time          `json:"recorded_at,omitempty" doc:"When it was recorded." example:"2026-10-28T09:30:00Z"`
	ApprovalAsked string              `json:"approval_asked,omitempty" format:"uuid" doc:"getCustomerReceipt only: the pending request to void it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4dd3"`
}

// CustomerReceiptPage is a page of receipts, newest date first.
type CustomerReceiptPage struct {
	Items      []CustomerReceipt `json:"items" nullable:"false"`
	NextCursor string            `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjYtMTAtMzF8MDE5MiJ9"`
}

// CustomerReceiptPosting is a receipt after it is recorded or voided, with the entry it made.
type CustomerReceiptPosting struct {
	Receipt CustomerReceipt `json:"receipt" doc:"The receipt after the command."`
	Entry   Entry           `json:"entry" doc:"The entry the command posted: the receipt's entry, or its reversing entry."`
}

// CustomerReceiptPath names a receipt in the path.
type CustomerReceiptPath struct {
	ReceiptID string `path:"receipt_id" format:"uuid" doc:"The receipt's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f60"`
}

type receiptListInput struct {
	PageQuery
	Status string `query:"status" enum:"posted,reversed" doc:"Only receipts with this status." example:"posted"`
	From   string `query:"from" format:"date" doc:"Only receipts dated on or after this day." example:"2026-10-01"`
	To     string `query:"to" format:"date" doc:"Only receipts dated on or before this day." example:"2026-10-31"`
	Q      string `query:"q" maxLength:"100" doc:"Only receipts whose number contains this text, ignoring case." example:"RC-0003"`
}

type receiptListOutput struct{ Body CustomerReceiptPage }

type recordReceiptInput struct {
	KeyHeader
	PreferHeader
	Body CustomerReceiptDraft
}

type receiptReadOutput struct {
	ETag string `header:"ETag" doc:"The receipt's version and status, for If-Match."`
	Body CustomerReceipt
}

type receiptVoidInput struct {
	CustomerReceiptPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body Reversal
}

type receiptPostingOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The receipt's version and status, for If-Match."`
	Location string `header:"Location" doc:"The receipt's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved, and the entry shows what the command would post (FR-INT-14)."`
	Body     CustomerReceiptPosting
}

type receiptRequestOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     Approval
}

func receiptETag(r CustomerReceipt) string { return etag(r.Version, r.Status) }

// receiptDraftOf reads a receipt from the request, with every invalid field at once.
func receiptDraftOf(in CustomerReceiptDraft, base money.Currency) (receipt.Draft, error) {
	var d receipt.Draft
	var fields []FieldProblem
	for _, x := range []struct {
		text, field string
		to          *uuid.UUID
	}{{in.CustomerID, "body.customer_id", &d.CustomerID}, {in.MoneyAccountID, "body.money_account_id", &d.AccountID}} {
		if x.text == "" {
			continue
		}
		id, err := uuid.Parse(x.text)
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: x.field, Message: "is not an ID", Fix: "Send its ID"})
		}
		*x.to = id
	}
	if in.MoneyAccountID == "" {
		fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.money_account_id", Message: "is empty", Fix: "Send the money account the receipt came into"})
	}
	date, err := time.Parse(time.DateOnly, in.Date)
	if err != nil {
		fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.date", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"})
	}
	d.Date, d.Reference, d.Notes = date, strings.TrimSpace(in.Reference), in.Notes
	cur, err := money.Lookup(in.Currency)
	if err != nil {
		return d, invalid(append(fields, FieldProblem{Code: money.CodeCurrency, Field: "body.currency", Message: "is not a currency Fyniq knows",
			Fix: "Send an ISO 4217 code such as SGD"}))
	}
	d.Currency, d.Unapplied = cur, money.Zero(cur)
	if in.FXRate != "" {
		r, err := money.ParseRate(in.FXRate, cur, base, RateSourceDocument)
		if err != nil {
			fields = append(fields, fieldOf(err, "body.fx_rate"))
		} else {
			d.Rate = &r
		}
	}
	if in.Received != nil {
		m, bad := moneyIn(*in.Received, "body.received")
		if bad != nil {
			fields = append(fields, *bad)
		} else {
			d.Received = &m
		}
	}
	if in.Unapplied != nil {
		m, bad := moneyIn(*in.Unapplied, "body.unapplied")
		if bad != nil {
			fields = append(fields, *bad)
		} else {
			d.Unapplied = m
		}
	}
	for i, x := range in.Invoices {
		at := func(f string) string { return "body.invoices[" + strconv.Itoa(i) + "]." + f }
		id, err := uuid.Parse(x.InvoiceID)
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: at("invoice_id"), Message: "is not an ID", Fix: "Send the invoice's ID"})
		}
		m, bad := moneyIn(x.Amount, at("amount"))
		if bad != nil {
			fields = append(fields, *bad)
			continue
		}
		d.Invoices = append(d.Invoices, receipt.Part{InvoiceID: id, Amount: m})
	}
	if len(fields) > 0 {
		return d, invalid(fields)
	}
	return d, nil
}

// receiptProblems turns the posting rule's problems into the API's, naming the receipt.
func receiptProblems(err error) error {
	var lp ledger.Problems
	if !errors.As(err, &lp) || len(lp) == 0 {
		return err
	}
	fields := make([]FieldProblem, 0, len(lp))
	for _, x := range lp {
		fields = append(fields, FieldProblem{Code: x.Code, Field: x.Field, Message: x.Message, Fix: x.Fix})
	}
	return problem(lp[0].Code, fmt.Sprintf("The receipt cannot be recorded: %d problem(s), each listed in errors.", len(lp)),
		"Correct the receipt and record it again.", fields...)
}

// receiptView is what showing receipts reads once per request: the base currency, the chart, and the pending requests
// to void a receipt.
type receiptView struct {
	base     money.Currency
	accounts map[uuid.UUID]storage.Account
	asked    map[string]string
}

func (a *API) receiptView(ctx context.Context, u storage.UnitOfWork) (receiptView, error) {
	o, err := u.Organisation(ctx)
	if err != nil {
		return receiptView{}, err
	}
	v := receiptView{accounts: map[uuid.UUID]storage.Account{}, asked: map[string]string{}}
	if v.base, err = money.Lookup(o.BaseCurrency); err != nil {
		return receiptView{}, err
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return receiptView{}, err
	}
	for _, x := range accounts {
		v.accounts[x.ID] = x
	}
	requests, err := u.Approvals().List(ctx, "")
	if err != nil {
		return receiptView{}, err
	}
	now := a.deps.Clock.Now()
	for _, r := range requests {
		if r.ObjectType == approval.ObjectReceipt && approval.StatusAt(requestOf(r), now) == approval.Pending {
			v.asked[r.ObjectKey] = r.ID.String()
		}
	}
	return v, nil
}

// receiptOf is a receipt as the API shows it, from its document. withImpact fills its ledger impact
// (getCustomerReceipt).
func (a *API) receiptOf(ctx context.Context, u storage.UnitOfWork, v receiptView, doc storage.Document, withImpact bool) (CustomerReceipt, error) {
	d, err := receipt.Read(doc, v.base)
	if err != nil {
		return CustomerReceipt{}, err
	}
	total, err := d.Total()
	if err != nil {
		return CustomerReceipt{}, err
	}
	out := CustomerReceipt{ID: doc.ID.String(), Number: doc.Number, Status: string(doc.Status), Version: doc.Version, Date: dayText(d.Date), Currency: d.Currency.Code,
		Reference: d.Reference, Total: moneyOut(total), Unapplied: moneyOut(d.Unapplied), Invoices: []InvoiceSettled{}, Notes: d.Notes, LedgerImpact: []EntryLine{},
		RecordedBy: personOf(doc.PostedBy), RecordedAt: utc(doc.PostedAt)}
	if d.Rate != nil {
		out.FXRate = d.Rate.String()
	}
	if d.Received != nil {
		r := moneyOut(*d.Received)
		out.Received = &r
	}
	if d.CustomerID != uuid.Nil {
		out.Customer = &InvoiceCustomer{ID: d.CustomerID.String()}
		if c, err := u.Contacts().Get(ctx, d.CustomerID); err == nil {
			out.Customer.Name = c.LegalName
		}
	}
	out.MoneyAccount = &BillPaymentAccount{ID: d.AccountID.String()}
	if acc, ok := v.accounts[d.AccountID]; ok {
		out.MoneyAccount.Code, out.MoneyAccount.Name = acc.Code, acc.Name
	}
	allocs, err := u.Allocations().OfSource(ctx, doc.ID)
	if err != nil {
		return CustomerReceipt{}, err
	}
	for _, x := range allocs {
		if x.ReversesID != nil || x.SourceVersion != doc.Version {
			continue
		}
		s := InvoiceSettled{InvoiceID: x.DocumentID.String(), Amount: storedOut(x.Amount, x.Currency), Base: storedOut(x.BaseAmount, x.BaseCurrency),
			RealisedFX: storedOut(x.RealisedFX, x.BaseCurrency)}
		if inv, err := u.Documents().Get(ctx, x.DocumentID); err == nil {
			s.InvoiceNumber = inv.Number
		}
		out.Invoices = append(out.Invoices, s)
	}
	if withImpact {
		e, err := u.Ledger().ByIdentity(ctx, ledger.CustomerReceiptIdentity(doc.ID, doc.Version, ledger.RuleCustomerReceipt))
		if err != nil {
			return CustomerReceipt{}, err
		}
		out.LedgerImpact = entryOf(e).Lines
		out.ApprovalAsked = v.asked[out.ID]
	}
	return out, a.name(ctx, u, out.RecordedBy)
}

func receiptName(r CustomerReceipt) string {
	name := cmpText(r.Number, "the receipt")
	if r.Customer != nil && r.Customer.Name != "" {
		name += " from " + r.Customer.Name
	}
	return name
}

// gateReceiptVoid refuses the void of a posted receipt in Founder mode: it needs a second person (FM-1).
func gateReceiptVoid(ctx context.Context, u storage.UnitOfWork) error {
	mode, _, _, err := teamModeOf(ctx, u)
	if err != nil {
		return err
	}
	if mode == team.Founder {
		return problem(CodeReceiptVoidApproval, "In Founder mode, voiding a recorded receipt needs a second person (FM-1).", "Ask for another person's approval to void it")
	}
	return nil
}

// receiptLocked is a receipt locked in u and shown, checked against If-Match.
func (a *API) receiptLocked(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, ifMatch string) (storage.Document, CustomerReceipt, receiptView, error) {
	doc, err := u.Documents().Lock(ctx, id)
	if err != nil {
		return storage.Document{}, CustomerReceipt{}, receiptView{}, err
	}
	v, err := a.receiptView(ctx, u)
	if err != nil {
		return storage.Document{}, CustomerReceipt{}, receiptView{}, err
	}
	r, err := a.receiptOf(ctx, u, v, doc, false)
	if err != nil {
		return storage.Document{}, CustomerReceipt{}, receiptView{}, err
	}
	return doc, r, v, checkIfMatch(ifMatch, receiptETag(r))
}

func registerCustomerReceipts(a *API) {
	formCodes := []string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, money.CodeNotDecimal, money.CodePrecision, money.CodeRange,
		money.CodeCurrency, money.CodeRate, receipt.CodeTooLong, receipt.CodeTooMany, storage.CodeRetry, storage.CodeLockTimeout}
	readCodes := []string{CodeNotFound, receipt.CodeNotReceipt, receipt.CodeBadDraft}

	register(a, authz.ListCustomerReceipts, huma.Operation{
		OperationID: "listCustomerReceipts", Method: http.MethodGet, Path: "/v1/customer-receipts", Tags: []string{"Sales"},
		Summary: "List the customer receipts",
		Description: "The receipts from customers recorded by hand, newest date first, a page at a time, narrowed by status, dates and " +
			"text in the number (FR-AR-18). Each has its customer, money account, total and what it settled of each invoice. " +
			"Read-only; no side effects.",
	}, append([]string{CodeCursor, CodeInvalid}, readCodes...), func(ctx context.Context, in *receiptListInput) (*receiptListOutput, error) {
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		afterDate, afterID, err := journalCursor(after)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := receiptListOutput{Body: CustomerReceiptPage{Items: []CustomerReceipt{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentCustomerReceipt, Status: storage.DocumentStatus(in.Status),
				From: in.From, To: in.To, Search: strings.TrimSpace(in.Q), AfterDate: afterDate, AfterID: afterID, Limit: in.PageSize + 1})
			if err != nil {
				return err
			}
			if len(docs) > in.PageSize {
				docs = docs[:in.PageSize]
				last := docs[len(docs)-1]
				out.Body.NextCursor = encodeCursor(last.Date + "|" + last.ID.String())
			}
			v, err := a.receiptView(ctx, u)
			if err != nil {
				return err
			}
			for _, d := range docs {
				x, err := a.receiptOf(ctx, u, v, d, false)
				if err != nil {
					return err
				}
				out.Body.Items = append(out.Body.Items, x)
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.GetCustomerReceipt, huma.Operation{
		OperationID: "getCustomerReceipt", Method: http.MethodGet, Path: "/v1/customer-receipts/{receipt_id}", Tags: []string{"Sales"},
		Summary: "Get a customer receipt",
		Description: "Returns a receipt recorded by hand with its ETag: its customer, money account, rate, what it settled of each invoice " +
			"with the carrying amount cleared and the realised FX (ENG-41), the lines its entry wrote (ledger_impact), and the " +
			"pending request to void it. Read-only; no side effects.",
	}, readCodes, func(ctx context.Context, in *CustomerReceiptPath) (*receiptReadOutput, error) {
		id, err := parseID("receipt_id", in.ReceiptID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out CustomerReceipt
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Get(ctx, id)
			if err != nil {
				return err
			}
			v, err := a.receiptView(ctx, u)
			if err != nil {
				return err
			}
			out, err = a.receiptOf(ctx, u, v, doc, true)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &receiptReadOutput{ETag: receiptETag(out), Body: out}, nil
	})

	recordCodes := append(append(append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, team.CodeAction}, formCodes...),
		postingCodes()...), ReceiptCodes...)

	register(a, authz.RecordCustomerReceipt, huma.Operation{
		OperationID: "recordCustomerReceipt", Method: http.MethodPost, Path: "/v1/customer-receipts", Tags: []string{"Sales"}, DefaultStatus: http.StatusCreated,
		Summary: "Record a customer receipt",
		Description: "Records money a customer paid, by hand, and posts it at once under the rule customer_receipt@1 (FR-AR-18; ENG-41): " +
			"Cr 1100 with what it settles of each invoice at that invoice's stored rate, Dr the money account with what came in (at " +
			"the receipt's rate, or what a base-currency account received for invoices in another currency), and the realised FX " +
			"to 7030; the part that settles what is left of an invoice clears its carrying amount, the cent of RND-FX a part leaves " +
			"on a base-only line. It settles at most what each invoice has open (RECEIPT-05); what it received beyond the invoices " +
			"is kept as a credit with the customer. The money has come in already, so no approval is asked. Side effects: the " +
			"receipt, its number, the entry, an allocation per invoice, a new ledger revision, balances, audit events. Every " +
			"problem comes back together. Idempotent with Idempotency-Key; Prefer: return=preview posts nothing and shows the entry.",
	}, recordCodes, func(ctx context.Context, in *recordReceiptInput) (*receiptPostingOutput, error) {
		p, _ := authn.From(ctx)
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (CustomerReceiptPosting, error) {
			if err := checkConflicts(ctx, u, p, "Posting"); err != nil {
				return CustomerReceiptPosting{}, err
			}
			v, err := a.receiptView(ctx, u)
			if err != nil {
				return CustomerReceiptPosting{}, err
			}
			d, err := receiptDraftOf(in.Body, v.base)
			if err != nil {
				return CustomerReceiptPosting{}, err
			}
			if d.CustomerID == uuid.Nil && len(d.Invoices) > 0 { // the first invoice's customer
				if doc, err := u.Documents().Get(ctx, d.Invoices[0].InvoiceID); err == nil && invoice.IsInvoice(doc) {
					if inv, err := invoice.Read(doc, v.base); err == nil {
						d.CustomerID = inv.CustomerID
					}
				}
			}
			r, err := a.receipts.Record(ctx, p.Organisation, d)
			if err != nil {
				return CustomerReceiptPosting{}, receiptProblems(err)
			}
			doc, err := u.Documents().Get(ctx, r.Receipt.ID)
			if err != nil {
				return CustomerReceiptPosting{}, err
			}
			out, err := a.receiptOf(ctx, u, v, doc, true)
			return CustomerReceiptPosting{Receipt: out, Entry: entryOf(r.Entry)}, err
		})
		if err != nil {
			return nil, err
		}
		if w.Previewed {
			return &receiptPostingOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
		}
		return &receiptPostingOutput{Status: w.Status, ETag: receiptETag(w.Body.Receipt), Location: "/v1/customer-receipts/" + w.Body.Receipt.ID,
			Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	voidCodes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		team.CodeAction, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeReceiptVoidApproval, receipt.CodeNotPosted,
		receipt.CodeVoidDate, storage.CodeAllocation, storage.CodeDuplicate}, append(postingCodes(), readCodes...)...)

	register(a, authz.VoidCustomerReceipt, huma.Operation{
		OperationID: "voidCustomerReceipt", Method: http.MethodPost, Path: "/v1/customer-receipts/{receipt_id}/void", Tags: []string{"Sales"},
		Summary: "Void a customer receipt",
		Description: "Voids a recorded receipt with a reversing entry on the date given, with a reason, and a reversing allocation for " +
			"each invoice it settled, which reopens the invoices (FR-GL-12, FR-AR-18); posted history never changes. The date is " +
			"not before the receipt's (RECEIPT-07). In Founder mode voiding needs a second person (FM-1): it is refused " +
			"(RECEIPT-10), and requestCustomerReceiptVoid asks for the approval. Side effects: the reversing entry and allocations, " +
			"a new ledger revision, balances, audit events with the reason. Needs If-Match with the receipt's ETag. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview voids nothing.",
	}, voidCodes, func(ctx context.Context, in *receiptVoidInput) (*receiptPostingOutput, error) {
		id, err := parseID("receipt_id", in.ReceiptID)
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
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (CustomerReceiptPosting, error) {
			_, _, v, err := a.receiptLocked(ctx, u, id, in.IfMatch)
			if err != nil {
				return CustomerReceiptPosting{}, err
			}
			if err := checkConflicts(ctx, u, p, "Posting"); err != nil {
				return CustomerReceiptPosting{}, err
			}
			if err := gateReceiptVoid(ctx, u); err != nil {
				return CustomerReceiptPosting{}, err
			}
			r, err := a.receipts.Void(ctx, p.Organisation, id, date, strings.TrimSpace(in.Body.Reason))
			if err != nil {
				return CustomerReceiptPosting{}, err
			}
			out, err := a.receiptOf(ctx, u, v, r.Receipt, true)
			return CustomerReceiptPosting{Receipt: out, Entry: entryOf(r.Entry)}, err
		})
		if err != nil {
			return nil, err
		}
		if w.Previewed {
			return &receiptPostingOutput{Status: http.StatusOK, Applied: applied(w), Body: w.Body}, nil
		}
		return &receiptPostingOutput{Status: http.StatusOK, ETag: receiptETag(w.Body.Receipt), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	requestCodes := []string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		CodeSessionRequired, CodeStepUpRequired, approval.CodePending, storage.CodeApproval, receipt.CodeNotReceipt, receipt.CodeNotPosted,
		receipt.CodeBadDraft, receipt.CodeVoidDate, storage.CodeRetry, storage.CodeLockTimeout, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}

	register(a, authz.RequestCustomerReceiptVoid, huma.Operation{
		OperationID: "requestCustomerReceiptVoid", Method: http.MethodPost, Path: "/v1/customer-receipts/{receipt_id}/void-approval", Tags: []string{"Sales"},
		Summary: "Ask for approval to void a customer receipt", DefaultStatus: http.StatusAccepted,
		Description: "Asks for another person's approval to void a recorded receipt with a reversing entry on the date given, with a " +
			"reason (FR-GL-12, FM-1): what voidCustomerReceipt asks for in Founder mode (RECEIPT-10). The answer is 202 with an " +
			"approval request, which a person who holds A on \"Invoices, credit notes\" and did not ask approves, stepped up; on the " +
			"approval the void posts and the invoices reopen. Asking needs a person signed in who passed step-up, and If-Match with " +
			"the receipt's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.",
	}, requestCodes, func(ctx context.Context, in *receiptVoidInput) (*receiptRequestOutput, error) {
		id, err := parseID("receipt_id", in.ReceiptID)
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
		reason := strings.TrimSpace(in.Body.Reason)
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
			doc, rec, _, err := a.receiptLocked(ctx, u, id, in.IfMatch)
			if err != nil {
				return Approval{}, err
			}
			if doc.Status != storage.DocumentPosted {
				return Approval{}, problem(receipt.CodeNotPosted, fmt.Sprintf("The receipt is %s.", doc.Status), "Ask for approval to void a posted receipt")
			}
			if rec.Date != "" && in.Body.Date < rec.Date {
				return Approval{}, problem(receipt.CodeVoidDate, fmt.Sprintf("The void is dated %s, before the receipt's %s.", dayText(date), rec.Date),
					"Date the void on or after the receipt")
			}
			return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectReceipt, ObjectKey: doc.ID.String(), Operation: approval.OpReceiptVoid,
				Summary: fmt.Sprintf("Void %s, %s %s, on %s: %s", receiptName(rec), rec.Total.Amount, rec.Total.Currency, in.Body.Date, reason),
				Execute: map[string]any{"receipt_id": doc.ID.String(), "date": in.Body.Date, "reason": reason},
				Diff:    []ApprovalDiff{{Field: "status", Before: "posted", After: "reversed"}, {Field: "void_date", After: in.Body.Date}}})
		})
		if err != nil {
			return nil, err
		}
		return &receiptRequestOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})
}
