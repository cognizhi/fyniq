package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/customercredit"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/team"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------ refunds to customers (FR-AR-04, FR-AR-18, FM-1; p1-sales)
//
// Money paid back to a customer against a credit with it (an issued credit note, or what a receipt left unapplied) is
// recorded by hand and posted at once by customer_refund@1: Dr 1100 at the credit's rate, Cr the money account, and the
// realised FX to 7030. Voiding it reopens the credit; in Founder mode a void needs a second person (FM-1). As a refund
// from a supplier is recorded.

// CodeCustomerRefundVoidApproval is the API's code for a void that needs a second person in Founder mode (FM-1).
const CodeCustomerRefundVoidApproval = "APPLY-11"

// CustomerRefundCodes are the codes the rule and the refund's own checks answer.
var CustomerRefundCodes = []string{customercredit.CodeCredit, customercredit.CodeTooMuch, ledger.CodeNotMoneyAccount, ledger.CodePaymentAmount,
	ledger.CodePaidAmount, ledger.CodeSystemRole, ledger.CodeLineCurrency}

// CustomerRefundApprovalCodes are the codes an approval of a refund's void may answer.
func CustomerRefundApprovalCodes() []string {
	return []string{customercredit.CodeNotRefund, customercredit.CodeNotPosted, customercredit.CodeVoidDate, customercredit.CodeBadDraft}
}

// CustomerRefundDraft is a refund as recorded.
type CustomerRefundDraft struct {
	CreditID       string `json:"credit_id" format:"uuid" doc:"The credit refunded: an issued credit note, or a customer receipt with an amount unapplied." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f70"`
	Date           string `json:"date" format:"date" doc:"The day the money left." example:"2026-10-22"`
	MoneyAccountID string `json:"money_account_id" format:"uuid" doc:"The bank, card or wallet account it left: an active money account in the credit's currency or in the base currency." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d10"`
	FXRate         string `json:"fx_rate,omitempty" doc:"For a credit in another currency refunded from an account in that currency: base currency per one unit, as on the bank's advice. Left out, the cascade's rate for the date (ENG-40)." example:"1.34"`
	Paid           *Money `json:"paid,omitempty" doc:"For a credit in another currency refunded from a base-currency account: what the account paid, in the base currency. Required then, and only then (LEDGER-27)."`
	Reference      string `json:"reference,omitempty" maxLength:"140" doc:"The bank's reference for it." example:"FAST 77120"`
	Amount         Money  `json:"amount" doc:"What the refund settles of the credit, in its currency: at most what is left of it (APPLY-05)."`
}

// CustomerRefund is money paid back to a customer against a credit.
type CustomerRefund struct {
	ID            string                  `json:"id" format:"uuid" doc:"The refund's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f90"`
	Number        string                  `json:"number,omitempty" doc:"Its number from the customer refund sequence (FR-ORG-04)." example:"CR-0001"`
	Status        string                  `json:"status" enum:"posted,reversed" doc:"posted once recorded; reversed by a void." example:"posted"`
	Version       int                     `json:"version" doc:"The version of its content; with the status it makes the ETag." example:"1"`
	Credit        CustomerAppliedDocument `json:"credit" doc:"The credit it settles."`
	Date          string                  `json:"date" format:"date" doc:"The day the money left." example:"2026-10-22"`
	MoneyAccount  *BillPaymentAccount     `json:"money_account,omitempty" doc:"The account it left."`
	Amount        Money                   `json:"amount" doc:"What it settled of the credit, in its currency."`
	FXRate        string                  `json:"fx_rate,omitempty" doc:"The rate typed, if one was." example:"1.34"`
	Paid          *Money                  `json:"paid,omitempty" doc:"What a base-currency account paid for a credit in another currency."`
	Reference     string                  `json:"reference,omitempty" doc:"The bank's reference." example:"FAST 77120"`
	Cleared       Money                   `json:"cleared" doc:"The credit's carrying amount it cleared, in the base currency."`
	RealisedFX    Money                   `json:"realised_fx" doc:"The realised gain (positive) or loss (negative): what the credit carried less what was paid (ENG-41)."`
	LedgerImpact  []EntryLine             `json:"ledger_impact" nullable:"false" doc:"The lines its entry wrote."`
	RecordedBy    *Person                 `json:"recorded_by,omitempty" doc:"Who recorded it."`
	RecordedAt    *time.Time              `json:"recorded_at,omitempty" doc:"When it was recorded." example:"2026-10-22T09:30:00Z"`
	ApprovalAsked string                  `json:"approval_asked,omitempty" format:"uuid" doc:"The pending request to void it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4dd4"`
}

// CustomerRefundPosting is a refund after it is recorded or voided, with the entry it posted.
type CustomerRefundPosting struct {
	Refund CustomerRefund `json:"refund" doc:"The refund after the command."`
	Entry  Entry          `json:"entry" doc:"The entry the command posted: the refund's, or its reversal."`
}

// CustomerRefundPath names a refund in the path.
type CustomerRefundPath struct {
	RefundID string `path:"refund_id" format:"uuid" doc:"The refund's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f90"`
}

type recordCustomerRefundInput struct {
	KeyHeader
	PreferHeader
	Body CustomerRefundDraft
}

type customerRefundReadOutput struct {
	ETag string `header:"ETag" doc:"The refund's version and status, for If-Match."`
	Body CustomerRefund
}

type customerRefundVoidInput struct {
	CustomerRefundPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body Reversal
}

type customerRefundPostingOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The refund's version and status, for If-Match."`
	Location string `header:"Location" doc:"The refund's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     CustomerRefundPosting
}

func customerRefundETag(x CustomerRefund) string { return etag(x.Version, x.Status) }

// customerRefundOf is a refund as the API shows it, from its document.
func (a *API) customerRefundOf(ctx context.Context, u storage.UnitOfWork, doc storage.Document) (CustomerRefund, error) {
	o, err := u.Organisation(ctx)
	if err != nil {
		return CustomerRefund{}, err
	}
	base, err := money.Lookup(o.BaseCurrency)
	if err != nil {
		return CustomerRefund{}, err
	}
	d, err := customercredit.ReadRefund(doc, base)
	if err != nil {
		return CustomerRefund{}, err
	}
	zero := Money{Amount: money.Zero(base).String(), Currency: base.Code}
	out := CustomerRefund{ID: doc.ID.String(), Number: doc.Number, Status: string(doc.Status), Version: doc.Version, Credit: customerAppliedDocument(ctx, u, d.CreditID),
		Date: dayText(d.Date), MoneyAccount: &BillPaymentAccount{ID: d.AccountID.String()}, Amount: moneyOut(d.Amount), Reference: d.Reference,
		Cleared: zero, RealisedFX: zero, LedgerImpact: []EntryLine{}, RecordedBy: personOf(doc.PostedBy), RecordedAt: utc(doc.PostedAt)}
	if acc, err := u.Accounts().Get(ctx, d.AccountID); err == nil {
		out.MoneyAccount.Code, out.MoneyAccount.Name = acc.Code, acc.Name
	}
	if d.Rate != nil {
		out.FXRate = d.Rate.String()
	}
	if d.Paid != nil {
		p := moneyOut(*d.Paid)
		out.Paid = &p
	}
	allocs, err := u.Allocations().OfSource(ctx, doc.ID)
	if err != nil {
		return CustomerRefund{}, err
	}
	for _, x := range allocs {
		if x.ReversesID == nil && x.SourceVersion == doc.Version {
			out.Cleared, out.RealisedFX = storedOut(x.BaseAmount, x.BaseCurrency), storedOut(x.RealisedFX, x.BaseCurrency)
		}
	}
	if e, err := u.Ledger().ByIdentity(ctx, ledger.CustomerRefundIdentity(doc.ID, doc.Version, ledger.RuleCustomerRefund)); err == nil {
		out.LedgerImpact = entryOf(e).Lines
	}
	now := a.deps.Clock.Now()
	requests, err := u.Approvals().List(ctx, "")
	if err != nil {
		return CustomerRefund{}, err
	}
	for _, r := range requests {
		if r.ObjectType == approval.ObjectCustomerRefund && r.ObjectKey == out.ID && approval.StatusAt(requestOf(r), now) == approval.Pending {
			out.ApprovalAsked = r.ID.String()
		}
	}
	return out, a.name(ctx, u, out.RecordedBy)
}

// customerRefundDraftOf reads a refund from the request, with every invalid field at once.
func customerRefundDraftOf(in CustomerRefundDraft, base money.Currency) (customercredit.RefundDraft, error) {
	var d customercredit.RefundDraft
	var fields []FieldProblem
	for _, x := range []struct {
		text, field string
		to          *uuid.UUID
	}{{in.CreditID, "body.credit_id", &d.CreditID}, {in.MoneyAccountID, "body.money_account_id", &d.AccountID}} {
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
	d.Date, d.Reference = date, strings.TrimSpace(in.Reference)
	amount, bad := moneyIn(in.Amount, "body.amount")
	if bad != nil {
		return d, invalid(append(fields, *bad))
	}
	d.Amount = amount
	if in.FXRate != "" {
		r, err := money.ParseRate(in.FXRate, amount.Currency(), base, RateSourceDocument)
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
	if len(fields) > 0 {
		return d, invalid(fields)
	}
	return d, nil
}

// customerRefundProblems turns the rule's problems into the API's, naming the refund.
func customerRefundProblems(err error) error {
	var lp ledger.Problems
	if !errors.As(err, &lp) || len(lp) == 0 {
		return err
	}
	fields := make([]FieldProblem, 0, len(lp))
	for _, x := range lp {
		fields = append(fields, FieldProblem{Code: x.Code, Field: x.Field, Message: x.Message, Fix: x.Fix})
	}
	return problem(lp[0].Code, fmt.Sprintf("The refund cannot be recorded: %d problem(s), each listed in errors.", len(lp)),
		"Correct the refund and record it again.", fields...)
}

func registerCustomerRefunds(a *API) {
	readCodes := []string{CodeNotFound, customercredit.CodeNotRefund, customercredit.CodeBadDraft}
	register(a, authz.GetCustomerRefund, huma.Operation{
		OperationID: "getCustomerRefund", Method: http.MethodGet, Path: "/v1/customer-refunds/{refund_id}", Tags: []string{"Sales"},
		Summary: "Get a refund to a customer",
		Description: "Returns a refund to a customer with its ETag: the credit it settles, the money account, the amount, the carrying amount " +
			"it cleared, the realised FX (ENG-41), the lines its entry wrote, and the pending request to void it. Read-only.",
	}, readCodes, func(ctx context.Context, in *CustomerRefundPath) (*customerRefundReadOutput, error) {
		id, err := parseID("refund_id", in.RefundID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out CustomerRefund
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Get(ctx, id)
			if err != nil {
				return err
			}
			out, err = a.customerRefundOf(ctx, u, doc)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &customerRefundReadOutput{ETag: customerRefundETag(out), Body: out}, nil
	})

	recordCodes := append(append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, team.CodeAction, CodeMalformed, CodeTooLarge,
		CodeMediaType, CodeInvalid, money.CodeNotDecimal, money.CodePrecision, money.CodeRange, money.CodeCurrency, money.CodeRate, customercredit.CodeBadDraft,
		storage.CodeAllocation}, postingCodes()...), CustomerRefundCodes...)
	register(a, authz.RecordCustomerRefund, huma.Operation{
		OperationID: "recordCustomerRefund", Method: http.MethodPost, Path: "/v1/customer-refunds", Tags: []string{"Sales"}, DefaultStatus: http.StatusCreated,
		Summary: "Record a refund to a customer",
		Description: "Records money paid back to a customer against a credit with it (an issued credit note, or what a customer receipt left " +
			"unapplied), by hand, and posts it at once under customer_refund@1 (FR-AR-04, FR-AR-18): Dr 1100 with the amount at the credit's " +
			"rate, Cr the money account with what left it, and the realised FX to 7030 (ENG-41). It settles at most what is left of the credit " +
			"(APPLY-05). Side effects: the refund, its number (CR-), the entry, an allocation, audit events. Every problem comes back together. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview posts nothing.",
	}, recordCodes, func(ctx context.Context, in *recordCustomerRefundInput) (*customerRefundPostingOutput, error) {
		p, _ := authn.From(ctx)
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (CustomerRefundPosting, error) {
			if err := checkConflicts(ctx, u, p, "Posting"); err != nil {
				return CustomerRefundPosting{}, err
			}
			base, err := baseOf(ctx, u)
			if err != nil {
				return CustomerRefundPosting{}, err
			}
			d, err := customerRefundDraftOf(in.Body, base)
			if err != nil {
				return CustomerRefundPosting{}, err
			}
			r, err := a.customerRefunds.Record(ctx, p.Organisation, d)
			if err != nil {
				return CustomerRefundPosting{}, customerRefundProblems(err)
			}
			doc, err := u.Documents().Get(ctx, r.Refund.ID)
			if err != nil {
				return CustomerRefundPosting{}, err
			}
			x, err := a.customerRefundOf(ctx, u, doc)
			return CustomerRefundPosting{Refund: x, Entry: entryOf(r.Entry)}, err
		})
		if err != nil {
			return nil, err
		}
		if w.Previewed {
			return &customerRefundPostingOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
		}
		return &customerRefundPostingOutput{Status: w.Status, ETag: customerRefundETag(w.Body.Refund), Location: "/v1/customer-refunds/" + w.Body.Refund.ID,
			Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	voidCodes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, team.CodeAction,
		CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeCustomerRefundVoidApproval, customercredit.CodeNotPosted, customercredit.CodeVoidDate,
		storage.CodeAllocation}, append(postingCodes(), readCodes...)...)
	register(a, authz.VoidCustomerRefund, huma.Operation{
		OperationID: "voidCustomerRefund", Method: http.MethodPost, Path: "/v1/customer-refunds/{refund_id}/void", Tags: []string{"Sales"},
		Summary: "Void a refund to a customer",
		Description: "Voids a refund with a reversing entry on the date given, with a reason, and a reversing allocation, which reopens the " +
			"credit (FR-GL-12, FR-AR-18). The date is not before the refund's (APPLY-07). In Founder mode voiding needs a second person " +
			"(FM-1): it is refused (APPLY-11), and requestCustomerRefundVoid asks for the approval. Needs If-Match with the refund's ETag. " +
			"Idempotent with Idempotency-Key.",
	}, voidCodes, func(ctx context.Context, in *customerRefundVoidInput) (*customerRefundPostingOutput, error) {
		id, err := parseID("refund_id", in.RefundID)
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
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (CustomerRefundPosting, error) {
			doc, err := u.Documents().Lock(ctx, id)
			if err != nil {
				return CustomerRefundPosting{}, err
			}
			before, err := a.customerRefundOf(ctx, u, doc)
			if err != nil {
				return CustomerRefundPosting{}, err
			}
			if err := checkIfMatch(in.IfMatch, customerRefundETag(before)); err != nil {
				return CustomerRefundPosting{}, err
			}
			if err := checkConflicts(ctx, u, p, "Posting"); err != nil {
				return CustomerRefundPosting{}, err
			}
			if mode, _, _, err := teamModeOf(ctx, u); err != nil {
				return CustomerRefundPosting{}, err
			} else if mode == team.Founder {
				return CustomerRefundPosting{}, problem(CodeCustomerRefundVoidApproval, "In Founder mode, voiding a refund to a customer needs a second person (FM-1).",
					"Ask for another person's approval to void it")
			}
			r, err := a.customerRefunds.Void(ctx, p.Organisation, id, date, strings.TrimSpace(in.Body.Reason))
			if err != nil {
				return CustomerRefundPosting{}, err
			}
			x, err := a.customerRefundOf(ctx, u, r.Refund)
			return CustomerRefundPosting{Refund: x, Entry: entryOf(r.Entry)}, err
		})
		if err != nil {
			return nil, err
		}
		if w.Previewed {
			return &customerRefundPostingOutput{Status: http.StatusOK, Applied: applied(w), Body: w.Body}, nil
		}
		return &customerRefundPostingOutput{Status: http.StatusOK, ETag: customerRefundETag(w.Body.Refund), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	requestCodes := []string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		CodeSessionRequired, CodeStepUpRequired, approval.CodePending, storage.CodeApproval, customercredit.CodeNotRefund, customercredit.CodeNotPosted,
		customercredit.CodeBadDraft, customercredit.CodeVoidDate, storage.CodeRetry, storage.CodeLockTimeout, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}
	register(a, authz.RequestCustomerRefundVoid, huma.Operation{
		OperationID: "requestCustomerRefundVoid", Method: http.MethodPost, Path: "/v1/customer-refunds/{refund_id}/void-approval", Tags: []string{"Sales"},
		Summary: "Ask for approval to void a refund to a customer", DefaultStatus: http.StatusAccepted,
		Description: "Asks for another person's approval to void a refund on the date given, with a reason (FR-GL-12, FM-1): what " +
			"voidCustomerRefund asks for in Founder mode (APPLY-11). The answer is 202 with an approval request, which a person who holds A " +
			"on \"Invoices, credit notes\" and did not ask approves, stepped up; on the approval the void posts and the credit reopens. Needs " +
			"a person signed in who passed step-up, and If-Match with the refund's ETag. Idempotent with Idempotency-Key.",
	}, requestCodes, func(ctx context.Context, in *customerRefundVoidInput) (*billRequestOutput, error) {
		id, err := parseID("refund_id", in.RefundID)
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
			x, err := a.customerRefundOf(ctx, u, doc)
			if err != nil {
				return Approval{}, err
			}
			if err := checkIfMatch(in.IfMatch, customerRefundETag(x)); err != nil {
				return Approval{}, err
			}
			if doc.Status != storage.DocumentPosted {
				return Approval{}, problem(customercredit.CodeNotPosted, fmt.Sprintf("The refund is %s.", doc.Status), "Ask for approval to void a posted refund")
			}
			if in.Body.Date < x.Date {
				return Approval{}, problem(customercredit.CodeVoidDate, fmt.Sprintf("The void is dated %s, before the refund's %s.", in.Body.Date, x.Date),
					"Date the void on or after the refund")
			}
			return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectCustomerRefund, ObjectKey: doc.ID.String(), Operation: approval.OpCustomerRefundVoid,
				Summary: fmt.Sprintf("Void %s of %s, %s %s, on %s: %s", cmpText(x.Number, "the refund"), cmpText(x.Credit.Number, "a credit"), x.Amount.Amount,
					x.Amount.Currency, in.Body.Date, reason),
				Execute: map[string]any{"refund_id": doc.ID.String(), "date": in.Body.Date, "reason": reason},
				Diff:    []ApprovalDiff{{Field: "status", Before: "posted", After: "reversed"}, {Field: "void_date", After: in.Body.Date}}})
		})
		if err != nil {
			return nil, err
		}
		return &billRequestOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})
}
