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
	"github.com/cognizhi/fyniq/internal/domain/director"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ settlements of founders' balances (FR-EQ-05, FR-EQ-07)
//
// What the company owes a director or shareholder on 2500 or 2120 is settled by a route (design D10): kept as a loan on
// terms, or waived (repaying comes with payment batches, converting with the register of share issues). An Owner or the
// Controller drafts it (A on "Director accounts") with its loan note, minute or deed, and asks for approval
// (director_settlement.approve), which another person who holds A gives, never the person settled (FM-9, APR-15; the
// database's FY253), and the Accountant too when every member who could sign is a director with a settlement of their
// own waiting or decided in the last 30 days ([accountant to confirm]). The approval keeps the loan with no entry, or
// posts the waiver: Dr 2500/2120 naming the person (waived) · Cr 3300 Capital contribution or an income account.

// SettlementCodes are the codes a settlement's own checks answer.
var SettlementCodes = []string{director.CodeSettlement, director.CodeNotPerson, director.CodeNotSettlement}

// SettlementTerms are a loan kept's terms.
type SettlementTerms struct {
	InterestRate string `json:"interest_rate,omitempty" doc:"The interest a year, as a percentage; 0, an interest-free loan, when left out." example:"0"`
	RepayOn      string `json:"repay_on,omitempty" format:"date" doc:"The day it is repaid; left out, it is repayable on demand." example:"2027-12-31"`
	Subordinated bool   `json:"subordinated,omitempty" doc:"Subordinated to the company's other creditors." example:"false"`
}

// SettlementWaiver is what a waiver credits.
type SettlementWaiver struct {
	CreditAccountID string `json:"credit_account_id" format:"uuid" doc:"The account it credits: 3300 Capital contribution when the person waives as a shareholder, or an income account the accountant chooses." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d33"`
	Capacity        string `json:"capacity" enum:"shareholder,other" doc:"The capacity in which the person waives it: as a shareholder (a capital contribution), or otherwise (income of the company)." example:"shareholder"`
	Reason          string `json:"reason" maxLength:"1000" doc:"Why the person waives it." example:"Waived ahead of the seed round"`
}

// DirectorSettlementDraft is a settlement as drafted.
type DirectorSettlementDraft struct {
	ContactID string            `json:"contact_id" format:"uuid" doc:"The director or shareholder the company owes." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	AccountID string            `json:"account_id" format:"uuid" doc:"The director's account it is owed on: 2500 or 2120." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d61"`
	Amount    Money             `json:"amount" doc:"The amount, in the base currency: at most what is owed on the account today."`
	Route     string            `json:"route,omitempty" enum:"keep,repay,convert,waive" doc:"keep as a loan on terms, or waive (FR-EQ-07); repay and convert are not built yet. Left out, the organisation's default route (founders.default_route), keep." example:"keep"`
	Terms     *SettlementTerms  `json:"terms,omitempty" doc:"A loan kept's terms."`
	Waiver    *SettlementWaiver `json:"waiver,omitempty" doc:"A waiver's account, capacity and reason."`
}

// DirectorSettlement is a settlement, drafted, waiting for approval, or decided.
type DirectorSettlement struct {
	ID               string            `json:"id" format:"uuid" doc:"Its ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fa9"`
	Number           string            `json:"number,omitempty" doc:"Its number, once decided (FR-ORG-04)." example:"DS-0001"`
	Status           string            `json:"status" enum:"draft,waiting,decided" doc:"draft; waiting for another person's approval; or decided: the loan kept, or the waiver posted." example:"waiting"`
	Version          int               `json:"version" doc:"The version of its content; with its status it makes the ETag." example:"1"`
	Date             string            `json:"date" format:"date" doc:"The day it was last drafted." example:"2026-11-03"`
	ContactID        string            `json:"contact_id" format:"uuid" doc:"The person." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	LegalName        string            `json:"legal_name" doc:"Their name." example:"Mei Lin Tan"`
	AccountID        string            `json:"account_id" format:"uuid" doc:"The director's account." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d61"`
	AccountCode      string            `json:"account_code,omitempty" doc:"Its code." example:"2500"`
	Amount           Money             `json:"amount" doc:"The amount settled."`
	Owed             Money             `json:"owed" doc:"What the company owes the person on the account today."`
	Route            string            `json:"route" enum:"keep,repay,convert,waive" doc:"The route." example:"keep"`
	Terms            *SettlementTerms  `json:"terms,omitempty" doc:"A loan kept's terms."`
	Waiver           *SettlementWaiver `json:"waiver,omitempty" doc:"A waiver's account, capacity and reason."`
	DeductedEarlier  bool              `json:"deducted_earlier" doc:"A decided waiver of amounts the company deducted earlier as costs (pay or fees on 2120): tagged for the tax computation [verify]." example:"false"`
	Agreements       int               `json:"agreements" doc:"How many files are attached and not removed: asking for approval needs one, the loan note, minute or deed." example:"1"`
	ApprovalAsked    string            `json:"approval_asked,omitempty" format:"uuid" doc:"The pending request to decide it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4dd1"`
	Checks           []BillCheck       `json:"checks" nullable:"false" doc:"A draft's statuses: ✖ while no agreement is attached, or while it does not fit what is owed now."`
	LedgerImpact     []EntryLine       `json:"ledger_impact" nullable:"false" doc:"A waiver's entry: the lines it would post as a dry run while undecided (FR-APR-02), and the lines it posted once decided; empty for a loan kept."`
	AccountantReview bool              `json:"accountant_review" doc:"Its request has a final step the Accountant signs, as every member who could sign benefits from a settlement of their own (FM-9) [accountant to confirm]." example:"false"`
}

// DirectorSettlementPage is a page of settlements, the latest first.
type DirectorSettlementPage struct {
	Items      []DirectorSettlement `json:"items" nullable:"false"`
	NextCursor string               `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjYtMTEtMDN8MDE5MiJ9"`
}

// DirectorSettlementPath names a settlement in the path.
type DirectorSettlementPath struct {
	SettlementID string `path:"settlement_id" format:"uuid" doc:"The settlement's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fa9"`
}

type settlementListInput struct {
	PageQuery
	ContactID string `query:"contact_id" format:"uuid" doc:"Only this person's settlements." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
}

type settlementListOutput struct{ Body DirectorSettlementPage }

type createSettlementInput struct {
	KeyHeader
	PreferHeader
	Body DirectorSettlementDraft
}

type replaceSettlementInput struct {
	DirectorSettlementPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body DirectorSettlementDraft
}

type settlementCommandInput struct {
	DirectorSettlementPath
	KeyHeader
	IfMatchHeader
	PreferHeader
}

type settlementOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"Its version and status, for If-Match."`
	Location string `header:"Location" doc:"Its URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     DirectorSettlement
}

type settlementReadOutput struct {
	ETag string `header:"ETag" doc:"Its version and status, for If-Match."`
	Body DirectorSettlement
}

type settlementRequestOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was asked (FR-INT-14)."`
	Body     Approval
}

func settlementETag(doc storage.Document) string { return etag(doc.Version, string(doc.Status)) }

// settlementIn reads a settlement as sent, with every invalid field at once; the route defaults to the setting's.
func settlementIn(ctx context.Context, u storage.UnitOfWork, d DirectorSettlementDraft, today time.Time) (director.Settlement, error) {
	s := director.Settlement{Route: d.Route}
	var fields []FieldProblem
	var err error
	if s.ContactID, err = uuid.Parse(d.ContactID); err != nil {
		fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.contact_id", Message: "is not an ID", Fix: "Send the person's contact ID"})
	}
	if s.AccountID, err = uuid.Parse(d.AccountID); err != nil {
		fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.account_id", Message: "is not an ID", Fix: "Send the director's account's ID"})
	}
	m, bad := moneyIn(d.Amount, "body.amount")
	if bad != nil {
		fields = append(fields, *bad)
	}
	s.Amount = m
	if s.Route == "" {
		if s.Route, err = director.DefaultRoute(ctx, u, today); err != nil {
			return s, err
		}
	}
	if t := d.Terms; t != nil {
		s.Terms = &director.Terms{InterestRate: cmpOr(strings.TrimSpace(t.InterestRate), "0"), Subordinated: t.Subordinated}
		if t.RepayOn != "" {
			on, err := time.Parse(time.DateOnly, t.RepayOn)
			if err != nil {
				fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.terms.repay_on", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD, or leave it out for on demand"})
			}
			s.Terms.RepayOn = &on
		}
	}
	if s.Route == director.RouteKeep && s.Terms == nil {
		s.Terms = &director.Terms{InterestRate: "0"}
	}
	if w := d.Waiver; w != nil {
		credit, err := uuid.Parse(w.CreditAccountID)
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.waiver.credit_account_id", Message: "is not an ID", Fix: "Send the account's ID"})
		}
		s.Waiver = &director.Waiver{CreditAccountID: credit, Capacity: w.Capacity, Reason: strings.TrimSpace(w.Reason)}
	}
	if len(fields) > 0 {
		return s, invalid(fields)
	}
	return s, nil
}

// settlementView is what showing settlements needs once: the base currency, the chart, the persons' names, today, and
// each settlement's pending request.
type settlementView struct {
	base     money.Currency
	accounts map[uuid.UUID]storage.Account
	names    map[uuid.UUID]string
	today    time.Time
	asked    map[string]storage.ApprovalRequest
}

func (a *API) settlementView(ctx context.Context, u storage.UnitOfWork) (settlementView, error) {
	v := settlementView{accounts: map[uuid.UUID]storage.Account{}, asked: map[string]storage.ApprovalRequest{}}
	var err error
	if v.base, err = baseOf(ctx, u); err != nil {
		return v, err
	}
	if v.names, err = directorNames(ctx, u); err != nil {
		return v, err
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return v, err
	}
	for _, x := range accounts {
		v.accounts[x.ID] = x
	}
	if v.today, err = orgToday(ctx, a, u); err != nil {
		return v, err
	}
	requests, err := u.Approvals().List(ctx, string(approval.Pending))
	if err != nil {
		return v, err
	}
	now := a.deps.Clock.Now()
	for _, r := range requests {
		if r.ObjectType == approval.ObjectDirectorSettlement && approval.StatusAt(requestOf(r), now) == approval.Pending {
			v.asked[r.ObjectKey] = r
		}
	}
	return v, nil
}

// settlementOf is a settlement as the API shows it.
func (a *API) settlementOf(ctx context.Context, u storage.UnitOfWork, v settlementView, doc storage.Document) (DirectorSettlement, error) {
	s, err := director.ReadSettlement(doc)
	if err != nil {
		return DirectorSettlement{}, err
	}
	owed, err := director.Owed(ctx, u, v.base, s.ContactID, s.AccountID, v.today)
	if err != nil {
		return DirectorSettlement{}, err
	}
	out := DirectorSettlement{ID: doc.ID.String(), Number: doc.Number, Status: "draft", Version: doc.Version, Date: dayText(s.Date), ContactID: s.ContactID.String(),
		LegalName: v.names[s.ContactID], AccountID: s.AccountID.String(), AccountCode: v.accounts[s.AccountID].Code, Amount: moneyOut(s.Amount),
		Owed: moneyOut(owed), Route: s.Route, DeductedEarlier: s.DeductedEarlier, Checks: []BillCheck{}, LedgerImpact: []EntryLine{}}
	if t := s.Terms; t != nil {
		out.Terms = &SettlementTerms{InterestRate: t.InterestRate, Subordinated: t.Subordinated}
		if t.RepayOn != nil {
			out.Terms.RepayOn = dayText(*t.RepayOn)
		}
	}
	if w := s.Waiver; w != nil {
		out.Waiver = &SettlementWaiver{CreditAccountID: w.CreditAccountID.String(), Capacity: w.Capacity, Reason: w.Reason}
	}
	if out.Agreements, err = director.Attached(ctx, u, doc.ID); err != nil {
		return DirectorSettlement{}, err
	}
	if r, ok := v.asked[doc.ID.String()]; ok {
		out.Status, out.ApprovalAsked = "waiting", r.ID.String()
		out.AccountantReview = len(r.PolicySteps) > 1
	}
	if doc.Status == storage.DocumentPosted {
		out.Status = "decided"
		if s.Route == director.RouteWaive {
			e, err := u.Ledger().ByIdentity(ctx, ledger.DirectorWaiverIdentity(doc.ID, doc.Version, ledger.RuleDirectorWaiver))
			if err != nil {
				return DirectorSettlement{}, err
			}
			out.LedgerImpact = entryOf(e).Lines
		}
		return out, nil
	}
	if out.Agreements == 0 {
		out.Checks = append(out.Checks, BillCheck{Severity: "error", Code: director.CodeNoAgreement, Field: "agreements",
			Message: "it has no agreement attached, so it cannot be sent for approval", Fix: "Attach the loan note or board minute, or the deed of waiver"})
	}
	c, err := a.settlements.Impact(ctx, u, doc, v.today)
	switch {
	case err != nil:
		p := problemFrom(ctx, err)
		out.Checks = append(out.Checks, BillCheck{Severity: "error", Code: p.Code, Message: strings.TrimSuffix(p.Detail, "."), Fix: strings.TrimSuffix(p.Fix, ".")})
	case c.Entry != nil:
		for i, l := range c.Entry.Lines {
			line := EntryLine{LineNo: i + 1, AccountID: l.AccountID.String(), Amount: moneyOut(l.Amount), FXRate: "1", FXRateSource: "base", BaseAmount: moneyOut(l.Base),
				Description: l.Description}
			if l.Party != uuid.Nil {
				line.PartyID = l.Party.String()
			}
			out.LedgerImpact = append(out.LedgerImpact, line)
		}
	}
	return out, nil
}

// readableSettlement is a settlement the caller may read: not found for one whose right on "Director accounts" is
// qualified.
func readableSettlement(ctx context.Context, u storage.UnitOfWork, p authn.Principal, id uuid.UUID) (storage.Document, error) {
	if !authz.Decide(authz.GetDirectorSettlement, p.Roles, p.AI()).Allowed {
		return storage.Document{}, ports.ErrNotFound
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return doc, err
	}
	if !director.IsSettlement(doc) {
		return doc, ports.ErrNotFound
	}
	return doc, nil
}

// settlementDependencies are what a settlement's envelope depends on: its version and status, its amount, and what is
// owed on its account now.
func (a *API) settlementDependencies(ctx context.Context, u storage.UnitOfWork, key string) (int, map[string]string, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return 0, nil, err
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return 0, nil, err
	}
	s, err := director.ReadSettlement(doc)
	if err != nil {
		return 0, nil, err
	}
	base, err := baseOf(ctx, u)
	if err != nil {
		return 0, nil, err
	}
	today, err := orgToday(ctx, a, u)
	if err != nil {
		return 0, nil, err
	}
	owed, err := director.Owed(ctx, u, base, s.ContactID, s.AccountID, today)
	if err != nil {
		return 0, nil, err
	}
	return doc.Version, map[string]string{"status": string(doc.Status), "amount": s.Amount.String() + " " + s.Amount.Currency().Code,
		"owed": owed.String() + " " + owed.Currency().Code}, nil
}

// settlementExecutable refuses the person settled: they never approve their own settlement (FM-9, APR-15).
func settlementExecutable(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, signer uuid.UUID) error {
	id, err := uuid.Parse(r.ObjectKey)
	if err != nil {
		return err
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return err
	}
	s, err := director.ReadSettlement(doc)
	if err != nil {
		return err
	}
	c, err := u.Contacts().Get(ctx, s.ContactID)
	if err != nil {
		return err
	}
	if c.MemberUserID != nil && *c.MemberUserID == signer {
		return problem(approval.CodeBeneficiary, "The settlement is yours, so another person approves it (FM-9).",
			"Ask another person who holds the approval right on director accounts")
	}
	return nil
}

// everyApproverBenefits says whether each member who could sign a settlement of contact asked by requester (one who
// holds A on "Director accounts": an Owner or the Controller), other than the requester and the person, is themselves a
// director or shareholder with a settlement of their own waiting or decided in the last 30 days (FM-9, design D10): then
// the Accountant reviews it too. With nobody else who could sign, it is false: nobody but the Accountant benefits-free.
func (a *API) everyApproverBenefits(ctx context.Context, u storage.UnitOfWork, v settlementView, requester, contact uuid.UUID) (bool, error) {
	c, err := u.Contacts().Get(ctx, contact)
	if err != nil {
		return false, err
	}
	members, err := u.Members().List(ctx, a.deps.Clock.Now())
	if err != nil {
		return false, err
	}
	asked := map[string]bool{}
	for k := range v.asked {
		asked[k] = true
	}
	benefits, err := director.SettlementsSince(ctx, u, asked, a.deps.Clock.Now().AddDate(0, 0, -30))
	if err != nil {
		return false, err
	}
	var candidates []uuid.UUID
	for _, m := range members {
		if m.UserID == requester || c.MemberUserID != nil && m.UserID == *c.MemberUserID {
			continue
		}
		if slices.ContainsFunc(m.Roles, func(r storage.RoleAssignment) bool { return r.Role == string(authz.Owner) || r.Role == string(authz.Controller) }) {
			candidates = append(candidates, m.UserID)
		}
	}
	contactOf := func(user uuid.UUID) (uuid.UUID, bool) {
		theirs, err := u.Contacts().OfMember(ctx, user)
		return theirs.ID, err == nil
	}
	return director.EveryApproverBenefits(candidates, contactOf, benefits), nil
}

func registerSettlements(a *API) {
	readCodes := []string{CodeNotFound, director.CodeNotSettlement, director.CodeSettlement}

	register(a, authz.ListDirectorSettlements, huma.Operation{
		OperationID: "listDirectorSettlements", Method: http.MethodGet, Path: "/v1/director-settlements", Tags: []string{"Founders"},
		Summary: "List settlements of founders' balances",
		Description: "The settlements of what the company owes directors and shareholders, drafted, waiting for approval and decided, the " +
			"latest first, a page at a time, narrowed by person (FR-EQ-05). Needs the right to read every director's account. Read-only; " +
			"no side effects.",
	}, append([]string{CodeCursor, CodeInvalid}, readCodes...), func(ctx context.Context, in *settlementListInput) (*settlementListOutput, error) {
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		afterDate, afterID, err := journalCursor(after)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := settlementListOutput{Body: DirectorSettlementPage{Items: []DirectorSettlement{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			v, err := a.settlementView(ctx, u)
			if err != nil {
				return err
			}
			for {
				docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentDirectorSettlement, AfterDate: afterDate, AfterID: afterID, Limit: 500})
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
					x, err := a.settlementOf(ctx, u, v, doc)
					if err != nil {
						return err
					}
					if in.ContactID != "" && x.ContactID != in.ContactID {
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

	register(a, authz.GetDirectorSettlement, huma.Operation{
		OperationID: "getDirectorSettlement", Method: http.MethodGet, Path: "/v1/director-settlements/{settlement_id}", Tags: []string{"Founders"},
		Summary: "Get a settlement of a founder's balance",
		Description: "Returns a settlement with its ETag: the person, account and amount, what is owed now, the route and its details, its " +
			"agreements, the pending request, and its ledger impact (a waiver's entry as a dry run, or as posted) (FR-EQ-05, FR-APR-02). " +
			"Read-only; no side effects.",
	}, readCodes, func(ctx context.Context, in *DirectorSettlementPath) (*settlementReadOutput, error) {
		id, err := parseID("settlement_id", in.SettlementID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out settlementReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := readableSettlement(ctx, u, p, id)
			if err != nil {
				return err
			}
			v, err := a.settlementView(ctx, u)
			if err != nil {
				return err
			}
			out.ETag = settlementETag(doc)
			out.Body, err = a.settlementOf(ctx, u, v, doc)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &out, nil
	})

	writeCodes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		money.CodeNotDecimal, money.CodePrecision, money.CodeRange, money.CodeCurrency, storage.CodeRetry, storage.CodeLockTimeout}, SettlementCodes...)
	shown := func(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (DirectorSettlement, error) {
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return DirectorSettlement{}, err
		}
		v, err := a.settlementView(ctx, u)
		if err != nil {
			return DirectorSettlement{}, err
		}
		return a.settlementOf(ctx, u, v, doc)
	}
	answer := func(w written[DirectorSettlement]) *settlementOutput {
		if w.Previewed {
			return &settlementOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
		}
		status := "draft"
		if w.Body.Status == "decided" {
			status = "posted"
		}
		return &settlementOutput{Status: w.Status, ETag: etag(w.Body.Version, status), Location: "/v1/director-settlements/" + w.Body.ID,
			Replayed: replayedHeader(w.Replayed), Body: w.Body}
	}

	register(a, authz.CreateDirectorSettlement, huma.Operation{
		OperationID: "createDirectorSettlement", Method: http.MethodPost, Path: "/v1/director-settlements", Tags: []string{"Founders"},
		DefaultStatus: http.StatusCreated, Summary: "Draft a settlement of a founder's balance",
		Description: "Drafts a settlement of what the company owes a director or shareholder on 2500 or 2120 (FR-EQ-05): at most what is owed " +
			"today, by a route (keep as a loan on terms, or waive with the account it credits, the capacity and the reason; the default " +
			"route is the setting founders.default_route). Attach the loan note, minute or deed with addDirectorSettlementEvidence, then " +
			"ask for approval with requestDirectorSettlementApproval. Side effects: the draft, audit events. Every problem comes back " +
			"together. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, writeCodes, func(ctx context.Context, in *createSettlementInput) (*settlementOutput, error) {
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (DirectorSettlement, error) {
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return DirectorSettlement{}, err
			}
			s, err := settlementIn(ctx, u, in.Body, today)
			if err != nil {
				return DirectorSettlement{}, err
			}
			doc, err := a.settlements.CreateIn(ctx, u, s, today)
			if err != nil {
				return DirectorSettlement{}, err
			}
			return shown(ctx, u, doc.ID)
		})
		if err != nil {
			return nil, err
		}
		return answer(w), nil
	})

	register(a, authz.ReplaceDirectorSettlement, huma.Operation{
		OperationID: "replaceDirectorSettlement", Method: http.MethodPut, Path: "/v1/director-settlements/{settlement_id}", Tags: []string{"Founders"},
		Summary: "Change a drafted settlement",
		Description: "Saves a new version of a settlement not decided yet, checked as drafting it is (FR-EQ-05); a pending request to decide " +
			"it then no longer matches. A decided settlement changes no more (FOUNDER-29). Side effects: the new version, audit events. " +
			"Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, director.CodeSettled}, writeCodes...), func(ctx context.Context, in *replaceSettlementInput) (*settlementOutput, error) {
		id, err := parseID("settlement_id", in.SettlementID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (DirectorSettlement, error) {
			doc, _, err := journal.DraftSettlement(ctx, u, id)
			if err != nil {
				return DirectorSettlement{}, err
			}
			if err := checkIfMatch(in.IfMatch, settlementETag(doc)); err != nil {
				return DirectorSettlement{}, err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return DirectorSettlement{}, err
			}
			s, err := settlementIn(ctx, u, in.Body, today)
			if err != nil {
				return DirectorSettlement{}, err
			}
			if _, err := a.settlements.ReviseIn(ctx, u, id, s, today); err != nil {
				return DirectorSettlement{}, err
			}
			return shown(ctx, u, id)
		})
		if err != nil {
			return nil, err
		}
		return answer(w), nil
	})

	register(a, authz.RequestDirectorSettlementApproval, huma.Operation{
		OperationID: "requestDirectorSettlementApproval", Method: http.MethodPost, Path: "/v1/director-settlements/{settlement_id}/approval",
		Tags: []string{"Founders"}, Summary: "Ask for approval of a settlement", DefaultStatus: http.StatusAccepted,
		Description: "Asks another person to approve a drafted settlement at the version read (FR-EQ-05, FR-APR-01), in every team mode. It " +
			"needs its loan note, minute or deed attached (FOUNDER-30) and must fit what is owed now. The answer is 202 with an approval " +
			"request, which a person who holds A on \"Director accounts, settlements, share issues\" approves, stepped up: never the one " +
			"who asked, nor the person settled (APR-15, FM-9; the database's FY253); when every member who could sign is a director with a " +
			"settlement of their own waiting or decided in the last 30 days, a final step the Accountant signs records their review " +
			"[accountant to confirm]. The envelope carries the ledger impact (FR-APR-02). On the approval a loan kept is decided with no " +
			"entry, and a waiver posts director_settlement@1 on that day: Dr 2500 or 2120 naming the person (waived), Cr the account " +
			"chosen; a waiver of 2120 is tagged deducted earlier [verify]. The request holds the settlement's version, status, amount and " +
			"what is owed, so a change voids it. Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview " +
			"checks it and asks nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, director.CodeSettled, director.CodeNoAgreement, CodeIdempotencyReused, CodeIdempotencyBusy,
		CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, storage.CodeRetry, storage.CodeLockTimeout, ledger.CodeWaiver, ledger.CodeParty,
		ledger.CodeUnknownAccount, ledger.CodeNoDate}, SettlementCodes...),
		func(ctx context.Context, in *settlementCommandInput) (*settlementRequestOutput, error) {
			id, err := parseID("settlement_id", in.SettlementID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
				doc, s, err := journal.DraftSettlement(ctx, u, id)
				if err != nil {
					return Approval{}, err
				}
				if err := checkIfMatch(in.IfMatch, settlementETag(doc)); err != nil {
					return Approval{}, err
				}
				v, err := a.settlementView(ctx, u)
				if err != nil {
					return Approval{}, err
				}
				c, err := a.settlements.Check(ctx, u, doc, v.today)
				if err != nil {
					return Approval{}, err
				}
				p, _ := authn.From(ctx)
				var then []approval.Step
				review, err := a.everyApproverBenefits(ctx, u, v, p.UserID, s.ContactID)
				if err != nil {
					return Approval{}, err
				}
				if review { // FM-9: every available approver benefits, so the Accountant reviews it too [accountant to confirm]
					then = []approval.Step{{Quorum: 1, Roles: []string{string(authz.Accountant)}}}
				}
				amount := s.Amount.String() + " " + s.Amount.Currency().Code
				diff := []ApprovalDiff{{Field: "status", Before: "draft", After: "decided"}, {Field: "route", After: s.Route}, {Field: "amount", After: amount},
					{Field: "accountant_review", After: strconv.FormatBool(review)}}
				if c.Entry != nil {
					for _, l := range c.Entry.Lines {
						side, amt := "debit", l.Amount
						if l.Amount.Sign() < 0 {
							side, amt = "credit", l.Amount.Neg()
						}
						diff = append(diff, ApprovalDiff{Field: side + " " + v.accounts[l.AccountID].Code, After: amt.String() + " " + amt.Currency().Code})
					}
				}
				return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectDirectorSettlement, ObjectKey: doc.ID.String(),
					Operation: approval.OpSettlementApprove,
					Summary:   fmt.Sprintf("Settle %s of %s's balance on %s: %s", amount, v.names[s.ContactID], v.accounts[s.AccountID].Code, s.Route),
					Execute:   map[string]any{"settlement_id": doc.ID.String(), "version": strconv.Itoa(doc.Version), "contact_id": s.ContactID.String()},
					Diff:      diff, Amounts: []approval.Amount{{Amount: s.Amount.String(), Currency: s.Amount.Currency().Code}}, Then: then})
			})
			if err != nil {
				return nil, err
			}
			return &settlementRequestOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})

	registerSettlementEvidence(a)
}
