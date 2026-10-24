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
	"github.com/cognizhi/fyniq/internal/domain/deposit"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/team"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ---------------------------------------------------------- customer deposits (FR-AR-09, ENG-45, FM-1; p1-sales)
//
// Money a customer pays ahead of an invoice is recorded by hand and posted at once by customer_deposit@1: Dr the money
// account, Cr 2410 in the base currency at the rate it came in, and Cr 2200 with the GST a part payment bears when
// received, once the organisation is GST-registered (ENG-45); a security deposit bears none until it is used. The final
// invoice deducts it (the owner's decision of 24 Oct 2026). Voiding one reverses its entry once nothing uses it; in
// Founder mode a void needs a second person (FM-1).

// CodeDepositVoidApproval is the API's code for a void that needs a second person in Founder mode (FM-1).
const CodeDepositVoidApproval = "DEPOSIT-10"

// DepositCodes are the codes the posting rule and the deposit's own checks answer.
var DepositCodes = []string{deposit.CodeCustomer, deposit.CodeKind, deposit.CodeGSTCode, deposit.CodeTooLong, ledger.CodeNotMoneyAccount,
	ledger.CodePaymentAmount, ledger.CodePaidAmount, ledger.CodeSystemRole, ledger.CodeLineCurrency, ledger.CodeGSTRate}

// DepositApprovalCodes are the codes an approval of a deposit's void may answer.
func DepositApprovalCodes() []string {
	return []string{deposit.CodeNotDeposit, deposit.CodeNotPosted, deposit.CodeVoidDate, deposit.CodeBadDraft, deposit.CodeUsed}
}

// CustomerDepositDraft is a deposit as recorded.
type CustomerDepositDraft struct {
	CustomerID     string `json:"customer_id" format:"uuid" doc:"The customer who paid it: a contact marked as a customer." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f20"`
	Kind           string `json:"kind" enum:"part_payment,security" doc:"part_payment: paid ahead for what will be invoiced, so it bears GST when received once the organisation is GST-registered (ENG-45); security: held as security, bearing no GST until it is used as payment (FR-AR-09)." example:"part_payment"`
	Date           string `json:"date" format:"date" doc:"The day the money came into the account." example:"2026-10-20"`
	MoneyAccountID string `json:"money_account_id" format:"uuid" doc:"The bank, card or wallet account it came into: an active money account in the deposit's currency or in the base currency." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d10"`
	Currency       string `json:"currency" pattern:"^[A-Z]{3}$" doc:"The deposit's currency." example:"SGD"`
	FXRate         string `json:"fx_rate,omitempty" doc:"For a deposit in another currency received into an account in that currency: base currency per one unit, as on the bank's advice. Left out, the cascade's rate for the date (ENG-40)." example:"1.35"`
	Received       *Money `json:"received,omitempty" doc:"For a deposit in another currency received into a base-currency account: what the account received, in the base currency. Required then, and only then (LEDGER-27)."`
	Amount         Money  `json:"amount" doc:"What the customer paid, GST included, in the deposit's currency."`
	GSTCode        string `json:"gst_code,omitempty" pattern:"^[A-Z]{2,4}$" doc:"A part payment only: the GST code of what it pays for, from the sg-gst pack in force on its date, such as SR; its GST is RND-GST-INCL of the amount in the base currency, none before the organisation is GST-registered. A security deposit names none." example:"SR"`
	Reference      string `json:"reference,omitempty" maxLength:"140" doc:"The bank's reference for it." example:"FAST 55120"`
	Notes          string `json:"notes,omitempty" maxLength:"2000" doc:"Notes kept with it." example:"30% deposit on the annual licence"`
}

// CustomerDeposit is money a customer paid ahead of an invoice.
type CustomerDeposit struct {
	ID            string              `json:"id" format:"uuid" doc:"The deposit's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fa0"`
	Number        string              `json:"number,omitempty" doc:"Its number from the deposit sequence (FR-ORG-04)." example:"DP-0001"`
	Status        string              `json:"status" enum:"posted,reversed" doc:"posted once recorded; reversed by a void." example:"posted"`
	Version       int                 `json:"version" doc:"The version of its content; with the status it makes the ETag." example:"1"`
	Customer      *InvoiceCustomer    `json:"customer,omitempty" doc:"The customer who paid it."`
	Kind          string              `json:"kind" enum:"part_payment,security" doc:"A part payment, or a security deposit." example:"part_payment"`
	Date          string              `json:"date,omitempty" format:"date" doc:"The day the money came in." example:"2026-10-20"`
	MoneyAccount  *BillPaymentAccount `json:"money_account,omitempty" doc:"The account it came into."`
	Amount        Money               `json:"amount" doc:"What the customer paid, GST included, in its currency."`
	FXRate        string              `json:"fx_rate,omitempty" doc:"The rate typed, if one was." example:"1.35"`
	Received      *Money              `json:"received,omitempty" doc:"What a base-currency account received for a deposit in another currency."`
	GSTCode       string              `json:"gst_code,omitempty" doc:"A part payment's GST code." example:"SR"`
	GSTCharged    bool                `json:"gst_charged" doc:"Whether it bore GST when received: a part payment of a standard-rated supply once the organisation is GST-registered." example:"true"`
	Base          *Money              `json:"base,omitempty" doc:"What it came to in the base currency, at the rate it came in; absent while it cannot be recorded."`
	GST           *Money              `json:"gst,omitempty" doc:"The output tax it bore, in the base currency."`
	Held          *Money              `json:"held,omitempty" doc:"What 2410 Customer deposits holds of it, in the base currency: its base amount less its GST, never revalued."`
	Used          *Money              `json:"used,omitempty" doc:"A posted deposit: what invoices have deducted or refunds have settled of it, in its currency."`
	Left          *Money              `json:"left,omitempty" doc:"A posted deposit: what is left of it to deduct or refund, in its currency."`
	Reference     string              `json:"reference,omitempty" doc:"The bank's reference." example:"FAST 55120"`
	Notes         string              `json:"notes,omitempty" doc:"Its notes." example:"30% deposit on the annual licence"`
	Checks        []InvoiceCheck      `json:"checks" nullable:"false" doc:"A preview only: what stops it being recorded (✖)."`
	LedgerImpact  []EntryLine         `json:"ledger_impact" nullable:"false" doc:"getCustomerDeposit only: the lines its entry wrote: debits positive."`
	RecordedBy    *Person             `json:"recorded_by,omitempty" doc:"Who recorded it."`
	RecordedAt    *time.Time          `json:"recorded_at,omitempty" doc:"When it was recorded." example:"2026-10-20T09:30:00Z"`
	ApprovalAsked string              `json:"approval_asked,omitempty" format:"uuid" doc:"getCustomerDeposit only: the pending request to void it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4dd5"`
}

// CustomerDepositPage is a page of deposits, newest date first.
type CustomerDepositPage struct {
	Items      []CustomerDeposit `json:"items" nullable:"false"`
	NextCursor string            `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjYtMTAtMzF8MDE5MiJ9"`
}

// CustomerDepositPosting is a deposit after it is recorded or voided, with the entry it made.
type CustomerDepositPosting struct {
	Deposit CustomerDeposit `json:"deposit" doc:"The deposit after the command."`
	Entry   Entry           `json:"entry" doc:"The entry the command posted: the deposit's entry, or its reversing entry."`
}

// CustomerDepositPath names a deposit in the path.
type CustomerDepositPath struct {
	DepositID string `path:"deposit_id" format:"uuid" doc:"The deposit's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fa0"`
}

type depositListInput struct {
	PageQuery
	Status string `query:"status" enum:"posted,reversed" doc:"Only deposits with this status." example:"posted"`
	From   string `query:"from" format:"date" doc:"Only deposits dated on or after this day." example:"2026-10-01"`
	To     string `query:"to" format:"date" doc:"Only deposits dated on or before this day." example:"2026-10-31"`
	Q      string `query:"q" maxLength:"100" doc:"Only deposits whose number contains this text, ignoring case." example:"DP-0001"`
}

type depositListOutput struct{ Body CustomerDepositPage }

type recordDepositInput struct {
	KeyHeader
	PreferHeader
	Body CustomerDepositDraft
}

type depositReadOutput struct {
	ETag string `header:"ETag" doc:"The deposit's version and status, for If-Match."`
	Body CustomerDeposit
}

type depositVoidInput struct {
	CustomerDepositPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body Reversal
}

type depositPostingOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The deposit's version and status, for If-Match."`
	Location string `header:"Location" doc:"The deposit's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     CustomerDepositPosting
}

func depositETag(x CustomerDeposit) string { return etag(x.Version, x.Status) }

// depositView is what showing deposits reads once per request: the base currency, the posting settings, and the
// pending requests to void a deposit.
type depositView struct {
	base     money.Currency
	settings ledger.Settings
	asked    map[string]string
}

func (a *API) depositView(ctx context.Context, u storage.UnitOfWork) (depositView, error) {
	s, err := journal.LoadSettings(ctx, u)
	if err != nil {
		return depositView{}, err
	}
	v := depositView{base: s.Base, settings: s, asked: map[string]string{}}
	requests, err := u.Approvals().List(ctx, "")
	if err != nil {
		return depositView{}, err
	}
	now := a.deps.Clock.Now()
	for _, r := range requests {
		if r.ObjectType == approval.ObjectDeposit && approval.StatusAt(requestOf(r), now) == approval.Pending {
			v.asked[r.ObjectKey] = r.ID.String()
		}
	}
	return v, nil
}

// depositOf is a deposit as the API shows it, from its document. withImpact fills its ledger impact
// (getCustomerDeposit).
func (a *API) depositOf(ctx context.Context, u storage.UnitOfWork, v depositView, doc storage.Document, withImpact bool) (CustomerDeposit, error) {
	d, err := deposit.Read(doc, v.base)
	if err != nil {
		return CustomerDeposit{}, err
	}
	out := CustomerDeposit{ID: doc.ID.String(), Number: doc.Number, Status: string(doc.Status), Version: doc.Version, Kind: d.Kind, Date: dayText(d.Date),
		MoneyAccount: &BillPaymentAccount{ID: d.AccountID.String()}, Amount: moneyOut(d.Amount), GSTCode: d.GSTCode, Reference: d.Reference, Notes: d.Notes,
		Checks: []InvoiceCheck{}, LedgerImpact: []EntryLine{}, RecordedBy: personOf(doc.PostedBy), RecordedAt: utc(doc.PostedAt)}
	if acc, ok := v.settings.Accounts[d.AccountID]; ok {
		out.MoneyAccount.Code = acc.Code
	}
	if acc, err := u.Accounts().Get(ctx, d.AccountID); err == nil {
		out.MoneyAccount.Name = acc.Name
	}
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
	if withImpact {
		out.ApprovalAsked = v.asked[out.ID]
	}
	if doc.Status != storage.DocumentDraft {
		o, err := deposit.OpenDeposit(ctx, u, doc.ID, v.settings)
		if err == nil {
			base, gst, held, used, left := moneyOut(o.Base), moneyOut(o.GST), moneyOut(o.Net), moneyOut(o.Used), moneyOut(o.Left)
			out.Base, out.GST, out.Held, out.Used, out.Left, out.GSTCharged = &base, &gst, &held, &used, &left, !o.GST.IsZero()
		}
		if withImpact {
			if e, err := u.Ledger().ByIdentity(ctx, ledger.CustomerDepositIdentity(doc.ID, doc.Version, ledger.RuleCustomerDeposit)); err == nil {
				out.LedgerImpact = entryOf(e).Lines
			}
		}
	}
	return out, a.name(ctx, u, out.RecordedBy)
}

// depositDraftOf reads a deposit from the request, with every invalid field at once.
func depositDraftOf(in CustomerDepositDraft, base money.Currency) (deposit.Draft, error) {
	d := deposit.Draft{Kind: in.Kind, GSTCode: in.GSTCode, Reference: strings.TrimSpace(in.Reference), Notes: in.Notes}
	var fields []FieldProblem
	for _, x := range []struct {
		text, field string
		to          *uuid.UUID
	}{{in.CustomerID, "body.customer_id", &d.CustomerID}, {in.MoneyAccountID, "body.money_account_id", &d.AccountID}} {
		id, err := uuid.Parse(x.text)
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: x.field, Message: "is not an ID", Fix: "Send its ID"})
		}
		*x.to = id
	}
	date, err := time.Parse(time.DateOnly, in.Date)
	if err != nil {
		fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.date", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"})
	}
	d.Date = date
	cur, err := money.Lookup(in.Currency)
	if err != nil {
		return d, invalid(append(fields, FieldProblem{Code: money.CodeCurrency, Field: "body.currency", Message: "is not a currency Fyniq knows",
			Fix: "Send an ISO 4217 code such as SGD"}))
	}
	d.Currency = cur
	amount, bad := moneyIn(in.Amount, "body.amount")
	if bad != nil {
		return d, invalid(append(fields, *bad))
	}
	if amount.Currency() != cur {
		fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.amount", Message: fmt.Sprintf("is in %s; the deposit is in %s", amount.Currency(), cur),
			Fix: "Send the amount in the deposit's currency"})
	}
	d.Amount = amount
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
	if len(fields) > 0 {
		return d, invalid(fields)
	}
	return d, nil
}

// depositProblems turns the rule's problems into the API's, naming the deposit.
func depositProblems(err error) error {
	probs, ok := err.(ledger.Problems)
	if !ok || len(probs) == 0 {
		return err
	}
	fields := make([]FieldProblem, 0, len(probs))
	for _, x := range probs {
		fields = append(fields, FieldProblem{Code: x.Code, Field: journalField(x.Field), Message: x.Message, Fix: x.Fix})
	}
	return problem(probs[0].Code, fmt.Sprintf("The deposit cannot be recorded: %d problem(s), each listed in errors.", len(probs)),
		"Correct the deposit and record it again.", fields...)
}

func registerDeposits(a *API) {
	readCodes := []string{CodeNotFound, deposit.CodeNotDeposit, deposit.CodeBadDraft}
	register(a, authz.ListCustomerDeposits, huma.Operation{
		OperationID: "listCustomerDeposits", Method: http.MethodGet, Path: "/v1/customer-deposits", Tags: []string{"Sales"},
		Summary: "List the customer deposits",
		Description: "The deposits and retainers customers paid ahead of their invoices, newest date first, a page at a time, narrowed by " +
			"status, dates and text in the number (FR-AR-09). Each has its customer, kind, money account, amount, the GST it bore, what " +
			"2410 holds of it and what is left of it. Read-only; no side effects.",
	}, append([]string{CodeCursor, CodeInvalid}, readCodes...), func(ctx context.Context, in *depositListInput) (*depositListOutput, error) {
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		afterDate, afterID, err := journalCursor(after)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := depositListOutput{Body: CustomerDepositPage{Items: []CustomerDeposit{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentCustomerDeposit, Status: storage.DocumentStatus(in.Status),
				From: in.From, To: in.To, Search: strings.TrimSpace(in.Q), AfterDate: afterDate, AfterID: afterID, Limit: in.PageSize + 1})
			if err != nil {
				return err
			}
			if len(docs) > in.PageSize {
				docs = docs[:in.PageSize]
				last := docs[len(docs)-1]
				out.Body.NextCursor = encodeCursor(last.Date + "|" + last.ID.String())
			}
			v, err := a.depositView(ctx, u)
			if err != nil {
				return err
			}
			for _, d := range docs {
				x, err := a.depositOf(ctx, u, v, d, false)
				if err != nil {
					return err
				}
				out.Body.Items = append(out.Body.Items, x)
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.GetCustomerDeposit, huma.Operation{
		OperationID: "getCustomerDeposit", Method: http.MethodGet, Path: "/v1/customer-deposits/{deposit_id}", Tags: []string{"Sales"},
		Summary: "Get a customer deposit",
		Description: "Returns a deposit with its ETag: its customer, kind, money account, rate, amount, the GST it bore, what 2410 holds of " +
			"it, what is used and left of it, the lines its entry wrote (ledger_impact), and the pending request to void it. Read-only.",
	}, readCodes, func(ctx context.Context, in *CustomerDepositPath) (*depositReadOutput, error) {
		id, err := parseID("deposit_id", in.DepositID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out CustomerDeposit
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Get(ctx, id)
			if err != nil {
				return err
			}
			v, err := a.depositView(ctx, u)
			if err != nil {
				return err
			}
			out, err = a.depositOf(ctx, u, v, doc, true)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &depositReadOutput{ETag: depositETag(out), Body: out}, nil
	})

	recordCodes := append(append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, team.CodeAction, CodeMalformed, CodeTooLarge,
		CodeMediaType, CodeInvalid, money.CodeNotDecimal, money.CodePrecision, money.CodeRange, money.CodeCurrency, money.CodeRate, deposit.CodeTooLong,
		storage.CodeRetry, storage.CodeLockTimeout}, postingCodes()...), DepositCodes...)
	register(a, authz.RecordCustomerDeposit, huma.Operation{
		OperationID: "recordCustomerDeposit", Method: http.MethodPost, Path: "/v1/customer-deposits", Tags: []string{"Sales"}, DefaultStatus: http.StatusCreated,
		Summary: "Record a customer deposit",
		Description: "Records money a customer paid ahead of an invoice, by hand, and posts it at once under customer_deposit@1 (FR-AR-09): " +
			"Dr the money account with what came in, Cr 2410 Customer deposits in the base currency at the rate it came in, and Cr 2200 " +
			"with the GST a part payment bears when received once the organisation is GST-registered (ENG-45; RND-GST-INCL); a security " +
			"deposit bears none until it is used. The final invoice deducts it. The money has come in already, so no approval is asked. " +
			"Side effects: the deposit, its number (DP-), the entry, audit events. Every problem comes back together. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview posts nothing and shows the entry.",
	}, recordCodes, func(ctx context.Context, in *recordDepositInput) (*depositPostingOutput, error) {
		p, _ := authn.From(ctx)
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (CustomerDepositPosting, error) {
			if err := checkConflicts(ctx, u, p, "Posting"); err != nil {
				return CustomerDepositPosting{}, err
			}
			v, err := a.depositView(ctx, u)
			if err != nil {
				return CustomerDepositPosting{}, err
			}
			d, err := depositDraftOf(in.Body, v.base)
			if err != nil {
				return CustomerDepositPosting{}, err
			}
			r, err := a.deposits.Record(ctx, p.Organisation, d)
			if err != nil {
				return CustomerDepositPosting{}, depositProblems(err)
			}
			doc, err := u.Documents().Get(ctx, r.Deposit.ID)
			if err != nil {
				return CustomerDepositPosting{}, err
			}
			out, err := a.depositOf(ctx, u, v, doc, true)
			return CustomerDepositPosting{Deposit: out, Entry: entryOf(r.Entry)}, err
		})
		if err != nil {
			return nil, err
		}
		if w.Previewed {
			return &depositPostingOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
		}
		return &depositPostingOutput{Status: w.Status, ETag: depositETag(w.Body.Deposit), Location: "/v1/customer-deposits/" + w.Body.Deposit.ID,
			Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	voidCodes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, team.CodeAction,
		CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeDepositVoidApproval, deposit.CodeNotPosted, deposit.CodeVoidDate, deposit.CodeUsed},
		append(postingCodes(), readCodes...)...)
	register(a, authz.VoidCustomerDeposit, huma.Operation{
		OperationID: "voidCustomerDeposit", Method: http.MethodPost, Path: "/v1/customer-deposits/{deposit_id}/void", Tags: []string{"Sales"},
		Summary: "Void a customer deposit",
		Description: "Voids a deposit with a reversing entry on the date given, with a reason (FR-GL-12). Nothing may use it: an invoice " +
			"that deducts it or a refund of it is voided first (DEPOSIT-08). The date is not before the deposit's (DEPOSIT-07). In Founder " +
			"mode voiding needs a second person (FM-1): it is refused (DEPOSIT-10), and requestCustomerDepositVoid asks for the approval. " +
			"Needs If-Match with the deposit's ETag. Idempotent with Idempotency-Key.",
	}, voidCodes, func(ctx context.Context, in *depositVoidInput) (*depositPostingOutput, error) {
		id, err := parseID("deposit_id", in.DepositID)
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
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (CustomerDepositPosting, error) {
			doc, err := u.Documents().Lock(ctx, id)
			if err != nil {
				return CustomerDepositPosting{}, err
			}
			v, err := a.depositView(ctx, u)
			if err != nil {
				return CustomerDepositPosting{}, err
			}
			before, err := a.depositOf(ctx, u, v, doc, false)
			if err != nil {
				return CustomerDepositPosting{}, err
			}
			if err := checkIfMatch(in.IfMatch, depositETag(before)); err != nil {
				return CustomerDepositPosting{}, err
			}
			if err := checkConflicts(ctx, u, p, "Posting"); err != nil {
				return CustomerDepositPosting{}, err
			}
			if mode, _, _, err := teamModeOf(ctx, u); err != nil {
				return CustomerDepositPosting{}, err
			} else if mode == team.Founder {
				return CustomerDepositPosting{}, problem(CodeDepositVoidApproval, "In Founder mode, voiding a recorded deposit needs a second person (FM-1).",
					"Ask for another person's approval to void it")
			}
			r, err := a.deposits.Void(ctx, p.Organisation, id, date, strings.TrimSpace(in.Body.Reason))
			if err != nil {
				return CustomerDepositPosting{}, err
			}
			x, err := a.depositOf(ctx, u, v, r.Deposit, true)
			return CustomerDepositPosting{Deposit: x, Entry: entryOf(r.Entry)}, err
		})
		if err != nil {
			return nil, err
		}
		if w.Previewed {
			return &depositPostingOutput{Status: http.StatusOK, Applied: applied(w), Body: w.Body}, nil
		}
		return &depositPostingOutput{Status: http.StatusOK, ETag: depositETag(w.Body.Deposit), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	requestCodes := []string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		CodeSessionRequired, CodeStepUpRequired, approval.CodePending, storage.CodeApproval, deposit.CodeNotDeposit, deposit.CodeNotPosted,
		deposit.CodeBadDraft, deposit.CodeVoidDate, storage.CodeRetry, storage.CodeLockTimeout, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}
	register(a, authz.RequestCustomerDepositVoid, huma.Operation{
		OperationID: "requestCustomerDepositVoid", Method: http.MethodPost, Path: "/v1/customer-deposits/{deposit_id}/void-approval", Tags: []string{"Sales"},
		Summary: "Ask for approval to void a customer deposit", DefaultStatus: http.StatusAccepted,
		Description: "Asks for another person's approval to void a deposit on the date given, with a reason (FR-GL-12, FM-1): what " +
			"voidCustomerDeposit asks for in Founder mode (DEPOSIT-10). The answer is 202 with an approval request, which a person who holds " +
			"A on \"Invoices, credit notes\" and did not ask approves, stepped up; on the approval the void posts. Needs a person signed in " +
			"who passed step-up, and If-Match with the deposit's ETag. Idempotent with Idempotency-Key.",
	}, requestCodes, func(ctx context.Context, in *depositVoidInput) (*billRequestOutput, error) {
		id, err := parseID("deposit_id", in.DepositID)
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
			doc, err := u.Documents().Lock(ctx, id)
			if err != nil {
				return Approval{}, err
			}
			v, err := a.depositView(ctx, u)
			if err != nil {
				return Approval{}, err
			}
			x, err := a.depositOf(ctx, u, v, doc, false)
			if err != nil {
				return Approval{}, err
			}
			if err := checkIfMatch(in.IfMatch, depositETag(x)); err != nil {
				return Approval{}, err
			}
			if doc.Status != storage.DocumentPosted {
				return Approval{}, problem(deposit.CodeNotPosted, fmt.Sprintf("The deposit is %s.", doc.Status), "Ask for approval to void a posted deposit")
			}
			if in.Body.Date < x.Date {
				return Approval{}, problem(deposit.CodeVoidDate, fmt.Sprintf("The void is dated %s, before the deposit's %s.", in.Body.Date, x.Date),
					"Date the void on or after the deposit")
			}
			name := cmpText(x.Number, "the deposit")
			if x.Customer != nil && x.Customer.Name != "" {
				name += " from " + x.Customer.Name
			}
			return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectDeposit, ObjectKey: doc.ID.String(), Operation: approval.OpDepositVoid,
				Summary: fmt.Sprintf("Void %s, %s %s, on %s: %s", name, x.Amount.Amount, x.Amount.Currency, in.Body.Date, reason),
				Execute: map[string]any{"deposit_id": doc.ID.String(), "date": in.Body.Date, "reason": reason},
				Diff:    []ApprovalDiff{{Field: "status", Before: "posted", After: "reversed"}, {Field: "void_date", After: in.Body.Date}}})
		})
		if err != nil {
			return nil, err
		}
		return &billRequestOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})
}
