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
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// --------------------------------------------- customer credits applied to invoices (FR-AR-04, FR-AR-18, FM-1)
//
// A credit with a customer (an issued credit note, or what a receipt left unapplied) is applied to one of the
// customer's invoices by an application, which settles both by the amount applied and posts only the realised FX the
// two stored rates make (customer_credit_application@1). Voiding it reverses that and reopens both; in Founder mode a
// void needs a second person (FM-1). As a supplier credit's application is to bills.

// CodeCustomerApplicationVoidApproval is the API's code for a void that needs a second person in Founder mode (FM-1).
const CodeCustomerApplicationVoidApproval = "APPLY-09"

// CustomerApplicationCodes are the codes the rule and the application's own checks answer.
var CustomerApplicationCodes = []string{customercredit.CodeCredit, customercredit.CodeInvoice, customercredit.CodeTooMuch, ledger.CodePaymentAmount,
	ledger.CodeWrongRate, ledger.CodeSystemRole}

// CustomerApplicationApprovalCodes are the codes an approval of an application's void may answer.
func CustomerApplicationApprovalCodes() []string {
	return []string{customercredit.CodeNotApplication, customercredit.CodeNotPosted, customercredit.CodeVoidDate, customercredit.CodeBadDraft}
}

// CustomerCreditApplicationDraft is an application as entered.
type CustomerCreditApplicationDraft struct {
	CreditID  string `json:"credit_id" format:"uuid" doc:"The credit: an issued credit note, or a customer receipt with an amount unapplied." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f70"`
	InvoiceID string `json:"invoice_id" format:"uuid" doc:"The invoice: an issued invoice of the credit's customer, in its currency." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f30"`
	Date      string `json:"date" format:"date" doc:"The day the credit is applied." example:"2026-10-21"`
	Amount    Money  `json:"amount" doc:"What is applied, in their currency: at most what the invoice has open and what is left of the credit (APPLY-05)."`
}

// CustomerAppliedDocument names a document a customer credit application settles.
type CustomerAppliedDocument struct {
	ID     string `json:"id" format:"uuid" doc:"The document." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f30"`
	Kind   string `json:"kind" enum:"invoice,credit_note,customer_receipt,customer_deposit" doc:"What it is." example:"invoice"`
	Number string `json:"number,omitempty" doc:"Its number." example:"INV-0042"`
}

// CustomerCreditApplication is a credit with a customer applied to an invoice.
type CustomerCreditApplication struct {
	ID             string                  `json:"id" format:"uuid" doc:"The application's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f80"`
	Status         string                  `json:"status" enum:"posted,reversed" doc:"posted once applied; reversed by a void." example:"posted"`
	Version        int                     `json:"version" doc:"The version of its content; with the status it makes the ETag." example:"1"`
	Credit         CustomerAppliedDocument `json:"credit" doc:"The credit applied."`
	Invoice        CustomerAppliedDocument `json:"invoice" doc:"The invoice it settles."`
	Date           string                  `json:"date" format:"date" doc:"The day it was applied." example:"2026-10-21"`
	Amount         Money                   `json:"amount" doc:"What was applied, in their currency."`
	InvoiceCleared Money                   `json:"invoice_cleared" doc:"The invoice's carrying amount it cleared, in the base currency."`
	CreditCleared  Money                   `json:"credit_cleared" doc:"The credit's carrying amount it cleared, in the base currency."`
	RealisedFX     Money                   `json:"realised_fx" doc:"The realised gain (positive) or loss (negative): what the credit carried less what the invoice carried (ENG-41); zero when the two agree and nothing posted."`
	LedgerImpact   []EntryLine             `json:"ledger_impact" nullable:"false" doc:"The lines its entry wrote; none when nothing posted."`
	AppliedBy      *Person                 `json:"applied_by,omitempty" doc:"Who applied it."`
	AppliedAt      *time.Time              `json:"applied_at,omitempty" doc:"When it was applied." example:"2026-10-21T09:30:00Z"`
	ApprovalAsked  string                  `json:"approval_asked,omitempty" format:"uuid" doc:"The pending request to void it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4dd3"`
}

// CustomerCreditApplicationPosting is an application after it is made or voided, with the entry it posted, if any.
type CustomerCreditApplicationPosting struct {
	Application CustomerCreditApplication `json:"application" doc:"The application after the command."`
	Entry       *Entry                    `json:"entry,omitempty" doc:"The entry the command posted: the realised FX, or its reversal; absent when nothing posted."`
}

// CustomerCreditApplicationPath names an application in the path.
type CustomerCreditApplicationPath struct {
	ApplicationID string `path:"application_id" format:"uuid" doc:"The application's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f80"`
}

type applyCustomerCreditInput struct {
	KeyHeader
	PreferHeader
	Body CustomerCreditApplicationDraft
}

type customerApplicationReadOutput struct {
	ETag string `header:"ETag" doc:"The application's version and status, for If-Match."`
	Body CustomerCreditApplication
}

type customerApplicationVoidInput struct {
	CustomerCreditApplicationPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body Reversal
}

type customerApplicationPostingOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The application's version and status, for If-Match."`
	Location string `header:"Location" doc:"The application's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     CustomerCreditApplicationPosting
}

func customerApplicationETag(x CustomerCreditApplication) string { return etag(x.Version, x.Status) }

func customerAppliedDocument(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) CustomerAppliedDocument {
	out := CustomerAppliedDocument{ID: id.String(), Kind: "invoice"}
	if doc, err := u.Documents().Get(ctx, id); err == nil {
		out.Number = doc.Number
		switch doc.Type {
		case storage.DocumentCreditNote:
			out.Kind = "credit_note"
		case storage.DocumentCustomerReceipt:
			out.Kind = "customer_receipt"
		case storage.DocumentCustomerDeposit:
			out.Kind = "customer_deposit"
		}
	}
	return out
}

// customerApplicationOf is an application as the API shows it, from its document.
func (a *API) customerApplicationOf(ctx context.Context, u storage.UnitOfWork, doc storage.Document) (CustomerCreditApplication, error) {
	d, err := customercredit.Read(doc)
	if err != nil {
		return CustomerCreditApplication{}, err
	}
	o, err := u.Organisation(ctx)
	if err != nil {
		return CustomerCreditApplication{}, err
	}
	zero := Money{Amount: "0.00", Currency: o.BaseCurrency}
	out := CustomerCreditApplication{ID: doc.ID.String(), Status: string(doc.Status), Version: doc.Version, Credit: customerAppliedDocument(ctx, u, d.CreditID),
		Invoice: customerAppliedDocument(ctx, u, d.InvoiceID), Date: dayText(d.Date), Amount: moneyOut(d.Amount), InvoiceCleared: zero, CreditCleared: zero,
		RealisedFX: zero, LedgerImpact: []EntryLine{}, AppliedBy: personOf(doc.PostedBy), AppliedAt: utc(doc.PostedAt)}
	allocs, err := u.Allocations().OfSource(ctx, doc.ID)
	if err != nil {
		return CustomerCreditApplication{}, err
	}
	for _, x := range allocs {
		if x.ReversesID != nil || x.SourceVersion != doc.Version {
			continue
		}
		if x.DocumentID == d.InvoiceID {
			out.InvoiceCleared, out.RealisedFX = storedOut(x.BaseAmount, x.BaseCurrency), storedOut(x.RealisedFX, x.BaseCurrency)
		} else {
			out.CreditCleared = storedOut(x.BaseAmount, x.BaseCurrency)
		}
	}
	if e, err := u.Ledger().ByIdentity(ctx, ledger.CustomerCreditApplicationIdentity(doc.ID, doc.Version, ledger.RuleCustomerCreditApplication)); err == nil {
		out.LedgerImpact = entryOf(e).Lines
	} else if !errors.Is(err, ports.ErrNotFound) {
		return CustomerCreditApplication{}, err
	}
	now := a.deps.Clock.Now()
	requests, err := u.Approvals().List(ctx, "")
	if err != nil {
		return CustomerCreditApplication{}, err
	}
	for _, r := range requests {
		if r.ObjectType == approval.ObjectCustomerApplication && r.ObjectKey == out.ID && approval.StatusAt(requestOf(r), now) == approval.Pending {
			out.ApprovalAsked = r.ID.String()
		}
	}
	return out, a.name(ctx, u, out.AppliedBy)
}

// customerApplicationDraftOf reads an application from the request, with every invalid field at once.
func customerApplicationDraftOf(in CustomerCreditApplicationDraft) (customercredit.Draft, error) {
	var d customercredit.Draft
	var fields []FieldProblem
	for _, x := range []struct {
		text, field string
		to          *uuid.UUID
	}{{in.CreditID, "body.credit_id", &d.CreditID}, {in.InvoiceID, "body.invoice_id", &d.InvoiceID}} {
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

// customerApplicationProblems turns the rule's problems into the API's, naming the application.
func customerApplicationProblems(err error) error {
	probs, ok := err.(ledger.Problems)
	if !ok || len(probs) == 0 {
		return err
	}
	fields := make([]FieldProblem, 0, len(probs))
	for _, x := range probs {
		fields = append(fields, FieldProblem{Code: x.Code, Field: x.Field, Message: x.Message, Fix: x.Fix})
	}
	return problem(probs[0].Code, fmt.Sprintf("The credit cannot be applied: %d problem(s), each listed in errors.", len(probs)),
		"Correct the application and apply it again.", fields...)
}

func registerCustomerCreditApplications(a *API) {
	readCodes := []string{CodeNotFound, customercredit.CodeNotApplication, customercredit.CodeBadDraft}
	register(a, authz.GetCustomerCreditApplication, huma.Operation{
		OperationID: "getCustomerCreditApplication", Method: http.MethodGet, Path: "/v1/customer-credit-applications/{application_id}", Tags: []string{"Sales"},
		Summary: "Get a credit applied to an invoice",
		Description: "Returns an application of a customer credit to an invoice with its ETag: the credit and the invoice, the amount, the " +
			"carrying amounts it cleared, the realised FX (ENG-41), the lines its entry wrote if any, and the pending request to void it. Read-only.",
	}, readCodes, func(ctx context.Context, in *CustomerCreditApplicationPath) (*customerApplicationReadOutput, error) {
		id, err := parseID("application_id", in.ApplicationID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out CustomerCreditApplication
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Get(ctx, id)
			if err != nil {
				return err
			}
			out, err = a.customerApplicationOf(ctx, u, doc)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &customerApplicationReadOutput{ETag: customerApplicationETag(out), Body: out}, nil
	})

	applyCodes := append(append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, team.CodeAction, CodeMalformed, CodeTooLarge,
		CodeMediaType, CodeInvalid, money.CodeNotDecimal, money.CodePrecision, money.CodeRange, money.CodeCurrency, storage.CodeAllocation}, postingCodes()...),
		CustomerApplicationCodes...)
	register(a, authz.ApplyCustomerCredit, huma.Operation{
		OperationID: "applyCustomerCredit", Method: http.MethodPost, Path: "/v1/customer-credit-applications", Tags: []string{"Sales"}, DefaultStatus: http.StatusCreated,
		Summary: "Apply a customer credit to an invoice",
		Description: "Applies a credit with a customer (an issued credit note, or what a customer receipt left unapplied) to one of the " +
			"customer's issued invoices in its currency (FR-AR-04, FR-AR-18): it settles both by the amount, at most what the invoice has open " +
			"and what is left of the credit (APPLY-05), and posts under customer_credit_application@1 only what their stored rates make differ, " +
			"the realised FX to 7030 (ENG-41); in the base currency it posts nothing. Side effects: the application, two allocations, the entry " +
			"when one posts, audit events. Every problem comes back together. Idempotent with Idempotency-Key; Prefer: return=preview applies nothing.",
	}, applyCodes, func(ctx context.Context, in *applyCustomerCreditInput) (*customerApplicationPostingOutput, error) {
		p, _ := authn.From(ctx)
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (CustomerCreditApplicationPosting, error) {
			if err := checkConflicts(ctx, u, p, "Posting"); err != nil {
				return CustomerCreditApplicationPosting{}, err
			}
			d, err := customerApplicationDraftOf(in.Body)
			if err != nil {
				return CustomerCreditApplicationPosting{}, err
			}
			r, err := a.customerApplications.Apply(ctx, p.Organisation, d)
			if err != nil {
				return CustomerCreditApplicationPosting{}, customerApplicationProblems(err)
			}
			doc, err := u.Documents().Get(ctx, r.Application.ID)
			if err != nil {
				return CustomerCreditApplicationPosting{}, err
			}
			out := CustomerCreditApplicationPosting{}
			if out.Application, err = a.customerApplicationOf(ctx, u, doc); err != nil {
				return out, err
			}
			if r.Entry.ID != uuid.Nil {
				e := entryOf(r.Entry)
				out.Entry = &e
			}
			return out, nil
		})
		if err != nil {
			return nil, err
		}
		if w.Previewed {
			return &customerApplicationPostingOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
		}
		return &customerApplicationPostingOutput{Status: w.Status, ETag: customerApplicationETag(w.Body.Application),
			Location: "/v1/customer-credit-applications/" + w.Body.Application.ID, Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	voidCodes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, team.CodeAction,
		CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeCustomerApplicationVoidApproval, customercredit.CodeNotPosted, customercredit.CodeVoidDate,
		storage.CodeAllocation}, append(postingCodes(), readCodes...)...)
	register(a, authz.VoidCustomerCreditApplication, huma.Operation{
		OperationID: "voidCustomerCreditApplication", Method: http.MethodPost, Path: "/v1/customer-credit-applications/{application_id}/void", Tags: []string{"Sales"},
		Summary: "Void a credit applied to an invoice",
		Description: "Voids an application on the date given, with a reason (FR-GL-12): its entry, if it posted one, is reversed, and each of " +
			"its allocations is reversed on that date, which reopens the invoice and the credit by the amount applied. The date is not before " +
			"the application's (APPLY-07). In Founder mode voiding needs a second person (FM-1): it is refused (APPLY-09), and " +
			"requestCustomerCreditApplicationVoid asks for the approval. Needs If-Match with the application's ETag. Idempotent with Idempotency-Key.",
	}, voidCodes, func(ctx context.Context, in *customerApplicationVoidInput) (*customerApplicationPostingOutput, error) {
		id, err := parseID("application_id", in.ApplicationID)
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
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (CustomerCreditApplicationPosting, error) {
			doc, err := u.Documents().Lock(ctx, id)
			if err != nil {
				return CustomerCreditApplicationPosting{}, err
			}
			before, err := a.customerApplicationOf(ctx, u, doc)
			if err != nil {
				return CustomerCreditApplicationPosting{}, err
			}
			if err := checkIfMatch(in.IfMatch, customerApplicationETag(before)); err != nil {
				return CustomerCreditApplicationPosting{}, err
			}
			if err := checkConflicts(ctx, u, p, "Posting"); err != nil {
				return CustomerCreditApplicationPosting{}, err
			}
			if mode, _, _, err := teamModeOf(ctx, u); err != nil {
				return CustomerCreditApplicationPosting{}, err
			} else if mode == team.Founder {
				return CustomerCreditApplicationPosting{}, problem(CodeCustomerApplicationVoidApproval,
					"In Founder mode, voiding a credit applied to an invoice needs a second person (FM-1).", "Ask for another person's approval to void it")
			}
			r, err := a.customerApplications.Void(ctx, p.Organisation, id, date, strings.TrimSpace(in.Body.Reason))
			if err != nil {
				return CustomerCreditApplicationPosting{}, err
			}
			out := CustomerCreditApplicationPosting{}
			if out.Application, err = a.customerApplicationOf(ctx, u, r.Application); err != nil {
				return out, err
			}
			if r.Entry.ID != uuid.Nil {
				e := entryOf(r.Entry)
				out.Entry = &e
			}
			return out, nil
		})
		if err != nil {
			return nil, err
		}
		if w.Previewed {
			return &customerApplicationPostingOutput{Status: http.StatusOK, Applied: applied(w), Body: w.Body}, nil
		}
		return &customerApplicationPostingOutput{Status: http.StatusOK, ETag: customerApplicationETag(w.Body.Application), Replayed: replayedHeader(w.Replayed),
			Body: w.Body}, nil
	})

	requestCodes := []string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		CodeSessionRequired, CodeStepUpRequired, approval.CodePending, storage.CodeApproval, customercredit.CodeNotApplication, customercredit.CodeNotPosted,
		customercredit.CodeBadDraft, customercredit.CodeVoidDate, storage.CodeRetry, storage.CodeLockTimeout, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}
	register(a, authz.RequestCustomerCreditApplicationVoid, huma.Operation{
		OperationID: "requestCustomerCreditApplicationVoid", Method: http.MethodPost, Path: "/v1/customer-credit-applications/{application_id}/void-approval",
		Tags: []string{"Sales"}, Summary: "Ask for approval to void a credit applied to an invoice", DefaultStatus: http.StatusAccepted,
		Description: "Asks for another person's approval to void an application on the date given, with a reason (FR-GL-12, FM-1): what " +
			"voidCustomerCreditApplication asks for in Founder mode (APPLY-09). The answer is 202 with an approval request, which a person who " +
			"holds A on \"Invoices, credit notes\" and did not ask approves, stepped up; on the approval the void posts and the invoice and the " +
			"credit reopen. Needs a person signed in who passed step-up, and If-Match with the application's ETag. Idempotent with Idempotency-Key.",
	}, requestCodes, func(ctx context.Context, in *customerApplicationVoidInput) (*billRequestOutput, error) {
		id, err := parseID("application_id", in.ApplicationID)
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
			x, err := a.customerApplicationOf(ctx, u, doc)
			if err != nil {
				return Approval{}, err
			}
			if err := checkIfMatch(in.IfMatch, customerApplicationETag(x)); err != nil {
				return Approval{}, err
			}
			if doc.Status != storage.DocumentPosted {
				return Approval{}, problem(customercredit.CodeNotPosted, fmt.Sprintf("The application is %s.", doc.Status), "Ask for approval to void a posted application")
			}
			if in.Body.Date < x.Date {
				return Approval{}, problem(customercredit.CodeVoidDate, fmt.Sprintf("The void is dated %s, before the application's %s.", in.Body.Date, x.Date),
					"Date the void on or after the application")
			}
			return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectCustomerApplication, ObjectKey: doc.ID.String(),
				Operation: approval.OpCustomerApplicationVoid,
				Summary: fmt.Sprintf("Void %s applied to %s, %s %s, on %s: %s", cmpText(x.Credit.Number, "a credit"), cmpText(x.Invoice.Number, "an invoice"),
					x.Amount.Amount, x.Amount.Currency, in.Body.Date, reason),
				Execute: map[string]any{"application_id": doc.ID.String(), "date": in.Body.Date, "reason": reason},
				Diff:    []ApprovalDiff{{Field: "status", Before: "posted", After: "reversed"}, {Field: "void_date", After: in.Body.Date}}})
		})
		if err != nil {
			return nil, err
		}
		return &billRequestOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})
}
