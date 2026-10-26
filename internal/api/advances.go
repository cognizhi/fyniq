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
	"github.com/cognizhi/fyniq/internal/domain/claim"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/team"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ cash advances to members (FR-AP-17; p1-expense-claims)
//
// Money advanced to a member before a trip is recorded by hand and posted at once by cash_advance@1 (Dr the advances
// account, 1250 · Cr the money account); the claims the member marks as settling it credit 1250 up to what is open on
// it, and what is left is refunded (Dr the money account · Cr 1250), each matched from the bank as money recorded by
// hand (UC-EXP-03, design D8). It is recorded under the "Payment batches" row, as a reimbursement is (the owner's
// decision of 26 Oct 2026, rev 0.148): the Accountant, the AP specialist; in Founder mode a void is asked for and
// approved by another person who holds A there (the Controller, FM-1). It is read under "Expense claims", a member
// seeing their own, so they can choose the advance their claim settles.

// CodeAdvanceVoidApproval is a void of an advance that needs a second person in Founder mode (FM-1).
const CodeAdvanceVoidApproval = "CLAIM-33"

// AdvanceCodes are the codes the rule and an advance's own checks answer.
var AdvanceCodes = []string{claim.CodeAdvancePerson, claim.CodeAdvanceRefund, claim.CodeTooLong, ledger.CodeAdvance, ledger.CodeNotMoneyAccount,
	ledger.CodePaymentAmount, ledger.CodeUnknownAccount, ledger.CodeNoDate}

// AdvanceApprovalCodes are the codes an approval of an advance's void may answer.
func AdvanceApprovalCodes() []string {
	return []string{claim.CodeNotAdvance, claim.CodeAdvanceSettled, claim.CodeBadDraft}
}

// CashAdvanceDraft is an advance as recorded.
type CashAdvanceDraft struct {
	PersonID       string `json:"person_id" format:"uuid" doc:"Who it is paid to: a member's user ID, whose claims settle it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	Date           string `json:"date" format:"date" doc:"The day the money left the account." example:"2026-10-01"`
	MoneyAccountID string `json:"money_account_id" format:"uuid" doc:"The bank, card or wallet account it left: an active money account in the base currency." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d10"`
	Amount         Money  `json:"amount" doc:"What was advanced, in the base currency."`
	Reference      string `json:"reference,omitempty" maxLength:"140" doc:"The bank's reference for it." example:"FAST 77120"`
	Notes          string `json:"notes,omitempty" maxLength:"2000" doc:"Notes kept with it: what it is for." example:"Jakarta trip, 6 to 9 October"`
}

// CashAdvanceRefundDraft is what is left of an advance, refunded by its member.
type CashAdvanceRefundDraft struct {
	Date           string `json:"date" format:"date" doc:"The day the money came in." example:"2026-10-14"`
	MoneyAccountID string `json:"money_account_id" format:"uuid" doc:"The bank, card or wallet account it came into: an active money account in the base currency." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d10"`
	Amount         Money  `json:"amount" doc:"What was refunded, in the base currency: at most what is still open on the advance."`
	Reference      string `json:"reference,omitempty" maxLength:"140" doc:"The bank's reference for it." example:"FAST 80211"`
	Notes          string `json:"notes,omitempty" maxLength:"2000" doc:"Notes kept with it." example:"Balance of the Jakarta advance"`
}

// CashAdvanceRef names an advance.
type CashAdvanceRef struct {
	ID     string `json:"id" format:"uuid" doc:"The advance's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fa1"`
	Number string `json:"number,omitempty" doc:"Its number." example:"ADV-0001"`
}

// CashAdvanceSettlement is what one claim or refund settled of an advance.
type CashAdvanceSettlement struct {
	DocumentID string `json:"document_id" format:"uuid" doc:"The claim or refund." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fd1"`
	Type       string `json:"type" enum:"expense_claim,cash_advance" doc:"expense_claim for a claim that settled it; cash_advance for a refund." example:"expense_claim"`
	Number     string `json:"number,omitempty" doc:"Its number." example:"CLM-0007"`
	Date       string `json:"date" format:"date" doc:"The day it settled the advance: the claim's or the refund's date." example:"2026-10-10"`
	Amount     Money  `json:"amount" doc:"What it settled, in the base currency."`
}

// CashAdvance is money advanced to a member, or what is left of it refunded.
type CashAdvance struct {
	ID             string                  `json:"id" format:"uuid" doc:"Its ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fa1"`
	Number         string                  `json:"number,omitempty" doc:"Its number from the advance sequence (FR-ORG-04)." example:"ADV-0001"`
	Kind           string                  `json:"kind" enum:"advance,refund" doc:"advance: money paid to the member; refund: what is left of an advance coming back." example:"advance"`
	Status         string                  `json:"status" enum:"posted,reversed" doc:"posted once recorded; reversed by a void." example:"posted"`
	Version        int                     `json:"version" doc:"The version of its content; with the status it makes the ETag." example:"1"`
	Person         Person                  `json:"person" doc:"The member it was paid to, or who refunded it."`
	Refunds        *CashAdvanceRef         `json:"refunds,omitempty" doc:"A refund: the advance it refunds."`
	Date           string                  `json:"date,omitempty" format:"date" doc:"The day the money moved." example:"2026-10-01"`
	MoneyAccountID string                  `json:"money_account_id" format:"uuid" doc:"The account it moved through." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d10"`
	Amount         Money                   `json:"amount" doc:"What moved, in the base currency."`
	Reference      string                  `json:"reference,omitempty" doc:"The bank's reference." example:"FAST 77120"`
	Notes          string                  `json:"notes,omitempty" doc:"Its notes." example:"Jakarta trip, 6 to 9 October"`
	Settled        *Money                  `json:"settled,omitempty" doc:"A posted advance: what the member's claims settled of it."`
	Refunded       *Money                  `json:"refunded,omitempty" doc:"A posted advance: what the member refunded of it, net of voids."`
	Open           *Money                  `json:"open,omitempty" doc:"A posted advance: what is still open on it, which the member's next claims settle or they refund."`
	SettledBy      []CashAdvanceSettlement `json:"settled_by" nullable:"false" doc:"getCashAdvance only, for an advance: each claim and refund that settled it, net of voids."`
	Checks         []BillCheck             `json:"checks" nullable:"false" doc:"Its warnings (⚠): an advance still open more than the claim deadline after it was paid."`
	LedgerImpact   []EntryLine             `json:"ledger_impact" nullable:"false" doc:"getCashAdvance only: the lines its entry wrote: debits positive."`
	RecordedBy     *Person                 `json:"recorded_by,omitempty" doc:"Who recorded it."`
	RecordedAt     *time.Time              `json:"recorded_at,omitempty" doc:"When it was recorded." example:"2026-10-01T09:30:00Z"`
	ApprovalAsked  string                  `json:"approval_asked,omitempty" format:"uuid" doc:"The pending request to void it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4dd1"`
}

// CashAdvancePage is a page of advances and refunds, newest date first.
type CashAdvancePage struct {
	Items      []CashAdvance `json:"items" nullable:"false"`
	NextCursor string        `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjYtMTAtMDF8MDE5MiJ9"`
}

// CashAdvancePath names an advance or a refund in the path.
type CashAdvancePath struct {
	AdvanceID string `path:"advance_id" format:"uuid" doc:"The advance's (or the refund's) ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fa1"`
}

type advanceListInput struct {
	PageQuery
	PersonID string `query:"person_id" format:"uuid" doc:"Only the advances and refunds of this member." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	Open     bool   `query:"open" doc:"Only posted advances with something still open: those a claim can settle." example:"true"`
}

type advanceListOutput struct{ Body CashAdvancePage }

type recordAdvanceInput struct {
	KeyHeader
	PreferHeader
	Body CashAdvanceDraft
}

type recordAdvanceRefundInput struct {
	CashAdvancePath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body CashAdvanceRefundDraft
}

type advanceCommandInput struct {
	CashAdvancePath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body Reversal
}

type advanceOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"Its version and status, for If-Match."`
	Location string `header:"Location" doc:"Its URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved, and it shows what it would post (FR-INT-14)."`
	Body     CashAdvance
}

type advanceReadOutput struct {
	ETag string `header:"ETag" doc:"Its version and status, for If-Match."`
	Body CashAdvance
}

func advanceETag(c CashAdvance) string { return etag(c.Version, c.Status) }

// advanceMoneyIn reads the date, the money account and the amount every advance and refund carries, with every invalid
// field at once.
func advanceMoneyIn(date, account string, amount Money, a *claim.Advance) []FieldProblem {
	var fields []FieldProblem
	id, err := uuid.Parse(account)
	if err != nil {
		fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.money_account_id", Message: "is not an ID", Fix: "Send the money account's ID"})
	}
	a.AccountID = id
	d, err := time.Parse(time.DateOnly, date)
	if err != nil {
		fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.date", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"})
	}
	a.Date = d
	m, bad := moneyIn(amount, "body.amount")
	if bad != nil {
		fields = append(fields, *bad)
	}
	a.Amount = m
	return fields
}

// advanceView is what showing advances needs once: the base currency, the organisation's today, and each one's pending
// request to void it.
type advanceView struct {
	base  money.Currency
	today time.Time
	asked map[string]string
}

func (a *API) advanceView(ctx context.Context, u storage.UnitOfWork) (advanceView, error) {
	o, err := u.Organisation(ctx)
	if err != nil {
		return advanceView{}, err
	}
	v := advanceView{asked: map[string]string{}}
	if v.base, err = money.Lookup(o.BaseCurrency); err != nil {
		return advanceView{}, err
	}
	if v.today, err = orgToday(ctx, a, u); err != nil {
		return advanceView{}, err
	}
	requests, err := u.Approvals().List(ctx, string(approval.Pending))
	if err != nil {
		return advanceView{}, err
	}
	now := a.deps.Clock.Now()
	for _, r := range requests {
		if r.ObjectType == approval.ObjectCashAdvance && approval.StatusAt(requestOf(r), now) == approval.Pending {
			v.asked[r.ObjectKey] = r.ID.String()
		}
	}
	return v, nil
}

// advanceOf is an advance or refund as the API shows it. withDetail fills an advance's settlements and its ledger
// impact (getCashAdvance).
func (a *API) advanceOf(ctx context.Context, u storage.UnitOfWork, v advanceView, doc storage.Document, withDetail bool) (CashAdvance, error) {
	x, err := claim.ReadAdvance(doc)
	if err != nil {
		return CashAdvance{}, err
	}
	out := CashAdvance{ID: doc.ID.String(), Number: doc.Number, Kind: "advance", Status: string(doc.Status), Version: doc.Version,
		Person: Person{Kind: "user", ID: x.PersonID.String()}, Date: dayText(x.Date), MoneyAccountID: x.AccountID.String(), Amount: moneyOut(x.Amount),
		Reference: x.Reference, Notes: x.Notes, SettledBy: []CashAdvanceSettlement{}, Checks: []BillCheck{}, LedgerImpact: []EntryLine{},
		RecordedBy: personOf(doc.PostedBy), RecordedAt: doc.PostedAt, ApprovalAsked: v.asked[doc.ID.String()]}
	if x.Refunds != nil {
		out.Kind = "refund"
		out.Refunds = &CashAdvanceRef{ID: x.Refunds.String()}
		if d, err := u.Documents().Get(ctx, *x.Refunds); err == nil {
			out.Refunds.Number = d.Number
		}
	}
	balances, err := claim.AdvanceBalances(ctx, u, []storage.Document{doc}, v.base, time.Time{})
	if err != nil {
		return CashAdvance{}, err
	}
	if b, ok := balances[doc.ID]; ok { // a posted advance
		settled, refunded, open := moneyOut(b.Settled), moneyOut(b.Refunded), moneyOut(b.Open)
		out.Settled, out.Refunded, out.Open = &settled, &refunded, &open
		warnings, err := claim.AdvanceWarnings(ctx, u, x, b, v.today)
		if err != nil {
			return CashAdvance{}, err
		}
		for _, w := range warnings {
			out.Checks = append(out.Checks, BillCheck{Severity: "warning", Code: w.Code, Field: w.Field, Message: w.Message, Fix: w.Fix})
		}
		if withDetail {
			if out.SettledBy, err = settlementsOf(ctx, u, doc.ID, v.base); err != nil {
				return CashAdvance{}, err
			}
		}
	}
	if withDetail {
		e, err := u.Ledger().ByIdentity(ctx, ledger.CashAdvanceIdentity(doc.ID, doc.Version, ledger.RuleCashAdvance))
		if err != nil {
			return CashAdvance{}, err
		}
		out.LedgerImpact = entryOf(e).Lines
	}
	return out, a.name(ctx, u, &out.Person, out.RecordedBy)
}

// isOpen says whether an advance as shown is a posted advance with something still open on it.
func isOpen(x CashAdvance, base money.Currency) bool {
	if x.Open == nil {
		return false
	}
	open, err := money.ParseStored(x.Open.Amount, base)
	return err == nil && open.Sign() > 0
}

// settlementsOf are the claims and refunds that settled an advance, oldest first: each allocation to it that no void
// reversed.
func settlementsOf(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, base money.Currency) ([]CashAdvanceSettlement, error) {
	allocs, err := u.Allocations().OfDocument(ctx, id)
	if err != nil {
		return nil, err
	}
	reversed := map[uuid.UUID]bool{}
	for _, x := range allocs {
		if x.ReversesID != nil {
			reversed[*x.ReversesID] = true
		}
	}
	out := []CashAdvanceSettlement{}
	for _, x := range allocs {
		if x.ReversesID != nil || reversed[x.ID] {
			continue
		}
		src, err := u.Documents().Get(ctx, x.SourceID)
		if err != nil {
			return nil, err
		}
		m, err := money.ParseStored(string(x.Amount), base)
		if err != nil {
			return nil, err
		}
		out = append(out, CashAdvanceSettlement{DocumentID: src.ID.String(), Type: string(src.Type), Number: src.Number, Date: dateOf(x.On), Amount: moneyOut(m)})
	}
	return out, nil
}

// advanceDependencies are what an advance's envelope depends on: its status, and its amount.
func (a *API) advanceDependencies(ctx context.Context, u storage.UnitOfWork, key string) (int, map[string]any, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return 0, nil, err
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return 0, nil, err
	}
	x, err := claim.ReadAdvance(doc)
	if err != nil {
		return 0, nil, err
	}
	return doc.Version, map[string]any{"status": string(doc.Status), "amount": x.Amount.String() + " " + x.Amount.Currency().Code}, nil
}

// executeAdvanceVoid voids the advance or refund as asked, on the approval.
func (a *API) executeAdvanceVoid(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, s stored) error {
	id, err := uuid.Parse(r.ObjectKey)
	if err != nil {
		return err
	}
	date, err := time.Parse(time.DateOnly, fmt.Sprint(s.Execute["date"]))
	if err != nil {
		return err
	}
	_, err = a.advances.VoidIn(storage.WithReason(ctx, fmt.Sprint(s.Execute["reason"])), u, id, date, fmt.Sprint(s.Execute["reason"]))
	return err
}

func registerAdvances(a *API) {
	readCodes := []string{CodeNotFound, claim.CodeNotAdvance, claim.CodeBadDraft}

	register(a, authz.ListCashAdvances, huma.Operation{
		OperationID: "listCashAdvances", Method: http.MethodGet, Path: "/v1/cash-advances", Tags: []string{"Expense claims"},
		Summary: "List cash advances and their refunds",
		Description: "Money advanced to members and what they refunded, newest date first, a page at a time, narrowed by member or to the " +
			"advances still open (FR-AP-17): each with its member, the account it moved through, and for an advance what claims " +
			"settled, what was refunded and what is still open, with ⚠ when it is open more than the claim deadline after it was " +
			"paid. A person whose right on \"Expense claims\" is their own claims only (an Employee) gets just their own: the " +
			"advances their claim can settle. Read-only; no side effects.",
	}, append([]string{CodeCursor, CodeInvalid}, readCodes...), func(ctx context.Context, in *advanceListInput) (*advanceListOutput, error) {
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		afterDate, afterID, err := journalCursor(after)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		person := in.PersonID
		if ownClaimsOnly(p) {
			person = p.UserID.String()
		}
		out := advanceListOutput{Body: CashAdvancePage{Items: []CashAdvance{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			v, err := a.advanceView(ctx, u)
			if err != nil {
				return err
			}
			for {
				docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentCashAdvance, AfterDate: afterDate, AfterID: afterID, Limit: 500})
				if err != nil {
					return err
				}
				for _, doc := range docs {
					afterDate, afterID = doc.Date, &doc.ID
					if len(out.Body.Items) == in.PageSize {
						last := out.Body.Items[len(out.Body.Items)-1]
						out.Body.NextCursor = encodeCursor(last.Date + "|" + last.ID)
						return nil
					}
					x, err := a.advanceOf(ctx, u, v, doc, false)
					if err != nil {
						return err
					}
					if person != "" && x.Person.ID != person || in.Open && !isOpen(x, v.base) {
						continue
					}
					out.Body.Items = append(out.Body.Items, x)
				}
				if len(docs) < 500 {
					return nil
				}
			}
		})
		return &out, err
	})

	register(a, authz.GetCashAdvance, huma.Operation{
		OperationID: "getCashAdvance", Method: http.MethodGet, Path: "/v1/cash-advances/{advance_id}", Tags: []string{"Expense claims"},
		Summary: "Get a cash advance or its refund",
		Description: "Returns an advance or a refund with its ETag: its member, the account it moved through, the lines its entry wrote, " +
			"the pending request to void it, and for an advance each claim and refund that settled it and what is still open. An " +
			"Employee gets only their own. Read-only; no side effects.",
	}, readCodes, func(ctx context.Context, in *CashAdvancePath) (*advanceReadOutput, error) {
		id, err := parseID("advance_id", in.AdvanceID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var x CashAdvance
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Get(ctx, id)
			if err != nil {
				return err
			}
			v, err := a.advanceView(ctx, u)
			if err != nil {
				return err
			}
			if x, err = a.advanceOf(ctx, u, v, doc, true); err != nil {
				return err
			}
			if x.Person.ID != p.UserID.String() && ownClaimsOnly(p) {
				return ports.ErrNotFound
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return &advanceReadOutput{ETag: advanceETag(x), Body: x}, nil
	})

	recordCodes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		money.CodeNotDecimal, money.CodePrecision, money.CodeRange, money.CodeCurrency, team.CodeAction}, append(postingCodes(), AdvanceCodes...)...)
	recorded := func(ctx context.Context, u storage.UnitOfWork, x claim.Advance) (CashAdvance, error) {
		rec, err := a.advances.RecordIn(ctx, u, x)
		if err != nil {
			return CashAdvance{}, err
		}
		v, err := a.advanceView(ctx, u)
		if err != nil {
			return CashAdvance{}, err
		}
		doc, err := u.Documents().Get(ctx, rec.Advance.ID)
		if err != nil {
			return CashAdvance{}, err
		}
		return a.advanceOf(ctx, u, v, doc, true)
	}
	answer := func(w written[CashAdvance]) *advanceOutput {
		if w.Previewed {
			return &advanceOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
		}
		return &advanceOutput{Status: w.Status, ETag: advanceETag(w.Body), Location: "/v1/cash-advances/" + w.Body.ID, Replayed: replayedHeader(w.Replayed),
			Body: w.Body}
	}
	register(a, authz.RecordCashAdvance, huma.Operation{
		OperationID: "recordCashAdvance", Method: http.MethodPost, Path: "/v1/cash-advances", Tags: []string{"Expense claims"},
		DefaultStatus: http.StatusCreated, Summary: "Record a cash advance to a member",
		Description: "Records money advanced to a member before a trip, by hand, and posts it at once under cash_advance@1 (FR-AP-17, " +
			"UC-EXP-03): Dr the advances account (the setting claims.advances_account, 1250 Staff & director advances), Cr the money " +
			"account, in the base currency. The member's claims that name it settle it, up to what is open on it; what is left is " +
			"refunded (recordCashAdvanceRefund). Bank import matches the bank line to it as money recorded by hand. Side effects: the " +
			"advance, its number (ADV-), the entry, audit events. Every problem comes back together. Idempotent with Idempotency-Key; " +
			"Prefer: return=preview posts nothing and shows the entry.",
	}, recordCodes, func(ctx context.Context, in *recordAdvanceInput) (*advanceOutput, error) {
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (CashAdvance, error) {
			x := claim.Advance{Reference: strings.TrimSpace(in.Body.Reference), Notes: in.Body.Notes}
			fields := advanceMoneyIn(in.Body.Date, in.Body.MoneyAccountID, in.Body.Amount, &x)
			id, err := uuid.Parse(in.Body.PersonID)
			if err != nil {
				fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.person_id", Message: "is not an ID", Fix: "Send the member's user ID"})
			}
			x.PersonID = id
			if len(fields) > 0 {
				return CashAdvance{}, invalid(fields)
			}
			return recorded(ctx, u, x)
		})
		if err != nil {
			return nil, err
		}
		return answer(w), nil
	})

	register(a, authz.RecordCashAdvanceRefund, huma.Operation{
		OperationID: "recordCashAdvanceRefund", Method: http.MethodPost, Path: "/v1/cash-advances/{advance_id}/refunds", Tags: []string{"Expense claims"},
		DefaultStatus: http.StatusCreated, Summary: "Record the refund of what is left of a cash advance",
		Description: "Records what is left of an advance, refunded by its member, and posts it at once under cash_advance@1 (FR-AP-17, " +
			"UC-EXP-03): Dr the money account, Cr the advances account, in the base currency, with an allocation to the advance. It " +
			"refunds at most what is still open on the advance, and its member is the advance's. Bank import matches the bank line to " +
			"it as money recorded by hand. Side effects: the refund, its number (ADV-), the entry, the allocation, audit events. Every " +
			"problem comes back together. Needs If-Match with the advance's ETag. Idempotent with Idempotency-Key; Prefer: " +
			"return=preview posts nothing and shows the entry.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, claim.CodeNotAdvance, claim.CodeBadDraft}, recordCodes...), func(ctx context.Context, in *recordAdvanceRefundInput) (*advanceOutput, error) {
		advance, err := parseID("advance_id", in.AdvanceID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (CashAdvance, error) {
			doc, err := u.Documents().Get(ctx, advance)
			if err != nil {
				return CashAdvance{}, err
			}
			if err := checkIfMatch(in.IfMatch, etag(doc.Version, string(doc.Status))); err != nil {
				return CashAdvance{}, err
			}
			of, err := claim.ReadAdvance(doc)
			if err != nil {
				return CashAdvance{}, err
			}
			x := claim.Advance{PersonID: of.PersonID, Refunds: &advance, Reference: strings.TrimSpace(in.Body.Reference), Notes: in.Body.Notes}
			if fields := advanceMoneyIn(in.Body.Date, in.Body.MoneyAccountID, in.Body.Amount, &x); len(fields) > 0 {
				return CashAdvance{}, invalid(fields)
			}
			return recorded(ctx, u, x)
		})
		if err != nil {
			return nil, err
		}
		return answer(w), nil
	})

	voidCodes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeAdvanceVoidApproval, team.CodeAction}, append(postingCodes(), AdvanceApprovalCodes()...)...)
	locked := func(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, ifMatch string) (storage.Document, CashAdvance, error) {
		doc, err := u.Documents().Lock(ctx, id)
		if err != nil {
			return doc, CashAdvance{}, err
		}
		v, err := a.advanceView(ctx, u)
		if err != nil {
			return doc, CashAdvance{}, err
		}
		x, err := a.advanceOf(ctx, u, v, doc, false)
		if err != nil {
			return doc, x, err
		}
		if doc.Status != storage.DocumentPosted {
			return doc, x, &claim.Error{Code: claim.CodeAdvanceSettled, Message: fmt.Sprintf("the advance is %s, not posted", doc.Status),
				Fix: "Only a posted advance or refund is voided"}
		}
		return doc, x, checkIfMatch(ifMatch, advanceETag(x))
	}
	register(a, authz.VoidCashAdvance, huma.Operation{
		OperationID: "voidCashAdvance", Method: http.MethodPost, Path: "/v1/cash-advances/{advance_id}/void", Tags: []string{"Expense claims"},
		Summary: "Void a cash advance or its refund",
		Description: "Voids an advance or a refund with a reversing entry on the date given, with a reason (FR-GL-12); a refund's " +
			"allocation is reversed too, which reopens the advance. An advance that claims or refunds settled is not voided " +
			"(CLAIM-31): void its refunds first. In Founder mode voiding needs a second person (FM-1): it is refused (CLAIM-33), and " +
			"requestCashAdvanceVoid asks for the approval. Side effects: the reversing entry and allocation, audit events with the " +
			"reason. Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview writes nothing.",
	}, voidCodes, func(ctx context.Context, in *advanceCommandInput) (*advanceOutput, error) {
		id, err := parseID("advance_id", in.AdvanceID)
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
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (CashAdvance, error) {
			if _, _, err := locked(ctx, u, id, in.IfMatch); err != nil {
				return CashAdvance{}, err
			}
			mode, _, _, err := teamModeOf(ctx, u)
			if err != nil {
				return CashAdvance{}, err
			}
			if mode == team.Founder {
				return CashAdvance{}, problem(CodeAdvanceVoidApproval, "In Founder mode, voiding a cash advance or its refund needs a second person (FM-1).",
					"Ask for another person's approval to void it")
			}
			rec, err := a.advances.VoidIn(storage.WithReason(ctx, in.Body.Reason), u, id, date, in.Body.Reason)
			if err != nil {
				return CashAdvance{}, err
			}
			v, err := a.advanceView(ctx, u)
			if err != nil {
				return CashAdvance{}, err
			}
			return a.advanceOf(ctx, u, v, rec.Advance, true)
		})
		if err != nil {
			return nil, err
		}
		return &advanceOutput{Status: http.StatusOK, ETag: advanceETag(w.Body), Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	requestCodes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		CodeSessionRequired, CodeStepUpRequired, approval.CodePending, storage.CodeApproval, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		storage.CodeRetry, storage.CodeLockTimeout}, AdvanceApprovalCodes()...)
	register(a, authz.RequestCashAdvanceVoid, huma.Operation{
		OperationID: "requestCashAdvanceVoid", Method: http.MethodPost, Path: "/v1/cash-advances/{advance_id}/void-approval",
		Tags: []string{"Expense claims"}, Summary: "Ask for approval to void a cash advance or its refund", DefaultStatus: http.StatusAccepted,
		Description: "Asks for another person's approval to void an advance or a refund (FM-1): the answer is 202 with an approval request, " +
			"which a person who holds A on \"Payment batches\" (the Controller) and did not ask approves, stepped up; on the approval it " +
			"is voided on the date given, as voidCashAdvance does. Needs a person signed in who passed step-up, and If-Match with its " +
			"ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.",
	}, requestCodes, func(ctx context.Context, in *advanceCommandInput) (*billRequestOutput, error) {
		id, err := parseID("advance_id", in.AdvanceID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		if _, err := time.Parse(time.DateOnly, in.Body.Date); err != nil {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.date", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"}})
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
			doc, x, err := locked(ctx, u, id, in.IfMatch)
			if err != nil {
				return Approval{}, err
			}
			what := "advance"
			if x.Kind == "refund" {
				what = "refund of an advance"
			}
			return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectCashAdvance, ObjectKey: id.String(), Operation: approval.OpCashAdvanceVoid,
				Summary: fmt.Sprintf("Void %s %s, %s, %s %s", what, doc.Number, cmpOr(x.Person.Name, "a member"), x.Amount.Amount, x.Amount.Currency),
				Execute: map[string]any{"advance_id": id.String(), "date": in.Body.Date, "reason": in.Body.Reason},
				Diff:    []ApprovalDiff{{Field: "status", Before: "posted", After: "reversed"}, {Field: "reason", After: in.Body.Reason}}})
		})
		if err != nil {
			return nil, err
		}
		return &billRequestOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})
}
