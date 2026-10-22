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
	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/billpayment"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/team"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------- payments of bills recorded by hand (FR-AP-02, FR-AR-18, FM-1)
//
// Until bank import matches payments, a payment the company made is recorded by hand (the owner's decision of 22 Oct
// 2026): the money account it left, the date, and what it settled of each of the supplier's bills, in their currency.
// Recording it posts bill_payment@1 at once, as the money has left already: Dr 2010 at each bill's stored rate, Cr the
// money account, and the realised FX to 7030 (ENG-41). Voiding it reverses the entry and its allocations, which reopens
// the bills; in Founder mode a void needs a second person (FM-1).

// CodePaymentVoidApproval is the API's code for a void that needs a second person in Founder mode (FM-1).
const CodePaymentVoidApproval = "BILLPAY-10"

// PaymentCodes are the codes the posting rule and the payment's own checks answer.
var PaymentCodes = []string{billpayment.CodeSupplier, billpayment.CodeBill, billpayment.CodeOverpaid, billpayment.CodeTooLong, billpayment.CodeTooMany,
	ledger.CodeNotMoneyAccount, ledger.CodePaymentAmount, ledger.CodePaidAmount, ledger.CodeSystemRole, ledger.CodeLineCurrency}

// PaymentApprovalCodes are the codes an approval of a payment's void may answer.
func PaymentApprovalCodes() []string {
	return []string{billpayment.CodeNotPayment, billpayment.CodeNotPosted, billpayment.CodeVoidDate, billpayment.CodeBadDraft}
}

// BillPaymentPart is what a payment settles of one bill, as entered.
type BillPaymentPart struct {
	BillID string `json:"bill_id" format:"uuid" doc:"The bill: one of the supplier's posted bills, in the payment's currency." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e30"`
	Amount Money  `json:"amount" doc:"What the payment settles of it, in the bill's currency: at most what is open of it (BILLPAY-05)."`
}

// BillPaymentDraft is a payment as recorded.
type BillPaymentDraft struct {
	SupplierID     string            `json:"supplier_id,omitempty" format:"uuid" doc:"The supplier paid; left out, the first bill's supplier. Every bill is the supplier's." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e20"`
	Date           string            `json:"date" format:"date" doc:"The day the money left the account." example:"2026-10-20"`
	MoneyAccountID string            `json:"money_account_id" format:"uuid" doc:"The bank, card or wallet account it left: an active money account in the bills' currency or in the base currency." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d10"`
	Currency       string            `json:"currency" pattern:"^[A-Z]{3}$" doc:"The bills' currency." example:"SGD"`
	FXRate         string            `json:"fx_rate,omitempty" doc:"For bills in another currency paid from an account in that currency: base currency per one unit, as on the bank's advice. Left out, the cascade's rate for the date (ENG-40)." example:"1.30"`
	Paid           *Money            `json:"paid,omitempty" doc:"For bills in another currency paid from a base-currency account: what the account paid, in the base currency, as the statement shows it; the payment settles at the rate it implies. Required then, and only then (LEDGER-27)."`
	Reference      string            `json:"reference,omitempty" maxLength:"140" doc:"The bank's reference for it." example:"FAST 88231"`
	Bills          []BillPaymentPart `json:"bills" maxItems:"100" doc:"What it settles of each bill."`
	Unapplied      *Money            `json:"unapplied,omitempty" doc:"What it paid beyond the bills, in their currency: kept as a credit with the supplier (FR-AR-18). Left out, nothing."`
	Notes          string            `json:"notes,omitempty" maxLength:"2000" doc:"Notes kept with it." example:"Paid in two parts, as agreed"`
}

// BillSettled is what a payment settled of one bill: its allocation (FR-AR-18).
type BillSettled struct {
	BillID     string `json:"bill_id" format:"uuid" doc:"The bill." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e30"`
	BillNumber string `json:"bill_number,omitempty" doc:"The bill's number." example:"BILL-0007"`
	Amount     Money  `json:"amount" doc:"What it settled, in the bill's currency."`
	Base       Money  `json:"base" doc:"The bill's carrying amount it cleared, in the base currency: the amount at the bill's stored rate, and for the part that settles what is left of a bill, what was left (ENG-14)."`
	RealisedFX Money  `json:"realised_fx" doc:"The realised gain (positive) or loss (negative) in the base currency: the amount at the payment's rate against the bill's stored rate, with no tolerance (ENG-41)."`
}

// BillPaymentAccount is the money account a payment left.
type BillPaymentAccount struct {
	ID   string `json:"id" format:"uuid" doc:"The account's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d10"`
	Code string `json:"code,omitempty" doc:"Its code." example:"1010"`
	Name string `json:"name,omitempty" doc:"Its name." example:"DBS current"`
}

// BillPayment is a payment of bills recorded by hand.
type BillPayment struct {
	ID            string              `json:"id" format:"uuid" doc:"The payment's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e40"`
	Number        string              `json:"number,omitempty" doc:"Its number from the payment sequence (FR-ORG-04)." example:"PAY-0003"`
	Status        string              `json:"status" enum:"posted,reversed" doc:"posted once recorded; reversed by a void." example:"posted"`
	Version       int                 `json:"version" doc:"The version of its content; with the status it makes the ETag." example:"1"`
	Supplier      *BillSupplier       `json:"supplier,omitempty" doc:"The supplier paid."`
	Date          string              `json:"date,omitempty" format:"date" doc:"The day the money left." example:"2026-10-20"`
	MoneyAccount  *BillPaymentAccount `json:"money_account,omitempty" doc:"The account it left."`
	Currency      string              `json:"currency" doc:"The bills' currency." example:"SGD"`
	FXRate        string              `json:"fx_rate,omitempty" doc:"The rate typed, if one was." example:"1.30"`
	Paid          *Money              `json:"paid,omitempty" doc:"What a base-currency account paid for bills in another currency."`
	Reference     string              `json:"reference,omitempty" doc:"The bank's reference." example:"FAST 88231"`
	Total         Money               `json:"total" doc:"What it paid in the bills' currency: every part and the unapplied amount."`
	Unapplied     Money               `json:"unapplied" doc:"What it paid beyond the bills: a credit with the supplier."`
	Bills         []BillSettled       `json:"bills" nullable:"false" doc:"What it settled of each bill, as recorded."`
	Notes         string              `json:"notes,omitempty" doc:"Its notes." example:"Paid in two parts"`
	LedgerImpact  []EntryLine         `json:"ledger_impact" nullable:"false" doc:"getBillPayment only: the lines its entry wrote: debits positive."`
	RecordedBy    *Person             `json:"recorded_by,omitempty" doc:"Who recorded it."`
	RecordedAt    *time.Time          `json:"recorded_at,omitempty" doc:"When it was recorded." example:"2026-10-20T09:30:00Z"`
	ApprovalAsked string              `json:"approval_asked,omitempty" format:"uuid" doc:"getBillPayment only: the pending request to void it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4dd1"`
}

// BillPaymentPage is a page of payments, newest date first.
type BillPaymentPage struct {
	Items      []BillPayment `json:"items" nullable:"false"`
	NextCursor string        `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjYtMTAtMzF8MDE5MiJ9"`
}

// BillPaymentPosting is a payment after it is recorded or voided, with the entry it made.
type BillPaymentPosting struct {
	Payment BillPayment `json:"payment" doc:"The payment after the command."`
	Entry   Entry       `json:"entry" doc:"The entry the command posted: the payment's entry, or its reversing entry."`
}

// BillPaymentPath names a payment in the path.
type BillPaymentPath struct {
	PaymentID string `path:"payment_id" format:"uuid" doc:"The payment's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e40"`
}

type paymentListInput struct {
	PageQuery
	Status string `query:"status" enum:"posted,reversed" doc:"Only payments with this status." example:"posted"`
	From   string `query:"from" format:"date" doc:"Only payments dated on or after this day." example:"2026-10-01"`
	To     string `query:"to" format:"date" doc:"Only payments dated on or before this day." example:"2026-10-31"`
	Q      string `query:"q" maxLength:"100" doc:"Only payments whose number contains this text, ignoring case." example:"PAY-0003"`
}

type paymentListOutput struct{ Body BillPaymentPage }

type recordPaymentInput struct {
	KeyHeader
	PreferHeader
	Body BillPaymentDraft
}

type paymentReadOutput struct {
	ETag string `header:"ETag" doc:"The payment's version and status, for If-Match."`
	Body BillPayment
}

type paymentVoidInput struct {
	BillPaymentPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body Reversal
}

type paymentPostingOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The payment's version and status, for If-Match."`
	Location string `header:"Location" doc:"The payment's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved, and the entry shows what the command would post (FR-INT-14)."`
	Body     BillPaymentPosting
}

func paymentETag(p BillPayment) string { return etag(p.Version, p.Status) }

// paymentDraftOf reads a payment from the request, with every invalid field at once.
func paymentDraftOf(in BillPaymentDraft, base money.Currency) (billpayment.Draft, error) {
	var d billpayment.Draft
	var fields []FieldProblem
	for _, x := range []struct {
		text, field string
		to          *uuid.UUID
	}{{in.SupplierID, "body.supplier_id", &d.SupplierID}, {in.MoneyAccountID, "body.money_account_id", &d.AccountID}} {
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
		fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.money_account_id", Message: "is empty", Fix: "Send the money account the payment left"})
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
	if in.Paid != nil {
		m, bad := moneyIn(*in.Paid, "body.paid")
		if bad != nil {
			fields = append(fields, *bad)
		} else {
			d.Paid = &m
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
	for i, x := range in.Bills {
		at := func(f string) string { return "body.bills[" + strconv.Itoa(i) + "]." + f }
		id, err := uuid.Parse(x.BillID)
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: at("bill_id"), Message: "is not an ID", Fix: "Send the bill's ID"})
		}
		m, bad := moneyIn(x.Amount, at("amount"))
		if bad != nil {
			fields = append(fields, *bad)
			continue
		}
		d.Bills = append(d.Bills, billpayment.Part{BillID: id, Amount: m})
	}
	if len(fields) > 0 {
		return d, invalid(fields)
	}
	return d, nil
}

// paymentProblems turns the posting rule's problems into the API's, naming the payment.
func paymentProblems(err error) error {
	var lp ledger.Problems
	if !errors.As(err, &lp) || len(lp) == 0 {
		return err
	}
	fields := make([]FieldProblem, 0, len(lp))
	for _, x := range lp {
		fields = append(fields, FieldProblem{Code: x.Code, Field: x.Field, Message: x.Message, Fix: x.Fix})
	}
	return problem(lp[0].Code, fmt.Sprintf("The payment cannot be recorded: %d problem(s), each listed in errors.", len(lp)),
		"Correct the payment and record it again.", fields...)
}

// paymentView is what showing payments reads once per request: the base currency, the chart, and the pending requests
// to void a payment.
type paymentView struct {
	base     money.Currency
	accounts map[uuid.UUID]storage.Account
	asked    map[string]string
}

func (a *API) paymentView(ctx context.Context, u storage.UnitOfWork) (paymentView, error) {
	o, err := u.Organisation(ctx)
	if err != nil {
		return paymentView{}, err
	}
	v := paymentView{accounts: map[uuid.UUID]storage.Account{}, asked: map[string]string{}}
	if v.base, err = money.Lookup(o.BaseCurrency); err != nil {
		return paymentView{}, err
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return paymentView{}, err
	}
	for _, x := range accounts {
		v.accounts[x.ID] = x
	}
	requests, err := u.Approvals().List(ctx, "")
	if err != nil {
		return paymentView{}, err
	}
	now := a.deps.Clock.Now()
	for _, r := range requests {
		if r.ObjectType == approval.ObjectPayment && approval.StatusAt(requestOf(r), now) == approval.Pending {
			v.asked[r.ObjectKey] = r.ID.String()
		}
	}
	return v, nil
}

func moneyOut(m money.Money) Money { return Money{Amount: m.String(), Currency: m.Currency().Code} }

// stored reads a stored decimal in a currency as the API shows it, at the currency's scale.
func storedOut(d storage.Decimal, code string) Money {
	if c, err := money.Lookup(code); err == nil {
		if m, err := money.ParseStored(string(d), c); err == nil {
			return moneyOut(m)
		}
	}
	return Money{Amount: string(d), Currency: code}
}

// paymentOf is a payment as the API shows it, from its document. withImpact fills its ledger impact (getBillPayment).
func (a *API) paymentOf(ctx context.Context, u storage.UnitOfWork, v paymentView, doc storage.Document, withImpact bool) (BillPayment, error) {
	d, err := billpayment.Read(doc, v.base)
	if err != nil {
		return BillPayment{}, err
	}
	total, err := d.Total()
	if err != nil {
		return BillPayment{}, err
	}
	out := BillPayment{ID: doc.ID.String(), Number: doc.Number, Status: string(doc.Status), Version: doc.Version, Date: dayText(d.Date), Currency: d.Currency.Code,
		Reference: d.Reference, Total: moneyOut(total), Unapplied: moneyOut(d.Unapplied), Bills: []BillSettled{}, Notes: d.Notes, LedgerImpact: []EntryLine{},
		RecordedBy: personOf(doc.PostedBy), RecordedAt: utc(doc.PostedAt)}
	if d.Rate != nil {
		out.FXRate = d.Rate.String()
	}
	if d.Paid != nil {
		p := moneyOut(*d.Paid)
		out.Paid = &p
	}
	if d.SupplierID != uuid.Nil {
		out.Supplier = &BillSupplier{ID: d.SupplierID.String()}
		if c, err := u.Contacts().Get(ctx, d.SupplierID); err == nil {
			out.Supplier.Name = c.LegalName
		}
	}
	out.MoneyAccount = &BillPaymentAccount{ID: d.AccountID.String()}
	if acc, ok := v.accounts[d.AccountID]; ok {
		out.MoneyAccount.Code, out.MoneyAccount.Name = acc.Code, acc.Name
	}
	allocs, err := u.Allocations().OfSource(ctx, doc.ID)
	if err != nil {
		return BillPayment{}, err
	}
	for _, x := range allocs {
		if x.ReversesID != nil || x.SourceVersion != doc.Version {
			continue
		}
		s := BillSettled{BillID: x.DocumentID.String(), Amount: storedOut(x.Amount, x.Currency), Base: storedOut(x.BaseAmount, x.BaseCurrency),
			RealisedFX: storedOut(x.RealisedFX, x.BaseCurrency)}
		if b, err := u.Documents().Get(ctx, x.DocumentID); err == nil {
			s.BillNumber = b.Number
		}
		out.Bills = append(out.Bills, s)
	}
	if withImpact {
		e, err := u.Ledger().ByIdentity(ctx, ledger.BillPaymentIdentity(doc.ID, doc.Version, ledger.RuleBillPayment))
		if err != nil {
			return BillPayment{}, err
		}
		out.LedgerImpact = entryOf(e).Lines
		out.ApprovalAsked = v.asked[out.ID]
	}
	return out, a.name(ctx, u, out.RecordedBy)
}

func paymentName(p BillPayment) string {
	name := cmpText(p.Number, "the payment")
	if p.Supplier != nil && p.Supplier.Name != "" {
		name += " to " + p.Supplier.Name
	}
	return name
}

// gatePaymentVoid refuses the void of a posted payment in Founder mode: it needs a second person (FM-1).
func gatePaymentVoid(ctx context.Context, u storage.UnitOfWork) error {
	mode, _, _, err := teamModeOf(ctx, u)
	if err != nil {
		return err
	}
	if mode == team.Founder {
		return problem(CodePaymentVoidApproval, "In Founder mode, voiding a recorded payment needs a second person (FM-1).", "Ask for another person's approval to void it")
	}
	return nil
}

// paymentLocked is a payment locked in u and shown, checked against If-Match.
func (a *API) paymentLocked(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, ifMatch string) (storage.Document, BillPayment, paymentView, error) {
	doc, err := u.Documents().Lock(ctx, id)
	if err != nil {
		return storage.Document{}, BillPayment{}, paymentView{}, err
	}
	v, err := a.paymentView(ctx, u)
	if err != nil {
		return storage.Document{}, BillPayment{}, paymentView{}, err
	}
	p, err := a.paymentOf(ctx, u, v, doc, false)
	if err != nil {
		return storage.Document{}, BillPayment{}, paymentView{}, err
	}
	return doc, p, v, checkIfMatch(ifMatch, paymentETag(p))
}

func registerBillPayments(a *API) {
	formCodes := []string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, money.CodeNotDecimal, money.CodePrecision, money.CodeRange,
		money.CodeCurrency, money.CodeRate, billpayment.CodeTooLong, billpayment.CodeTooMany, storage.CodeRetry, storage.CodeLockTimeout}
	readCodes := []string{CodeNotFound, billpayment.CodeNotPayment, billpayment.CodeBadDraft}

	register(a, authz.ListBillPayments, huma.Operation{
		OperationID: "listBillPayments", Method: http.MethodGet, Path: "/v1/bill-payments", Tags: []string{"Purchases"},
		Summary: "List the payments of bills",
		Description: "The payments of suppliers' bills recorded by hand, newest date first, a page at a time, narrowed by status, dates " +
			"and text in the number (FR-AP-02). Each has its supplier, money account, total and what it settled of each bill. " +
			"Read-only; no side effects.",
	}, append([]string{CodeCursor, CodeInvalid}, readCodes...), func(ctx context.Context, in *paymentListInput) (*paymentListOutput, error) {
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		afterDate, afterID, err := journalCursor(after)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := paymentListOutput{Body: BillPaymentPage{Items: []BillPayment{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentBillPayment, Status: storage.DocumentStatus(in.Status),
				From: in.From, To: in.To, Search: strings.TrimSpace(in.Q), AfterDate: afterDate, AfterID: afterID, Limit: in.PageSize + 1})
			if err != nil {
				return err
			}
			if len(docs) > in.PageSize {
				docs = docs[:in.PageSize]
				last := docs[len(docs)-1]
				out.Body.NextCursor = encodeCursor(last.Date + "|" + last.ID.String())
			}
			v, err := a.paymentView(ctx, u)
			if err != nil {
				return err
			}
			for _, d := range docs {
				x, err := a.paymentOf(ctx, u, v, d, false)
				if err != nil {
					return err
				}
				out.Body.Items = append(out.Body.Items, x)
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.GetBillPayment, huma.Operation{
		OperationID: "getBillPayment", Method: http.MethodGet, Path: "/v1/bill-payments/{payment_id}", Tags: []string{"Purchases"},
		Summary: "Get a payment of bills",
		Description: "Returns a payment recorded by hand with its ETag: its supplier, money account, rate, what it settled of each bill with " +
			"the carrying amount cleared and the realised FX (ENG-41), the lines its entry wrote (ledger_impact), and the pending " +
			"request to void it. Read-only; no side effects.",
	}, readCodes, func(ctx context.Context, in *BillPaymentPath) (*paymentReadOutput, error) {
		id, err := parseID("payment_id", in.PaymentID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out BillPayment
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Get(ctx, id)
			if err != nil {
				return err
			}
			v, err := a.paymentView(ctx, u)
			if err != nil {
				return err
			}
			out, err = a.paymentOf(ctx, u, v, doc, true)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &paymentReadOutput{ETag: paymentETag(out), Body: out}, nil
	})

	recordCodes := append(append(append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, team.CodeAction}, formCodes...),
		postingCodes()...), PaymentCodes...)

	register(a, authz.RecordBillPayment, huma.Operation{
		OperationID: "recordBillPayment", Method: http.MethodPost, Path: "/v1/bill-payments", Tags: []string{"Purchases"}, DefaultStatus: http.StatusCreated,
		Summary: "Record a payment of bills",
		Description: "Records a payment the company made to a supplier, by hand, and posts it at once under the rule bill_payment@1 " +
			"(FR-AP-02; ENG-41): Dr 2010 with what it settles of each bill at that bill's stored rate, Cr the money account with what " +
			"left it (at the payment's rate, or what a base-currency account paid for bills in another currency), and the realised " +
			"FX to 7030; the part that settles what is left of a bill clears its carrying amount, the cent of RND-FX a part leaves " +
			"on a base-only line. It settles at most what each bill has open (BILLPAY-05); what it paid beyond the bills is kept " +
			"as a credit with the supplier. The money has left already, so no approval is asked. Side effects: the payment, its " +
			"number, the entry, an allocation per bill, a new ledger revision, balances, audit events. Every problem comes back " +
			"together. Idempotent with Idempotency-Key; Prefer: return=preview posts nothing and shows the entry.",
	}, recordCodes, func(ctx context.Context, in *recordPaymentInput) (*paymentPostingOutput, error) {
		p, _ := authn.From(ctx)
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (BillPaymentPosting, error) {
			if err := checkConflicts(ctx, u, p, "Posting"); err != nil {
				return BillPaymentPosting{}, err
			}
			v, err := a.paymentView(ctx, u)
			if err != nil {
				return BillPaymentPosting{}, err
			}
			d, err := paymentDraftOf(in.Body, v.base)
			if err != nil {
				return BillPaymentPosting{}, err
			}
			if d.SupplierID == uuid.Nil && len(d.Bills) > 0 { // the first bill's supplier
				if doc, err := u.Documents().Get(ctx, d.Bills[0].BillID); err == nil && doc.Type == storage.DocumentBill {
					if b, err := bill.Read(doc, v.base); err == nil {
						d.SupplierID = b.SupplierID
					}
				}
			}
			r, err := a.payments.Record(ctx, p.Organisation, d)
			if err != nil {
				return BillPaymentPosting{}, paymentProblems(err)
			}
			doc, err := u.Documents().Get(ctx, r.Payment.ID)
			if err != nil {
				return BillPaymentPosting{}, err
			}
			out, err := a.paymentOf(ctx, u, v, doc, true)
			return BillPaymentPosting{Payment: out, Entry: entryOf(r.Entry)}, err
		})
		if err != nil {
			return nil, err
		}
		if w.Previewed {
			return &paymentPostingOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
		}
		return &paymentPostingOutput{Status: w.Status, ETag: paymentETag(w.Body.Payment), Location: "/v1/bill-payments/" + w.Body.Payment.ID,
			Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	voidCodes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		team.CodeAction, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodePaymentVoidApproval, billpayment.CodeNotPosted,
		billpayment.CodeVoidDate, storage.CodeAllocation, storage.CodeDuplicate}, append(postingCodes(), readCodes...)...)

	register(a, authz.VoidBillPayment, huma.Operation{
		OperationID: "voidBillPayment", Method: http.MethodPost, Path: "/v1/bill-payments/{payment_id}/void", Tags: []string{"Purchases"},
		Summary: "Void a payment of bills",
		Description: "Voids a recorded payment with a reversing entry on the date given, with a reason, and a reversing allocation for " +
			"each bill it settled, which reopens the bills (FR-GL-12, FR-AR-18); posted history never changes. The date is not " +
			"before the payment's (BILLPAY-07). In Founder mode voiding needs a second person (FM-1): it is refused (BILLPAY-10), " +
			"and requestBillPaymentVoid asks for the approval. Side effects: the reversing entry and allocations, a new ledger " +
			"revision, balances, audit events with the reason. Needs If-Match with the payment's ETag. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview voids nothing.",
	}, voidCodes, func(ctx context.Context, in *paymentVoidInput) (*paymentPostingOutput, error) {
		id, err := parseID("payment_id", in.PaymentID)
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
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (BillPaymentPosting, error) {
			_, _, v, err := a.paymentLocked(ctx, u, id, in.IfMatch)
			if err != nil {
				return BillPaymentPosting{}, err
			}
			if err := checkConflicts(ctx, u, p, "Posting"); err != nil {
				return BillPaymentPosting{}, err
			}
			if err := gatePaymentVoid(ctx, u); err != nil {
				return BillPaymentPosting{}, err
			}
			r, err := a.payments.Void(ctx, p.Organisation, id, date, strings.TrimSpace(in.Body.Reason))
			if err != nil {
				return BillPaymentPosting{}, err
			}
			out, err := a.paymentOf(ctx, u, v, r.Payment, true)
			return BillPaymentPosting{Payment: out, Entry: entryOf(r.Entry)}, err
		})
		if err != nil {
			return nil, err
		}
		if w.Previewed {
			return &paymentPostingOutput{Status: http.StatusOK, Applied: applied(w), Body: w.Body}, nil
		}
		return &paymentPostingOutput{Status: http.StatusOK, ETag: paymentETag(w.Body.Payment), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	requestCodes := []string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		CodeSessionRequired, CodeStepUpRequired, approval.CodePending, storage.CodeApproval, billpayment.CodeNotPayment, billpayment.CodeNotPosted,
		billpayment.CodeBadDraft, billpayment.CodeVoidDate, storage.CodeRetry, storage.CodeLockTimeout, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}

	register(a, authz.RequestBillPaymentVoid, huma.Operation{
		OperationID: "requestBillPaymentVoid", Method: http.MethodPost, Path: "/v1/bill-payments/{payment_id}/void-approval", Tags: []string{"Purchases"},
		Summary: "Ask for approval to void a payment of bills", DefaultStatus: http.StatusAccepted,
		Description: "Asks for another person's approval to void a recorded payment with a reversing entry on the date given, with a " +
			"reason (FR-GL-12, FM-1): what voidBillPayment asks for in Founder mode (BILLPAY-10). The answer is 202 with an approval " +
			"request, which a person who holds A on \"Bills, vendor credits\" and did not ask approves, stepped up; on the approval " +
			"the void posts and the bills reopen. Asking needs a person signed in who passed step-up, and If-Match with the " +
			"payment's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.",
	}, requestCodes, func(ctx context.Context, in *paymentVoidInput) (*billRequestOutput, error) {
		id, err := parseID("payment_id", in.PaymentID)
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
			doc, pay, _, err := a.paymentLocked(ctx, u, id, in.IfMatch)
			if err != nil {
				return Approval{}, err
			}
			if doc.Status != storage.DocumentPosted {
				return Approval{}, problem(billpayment.CodeNotPosted, fmt.Sprintf("The payment is %s.", doc.Status), "Ask for approval to void a posted payment")
			}
			if pay.Date != "" && in.Body.Date < pay.Date {
				return Approval{}, problem(billpayment.CodeVoidDate, fmt.Sprintf("The void is dated %s, before the payment's %s.", dayText(date), pay.Date),
					"Date the void on or after the payment")
			}
			return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectPayment, ObjectKey: doc.ID.String(), Operation: approval.OpPaymentVoid,
				Summary: fmt.Sprintf("Void %s, %s %s, on %s: %s", paymentName(pay), pay.Total.Amount, pay.Total.Currency, in.Body.Date, reason),
				Execute: map[string]any{"payment_id": doc.ID.String(), "date": in.Body.Date, "reason": reason},
				Diff:    []ApprovalDiff{{Field: "status", Before: "posted", After: "reversed"}, {Field: "void_date", After: in.Body.Date}}})
		})
		if err != nil {
			return nil, err
		}
		return &billRequestOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})
}
