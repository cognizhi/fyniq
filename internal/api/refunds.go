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
	"github.com/cognizhi/fyniq/internal/domain/credits"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/team"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// --------------------------------------------------------------- refunds from suppliers (FR-AP-03, FR-AR-18, FM-1)
//
// Money a supplier pays back against a credit with it (a posted vendor credit, or what a payment left unapplied) is
// recorded by hand and posted at once by supplier_refund@1: Dr the money account, Cr 2010 at the credit's rate, and the
// realised FX to 7030. Voiding it reopens the credit; in Founder mode a void needs a second person (FM-1).

// CodeRefundVoidApproval is the API's code for a void that needs a second person in Founder mode (FM-1).
const CodeRefundVoidApproval = "CREDIT-11"

// RefundCodes are the codes the rule and the refund's own checks answer.
var RefundCodes = []string{credits.CodeCredit, credits.CodeTooMuch, ledger.CodeNotMoneyAccount, ledger.CodePaymentAmount, ledger.CodePaidAmount,
	ledger.CodeSystemRole, ledger.CodeLineCurrency}

// RefundApprovalCodes are the codes an approval of a refund's void may answer.
func RefundApprovalCodes() []string {
	return []string{credits.CodeNotRefund, credits.CodeNotPosted, credits.CodeVoidDate, credits.CodeBadDraft}
}

// SupplierRefundDraft is a refund as recorded.
type SupplierRefundDraft struct {
	CreditID       string `json:"credit_id" format:"uuid" doc:"The credit refunded: a posted vendor credit, or a payment of bills with an amount unapplied." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e50"`
	Date           string `json:"date" format:"date" doc:"The day the money came in." example:"2026-10-22"`
	MoneyAccountID string `json:"money_account_id" format:"uuid" doc:"The bank, card or wallet account it came into: an active money account in the credit's currency or in the base currency." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d10"`
	FXRate         string `json:"fx_rate,omitempty" doc:"For a credit in another currency refunded into an account in that currency: base currency per one unit, as on the bank's advice. Left out, the cascade's rate for the date (ENG-40)." example:"1.34"`
	Received       *Money `json:"received,omitempty" doc:"For a credit in another currency refunded into a base-currency account: what the account received, in the base currency. Required then, and only then (LEDGER-27)."`
	Reference      string `json:"reference,omitempty" maxLength:"140" doc:"The bank's reference for it." example:"FAST 99102"`
	Amount         Money  `json:"amount" doc:"What the refund settles of the credit, in its currency: at most what is left of it (CREDIT-05)."`
}

// SupplierRefund is money a supplier paid back against a credit.
type SupplierRefund struct {
	ID            string              `json:"id" format:"uuid" doc:"The refund's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e70"`
	Number        string              `json:"number,omitempty" doc:"Its number from the refund sequence (FR-ORG-04)." example:"RF-0001"`
	Status        string              `json:"status" enum:"posted,reversed" doc:"posted once recorded; reversed by a void." example:"posted"`
	Version       int                 `json:"version" doc:"The version of its content; with the status it makes the ETag." example:"1"`
	Credit        AppliedDocument     `json:"credit" doc:"The credit it settles."`
	Date          string              `json:"date" format:"date" doc:"The day the money came in." example:"2026-10-22"`
	MoneyAccount  *BillPaymentAccount `json:"money_account,omitempty" doc:"The account it came into."`
	Amount        Money               `json:"amount" doc:"What it settled of the credit, in its currency."`
	FXRate        string              `json:"fx_rate,omitempty" doc:"The rate typed, if one was." example:"1.34"`
	Received      *Money              `json:"received,omitempty" doc:"What a base-currency account received for a credit in another currency."`
	Reference     string              `json:"reference,omitempty" doc:"The bank's reference." example:"FAST 99102"`
	Cleared       Money               `json:"cleared" doc:"The credit's carrying amount it cleared, in the base currency."`
	RealisedFX    Money               `json:"realised_fx" doc:"The realised gain (positive) or loss (negative): what came in less what the credit carried (ENG-41)."`
	LedgerImpact  []EntryLine         `json:"ledger_impact" nullable:"false" doc:"The lines its entry wrote."`
	RecordedBy    *Person             `json:"recorded_by,omitempty" doc:"Who recorded it."`
	RecordedAt    *time.Time          `json:"recorded_at,omitempty" doc:"When it was recorded." example:"2026-10-22T09:30:00Z"`
	ApprovalAsked string              `json:"approval_asked,omitempty" format:"uuid" doc:"The pending request to void it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4dd1"`
}

// SupplierRefundPosting is a refund after it is recorded or voided, with the entry it posted.
type SupplierRefundPosting struct {
	Refund SupplierRefund `json:"refund" doc:"The refund after the command."`
	Entry  Entry          `json:"entry" doc:"The entry the command posted: the refund's, or its reversal."`
}

// SupplierRefundPath names a refund in the path.
type SupplierRefundPath struct {
	RefundID string `path:"refund_id" format:"uuid" doc:"The refund's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e70"`
}

type recordRefundInput struct {
	KeyHeader
	PreferHeader
	Body SupplierRefundDraft
}

type refundReadOutput struct {
	ETag string `header:"ETag" doc:"The refund's version and status, for If-Match."`
	Body SupplierRefund
}

type refundVoidInput struct {
	SupplierRefundPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body Reversal
}

type refundPostingOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The refund's version and status, for If-Match."`
	Location string `header:"Location" doc:"The refund's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     SupplierRefundPosting
}

func refundETag(x SupplierRefund) string { return etag(x.Version, x.Status) }

// refundOf is a refund as the API shows it, from its document.
func (a *API) refundOf(ctx context.Context, u storage.UnitOfWork, doc storage.Document) (SupplierRefund, error) {
	o, err := u.Organisation(ctx)
	if err != nil {
		return SupplierRefund{}, err
	}
	base, err := money.Lookup(o.BaseCurrency)
	if err != nil {
		return SupplierRefund{}, err
	}
	d, err := credits.ReadRefund(doc, base)
	if err != nil {
		return SupplierRefund{}, err
	}
	zero := Money{Amount: money.Zero(base).String(), Currency: base.Code}
	out := SupplierRefund{ID: doc.ID.String(), Number: doc.Number, Status: string(doc.Status), Version: doc.Version, Credit: appliedDocument(ctx, u, d.CreditID),
		Date: dayText(d.Date), MoneyAccount: &BillPaymentAccount{ID: d.AccountID.String()}, Amount: moneyOut(d.Amount), Reference: d.Reference,
		Cleared: zero, RealisedFX: zero, LedgerImpact: []EntryLine{}, RecordedBy: personOf(doc.PostedBy), RecordedAt: utc(doc.PostedAt)}
	if acc, err := u.Accounts().Get(ctx, d.AccountID); err == nil {
		out.MoneyAccount.Code, out.MoneyAccount.Name = acc.Code, acc.Name
	}
	if d.Rate != nil {
		out.FXRate = d.Rate.String()
	}
	if d.Received != nil {
		r := moneyOut(*d.Received)
		out.Received = &r
	}
	allocs, err := u.Allocations().OfSource(ctx, doc.ID)
	if err != nil {
		return SupplierRefund{}, err
	}
	for _, x := range allocs {
		if x.ReversesID == nil && x.SourceVersion == doc.Version {
			out.Cleared, out.RealisedFX = storedOut(x.BaseAmount, x.BaseCurrency), storedOut(x.RealisedFX, x.BaseCurrency)
		}
	}
	if e, err := u.Ledger().ByIdentity(ctx, ledger.SupplierRefundIdentity(doc.ID, doc.Version, ledger.RuleSupplierRefund)); err == nil {
		out.LedgerImpact = entryOf(e).Lines
	}
	now := a.deps.Clock.Now()
	requests, err := u.Approvals().List(ctx, "")
	if err != nil {
		return SupplierRefund{}, err
	}
	for _, r := range requests {
		if r.ObjectType == approval.ObjectRefund && r.ObjectKey == out.ID && approval.StatusAt(requestOf(r), now) == approval.Pending {
			out.ApprovalAsked = r.ID.String()
		}
	}
	return out, a.name(ctx, u, out.RecordedBy)
}

// refundDraftOf reads a refund from the request, with every invalid field at once.
func refundDraftOf(in SupplierRefundDraft, base money.Currency) (credits.RefundDraft, error) {
	var d credits.RefundDraft
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

func registerSupplierRefunds(a *API) {
	readCodes := []string{CodeNotFound, credits.CodeNotRefund, credits.CodeBadDraft}
	register(a, authz.GetSupplierRefund, huma.Operation{
		OperationID: "getSupplierRefund", Method: http.MethodGet, Path: "/v1/supplier-refunds/{refund_id}", Tags: []string{"Purchases"},
		Summary:     "Get a refund from a supplier",
		Description: "Returns a refund with its ETag: the credit it settles, the money account, the amount, the carrying amount it cleared, the realised FX (ENG-41), the lines its entry wrote, and the pending request to void it. Read-only.",
	}, readCodes, func(ctx context.Context, in *SupplierRefundPath) (*refundReadOutput, error) {
		id, err := parseID("refund_id", in.RefundID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out SupplierRefund
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Get(ctx, id)
			if err != nil {
				return err
			}
			out, err = a.refundOf(ctx, u, doc)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &refundReadOutput{ETag: refundETag(out), Body: out}, nil
	})

	recordCodes := append(append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, team.CodeAction, CodeMalformed, CodeTooLarge,
		CodeMediaType, CodeInvalid, money.CodeNotDecimal, money.CodePrecision, money.CodeRange, money.CodeCurrency, money.CodeRate, credits.CodeBadDraft,
		storage.CodeAllocation}, postingCodes()...), RefundCodes...)
	register(a, authz.RecordSupplierRefund, huma.Operation{
		OperationID: "recordSupplierRefund", Method: http.MethodPost, Path: "/v1/supplier-refunds", Tags: []string{"Purchases"}, DefaultStatus: http.StatusCreated,
		Summary: "Record a refund from a supplier",
		Description: "Records money a supplier paid back against a credit with it (a posted vendor credit, or what a payment of bills left " +
			"unapplied), by hand, and posts it at once under supplier_refund@1 (FR-AP-03): Dr the money account with what came in, Cr 2010 " +
			"with the amount at the credit's rate, and the realised FX to 7030 (ENG-41). It settles at most what is left of the credit " +
			"(CREDIT-05). Side effects: the refund, its number (RF-), the entry, an allocation, audit events. Every problem comes back " +
			"together. Idempotent with Idempotency-Key; Prefer: return=preview posts nothing.",
	}, recordCodes, func(ctx context.Context, in *recordRefundInput) (*refundPostingOutput, error) {
		p, _ := authn.From(ctx)
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (SupplierRefundPosting, error) {
			if err := checkConflicts(ctx, u, p, "Posting"); err != nil {
				return SupplierRefundPosting{}, err
			}
			o, err := u.Organisation(ctx)
			if err != nil {
				return SupplierRefundPosting{}, err
			}
			base, err := money.Lookup(o.BaseCurrency)
			if err != nil {
				return SupplierRefundPosting{}, err
			}
			d, err := refundDraftOf(in.Body, base)
			if err != nil {
				return SupplierRefundPosting{}, err
			}
			r, err := a.refunds.Record(ctx, p.Organisation, d)
			if err != nil {
				return SupplierRefundPosting{}, paymentProblems(err)
			}
			doc, err := u.Documents().Get(ctx, r.Refund.ID)
			if err != nil {
				return SupplierRefundPosting{}, err
			}
			x, err := a.refundOf(ctx, u, doc)
			return SupplierRefundPosting{Refund: x, Entry: entryOf(r.Entry)}, err
		})
		if err != nil {
			return nil, err
		}
		if w.Previewed {
			return &refundPostingOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
		}
		return &refundPostingOutput{Status: w.Status, ETag: refundETag(w.Body.Refund), Location: "/v1/supplier-refunds/" + w.Body.Refund.ID,
			Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	voidCodes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, team.CodeAction,
		CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeRefundVoidApproval, credits.CodeNotPosted, credits.CodeVoidDate, storage.CodeAllocation},
		append(postingCodes(), readCodes...)...)
	register(a, authz.VoidSupplierRefund, huma.Operation{
		OperationID: "voidSupplierRefund", Method: http.MethodPost, Path: "/v1/supplier-refunds/{refund_id}/void", Tags: []string{"Purchases"},
		Summary: "Void a refund from a supplier",
		Description: "Voids a refund with a reversing entry on the date given, with a reason, and a reversing allocation, which reopens the " +
			"credit (FR-GL-12, FR-AR-18). The date is not before the refund's (CREDIT-07). In Founder mode voiding needs a second person " +
			"(FM-1): it is refused (CREDIT-11), and requestSupplierRefundVoid asks for the approval. Needs If-Match with the refund's ETag. " +
			"Idempotent with Idempotency-Key.",
	}, voidCodes, func(ctx context.Context, in *refundVoidInput) (*refundPostingOutput, error) {
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
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (SupplierRefundPosting, error) {
			doc, err := u.Documents().Lock(ctx, id)
			if err != nil {
				return SupplierRefundPosting{}, err
			}
			before, err := a.refundOf(ctx, u, doc)
			if err != nil {
				return SupplierRefundPosting{}, err
			}
			if err := checkIfMatch(in.IfMatch, refundETag(before)); err != nil {
				return SupplierRefundPosting{}, err
			}
			if err := checkConflicts(ctx, u, p, "Posting"); err != nil {
				return SupplierRefundPosting{}, err
			}
			if mode, _, _, err := teamModeOf(ctx, u); err != nil {
				return SupplierRefundPosting{}, err
			} else if mode == team.Founder {
				return SupplierRefundPosting{}, problem(CodeRefundVoidApproval, "In Founder mode, voiding a recorded refund needs a second person (FM-1).",
					"Ask for another person's approval to void it")
			}
			r, err := a.refunds.Void(ctx, p.Organisation, id, date, strings.TrimSpace(in.Body.Reason))
			if err != nil {
				return SupplierRefundPosting{}, err
			}
			x, err := a.refundOf(ctx, u, r.Refund)
			return SupplierRefundPosting{Refund: x, Entry: entryOf(r.Entry)}, err
		})
		if err != nil {
			return nil, err
		}
		if w.Previewed {
			return &refundPostingOutput{Status: http.StatusOK, Applied: applied(w), Body: w.Body}, nil
		}
		return &refundPostingOutput{Status: http.StatusOK, ETag: refundETag(w.Body.Refund), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	requestCodes := []string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		CodeSessionRequired, CodeStepUpRequired, approval.CodePending, storage.CodeApproval, credits.CodeNotRefund, credits.CodeNotPosted,
		credits.CodeBadDraft, credits.CodeVoidDate, storage.CodeRetry, storage.CodeLockTimeout, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}
	register(a, authz.RequestSupplierRefundVoid, huma.Operation{
		OperationID: "requestSupplierRefundVoid", Method: http.MethodPost, Path: "/v1/supplier-refunds/{refund_id}/void-approval", Tags: []string{"Purchases"},
		Summary: "Ask for approval to void a refund from a supplier", DefaultStatus: http.StatusAccepted,
		Description: "Asks for another person's approval to void a refund on the date given, with a reason (FR-GL-12, FM-1): what " +
			"voidSupplierRefund asks for in Founder mode (CREDIT-11). The answer is 202 with an approval request, which a person who holds A " +
			"on \"Bills, vendor credits\" and did not ask approves, stepped up; on the approval the void posts and the credit reopens. Needs " +
			"a person signed in who passed step-up, and If-Match with the refund's ETag. Idempotent with Idempotency-Key.",
	}, requestCodes, func(ctx context.Context, in *refundVoidInput) (*billRequestOutput, error) {
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
			x, err := a.refundOf(ctx, u, doc)
			if err != nil {
				return Approval{}, err
			}
			if err := checkIfMatch(in.IfMatch, refundETag(x)); err != nil {
				return Approval{}, err
			}
			if doc.Status != storage.DocumentPosted {
				return Approval{}, problem(credits.CodeNotPosted, fmt.Sprintf("The refund is %s.", doc.Status), "Ask for approval to void a posted refund")
			}
			if in.Body.Date < x.Date {
				return Approval{}, problem(credits.CodeVoidDate, fmt.Sprintf("The void is dated %s, before the refund's %s.", in.Body.Date, x.Date),
					"Date the void on or after the refund")
			}
			return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectRefund, ObjectKey: doc.ID.String(), Operation: approval.OpRefundVoid,
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
