package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/team"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ bills (FR-AP-02, FM-1, FM-2, SOD-3; p1-purchases)
//
// A supplier's bill is drafted, checked, posted and voided as a journal is, through the posting service (bill@1).
// Approval as for journals: a bill whose total in the base currency is above the self-approval threshold posts only on
// another person's approval, in any team mode; at or below it, a Founder-mode post is logged as self-approved for the
// weekly review (FM-2, FM-3); whoever prepared it never approves it (SOD-3); and in Founder mode voiding a posted bill
// needs a second person (FM-1).

// The API's codes for bills' approvals.
const (
	CodeBillNeedsApproval = "BILL-09" // the bill's total is above the self-approval threshold: ask for approval (FM-2)
	CodeBillVoidApproval  = "BILL-10" // in Founder mode a void needs a second person (FM-1)
	CodeOwnBill           = "BILL-11" // whoever prepared the bill never approves its post (SOD-3)
	CodeBillEditApproval  = "BILL-12" // in Founder mode an edit of a posted bill needs a second person (FM-1)
)

// BillCodes are the codes the posting rules and the bill's own checks answer.
var BillCodes = []string{bill.CodeSupplier, bill.CodeGSTCode, bill.CodeDueDate, bill.CodeTooLong, ledger.CodeBillTotal, ledger.CodeSystemRole,
	ledger.CodeRuleAccount, ledger.CodeLineCurrency}

// BillApprovalCodes are the codes an approval of a bill's post or void may answer: SOD-3, and the posting's own.
func BillApprovalCodes() []string {
	return append([]string{CodeOwnBill, bill.CodeNotBill, bill.CodeNotDraft, bill.CodeNotPosted, bill.CodeBadDraft}, BillCodes...)
}

// BillLineDraft is one line of a bill as entered.
type BillLineDraft struct {
	AccountID   string `json:"account_id" format:"uuid" doc:"The expense or asset account the line costs." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d60"`
	Description string `json:"description,omitempty" maxLength:"500" doc:"What the line is for." example:"Confluence, 10 seats, October"`
	Net         Money  `json:"net" doc:"The line's amount before GST, in the bill's currency."`
	GSTCode     string `json:"gst_code" pattern:"^[A-Z]{2,4}$" doc:"The line's GST code for purchases, from the sg-gst pack: TX, IM, ZP, BL, NR, EP or OP." example:"TX"`
	GST         Money  `json:"gst" doc:"The line's GST as the supplier states it, in the bill's currency; 0 when none is charged."`
	StatedGST   *Money `json:"stated_gst,omitempty" doc:"For a bill in another currency: the GST the tax invoice states in the base currency, which is the input tax claimed (ENG-42). Without it the input tax is not claimed and stays in the cost (⚠)."`
}

// BillDraft is a bill as entered: checked for its form when saved, and by the posting rules when it posts.
type BillDraft struct {
	SupplierID  string          `json:"supplier_id,omitempty" format:"uuid" doc:"The supplier: a contact marked as a vendor." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e20"`
	SupplierRef string          `json:"supplier_ref,omitempty" maxLength:"60" doc:"The supplier's own number for the bill, as on their invoice." example:"AU-1182"`
	Date        string          `json:"date,omitempty" format:"date" doc:"The bill's date: its tax point." example:"2026-10-14"`
	DueDate     string          `json:"due_date,omitempty" format:"date" doc:"When it falls due; left out, the supplier's payment terms, else the organisation's (FR-ORG-04)." example:"2026-11-13"`
	Currency    string          `json:"currency" pattern:"^[A-Z]{3}$" doc:"The bill's currency." example:"SGD"`
	FXRate      string          `json:"fx_rate,omitempty" doc:"For a bill in another currency: base currency per one unit, as typed on the document. Left out, the bill takes the cascade's rate for its date (ENG-40)." example:"1.34"`
	Lines       []BillLineDraft `json:"lines" maxItems:"200" doc:"The bill's lines."`
	Notes       string          `json:"notes,omitempty" maxLength:"2000" doc:"Notes kept with the bill." example:"Annual renewal, 10 seats"`
}

// BillSupplier is a bill's supplier, as the bill shows it.
type BillSupplier struct {
	ID   string `json:"id" format:"uuid" doc:"The contact's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e20"`
	Name string `json:"name,omitempty" doc:"The contact's legal name." example:"Atlassian Pty Ltd"`
}

// BillStates are a bill's separate states (FR-AP-02, as FR-AR-03's for invoices): only posting and voiding change the
// ledger; becoming overdue never does.
type BillStates struct {
	Approval string `json:"approval" enum:"draft,awaiting_approval,approved,rejected" doc:"draft until it is posted or asked for; awaiting_approval while a request to post it waits; approved once posted; rejected when the latest request was rejected and it is still a draft." example:"draft"`
	Ledger   string `json:"ledger" enum:"not_posted,posted,voided" doc:"not_posted, posted, or voided by a reversal." example:"not_posted"`
	Payment  string `json:"payment" enum:"unpaid,scheduled,part_paid,paid,written_off" doc:"What has been paid of it. Payments arrive with p1-purchases' payments slice; until then a posted bill is unpaid." example:"unpaid"`
	Due      string `json:"due" enum:"not_due,overdue" doc:"overdue once its due date has passed while it is posted and unpaid; not_due otherwise." example:"not_due"`
}

// BillCheck is one of a bill's statuses: an error (✖) that stops it posting, or a warning (⚠) it posts with and the
// approver sees.
type BillCheck struct {
	Severity string `json:"severity" enum:"error,warning" doc:"error (✖): it cannot post until fixed; warning (⚠): it posts, and is reviewed." example:"warning"`
	Code     string `json:"code" doc:"The rule's stable code (ENG-23)." example:"BILL-GST-DIFFERS"`
	Field    string `json:"field,omitempty" doc:"The field it is about." example:"lines"`
	Message  string `json:"message" doc:"What is wrong, or why it is flagged." example:"the stated GST 2.73 SGD differs by 0.01 SGD from the recomputation"`
	Fix      string `json:"fix,omitempty" doc:"How to fix it." example:"Check the GST against the supplier's invoice"`
}

// Bill is a supplier's bill: its current version, its states and what posting it does.
type Bill struct {
	ID            string          `json:"id" format:"uuid" doc:"The bill's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e30"`
	Number        string          `json:"number,omitempty" doc:"Its number from the bill sequence: given when it posts, or when it is saved if the sequence is not gap-free (FR-ORG-04)." example:"BILL-0007"`
	Status        string          `json:"status" enum:"draft,posted,reversed" doc:"Draft (✎), posted, or reversed by a void." example:"draft"`
	Version       int             `json:"version" doc:"The version of its content; with the status it makes the ETag." example:"1"`
	Supplier      *BillSupplier   `json:"supplier,omitempty" doc:"The supplier, with its name."`
	SupplierRef   string          `json:"supplier_ref,omitempty" doc:"The supplier's own number for it." example:"AU-1182"`
	Date          string          `json:"date,omitempty" format:"date" doc:"The bill's date." example:"2026-10-14"`
	DueDate       string          `json:"due_date,omitempty" format:"date" doc:"When it falls due: as entered, or from the payment terms." example:"2026-11-13"`
	Currency      string          `json:"currency" doc:"The bill's currency." example:"SGD"`
	FXRate        string          `json:"fx_rate,omitempty" doc:"The rate typed on the document, if one was." example:"1.34"`
	Lines         []BillLineDraft `json:"lines" nullable:"false" doc:"The lines as entered."`
	Notes         string          `json:"notes,omitempty" doc:"Its notes." example:"Annual renewal"`
	Total         Money           `json:"total" doc:"Every line's net and GST, in the bill's currency."`
	Payable       *Money          `json:"payable,omitempty" doc:"What it adds to accounts payable in the base currency: as posted, or as posting would post it; absent while it cannot post."`
	States        BillStates      `json:"states" doc:"Its approval, ledger, payment and due states."`
	Checks        []BillCheck     `json:"checks" nullable:"false" doc:"getBill only: what stops a draft posting (✖), and the warnings it posts with (⚠)."`
	LedgerImpact  []EntryLine     `json:"ledger_impact" nullable:"false" doc:"getBill only: the lines posting it writes (a draft), or wrote (posted): debits positive."`
	PreparedBy    *Person         `json:"prepared_by,omitempty" doc:"Who saved the current version."`
	PostedBy      *Person         `json:"posted_by,omitempty" doc:"Who posted it."`
	PostedAt      *time.Time      `json:"posted_at,omitempty" doc:"When it was posted." example:"2026-10-14T09:30:00Z"`
	ApprovalAsked string          `json:"approval_asked,omitempty" format:"uuid" doc:"getBill only: the pending request to post or void it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4dd1"`
}

// BillPage is a page of bills, newest date first.
type BillPage struct {
	Items      []Bill `json:"items" nullable:"false"`
	NextCursor string `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjYtMTAtMzF8MDE5MiJ9"`
}

// BillPath names a bill in the path.
type BillPath struct {
	BillID string `path:"bill_id" format:"uuid" doc:"The bill's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e30"`
}

type billListInput struct {
	PageQuery
	Status string `query:"status" enum:"draft,posted,reversed" doc:"Only bills with this status." example:"draft"`
	From   string `query:"from" format:"date" doc:"Only bills dated on or after this day." example:"2026-10-01"`
	To     string `query:"to" format:"date" doc:"Only bills dated on or before this day." example:"2026-10-31"`
	Q      string `query:"q" maxLength:"100" doc:"Only bills whose number contains this text, ignoring case." example:"BILL-0007"`
}

type billListOutput struct{ Body BillPage }

type createBillInput struct {
	KeyHeader
	PreferHeader
	Body BillDraft
}

type replaceBillInput struct {
	BillPath
	IfMatchHeader
	PreferHeader
	Body BillDraft
}

type billOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The bill's version and status, for If-Match."`
	Location string `header:"Location" doc:"The bill's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     Bill
}

type billReadOutput struct {
	ETag string `header:"ETag" doc:"The bill's version and status, for If-Match."`
	Body Bill
}

type billCommandInput struct {
	BillPath
	KeyHeader
	IfMatchHeader
	PreferHeader
}

type billVoidInput struct {
	BillPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body Reversal
}

// BillEdit is the correction of a posted bill: its new content, and why (FR-GL-12).
type BillEdit struct {
	BillDraft
	Reason string `json:"reason" minLength:"1" maxLength:"500" doc:"Why it is edited: the audit trail and the reversal's memo keep it (FR-GL-12)." example:"The supplier credited one seat"`
}

// BillEdited is what an edit posted.
type BillEdited struct {
	Bill     Bill  `json:"bill" doc:"The bill at its new version, still posted, with whoever posted the edit."`
	Reversal Entry `json:"reversal" doc:"The entry that reverses the one the bill had, dated on that entry's date."`
	Entry    Entry `json:"entry" doc:"The entry of the new version, on its date."`
}

type billEditInput struct {
	BillPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body BillEdit
}

type billEditOutput struct {
	ETag     string `header:"ETag" doc:"The bill's new version and status, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved, and the entries show what the edit would post (FR-INT-14)."`
	Body     BillEdited
}

// BillPosting is a bill after a post or a void, with the entry it made.
type BillPosting struct {
	Bill  Bill  `json:"bill" doc:"The bill after the command."`
	Entry Entry `json:"entry" doc:"The entry the command posted: the bill's entry, or its reversing entry."`
}

type billPostingOutput struct {
	ETag     string `header:"ETag" doc:"The bill's new version and status, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved, and the entry shows what the command would write (FR-INT-14)."`
	Body     BillPosting
}

type billRequestOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     Approval
}

func billETag(b Bill) string { return etag(b.Version, b.Status) }

// moneyIn reads a Money of the request at path.
func moneyIn(m Money, path string) (money.Money, *FieldProblem) {
	cur, err := money.Lookup(m.Currency)
	if err != nil {
		return money.Money{}, &FieldProblem{Code: money.CodeCurrency, Field: path + ".currency", Message: "is not a currency Fyniq knows", Fix: "Send an ISO 4217 code such as SGD"}
	}
	v, err := money.ParseField(m.Amount, cur, path+".amount")
	if err != nil {
		f := fieldOf(err, path+".amount")
		return money.Money{}, &f
	}
	return v, nil
}

// billDraftOf reads a bill from the request, with every invalid field at once.
func billDraftOf(in BillDraft, base money.Currency) (bill.Draft, error) {
	var d bill.Draft
	var fields []FieldProblem
	if in.SupplierID != "" {
		id, err := uuid.Parse(in.SupplierID)
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.supplier_id", Message: "is not an ID", Fix: "Send the supplier's contact ID"})
		}
		d.SupplierID = id
	}
	d.SupplierRef, d.Notes = strings.TrimSpace(in.SupplierRef), in.Notes
	for _, x := range []struct {
		text, field string
		to          *time.Time
	}{{in.Date, "body.date", &d.Date}, {in.DueDate, "body.due_date", &d.DueDate}} {
		if x.text == "" {
			continue
		}
		t, err := time.Parse(time.DateOnly, x.text)
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: x.field, Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"})
		}
		*x.to = t
	}
	cur, err := money.Lookup(in.Currency)
	if err != nil {
		return d, invalid(append(fields, FieldProblem{Code: money.CodeCurrency, Field: "body.currency", Message: "is not a currency Fyniq knows",
			Fix: "Send an ISO 4217 code such as SGD"}))
	}
	d.Currency = cur
	if in.FXRate != "" {
		r, err := money.ParseRate(in.FXRate, cur, base, RateSourceDocument)
		if err != nil {
			fields = append(fields, fieldOf(err, "body.fx_rate"))
		} else {
			d.Rate = &r
		}
	}
	for i, l := range in.Lines {
		at := func(f string) string { return "body.lines[" + strconv.Itoa(i) + "]." + f }
		id, err := uuid.Parse(l.AccountID)
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: at("account_id"), Message: "is not an ID", Fix: "Send the account's ID"})
		}
		line := bill.Line{AccountID: id, Description: l.Description, GSTCode: l.GSTCode}
		var bad *FieldProblem
		if line.Net, bad = moneyIn(l.Net, at("net")); bad != nil {
			fields = append(fields, *bad)
			continue
		}
		if line.GST, bad = moneyIn(l.GST, at("gst")); bad != nil {
			fields = append(fields, *bad)
			continue
		}
		if l.StatedGST != nil {
			s, bad := moneyIn(*l.StatedGST, at("stated_gst"))
			if bad != nil {
				fields = append(fields, *bad)
				continue
			}
			line.StatedGST = &s
		}
		d.Lines = append(d.Lines, line)
	}
	if len(fields) > 0 {
		return d, invalid(fields)
	}
	return d, nil
}

// billView is what showing bills reads once per request: the base currency, the organisation's today, the accounts
// payable account, and the pending and rejected requests about bills.
type billView struct {
	base     money.Currency
	today    time.Time
	payables uuid.UUID
	asked    map[string]string // a bill's ID: its pending request
	rejected map[string]int    // a bill's ID: the version its latest rejected request to post was for
}

func (a *API) billView(ctx context.Context, u storage.UnitOfWork) (billView, error) {
	o, err := u.Organisation(ctx)
	if err != nil {
		return billView{}, err
	}
	v := billView{asked: map[string]string{}, rejected: map[string]int{}}
	if v.base, err = money.Lookup(o.BaseCurrency); err != nil {
		return billView{}, err
	}
	if v.today, err = orgToday(ctx, a, u); err != nil {
		return billView{}, err
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return billView{}, err
	}
	for _, x := range accounts {
		if string(x.Role) == ledger.RoleAccountsPayable {
			v.payables = x.ID
		}
	}
	requests, err := u.Approvals().List(ctx, "")
	if err != nil {
		return billView{}, err
	}
	now := a.deps.Clock.Now()
	for _, r := range requests {
		if r.ObjectType != approval.ObjectBill {
			continue
		}
		switch approval.StatusAt(requestOf(r), now) {
		case approval.Pending:
			v.asked[r.ObjectKey] = r.ID.String()
		case approval.Rejected:
			if r.Operation == approval.OpBillPost {
				var c struct {
					Version string `json:"version"`
				}
				if json.Unmarshal(r.Change, &c) == nil {
					if n, err := strconv.Atoi(c.Version); err == nil && n >= v.rejected[r.ObjectKey] {
						v.rejected[r.ObjectKey] = n
					}
				}
			}
		}
	}
	return v, nil
}

// billOf is a bill as the API shows it, from its document. withChecks fills its checks and ledger impact (getBill).
func (a *API) billOf(ctx context.Context, u storage.UnitOfWork, v billView, doc storage.Document, withChecks bool) (Bill, error) {
	d, err := bill.Read(doc, v.base)
	if err != nil {
		return Bill{}, err
	}
	out := Bill{ID: doc.ID.String(), Number: doc.Number, Status: string(doc.Status), Version: doc.Version, SupplierRef: d.SupplierRef,
		Date: dayText(d.Date), DueDate: dayText(d.DueDate), Currency: d.Currency.Code, Notes: d.Notes, Lines: []BillLineDraft{}, Checks: []BillCheck{},
		LedgerImpact: []EntryLine{}, PreparedBy: personOf(doc.SavedBy), PostedBy: personOf(doc.PostedBy), PostedAt: utc(doc.PostedAt)}
	if d.Rate != nil {
		out.FXRate = d.Rate.String()
	}
	if d.SupplierID != uuid.Nil {
		out.Supplier = &BillSupplier{ID: d.SupplierID.String()}
		if c, err := u.Contacts().Get(ctx, d.SupplierID); err == nil {
			out.Supplier.Name = c.LegalName
		}
	}
	for _, l := range d.Lines {
		bl := BillLineDraft{AccountID: l.AccountID.String(), Description: l.Description, GSTCode: l.GSTCode,
			Net: Money{Amount: l.Net.String(), Currency: l.Net.Currency().Code}, GST: Money{Amount: l.GST.String(), Currency: l.GST.Currency().Code}}
		if l.StatedGST != nil {
			bl.StatedGST = &Money{Amount: l.StatedGST.String(), Currency: l.StatedGST.Currency().Code}
		}
		out.Lines = append(out.Lines, bl)
	}
	total, err := d.Total()
	if err != nil {
		return Bill{}, err
	}
	out.Total = Money{Amount: total.String(), Currency: total.Currency().Code}

	out.States = BillStates{Approval: "draft", Ledger: "not_posted", Payment: "unpaid", Due: "not_due"}
	switch doc.Status {
	case storage.DocumentPosted:
		out.States.Approval, out.States.Ledger = "approved", "posted"
	case storage.DocumentReversed:
		out.States.Approval, out.States.Ledger = "approved", "voided"
	default:
		if out.ApprovalAsked = v.asked[out.ID]; out.ApprovalAsked != "" {
			out.States.Approval = "awaiting_approval"
		} else if n, ok := v.rejected[out.ID]; ok && n == doc.Version {
			out.States.Approval = "rejected"
		}
	}
	if doc.Status == storage.DocumentDraft {
		c, err := journal.CheckBill(ctx, u, doc)
		if err != nil {
			return Bill{}, err
		}
		if out.DueDate == "" {
			out.DueDate = dayText(c.Prepared.DueDate)
		}
		if withChecks {
			for _, p := range c.Problems {
				out.Checks = append(out.Checks, BillCheck{Severity: "error", Code: p.Code, Field: journalField(p.Field), Message: p.Message, Fix: p.Fix})
			}
			for _, w := range c.Prepared.Warnings {
				out.Checks = append(out.Checks, BillCheck{Severity: "warning", Code: w.Code, Field: journalField(w.Field), Message: w.Message, Fix: w.Fix})
			}
		}
		if len(c.Problems) == 0 {
			out.Payable = &Money{Amount: c.Payable.String(), Currency: c.Payable.Currency().Code}
			if withChecks {
				out.LedgerImpact = impactOf(c.Entry)
			}
		}
		return out, a.name(ctx, u, out.PreparedBy, out.PostedBy)
	}
	e, err := u.Ledger().ByIdentity(ctx, ledger.BillIdentity(doc.ID, doc.Version, ledger.RuleBill))
	if err != nil {
		return Bill{}, err
	}
	for _, l := range entryOf(e).Lines {
		if withChecks {
			out.LedgerImpact = append(out.LedgerImpact, l)
		}
		if l.AccountID == v.payables.String() {
			p := Money{Amount: strings.TrimPrefix(l.BaseAmount.Amount, "-"), Currency: l.BaseAmount.Currency}
			out.Payable = &p
		}
	}
	if out.DueDate == "" {
		if c, err := journal.CheckBill(ctx, u, doc); err == nil {
			out.DueDate = dayText(c.Prepared.DueDate)
		}
	}
	if doc.Status == storage.DocumentPosted && out.DueDate != "" && out.DueDate < dateOf(v.today) {
		out.States.Due = "overdue"
	}
	return out, a.name(ctx, u, out.PreparedBy, out.PostedBy)
}

// impactOf is an unwritten entry's lines as the API shows them.
func impactOf(e ledger.Entry) []EntryLine {
	out := make([]EntryLine, 0, len(e.Lines))
	for i, l := range e.Lines {
		out = append(out, EntryLine{LineNo: i + 1, AccountID: l.AccountID.String(), Amount: Money{Amount: l.Amount.String(), Currency: l.Amount.Currency().Code},
			FXRate: l.Rate.String(), FXRateSource: l.Rate.Source, BaseAmount: Money{Amount: l.Base.String(), Currency: l.Base.Currency().Code},
			Description: l.Description})
	}
	return out
}

func dayText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return dateOf(t)
}

// billProblems turns the posting rules' problems into the API's, naming the bill.
func billProblems(err error) error {
	var lp ledger.Problems
	if !errors.As(err, &lp) || len(lp) == 0 {
		return err
	}
	fields := make([]FieldProblem, 0, len(lp))
	for _, x := range lp {
		fields = append(fields, FieldProblem{Code: x.Code, Field: journalField(x.Field), Message: x.Message, Fix: x.Fix})
	}
	return problem(lp[0].Code, fmt.Sprintf("The bill cannot post: %d problem(s), each listed in errors.", len(lp)),
		"Correct the bill and post it again.", fields...)
}

// gateBillPost refuses a post above the threshold (FM-2), and says whether a post at or below it is a Founder-mode
// self-approval to log.
func (a *API) gateBillPost(ctx context.Context, u storage.UnitOfWork, p authn.Principal, id uuid.UUID) (selfApproved bool, total money.Money, err error) {
	c, err := a.bills.Check(ctx, p.Organisation, id, nil)
	if err != nil {
		return false, total, err
	}
	if len(c.Problems) > 0 {
		return false, total, billProblems(c.Problems)
	}
	total = c.Payable
	threshold, err := selfApprovalThreshold(ctx, a, u, total.Currency())
	if err != nil {
		return false, total, err
	}
	if over, err := total.Cmp(threshold); err != nil {
		return false, total, err
	} else if over > 0 {
		return false, total, problem(CodeBillNeedsApproval,
			fmt.Sprintf("The bill's total is %s %s, above the self-approval threshold of %s.", total, total.Currency().Code, threshold),
			"Ask for another person's approval to post it")
	}
	mode, _, _, err := teamModeOf(ctx, u)
	return mode == team.Founder, total, err
}

// logBillSelfApproval records a Founder-mode post at or below the threshold for the weekly review (FM-2, FM-3).
func (a *API) logBillSelfApproval(ctx context.Context, u storage.UnitOfWork, p authn.Principal, id uuid.UUID, total money.Money) error {
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return err
	}
	var actor *uuid.UUID
	if p.UserID != uuid.Nil {
		actor = &p.UserID
	}
	_, err = u.ReviewLog().Log(ctx, storage.NewReviewLogEntry{Kind: storage.ReviewSelfApproved, ObjectType: approval.ObjectBill, ObjectKey: id.String(),
		Summary: fmt.Sprintf("Posted bill %s of %s %s alone, at or below the self-approval threshold", cmpText(doc.Number, "(a draft)"), total, total.Currency().Code),
		Actor:   actor, LoggedAt: a.deps.Clock.Now()})
	return err
}

func cmpText(s, otherwise string) string {
	if s == "" {
		return otherwise
	}
	return s
}

// gateBillVoid refuses the void of a posted bill in Founder mode: it needs a second person (FM-1).
func gateBillVoid(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) error {
	doc, err := u.Documents().Get(ctx, id)
	if err != nil || doc.Status != storage.DocumentPosted {
		return err
	}
	mode, _, _, err := teamModeOf(ctx, u)
	if err != nil {
		return err
	}
	if mode == team.Founder {
		return problem(CodeBillVoidApproval, "In Founder mode, voiding a posted bill needs a second person (FM-1).", "Ask for another person's approval to void it")
	}
	return nil
}

// gateBillEdit refuses an edit in Founder mode (FM-1), and in any mode an edit whose new version adds more than the
// self-approval threshold to accounts payable (FM-2). A bill that is not posted is left to the edit's own refusal; an
// edit the rules refuse answers their problems.
func (a *API) gateBillEdit(ctx context.Context, u storage.UnitOfWork, p authn.Principal, doc storage.Document, d bill.Draft) error {
	if doc.Status != storage.DocumentPosted {
		return nil
	}
	mode, _, _, err := teamModeOf(ctx, u)
	if err != nil {
		return err
	}
	if mode == team.Founder {
		return problem(CodeBillEditApproval, "In Founder mode, editing a posted bill needs a second person (FM-1).", "Ask for another person's approval to edit it")
	}
	c, err := a.bills.Check(ctx, p.Organisation, doc.ID, &d)
	if err != nil {
		return err
	}
	if len(c.Problems) > 0 {
		return billProblems(c.Problems)
	}
	threshold, err := selfApprovalThreshold(ctx, a, u, c.Payable.Currency())
	if err != nil {
		return err
	}
	if over, err := c.Payable.Cmp(threshold); err != nil {
		return err
	} else if over > 0 {
		return problem(CodeBillNeedsApproval,
			fmt.Sprintf("The edited bill's total is %s %s, above the self-approval threshold of %s.", c.Payable, c.Payable.Currency().Code, threshold),
			"Ask for another person's approval to edit it")
	}
	return nil
}

// billEditDiff is what an edit changes, field by field and line by line, for the approver (FR-APR-02).
func billEditDiff(ctx context.Context, u storage.UnitOfWork, before Bill, after BillDraft, payableBefore, payableAfter string) ([]ApprovalDiff, error) {
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, ac := range accounts {
		names[ac.ID.String()] = ac.Code + " " + ac.Name
	}
	line := func(l BillLineDraft) string {
		s := fmt.Sprintf("%s: %s %s + %s GST (%s)", names[l.AccountID], l.Net.Amount, l.Net.Currency, l.GST.Amount, l.GSTCode)
		if l.StatedGST != nil {
			s += fmt.Sprintf(", %s %s stated", l.StatedGST.Amount, l.StatedGST.Currency)
		}
		if l.Description != "" {
			s += " (" + l.Description + ")"
		}
		return s
	}
	var out []ApprovalDiff
	for _, f := range []struct{ field, before, after string }{{"supplier_ref", before.SupplierRef, after.SupplierRef}, {"date", before.Date, after.Date},
		{"currency", before.Currency, after.Currency}, {"fx_rate", before.FXRate, after.FXRate}} {
		if f.before != f.after {
			out = append(out, ApprovalDiff{Field: f.field, Before: f.before, After: f.after})
		}
	}
	if before.Supplier != nil && before.Supplier.ID != after.SupplierID {
		out = append(out, ApprovalDiff{Field: "supplier_id", Before: before.Supplier.ID, After: after.SupplierID})
	}
	out = append(out, ApprovalDiff{Field: "payable", Before: payableBefore, After: payableAfter})
	for i := 0; i < max(len(before.Lines), len(after.Lines)); i++ {
		var b, f string
		if i < len(before.Lines) {
			b = line(before.Lines[i])
		}
		if i < len(after.Lines) {
			f = line(after.Lines[i])
		}
		if b != f {
			out = append(out, ApprovalDiff{Field: fmt.Sprintf("line %d", i+1), Before: b, After: f})
		}
	}
	return out, nil
}

// executeBillEdit runs an approved edit: the content the request kept, checked against the hash its envelope holds,
// posted as the approver, with the requester as the one who prepared the new version.
func (a *API) executeBillEdit(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, s stored) error {
	text := func(k string) string { v, _ := s.Execute[k].(string); return v }
	content, sum, err := canonicalOf(s.Content)
	if err != nil {
		return err
	}
	if sum != text("content_sha256") {
		return fmt.Errorf("approval: the content of request %s does not match its envelope", r.ID)
	}
	var body BillDraft
	if err := json.Unmarshal(content, &body); err != nil {
		return err
	}
	id, err := uuid.Parse(r.ObjectKey)
	if err != nil {
		return err
	}
	base, err := baseOf(ctx, u)
	if err != nil {
		return err
	}
	d, err := billDraftOf(body, base)
	if err != nil {
		return err
	}
	o, err := u.Organisation(ctx)
	if err != nil {
		return err
	}
	_, err = a.bills.Edit(ctx, o.ID, id, d, text("reason"), text("prepared_by"))
	return err
}

// contentOfBill is an edit's content as the request keeps it, canonical, and its SHA-256, which the envelope holds.
func contentOfBill(d BillDraft) ([]byte, string, error) {
	raw, err := json.Marshal(d)
	if err != nil {
		return nil, "", err
	}
	return canonicalOf(raw)
}

// billPayable is a bill's total in the base currency, as its draft would post or as it posted.
func (a *API) billPayable(ctx context.Context, u storage.UnitOfWork, v billView, doc storage.Document) (string, error) {
	b, err := a.billOf(ctx, u, v, doc, false)
	if err != nil {
		return "", err
	}
	if b.Payable == nil {
		return "", billProblems(errors.New("no payable"))
	}
	return b.Payable.Amount, nil
}

// billCommand runs a command that posts for a bill: If-Match is checked against the bill locked in the same unit of
// work, and a person whose roles administer users and post does not post in a mode that blocks conflicts (SOD-6).
func (a *API) billCommand(ctx context.Context, rawID, key, ifMatch, prefer string, request any,
	command func(ctx context.Context, u storage.UnitOfWork, p authn.Principal, id uuid.UUID) (storage.Entry, error)) (*billPostingOutput, error) {
	id, err := parseID("bill_id", rawID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(ifMatch) == "" {
		return nil, checkIfMatch(ifMatch, "")
	}
	p, _ := authn.From(ctx)
	w, err := write(ctx, a, key, prefer, request, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (BillPosting, error) {
		doc, err := u.Documents().Lock(ctx, id)
		if err != nil {
			return BillPosting{}, err
		}
		v, err := a.billView(ctx, u)
		if err != nil {
			return BillPosting{}, err
		}
		before, err := a.billOf(ctx, u, v, doc, false)
		if err != nil {
			return BillPosting{}, err
		}
		if err := checkIfMatch(ifMatch, billETag(before)); err != nil {
			return BillPosting{}, err
		}
		if err := checkConflicts(ctx, u, p, "Posting"); err != nil {
			return BillPosting{}, err
		}
		entry, err := command(ctx, u, p, id)
		if err != nil {
			return BillPosting{}, billProblems(err)
		}
		after, err := u.Documents().Get(ctx, id)
		if err != nil {
			return BillPosting{}, err
		}
		if v, err = a.billView(ctx, u); err != nil {
			return BillPosting{}, err
		}
		b, err := a.billOf(ctx, u, v, after, false)
		return BillPosting{Bill: b, Entry: entryOf(entry)}, err
	})
	if err != nil {
		return nil, err
	}
	if w.Previewed {
		return &billPostingOutput{Applied: applied(w), Body: w.Body}, nil
	}
	return &billPostingOutput{ETag: billETag(w.Body.Bill), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
}

// billRequest asks for approval of a change to a bill at the version read.
func (a *API) billRequest(ctx context.Context, rawID, key, ifMatch, prefer string, request any, status storage.DocumentStatus,
	change func(doc storage.Document, b Bill, payable string) pendingChange) (*billRequestOutput, error) {
	id, err := parseID("bill_id", rawID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(ifMatch) == "" {
		return nil, checkIfMatch(ifMatch, "")
	}
	w, err := write(ctx, a, key, prefer, request, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
		doc, err := u.Documents().Lock(ctx, id)
		if err != nil {
			return Approval{}, err
		}
		v, err := a.billView(ctx, u)
		if err != nil {
			return Approval{}, err
		}
		b, err := a.billOf(ctx, u, v, doc, false)
		if err != nil {
			return Approval{}, err
		}
		if err := checkIfMatch(ifMatch, billETag(b)); err != nil {
			return Approval{}, err
		}
		if doc.Status != status {
			code := bill.CodeNotDraft
			if status == storage.DocumentPosted {
				code = bill.CodeNotPosted
			}
			return Approval{}, problem(code, fmt.Sprintf("The bill is %s.", doc.Status), "Ask for approval of a bill in the state it needs")
		}
		payable, err := a.billPayable(ctx, u, v, doc)
		if err != nil {
			return Approval{}, err
		}
		return a.requestApproval(ctx, u, change(doc, b, payable))
	})
	if err != nil {
		return nil, err
	}
	return &billRequestOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
}

func billName(doc storage.Document, b Bill) string {
	name := cmpText(doc.Number, "the draft bill")
	if b.Supplier != nil && b.Supplier.Name != "" {
		name += " from " + b.Supplier.Name
	}
	if b.SupplierRef != "" {
		name += " (" + b.SupplierRef + ")"
	}
	return name
}

func registerBills(a *API) {
	formCodes := []string{CodeMalformed, CodeTooLarge, CodeMediaType, money.CodeNotDecimal, money.CodePrecision, money.CodeRange, money.CodeCurrency,
		money.CodeRate, bill.CodeTooLong, storage.CodeRetry, storage.CodeLockTimeout}
	readCodes := []string{CodeNotFound, bill.CodeNotBill, bill.CodeBadDraft}

	register(a, authz.ListBills, huma.Operation{
		OperationID: "listBills", Method: http.MethodGet, Path: "/v1/bills", Tags: []string{"Purchases"},
		Summary: "List the bills",
		Description: "The organisation's bills at their current version, newest date first (a draft without a date last), a page at a " +
			"time, narrowed by status, dates and text in the number (FR-AP-02). Each has its supplier, total, due date and states. " +
			"Read-only; no side effects.",
	}, append([]string{CodeCursor, CodeInvalid}, readCodes...), func(ctx context.Context, in *billListInput) (*billListOutput, error) {
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		afterDate, afterID, err := journalCursor(after)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := billListOutput{Body: BillPage{Items: []Bill{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentBill, Status: storage.DocumentStatus(in.Status),
				From: in.From, To: in.To, Search: strings.TrimSpace(in.Q), AfterDate: afterDate, AfterID: afterID, Limit: in.PageSize + 1})
			if err != nil {
				return err
			}
			if len(docs) > in.PageSize {
				docs = docs[:in.PageSize]
				last := docs[len(docs)-1]
				out.Body.NextCursor = encodeCursor(last.Date + "|" + last.ID.String())
			}
			v, err := a.billView(ctx, u)
			if err != nil {
				return err
			}
			for _, d := range docs {
				b, err := a.billOf(ctx, u, v, d, false)
				if err != nil {
					return err
				}
				out.Body.Items = append(out.Body.Items, b)
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.CreateBill, huma.Operation{
		OperationID: "createBill", Method: http.MethodPost, Path: "/v1/bills", Tags: []string{"Purchases"}, DefaultStatus: http.StatusCreated,
		Summary: "Save a draft bill",
		Description: "Saves a supplier's bill as a draft (✎) at version 1 (FR-AP-02). A draft is checked for its form only; getBill shows " +
			"what posting it would do, and postBill checks it all. Side effects: a draft document and its audit event; nothing posts " +
			"until postBill. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld}, formCodes...),
		func(ctx context.Context, in *createBillInput) (*billOutput, error) {
			p, _ := authn.From(ctx)
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (Bill, error) {
				org, err := u.Organisation(ctx)
				if err != nil {
					return Bill{}, err
				}
				base, err := money.Lookup(org.BaseCurrency)
				if err != nil {
					return Bill{}, err
				}
				d, err := billDraftOf(in.Body, base)
				if err != nil {
					return Bill{}, err
				}
				doc, err := a.bills.Create(ctx, p.Organisation, d)
				if err != nil {
					return Bill{}, err
				}
				v, err := a.billView(ctx, u)
				if err != nil {
					return Bill{}, err
				}
				return a.billOf(ctx, u, v, doc, false)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &billOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &billOutput{Status: w.Status, ETag: billETag(w.Body), Location: "/v1/bills/" + w.Body.ID, Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	register(a, authz.GetBill, huma.Operation{
		OperationID: "getBill", Method: http.MethodGet, Path: "/v1/bills/{bill_id}", Tags: []string{"Purchases"},
		Summary: "Get a bill",
		Description: "Returns a bill's current version with its ETag: its supplier, lines and total, its approval, ledger, payment and due " +
			"states, what stops a draft posting (✖) and the warnings it posts with (⚠: the stated GST against RND-GST-EXCL, a " +
			"duplicate, a foreign line with no SGD GST stated), the lines posting it writes or wrote (ledger_impact), and the pending " +
			"request to post or void it. Read-only; no side effects.",
	}, readCodes, func(ctx context.Context, in *BillPath) (*billReadOutput, error) {
		id, err := parseID("bill_id", in.BillID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var b Bill
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Get(ctx, id)
			if err != nil {
				return err
			}
			v, err := a.billView(ctx, u)
			if err != nil {
				return err
			}
			b, err = a.billOf(ctx, u, v, doc, true)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &billReadOutput{ETag: billETag(b), Body: b}, nil
	})

	register(a, authz.ReplaceBill, huma.Operation{
		OperationID: "replaceBill", Method: http.MethodPut, Path: "/v1/bills/{bill_id}", Tags: []string{"Purchases"},
		Summary: "Change a draft bill",
		Description: "Saves the draft's new content as its next version, recording who saved it; the earlier versions are kept. Only a " +
			"draft changes this way. Side effects: the new version and its audit event. Needs If-Match with the draft's ETag. " +
			"Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, bill.CodeNotBill, bill.CodeNotDraft, bill.CodeBadDraft}, formCodes...),
		func(ctx context.Context, in *replaceBillInput) (*billOutput, error) {
			id, err := parseID("bill_id", in.BillID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			p, _ := authn.From(ctx)
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (Bill, error) {
				doc, err := u.Documents().Lock(ctx, id)
				if err != nil {
					return Bill{}, err
				}
				v, err := a.billView(ctx, u)
				if err != nil {
					return Bill{}, err
				}
				before, err := a.billOf(ctx, u, v, doc, false)
				if err != nil {
					return Bill{}, err
				}
				if err := checkIfMatch(in.IfMatch, billETag(before)); err != nil {
					return Bill{}, err
				}
				org, err := u.Organisation(ctx)
				if err != nil {
					return Bill{}, err
				}
				base, err := money.Lookup(org.BaseCurrency)
				if err != nil {
					return Bill{}, err
				}
				d, err := billDraftOf(in.Body, base)
				if err != nil {
					return Bill{}, err
				}
				after, err := a.bills.Revise(ctx, p.Organisation, id, d)
				if err != nil {
					return Bill{}, err
				}
				return a.billOf(ctx, u, v, after, false)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &billOutput{Status: http.StatusOK, Applied: applied(w), Body: w.Body}, nil
			}
			return &billOutput{Status: http.StatusOK, ETag: billETag(w.Body), Body: w.Body}, nil
		})

	postCodes := append(append([]string{bill.CodeNotDraft, bill.CodeNotPosted, CodeBillNeedsApproval, CodeBillVoidApproval, CodeMalformed, CodeTooLarge,
		CodeMediaType}, postingCodes()...), append(BillCodes, readCodes...)...)

	register(a, authz.PostBill, huma.Operation{
		OperationID: "postBill", Method: http.MethodPost, Path: "/v1/bills/{bill_id}/post", Tags: []string{"Purchases"},
		Summary: "Post a draft bill",
		Description: "Posts a draft bill under the rule bill@1 (FR-AP-02): Dr each line's account with its net (with its GST when the input " +
			"tax is not claimable), Dr 2200 with the claimable GST as stated, Cr 2010 with the total, and for a bill in another " +
			"currency the difference to 7030, written with the bill's change to posted and its number in one transaction (ENG-02). " +
			"Every problem comes back together. Approval as for journals: a bill whose total in the base currency is above the " +
			"self-approval threshold is refused (BILL-09): ask with requestBillPost; at or below it, a Founder-mode post is logged as " +
			"self-approved for the weekly review (FM-2, FM-3). Side effects: the entry, a new ledger revision, balances, audit events. " +
			"Needs If-Match with the draft's ETag. Idempotent with Idempotency-Key; Prefer: return=preview posts nothing.",
	}, postCodes, func(ctx context.Context, in *billCommandInput) (*billPostingOutput, error) {
		return a.billCommand(ctx, in.BillID, in.IdempotencyKey, in.IfMatch, in.Prefer, in, func(ctx context.Context, u storage.UnitOfWork, p authn.Principal, id uuid.UUID) (storage.Entry, error) {
			self, total, err := a.gateBillPost(ctx, u, p, id)
			if err != nil {
				return storage.Entry{}, err
			}
			e, err := a.bills.Post(ctx, p.Organisation, id)
			if err == nil && self {
				err = a.logBillSelfApproval(ctx, u, p, id, total)
			}
			return e, err
		})
	})

	register(a, authz.VoidBill, huma.Operation{
		OperationID: "voidBill", Method: http.MethodPost, Path: "/v1/bills/{bill_id}/void", Tags: []string{"Purchases"},
		Summary: "Void a posted bill",
		Description: "Voids a posted bill with a reversing entry on the date given, with a reason (FR-GL-12); posted history never " +
			"changes. In Founder mode voiding needs a second person (FM-1): it is refused (BILL-10), and requestBillVoid asks for the " +
			"approval. Side effects: the reversing entry, a new ledger revision, balances, audit events with the reason. Needs If-Match " +
			"with the bill's ETag. Idempotent with Idempotency-Key.",
	}, postCodes, func(ctx context.Context, in *billVoidInput) (*billPostingOutput, error) {
		date, err := time.Parse(time.DateOnly, in.Body.Date)
		if err != nil {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.date", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"}})
		}
		return a.billCommand(ctx, in.BillID, in.IdempotencyKey, in.IfMatch, in.Prefer, in, func(ctx context.Context, u storage.UnitOfWork, p authn.Principal, id uuid.UUID) (storage.Entry, error) {
			if err := gateBillVoid(ctx, u, id); err != nil {
				return storage.Entry{}, err
			}
			return a.bills.Void(ctx, p.Organisation, id, date, in.Body.Reason)
		})
	})

	requestCodes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		CodeSessionRequired, CodeStepUpRequired, approval.CodePending, storage.CodeApproval, bill.CodeNotBill, bill.CodeNotDraft, bill.CodeNotPosted,
		bill.CodeBadDraft, storage.CodeRetry, storage.CodeLockTimeout, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}, append(BillCodes, LedgerCodes...)...)

	register(a, authz.RequestBillPost, huma.Operation{
		OperationID: "requestBillPost", Method: http.MethodPost, Path: "/v1/bills/{bill_id}/post-approval", Tags: []string{"Purchases"},
		Summary: "Ask for approval to post a bill", DefaultStatus: http.StatusAccepted,
		Description: "Asks for another person's approval to post a draft bill at the version read (FM-2, SOD-3): what postBill asks for " +
			"when the bill's total is above the self-approval threshold (BILL-09). The answer is 202 with an approval request, which a " +
			"person who holds A on \"Bills, vendor credits\", did not ask and did not prepare the bill approves, stepped up; on the " +
			"approval the bill posts, as the approver. The request holds the bill's version, status and total, so a change to the " +
			"draft voids it. Asking needs a person signed in who passed step-up, and If-Match with the bill's ETag. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview checks it and asks nothing.",
	}, requestCodes, func(ctx context.Context, in *billCommandInput) (*billRequestOutput, error) {
		return a.billRequest(ctx, in.BillID, in.IdempotencyKey, in.IfMatch, in.Prefer, in, storage.DocumentDraft,
			func(doc storage.Document, b Bill, payable string) pendingChange {
				return pendingChange{ObjectType: approval.ObjectBill, ObjectKey: doc.ID.String(), Operation: approval.OpBillPost,
					Summary: fmt.Sprintf("Post %s, total %s %s", billName(doc, b), payable, cmpText(b.Total.Currency, "")),
					Execute: map[string]any{"bill_id": doc.ID.String(), "version": fmt.Sprint(doc.Version)},
					Diff:    []ApprovalDiff{{Field: "status", Before: "draft", After: "posted"}, {Field: "payable", After: payable}}}
			})
	})

	editCodes := append([]string{CodeBillEditApproval, storage.CodeDocumentEdit, CodeInvalid}, postCodes...)

	register(a, authz.EditBill, huma.Operation{
		OperationID: "editBill", Method: http.MethodPost, Path: "/v1/bills/{bill_id}/edit", Tags: []string{"Purchases"},
		Summary: "Edit a posted bill",
		Description: "Corrects a posted bill in an open period (FR-GL-12): saves the content sent as its next version, reverses the " +
			"entry it had on that entry's own date, and posts the new version's entry, in one transaction; the bill stays posted, " +
			"keeps its number, and records who posted the edit. An edit needs the approval a void needs: in Founder mode it is " +
			"refused (BILL-12), and in any mode a new version whose total is above the self-approval threshold is refused " +
			"(BILL-09); requestBillEdit asks for the approval. Side effects: a new version, two entries, a new ledger revision, " +
			"balances, audit events with the reason. Needs If-Match with the bill's ETag. Idempotent with Idempotency-Key; " +
			"Prefer: return=preview shows the two entries and saves nothing.",
	}, editCodes, func(ctx context.Context, in *billEditInput) (*billEditOutput, error) {
		id, err := parseID("bill_id", in.BillID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		reason := strings.TrimSpace(in.Body.Reason)
		if reason == "" {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.reason", Message: "is empty", Fix: "Say why the bill is edited"}})
		}
		p, _ := authn.From(ctx)
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (BillEdited, error) {
			doc, err := u.Documents().Lock(ctx, id)
			if err != nil {
				return BillEdited{}, err
			}
			v, err := a.billView(ctx, u)
			if err != nil {
				return BillEdited{}, err
			}
			before, err := a.billOf(ctx, u, v, doc, false)
			if err != nil {
				return BillEdited{}, err
			}
			if err := checkIfMatch(in.IfMatch, billETag(before)); err != nil {
				return BillEdited{}, err
			}
			if err := checkConflicts(ctx, u, p, "Posting"); err != nil {
				return BillEdited{}, err
			}
			d, err := billDraftOf(in.Body.BillDraft, v.base)
			if err != nil {
				return BillEdited{}, err
			}
			if err := a.gateBillEdit(ctx, u, p, doc, d); err != nil {
				return BillEdited{}, err
			}
			res, err := a.bills.Edit(ctx, p.Organisation, id, d, reason, "")
			if err != nil {
				return BillEdited{}, billProblems(err)
			}
			after, err := u.Documents().Get(ctx, id)
			if err != nil {
				return BillEdited{}, err
			}
			b, err := a.billOf(ctx, u, v, after, false)
			return BillEdited{Bill: b, Reversal: entryOf(res.Reversal), Entry: entryOf(res.Entry)}, err
		})
		if err != nil {
			return nil, err
		}
		if w.Previewed {
			return &billEditOutput{Applied: applied(w), Body: w.Body}, nil
		}
		return &billEditOutput{ETag: billETag(w.Body.Bill), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	register(a, authz.RequestBillEdit, huma.Operation{
		OperationID: "requestBillEdit", Method: http.MethodPost, Path: "/v1/bills/{bill_id}/edit-approval", Tags: []string{"Purchases"},
		Summary: "Ask for approval to edit a posted bill", DefaultStatus: http.StatusAccepted,
		Description: "Asks for another person's approval to edit a posted bill with the content sent (FR-GL-12, FM-1, FM-2): what " +
			"editBill asks for in Founder mode (BILL-12) or above the self-approval threshold (BILL-09). The content must pass the " +
			"posting rules. The answer is 202 with an approval request whose changes list each field and line that changes and the " +
			"total payable before and after; it keeps the content, and its envelope holds the content's SHA-256 with the bill's " +
			"version, status and total, so any change to the bill voids it. A person who holds A on \"Bills, vendor credits\" and did " +
			"not ask approves it, stepped up; on the approval the edit posts as the approver, with the requester as the one who " +
			"prepared the new version. Asking needs a person signed in who passed step-up, and If-Match with the bill's ETag. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.",
	}, requestCodes, func(ctx context.Context, in *billEditInput) (*billRequestOutput, error) {
		id, err := parseID("bill_id", in.BillID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		reason := strings.TrimSpace(in.Body.Reason)
		if reason == "" {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.reason", Message: "is empty", Fix: "Say why the bill is edited"}})
		}
		p, _ := authn.From(ctx)
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
			doc, err := u.Documents().Lock(ctx, id)
			if err != nil {
				return Approval{}, err
			}
			v, err := a.billView(ctx, u)
			if err != nil {
				return Approval{}, err
			}
			b, err := a.billOf(ctx, u, v, doc, false)
			if err != nil {
				return Approval{}, err
			}
			if err := checkIfMatch(in.IfMatch, billETag(b)); err != nil {
				return Approval{}, err
			}
			if doc.Status != storage.DocumentPosted {
				return Approval{}, problem(bill.CodeNotPosted, fmt.Sprintf("The bill is %s.", doc.Status), "Ask for approval of a bill in the state it needs")
			}
			d, err := billDraftOf(in.Body.BillDraft, v.base)
			if err != nil {
				return Approval{}, err
			}
			c, err := a.bills.Check(ctx, p.Organisation, doc.ID, &d)
			if err != nil {
				return Approval{}, err
			}
			if len(c.Problems) > 0 {
				return Approval{}, billProblems(c.Problems)
			}
			payable, err := a.billPayable(ctx, u, v, doc)
			if err != nil {
				return Approval{}, err
			}
			content, sum, err := contentOfBill(in.Body.BillDraft)
			if err != nil {
				return Approval{}, err
			}
			diff, err := billEditDiff(ctx, u, b, in.Body.BillDraft, payable, c.Payable.String())
			if err != nil {
				return Approval{}, err
			}
			return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectBill, ObjectKey: doc.ID.String(), Operation: approval.OpBillEdit,
				Summary: fmt.Sprintf("Edit %s: %s", billName(doc, b), reason),
				Execute: map[string]any{"bill_id": doc.ID.String(), "version": fmt.Sprint(doc.Version), "content_sha256": sum,
					"reason": reason, "prepared_by": (storage.Actor{Type: "user", ID: p.UserID.String()}).String()},
				Content: content, Diff: diff})
		})
		if err != nil {
			return nil, err
		}
		return &billRequestOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.RequestBillVoid, huma.Operation{
		OperationID: "requestBillVoid", Method: http.MethodPost, Path: "/v1/bills/{bill_id}/void-approval", Tags: []string{"Purchases"},
		Summary: "Ask for approval to void a bill", DefaultStatus: http.StatusAccepted,
		Description: "Asks for another person's approval to void a posted bill with a reversing entry on the date given, with a reason " +
			"(FR-GL-12, FM-1): what voidBill asks for in Founder mode (BILL-10). The answer is 202 with an approval request, which a " +
			"person who holds A on \"Bills, vendor credits\" and did not ask approves, stepped up; on the approval the reversal posts. " +
			"Asking needs a person signed in who passed step-up, and If-Match with the bill's ETag. Idempotent with Idempotency-Key; " +
			"Prefer: return=preview checks it and asks nothing.",
	}, requestCodes, func(ctx context.Context, in *billVoidInput) (*billRequestOutput, error) {
		if _, err := time.Parse(time.DateOnly, in.Body.Date); err != nil {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.date", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"}})
		}
		reason := strings.TrimSpace(in.Body.Reason)
		return a.billRequest(ctx, in.BillID, in.IdempotencyKey, in.IfMatch, in.Prefer, in, storage.DocumentPosted,
			func(doc storage.Document, b Bill, payable string) pendingChange {
				return pendingChange{ObjectType: approval.ObjectBill, ObjectKey: doc.ID.String(), Operation: approval.OpBillVoid,
					Summary: fmt.Sprintf("Void %s on %s: %s", billName(doc, b), in.Body.Date, reason),
					Execute: map[string]any{"bill_id": doc.ID.String(), "version": fmt.Sprint(doc.Version), "date": in.Body.Date, "reason": reason},
					Diff:    []ApprovalDiff{{Field: "status", Before: "posted", After: "reversed"}, {Field: "void_date", After: in.Body.Date}}}
			})
	})
}
