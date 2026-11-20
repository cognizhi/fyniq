package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/payee"
	"github.com/cognizhi/fyniq/internal/domain/paymentfile"
	"github.com/cognizhi/fyniq/internal/domain/peoplelink"
	"github.com/cognizhi/fyniq/internal/domain/salarybatch"
	"github.com/cognizhi/fyniq/internal/domain/team"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ salary payment batches (FR-PAY-11, FR-PPL-10, 12, 13, 14, 17; p1-payroll-link task 4.2, 6.1, 6.3)
//
// A salary batch is made when Nemura sends the payment lines of a run Fyniq posted (receivePayrollPayments). A Payroll administrator prepares it (money account, payment date, method) and asks for
// its approval; another person who holds A on "Pay runs, employee data" and neither asked for it nor prepared or approved the run in Nemura approves it (FM-1, SOD-4); the Payment releaser, a
// person stepped up and not the run's preparer or approver in any team mode, releases it. A release writes the payment file (a generic format Fyniq writes: the payment list) for the lines that are
// free to go now and leaves the rest waiting: a person held for tax clearance (PPL-21, PPL-22), a person whose bank details Nemura has since changed (SB-07) and a person with an unknown outcome in the
// run (SB-08) wait while the others are paid, each in a later release once the reason is gone (FR-PPL-13, FR-PPL-14). No break-glass reaches a salary: there is no way to release one without the
// approval. Lines, names, accounts and amounts are shown only to the roles that may create or approve on the pay-run row; every other role that may see the batch sees its total, count and state
// (FR-PPL-17).

// SalaryBatchLine is one person's payment as the roles with the salary-payments right see it.
type SalaryBatchLine struct {
	LineID            string `json:"line_id" format:"uuid" doc:"Nemura's ID of the payment line." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c11"`
	PersonID          string `json:"person_id" format:"uuid" doc:"The person of the employee projection." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c12"`
	PersonName        string `json:"person_name,omitempty" doc:"The person's display name in the projection." example:"Wei Jie Lim"`
	Amount            Money  `json:"amount" doc:"What the line pays, in SGD."`
	Account           string `json:"account,omitempty" doc:"The payee's bank and account, masked to its last four digits." example:"DBSSSGSG ••••4321"`
	BankDetailVersion int    `json:"bank_detail_version" doc:"The version of the bank-detail record the line was made from (FR-PPL-14)." example:"3"`
	LineVersion       int    `json:"line_version" doc:"The line's version: a revised line is a new version, and the batch is approved again." example:"1"`
	State             string `json:"state" enum:"scheduled,released,sent,accepted,paid,rejected,returned,unknown" doc:"Where the payment is (FR-PPL-12): paid only from the bank feed matching the debit or a person's record with evidence." example:"scheduled"`
	InstructionID     string `json:"instruction_id,omitempty" doc:"The instruction ID the release gave it." example:"SB0003-R1-L07"`
	Waiting           string `json:"waiting,omitempty" enum:"held,hold_unknown,bank_details,unknown_outcome" doc:"Why the line is not released now: held for tax clearance, Nemura did not say whether it is held, the person's bank details changed, or an unknown outcome blocks a new instruction." example:"held"`
}

// SalaryBatchRelease is one release of a salary batch: never the file's content.
type SalaryBatchRelease struct {
	ID         string `json:"id" format:"uuid" doc:"The release." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c13"`
	ReleasedBy Person `json:"released_by" doc:"Who released it."`
	ReleasedAt string `json:"released_at" format:"date-time" doc:"When it was released." example:"2026-11-30T09:30:00+08:00"`
	ApprovalID string `json:"approval_id" format:"uuid" doc:"The approval it was released under." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c20"`
	Writer     string `json:"writer" doc:"The file's format and version." example:"payment-list@1"`
	FileName   string `json:"file_name" doc:"The file's name." example:"SB-0003-R1-payment-list.csv"`
	SHA256     string `json:"sha256" doc:"The SHA-256 of the file as written, in hexadecimal." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	Lines      int    `json:"lines,omitempty" doc:"How many people it pays; only with the salary-payments right, since a release of one person would state their pay (FR-PPL-17)." example:"11"`
	Total      *Money `json:"total,omitempty" doc:"What it pays in all; only with the salary-payments right (FR-PPL-17)."`
}

// SalaryBatch is a salary batch as every role that may see it sees it: the run, the state, the totals and counts. The lines appear only to the roles with the salary-payments right.
type SalaryBatch struct {
	ID             string               `json:"id" format:"uuid" doc:"The batch's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c10"`
	Number         string               `json:"number" doc:"Its number." example:"SB-0003"`
	RunID          string               `json:"run_id" format:"uuid" doc:"The pay run in Nemura." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5d01"`
	RunVersion     int                  `json:"run_version" doc:"The version of the run it pays." example:"1"`
	WageMonth      string               `json:"wage_month" doc:"The wage month, YYYY-MM." example:"2026-11"`
	Status         string               `json:"status" enum:"open,approved,cancelled" doc:"open: waiting for preparation or approval; approved: ready to release; cancelled: before any release, with a reason." example:"approved"`
	State          string               `json:"state" enum:"received,prepared,approved,released_in_part,released,paid_in_part,paid,cancelled" doc:"What a person reads: from its lines' states. A batch is paid only when every line is paid (FR-PPL-12)." example:"approved"`
	MoneyAccountID string               `json:"money_account_id,omitempty" format:"uuid" doc:"The SGD bank account it is paid from, once prepared." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d10"`
	PaymentDate    string               `json:"payment_date,omitempty" format:"date" doc:"The day the bank pays it, once prepared." example:"2026-11-30"`
	Method         string               `json:"method,omitempty" enum:"fast,giro" doc:"How the bank pays it, once prepared." example:"giro"`
	LineCount      int                  `json:"line_count" doc:"How many people it pays." example:"12"`
	Total          Money                `json:"total" doc:"What it pays in all, in SGD."`
	Released       int                  `json:"released" doc:"How many lines a release has taken." example:"11"`
	Paid           int                  `json:"paid" doc:"How many lines are paid." example:"0"`
	Waiting        int                  `json:"waiting" doc:"How many lines are not free to release now (held, new bank details, unknown outcome)." example:"1"`
	Reapproval     bool                 `json:"reapproval_needed" doc:"Approved: a line or a bank detail changed since its latest approval, so it is not released until it is approved again (FR-PPL-14)." example:"false"`
	ApprovalID     string               `json:"approval_id,omitempty" format:"uuid" doc:"The approval request it waits for or was last approved under." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c20"`
	Releases       []SalaryBatchRelease `json:"releases" nullable:"false" doc:"Its releases, oldest first."`
	LinesVisible   bool                 `json:"lines_visible" doc:"Whether the caller holds the salary-payments right and so sees the lines (FR-PPL-17)." example:"false"`
	Lines          []SalaryBatchLine    `json:"lines,omitempty" doc:"Each person's line; present only with the salary-payments right."`
	Checks         []BillCheck          `json:"checks" nullable:"false" doc:"What stops it being asked for or released now (✖), without a name or an amount."`
	Version        int                  `json:"version" doc:"The batch's version; with its status it makes the ETag." example:"1"`
	LinesVersion   int                  `json:"lines_version" doc:"The latest payments.ready object version received." example:"1"`
	CancelReason   string               `json:"cancel_reason,omitempty" doc:"Why it was cancelled." example:"The run was withdrawn"`
}

// SalaryBatchPage is a page of salary batches, newest first.
type SalaryBatchPage struct {
	Items      []SalaryBatch `json:"items" nullable:"false"`
	NextCursor string        `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"MTI"`
}

// SalaryBatchPrepare names where and when a salary batch is paid from.
type SalaryBatchPrepare struct {
	MoneyAccountID string `json:"money_account_id" format:"uuid" doc:"The SGD bank account the batch is paid from: the chart's money account." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d10"`
	PaymentDate    string `json:"payment_date" format:"date" doc:"The day the bank pays it." example:"2026-11-30"`
	Method         string `json:"method" enum:"fast,giro" doc:"FAST pays within minutes, up to the bank's limit; GIRO over the next working days." example:"giro"`
}

// SalaryBatchCancel is why a batch is cancelled.
type SalaryBatchCancel struct {
	Reason string `json:"reason" minLength:"1" maxLength:"500" doc:"Why it is cancelled: its lines are not paid in this batch." example:"The run was withdrawn in Nemura"`
}

// SalaryLineState records what became of one payment, with evidence (FR-PPL-12): a person's record, never the file's or a release's. A bank-feed match records paid itself.
type SalaryLineState struct {
	State      string `json:"state" enum:"sent,accepted,paid,rejected,returned,unknown" doc:"What became of the payment. Paid, rejected, returned and unknown are recorded with evidence; an exported file or a release never makes a line paid." example:"rejected"`
	EvidenceID string `json:"evidence_id" format:"uuid" doc:"A document of the person paid or the organisation that shows it (a bank advice, a statement): required." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c30"`
	On         string `json:"on" format:"date" doc:"The day it happened." example:"2026-11-30"`
}

// SalaryBatchFile is a released salary file: its content in base64. It holds names and full account numbers.
type SalaryBatchFile = PaymentBatchFile

// SalaryBatchLinePath names a line in the path.
type SalaryBatchLinePath struct {
	SalaryBatchPath
	LineID string `path:"line_id" format:"uuid" doc:"Nemura's ID of the payment line." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c11"`
}

// SalaryBatchPath names a salary batch in the path.
type SalaryBatchPath struct {
	BatchID string `path:"batch_id" format:"uuid" doc:"The salary batch's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c10"`
}

type (
	salaryListInput struct {
		PageQuery
		Status string `query:"status" enum:"open,approved,cancelled" doc:"Only batches with this status." example:"approved"`
	}
	salaryListOutput struct{ Body SalaryBatchPage }
	salaryGetInput   struct{ SalaryBatchPath }
	salaryOutput     struct {
		ETag string `header:"ETag" doc:"The batch's version and status, for If-Match."`
		Body SalaryBatch
	}
	salaryWriteOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"The batch's version and status, for If-Match."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was changed."`
		Body     SalaryBatch
	}
	salaryApprovalOutput struct {
		Status   int
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was asked."`
		Body     Approval
	}
	salaryPrepareInput struct {
		SalaryBatchPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body SalaryBatchPrepare
	}
	salaryCommandInput struct {
		SalaryBatchPath
		KeyHeader
		IfMatchHeader
		PreferHeader
	}
	salaryCancelInput struct {
		SalaryBatchPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body SalaryBatchCancel
	}
	salaryStateInput struct {
		SalaryBatchLinePath
		KeyHeader
		PreferHeader
		Body SalaryLineState
	}
	salaryFileOutput struct{ Body SalaryBatchFile }
)

var salaryFixes = map[string]string{
	salarybatch.CodeNotPrepared: "Prepare the batch: choose the money account, the payment date and the method",
	salarybatch.CodeAccount:     "Choose an open SGD bank account: a salary batch pays SGD by FAST or GIRO",
	salarybatch.CodeNothing:     "Wait until a hold is lifted or a changed bank detail is approved again, or take the lines out",
	salarybatch.CodeNotOpen:     "Ask for the batch's approval first, or open a batch that is not cancelled",
	salarybatch.CodeReapprove:   "Ask for the batch's approval again: the approver signs for the lines and bank details it holds now",
	salarybatch.CodeSeparation:  "Ask another Payment releaser who neither prepared, asked for or approved the batch nor prepared or approved the pay run in Nemura",
	salarybatch.CodeBlocked:     "Wait for Nemura to send the person's line again with the new bank details, then approve the batch again",
	salarybatch.CodeUnknown:     "Resolve the unknown outcome first: record whether the money was paid, rejected or returned",
	salarybatch.CodeReason:      "Say why it is cancelled",
	salarybatch.CodeDate:        "Send the payment date as YYYY-MM-DD, in or after the wage month",
	salarybatch.CodeMethod:      "Choose fast or giro",
	salarybatch.CodeState:       "Choose a state the line may move to from where it is; paid, rejected, returned and unknown need evidence",
	salarybatch.CodeLine:        "Use a line of this batch",
}

func salaryProblem(code, detail string) *Problem { return problem(code, detail, salaryFixes[code]) }

// seesSalaryLines is whether the caller holds the salary-payments right: a right to create or approve on the pay-run row, which only the Owner and the Payroll administrator hold by default; the
// Payment releaser, the Controller, the Accountant and the Auditor see totals, counts and states (FR-PPL-17).
func seesSalaryLines(p authn.Principal) bool {
	for _, need := range []authz.Right{authz.Create, authz.Approve} {
		d := authz.Decide(authz.Policy{Operation: "seeSalaryLines", Capability: authz.PayRuns, Need: need, Unqualified: true}, p.Roles, p.AI())
		if d.Allowed && d.Right != authz.Masked {
			return true
		}
	}
	return false
}

func salaryNumber(b storage.SalaryBatch) string { return fmt.Sprintf("SB-%04d", b.Seq) }

func salaryETag(b storage.SalaryBatch) string { return etag(b.Version, b.Status) }

// salaryLatestApproval is a batch's latest approved request, with the dependencies its envelope signed.
func salaryLatestApproval(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (*storage.ApprovalRequest, map[string]any, error) {
	approved, err := u.Approvals().List(ctx, string(approval.Approved))
	if err != nil {
		return nil, nil, err
	}
	var latest *storage.ApprovalRequest
	for _, r := range approved {
		if r.ObjectType == approval.ObjectSalaryBatch && r.ObjectKey == id.String() &&
			(latest == nil || r.DecidedAt != nil && latest.DecidedAt != nil && !r.DecidedAt.Before(*latest.DecidedAt)) {
			x := r
			latest = &x
		}
	}
	if latest == nil {
		return nil, nil, nil
	}
	return decodeDependencies(latest)
}

// salaryDependencies are what an approval of the batch binds: its version and status, its total and a digest of its lines (never a name or an account), and the run (FR-PPL-14).
func salaryDependencies(b storage.SalaryBatch, lines []storage.SalaryLine) (map[string]any, error) {
	total, err := salarybatch.Total(lines)
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": b.Status, "total": total.String(), "lines": salarybatch.LinesDigest(lines), "run": fmt.Sprintf("%s@%d", b.RunID, b.RunVersion)}, nil
}

// salaryAnalysis is what stops each line of a batch now, read from the projection: held, new bank details, an unknown outcome. The ask to Nemura (five seconds) is made at a release, not here.
type salaryAnalysis struct {
	batch    storage.SalaryBatch
	lines    []storage.SalaryLine
	latest   map[uuid.UUID]storage.SalaryLineEvent
	released map[uuid.UUID]bool
	people   map[uuid.UUID]peoplelink.Person
	waiting  map[uuid.UUID]string // line ref -> reason
}

func (a *API) analyseSalary(ctx context.Context, u storage.UnitOfWork, b storage.SalaryBatch) (salaryAnalysis, error) {
	out := salaryAnalysis{batch: b, released: map[uuid.UUID]bool{}, people: map[uuid.UUID]peoplelink.Person{}, waiting: map[uuid.UUID]string{}}
	var err error
	if out.lines, err = u.SalaryBatches().Lines(ctx, b.ID); err != nil {
		return out, err
	}
	events, err := u.SalaryBatches().Events(ctx, b.ID)
	if err != nil {
		return out, err
	}
	out.latest = salarybatch.Latest(events)
	rl, err := u.SalaryBatches().ReleaseLines(ctx, b.ID)
	if err != nil {
		return out, err
	}
	for _, x := range rl {
		out.released[x.LineRef] = true
	}
	for _, l := range out.lines {
		if out.released[l.LineRef] {
			continue
		}
		person, err := u.PeopleLinks().Person(ctx, l.PersonID)
		if err != nil {
			return out, err
		}
		out.people[l.PersonID] = person
		switch {
		case person.Held:
			out.waiting[l.LineRef] = "held"
		case person.BankVersion > l.BankDetailVersion:
			out.waiting[l.LineRef] = "bank_details"
		default:
			states, err := u.SalaryBatches().StatesOfRunPerson(ctx, b.RunID, l.PersonID)
			if err != nil {
				return out, err
			}
			for _, s := range states {
				if salarybatch.BlocksInstruction(s) {
					out.waiting[l.LineRef] = "unknown_outcome"
				}
			}
		}
	}
	return out, nil
}

// maskAccount is a bank and account as an approver or a viewer sees them: the BIC and the last four digits.
func maskAccount(d salarybatch.Detail) string {
	n := d.AccountNumber
	if len(n) > 4 {
		n = n[len(n)-4:]
	}
	return d.BankBIC + " ••••" + n
}

// salaryOf is a batch as the caller sees it: totals, counts and states for every role that may read it, lines only with the salary-payments right.
func (a *API) salaryOf(ctx context.Context, u storage.UnitOfWork, p authn.Principal, b storage.SalaryBatch, withLines bool) (SalaryBatch, error) {
	an, err := a.analyseSalary(ctx, u, b)
	if err != nil {
		return SalaryBatch{}, err
	}
	total, err := salarybatch.Total(an.lines)
	if err != nil {
		return SalaryBatch{}, err
	}
	out := SalaryBatch{ID: b.ID.String(), Number: salaryNumber(b), RunID: b.RunID.String(), RunVersion: b.RunVersion, WageMonth: b.WageMonth.Format("2006-01"), Status: b.Status,
		State: salarybatch.Overall(b, an.latest, an.lines), LineCount: len(an.lines), Total: moneyOut(total), Releases: []SalaryBatchRelease{}, Checks: []BillCheck{},
		Version: b.Version, LinesVersion: b.LinesVersion, CancelReason: b.CancelReason, LinesVisible: seesSalaryLines(p)}
	if b.PayDate != nil {
		out.PaymentDate, out.Method = b.PayDate.Format(time.DateOnly), b.Method
		out.MoneyAccountID = b.MoneyAccountID.String()
	}
	for _, l := range an.lines {
		switch s := an.latest[l.LineRef].State; {
		case s == storage.LinePaid:
			out.Paid++
			out.Released++
		case an.released[l.LineRef]:
			out.Released++
		}
		if an.waiting[l.LineRef] != "" {
			out.Waiting++
		}
	}
	r, deps, err := salaryLatestApproval(ctx, u, b.ID)
	if err != nil {
		return SalaryBatch{}, err
	}
	if r != nil {
		out.ApprovalID = r.ID.String()
	}
	if b.Status == storage.SalaryApproved {
		now, err := salaryDependencies(b, an.lines)
		if err != nil {
			return SalaryBatch{}, err
		}
		out.Reapproval = r == nil || fmt.Sprint(deps["lines"]) != fmt.Sprint(now["lines"]) || fmt.Sprint(deps["total"]) != fmt.Sprint(now["total"])
	}
	releases, err := u.SalaryBatches().Releases(ctx, b.ID)
	if err != nil {
		return SalaryBatch{}, err
	}
	rl, err := u.SalaryBatches().ReleaseLines(ctx, b.ID)
	if err != nil {
		return SalaryBatch{}, err
	}
	for _, rel := range releases {
		who := Person{Kind: "user", ID: rel.ReleasedBy.String()}
		if err := a.name(ctx, u, &who); err != nil {
			return SalaryBatch{}, err
		}
		release := SalaryBatchRelease{ID: rel.ID.String(), ReleasedBy: who, ReleasedAt: rel.ReleasedAt.UTC().Format(time.RFC3339), ApprovalID: rel.ApprovalID.String(),
			Writer: rel.Writer, FileName: rel.FileName, SHA256: hex.EncodeToString(rel.SHA256)}
		if out.LinesVisible {
			relTotal, _ := money.ParseStored(string(rel.Total), salarybatch.SGD)
			t := moneyOut(relTotal)
			release.Lines, release.Total = rel.Lines, &t
		}
		out.Releases = append(out.Releases, release)
	}
	if withLines && out.LinesVisible {
		instruction := map[uuid.UUID]string{}
		for _, x := range rl {
			instruction[x.LineRef] = x.InstructionID
		}
		person := func(id uuid.UUID) string {
			if pp, ok := an.people[id]; ok {
				return pp.DisplayName
			}
			pp, err := u.PeopleLinks().Person(ctx, id)
			if err != nil {
				return ""
			}
			return pp.DisplayName
		}
		for _, l := range an.lines {
			amount, _ := salarybatch.Amount(l)
			line := SalaryBatchLine{LineID: l.LineRef.String(), PersonID: l.PersonID.String(), PersonName: person(l.PersonID), Amount: moneyOut(amount), BankDetailVersion: l.BankDetailVersion,
				LineVersion: l.LineVersion, State: an.latest[l.LineRef].State, InstructionID: instruction[l.LineRef], Waiting: an.waiting[l.LineRef]}
			if a.deps.Fields != nil {
				if clear, err := a.deps.Fields.Open(ctx, l.Sealed, salarybatch.LineAAD(p.Organisation, b.ID, l.LineRef, l.LineVersion)); err == nil {
					if d, err := salarybatch.ReadDetail(clear); err == nil {
						line.Account = maskAccount(d)
					}
				}
			}
			out.Lines = append(out.Lines, line)
		}
	}
	return out, nil
}

// salaryChecks are what stops a batch being asked for now, with no name and no amount.
func (a *API) salaryChecks(ctx context.Context, u storage.UnitOfWork, b storage.SalaryBatch) ([]BillCheck, error) {
	var out []BillCheck
	add := func(code, field, msg string) {
		out = append(out, BillCheck{Severity: "error", Code: code, Field: field, Message: msg, Fix: salaryFixes[code]})
	}
	if b.Status == storage.SalaryCancelled {
		add(salarybatch.CodeNotOpen, "status", "The batch is cancelled.")
		return out, nil
	}
	if b.PayDate == nil {
		add(salarybatch.CodeNotPrepared, "money_account_id", "The batch has no money account, payment date and method yet.")
		return out, nil
	}
	if problem := a.checkSalaryAccount(ctx, u, b); problem != nil {
		add(salarybatch.CodeAccount, "money_account_id", problem.Detail)
	}
	return out, nil
}

// checkSalaryAccount is nil when the batch's money account is an open SGD bank account on its payment date.
func (a *API) checkSalaryAccount(ctx context.Context, u storage.UnitOfWork, b storage.SalaryBatch) *Problem {
	all, err := u.Banking().MoneyAccounts(ctx)
	if err != nil {
		return problem(salarybatch.CodeAccount, "The money accounts could not be read.", salaryFixes[salarybatch.CodeAccount])
	}
	for _, m := range all {
		if m.AccountID != b.MoneyAccountID {
			continue
		}
		if m.Currency != salarybatch.SGD.Code || m.Kind != storage.MoneyBank || (m.ClosedOn != nil && b.PayDate != nil && !m.ClosedOn.After(*b.PayDate)) {
			return problem(salarybatch.CodeAccount, "The money account is not an open SGD bank account.", salaryFixes[salarybatch.CodeAccount])
		}
		return nil
	}
	return problem(salarybatch.CodeAccount, "The money account is not a bank account in Banking.", salaryFixes[salarybatch.CodeAccount])
}

func (a *API) salaryBatch(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, forUpdate bool) (storage.SalaryBatch, error) {
	b, err := u.SalaryBatches().Get(ctx, id, forUpdate)
	if errors.Is(err, ports.ErrNotFound) {
		return b, problem(CodeNotFound, "There is no such salary batch.", "Check the batch's ID")
	}
	return b, err
}

// salaryDependenciesOf is the approval's dependencies for the batch key.
func (a *API) salaryDependenciesOf(ctx context.Context, u storage.UnitOfWork, key string) (int, map[string]any, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return 0, nil, err
	}
	b, err := a.salaryBatch(ctx, u, id, false)
	if err != nil {
		return 0, nil, err
	}
	lines, err := u.SalaryBatches().Lines(ctx, id)
	if err != nil {
		return 0, nil, err
	}
	deps, err := salaryDependencies(b, lines)
	return b.Version, deps, err
}

// salaryExecutable refuses a signer who prepared or approved the run in Nemura (SOD-4, by issuer and subject) or who is the one the link prepared it for: FM-1's own check, that the requester does not
// approve, is the engine's.
func salaryExecutable(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, signer uuid.UUID) error {
	id, err := uuid.Parse(r.ObjectKey)
	if err != nil {
		return err
	}
	if party, err := u.SalaryBatches().IsRunParty(ctx, id, signer); err != nil {
		return err
	} else if party {
		return salaryProblem(salarybatch.CodeSeparation, "You prepared or approved this pay run in Nemura, so another person approves its salary batch (SOD-4).")
	}
	return nil
}

// executeSalaryApproval approves a batch on its approval: its checks again and the approved lines' digest, then the batch approved (the database refuses it without a second person who is neither
// the requester nor the run's preparer or approver). A re-approval of an approved batch changes nothing but the approval the release checks.
func (a *API) executeSalaryApproval(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest) error {
	id, err := uuid.Parse(r.ObjectKey)
	if err != nil {
		return err
	}
	b, err := a.salaryBatch(ctx, u, id, true)
	if err != nil {
		return err
	}
	checks, err := a.salaryChecks(ctx, u, b)
	if err != nil {
		return err
	}
	if len(checks) > 0 {
		return salaryProblem(checks[0].Code, checks[0].Message)
	}
	if b.Status == storage.SalaryApproved {
		return nil
	}
	_, err = u.SalaryBatches().Approve(ctx, id, r.ID, a.deps.Clock.Now())
	return err
}

// salaryConflicts are the separation rules a release by releaser breaks in the batch's own people (SOD-2): preparing, asking for or approving it.
func salaryConflicts(ctx context.Context, u storage.UnitOfWork, b storage.SalaryBatch, r *storage.ApprovalRequest, releaser uuid.UUID) ([]string, error) {
	people, err := actorIDs(ctx, u, b.PreparedBy, r)
	if err != nil {
		return nil, err
	}
	if slices.Contains(people, releaser) {
		return []string{"SOD-2: the releaser prepared, asked for or approved the batch"}, nil
	}
	return nil, nil
}

// releaseSalaryBatch releases the lines of an approved batch that are free to go now, by p, in u, and leaves the others waiting (FR-PPL-13, FR-PPL-14, FR-PPL-12).
func (a *API) releaseSalaryBatch(ctx context.Context, u storage.UnitOfWork, p authn.Principal, id uuid.UUID, ifMatch string) (SalaryBatch, error) {
	b, err := a.salaryBatch(ctx, u, id, true)
	if err != nil {
		return SalaryBatch{}, err
	}
	if err := checkIfMatch(ifMatch, salaryETag(b)); err != nil {
		return SalaryBatch{}, err
	}
	if b.Status != storage.SalaryApproved {
		return SalaryBatch{}, salaryProblem(salarybatch.CodeNotOpen, "The batch is not approved, so it is not released.")
	}
	// SOD-4: whoever prepared or approved the run in Nemura never releases its salary, in every team mode (the database refuses it too).
	if party, err := u.SalaryBatches().IsRunParty(ctx, id, p.UserID); err != nil {
		return SalaryBatch{}, err
	} else if party {
		return SalaryBatch{}, salaryProblem(salarybatch.CodeSeparation, "You prepared or approved this pay run in Nemura, so another person releases its salary batch (SOD-4).")
	}
	if pr := a.checkSalaryAccount(ctx, u, b); pr != nil {
		return SalaryBatch{}, pr
	}
	an, err := a.analyseSalary(ctx, u, b)
	if err != nil {
		return SalaryBatch{}, err
	}
	r, deps, err := salaryLatestApproval(ctx, u, id)
	if err != nil {
		return SalaryBatch{}, err
	}
	now, err := salaryDependencies(b, an.lines)
	if err != nil {
		return SalaryBatch{}, err
	}
	if r == nil || fmt.Sprint(deps["lines"]) != fmt.Sprint(now["lines"]) || fmt.Sprint(deps["total"]) != fmt.Sprint(now["total"]) {
		return SalaryBatch{}, salaryProblem(salarybatch.CodeReapprove, "A line or a bank detail changed since the batch was approved.")
	}
	found, err := salaryConflicts(ctx, u, b, r, p.UserID)
	if err != nil {
		return SalaryBatch{}, err
	}
	mode, _, _, err := teamModeOf(ctx, u)
	if err != nil {
		return SalaryBatch{}, err
	}
	if len(found) > 0 && team.Blocks(mode) {
		return SalaryBatch{}, salaryProblem(salarybatch.CodeSeparation, "You cannot release this batch: "+strings.Join(found, "; ")+".")
	}

	// The lines free to go: not released, not waiting on the projection; then the ask to Nemura (five seconds) for the people who are not held there, so a hold it knows of and the projection does not
	// stops the person's line, and a line it cannot answer for waits (PPL-22). The rest go.
	var free []storage.SalaryLine
	var people []peoplelink.Person
	for _, l := range an.lines {
		if an.released[l.LineRef] || an.waiting[l.LineRef] != "" || an.latest[l.LineRef].State != storage.LineScheduled {
			continue
		}
		free = append(free, l)
		people = append(people, an.people[l.PersonID])
	}
	status, err := holdStatus(ctx, u, a.deps.Holds, people)
	if err != nil {
		return SalaryBatch{}, err
	}
	var go_ []storage.SalaryLine
	reasons := map[string]int{}
	for _, l := range an.lines {
		if w := an.waiting[l.LineRef]; w != "" && !an.released[l.LineRef] {
			reasons[w]++
		}
	}
	for _, l := range free {
		switch status[l.PersonID] {
		case "held":
			reasons["held"]++
		case "unknown":
			reasons["hold_unknown"]++
		default:
			go_ = append(go_, l)
		}
	}
	if len(go_) == 0 {
		return SalaryBatch{}, salaryProblem(salarybatch.CodeNothing, "No line can be released now: "+waitingWords(reasons)+".")
	}

	// The file its money account gives now: one its bank takes and Fyniq writes, else nothing is released.
	macc, err := salaryMoneyAccount(ctx, u, b.MoneyAccountID)
	if err != nil {
		return SalaryBatch{}, err
	}
	provider, err := banking.ProviderOfAccount(ctx, u, macc)
	if err != nil {
		return SalaryBatch{}, err
	}
	writer, refused := payFiles.Writer(macc.PaymentFile, provider)
	if refused != nil {
		return SalaryBatch{}, problem(refused.Code, sentence(refused.Message), "Change the money account's payment file to one its bank takes, such as the payment list, then release the batch")
	}
	if a.deps.Fields == nil {
		return SalaryBatch{}, payee.NoKeys()
	}
	releases, err := u.SalaryBatches().Releases(ctx, id)
	if err != nil {
		return SalaryBatch{}, err
	}
	round := len(releases) + 1
	number := salaryNumber(b)
	f := paymentfile.File{Batch: fmt.Sprintf("%s-R%d", number, round), PaymentDate: *b.PayDate, Method: b.Method, Debit: paymentfile.Debit{Name: macc.Name, Identifier: macc.Identifier}}
	total := money.Zero(salarybatch.SGD)
	var relLines []storage.SalaryReleaseLine
	for i, l := range go_ {
		clear, err := a.deps.Fields.Open(ctx, l.Sealed, salarybatch.LineAAD(p.Organisation, id, l.LineRef, l.LineVersion))
		if err != nil {
			return SalaryBatch{}, err
		}
		d, err := salarybatch.ReadDetail(clear)
		if err != nil {
			return SalaryBatch{}, err
		}
		amount, err := salarybatch.Amount(l)
		if err != nil {
			return SalaryBatch{}, err
		}
		if total, err = total.Add(amount); err != nil {
			return SalaryBatch{}, err
		}
		instruction := fmt.Sprintf("SB%04d-R%d-L%02d", b.Seq, round, i+1)
		f.Payments = append(f.Payments, paymentfile.Payment{Payee: d.PayeeName, Kind: "employee", BIC: d.BankBIC, AccountName: d.AccountName, Number: d.AccountNumber, Amount: amount, Reference: instruction})
		relLines = append(relLines, storage.SalaryReleaseLine{LineRef: l.LineRef, LineID: l.ID, InstructionID: instruction})
	}
	out, err := writer.Write(f)
	if err != nil {
		return SalaryBatch{}, err
	}
	sum := sha256.Sum256(out.Data)
	sealed, err := a.deps.Fields.Seal(ctx, out.Data, salarybatch.FileAAD(p.Organisation, id, sum[:]))
	if err != nil {
		return SalaryBatch{}, err
	}
	at := a.deps.Clock.Now()
	rel, err := u.SalaryBatches().Release(ctx, storage.SalaryRelease{BatchID: id, BatchVersion: b.Version, ApprovalID: r.ID, ReleasedBy: p.UserID, ReleasedAt: at, Writer: writer.Name(), FileName: out.Name,
		MediaType: out.MediaType, Sealed: sealed, SHA256: sum[:], Total: storage.Decimal(total.String())}, relLines, at)
	if err != nil {
		return SalaryBatch{}, err
	}
	// In Founder mode a conflict is allowed, with the approval as its control, and logged for the weekly review (FM-3).
	if len(found) > 0 {
		names, err := memberNames(ctx, u, at)
		if err != nil {
			return SalaryBatch{}, err
		}
		for _, c := range found {
			actor := p.UserID
			if _, err := u.ReviewLog().Log(ctx, storage.NewReviewLogEntry{Kind: storage.ReviewReleaseConflict, ObjectType: approval.ObjectSalaryBatch, ObjectKey: id.String(),
				Summary: fmt.Sprintf("%s released %s, round %d, %s SGD, approved under request %s: %s", names[p.UserID], number, round, total.String(), r.ID, c), Actor: &actor, LoggedAt: at}); err != nil {
				return SalaryBatch{}, err
			}
		}
	}
	_ = rel
	b, err = u.SalaryBatches().Get(ctx, id, false)
	if err != nil {
		return SalaryBatch{}, err
	}
	return a.salaryOf(ctx, u, p, b, true)
}

// waitingWords says, without a name, why the lines left are not released.
func waitingWords(reasons map[string]int) string {
	var parts []string
	for _, k := range []string{"held", "hold_unknown", "bank_details", "unknown_outcome"} {
		if n := reasons[k]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, map[string]string{"held": "held for tax clearance", "hold_unknown": "waiting for Nemura to say whether they are held",
				"bank_details": "with bank details that changed", "unknown_outcome": "with an unknown outcome"}[k]))
		}
	}
	if len(parts) == 0 {
		return "every line is released already"
	}
	return strings.Join(parts, ", ")
}

// salaryMoneyAccount is the batch's money account by its chart account.
func salaryMoneyAccount(ctx context.Context, u storage.UnitOfWork, chart uuid.UUID) (storage.MoneyAccount, error) {
	all, err := u.Banking().MoneyAccounts(ctx)
	if err != nil {
		return storage.MoneyAccount{}, err
	}
	for _, m := range all {
		if m.AccountID == chart {
			return m, nil
		}
	}
	return storage.MoneyAccount{}, problem(salarybatch.CodeAccount, "The money account is not a bank account in Banking.", salaryFixes[salarybatch.CodeAccount])
}

func registerSalaryBatches(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}

	register(a, authz.ListSalaryBatches, huma.Operation{
		OperationID: "listSalaryBatches", Method: http.MethodGet, Path: "/v1/salary-batches", Tags: []string{"People"},
		Summary: "List salary batches",
		Description: "The salary payment batches, newest first, a page at a time, narrowed by status (FR-PAY-11): each with its run, state, total and counts. Names, accounts and amounts of lines are " +
			"never in a list, and appear in a batch only for the roles with the salary-payments right (FR-PPL-17). Read-only; no side effects.",
	}, []string{CodeCursor, CodeInvalid}, func(ctx context.Context, in *salaryListInput) (*salaryListOutput, error) {
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		below := 0
		if after != "" {
			if below, err = strconv.Atoi(after); err != nil {
				return nil, problem(CodeCursor, "The cursor is not one this list gave.", "Start again without a cursor")
			}
		}
		p, _ := authn.From(ctx)
		out := salaryListOutput{Body: SalaryBatchPage{Items: []SalaryBatch{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.SalaryBatches().List(ctx)
			if err != nil {
				return err
			}
			for _, b := range all {
				if below > 0 && b.Seq >= below || in.Status != "" && b.Status != in.Status {
					continue
				}
				if len(out.Body.Items) == in.PageSize {
					out.Body.NextCursor = encodeCursor(strconv.Itoa(out.Body.Items[len(out.Body.Items)-1].seq()))
					return nil
				}
				s, err := a.salaryOf(ctx, u, p, b, false)
				if err != nil {
					return err
				}
				out.Body.Items = append(out.Body.Items, s)
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.GetSalaryBatch, huma.Operation{
		OperationID: "getSalaryBatch", Method: http.MethodGet, Path: "/v1/salary-batches/{batch_id}", Tags: []string{"People"},
		Summary: "Get a salary batch",
		Description: "One salary batch: its run, state, money account, payment date, method, total, counts, releases and what stops it now (FR-PAY-11). With the salary-payments right it also lists each line " +
			"(the person, the amount, the account masked to its last four digits, the line's state and why it waits); without it the lines are not in the answer (FR-PPL-17). Read-only; no side effects.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *salaryGetInput) (*salaryOutput, error) {
		id, err := parseID("batch_id", in.BatchID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out salaryOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			b, err := a.salaryBatch(ctx, u, id, false)
			if err != nil {
				return err
			}
			if out.Body, err = a.salaryOf(ctx, u, p, b, true); err != nil {
				return err
			}
			if out.Body.Checks, err = a.salaryChecks(ctx, u, b); err != nil {
				return err
			}
			out.ETag = salaryETag(b)
			return nil
		})
		return &out, err
	})

	register(a, authz.PrepareSalaryBatch, huma.Operation{
		OperationID: "prepareSalaryBatch", Method: http.MethodPost, Path: "/v1/salary-batches/{batch_id}/prepare", Tags: []string{"People"},
		Summary: "Prepare a salary batch",
		Description: "Names the SGD bank account the batch is paid from, the payment date and the method (FAST or GIRO) at the version read (FR-PAY-11). It raises the batch's version, so an earlier approval of " +
			"it is stale and the batch is approved again. A cancelled batch, or one that has a line released, is not prepared again. Side effects: the batch's version and its audit event. Needs If-Match with the " +
			"batch's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeInvalid, salarybatch.CodeDate, salarybatch.CodeMethod,
		salarybatch.CodeAccount, salarybatch.CodeNotOpen, storage.CodeSalaryBatch}, locks...), func(ctx context.Context, in *salaryPrepareInput) (*salaryWriteOutput, error) {
		id, err := parseID("batch_id", in.BatchID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		p, _ := authn.From(ctx)
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (SalaryBatch, error) {
			b, err := a.salaryBatch(ctx, u, id, true)
			if err != nil {
				return SalaryBatch{}, err
			}
			if err := checkIfMatch(in.IfMatch, salaryETag(b)); err != nil {
				return SalaryBatch{}, err
			}
			releases, err := u.SalaryBatches().Releases(ctx, id)
			if err != nil {
				return SalaryBatch{}, err
			}
			if b.Status == storage.SalaryCancelled || len(releases) > 0 {
				return SalaryBatch{}, salaryProblem(salarybatch.CodeNotOpen, "A cancelled batch, or one with a line released, is not prepared again.")
			}
			account, err := parseID("body.money_account_id", in.Body.MoneyAccountID)
			if err != nil {
				return SalaryBatch{}, err
			}
			day, err := time.Parse(time.DateOnly, in.Body.PaymentDate)
			if err != nil || day.Before(b.WageMonth) {
				return SalaryBatch{}, salaryProblem(salarybatch.CodeDate, "The payment date is not a date, or is before the wage month began.")
			}
			if in.Body.Method != salarybatch.FAST && in.Body.Method != salarybatch.GIRO {
				return SalaryBatch{}, salaryProblem(salarybatch.CodeMethod, "The method is not FAST or GIRO.")
			}
			probe := b
			probe.MoneyAccountID, probe.PayDate = account, &day
			if pr := a.checkSalaryAccount(ctx, u, probe); pr != nil {
				return SalaryBatch{}, pr
			}
			// A prepared batch that was approved is approved again: its version moves on, and the release checks the new approval.
			if b, err = u.SalaryBatches().Prepare(ctx, id, b.Version, account, day, in.Body.Method, "user:"+p.UserID.String(), a.deps.Clock.Now()); err != nil {
				return SalaryBatch{}, err
			}
			return a.salaryOf(ctx, u, p, b, true)
		})
		if err != nil {
			return nil, err
		}
		return &salaryWriteOutput{Status: w.Status, ETag: etag(w.Body.Version, w.Body.Status), Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.RequestSalaryBatchApproval, huma.Operation{
		OperationID: "requestSalaryBatchApproval", Method: http.MethodPost, Path: "/v1/salary-batches/{batch_id}/approval", Tags: []string{"People"}, DefaultStatus: http.StatusAccepted,
		Summary: "Ask for approval of a salary batch",
		Description: "Asks for another person's approval of a prepared salary batch at the version read, or for its approval again after a line or a bank detail changed (FR-PAY-11, FR-PPL-14, FM-1), " +
			"in every team mode. The answer is 202 with the request, which a person who holds A on \"Pay runs, employee data\", did not ask for it and did not prepare or approve the run in Nemura (SOD-4), " +
			"and passed step-up, approves; its envelope holds the batch's total and a digest of its lines (each person's amount and bank-detail record, version and digest), never a name or an account, so the " +
			"approval is for exactly these lines. Needs a person signed in who passed step-up, and If-Match with the batch's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeSessionRequired, CodeStepUpRequired, approval.CodePending,
		storage.CodeApproval, salarybatch.CodeNotPrepared, salarybatch.CodeAccount, salarybatch.CodeNotOpen}, locks...), func(ctx context.Context, in *salaryCommandInput) (*salaryApprovalOutput, error) {
		id, err := parseID("batch_id", in.BatchID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
			b, err := a.salaryBatch(ctx, u, id, true)
			if err != nil {
				return Approval{}, err
			}
			if err := checkIfMatch(in.IfMatch, salaryETag(b)); err != nil {
				return Approval{}, err
			}
			checks, err := a.salaryChecks(ctx, u, b)
			if err != nil {
				return Approval{}, err
			}
			if len(checks) > 0 {
				return Approval{}, salaryProblem(checks[0].Code, checks[0].Message)
			}
			lines, err := u.SalaryBatches().Lines(ctx, id)
			if err != nil {
				return Approval{}, err
			}
			total, err := salarybatch.Total(lines)
			if err != nil {
				return Approval{}, err
			}
			verb := "Pay"
			if b.Status == storage.SalaryApproved {
				verb = "Approve again, after a line or a bank detail changed:"
			}
			summary := fmt.Sprintf("%s salary batch %s: %d %s, %s SGD, on %s by %s", verb, salaryNumber(b), len(lines), map[bool]string{true: "person", false: "people"}[len(lines) == 1], total.String(),
				b.PayDate.Format("2 Jan 2006"), strings.ToUpper(b.Method))
			// No name and no account in the request, its summary or its diff: an approver who may not see the lines signs for a total and a digest (FR-PPL-17).
			return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectSalaryBatch, ObjectKey: id.String(), Operation: approval.OpSalaryBatchApprove, Summary: summary,
				Execute: map[string]any{"batch_id": id.String(), "version": fmt.Sprint(b.Version)}, Diff: []ApprovalDiff{{Field: "People paid", After: strconv.Itoa(len(lines))}, {Field: "Total", After: total.String() + " SGD"}},
				Amounts: []approval.Amount{{Amount: total.String(), Currency: salarybatch.SGD.Code}}})
		})
		if err != nil {
			return nil, err
		}
		return &salaryApprovalOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.ReleaseSalaryBatch, huma.Operation{
		OperationID: "releaseSalaryBatch", Method: http.MethodPost, Path: "/v1/salary-batches/{batch_id}/release", Tags: []string{"People"},
		Summary: "Release the free lines of a salary batch",
		Description: "Releases an approved salary batch (FR-PAY-11, FR-PPL-13): the Payment releaser, a person stepped up in the last 5 minutes, who neither asked for, approved nor prepared the batch (SOD-2) " +
			"and did not prepare or approve the pay run in Nemura (SOD-4, in every team mode). The latest approval must still hold the lines and the total (else SB-05: ask again). It asks Nemura, the hold's " +
			"authority, within five seconds whether each person is held (PPL-21, PPL-22), and releases the lines that are free to go: a person held, one whose bank details changed, or one with an unknown " +
			"outcome in the run waits while the others are paid, and goes in a later release (SB-03 when none can go). Side effects, in one transaction: the payment file the money account is set to, " +
			"sealed (SEC-11) with its SHA-256, an instruction ID per line, each line recorded as released (never paid: that comes from the bank feed or a person's record with evidence), and the audit " +
			"event with counts and the file's hash only. No break-glass releases a salary. Needs If-Match with the batch's ETag. Idempotent with Idempotency-Key; Prefer: return=preview runs every check " +
			"and releases nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeSessionRequired, CodeStepUpRequired, salarybatch.CodeNotOpen,
		salarybatch.CodeReapprove, salarybatch.CodeSeparation, salarybatch.CodeNothing, salarybatch.CodeAccount, peoplelink.CodeHold, peoplelink.CodeHoldUnknown, payee.CodeNoKeys, storage.CodeSalaryBatch,
		team.CodeAction, paymentfile.CodeProvider, paymentfile.CodeNotBuilt}, locks...), func(ctx context.Context, in *salaryCommandInput) (*salaryWriteOutput, error) {
		id, err := parseID("batch_id", in.BatchID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		p, err := a.steppedUpPerson(ctx, "Releasing a salary batch")
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (SalaryBatch, error) {
			if err := checkConflicts(ctx, u, p, "Releasing a salary batch"); err != nil { // SOD-6
				return SalaryBatch{}, err
			}
			return a.releaseSalaryBatch(ctx, u, p, id, in.IfMatch)
		})
		if err != nil {
			return nil, err
		}
		if w.Previewed {
			return &salaryWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
		}
		return &salaryWriteOutput{Status: w.Status, ETag: etag(w.Body.Version, w.Body.Status), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	register(a, authz.GetSalaryBatchFile, huma.Operation{
		OperationID: "getSalaryBatchFile", Method: http.MethodGet, Path: "/v1/salary-batches/{batch_id}/releases/{release_id}/file", Tags: []string{"People"},
		Summary: "Download a salary release's payment file",
		Description: "Returns the file a release wrote, as it was written, in base64, with its SHA-256 (FR-PAY-11): the payment list or the bank's own file its money account was set to. For the Payment releaser, " +
			"a person stepped up in the last 5 minutes. It holds names and full account numbers: key it into the bank or upload it there, then delete it. Side effects: an audit event of the download, with the " +
			"file's hash and no name. Never cached.",
	}, []string{CodeNotFound, CodeSessionRequired, CodeStepUpRequired, payee.CodeNoKeys}, func(ctx context.Context, in *salaryFileInput) (*salaryFileOutput, error) {
		id, err := parseID("batch_id", in.BatchID)
		if err != nil {
			return nil, err
		}
		rid, err := parseID("release_id", in.ReleaseID)
		if err != nil {
			return nil, err
		}
		p, err := a.steppedUpPerson(ctx, "Downloading a salary payment file")
		if err != nil {
			return nil, err
		}
		var out salaryFileOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			rel, err := u.SalaryBatches().ReleaseOf(ctx, rid)
			if errors.Is(err, ports.ErrNotFound) || err == nil && rel.BatchID != id {
				return problem(CodeNotFound, "There is no such release of this salary batch.", "Check the release's ID")
			}
			if err != nil {
				return err
			}
			if a.deps.Fields == nil {
				return payee.NoKeys()
			}
			data, err := a.deps.Fields.Open(ctx, rel.Sealed, salarybatch.FileAAD(p.Organisation, id, rel.SHA256))
			if err != nil {
				return err
			}
			if sum := sha256.Sum256(data); !slices.Equal(sum[:], rel.SHA256) {
				return fmt.Errorf("salary batch %s: the file does not match its hash", id)
			}
			if err := u.Audit().Record(ctx, storage.AuditRecord{Action: "salary_batch.file_downloaded", ObjectType: "salary_batch", ObjectID: id,
				Changes: map[string][2]any{"file": {nil, map[string]any{"release": rid.String(), "sha256": hex.EncodeToString(rel.SHA256)}}}}); err != nil {
				return err
			}
			out.Body = SalaryBatchFile{FileName: rel.FileName, MediaType: rel.MediaType, SHA256: hex.EncodeToString(rel.SHA256), Size: len(data), Content: base64.StdEncoding.EncodeToString(data)}
			return nil
		})
		return &out, err
	})

	register(a, authz.CancelSalaryBatch, huma.Operation{
		OperationID: "cancelSalaryBatch", Method: http.MethodPost, Path: "/v1/salary-batches/{batch_id}/cancel", Tags: []string{"People"},
		Summary: "Cancel a salary batch",
		Description: "Cancels a salary batch with no line released, with a reason (FR-PAY-11): it pays nothing. A batch with a line released is never cancelled: record what became of each payment " +
			"(recordSalaryLineState). Side effects: the batch cancelled and its audit event with the reason. Needs If-Match with the batch's ETag. Idempotent with Idempotency-Key; Prefer: return=preview " +
			"checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeInvalid, salarybatch.CodeReason, salarybatch.CodeNotOpen,
		storage.CodeSalaryBatch}, locks...), func(ctx context.Context, in *salaryCancelInput) (*salaryWriteOutput, error) {
		id, err := parseID("batch_id", in.BatchID)
		if err != nil {
			return nil, err
		}
		reason := strings.TrimSpace(in.Body.Reason)
		if reason == "" {
			return nil, salaryProblem(salarybatch.CodeReason, "Cancelling a batch needs a reason.")
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		p, _ := authn.From(ctx)
		ctx = storage.WithReason(ctx, reason)
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (SalaryBatch, error) {
			b, err := a.salaryBatch(ctx, u, id, true)
			if err != nil {
				return SalaryBatch{}, err
			}
			if err := checkIfMatch(in.IfMatch, salaryETag(b)); err != nil {
				return SalaryBatch{}, err
			}
			releases, err := u.SalaryBatches().Releases(ctx, id)
			if err != nil {
				return SalaryBatch{}, err
			}
			if b.Status == storage.SalaryCancelled || len(releases) > 0 {
				return SalaryBatch{}, salaryProblem(salarybatch.CodeNotOpen, "A batch that is cancelled or has a line released is not cancelled.")
			}
			if b, err = u.SalaryBatches().Cancel(ctx, id, reason, a.deps.Clock.Now()); err != nil {
				return SalaryBatch{}, err
			}
			return a.salaryOf(ctx, u, p, b, true)
		})
		if err != nil {
			return nil, err
		}
		return &salaryWriteOutput{Status: w.Status, ETag: etag(w.Body.Version, w.Body.Status), Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})
}

// seq is the batch's number for the cursor.
func (s SalaryBatch) seq() int {
	n, _ := strconv.Atoi(strings.TrimPrefix(s.Number, "SB-"))
	return n
}

type salaryFileInput struct {
	SalaryBatchPath
	ReleaseID string `path:"release_id" format:"uuid" doc:"The release's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c13"`
}

// decodeDependencies reads the dependencies an approval's envelope signed.
func decodeDependencies(r *storage.ApprovalRequest) (*storage.ApprovalRequest, map[string]any, error) {
	var env struct {
		Dependencies map[string]any `json:"dependencies"`
	}
	if err := json.Unmarshal([]byte(r.Envelope), &env); err != nil {
		return nil, nil, err
	}
	return r, env.Dependencies, nil
}
