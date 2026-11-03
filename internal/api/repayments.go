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
	"github.com/cognizhi/fyniq/internal/domain/director"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ directors' repayments (FR-EQ-05, FR-BNK-12; p1-founders-money)
//
// A settlement approved to be repaid is paid by a payment batch (design D10): the batch's release records one repayment
// per director, posted by director_repayment@1 (Dr 2500/2120 naming the person (paid out) · Cr the money account), and
// bank import matches it to the statement line. Nobody records one by hand (the database's FY254). One the bank
// rejected is voided on another person's approval who holds A on "Payment batches", never the person repaid (FM-9,
// APR-15): its entry is reversed and the settlements are open again for the next batch.

// RepaymentApprovalCodes are the codes an approval of a repayment's void may answer.
func RepaymentApprovalCodes() []string {
	return []string{director.CodeNotRepayment, director.CodeNotPosted, director.CodeNotRepaid}
}

// DirectorRepaymentPart is what a repayment paid of one settlement.
type DirectorRepaymentPart struct {
	SettlementID     string `json:"settlement_id" format:"uuid" doc:"The settlement it repaid." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fa9"`
	SettlementNumber string `json:"settlement_number,omitempty" doc:"The settlement's number." example:"DS-0001"`
	AccountID        string `json:"account_id" format:"uuid" doc:"The director's account it was owed on: 2500 or 2120." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d61"`
	AccountCode      string `json:"account_code,omitempty" doc:"Its code." example:"2500"`
	Amount           Money  `json:"amount" doc:"What it repaid of the settlement, in the base currency."`
}

// DirectorRepayment is what a payment batch repaid a director or shareholder.
type DirectorRepayment struct {
	ID             string                  `json:"id" format:"uuid" doc:"The repayment's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	Number         string                  `json:"number,omitempty" doc:"Its number from the repayment sequence (FR-ORG-04)." example:"DR-0001"`
	Status         string                  `json:"status" enum:"posted,reversed" doc:"posted by the batch's release; reversed by a void, as when the bank rejected it." example:"posted"`
	Version        int                     `json:"version" doc:"The version of its content; with the status it makes the ETag." example:"1"`
	ContactID      string                  `json:"contact_id" format:"uuid" doc:"The director or shareholder repaid." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	LegalName      string                  `json:"legal_name" doc:"Their name." example:"Mei Lin Tan"`
	Date           string                  `json:"date" format:"date" doc:"The batch's payment date: the day it posted." example:"2026-11-30"`
	MoneyAccountID string                  `json:"money_account_id" format:"uuid" doc:"The bank account it left." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d10"`
	Reference      string                  `json:"reference,omitempty" doc:"The batch's number, as the payment list carries it." example:"PB-0007"`
	BatchID        string                  `json:"batch_id,omitempty" format:"uuid" doc:"The payment batch whose release paid it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c10"`
	Total          Money                   `json:"total" doc:"What it repaid, in the base currency."`
	Settlements    []DirectorRepaymentPart `json:"settlements" nullable:"false" doc:"What it repaid of each settlement."`
	Paid           bool                    `json:"paid" doc:"Bank import matched it to its statement line." example:"false"`
	LedgerImpact   []EntryLine             `json:"ledger_impact" nullable:"false" doc:"The lines its entry wrote: debits positive."`
	ApprovalAsked  string                  `json:"approval_asked,omitempty" format:"uuid" doc:"The pending request to void it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4dd1"`
}

// DirectorRepaymentPath names a repayment in the path.
type DirectorRepaymentPath struct {
	RepaymentID string `path:"repayment_id" format:"uuid" doc:"The repayment's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
}

type repaymentReadOutput struct {
	ETag string `header:"ETag" doc:"The repayment's version and status, for If-Match."`
	Body DirectorRepayment
}

type repaymentVoidInput struct {
	DirectorRepaymentPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body Reversal
}

func repaymentETag(doc storage.Document) string { return etag(doc.Version, string(doc.Status)) }

// repaymentDoc is the repayment with id, not found for any other kind of document.
func repaymentDoc(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, lock bool) (storage.Document, error) {
	get := u.Documents().Get
	if lock {
		get = u.Documents().Lock
	}
	doc, err := get(ctx, id)
	if err != nil {
		return doc, err
	}
	if doc.Type != storage.DocumentDirectorRepayment {
		return doc, ports.ErrNotFound
	}
	return doc, nil
}

// repaymentOf is a repayment as the API shows it.
func (a *API) repaymentOf(ctx context.Context, u storage.UnitOfWork, doc storage.Document) (DirectorRepayment, error) {
	r, err := director.ReadRepayment(doc)
	if err != nil {
		return DirectorRepayment{}, err
	}
	base, err := baseOf(ctx, u)
	if err != nil {
		return DirectorRepayment{}, err
	}
	c, err := u.Contacts().Get(ctx, r.ContactID)
	if err != nil {
		return DirectorRepayment{}, err
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return DirectorRepayment{}, err
	}
	codes := map[uuid.UUID]string{}
	for _, x := range accounts {
		codes[x.ID] = x.Code
	}
	out := DirectorRepayment{ID: doc.ID.String(), Number: doc.Number, Status: string(doc.Status), Version: doc.Version, ContactID: r.ContactID.String(),
		LegalName: c.LegalName, Date: dayText(r.Date), MoneyAccountID: r.AccountID.String(), Reference: r.Reference, Settlements: []DirectorRepaymentPart{},
		LedgerImpact: []EntryLine{}}
	total := money.Zero(base)
	rule, err := director.PrepareRepayment(ctx, u, doc)
	if err != nil {
		return DirectorRepayment{}, err
	}
	for _, x := range rule.Settlements {
		sd, err := u.Documents().Get(ctx, x.SettlementID)
		if err != nil {
			return DirectorRepayment{}, err
		}
		out.Settlements = append(out.Settlements, DirectorRepaymentPart{SettlementID: x.SettlementID.String(), SettlementNumber: sd.Number,
			AccountID: x.Account.String(), AccountCode: codes[x.Account], Amount: moneyOut(x.Amount)})
		if total, err = total.Add(x.Amount); err != nil {
			return DirectorRepayment{}, err
		}
	}
	out.Total = moneyOut(total)
	if rel, err := u.PaymentBatches().ReleaseOfPayment(ctx, doc.ID); err != nil {
		return DirectorRepayment{}, err
	} else if rel != nil {
		out.BatchID = rel.BatchID.String()
	}
	matches, err := u.BankMatching().Matches(ctx, storage.BankMatchFilter{DocumentID: &doc.ID})
	if err != nil {
		return DirectorRepayment{}, err
	}
	for _, m := range matches {
		out.Paid = out.Paid || m.UndoneAt == nil
	}
	e, err := u.Ledger().ByIdentity(ctx, ledger.DirectorRepaymentIdentity(doc.ID, doc.Version, ledger.RuleDirectorRepayment))
	if err != nil {
		return DirectorRepayment{}, err
	}
	out.LedgerImpact = entryOf(e).Lines
	pending, err := u.Approvals().List(ctx, string(approval.Pending))
	if err != nil {
		return DirectorRepayment{}, err
	}
	for _, x := range pending {
		if x.ObjectType == approval.ObjectDirectorRepayment && x.ObjectKey == doc.ID.String() {
			out.ApprovalAsked = x.ID.String()
		}
	}
	return out, nil
}

// repaymentDependencies are what a repayment's void depends on: its version and status, and what it repaid.
func (a *API) repaymentDependencies(ctx context.Context, u storage.UnitOfWork, key string) (int, map[string]any, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return 0, nil, err
	}
	doc, err := repaymentDoc(ctx, u, id, false)
	if err != nil {
		return 0, nil, err
	}
	r, err := a.repaymentOf(ctx, u, doc)
	if err != nil {
		return 0, nil, err
	}
	return doc.Version, map[string]any{"status": string(doc.Status), "repaid": r.Total.Amount + " " + r.Total.Currency}, nil
}

// repaymentExecutable refuses the person repaid: a void leaves what the company owes them open again, so they never
// approve it (FM-9, APR-15).
func repaymentExecutable(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, signer uuid.UUID) error {
	id, err := uuid.Parse(r.ObjectKey)
	if err != nil {
		return err
	}
	doc, err := repaymentDoc(ctx, u, id, false)
	if err != nil {
		return err
	}
	rp, err := director.ReadRepayment(doc)
	if err != nil {
		return err
	}
	c, err := u.Contacts().Get(ctx, rp.ContactID)
	if err != nil {
		return err
	}
	if c.MemberUserID != nil && *c.MemberUserID == signer {
		return problem(approval.CodeBeneficiary, "The repayment is yours, so another person approves its void (FM-9).",
			"Ask another person who holds the approval right on payment batches")
	}
	return nil
}

// executeRepaymentVoid voids a repayment on its approval, on the date asked, with the reason.
func (a *API) executeRepaymentVoid(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, s stored) error {
	id, err := uuid.Parse(r.ObjectKey)
	if err != nil {
		return err
	}
	date, err := time.Parse(time.DateOnly, fmt.Sprint(s.Execute["date"]))
	if err != nil {
		return err
	}
	reason := fmt.Sprint(s.Execute["reason"])
	_, err = a.repayments.VoidIn(storage.WithReason(ctx, reason), u, id, date, reason)
	return err
}

func registerRepayments(a *API) {
	register(a, authz.GetDirectorRepayment, huma.Operation{
		OperationID: "getDirectorRepayment", Method: http.MethodGet, Path: "/v1/director-repayments/{repayment_id}", Tags: []string{"Founders"},
		Summary: "Get a repayment to a director",
		Description: "What a payment batch's release repaid a director or shareholder (FR-EQ-05, FR-BNK-12): the settlements it repaid, the " +
			"batch, whether bank import matched it, and the lines it posted (Dr 2500 or 2120 naming the person, Cr the bank). Read-only; no " +
			"side effects.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *DirectorRepaymentPath) (*repaymentReadOutput, error) {
		id, err := parseID("repayment_id", in.RepaymentID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out repaymentReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := repaymentDoc(ctx, u, id, false)
			if err != nil {
				return err
			}
			out.Body, err = a.repaymentOf(ctx, u, doc)
			out.ETag = repaymentETag(doc)
			return err
		})
		return &out, err
	})

	register(a, authz.RequestDirectorRepaymentVoid, huma.Operation{
		OperationID: "requestDirectorRepaymentVoid", Method: http.MethodPost, Path: "/v1/director-repayments/{repayment_id}/void-approval",
		Tags: []string{"Founders"}, Summary: "Ask for approval to void a repayment to a director", DefaultStatus: http.StatusAccepted,
		Description: "Asks another person's approval to void a repayment, as when the bank rejected it (FR-GL-12, FM-1), in every team mode: the " +
			"answer is 202 with an approval request, which a person who holds A on \"Payment batches\" approves, stepped up, never the one " +
			"who asked nor the person repaid (APR-15, FM-9). On the approval it is voided on the date given: a reversing entry and " +
			"reversing allocations, so its settlements are open for the next batch. Needs a person signed in who passed step-up, and " +
			"If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		CodeSessionRequired, CodeStepUpRequired, approval.CodePending, storage.CodeApproval, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		storage.CodeRetry, storage.CodeLockTimeout}, RepaymentApprovalCodes()...),
		func(ctx context.Context, in *repaymentVoidInput) (*billRequestOutput, error) {
			id, err := parseID("repayment_id", in.RepaymentID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			date, err := time.Parse(time.DateOnly, in.Body.Date)
			if err != nil {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.date", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"}})
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
				doc, err := repaymentDoc(ctx, u, id, true)
				if err != nil {
					return Approval{}, err
				}
				if err := checkIfMatch(in.IfMatch, repaymentETag(doc)); err != nil {
					return Approval{}, err
				}
				r, err := a.repaymentOf(ctx, u, doc)
				if err != nil {
					return Approval{}, err
				}
				if doc.Status != storage.DocumentPosted {
					return Approval{}, &director.Error{Code: director.CodeNotPosted, Message: "the repayment is voided already", Fix: "Nothing to void"}
				}
				if d, _ := time.Parse(time.DateOnly, r.Date); date.Before(d) {
					return Approval{}, &director.Error{Code: director.CodeNotPosted, Message: fmt.Sprintf("the void is dated %s, before the repayment's %s", in.Body.Date, r.Date),
						Fix: "Date the void on or after the repayment"}
				}
				diff := []ApprovalDiff{{Field: "status", Before: "posted", After: "reversed"}, {Field: "reason", After: in.Body.Reason}}
				for _, x := range r.Settlements {
					diff = append(diff, ApprovalDiff{Field: "open " + x.SettlementNumber, After: "+" + x.Amount.Amount + " " + x.Amount.Currency})
				}
				return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectDirectorRepayment, ObjectKey: id.String(), Operation: approval.OpRepaymentVoid,
					Summary: fmt.Sprintf("Void repayment %s to %s, %s %s", doc.Number, r.LegalName, r.Total.Amount, r.Total.Currency),
					Execute: map[string]any{"repayment_id": id.String(), "date": in.Body.Date, "reason": in.Body.Reason}, Diff: diff,
					Amounts: []approval.Amount{{Amount: r.Total.Amount, Currency: r.Total.Currency}}})
			})
			if err != nil {
				return nil, err
			}
			return &billRequestOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})
}
