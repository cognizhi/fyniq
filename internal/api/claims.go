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
	"github.com/cognizhi/fyniq/internal/domain/claim"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ expense claims (FR-AP-07, FR-AP-14, FR-AP-17; p1-expense-claims)
//
// A member claims what they spent for the company: they draft a claim, change it while it is theirs, and submit it,
// which asks for approval (design D3; the owner's decision of 26 Oct 2026, rev 0.147): one person other than the
// claimant who holds A on "Expense claims" approves it, and the Controller too when a line is above its category's
// limit; the last approval posts it under expense_claim@1, and a rejection returns it to its claimant with the comment.
// A person whose right on "Expense claims" is their own claims only (the Employee's "C (own)") sees and acts on their
// own claims and no one else's (design D10).

// The API's codes for expense claims.
const (
	CodeClaimNotYours = "CLAIM-09" // only its claimant changes or submits a claim
	CodeClaimWaiting  = "CLAIM-10" // the claim waits for approval: withdraw it to change it
	CodeOwnClaim      = "CLAIM-11" // nobody approves their own claim (FR-AP-17)
)

// ClaimCodes are the codes the posting rule and the claim's own checks answer.
var ClaimCodes = []string{claim.CodeClaimant, claim.CodeCategory, claim.CodeTooLong, claim.CodeReceipt, claim.CodeGSTCode, claim.CodeMileage,
	claim.CodeMileageRate, ledger.CodeClaimLine, ledger.CodeClaimPaid, ledger.CodeSystemRole, ledger.CodeRuleAccount, ledger.CodeLineCurrency}

// ClaimApprovalCodes are the codes an approval of a claim's post may answer: the claimant's own, and the posting's.
func ClaimApprovalCodes() []string {
	return append([]string{CodeOwnClaim, claim.CodeNotClaim, claim.CodeNotDraft, claim.CodeBadDraft, storage.CodeClaim}, ClaimCodes...)
}

// ExpenseClaimLine is one line of a claim: a receipt, as the receipt shows it.
type ExpenseClaimLine struct {
	Kind        string `json:"kind,omitempty" enum:"receipt,mileage" doc:"receipt: what was bought, as the receipt shows it (the default); mileage: a trip in the claimant's own car or motorcycle, at the organisation's rate per km (FR-AP-16)." example:"receipt"`
	Date        string `json:"date,omitempty" format:"date" doc:"The receipt's or the trip's date: its rate and its GST treatment are read on it." example:"2026-10-24"`
	Merchant    string `json:"merchant,omitempty" maxLength:"120" doc:"Who was paid, as the receipt shows it." example:"ComfortDelGro"`
	CategoryID  string `json:"category_id" format:"uuid" doc:"The expense policy's category it is for: the account it costs (FR-AP-15)." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fc1"`
	Description string `json:"description,omitempty" maxLength:"500" doc:"What it was for: a trip's purpose." example:"Taxi to Acme"`
	Amount      *Money `json:"amount,omitempty" doc:"A receipt: what it shows was paid, including GST, in its currency (required). A trip: in answers, what RND-MILEAGE made of it."`
	GSTCode     string `json:"gst_code,omitempty" pattern:"^[A-Z]{2,4}$" doc:"A receipt's GST code for purchases, from the sg-gst pack: TX when it shows GST, NR when the merchant is not GST-registered, OP outside Singapore. A trip has none." example:"NR"`
	GST         *Money `json:"gst,omitempty" doc:"The GST the receipt shows, in its currency; none when left out."`
	StatedGST   *Money `json:"stated_gst,omitempty" doc:"For a receipt in another currency: the GST its tax invoice states in the base currency (ENG-42); without it the GST is not claimed."`
	Paid        *Money `json:"paid,omitempty" doc:"For a receipt in another currency: what left the claimant's account in the base currency, as their card statement shows it; what the company owes them for it (FR-AP-17). Left out: the amount at the rate on its date."`
	FXRate      string `json:"fx_rate,omitempty" doc:"For a receipt in another currency, a rate into the base currency typed by the claimant; left out, the rate on its date (ENG-40)." example:"0.0000850"`
	Attendees   string `json:"attendees,omitempty" maxLength:"500" doc:"Who attended, for a meal or entertainment." example:"Mei Ling Tan; Tan Wei, Acme"`
	Document    string `json:"document,omitempty" enum:"tax_invoice,simplified,none" doc:"What the receipt is, for its input tax (FR-AP-20): a tax invoice (the default), a simplified tax invoice (the merchant's GST number, no customer named), or none." example:"simplified"`
	AddressedTo string `json:"addressed_to,omitempty" enum:"company,agent" doc:"For a tax invoice: addressed to the company (the default), or to the claimant as its agent." example:"company"`
	From        string `json:"from,omitempty" maxLength:"300" doc:"A trip: where it started, an address or a saved place's (claim places). Never sent anywhere: the maps service stays off." example:"Office, 1 Raffles Place"`
	To          string `json:"to,omitempty" maxLength:"300" doc:"A trip: where it went." example:"Acme HQ, 10 Anson Road"`
	Return      bool   `json:"return,omitempty" doc:"A trip: true when it came back the same way. The distance is still the whole trip's, as driven." example:"true"`
	Vehicle     string `json:"vehicle,omitempty" enum:"car,motorcycle" doc:"A trip: the vehicle, whose rate per km it is claimed at." example:"car"`
	Distance    string `json:"distance,omitempty" pattern:"^[0-9]{1,5}(\\.[0-9]{1,3})?$" doc:"A trip: the whole trip's distance in km, as driven." example:"18.4"`
	ClaimedKm   string `json:"claimed_km,omitempty" doc:"A trip, in answers: the distance rounded to 0.1 km (RND-MILEAGE)." example:"18.4"`
	Rate        *Money `json:"rate,omitempty" doc:"A trip, in answers: the vehicle's rate per km on its date."`
}

// ExpenseClaimDraft is a claim as its claimant enters it.
type ExpenseClaimDraft struct {
	Date  string             `json:"date,omitempty" format:"date" doc:"The claim's date: the day its entry posts on, on or after every receipt's." example:"2026-10-26"`
	Title string             `json:"title,omitempty" maxLength:"120" doc:"What the claim is for." example:"Acme visit"`
	Notes string             `json:"notes,omitempty" maxLength:"2000" doc:"Notes for the approver: why a line is above its limit, for example." example:"Client lunch ran over the meals limit."`
	Lines []ExpenseClaimLine `json:"lines" nullable:"false" maxItems:"100" doc:"Its lines: a receipt each."`
}

// ExpenseClaimReturn is the latest return of a claim to its claimant: who returned it, when, and why (UC-EXP-04).
type ExpenseClaimReturn struct {
	By      *Person   `json:"by,omitempty" doc:"Who returned it."`
	At      time.Time `json:"at" doc:"When." example:"2026-10-26T06:00:00Z"`
	Comment string    `json:"comment" doc:"Why, in the approver's words: what to fix." example:"Add who attended the lunch."`
}

// ExpenseClaim is a member's claim: its current version, where it stands, and what posting it does.
type ExpenseClaim struct {
	ID            string              `json:"id" format:"uuid" doc:"The claim's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fd1"`
	Number        string              `json:"number,omitempty" doc:"Its number from the claim sequence, given when it is first saved (FR-ORG-04)." example:"CLM-0007"`
	Status        string              `json:"status" enum:"draft,posted,reversed" doc:"Draft (✎) until it is approved, then posted." example:"draft"`
	Version       int                 `json:"version" doc:"The version of its content; with the status it makes the ETag." example:"1"`
	Claimant      Person              `json:"claimant" doc:"Who claims: the member the company owes."`
	Stage         string              `json:"stage" enum:"draft,submitted,returned,approved,scheduled,paid" doc:"Where it stands for its claimant (FR-AP-14): draft; submitted while its approval waits; returned when its latest request was rejected, with the comment; approved once posted; paid once reimbursements paid back all it owes. scheduled arrives with payment batches." example:"submitted"`
	Date          string              `json:"date,omitempty" format:"date" doc:"The claim's date." example:"2026-10-26"`
	Title         string              `json:"title,omitempty" doc:"What it is for." example:"Acme visit"`
	Notes         string              `json:"notes,omitempty" doc:"Its notes." example:"Client lunch ran over the meals limit."`
	Lines         []ExpenseClaimLine  `json:"lines" nullable:"false" doc:"The lines as entered."`
	Total         *Money              `json:"total,omitempty" doc:"What the company owes for it in the base currency, as posted or as posting would post it; absent while it cannot post."`
	Returned      *ExpenseClaimReturn `json:"returned,omitempty" doc:"Its latest return, while it is returned."`
	TaxDecisions  []BillTaxDecision   `json:"input_tax_decisions" nullable:"false" doc:"getExpenseClaim only: how each line's input tax was decided, one a line in order (FR-AP-20)."`
	Checks        []BillCheck         `json:"checks" nullable:"false" doc:"getExpenseClaim only: what stops a draft being submitted (✖), and the warnings it posts with (⚠), such as a line above its category's limit, which finance approves too."`
	LedgerImpact  []EntryLine         `json:"ledger_impact" nullable:"false" doc:"getExpenseClaim only: the lines posting it writes (a draft), or wrote (posted): debits positive."`
	PreparedBy    *Person             `json:"prepared_by,omitempty" doc:"Who saved the current version."`
	PostedBy      *Person             `json:"posted_by,omitempty" doc:"Who approved it last: its entry's poster."`
	PostedAt      *time.Time          `json:"posted_at,omitempty" doc:"When it was posted." example:"2026-10-26T06:30:00Z"`
	ApprovalAsked string              `json:"approval_asked,omitempty" format:"uuid" doc:"The pending request to post it, while it is submitted." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4dd1"`
}

// ExpenseClaimPage is a page of claims, newest date first.
type ExpenseClaimPage struct {
	Items      []ExpenseClaim `json:"items" nullable:"false"`
	NextCursor string         `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjYtMTAtMjZ8MDE5MiJ9"`
}

// ExpenseClaimPath names a claim in the path.
type ExpenseClaimPath struct {
	ClaimID string `path:"claim_id" format:"uuid" doc:"The claim's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fd1"`
}

type claimListInput struct {
	PageQuery
	Whose string `query:"whose" enum:"mine,waiting_for_me,everyone" default:"mine" doc:"mine: the caller's own claims (the default); waiting_for_me: the claims whose approval the caller may sign now; everyone: every member's, for a person who holds a right on everyone's (a person whose right is their own claims sees only theirs)." example:"mine"`
	Stage string `query:"stage" enum:"draft,submitted,returned,approved,paid" doc:"Only claims at this stage." example:"submitted"`
}

type claimListOutput struct{ Body ExpenseClaimPage }

type createClaimInput struct {
	KeyHeader
	PreferHeader
	Body ExpenseClaimDraft
}

type replaceClaimInput struct {
	ExpenseClaimPath
	IfMatchHeader
	PreferHeader
	Body ExpenseClaimDraft
}

type claimCommandInput struct {
	ExpenseClaimPath
	KeyHeader
	IfMatchHeader
	PreferHeader
}

type claimOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The claim's version and status, for If-Match."`
	Location string `header:"Location" doc:"The claim's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     ExpenseClaim
}

type claimReadOutput struct {
	ETag string `header:"ETag" doc:"The claim's version and status, for If-Match."`
	Body ExpenseClaim
}

func claimETag(c ExpenseClaim) string { return etag(c.Version, c.Status) }

// ownClaimsOnly says the caller's right on "Expense claims" is their own claims only: the Employee's "C (own)"
// (Appendix D, design D10).
func ownClaimsOnly(p authn.Principal) bool {
	return !authz.Decide(authz.Policy{Operation: "view", Capability: authz.ExpenseClaims, Need: authz.View, Unqualified: true}, p.Roles, p.AI()).Allowed
}

// claimDraftOf reads a claim from the request, with every invalid field at once; the claimant is the caller's.
func claimDraftOf(in ExpenseClaimDraft, base money.Currency) (claim.Draft, error) {
	d := claim.Draft{Title: strings.TrimSpace(in.Title), Notes: in.Notes}
	var fields []FieldProblem
	if in.Date != "" {
		t, err := time.Parse(time.DateOnly, in.Date)
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.date", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"})
		}
		d.Date = t
	}
	for i, l := range in.Lines {
		at := func(f string) string { return "body.lines[" + strconv.Itoa(i) + "]." + f }
		line := claim.Line{Kind: claim.LineReceipt, Merchant: strings.TrimSpace(l.Merchant), Description: l.Description, GSTCode: l.GSTCode,
			Attendees: strings.TrimSpace(l.Attendees), Document: l.Document, AddressedTo: l.AddressedTo}
		id, err := uuid.Parse(l.CategoryID)
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: at("category_id"), Message: "is not an ID", Fix: "Send the category's ID"})
		}
		line.CategoryID = id
		if l.Date != "" {
			if line.Date, err = time.Parse(time.DateOnly, l.Date); err != nil {
				fields = append(fields, FieldProblem{Code: CodeInvalid, Field: at("date"), Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"})
			}
		}
		if l.Kind == claim.LineMileage {
			line.Kind, line.From, line.To, line.Return, line.Vehicle = claim.LineMileage, strings.TrimSpace(l.From), strings.TrimSpace(l.To), l.Return, l.Vehicle
			if line.Distance, err = money.ParseQuantity(cmpOr(l.Distance, "0"), at("distance")); err != nil {
				fields = append(fields, fieldOf(err, at("distance")))
			}
			d.Lines = append(d.Lines, line)
			continue
		}
		if l.Amount == nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: at("amount"), Message: "is missing", Fix: "Send what the receipt shows was paid"})
			continue
		}
		var bad *FieldProblem
		if line.Amount, bad = moneyIn(*l.Amount, at("amount")); bad != nil {
			fields = append(fields, *bad)
			continue
		}
		line.GST = money.Zero(line.Amount.Currency())
		if l.GST != nil {
			if line.GST, bad = moneyIn(*l.GST, at("gst")); bad != nil {
				fields = append(fields, *bad)
				continue
			}
		}
		for _, x := range []struct {
			in   *Money
			to   **money.Money
			name string
		}{{l.StatedGST, &line.StatedGST, "stated_gst"}, {l.Paid, &line.Paid, "paid"}} {
			if x.in == nil {
				continue
			}
			m, bad := moneyIn(*x.in, at(x.name))
			if bad != nil {
				fields = append(fields, *bad)
				continue
			}
			*x.to = &m
		}
		if l.FXRate != "" {
			r, err := money.ParseRate(l.FXRate, line.Amount.Currency(), base, RateSourceDocument)
			if err != nil {
				fields = append(fields, fieldOf(err, at("fx_rate")))
			} else {
				line.Rate = &r
			}
		}
		d.Lines = append(d.Lines, line)
	}
	if len(fields) > 0 {
		return d, invalid(fields)
	}
	return d, nil
}

// claimLinesOut are a claim's lines as the API shows them, each trip with what RND-MILEAGE made of it when trips says.
func claimLinesOut(d claim.Draft, trips map[int]claim.Trip) []ExpenseClaimLine {
	out := make([]ExpenseClaimLine, 0, len(d.Lines))
	for i, l := range d.Lines {
		if l.Kind == claim.LineMileage {
			x := ExpenseClaimLine{Kind: claim.LineMileage, Date: dayText(l.Date), CategoryID: l.CategoryID.String(), Description: l.Description,
				From: l.From, To: l.To, Return: l.Return, Vehicle: l.Vehicle, Distance: l.Distance.String()}
			if t, ok := trips[i]; ok {
				amount, rate := moneyOut(t.Amount), moneyOut(t.Rate)
				x.Amount, x.Rate, x.ClaimedKm = &amount, &rate, t.Km.String()
			}
			out = append(out, x)
			continue
		}
		amount := moneyOut(l.Amount)
		x := ExpenseClaimLine{Kind: claim.LineReceipt, Date: dayText(l.Date), Merchant: l.Merchant, CategoryID: l.CategoryID.String(),
			Description: l.Description, Amount: &amount, GSTCode: l.GSTCode, Attendees: l.Attendees, Document: l.Document,
			AddressedTo: l.AddressedTo}
		g := moneyOut(l.GST)
		x.GST = &g
		if l.StatedGST != nil {
			s := moneyOut(*l.StatedGST)
			x.StatedGST = &s
		}
		if l.Paid != nil {
			p := moneyOut(*l.Paid)
			x.Paid = &p
		}
		if l.Rate != nil {
			x.FXRate = l.Rate.String()
		}
		out = append(out, x)
	}
	return out
}

// claimView is what showing claims needs once: the base currency, and each claim's latest request to post it.
type claimView struct {
	base   money.Currency
	latest map[string]storage.ApprovalRequest // a claim's ID: its latest request, by when it was asked
}

func (a *API) claimView(ctx context.Context, u storage.UnitOfWork) (claimView, error) {
	o, err := u.Organisation(ctx)
	if err != nil {
		return claimView{}, err
	}
	v := claimView{latest: map[string]storage.ApprovalRequest{}}
	if v.base, err = money.Lookup(o.BaseCurrency); err != nil {
		return claimView{}, err
	}
	requests, err := u.Approvals().List(ctx, "")
	if err != nil {
		return claimView{}, err
	}
	now := a.deps.Clock.Now()
	for _, r := range requests {
		if r.ObjectType != approval.ObjectClaim {
			continue
		}
		r.Status = string(approval.StatusAt(requestOf(r), now))
		// The latest by when it was asked, then by its ID (UUIDv7, in the order made) for two asked in the same instant.
		if l, ok := v.latest[r.ObjectKey]; !ok || r.RequestedAt.After(l.RequestedAt) || r.RequestedAt.Equal(l.RequestedAt) && r.ID.String() > l.ID.String() {
			v.latest[r.ObjectKey] = r
		}
	}
	return v, nil
}

// claimOf is a claim as the API shows it, from its document. withChecks fills its checks, tax decisions and ledger
// impact (getExpenseClaim).
func (a *API) claimOf(ctx context.Context, u storage.UnitOfWork, v claimView, doc storage.Document, withChecks bool) (ExpenseClaim, error) {
	d, err := claim.Read(doc, v.base)
	if err != nil {
		return ExpenseClaim{}, err
	}
	out := ExpenseClaim{ID: doc.ID.String(), Number: doc.Number, Status: string(doc.Status), Version: doc.Version,
		Claimant: Person{Kind: "user", ID: d.ClaimantID.String()}, Stage: "draft", Date: dayText(d.Date), Title: d.Title, Notes: d.Notes,
		Lines: claimLinesOut(d, nil), TaxDecisions: []BillTaxDecision{}, Checks: []BillCheck{}, LedgerImpact: []EntryLine{},
		PreparedBy: personOf(doc.SavedBy), PostedBy: personOf(doc.PostedBy), PostedAt: doc.PostedAt}
	var returnedBy *Person
	if r, ok := v.latest[out.ID]; ok && doc.Status == storage.DocumentDraft {
		switch approval.Status(r.Status) {
		case approval.Pending:
			out.Stage, out.ApprovalAsked = "submitted", r.ID.String()
		case approval.Rejected:
			out.Stage = "returned"
			out.Returned = &ExpenseClaimReturn{Comment: r.StatusReason}
			if r.DecidedAt != nil {
				out.Returned.At = *r.DecidedAt
			}
			sigs, err := u.Approvals().Signatures(ctx, r.ID)
			if err != nil {
				return ExpenseClaim{}, err
			}
			for _, s := range sigs {
				if s.Decision == "reject" {
					returnedBy = &Person{Kind: "user", ID: s.Signer.String()}
					out.Returned.By = returnedBy
				}
			}
		}
	}
	if doc.Status == storage.DocumentDraft {
		c, err := journal.CheckClaim(ctx, u, doc)
		if err != nil {
			return ExpenseClaim{}, err
		}
		out.Lines = claimLinesOut(d, c.Prepared.Trips)
		if withChecks {
			out.TaxDecisions = taxDecisionsOut(c.Prepared.InputTax)
			for _, p := range c.Problems {
				out.Checks = append(out.Checks, BillCheck{Severity: "error", Code: p.Code, Field: journalField(p.Field), Message: p.Message, Fix: p.Fix})
			}
			for _, w := range c.Prepared.Warnings {
				out.Checks = append(out.Checks, BillCheck{Severity: "warning", Code: w.Code, Field: journalField(w.Field), Message: w.Message, Fix: w.Fix})
			}
		}
		if len(c.Problems) == 0 {
			t := moneyOut(c.Owed)
			out.Total = &t
			if withChecks {
				out.LedgerImpact = impactOf(c.Entry)
			}
		}
		return out, a.name(ctx, u, &out.Claimant, out.PreparedBy, out.PostedBy, returnedBy)
	}
	out.Stage = "approved"
	e, err := u.Ledger().ByIdentity(ctx, ledger.ExpenseClaimIdentity(doc.ID, doc.Version, ledger.RuleExpenseClaim))
	if err != nil {
		return ExpenseClaim{}, err
	}
	p, _, err := claim.Prepare(ctx, u, doc, v.base)
	if err != nil {
		return ExpenseClaim{}, err
	}
	out.Lines = claimLinesOut(d, p.Trips)
	if withChecks {
		out.TaxDecisions = taxDecisionsOut(p.InputTax)
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return ExpenseClaim{}, err
	}
	payable := map[uuid.UUID]bool{}
	for _, x := range accounts {
		payable[x.ID] = string(x.Role) == ledger.RoleExpenseClaimsPayable
	}
	owed := money.Zero(v.base)
	for _, l := range e.Lines {
		if payable[l.AccountID] {
			if x, err := money.ParseStored(string(l.BaseAmount), v.base); err == nil {
				owed, _ = owed.Add(x.Neg())
			}
		}
	}
	t := moneyOut(owed)
	out.Total = &t
	if doc.Status == storage.DocumentPosted { // paid once reimbursements have paid back all it owes (FR-AP-14)
		b, err := claim.Balances(ctx, u, []storage.Document{doc}, v.base, time.Time{})
		if err != nil {
			return ExpenseClaim{}, err
		}
		if x, ok := b[doc.ID]; ok && x.Owed.Sign() > 0 && x.Open.Sign() <= 0 {
			out.Stage = "paid"
		}
	}
	if withChecks {
		out.LedgerImpact = entryOf(e).Lines
	}
	return out, a.name(ctx, u, &out.Claimant, out.PreparedBy, out.PostedBy)
}

// claimProblems turns the rule's problems into the API's, naming the claim.
func claimProblems(lp ledger.Problems) error {
	fields := make([]FieldProblem, 0, len(lp))
	for _, x := range lp {
		fields = append(fields, FieldProblem{Code: x.Code, Field: journalField(x.Field), Message: x.Message, Fix: x.Fix})
	}
	return problem(lp[0].Code, fmt.Sprintf("The claim cannot be submitted: %d problem(s), each listed in errors.", len(lp)),
		"Correct the claim and submit it again.", fields...)
}

// ownClaim locks a claim for its claimant: the caller, who must be its claimant; one whose right is their own claims
// only does not learn of anyone else's.
func ownClaim(ctx context.Context, u storage.UnitOfWork, p authn.Principal, id uuid.UUID, base money.Currency) (storage.Document, claim.Draft, error) {
	doc, err := u.Documents().Lock(ctx, id)
	if err != nil {
		return doc, claim.Draft{}, err
	}
	d, err := claim.Read(doc, base)
	if err != nil {
		return doc, d, err
	}
	switch {
	case d.ClaimantID != p.UserID && ownClaimsOnly(p):
		return doc, d, ports.ErrNotFound
	case d.ClaimantID != p.UserID:
		return doc, d, problem(CodeClaimNotYours, "Only the claimant changes or submits their claim.", "Ask them to, or return it to them when you decide it")
	}
	return doc, d, nil
}

// claimant is the caller as a claimant: a person, who claims for themselves.
func claimant(ctx context.Context) (authn.Principal, error) {
	p, _ := authn.From(ctx)
	if p.UserID == uuid.Nil || p.Class != authn.ClassUser || p.AI() {
		return p, problem(CodeClaimNotYours, "A claim is a person's own: only a member signed in as themselves claims.", "Sign in as yourself")
	}
	return p, nil
}

// claimDependencies are what a claim's envelope depends on: its version and status, and what the company owes for it.
func claimDependencies(ctx context.Context, u storage.UnitOfWork, key string) (int, map[string]any, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return 0, nil, err
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return 0, nil, err
	}
	owed := ""
	if doc.Status == storage.DocumentDraft {
		c, err := journal.CheckClaim(ctx, u, doc)
		if err != nil {
			return 0, nil, err
		}
		if len(c.Problems) == 0 {
			owed = c.Owed.String() + " " + c.Owed.Currency().Code
		}
	}
	return doc.Version, map[string]any{"claim_version": doc.Version, "status": string(doc.Status), "owed": owed}, nil
}

// claimExecutable refuses the claimant's own approval of their claim (FR-AP-17): the request's requester never signs
// it, and nor does its claimant, whoever asked.
func claimExecutable(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, signer uuid.UUID) error {
	id, err := uuid.Parse(r.ObjectKey)
	if err != nil {
		return err
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return err
	}
	o, err := u.Organisation(ctx)
	if err != nil {
		return err
	}
	base, err := money.Lookup(o.BaseCurrency)
	if err != nil {
		return err
	}
	d, err := claim.Read(doc, base)
	if err != nil {
		return err
	}
	if d.ClaimantID == signer {
		return problem(CodeOwnClaim, "This is your own claim, so another person approves it (FR-AP-17).", "Ask another person who holds the approval right")
	}
	return nil
}

func registerClaims(a *API) {
	formCodes := []string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, money.CodeNotDecimal, money.CodePrecision, money.CodeRange,
		money.CodeCurrency, money.CodeRate, claim.CodeTooLong, claim.CodeReceipt, CodeClaimNotYours, storage.CodeRetry, storage.CodeLockTimeout}
	readCodes := []string{CodeNotFound, claim.CodeNotClaim, claim.CodeBadDraft}

	register(a, authz.ListExpenseClaims, huma.Operation{
		OperationID: "listExpenseClaims", Method: http.MethodGet, Path: "/v1/expense-claims", Tags: []string{"Expense claims"},
		Summary: "List expense claims",
		Description: "The claims at their current version, newest date first (a draft without a date last), a page at a time (FR-AP-14): the " +
			"caller's own (the default), those whose approval waits for the caller, or everyone's. A person whose right on \"Expense " +
			"claims\" is their own claims only (an Employee) sees only theirs, whatever they ask for. Each has its claimant, stage and " +
			"total. Read-only; no side effects.",
	}, append([]string{CodeCursor}, readCodes...), func(ctx context.Context, in *claimListInput) (*claimListOutput, error) {
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		afterDate, afterID, err := journalCursor(after)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		whose := cmpOr(in.Whose, "mine")
		if ownClaimsOnly(p) && whose == "everyone" {
			whose = "mine"
		}
		out := claimListOutput{Body: ExpenseClaimPage{Items: []ExpenseClaim{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			v, err := a.claimView(ctx, u)
			if err != nil {
				return err
			}
			waiting := map[string]bool{}
			if whose == "waiting_for_me" {
				for key, r := range v.latest {
					if approval.Status(r.Status) != approval.Pending || !maySee(p, r) {
						continue
					}
					sigs, err := u.Approvals().Signatures(ctx, r.ID)
					if err != nil {
						return err
					}
					waiting[key] = mayDecide(p, r, sigs)
				}
			}
			for {
				docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentExpenseClaim, AfterDate: afterDate, AfterID: afterID, Limit: 500})
				if err != nil {
					return err
				}
				for _, doc := range docs {
					afterDate, afterID = doc.Date, &doc.ID
					if len(out.Body.Items) == in.PageSize {
						out.Body.NextCursor = encodeCursor(lastClaim(out.Body.Items))
						return nil
					}
					d, err := claim.Read(doc, v.base)
					if err != nil {
						return err
					}
					switch {
					case whose == "mine" && d.ClaimantID != p.UserID, whose == "waiting_for_me" && !waiting[doc.ID.String()]:
						continue
					}
					c, err := a.claimOf(ctx, u, v, doc, false)
					if err != nil {
						return err
					}
					if in.Stage == "" || c.Stage == in.Stage {
						out.Body.Items = append(out.Body.Items, c)
					}
				}
				if len(docs) < 500 {
					return nil
				}
			}
		})
		return &out, err
	})

	register(a, authz.CreateExpenseClaim, huma.Operation{
		OperationID: "createExpenseClaim", Method: http.MethodPost, Path: "/v1/expense-claims", Tags: []string{"Expense claims"},
		DefaultStatus: http.StatusCreated, Summary: "Save a draft expense claim",
		Description: "Saves the caller's own claim as a draft (✎) at version 1, numbered from the claim sequence (FR-AP-07, FR-AP-14): the " +
			"claimant is always the caller. A draft is checked for its form only; getExpenseClaim shows what posting it would do, and " +
			"submitExpenseClaim checks it all. Side effects: a draft document and its audit event; nothing posts until another person " +
			"approves it. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld}, formCodes...),
		func(ctx context.Context, in *createClaimInput) (*claimOutput, error) {
			p, err := claimant(ctx)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (ExpenseClaim, error) {
				v, err := a.claimView(ctx, u)
				if err != nil {
					return ExpenseClaim{}, err
				}
				d, err := claimDraftOf(in.Body, v.base)
				if err != nil {
					return ExpenseClaim{}, err
				}
				d.ClaimantID = p.UserID
				doc, err := a.claims.Create(ctx, p.Organisation, d)
				if err != nil {
					return ExpenseClaim{}, err
				}
				return a.claimOf(ctx, u, v, doc, false)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &claimOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &claimOutput{Status: w.Status, ETag: claimETag(w.Body), Location: "/v1/expense-claims/" + w.Body.ID, Replayed: replayedHeader(w.Replayed),
				Body: w.Body}, nil
		})

	register(a, authz.GetExpenseClaim, huma.Operation{
		OperationID: "getExpenseClaim", Method: http.MethodGet, Path: "/v1/expense-claims/{claim_id}", Tags: []string{"Expense claims"},
		Summary: "Get an expense claim",
		Description: "Returns a claim's current version with its ETag: its claimant and lines, its stage (with the comment when it was " +
			"returned), what the company owes for it, what stops it being submitted (✖) and its warnings (⚠: a line above its " +
			"category's limit, which finance approves too), how each line's input tax was decided, and the lines posting it writes or " +
			"wrote. A person whose right is their own claims only gets theirs alone. Read-only; no side effects.",
	}, readCodes, func(ctx context.Context, in *ExpenseClaimPath) (*claimReadOutput, error) {
		id, err := parseID("claim_id", in.ClaimID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var c ExpenseClaim
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Get(ctx, id)
			if err != nil {
				return err
			}
			v, err := a.claimView(ctx, u)
			if err != nil {
				return err
			}
			if c, err = a.claimOf(ctx, u, v, doc, true); err != nil {
				return err
			}
			if c.Claimant.ID != p.UserID.String() && ownClaimsOnly(p) {
				return ports.ErrNotFound
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return &claimReadOutput{ETag: claimETag(c), Body: c}, nil
	})

	register(a, authz.ReplaceExpenseClaim, huma.Operation{
		OperationID: "replaceExpenseClaim", Method: http.MethodPut, Path: "/v1/expense-claims/{claim_id}", Tags: []string{"Expense claims"},
		Summary: "Change a draft expense claim",
		Description: "Saves the claimant's new content as the claim's next version; the earlier versions are kept, so a returned claim " +
			"keeps what it was returned as (UC-EXP-04). Only the claimant changes their claim, only while it is a draft, and not while " +
			"its approval waits (withdrawExpenseClaim first). Side effects: the new version and its audit event. Needs If-Match with " +
			"the claim's ETag. Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, claim.CodeNotClaim, claim.CodeNotDraft, claim.CodeBadDraft, CodeClaimWaiting}, formCodes...),
		func(ctx context.Context, in *replaceClaimInput) (*claimOutput, error) {
			id, err := parseID("claim_id", in.ClaimID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			p, err := claimant(ctx)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (ExpenseClaim, error) {
				v, err := a.claimView(ctx, u)
				if err != nil {
					return ExpenseClaim{}, err
				}
				doc, _, err := ownClaim(ctx, u, p, id, v.base)
				if err != nil {
					return ExpenseClaim{}, err
				}
				before, err := a.claimOf(ctx, u, v, doc, false)
				if err != nil {
					return ExpenseClaim{}, err
				}
				if err := checkIfMatch(in.IfMatch, claimETag(before)); err != nil {
					return ExpenseClaim{}, err
				}
				if before.Stage == "submitted" {
					return ExpenseClaim{}, problem(CodeClaimWaiting, "The claim waits for approval, so it does not change.",
						"Withdraw it to change it, then submit it again")
				}
				d, err := claimDraftOf(in.Body, v.base)
				if err != nil {
					return ExpenseClaim{}, err
				}
				d.ClaimantID = p.UserID
				after, err := a.claims.Revise(ctx, p.Organisation, id, d)
				if err != nil {
					return ExpenseClaim{}, err
				}
				return a.claimOf(ctx, u, v, after, false)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &claimOutput{Status: http.StatusOK, Applied: applied(w), Body: w.Body}, nil
			}
			return &claimOutput{Status: http.StatusOK, ETag: claimETag(w.Body), Body: w.Body}, nil
		})

	submitCodes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		CodeSessionRequired, CodeStepUpRequired, approval.CodePending, storage.CodeApproval, claim.CodeNotClaim, claim.CodeNotDraft, claim.CodeBadDraft,
		CodeClaimNotYours, storage.CodeRetry, storage.CodeLockTimeout, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid},
		append(slices.Clone(ClaimCodes), LedgerCodes...)...)

	register(a, authz.SubmitExpenseClaim, huma.Operation{
		OperationID: "submitExpenseClaim", Method: http.MethodPost, Path: "/v1/expense-claims/{claim_id}/submit", Tags: []string{"Expense claims"},
		Summary: "Submit an expense claim for approval", DefaultStatus: http.StatusAccepted,
		Description: "Submits the caller's own draft claim at the version read (FR-AP-17): it is checked whole, and what stops it (✖) comes " +
			"back with every problem at once. The answer is 202 with an approval request: one person other than the claimant who holds " +
			"A on \"Expense claims\" approves it, and the Controller too, as a second step, when a line is above its category's limit " +
			"(the owner's decision of 26 Oct 2026). Approving the last step posts it under expense_claim@1: Dr each line's expense and " +
			"its claimable input tax, Cr 2150 Expense claims payable with what the company owes. Rejecting it returns it to the claimant " +
			"with the comment, to fix and submit again. Nobody approves their own claim, in any team mode. Needs the claimant signed in, " +
			"stepped up, and If-Match with the claim's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks " +
			"nothing.",
	}, submitCodes, func(ctx context.Context, in *claimCommandInput) (*billRequestOutput, error) {
		id, err := parseID("claim_id", in.ClaimID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		p, err := claimant(ctx)
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
			v, err := a.claimView(ctx, u)
			if err != nil {
				return Approval{}, err
			}
			doc, _, err := ownClaim(ctx, u, p, id, v.base)
			if err != nil {
				return Approval{}, err
			}
			if doc.Status != storage.DocumentDraft {
				return Approval{}, &claim.Error{Code: claim.CodeNotDraft, Message: fmt.Sprintf("the claim is %s, not a draft", doc.Status),
					Fix: "Only a draft is submitted"}
			}
			c, err := a.claimOf(ctx, u, v, doc, false)
			if err != nil {
				return Approval{}, err
			}
			if err := checkIfMatch(in.IfMatch, claimETag(c)); err != nil {
				return Approval{}, err
			}
			checked, err := journal.CheckClaim(ctx, u, doc)
			if err != nil {
				return Approval{}, err
			}
			if len(checked.Problems) > 0 {
				return Approval{}, claimProblems(checked.Problems)
			}
			var then []approval.Step
			if checked.Prepared.OverLimit { // a line above its category's limit: finance approves too (FR-AP-17)
				then = []approval.Step{{Quorum: 1, Roles: []string{string(authz.Controller)}}}
			}
			owed := checked.Owed.String()
			name := cmpOr(c.Claimant.Name, "a member")
			what := cmpOr(c.Number, "claim")
			return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectClaim, ObjectKey: id.String(), Operation: approval.OpClaimPost,
				Summary: fmt.Sprintf("Expense claim %s of %s, %s %s", what, name, owed, v.base.Code),
				Execute: map[string]any{"claim_id": id.String(), "claim_version": doc.Version},
				Diff: []ApprovalDiff{{Field: "status", Before: "draft", After: "posted"}, {Field: "owed", After: owed + " " + v.base.Code},
					{Field: "finance_step", After: strconv.FormatBool(checked.Prepared.OverLimit)}},
				Amounts: []approval.Amount{{Amount: owed, Currency: v.base.Code}}, Then: then})
		})
		if err != nil {
			return nil, err
		}
		return &billRequestOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})
	registerClaimWithdrawal(a)
}

// claimWithdrawn is withdrawExpenseClaim's answer.
type claimWithdrawn struct {
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	ETag     string `header:"ETag" doc:"The claim's version and status, for If-Match."`
	Body     ExpenseClaim
}

func registerClaimWithdrawal(a *API) {
	register(a, authz.WithdrawExpenseClaim, huma.Operation{
		OperationID: "withdrawExpenseClaim", Method: http.MethodPost, Path: "/v1/expense-claims/{claim_id}/withdraw", Tags: []string{"Expense claims"},
		Summary: "Withdraw a submitted expense claim",
		Description: "Withdraws the caller's own submitted claim from approval: its request becomes void with the reason \"withdrawn by its " +
			"requester\", any approvals it had are voided, and the claim is a draft again to change and submit again. A claimant reaches " +
			"their claim's request only this way, never through the approvals (design D10). Needs the claimant signed in, and If-Match " +
			"with the claim's ETag. Side effects: the request void and its audit event. Idempotent with Idempotency-Key; Prefer: " +
			"return=preview checks it and saves nothing.",
	}, []string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeClaimNotYours,
		claim.CodeNotClaim, claim.CodeBadDraft, approval.CodeNotPending, storage.CodeApproval, storage.CodeVersion, storage.CodeRetry, storage.CodeLockTimeout},
		func(ctx context.Context, in *claimCommandInput) (*claimWithdrawn, error) {
			id, err := parseID("claim_id", in.ClaimID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			p, err := claimant(ctx)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (ExpenseClaim, error) {
				v, err := a.claimView(ctx, u)
				if err != nil {
					return ExpenseClaim{}, err
				}
				doc, _, err := ownClaim(ctx, u, p, id, v.base)
				if err != nil {
					return ExpenseClaim{}, err
				}
				before, err := a.claimOf(ctx, u, v, doc, false)
				if err != nil {
					return ExpenseClaim{}, err
				}
				if err := checkIfMatch(in.IfMatch, claimETag(before)); err != nil {
					return ExpenseClaim{}, err
				}
				r, ok := v.latest[id.String()]
				if before.Stage != "submitted" || !ok || r.RequestedBy != p.UserID {
					return ExpenseClaim{}, problem(approval.CodeNotPending, "The claim is not waiting for approval.", "Nothing to withdraw")
				}
				locked, err := u.Approvals().Lock(ctx, r.ID)
				if err != nil {
					return ExpenseClaim{}, err
				}
				if _, err := u.Approvals().Decide(ctx, locked.ID, locked.Version, string(approval.Void), "withdrawn by its requester", a.deps.Clock.Now()); err != nil {
					return ExpenseClaim{}, err
				}
				if v, err = a.claimView(ctx, u); err != nil {
					return ExpenseClaim{}, err
				}
				return a.claimOf(ctx, u, v, doc, false)
			})
			if err != nil {
				return nil, err
			}
			return &claimWithdrawn{Replayed: replayedHeader(w.Replayed), Applied: applied(w), ETag: claimETag(w.Body), Body: w.Body}, nil
		})
}

// lastClaim is the cursor after the last claim of a page: its date and ID.
func lastClaim(items []ExpenseClaim) string {
	last := items[len(items)-1]
	return last.Date + "|" + last.ID
}
