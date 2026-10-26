package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/claim"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/team"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ reimbursements of claims (FR-AP-07; p1-expense-claims)
//
// Until payment batches exist, the money paid back to a claimant is recorded by hand and posted at once by
// claim_reimbursement@1 (Dr 2150 · Cr the money account), settling their claims; bank import matches it (the owner's
// decision of 26 Oct 2026, rev 0.147). It is recorded under the "Payment batches" row (the Accountant, the AP
// specialist), and in Founder mode a void is asked for and approved by another person who holds A there (the
// Controller, FM-1; rev 0.148). What the company owes each claimant is read from the claims and the reimbursements,
// and reconciled with 2150 (design D4).

// CodeReimbursementVoidApproval is a void that needs a second person in Founder mode (FM-1).
const CodeReimbursementVoidApproval = "CLAIM-17"

// ReimbursementCodes are the codes the rule and the reimbursement's own checks answer.
var ReimbursementCodes = []string{claim.CodeClaimant, claim.CodeNotOwed, claim.CodeOverpaid, claim.CodeTooLong, ledger.CodeNotMoneyAccount,
	ledger.CodePaymentAmount, ledger.CodeSystemRole, ledger.CodeUnknownAccount}

// ReimbursementApprovalCodes are the codes an approval of a reimbursement's void may answer.
func ReimbursementApprovalCodes() []string {
	return []string{claim.CodeNotReimbursement, claim.CodeNotPosted, claim.CodeVoidDate, claim.CodeBadDraft}
}

// ClaimReimbursementPart is what a reimbursement pays back of one claim.
type ClaimReimbursementPart struct {
	ClaimID     string `json:"claim_id" format:"uuid" doc:"The claim it pays back: an approved claim of the claimant." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fd1"`
	ClaimNumber string `json:"claim_number,omitempty" doc:"The claim's number, in answers." example:"CLM-0007"`
	Amount      Money  `json:"amount" doc:"What it pays back of the claim, in the base currency: at most what is still owed on it."`
}

// ClaimReimbursementDraft is a reimbursement as recorded.
type ClaimReimbursementDraft struct {
	ClaimantID     string                   `json:"claimant_id" format:"uuid" doc:"Whose claims it pays back: the claimant's user ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	Date           string                   `json:"date" format:"date" doc:"The day the money left the account." example:"2026-10-20"`
	MoneyAccountID string                   `json:"money_account_id" format:"uuid" doc:"The bank, card or wallet account it left: an active money account in the base currency." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d10"`
	Reference      string                   `json:"reference,omitempty" maxLength:"140" doc:"The bank's reference for it." example:"FAST 88231"`
	Claims         []ClaimReimbursementPart `json:"claims" maxItems:"100" doc:"What it pays back of each claim."`
	Notes          string                   `json:"notes,omitempty" maxLength:"2000" doc:"Notes kept with it." example:"October's claims"`
}

// ClaimReimbursement is money paid back to a claimant for their claims.
type ClaimReimbursement struct {
	ID             string                   `json:"id" format:"uuid" doc:"The reimbursement's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fe1"`
	Number         string                   `json:"number,omitempty" doc:"Its number from the reimbursement sequence (FR-ORG-04)." example:"RB-0003"`
	Status         string                   `json:"status" enum:"posted,reversed" doc:"posted once recorded; reversed by a void." example:"posted"`
	Version        int                      `json:"version" doc:"The version of its content; with the status it makes the ETag." example:"1"`
	Claimant       Person                   `json:"claimant" doc:"Whose claims it paid back."`
	Date           string                   `json:"date,omitempty" format:"date" doc:"The day the money left." example:"2026-10-20"`
	MoneyAccountID string                   `json:"money_account_id" format:"uuid" doc:"The account it left." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d10"`
	Reference      string                   `json:"reference,omitempty" doc:"The bank's reference." example:"FAST 88231"`
	Total          Money                    `json:"total" doc:"What it paid back, in the base currency."`
	Claims         []ClaimReimbursementPart `json:"claims" nullable:"false" doc:"What it paid back of each claim, as recorded."`
	Notes          string                   `json:"notes,omitempty" doc:"Its notes." example:"October's claims"`
	LedgerImpact   []EntryLine              `json:"ledger_impact" nullable:"false" doc:"getClaimReimbursement only: the lines its entry wrote: debits positive."`
	RecordedBy     *Person                  `json:"recorded_by,omitempty" doc:"Who recorded it."`
	RecordedAt     *time.Time               `json:"recorded_at,omitempty" doc:"When it was recorded." example:"2026-10-20T09:30:00Z"`
	ApprovalAsked  string                   `json:"approval_asked,omitempty" format:"uuid" doc:"The pending request to void it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4dd1"`
}

// ClaimReimbursementPage is a page of reimbursements, newest date first.
type ClaimReimbursementPage struct {
	Items      []ClaimReimbursement `json:"items" nullable:"false"`
	NextCursor string               `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjYtMTAtMjB8MDE5MiJ9"`
}

// ClaimReimbursementPath names a reimbursement in the path.
type ClaimReimbursementPath struct {
	ReimbursementID string `path:"reimbursement_id" format:"uuid" doc:"The reimbursement's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fe1"`
}

// ClaimOwed is where one claim stands with its claimant.
type ClaimOwed struct {
	ClaimID string `json:"claim_id" format:"uuid" doc:"The claim." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fd1"`
	Number  string `json:"number,omitempty" doc:"Its number." example:"CLM-0007"`
	Date    string `json:"date,omitempty" format:"date" doc:"Its date." example:"2026-10-15"`
	Owed    Money  `json:"owed" doc:"What its entry credited to 2150."`
	Paid    Money  `json:"paid" doc:"What reimbursements paid back of it, net of their voids."`
	Open    Money  `json:"open" doc:"What the company still owes for it."`
}

// ClaimantOwed is what the company owes one claimant: their approved claims not yet paid back in full.
type ClaimantOwed struct {
	Claimant Person      `json:"claimant" doc:"The claimant."`
	Open     Money       `json:"open" doc:"What the company owes them, in the base currency."`
	Claims   []ClaimOwed `json:"claims" nullable:"false" doc:"Their approved claims with something still owed, oldest first."`
}

// ClaimsOwed is the 2150 sub-ledger by person (design D4): what the company owes each claimant, and, for those who see
// everyone's, 2150's balance from the balance service and any difference, shown and never hidden (INV-07's pattern).
type ClaimsOwed struct {
	AsAt       string         `json:"as_at" format:"date" doc:"The day it is read as at: the claims dated by then, less what was paid back by then." example:"2026-10-26"`
	People     []ClaimantOwed `json:"people" nullable:"false" doc:"Each claimant the company owes, by name; for a person whose right is their own claims only, just themselves."`
	Total      Money          `json:"total" doc:"What the company owes them together."`
	Account    *Money         `json:"account_balance,omitempty" doc:"Everyone's only: 2150 Expense claims payable's balance through the day, as a credit."`
	Difference *Money         `json:"difference,omitempty" doc:"Everyone's only: the account's balance less the sub-ledger's total; anything but zero is something the claims do not explain, such as a manual journal to 2150."`
}

type reimbursementListInput struct {
	PageQuery
	ClaimantID string `query:"claimant_id" format:"uuid" doc:"Only reimbursements of this claimant's claims." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
}

type reimbursementListOutput struct{ Body ClaimReimbursementPage }

type recordReimbursementInput struct {
	KeyHeader
	PreferHeader
	Body ClaimReimbursementDraft
}

type reimbursementCommandInput struct {
	ClaimReimbursementPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body Reversal
}

type reimbursementOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The reimbursement's version and status, for If-Match."`
	Location string `header:"Location" doc:"The reimbursement's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved, and the reimbursement shows what it would post (FR-INT-14)."`
	Body     ClaimReimbursement
}

type reimbursementReadOutput struct {
	ETag string `header:"ETag" doc:"The reimbursement's version and status, for If-Match."`
	Body ClaimReimbursement
}

type claimsOwedInput struct {
	AsAt string `query:"as_at" format:"date" doc:"The day to read it as at; left out, the organisation's today." example:"2026-10-31"`
}

type claimsOwedOutput struct{ Body ClaimsOwed }

func reimbursementETag(r ClaimReimbursement) string { return etag(r.Version, r.Status) }

// reimbursementDraftOf reads a reimbursement from the request, with every invalid field at once.
func reimbursementDraftOf(in ClaimReimbursementDraft) (claim.Reimbursement, error) {
	r := claim.Reimbursement{Reference: strings.TrimSpace(in.Reference), Notes: in.Notes}
	var fields []FieldProblem
	for _, x := range []struct {
		text, field string
		to          *uuid.UUID
	}{{in.ClaimantID, "body.claimant_id", &r.ClaimantID}, {in.MoneyAccountID, "body.money_account_id", &r.AccountID}} {
		id, err := uuid.Parse(x.text)
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: x.field, Message: "is not an ID", Fix: "Send the ID"})
		}
		*x.to = id
	}
	d, err := time.Parse(time.DateOnly, in.Date)
	if err != nil {
		fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.date", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"})
	}
	r.Date = d
	for i, c := range in.Claims {
		at := func(f string) string { return "body.claims[" + strconv.Itoa(i) + "]." + f }
		id, err := uuid.Parse(c.ClaimID)
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: at("claim_id"), Message: "is not an ID", Fix: "Send the claim's ID"})
		}
		m, bad := moneyIn(c.Amount, at("amount"))
		if bad != nil {
			fields = append(fields, *bad)
			continue
		}
		r.Claims = append(r.Claims, claim.Part{ClaimID: id, Amount: m})
	}
	if len(fields) > 0 {
		return r, invalid(fields)
	}
	return r, nil
}

// reimbursementView is what showing reimbursements needs once: the base currency, each one's pending request to void
// it, and the claims' numbers.
type reimbursementView struct {
	base  money.Currency
	asked map[string]string
}

func (a *API) reimbursementView(ctx context.Context, u storage.UnitOfWork) (reimbursementView, error) {
	o, err := u.Organisation(ctx)
	if err != nil {
		return reimbursementView{}, err
	}
	v := reimbursementView{asked: map[string]string{}}
	if v.base, err = money.Lookup(o.BaseCurrency); err != nil {
		return reimbursementView{}, err
	}
	requests, err := u.Approvals().List(ctx, string(approval.Pending))
	if err != nil {
		return reimbursementView{}, err
	}
	now := a.deps.Clock.Now()
	for _, r := range requests {
		if r.ObjectType == approval.ObjectReimbursement && approval.StatusAt(requestOf(r), now) == approval.Pending {
			v.asked[r.ObjectKey] = r.ID.String()
		}
	}
	return v, nil
}

// reimbursementOf is a reimbursement as the API shows it. withImpact fills its ledger impact (getClaimReimbursement).
func (a *API) reimbursementOf(ctx context.Context, u storage.UnitOfWork, v reimbursementView, doc storage.Document, withImpact bool) (ClaimReimbursement, error) {
	r, err := claim.ReadReimbursement(doc)
	if err != nil {
		return ClaimReimbursement{}, err
	}
	out := ClaimReimbursement{ID: doc.ID.String(), Number: doc.Number, Status: string(doc.Status), Version: doc.Version,
		Claimant: Person{Kind: "user", ID: r.ClaimantID.String()}, Date: dayText(r.Date), MoneyAccountID: r.AccountID.String(), Reference: r.Reference,
		Notes: r.Notes, Claims: []ClaimReimbursementPart{}, LedgerImpact: []EntryLine{}, RecordedBy: personOf(doc.PostedBy), RecordedAt: doc.PostedAt,
		ApprovalAsked: v.asked[doc.ID.String()]}
	total := money.Zero(v.base)
	for _, c := range r.Claims {
		part := ClaimReimbursementPart{ClaimID: c.ClaimID.String(), Amount: moneyOut(c.Amount)}
		if d, err := u.Documents().Get(ctx, c.ClaimID); err == nil {
			part.ClaimNumber = d.Number
		}
		out.Claims = append(out.Claims, part)
		total, _ = total.Add(c.Amount)
	}
	out.Total = moneyOut(total)
	if withImpact && doc.Status != storage.DocumentDraft {
		e, err := u.Ledger().ByIdentity(ctx, ledger.ClaimReimbursementIdentity(doc.ID, doc.Version, ledger.RuleClaimReimbursement))
		if err != nil {
			return ClaimReimbursement{}, err
		}
		out.LedgerImpact = entryOf(e).Lines
	}
	return out, a.name(ctx, u, &out.Claimant, out.RecordedBy)
}

// reimbursementDependencies are what a reimbursement's envelope depends on: its status, and what it paid back.
func (a *API) reimbursementDependencies(ctx context.Context, u storage.UnitOfWork, key string) (int, map[string]any, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return 0, nil, err
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return 0, nil, err
	}
	v, err := a.reimbursementView(ctx, u)
	if err != nil {
		return 0, nil, err
	}
	r, err := a.reimbursementOf(ctx, u, v, doc, false)
	if err != nil {
		return 0, nil, err
	}
	return doc.Version, map[string]any{"status": string(doc.Status), "paid": r.Total.Amount + " " + r.Total.Currency}, nil
}

// executeReimbursementVoid voids the reimbursement as asked, on the approval.
func (a *API) executeReimbursementVoid(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, s stored) error {
	id, err := uuid.Parse(r.ObjectKey)
	if err != nil {
		return err
	}
	date, err := time.Parse(time.DateOnly, fmt.Sprint(s.Execute["date"]))
	if err != nil {
		return err
	}
	_, err = a.reimbursements.VoidIn(storage.WithReason(ctx, fmt.Sprint(s.Execute["reason"])), u, id, date, fmt.Sprint(s.Execute["reason"]))
	return err
}

// claimsOwed is the 2150 sub-ledger by person as p may see it, as at asAt (zero: the organisation's today).
func (a *API) claimsOwed(ctx context.Context, u storage.UnitOfWork, p authn.Principal, asAt time.Time) (ClaimsOwed, error) {
	o, err := u.Organisation(ctx)
	if err != nil {
		return ClaimsOwed{}, err
	}
	base, err := money.Lookup(o.BaseCurrency)
	if err != nil {
		return ClaimsOwed{}, err
	}
	today := asAt
	if today.IsZero() {
		if today, err = orgToday(ctx, a, u); err != nil {
			return ClaimsOwed{}, err
		}
	}
	own := ownClaimsOnly(p)
	var docs []storage.Document
	var afterDate string
	var afterID *uuid.UUID
	for {
		page, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentExpenseClaim, Status: storage.DocumentPosted, AfterDate: afterDate,
			AfterID: afterID, Limit: 500})
		if err != nil {
			return ClaimsOwed{}, err
		}
		docs = append(docs, page...)
		if len(page) < 500 {
			break
		}
		afterDate, afterID = page[len(page)-1].Date, &page[len(page)-1].ID
	}
	balances, err := claim.Balances(ctx, u, docs, base, today)
	if err != nil {
		return ClaimsOwed{}, err
	}
	out := ClaimsOwed{AsAt: dateOf(today), People: []ClaimantOwed{}}
	total := money.Zero(base)
	people := map[uuid.UUID]*ClaimantOwed{}
	var order []uuid.UUID
	slices.SortFunc(docs, func(x, y storage.Document) int { return strings.Compare(x.Date+x.ID.String(), y.Date+y.ID.String()) })
	for _, d := range docs {
		b, ok := balances[d.ID]
		if !ok {
			continue
		}
		total, _ = total.Add(b.Open)
		draft, err := claim.Read(d, base)
		if err != nil {
			return ClaimsOwed{}, err
		}
		if b.Open.IsZero() || own && draft.ClaimantID != p.UserID {
			continue
		}
		c := people[draft.ClaimantID]
		if c == nil {
			c = &ClaimantOwed{Claimant: Person{Kind: "user", ID: draft.ClaimantID.String()}, Open: moneyOut(money.Zero(base)), Claims: []ClaimOwed{}}
			people[draft.ClaimantID] = c
			order = append(order, draft.ClaimantID)
		}
		open, _ := money.ParseStored(c.Open.Amount, base)
		open, _ = open.Add(b.Open)
		c.Open = moneyOut(open)
		c.Claims = append(c.Claims, ClaimOwed{ClaimID: d.ID.String(), Number: d.Number, Date: d.Date, Owed: moneyOut(b.Owed), Paid: moneyOut(b.Settled),
			Open: moneyOut(b.Open)})
	}
	sum := money.Zero(base)
	for _, id := range order {
		c := people[id]
		if err := a.name(ctx, u, &c.Claimant); err != nil {
			return ClaimsOwed{}, err
		}
		x, _ := money.ParseStored(c.Open.Amount, base)
		sum, _ = sum.Add(x)
		out.People = append(out.People, *c)
	}
	slices.SortFunc(out.People, func(x, y ClaimantOwed) int { return strings.Compare(x.Claimant.Name, y.Claimant.Name) })
	out.Total = moneyOut(sum)
	if own {
		return out, nil
	}
	out.Total = moneyOut(total)
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return ClaimsOwed{}, err
	}
	for _, acc := range accounts {
		if string(acc.Role) != ledger.RoleExpenseClaimsPayable {
			continue
		}
		_, value, err := balance.Through(ctx, u, acc.ID, base, base, today)
		if err != nil {
			return ClaimsOwed{}, err
		}
		credit, diff := value.Neg(), money.Zero(base)
		diff, _ = credit.Sub(total)
		c, d := moneyOut(credit), moneyOut(diff)
		out.Account, out.Difference = &c, &d
	}
	return out, nil
}

func registerReimbursements(a *API) {
	readCodes := []string{CodeNotFound, claim.CodeNotReimbursement, claim.CodeBadDraft}

	register(a, authz.ListClaimReimbursements, huma.Operation{
		OperationID: "listClaimReimbursements", Method: http.MethodGet, Path: "/v1/claim-reimbursements", Tags: []string{"Expense claims"},
		Summary: "List reimbursements of expense claims",
		Description: "The reimbursements recorded by hand, newest date first, a page at a time, narrowed by claimant (FR-AP-07): each with " +
			"its claimant, the account it left, and what it paid back of each claim. Read-only; no side effects.",
	}, append([]string{CodeCursor, CodeInvalid}, readCodes...), func(ctx context.Context, in *reimbursementListInput) (*reimbursementListOutput, error) {
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		afterDate, afterID, err := journalCursor(after)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := reimbursementListOutput{Body: ClaimReimbursementPage{Items: []ClaimReimbursement{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			v, err := a.reimbursementView(ctx, u)
			if err != nil {
				return err
			}
			for {
				docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentClaimReimbursement, AfterDate: afterDate, AfterID: afterID, Limit: 500})
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
					r, err := a.reimbursementOf(ctx, u, v, doc, false)
					if err != nil {
						return err
					}
					if in.ClaimantID == "" || r.Claimant.ID == in.ClaimantID {
						out.Body.Items = append(out.Body.Items, r)
					}
				}
				if len(docs) < 500 {
					return nil
				}
			}
		})
		return &out, err
	})

	recordCodes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		money.CodeNotDecimal, money.CodePrecision, money.CodeRange, money.CodeCurrency, team.CodeAction}, append(postingCodes(), ReimbursementCodes...)...)
	register(a, authz.RecordClaimReimbursement, huma.Operation{
		OperationID: "recordClaimReimbursement", Method: http.MethodPost, Path: "/v1/claim-reimbursements", Tags: []string{"Expense claims"},
		DefaultStatus: http.StatusCreated, Summary: "Record a reimbursement of expense claims",
		Description: "Records money paid back to a claimant for their approved claims, by hand, and posts it at once under " +
			"claim_reimbursement@1 (FR-AP-07; the owner's decision of 26 Oct 2026, until payment batches): Dr 2150 Expense claims " +
			"payable, Cr the money account, in the base currency. Each claim it pays back must be the claimant's, approved, with at " +
			"least that much still owed on it. Bank import matches the bank line to it as money recorded by hand. Side effects: the " +
			"reimbursement, its number (RB-), the entry, an allocation to each claim, audit events. Every problem comes back together. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview posts nothing and shows the entry.",
	}, recordCodes, func(ctx context.Context, in *recordReimbursementInput) (*reimbursementOutput, error) {
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (ClaimReimbursement, error) {
			r, err := reimbursementDraftOf(in.Body)
			if err != nil {
				return ClaimReimbursement{}, err
			}
			rec, err := a.reimbursements.RecordIn(ctx, u, r)
			if err != nil {
				return ClaimReimbursement{}, err
			}
			v, err := a.reimbursementView(ctx, u)
			if err != nil {
				return ClaimReimbursement{}, err
			}
			rec.Reimbursement, err = u.Documents().Get(ctx, rec.Reimbursement.ID)
			if err != nil {
				return ClaimReimbursement{}, err
			}
			return a.reimbursementOf(ctx, u, v, rec.Reimbursement, true)
		})
		if err != nil {
			return nil, err
		}
		if w.Previewed {
			return &reimbursementOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
		}
		return &reimbursementOutput{Status: w.Status, ETag: reimbursementETag(w.Body), Location: "/v1/claim-reimbursements/" + w.Body.ID,
			Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	register(a, authz.GetClaimReimbursement, huma.Operation{
		OperationID: "getClaimReimbursement", Method: http.MethodGet, Path: "/v1/claim-reimbursements/{reimbursement_id}", Tags: []string{"Expense claims"},
		Summary: "Get a reimbursement of expense claims",
		Description: "Returns a reimbursement with its ETag: its claimant, the account it left, what it paid back of each claim, the lines " +
			"its entry wrote, and the pending request to void it. Read-only; no side effects.",
	}, readCodes, func(ctx context.Context, in *ClaimReimbursementPath) (*reimbursementReadOutput, error) {
		id, err := parseID("reimbursement_id", in.ReimbursementID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var r ClaimReimbursement
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Get(ctx, id)
			if err != nil {
				return err
			}
			v, err := a.reimbursementView(ctx, u)
			if err != nil {
				return err
			}
			r, err = a.reimbursementOf(ctx, u, v, doc, true)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &reimbursementReadOutput{ETag: reimbursementETag(r), Body: r}, nil
	})

	voidCodes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeReimbursementVoidApproval, team.CodeAction}, append(postingCodes(), ReimbursementApprovalCodes()...)...)
	locked := func(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, ifMatch string) (storage.Document, ClaimReimbursement, error) {
		doc, err := u.Documents().Lock(ctx, id)
		if err != nil {
			return doc, ClaimReimbursement{}, err
		}
		v, err := a.reimbursementView(ctx, u)
		if err != nil {
			return doc, ClaimReimbursement{}, err
		}
		r, err := a.reimbursementOf(ctx, u, v, doc, false)
		if err != nil {
			return doc, r, err
		}
		if doc.Status != storage.DocumentPosted {
			return doc, r, &claim.Error{Code: claim.CodeNotPosted, Message: fmt.Sprintf("the reimbursement is %s, not posted", doc.Status),
				Fix: "Only a posted reimbursement is voided"}
		}
		return doc, r, checkIfMatch(ifMatch, reimbursementETag(r))
	}
	register(a, authz.VoidClaimReimbursement, huma.Operation{
		OperationID: "voidClaimReimbursement", Method: http.MethodPost, Path: "/v1/claim-reimbursements/{reimbursement_id}/void", Tags: []string{"Expense claims"},
		Summary: "Void a reimbursement of expense claims",
		Description: "Voids a reimbursement with a reversing entry and reversing allocations on the date given, with a reason (FR-GL-12): the " +
			"claims are owed again. In Founder mode voiding needs a second person (FM-1): it is refused (CLAIM-17), and " +
			"requestClaimReimbursementVoid asks for the approval. Side effects: the reversing entry and allocations, audit events with " +
			"the reason. Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview writes nothing.",
	}, voidCodes, func(ctx context.Context, in *reimbursementCommandInput) (*reimbursementOutput, error) {
		id, err := parseID("reimbursement_id", in.ReimbursementID)
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
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (ClaimReimbursement, error) {
			if _, _, err := locked(ctx, u, id, in.IfMatch); err != nil {
				return ClaimReimbursement{}, err
			}
			mode, _, _, err := teamModeOf(ctx, u)
			if err != nil {
				return ClaimReimbursement{}, err
			}
			if mode == team.Founder {
				return ClaimReimbursement{}, problem(CodeReimbursementVoidApproval, "In Founder mode, voiding a reimbursement needs a second person (FM-1).",
					"Ask for another person's approval to void it")
			}
			rec, err := a.reimbursements.VoidIn(storage.WithReason(ctx, in.Body.Reason), u, id, date, in.Body.Reason)
			if err != nil {
				return ClaimReimbursement{}, err
			}
			v, err := a.reimbursementView(ctx, u)
			if err != nil {
				return ClaimReimbursement{}, err
			}
			return a.reimbursementOf(ctx, u, v, rec.Reimbursement, true)
		})
		if err != nil {
			return nil, err
		}
		return &reimbursementOutput{Status: http.StatusOK, ETag: reimbursementETag(w.Body), Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	requestCodes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		CodeSessionRequired, CodeStepUpRequired, approval.CodePending, storage.CodeApproval, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		storage.CodeRetry, storage.CodeLockTimeout}, ReimbursementApprovalCodes()...)
	register(a, authz.RequestClaimReimbursementVoid, huma.Operation{
		OperationID: "requestClaimReimbursementVoid", Method: http.MethodPost, Path: "/v1/claim-reimbursements/{reimbursement_id}/void-approval",
		Tags: []string{"Expense claims"}, Summary: "Ask for approval to void a reimbursement of expense claims", DefaultStatus: http.StatusAccepted,
		Description: "Asks for another person's approval to void a reimbursement (FM-1): the answer is 202 with an approval request, which a " +
			"person who holds A on \"Payment batches\" (the Controller) and did not ask approves, stepped up; on the approval it is voided " +
			"on the date given, as voidClaimReimbursement does. Needs a person signed in who passed step-up, and If-Match with its ETag. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.",
	}, requestCodes, func(ctx context.Context, in *reimbursementCommandInput) (*billRequestOutput, error) {
		id, err := parseID("reimbursement_id", in.ReimbursementID)
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
			doc, r, err := locked(ctx, u, id, in.IfMatch)
			if err != nil {
				return Approval{}, err
			}
			return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectReimbursement, ObjectKey: id.String(), Operation: approval.OpReimbursementVoid,
				Summary: fmt.Sprintf("Void reimbursement %s to %s, %s %s", doc.Number, cmpOr(r.Claimant.Name, "a claimant"), r.Total.Amount, r.Total.Currency),
				Execute: map[string]any{"reimbursement_id": id.String(), "date": in.Body.Date, "reason": in.Body.Reason},
				Diff:    []ApprovalDiff{{Field: "status", Before: "posted", After: "reversed"}, {Field: "reason", After: in.Body.Reason}}})
		})
		if err != nil {
			return nil, err
		}
		return &billRequestOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.GetExpenseClaimsOwed, huma.Operation{
		OperationID: "getExpenseClaimsOwed", Method: http.MethodGet, Path: "/v1/expense-claims/owed", Tags: []string{"Expense claims"},
		Summary: "What the company owes its claimants",
		Description: "The 2150 sub-ledger by person (FR-AP-14, design D4): each claimant's approved claims with something still owed, and " +
			"what the company owes them in all, as at a day (the organisation's today by default). A person whose right is their own claims only (an " +
			"Employee) gets just their own: what My claims shows as owed to them. Everyone's also shows 2150's balance from the balance " +
			"service and any difference from the claims, never hidden. Read-only; no side effects.",
	}, []string{CodeInvalid}, func(ctx context.Context, in *claimsOwedInput) (*claimsOwedOutput, error) {
		var asAt time.Time
		if in.AsAt != "" {
			var err error
			if asAt, err = time.Parse(time.DateOnly, in.AsAt); err != nil {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "as_at", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"}})
			}
		}
		p, _ := authn.From(ctx)
		var out ClaimsOwed
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			out, err = a.claimsOwed(ctx, u, p, asAt)
			return err
		})
		return &claimsOwedOutput{Body: out}, err
	})
}
