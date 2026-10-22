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
	"github.com/cognizhi/fyniq/internal/domain/credits"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/team"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// --------------------------------------------------- supplier credits applied to bills (FR-AP-03, FR-AR-18, FM-1)
//
// A credit with a supplier (a posted vendor credit, or what a payment left unapplied) is applied to one of the
// supplier's bills by an application, which settles both by the amount applied and posts only the realised FX the two
// stored rates make (credit_application@1). Voiding it reverses that and reopens both; in Founder mode a void needs a
// second person (FM-1).

// CodeApplicationVoidApproval is the API's code for a void that needs a second person in Founder mode (FM-1).
const CodeApplicationVoidApproval = "CREDIT-09"

// ApplicationCodes are the codes the rule and the application's own checks answer.
var ApplicationCodes = []string{credits.CodeCredit, credits.CodeBill, credits.CodeTooMuch, ledger.CodePaymentAmount, ledger.CodeWrongRate,
	ledger.CodeSystemRole}

// ApplicationApprovalCodes are the codes an approval of an application's void may answer.
func ApplicationApprovalCodes() []string {
	return []string{credits.CodeNotApplication, credits.CodeNotPosted, credits.CodeVoidDate, credits.CodeBadDraft}
}

// CreditApplicationDraft is an application as entered.
type CreditApplicationDraft struct {
	CreditID string `json:"credit_id" format:"uuid" doc:"The credit: a posted vendor credit, or a payment of bills with an amount unapplied." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e50"`
	BillID   string `json:"bill_id" format:"uuid" doc:"The bill: a posted bill of the credit's supplier, in its currency." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e30"`
	Date     string `json:"date" format:"date" doc:"The day the credit is applied." example:"2026-10-21"`
	Amount   Money  `json:"amount" doc:"What is applied, in their currency: at most what the bill has open and what is left of the credit (CREDIT-05)."`
}

// AppliedDocument names a document an application settles.
type AppliedDocument struct {
	ID     string `json:"id" format:"uuid" doc:"The document." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e30"`
	Kind   string `json:"kind" enum:"bill,vendor_credit,bill_payment" doc:"What it is." example:"bill"`
	Number string `json:"number,omitempty" doc:"Its number." example:"BILL-0007"`
}

// CreditApplication is a supplier credit applied to a bill.
type CreditApplication struct {
	ID            string          `json:"id" format:"uuid" doc:"The application's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e60"`
	Status        string          `json:"status" enum:"posted,reversed" doc:"posted once applied; reversed by a void." example:"posted"`
	Version       int             `json:"version" doc:"The version of its content; with the status it makes the ETag." example:"1"`
	Credit        AppliedDocument `json:"credit" doc:"The credit applied."`
	Bill          AppliedDocument `json:"bill" doc:"The bill it settles."`
	Date          string          `json:"date" format:"date" doc:"The day it was applied." example:"2026-10-21"`
	Amount        Money           `json:"amount" doc:"What was applied, in their currency."`
	BillCleared   Money           `json:"bill_cleared" doc:"The bill's carrying amount it cleared, in the base currency."`
	CreditCleared Money           `json:"credit_cleared" doc:"The credit's carrying amount it cleared, in the base currency."`
	RealisedFX    Money           `json:"realised_fx" doc:"The realised gain (positive) or loss (negative): what the bill carried less what the credit carried (ENG-41); zero when the two agree and nothing posted."`
	LedgerImpact  []EntryLine     `json:"ledger_impact" nullable:"false" doc:"The lines its entry wrote; none when nothing posted."`
	AppliedBy     *Person         `json:"applied_by,omitempty" doc:"Who applied it."`
	AppliedAt     *time.Time      `json:"applied_at,omitempty" doc:"When it was applied." example:"2026-10-21T09:30:00Z"`
	ApprovalAsked string          `json:"approval_asked,omitempty" format:"uuid" doc:"The pending request to void it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4dd1"`
}

// CreditApplicationPosting is an application after it is made or voided, with the entry it posted, if any.
type CreditApplicationPosting struct {
	Application CreditApplication `json:"application" doc:"The application after the command."`
	Entry       *Entry            `json:"entry,omitempty" doc:"The entry the command posted: the realised FX, or its reversal; absent when nothing posted."`
}

// CreditApplicationPath names an application in the path.
type CreditApplicationPath struct {
	ApplicationID string `path:"application_id" format:"uuid" doc:"The application's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e60"`
}

type applyCreditInput struct {
	KeyHeader
	PreferHeader
	Body CreditApplicationDraft
}

type applicationReadOutput struct {
	ETag string `header:"ETag" doc:"The application's version and status, for If-Match."`
	Body CreditApplication
}

type applicationVoidInput struct {
	CreditApplicationPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body Reversal
}

type applicationPostingOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The application's version and status, for If-Match."`
	Location string `header:"Location" doc:"The application's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     CreditApplicationPosting
}

func applicationETag(x CreditApplication) string { return etag(x.Version, x.Status) }

func appliedDocument(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) AppliedDocument {
	out := AppliedDocument{ID: id.String(), Kind: "bill"}
	if doc, err := u.Documents().Get(ctx, id); err == nil {
		out.Number = doc.Number
		switch doc.Type {
		case storage.DocumentVendorCredit:
			out.Kind = "vendor_credit"
		case storage.DocumentBillPayment:
			out.Kind = "bill_payment"
		}
	}
	return out
}

// applicationOf is an application as the API shows it, from its document.
func (a *API) applicationOf(ctx context.Context, u storage.UnitOfWork, doc storage.Document) (CreditApplication, error) {
	d, err := credits.Read(doc)
	if err != nil {
		return CreditApplication{}, err
	}
	o, err := u.Organisation(ctx)
	if err != nil {
		return CreditApplication{}, err
	}
	zero := Money{Amount: "0.00", Currency: o.BaseCurrency}
	out := CreditApplication{ID: doc.ID.String(), Status: string(doc.Status), Version: doc.Version, Credit: appliedDocument(ctx, u, d.CreditID),
		Bill: appliedDocument(ctx, u, d.BillID), Date: dayText(d.Date), Amount: moneyOut(d.Amount), BillCleared: zero, CreditCleared: zero, RealisedFX: zero,
		LedgerImpact: []EntryLine{}, AppliedBy: personOf(doc.PostedBy), AppliedAt: utc(doc.PostedAt)}
	allocs, err := u.Allocations().OfSource(ctx, doc.ID)
	if err != nil {
		return CreditApplication{}, err
	}
	for _, x := range allocs {
		if x.ReversesID != nil || x.SourceVersion != doc.Version {
			continue
		}
		if x.DocumentID == d.BillID {
			out.BillCleared, out.RealisedFX = storedOut(x.BaseAmount, x.BaseCurrency), storedOut(x.RealisedFX, x.BaseCurrency)
		} else {
			out.CreditCleared = storedOut(x.BaseAmount, x.BaseCurrency)
		}
	}
	if e, err := u.Ledger().ByIdentity(ctx, ledger.CreditApplicationIdentity(doc.ID, doc.Version, ledger.RuleCreditApplication)); err == nil {
		out.LedgerImpact = entryOf(e).Lines
	} else if !errors.Is(err, ports.ErrNotFound) {
		return CreditApplication{}, err
	}
	now := a.deps.Clock.Now()
	requests, err := u.Approvals().List(ctx, "")
	if err != nil {
		return CreditApplication{}, err
	}
	for _, r := range requests {
		if r.ObjectType == approval.ObjectApplication && r.ObjectKey == out.ID && approval.StatusAt(requestOf(r), now) == approval.Pending {
			out.ApprovalAsked = r.ID.String()
		}
	}
	return out, a.name(ctx, u, out.AppliedBy)
}

// applicationDraftOf reads an application from the request, with every invalid field at once.
func applicationDraftOf(in CreditApplicationDraft) (credits.Draft, error) {
	var d credits.Draft
	var fields []FieldProblem
	for _, x := range []struct {
		text, field string
		to          *uuid.UUID
	}{{in.CreditID, "body.credit_id", &d.CreditID}, {in.BillID, "body.bill_id", &d.BillID}} {
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

// applicationProblems turns the rule's problems into the API's, naming the application.
func applicationProblems(err error) error {
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

func registerCreditApplications(a *API) {
	readCodes := []string{CodeNotFound, credits.CodeNotApplication, credits.CodeBadDraft}
	register(a, authz.GetCreditApplication, huma.Operation{
		OperationID: "getCreditApplication", Method: http.MethodGet, Path: "/v1/credit-applications/{application_id}", Tags: []string{"Purchases"},
		Summary: "Get a credit applied to a bill",
		Description: "Returns an application of a supplier credit to a bill with its ETag: the credit and the bill, the amount, the carrying " +
			"amounts it cleared, the realised FX (ENG-41), the lines its entry wrote if any, and the pending request to void it. Read-only.",
	}, readCodes, func(ctx context.Context, in *CreditApplicationPath) (*applicationReadOutput, error) {
		id, err := parseID("application_id", in.ApplicationID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out CreditApplication
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Get(ctx, id)
			if err != nil {
				return err
			}
			out, err = a.applicationOf(ctx, u, doc)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &applicationReadOutput{ETag: applicationETag(out), Body: out}, nil
	})

	applyCodes := append(append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, team.CodeAction, CodeMalformed, CodeTooLarge,
		CodeMediaType, CodeInvalid, money.CodeNotDecimal, money.CodePrecision, money.CodeRange, money.CodeCurrency, storage.CodeAllocation}, postingCodes()...),
		ApplicationCodes...)
	register(a, authz.ApplyCredit, huma.Operation{
		OperationID: "applyCredit", Method: http.MethodPost, Path: "/v1/credit-applications", Tags: []string{"Purchases"}, DefaultStatus: http.StatusCreated,
		Summary: "Apply a supplier credit to a bill",
		Description: "Applies a credit with a supplier (a posted vendor credit, or what a payment of bills left unapplied) to one of the " +
			"supplier's posted bills in its currency (FR-AP-03, FR-AR-18): it settles both by the amount, at most what the bill has open and " +
			"what is left of the credit (CREDIT-05), and posts under credit_application@1 only what their stored rates make differ, the " +
			"realised FX to 7030 (ENG-41); in the base currency it posts nothing. Side effects: the application, two allocations, the entry " +
			"when one posts, audit events. Every problem comes back together. Idempotent with Idempotency-Key; Prefer: return=preview applies nothing.",
	}, applyCodes, func(ctx context.Context, in *applyCreditInput) (*applicationPostingOutput, error) {
		p, _ := authn.From(ctx)
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (CreditApplicationPosting, error) {
			if err := checkConflicts(ctx, u, p, "Posting"); err != nil {
				return CreditApplicationPosting{}, err
			}
			d, err := applicationDraftOf(in.Body)
			if err != nil {
				return CreditApplicationPosting{}, err
			}
			r, err := a.applications.Apply(ctx, p.Organisation, d)
			if err != nil {
				return CreditApplicationPosting{}, applicationProblems(err)
			}
			doc, err := u.Documents().Get(ctx, r.Application.ID)
			if err != nil {
				return CreditApplicationPosting{}, err
			}
			out := CreditApplicationPosting{}
			if out.Application, err = a.applicationOf(ctx, u, doc); err != nil {
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
			return &applicationPostingOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
		}
		return &applicationPostingOutput{Status: w.Status, ETag: applicationETag(w.Body.Application), Location: "/v1/credit-applications/" + w.Body.Application.ID,
			Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	voidCodes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, team.CodeAction,
		CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeApplicationVoidApproval, credits.CodeNotPosted, credits.CodeVoidDate, storage.CodeAllocation},
		append(postingCodes(), readCodes...)...)
	register(a, authz.VoidCreditApplication, huma.Operation{
		OperationID: "voidCreditApplication", Method: http.MethodPost, Path: "/v1/credit-applications/{application_id}/void", Tags: []string{"Purchases"},
		Summary: "Void a credit applied to a bill",
		Description: "Voids an application on the date given, with a reason (FR-GL-12): its entry, if it posted one, is reversed, and each of " +
			"its allocations is reversed on that date, which reopens the bill and the credit by the amount applied. The date is not before " +
			"the application's (CREDIT-07). In Founder mode voiding needs a second person (FM-1): it is refused (CREDIT-09), and " +
			"requestCreditApplicationVoid asks for the approval. Needs If-Match with the application's ETag. Idempotent with Idempotency-Key.",
	}, voidCodes, func(ctx context.Context, in *applicationVoidInput) (*applicationPostingOutput, error) {
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
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (CreditApplicationPosting, error) {
			doc, err := u.Documents().Lock(ctx, id)
			if err != nil {
				return CreditApplicationPosting{}, err
			}
			before, err := a.applicationOf(ctx, u, doc)
			if err != nil {
				return CreditApplicationPosting{}, err
			}
			if err := checkIfMatch(in.IfMatch, applicationETag(before)); err != nil {
				return CreditApplicationPosting{}, err
			}
			if err := checkConflicts(ctx, u, p, "Posting"); err != nil {
				return CreditApplicationPosting{}, err
			}
			if mode, _, _, err := teamModeOf(ctx, u); err != nil {
				return CreditApplicationPosting{}, err
			} else if mode == team.Founder {
				return CreditApplicationPosting{}, problem(CodeApplicationVoidApproval, "In Founder mode, voiding an applied credit needs a second person (FM-1).",
					"Ask for another person's approval to void it")
			}
			r, err := a.applications.Void(ctx, p.Organisation, id, date, strings.TrimSpace(in.Body.Reason))
			if err != nil {
				return CreditApplicationPosting{}, err
			}
			out := CreditApplicationPosting{}
			if out.Application, err = a.applicationOf(ctx, u, r.Application); err != nil {
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
			return &applicationPostingOutput{Status: http.StatusOK, Applied: applied(w), Body: w.Body}, nil
		}
		return &applicationPostingOutput{Status: http.StatusOK, ETag: applicationETag(w.Body.Application), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	requestCodes := []string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		CodeSessionRequired, CodeStepUpRequired, approval.CodePending, storage.CodeApproval, credits.CodeNotApplication, credits.CodeNotPosted,
		credits.CodeBadDraft, credits.CodeVoidDate, storage.CodeRetry, storage.CodeLockTimeout, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}
	register(a, authz.RequestCreditApplicationVoid, huma.Operation{
		OperationID: "requestCreditApplicationVoid", Method: http.MethodPost, Path: "/v1/credit-applications/{application_id}/void-approval",
		Tags: []string{"Purchases"}, Summary: "Ask for approval to void a credit applied to a bill", DefaultStatus: http.StatusAccepted,
		Description: "Asks for another person's approval to void an application on the date given, with a reason (FR-GL-12, FM-1): what " +
			"voidCreditApplication asks for in Founder mode (CREDIT-09). The answer is 202 with an approval request, which a person who holds " +
			"A on \"Bills, vendor credits\" and did not ask approves, stepped up; on the approval the void posts and the bill and the credit " +
			"reopen. Needs a person signed in who passed step-up, and If-Match with the application's ETag. Idempotent with Idempotency-Key.",
	}, requestCodes, func(ctx context.Context, in *applicationVoidInput) (*billRequestOutput, error) {
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
			x, err := a.applicationOf(ctx, u, doc)
			if err != nil {
				return Approval{}, err
			}
			if err := checkIfMatch(in.IfMatch, applicationETag(x)); err != nil {
				return Approval{}, err
			}
			if doc.Status != storage.DocumentPosted {
				return Approval{}, problem(credits.CodeNotPosted, fmt.Sprintf("The application is %s.", doc.Status), "Ask for approval to void a posted application")
			}
			if in.Body.Date < x.Date {
				return Approval{}, problem(credits.CodeVoidDate, fmt.Sprintf("The void is dated %s, before the application's %s.", in.Body.Date, x.Date),
					"Date the void on or after the application")
			}
			return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectApplication, ObjectKey: doc.ID.String(), Operation: approval.OpApplicationVoid,
				Summary: fmt.Sprintf("Void %s applied to %s, %s %s, on %s: %s", cmpText(x.Credit.Number, "a credit"), cmpText(x.Bill.Number, "a bill"),
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
