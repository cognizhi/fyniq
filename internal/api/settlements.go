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
// terms, repaid through a payment batch, or waived (converting comes with the register of share issues). An Owner or the
// Controller drafts it (A on "Director accounts") with its loan note, minute or deed, and asks for approval
// (director_settlement.approve), which another person who holds A gives, never the person settled (FM-9, APR-15; the
// database's FY253), and the Accountant too when every member who could sign is a director with a settlement of their
// own waiting or decided in the last 30 days ([accountant to confirm]). The approval keeps the loan with no entry, makes a
// repayment payable by the next payment batch into the person's account in force (its release posts Dr 2500/2120 naming
// the person (paid out) · Cr the bank), or posts the waiver: Dr 2500/2120 naming the person (waived) · Cr 3300 Capital
// contribution or an income account.

// SettlementCodes are the codes a settlement's own checks answer.
var SettlementCodes = []string{director.CodeSettlement, director.CodeNotPerson, director.CodeNotSettlement, director.CodeNoRepayTo}

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

// SettlementConversion is what a balance converted to shares is (FR-EQ-06, design D11).
type SettlementConversion struct {
	ClassAccountID      string `json:"class_account_id" format:"uuid" doc:"The class: its share-capital account, 3010 ordinary or 3020 preference." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d30"`
	Price               string `json:"price" doc:"The price per share in the base currency, a decimal string with at most 6 decimals." example:"1.20"`
	PriceBasis          string `json:"price_basis,omitempty" enum:"latest_priced_round,valuation,unanimous" doc:"Where the price comes from: the latest priced round, an independent valuation, or a price every shareholder approves. Left out, the setting founders.price_basis." example:"latest_priced_round"`
	Capacity            string `json:"capacity" enum:"lender,employee,shareholder" doc:"The capacity in which the founder converts." example:"lender"`
	Measurement         string `json:"measurement,omitempty" enum:"carrying,fair_value" doc:"The measurement basis the accountant picks: the carrying amount (the default) or the shares' fair value with the difference in profit or loss." example:"carrying"`
	FairValuePrice      string `json:"fair_value_price,omitempty" doc:"Under fair value: the shares' fair value per share." example:"1.25"`
	DifferenceAccountID string `json:"difference_account_id,omitempty" format:"uuid" doc:"Under fair value: the profit-or-loss account the difference goes to." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d71"`
	Note                string `json:"note,omitempty" maxLength:"1000" doc:"Under fair value: the accountant's note on the basis." example:"Measured at the seed round's price, as the founder converts as a lender"`
}

// SettlementShares are what a conversion allots under RND-SHARE.
type SettlementShares struct {
	Shares    string `json:"shares" doc:"The whole shares allotted." example:"10288"`
	Converted Money  `json:"converted" doc:"What they convert: round(shares × price)."`
	Remainder Money  `json:"remainder" doc:"What stays owed on the director's account."`
}

// ConversionACRA are an approved conversion's ACRA steps (FR-EQ-09, design D12) and what they raise.
type ConversionACRA struct {
	ResolvedOn        string           `json:"resolved_on,omitempty" format:"date" doc:"The day the shareholders resolved to issue the shares." example:"2026-10-06"`
	ApprovalLodgedOn  string           `json:"approval_lodged_on,omitempty" format:"date" doc:"The day their approval was lodged with ACRA: due within 14 days of the resolution (s161(5), s186)." example:"2026-10-12"`
	AGMDueOn          string           `json:"agm_due_on" format:"date" doc:"The date by which the next AGM must be held, when the approval lapses: as recorded, or worked out from the financial years [verify]." example:"2027-06-30"`
	AllotmentLodgedOn string           `json:"allotment_lodged_on,omitempty" format:"date" doc:"The day the return of allotment was lodged with the conversion agreement (s63, s63B)." example:"2026-10-20"`
	Steps             []ConversionStep `json:"steps" nullable:"false" doc:"Every step recorded, earliest first: a later one of a kind supersedes the earlier."`
	Flags             []ConversionFlag `json:"flags" nullable:"false" doc:"What the steps raise today: ⏰ due, or ⚠ warning. Empty once effective."`
}

// ConversionStep is one ACRA step as recorded.
type ConversionStep struct {
	ID         string `json:"id" format:"uuid" doc:"The step's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5f40"`
	Kind       string `json:"kind" enum:"resolved,approval_lodged,agm_due,allotment_lodged" doc:"Which step." example:"approval_lodged"`
	On         string `json:"on" format:"date" doc:"Its day." example:"2026-10-12"`
	Reference  string `json:"reference,omitempty" doc:"ACRA's transaction number, or what it rests on." example:"T26ACRA00123"`
	RecordedAt string `json:"recorded_at" format:"date-time" doc:"When it was recorded." example:"2026-10-12T09:30:00Z"`
}

// ConversionFlag is what an approved conversion's steps raise.
type ConversionFlag struct {
	Code    string `json:"code" doc:"The flag's stable code." example:"FOUNDER-38"`
	Kind    string `json:"kind" enum:"due,warning" doc:"due (⏰): a statutory step is due; warning (⚠): please review." example:"due"`
	Message string `json:"message" doc:"What it says." example:"the shareholders' approval to issue the shares is due to be lodged with ACRA by 20 Oct 2026"`
	Fix     string `json:"fix" doc:"What to do." example:"Lodge it, attach ACRA's acknowledgement and record the day it was lodged"`
	DueOn   string `json:"due_on,omitempty" format:"date" doc:"The day it is due, or lapses." example:"2026-10-20"`
}

// ConversionStepRequest is an ACRA step as recorded.
type ConversionStepRequest struct {
	Kind      string `json:"kind" enum:"resolved,approval_lodged,agm_due,allotment_lodged" doc:"Which step." example:"approval_lodged"`
	On        string `json:"on" format:"date" doc:"Its day: no later than today, except the AGM date." example:"2026-10-12"`
	Reference string `json:"reference,omitempty" maxLength:"200" doc:"ACRA's transaction number, or what it rests on." example:"T26ACRA00123"`
}

type conversionStepInput struct {
	DirectorSettlementPath
	KeyHeader
	PreferHeader
	Body ConversionStepRequest
}

// DirectorSettlementDraft is a settlement as drafted.
type DirectorSettlementDraft struct {
	ContactID  string                `json:"contact_id" format:"uuid" doc:"The director or shareholder the company owes." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	AccountID  string                `json:"account_id" format:"uuid" doc:"The director's account it is owed on: 2500 or 2120." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d61"`
	Amount     Money                 `json:"amount" doc:"The amount, in the base currency: at most what is owed on the account today."`
	Route      string                `json:"route,omitempty" enum:"keep,repay,convert,waive" doc:"keep as a loan on terms; repay through a payment batch, into the person's approved SGD account at a Singapore bank (FOUNDER-32 while they have none); convert to shares, approved now and effective once ACRA registers the allotment (FR-EQ-06); or waive (FR-EQ-07). Left out, the organisation's default route (founders.default_route), keep." example:"keep"`
	Terms      *SettlementTerms      `json:"terms,omitempty" doc:"A loan kept's terms."`
	Waiver     *SettlementWaiver     `json:"waiver,omitempty" doc:"A waiver's account, capacity and reason."`
	Conversion *SettlementConversion `json:"conversion,omitempty" doc:"A conversion's class, price and basis, capacity and measurement basis."`
}

// DirectorSettlement is a settlement, drafted, waiting for approval, or decided.
type DirectorSettlement struct {
	ID               string                `json:"id" format:"uuid" doc:"Its ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fa9"`
	Number           string                `json:"number,omitempty" doc:"Its number, once decided (FR-ORG-04)." example:"DS-0001"`
	Status           string                `json:"status" enum:"draft,waiting,decided,effective" doc:"draft; waiting for another person's approval; decided: the loan kept, the repayment payable by a payment batch, the waiver posted, or the conversion approved and pending ACRA's register; or effective: the conversion's shares in the register and posted." example:"waiting"`
	Version          int                   `json:"version" doc:"The version of its content; with its status it makes the ETag." example:"1"`
	Date             string                `json:"date" format:"date" doc:"The day it was last drafted." example:"2026-11-03"`
	ContactID        string                `json:"contact_id" format:"uuid" doc:"The person." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	LegalName        string                `json:"legal_name" doc:"Their name." example:"Mei Lin Tan"`
	AccountID        string                `json:"account_id" format:"uuid" doc:"The director's account." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d61"`
	AccountCode      string                `json:"account_code,omitempty" doc:"Its code." example:"2500"`
	Amount           Money                 `json:"amount" doc:"The amount settled."`
	Owed             Money                 `json:"owed" doc:"What the company owes the person on the account today."`
	Route            string                `json:"route" enum:"keep,repay,convert,waive" doc:"The route." example:"keep"`
	Terms            *SettlementTerms      `json:"terms,omitempty" doc:"A loan kept's terms."`
	Waiver           *SettlementWaiver     `json:"waiver,omitempty" doc:"A waiver's account, capacity and reason."`
	DeductedEarlier  bool                  `json:"deducted_earlier" doc:"A decided waiver of amounts the company deducted earlier as costs (pay or fees on 2120): tagged for the tax computation [verify]." example:"false"`
	RepayTo          *PayeeAccount         `json:"repay_to,omitempty" doc:"A repayment: the account in force it is paid into, the linked member's own (FR-AP-22) or else the contact's (FR-CT-03), by its last four characters; absent while there is none."`
	Repaid           *Money                `json:"repaid,omitempty" doc:"A decided repayment: what payment batches have repaid of it, net of voids; the rest is open for the next batch."`
	Conversion       *SettlementConversion `json:"conversion,omitempty" doc:"A conversion's class, price and basis, capacity and measurement basis."`
	Shares           *SettlementShares     `json:"shares,omitempty" doc:"A conversion: the whole shares RND-SHARE allots, what they convert and what stays owed."`
	LapsesOn         string                `json:"lapses_on,omitempty" format:"date" doc:"An approved conversion not yet effective: the day the shareholders' approval lapses, the date by which the next AGM must be held [verify]; after it a new approval is needed." example:"2027-06-30"`
	ShareIssue       *ShareIssue           `json:"share_issue,omitempty" doc:"An effective conversion: its share issue in the register."`
	ACRA             *ConversionACRA       `json:"acra,omitempty" doc:"An approved conversion: its ACRA steps and what they raise (FR-EQ-09)."`
	Agreements       int                   `json:"agreements" doc:"How many files are attached and not removed: asking for approval needs one, the loan note, minute or deed." example:"1"`
	ApprovalAsked    string                `json:"approval_asked,omitempty" format:"uuid" doc:"The pending request to decide it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4dd1"`
	Checks           []BillCheck           `json:"checks" nullable:"false" doc:"A draft's statuses: ✖ while no agreement is attached, or while it does not fit what is owed now."`
	LedgerImpact     []EntryLine           `json:"ledger_impact" nullable:"false" doc:"A waiver's entry: the lines it would post as a dry run while undecided (FR-APR-02), and the lines it posted once decided; empty for a loan kept."`
	AccountantReview bool                  `json:"accountant_review" doc:"Its request has a final step the Accountant signs, as every member who could sign benefits from a settlement of their own (FM-9) [accountant to confirm]." example:"false"`
}

// DirectorSettlementPage is a page of settlements, the latest first.
type DirectorSettlementPage struct {
	Items        []DirectorSettlement `json:"items" nullable:"false"`
	DefaultRoute string               `json:"default_route" enum:"keep,repay,convert,waive" doc:"The route a new settlement starts with: the setting founders.default_route in force today (FR-EQ-05, recommended default 15)." example:"keep"`
	NextCursor   string               `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjYtMTEtMDN8MDE5MiJ9"`
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

// SettlementEffective is what records a conversion as registered by ACRA.
type SettlementEffective struct {
	LodgementRef   string `json:"lodgement_ref" minLength:"1" maxLength:"100" doc:"ACRA's lodgement reference for the allotment." example:"AA2026113000456"`
	ResolutionRefs string `json:"resolution_refs" minLength:"1" maxLength:"500" doc:"The shareholders' approval and the board resolution, by reference." example:"Written resolution of 6 Oct 2026; board resolution of 8 Oct 2026"`
	EffectiveOn    string `json:"effective_on" format:"date" doc:"The day ACRA's register of members shows the allotment: no later than today, no earlier than the approval." example:"2026-11-30"`
}

type settlementEffectiveInput struct {
	DirectorSettlementPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body SettlementEffective
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
	if c := d.Conversion; c != nil {
		class, err := uuid.Parse(c.ClassAccountID)
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.conversion.class_account_id", Message: "is not an ID", Fix: "Send the share-capital account's ID"})
		}
		s.Conversion = &director.Conversion{ClassAccountID: class, Price: strings.TrimSpace(c.Price), PriceBasis: c.PriceBasis, Capacity: c.Capacity,
			Measurement: cmpOr(c.Measurement, director.MeasurementCarrying), FairValuePrice: strings.TrimSpace(c.FairValuePrice), Note: strings.TrimSpace(c.Note)}
		if s.Conversion.PriceBasis == "" {
			if s.Conversion.PriceBasis, err = director.DefaultPriceBasis(ctx, u, today); err != nil {
				return s, err
			}
		}
		if c.DifferenceAccountID != "" {
			if s.Conversion.DifferenceAccountID, err = uuid.Parse(c.DifferenceAccountID); err != nil {
				fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.conversion.difference_account_id", Message: "is not an ID", Fix: "Send the account's ID"})
			}
		}
	}
	if len(fields) > 0 {
		return s, invalid(fields)
	}
	return s, nil
}

// settlementView is what showing settlements needs once: the base currency, the chart, the persons' names, today, and
// each settlement's pending request.
type settlementView struct {
	profile  storage.OrganisationProfile
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
	o, err := u.Organisation(ctx)
	if err != nil {
		return v, err
	}
	v.profile = o.Profile
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
	if c := s.Conversion; c != nil {
		out.Conversion = &SettlementConversion{ClassAccountID: c.ClassAccountID.String(), Price: c.Price, PriceBasis: c.PriceBasis, Capacity: c.Capacity,
			Measurement: c.Measurement, FairValuePrice: c.FairValuePrice, Note: c.Note}
		if c.DifferenceAccountID != uuid.Nil {
			out.Conversion.DifferenceAccountID = c.DifferenceAccountID.String()
		}
		if sh, _, err := s.Shares(v.base); err == nil {
			out.Shares = &SettlementShares{Shares: sh.Count.Text('f'), Converted: moneyOut(sh.Converted), Remainder: moneyOut(sh.Remainder)}
		}
		if doc.Status == storage.DocumentPosted {
			issue, err := u.ShareIssues().OfSettlement(ctx, doc.ID)
			if err != nil {
				return DirectorSettlement{}, err
			}
			if issue != nil {
				x := shareIssueOf(*issue, v.names[issue.HolderID], v.accounts[issue.ClassAccountID].Code)
				out.ShareIssue = &x
			}
			if s.ApprovedOn != nil {
				if out.ACRA, err = a.conversionACRA(ctx, u, v, doc.ID, *s.ApprovedOn, issue != nil); err != nil {
					return DirectorSettlement{}, err
				}
				if issue == nil {
					out.LapsesOn = out.ACRA.AGMDueOn
				}
			}
		}
	}
	if s.Route == director.RouteRepay {
		_, _, bank, err := director.RepayTo(ctx, u, s.ContactID)
		if err != nil {
			return DirectorSettlement{}, err
		}
		if bank != nil {
			acc := payeeAccountOf(*bank)
			out.RepayTo = &acc
		}
		if doc.Status == storage.DocumentPosted {
			p, err := director.PayableOf(ctx, u, v.base, doc.ID)
			if err != nil {
				return DirectorSettlement{}, err
			}
			repaid, err := s.Amount.Sub(p.Open)
			if err != nil {
				return DirectorSettlement{}, err
			}
			m := moneyOut(repaid)
			out.Repaid = &m
		}
	}
	if r, ok := v.asked[doc.ID.String()]; ok {
		out.Status, out.ApprovalAsked = "waiting", r.ID.String()
		out.AccountantReview = len(r.PolicySteps) > 1
	}
	if doc.Status == storage.DocumentPosted {
		out.Status = "decided"
		switch {
		case s.Route == director.RouteWaive:
			e, err := u.Ledger().ByIdentity(ctx, ledger.DirectorWaiverIdentity(doc.ID, doc.Version, ledger.RuleDirectorWaiver))
			if err != nil {
				return DirectorSettlement{}, err
			}
			out.LedgerImpact = entryOf(e).Lines
		case out.ShareIssue != nil && out.ShareIssue.EntryID != "": // effective: what the register posted
			out.Status = "effective"
			e, err := u.Ledger().Entry(ctx, uuid.MustParse(out.ShareIssue.EntryID))
			if err != nil {
				return DirectorSettlement{}, err
			}
			out.LedgerImpact = entryOf(e).Lines
		case s.Route == director.RouteConvert: // approved, pending ACRA: what it would post if registered today
			settings, err := journal.LoadSettings(ctx, u)
			if err != nil {
				return DirectorSettlement{}, err
			}
			if c, _, err := s.ConversionRule(doc.ID, v.today, doc.Number, v.base); err == nil {
				if e, err := ledger.PostConversion(c, settings); err == nil {
					out.LedgerImpact = impactOf(e)
				}
			}
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
		out.LedgerImpact = impactOf(*c.Entry)
	}
	return out, nil
}

// conversionACRA is an approved conversion's ACRA steps, and what they raise today while it is not effective.
func (a *API) conversionACRA(ctx context.Context, u storage.UnitOfWork, v settlementView, id uuid.UUID, approvedOn time.Time, effective bool) (*ConversionACRA, error) {
	all, err := u.ConversionSteps().Of(ctx, id)
	if err != nil {
		return nil, err
	}
	steps := director.StepsOf(all)
	out := &ConversionACRA{Steps: []ConversionStep{}, Flags: []ConversionFlag{}}
	for _, x := range all {
		out.Steps = append(out.Steps, ConversionStep{ID: x.ID.String(), Kind: x.Kind, On: dateOf(x.On), Reference: x.Reference, RecordedAt: x.RecordedAt.UTC().Format(time.RFC3339)})
	}
	on := func(kind string) string {
		if x, ok := steps[kind]; ok {
			return dateOf(x.On)
		}
		return ""
	}
	out.ResolvedOn, out.ApprovalLodgedOn, out.AllotmentLodgedOn = on(storage.StepResolved), on(storage.StepApprovalLodged), on(storage.StepAllotmentLodged)
	lapses, ok := director.LapsesOnFor(steps, v.profile, approvedOn)
	if ok {
		out.AGMDueOn = dateOf(lapses)
	}
	if effective {
		return out, nil
	}
	days, err := director.RegistrationDays(ctx, u, v.today)
	if err != nil {
		return nil, err
	}
	for _, f := range director.Flags(steps, approvedOn, lapses, v.today, days) {
		x := ConversionFlag{Code: f.Code, Kind: f.Kind, Message: f.Message, Fix: f.Fix}
		if f.DueOn != nil {
			x.DueOn = dateOf(*f.DueOn)
		}
		out.Flags = append(out.Flags, x)
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
		if slices.ContainsFunc(m.Roles, func(r storage.RoleAssignment) bool {
			return r.Role == string(authz.Owner) || r.Role == string(authz.Controller)
		}) {
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
			"latest first, a page at a time, narrowed by person (FR-EQ-05), with the route a new one starts with (founders.default_route). " +
			"Needs the right to read every director's account. Read-only; no side effects.",
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
			if out.Body.DefaultRoute, err = director.DefaultRoute(ctx, u, v.today); err != nil {
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
		if w.Body.Status == "decided" || w.Body.Status == "effective" {
			status = "posted"
		}
		return &settlementOutput{Status: w.Status, ETag: etag(w.Body.Version, status), Location: "/v1/director-settlements/" + w.Body.ID,
			Replayed: replayedHeader(w.Replayed), Body: w.Body}
	}

	register(a, authz.CreateDirectorSettlement, huma.Operation{
		OperationID: "createDirectorSettlement", Method: http.MethodPost, Path: "/v1/director-settlements", Tags: []string{"Founders"},
		DefaultStatus: http.StatusCreated, Summary: "Draft a settlement of a founder's balance",
		Description: "Drafts a settlement of what the company owes a director or shareholder on 2500 or 2120 (FR-EQ-05): at most what is owed " +
			"today less what other settlements approved to be repaid still have open, by a route (keep as a loan on terms; repay through " +
			"a payment batch, into the person's approved account (FOUNDER-32 while there is none); convert to shares, with the class, the " +
			"price per share and its basis, the capacity and the measurement basis, the shares by RND-SHARE; or waive with the account it credits, " +
			"the capacity and the reason; the default route is the setting founders.default_route). Attach the loan note, minute or deed with addDirectorSettlementEvidence, then " +
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

	register(a, authz.RecordConversionStep, huma.Operation{
		OperationID: "recordConversionStep", Method: http.MethodPost, Path: "/v1/director-settlements/{settlement_id}/acra-steps",
		Tags: []string{"Founders"}, DefaultStatus: http.StatusCreated, Summary: "Record an ACRA step of a conversion",
		Description: "Records one ACRA step of an approved conversion not yet effective (FR-EQ-09, design D12): the day the shareholders " +
			"resolved; the day their approval was lodged with ACRA, due within 14 days of the resolution (s161(5), s186; ⏰ until it is); " +
			"the date by which the next AGM must be held, when it differs from the one worked out [verify], which is when the approval " +
			"lapses; or the day the return of allotment was lodged. Attach ACRA's acknowledgement with addDirectorSettlementEvidence. " +
			"A later step of a kind supersedes the earlier, which stays. Needs a person signed in who passed step-up. Side effects: the " +
			"step, append-only, and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, []string{director.CodeStep, director.CodeNotApproved, storage.CodeConversionStep, CodeNotFound, CodeSessionRequired, CodeStepUpRequired, CodeInvalid,
		CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, storage.CodeRetry, storage.CodeLockTimeout},
		func(ctx context.Context, in *conversionStepInput) (*settlementOutput, error) {
			id, err := parseID("settlement_id", in.SettlementID)
			if err != nil {
				return nil, err
			}
			on, err := time.Parse(time.DateOnly, in.Body.On)
			if err != nil {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.on", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"}})
			}
			p, err := a.steppedUpPerson(ctx, "Recording a conversion's ACRA step")
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (DirectorSettlement, error) {
				doc, err := u.Documents().Lock(ctx, id)
				if err != nil {
					return DirectorSettlement{}, err
				}
				st, err := director.ReadSettlement(doc)
				if err != nil {
					return DirectorSettlement{}, err
				}
				issue, err := u.ShareIssues().OfSettlement(ctx, id)
				if err != nil {
					return DirectorSettlement{}, err
				}
				if doc.Status != storage.DocumentPosted || st.Route != director.RouteConvert || issue != nil {
					return DirectorSettlement{}, &director.Error{Code: director.CodeNotApproved, Message: "only an approved conversion waiting for ACRA records its steps",
						Fix: "Record the steps between the conversion's approval and its effective date"}
				}
				today, err := orgToday(ctx, a, u)
				if err != nil {
					return DirectorSettlement{}, err
				}
				ref := strings.TrimSpace(in.Body.Reference)
				if err := director.CheckStep(in.Body.Kind, on, ref, today); err != nil {
					return DirectorSettlement{}, err
				}
				if _, err := u.ConversionSteps().Record(ctx, storage.ConversionStep{SettlementID: id, Kind: in.Body.Kind, On: on, Reference: ref,
					RecordedBy: p.UserID, RecordedAt: a.deps.Clock.Now()}); err != nil {
					return DirectorSettlement{}, err
				}
				return shown(ctx, u, id)
			})
			if err != nil {
				return nil, err
			}
			return answer(w), nil
		})

	register(a, authz.RecordConversionEffective, huma.Operation{
		OperationID: "recordConversionEffective", Method: http.MethodPost, Path: "/v1/director-settlements/{settlement_id}/effective",
		Tags: []string{"Founders"}, Summary: "Record a conversion as registered by ACRA",
		Description: "Makes an approved conversion of a founder's balance effective (FR-EQ-06, FR-EQ-09, design D11): ACRA's lodgement " +
			"reference, the resolutions it rests on, and the effective date, the day ACRA's electronic register of members shows the " +
			"allotment: no later than today, no earlier than the approval, and no later than the day the shareholders' approval lapses " +
			"(lapses_on; FOUNDER-36). In one transaction it posts the conversion from the register on that date, never backdated " +
			"(share_issue@1: Dr 2500 or 2120 naming the person, converted; Cr the class's share capital; under fair value, the " +
			"difference to the account chosen), and records the share issue with its entry. A period that is closed refuses it: " +
			"reopen the period, never a later date. Needs a person signed in who passed step-up, never the person converting " +
			"(APR-15), and If-Match with the settlement's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and " +
			"saves nothing.",
	}, append([]string{director.CodeNotApproved, director.CodeEffective, director.CodeSettlement, ledger.CodeConversion, storage.CodeShareIssue,
		approval.CodeBeneficiary, CodeSessionRequired, CodeStepUpRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}, postingCodes()...),
		func(ctx context.Context, in *settlementEffectiveInput) (*settlementOutput, error) {
			id, err := parseID("settlement_id", in.SettlementID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			on, err := time.Parse(time.DateOnly, in.Body.EffectiveOn)
			if err != nil {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.effective_on", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"}})
			}
			p, err := a.steppedUpPerson(ctx, "Recording a conversion as registered")
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (DirectorSettlement, error) {
				doc, err := u.Documents().Lock(ctx, id)
				if err != nil {
					return DirectorSettlement{}, err
				}
				if !director.IsSettlement(doc) {
					return DirectorSettlement{}, ports.ErrNotFound
				}
				if err := checkIfMatch(in.IfMatch, settlementETag(doc)); err != nil {
					return DirectorSettlement{}, err
				}
				st, err := director.ReadSettlement(doc)
				if err != nil {
					return DirectorSettlement{}, err
				}
				if c, err := u.Contacts().Get(ctx, st.ContactID); err != nil {
					return DirectorSettlement{}, err
				} else if c.MemberUserID != nil && *c.MemberUserID == p.UserID {
					return DirectorSettlement{}, problem(approval.CodeBeneficiary, "The conversion is yours, so another person records it (FM-9).",
						"Ask another person who holds the approval right on director accounts")
				}
				v, err := a.settlementView(ctx, u)
				if err != nil {
					return DirectorSettlement{}, err
				}
				var lapses time.Time
				if st.ApprovedOn != nil {
					all, err := u.ConversionSteps().Of(ctx, id)
					if err != nil {
						return DirectorSettlement{}, err
					}
					lapses, _ = director.LapsesOnFor(director.StepsOf(all), v.profile, *st.ApprovedOn)
				}
				if _, _, err := a.settlements.EffectiveIn(ctx, u, id, journal.Effective{LodgementRef: strings.TrimSpace(in.Body.LodgementRef),
					ResolutionRefs: strings.TrimSpace(in.Body.ResolutionRefs), EffectiveOn: on, Today: v.today, LapsesOn: lapses, RecordedBy: p.UserID}); err != nil {
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
			"entry, a repayment becomes payable by the next payment batch, into the person's account in force (repay_to in the diff), " +
			"and a waiver posts director_settlement@1 on that day: Dr 2500 or 2120 naming the person (waived), Cr the account " +
			"chosen; a waiver of 2120 is tagged deducted earlier [verify]. The request holds the settlement's version, status, amount and " +
			"what is owed, so a change voids it. Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview " +
			"checks it and asks nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, director.CodeSettled, director.CodeNoAgreement, CodeIdempotencyReused, CodeIdempotencyBusy,
		CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, storage.CodeRetry, storage.CodeLockTimeout, ledger.CodeWaiver, ledger.CodeParty,
		ledger.CodeConversion,
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
				if cv := s.Conversion; s.Route == director.RouteConvert && cv != nil { // what the shareholders approve (FR-EQ-06)
					if sh, _, err := s.Shares(v.base); err == nil {
						diff = append(diff, ApprovalDiff{Field: "shares", After: sh.Count.Text('f') + " at " + cv.Price + " a share (" + cv.PriceBasis + ")"},
							ApprovalDiff{Field: "converted", After: sh.Converted.String() + " " + sh.Converted.Currency().Code},
							ApprovalDiff{Field: "stays owed", After: sh.Remainder.String() + " " + sh.Remainder.Currency().Code})
					}
					diff = append(diff, ApprovalDiff{Field: "capacity", After: cv.Capacity}, ApprovalDiff{Field: "measurement", After: cv.Measurement})
				}
				if s.Route == director.RouteRepay { // where the next payment batch pays it: the person's account in force now
					_, _, bank, err := director.RepayTo(ctx, u, s.ContactID)
					if err != nil {
						return Approval{}, err
					}
					if bank != nil {
						diff = append(diff, ApprovalDiff{Field: "repay_to", After: accountWords(payeeAccountOf(*bank))})
					}
				}
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
