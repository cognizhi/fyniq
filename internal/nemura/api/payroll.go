package api

import (
	"context"
	"errors"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/nemura/payroll"
	"github.com/cognizhi/fyniq/internal/nemura/people"
)

// Problem codes of payroll (the catalogue is in problem.go).
const (
	CodePayrollInvalid  = payroll.CodeInvalid
	CodePayrollConflict = payroll.CodeConflict
)

// PayGroup is the people paid on one calendar (NM-PAY-03).
type PayGroup struct {
	ID           string `json:"id" format:"uuid" doc:"The pay group." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f30"`
	Name         string `json:"name" doc:"Its name." example:"Monthly"`
	PayDay       int    `json:"pay_day" doc:"The day of the month salary is paid; 0 is the last day of the month." example:"25"`
	CutoffDays   int    `json:"cutoff_days" doc:"Inputs are accepted until this many days before the pay day." example:"7"`
	ApprovalDays int    `json:"approval_days" doc:"The run is approved at the latest this many days before the pay day." example:"3"`
}

// PayGroupInput makes a pay group.
type PayGroupInput struct {
	Name         string `json:"name" minLength:"1" maxLength:"80" doc:"Its name." example:"Monthly"`
	PayDay       int    `json:"pay_day" minimum:"0" maximum:"28" doc:"The day of the month salary is paid; 0 is the last day of the month." example:"25"`
	CutoffDays   int    `json:"cutoff_days" minimum:"0" maximum:"20" doc:"Inputs are accepted until this many days before the pay day." example:"7"`
	ApprovalDays int    `json:"approval_days" minimum:"0" maximum:"20" doc:"The run is approved at the latest this many days before the pay day; no more than the cut-off." example:"3"`
}

// PayItem is an allowance or a fixed deduction a person has every month.
type PayItem struct {
	Key      string `json:"key" minLength:"1" maxLength:"40" doc:"The item's key, used once in a record." example:"transport"`
	Name     string `json:"name" minLength:"1" maxLength:"80" doc:"As the payslip shows it." example:"Transport allowance"`
	Kind     string `json:"kind" enum:"allowance,fixed_deduction" doc:"An allowance adds to pay; a fixed deduction takes from it." example:"allowance"`
	Nature   string `json:"nature" enum:"allowance,overtime,other,accommodation,cooperative,other_deduction" doc:"What it is: an allowance's or a deduction's kind." example:"allowance"`
	Amount   string `json:"amount" pattern:"^[0-9]{1,14}(\\.[0-9]{1,2})?$" doc:"Dollars a month, as a decimal string." example:"100.00"`
	CPFWages bool   `json:"cpf_wages" doc:"Whether it is CPF wages (FR-PAY-17); a deduction never is." example:"true"`
}

// YearToDate is the CPF wages this employer has counted for a person in a year.
type YearToDate struct {
	Year      int    `json:"year" doc:"The calendar year." example:"2026"`
	OW        string `json:"ow" doc:"Ordinary wages subject to CPF." example:"25000.00"`
	AWSubject string `json:"aw_subject" doc:"Additional wages subject to CPF." example:"0.00"`
	AWPaid    string `json:"aw_paid" doc:"Additional wages paid." example:"0.00"`
	Through   string `json:"through_month" format:"date" doc:"The first day of the last month counted." example:"2026-05-01"`
	Source    string `json:"source" enum:"import,run" doc:"Imported at cut-over, or added to by a run." example:"import"`
}

// PayView is a person's pay on a day. Only the Payroll administrator and the Owner read it.
type PayView struct {
	From          string      `json:"effective_from" format:"date" doc:"The day this record started." example:"2026-01-05"`
	To            string      `json:"effective_to,omitempty" format:"date" doc:"The day it ended, when it has." example:"2026-07-01"`
	GroupID       string      `json:"pay_group_id" format:"uuid" doc:"The pay group." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f30"`
	Group         string      `json:"pay_group" doc:"Its name." example:"Monthly"`
	BasicMonthly  string      `json:"basic_monthly" doc:"The basic salary a month, in dollars." example:"4500.00"`
	SHGFund       string      `json:"shg_fund,omitempty" doc:"The self-help group fund, when there is one." example:"CDAC"`
	SHGOptOut     bool        `json:"shg_opt_out" doc:"Whether the employee opted out of the fund." example:"false"`
	JointElection bool        `json:"joint_election" doc:"Whether a joint election to contribute at full employer rates is in force." example:"false"`
	Items         []PayItem   `json:"items" doc:"The allowances and fixed deductions."`
	YearToDate    *YearToDate `json:"year_to_date,omitempty" doc:"The year to date for the year of the day asked, when there is one."`
}

// PersonPayInput starts a person's pay record from a day.
type PersonPayInput struct {
	GroupID      string    `json:"pay_group_id" format:"uuid" doc:"The pay group." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f30"`
	BasicMonthly string    `json:"basic_monthly" pattern:"^[0-9]{1,14}(\\.[0-9]{1,2})?$" doc:"The basic salary a month, in dollars." example:"4500.00"`
	SHGFund      string    `json:"shg_fund,omitempty" enum:"CDAC,ECF,MBMF,SINDA," doc:"The self-help group fund, or empty for none." example:"CDAC"`
	SHGOptOut    bool      `json:"shg_opt_out,omitempty" doc:"The employee opted out of the fund." example:"false"`
	Items        []PayItem `json:"items,omitempty" maxItems:"30" doc:"The allowances and fixed deductions."`
}

// YTDInput imports a year to date.
type YTDInput struct {
	Year      int    `json:"year" minimum:"2020" maximum:"2100" doc:"The calendar year." example:"2026"`
	OW        string `json:"ow" pattern:"^[0-9]{1,14}(\\.[0-9]{1,2})?$" doc:"Ordinary wages subject to CPF this employer has counted." example:"25000.00"`
	AWSubject string `json:"aw_subject" pattern:"^[0-9]{1,14}(\\.[0-9]{1,2})?$" doc:"Additional wages subject to CPF." example:"0.00"`
	AWPaid    string `json:"aw_paid" pattern:"^[0-9]{1,14}(\\.[0-9]{1,2})?$" doc:"Additional wages paid, subject or not." example:"0.00"`
	Through   string `json:"through_month" format:"date" doc:"The first day of the last month counted." example:"2026-05-01"`
}

// OneOff is an item paid once.
type OneOff struct {
	ID        string `json:"id" format:"uuid" doc:"The item." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f31"`
	PersonID  string `json:"person_id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Person    string `json:"person" doc:"Their legal name." example:"Ben Lim"`
	Month     string `json:"month" format:"date" doc:"The wage month it is paid in." example:"2026-06-01"`
	Key       string `json:"key" doc:"Its key, used once for a person and month." example:"bonus-2026"`
	Name      string `json:"name" doc:"As the payslip shows it." example:"Annual bonus"`
	Nature    string `json:"nature" enum:"bonus,commission,allowance,other" doc:"What it is." example:"bonus"`
	Amount    string `json:"amount" doc:"Dollars." example:"5000.00"`
	CPFWages  bool   `json:"cpf_wages" doc:"Whether it is CPF wages." example:"true"`
	ForMonth  string `json:"for_month,omitempty" format:"date" doc:"The month of employment it is for." example:"2025-12-01"`
	PayableOn string `json:"payable_on,omitempty" format:"date" doc:"The day it is payable." example:"2026-06-30"`
	Note      string `json:"note,omitempty" doc:"A note." example:"Annual bonus."`
	Used      bool   `json:"used" doc:"Whether a run used it." example:"false"`
}

// OneOffInput adds an item.
type OneOffInput struct {
	Key       string `json:"key" minLength:"1" maxLength:"40" doc:"The item's key, used once for a person and month." example:"bonus-2026"`
	Name      string `json:"name" minLength:"1" maxLength:"80" doc:"As the payslip shows it." example:"Annual bonus"`
	Nature    string `json:"nature" enum:"bonus,commission,allowance,other" doc:"What it is." example:"bonus"`
	Amount    string `json:"amount" pattern:"^[0-9]{1,14}(\\.[0-9]{1,2})?$" doc:"Dollars." example:"5000.00"`
	CPFWages  bool   `json:"cpf_wages" doc:"Whether it is CPF wages." example:"true"`
	ForMonth  string `json:"for_month,omitempty" format:"date" doc:"The first day of the month of employment it is for; empty is the wage month." example:"2025-12-01"`
	PayableOn string `json:"payable_on,omitempty" format:"date" doc:"The day it is payable; empty is the pay day." example:"2026-06-30"`
	Note      string `json:"note,omitempty" maxLength:"300" doc:"A note." example:"Annual bonus."`
}

// RunFlag is something the preparer or the approver must see: ✖ blocks approval, ⚠ is shown.
type RunFlag struct {
	Code    string `json:"code" doc:"What it is about." example:"WAGE_CLASS"`
	Fail    bool   `json:"fail" doc:"True for ✖: the run cannot be approved with it." example:"false"`
	Message string `json:"message" doc:"In plain words, naming the person and what to do." example:"Commission: It is for the month's employment and payable by the 14th of the following month."`
}

// RunStep is one line of a calculation's working.
type RunStep struct {
	Rule string `json:"rule" doc:"The rule or requirement." example:"RND-CPF"`
	Text string `json:"text" doc:"What it did." example:"Rates employer 0.17 and employee 0.20: total 1850."`
}

// RunItem is a pay item of a person's month with its class and the record it came from.
type RunItem struct {
	Key        string `json:"key" doc:"The item." example:"basic"`
	Name       string `json:"name" doc:"As the payslip shows it." example:"Basic salary"`
	Class      string `json:"class" enum:"ordinary,additional,none,deduction" doc:"Ordinary or additional CPF wages by the facts, none when it is not CPF wages, or a deduction." example:"ordinary"`
	Amount     string `json:"amount" doc:"Dollars." example:"5000.00"`
	Why        string `json:"why,omitempty" doc:"Why it is in that class." example:"It is for the month's employment and payable by the 14th of the following month."`
	SourceType string `json:"source_type,omitempty" doc:"The kind of record it came from: person_pay, person_pay_item, pay_one_off, pay_input or overtime_entry." example:"person_pay"`
	SourceID   string `json:"source_id,omitempty" doc:"The record it came from." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f40"`
	Mismatch   bool   `json:"name_disagrees,omitempty" doc:"True when the item's name suggests another class than the facts give." example:"false"`
}

// RunLine is one person's month in a version.
type RunLine struct {
	PersonID       string            `json:"person_id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Person         string            `json:"person" doc:"Their legal name." example:"Ben Lim"`
	Gross          string            `json:"gross" doc:"Gross pay." example:"5000.00"`
	OWSubject      string            `json:"ow_subject" doc:"Ordinary wages subject to CPF." example:"5000.00"`
	AWSubject      string            `json:"aw_subject" doc:"Additional wages subject to CPF." example:"0.00"`
	CPFTotal       string            `json:"cpf_total" doc:"The total CPF contribution." example:"1850.00"`
	CPFEmployee    string            `json:"cpf_employee" doc:"The employee's CPF." example:"1000.00"`
	CPFEmployer    string            `json:"cpf_employer" doc:"The employer's CPF." example:"850.00"`
	SDL            string            `json:"sdl" doc:"The person's SDL before the employer-month rounding." example:"11.25"`
	SHG            string            `json:"shg" doc:"The self-help group fund." example:"0.00"`
	Deductions     string            `json:"deductions" doc:"Authorised deductions." example:"0.00"`
	Reimbursements string            `json:"reimbursements" doc:"Non-wage reimbursements paid with the salary." example:"0.00"`
	Held           string            `json:"held" doc:"Monies held under a clearance hold." example:"0.00"`
	Net            string            `json:"net" doc:"Net pay." example:"4000.00"`
	Disburse       string            `json:"disburse" doc:"The amount to disburse." example:"4000.00"`
	EmployerCost   string            `json:"employer_cost" doc:"Gross + employer CPF + SDL." example:"5861.25"`
	Items          []RunItem         `json:"items" doc:"The pay items."`
	Working        []RunStep         `json:"working" doc:"The steps of the calculation."`
	Flags          []RunFlag         `json:"flags" doc:"What the preparer or the approver must see."`
	Overridden     []string          `json:"overridden,omitempty" doc:"The figures an override replaced."`
	Computed       map[string]string `json:"computed,omitempty" doc:"The figures the engine worked out, where an override replaced one."`
}

// RunTotals are a version's totals, kept apart (FR-PAY-18).
type RunTotals struct {
	Gross          string `json:"gross" example:"5000.00" doc:"Gross pay."`
	CPFEmployee    string `json:"cpf_employee" example:"1000.00" doc:"Employee CPF."`
	CPFEmployer    string `json:"cpf_employer" example:"850.00" doc:"Employer CPF."`
	SDLPayable     string `json:"sdl_payable" example:"11.00" doc:"The employer-month SDL, rounded down to the dollar."`
	SHG            string `json:"shg" example:"0.00" doc:"Self-help group funds."`
	Deductions     string `json:"deductions" example:"0.00" doc:"Authorised deductions."`
	Reimbursements string `json:"reimbursements" example:"0.00" doc:"Reimbursements paid with the salary."`
	Net            string `json:"net" example:"4000.00" doc:"Net pay."`
	Disburse       string `json:"disburse" example:"4000.00" doc:"The amount to disburse."`
	EmployerCost   string `json:"employer_cost" example:"5861.00" doc:"Gross + employer CPF + SDL payable."`
}

// RunVersion is a calculation of a run: read-only once written.
type RunVersion struct {
	ID         string    `json:"id" format:"uuid" doc:"The version." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f41"`
	Number     int       `json:"version" doc:"Its number in the run." example:"1"`
	State      string    `json:"state" enum:"calculated,approved,superseded,posted,paid_in_part,paid,closed" doc:"Where it is." example:"calculated"`
	Digest     string    `json:"digest" doc:"The SHA-256 of its figures, which an approver confirms." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	PreparedBy string    `json:"prepared_by" format:"uuid" doc:"The member who calculated it." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e45"`
	PreparedAt string    `json:"prepared_at" format:"date-time" doc:"When." example:"2026-11-20T10:00:00Z"`
	ApprovedBy string    `json:"approved_by,omitempty" format:"uuid" doc:"The member who approved it, never its preparer." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e46"`
	ApprovedAt string    `json:"approved_at,omitempty" format:"date-time" doc:"When." example:"2026-11-20T11:00:00Z"`
	Reason     string    `json:"reason,omitempty" doc:"Why a correction was made." example:"A bonus was missed."`
	People     int       `json:"people" doc:"How many people it pays." example:"12"`
	Failing    int       `json:"flags_failing" doc:"How many ✖ it has." example:"0"`
	Warning    int       `json:"flags_warning" doc:"How many ⚠ it has." example:"1"`
	Packs      []string  `json:"pack_versions" doc:"The pack versions it read."`
	Totals     RunTotals `json:"totals" doc:"Its totals."`
}

// RunOverride is an override on a run.
type RunOverride struct {
	ID       string `json:"id" format:"uuid" doc:"The override." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f42"`
	PersonID string `json:"person_id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Person   string `json:"person" doc:"Their legal name." example:"Ben Lim"`
	Field    string `json:"field" enum:"cpf_employee,cpf_employer,sdl,shg" doc:"The figure it replaces." example:"cpf_employee"`
	Value    string `json:"value" doc:"The figure it puts in place." example:"999.00"`
	Reason   string `json:"reason" doc:"Why." example:"The CPF Board's own figure for the month."`
}

// Run is a month's run for a pay group.
type Run struct {
	ID      string     `json:"id" format:"uuid" doc:"The run." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f43"`
	Month   string     `json:"month" format:"date" doc:"The wage month." example:"2026-06-01"`
	GroupID string     `json:"pay_group_id" format:"uuid" doc:"The pay group." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f30"`
	Group   string     `json:"pay_group" doc:"Its name." example:"Monthly"`
	PayDay  string     `json:"pay_day" format:"date" doc:"The pay day." example:"2026-06-25"`
	Latest  RunVersion `json:"latest" doc:"The newest version."`
}

// RunDetail is a run with its newest version's lines, its versions and its overrides.
type RunDetail struct {
	Run
	Lines     []RunLine     `json:"lines" doc:"The newest version's lines, one for each person."`
	Versions  []RunVersion  `json:"versions" doc:"Every version, newest first."`
	Overrides []RunOverride `json:"overrides" doc:"The overrides in force on the run."`
}

// Payroll is pay records behind the API.
type Payroll interface {
	StartRun(ctx context.Context, s Session, month time.Time, group uuid.UUID) (Run, error)
	Runs(ctx context.Context, s Session, month time.Time) ([]Run, error)
	RunDetail(ctx context.Context, s Session, id uuid.UUID) (RunDetail, error)
	Recalculate(ctx context.Context, s Session, id uuid.UUID, reason string) (Run, error)
	AddOverride(ctx context.Context, s Session, run, person uuid.UUID, field, value, reason string) (string, error)
	WithdrawOverride(ctx context.Context, s Session, id uuid.UUID) error
	Groups(ctx context.Context, s Session) ([]PayGroup, error)
	CreateGroup(ctx context.Context, s Session, in PayGroupInput) (string, error)
	Pay(ctx context.Context, s Session, person uuid.UUID, on time.Time) (PayView, bool, error)
	SetPay(ctx context.Context, s Session, person uuid.UUID, from time.Time, in PersonPayInput) error
	SetElection(ctx context.Context, s Session, person uuid.UUID, from time.Time, evidence uuid.UUID) error
	ImportYTD(ctx context.Context, s Session, person uuid.UUID, in YTDInput) error
	OneOffs(ctx context.Context, s Session, month time.Time) ([]OneOff, error)
	AddOneOff(ctx context.Context, s Session, person uuid.UUID, month time.Time, in OneOffInput) (string, error)
}

func init() {
	IdempotencyExempt["POST /v1/payroll/runs"] = "a month has one run for a pay group: a repeat is refused (NM_PAYROLL_CONFLICT)"
	IdempotencyExempt["POST /v1/payroll/runs/{id}/versions"] = "each calculation is its own version of the run, with its own digest"
	IdempotencyExempt["POST /v1/payroll/runs/{id}/overrides"] = "each override is its own record with its own reason: a repeat replaces nothing and the next calculation applies both in order"
	IdempotencyExempt["POST /v1/payroll/overrides/{id}/withdrawal"] = "an override is withdrawn once: a repeat is refused (NM_NOT_FOUND)"
	IdempotencyExempt["POST /v1/payroll/groups"] = "a pay group's name is used once: a repeat is refused (NM_PAYROLL_CONFLICT)"
	IdempotencyExempt["POST /v1/people/{id}/pay"] = "a repeat starts a record on the current one's own start day, which is refused (NM_PAYROLL_CONFLICT)"
	IdempotencyExempt["POST /v1/people/{id}/pr-election"] = "a repeat starts an election on the current one's own start day, which is refused (NM_PAYROLL_CONFLICT)"
	IdempotencyExempt["POST /v1/people/{id}/pay/year-to-date"] = "a year's year to date is imported once: a repeat is refused (NM_PAYROLL_CONFLICT)"
	IdempotencyExempt["POST /v1/people/{id}/pay/one-offs"] = "an item's key is used once for a person and month: a repeat is refused (NM_PAYROLL_CONFLICT)"
}

func payrollError(err error) error {
	var r *payroll.Refusal
	var p *people.Refusal
	switch {
	case errors.As(err, &r):
		return Err(r.Code, r.Message)
	case errors.As(err, &p):
		return Err(p.Code, p.Message)
	}
	return huma.Error500InternalServerError("The pay could not be handled.")
}

func payrollErrorOrNil(err error) error {
	if err == nil {
		return nil
	}
	return payrollError(err)
}

type payIDInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
}

type payGetInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	AsOf   string `query:"as_of" format:"date" required:"false" doc:"The day to show the pay on; today when empty." example:"2026-06-01"`
}

type paySetInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Body   struct {
		From string         `json:"effective_from" format:"date" doc:"The day the new pay record starts; the current one ends that day." example:"2026-07-01"`
		Pay  PersonPayInput `json:"pay" doc:"The pay record."`
	}
}

type electionInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Body   struct {
		From     string `json:"effective_from" format:"date" doc:"The day the election starts." example:"2026-02-10"`
		Evidence string `json:"evidence_document_id" format:"uuid" doc:"A document on the person's file: the CPF Board's approval of the joint application." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ef9"`
	}
}

type ytdInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Body   YTDInput
}

type groupsOutput struct {
	Body struct {
		Groups []PayGroup `json:"groups" doc:"The pay groups by name."`
	}
}

type groupCreateInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Body   PayGroupInput
}

type payOutput struct {
	Body struct {
		Recorded bool    `json:"recorded" doc:"Whether the person has pay on the day." example:"true"`
		Pay      PayView `json:"pay" doc:"The pay."`
	}
}

type oneOffsInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Month  string `query:"month" format:"date" doc:"The wage month, the first day." example:"2026-06-01"`
}

type oneOffsOutput struct {
	Body struct {
		Items []OneOff `json:"items" doc:"The month's one-off items."`
	}
}

type oneOffAddInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Body   struct {
		Month string      `json:"month" format:"date" doc:"The wage month, the first day." example:"2026-06-01"`
		Item  OneOffInput `json:"item" doc:"The item."`
	}
}

func (a *API) registerPayroll() {
	a.registerPayRuns()
	// payroller is the signed-in Payroll administrator or Owner: pay is theirs alone (Appendix B: Pay, pay history, pay runs).
	payroller := func(ctx context.Context, cookie string) (Session, error) {
		s, err := a.signedIn(ctx, cookie)
		if err != nil {
			return s, err
		}
		p, err := a.letterPowers(ctx, s)
		if err != nil {
			return s, err
		}
		if !p.Pay {
			return s, Err(CodeRoleRequired, "Only the Payroll administrator and the Owner see and change pay.")
		}
		return s, nil
	}
	person := func(id string) (uuid.UUID, error) {
		pid, err := uuid.Parse(id)
		if err != nil {
			return uuid.Nil, Err(CodePersonNotFound, "There is no such person in this organisation.")
		}
		return pid, nil
	}
	huma.Register(a.API, huma.Operation{
		OperationID: "listPayGroups", Method: "GET", Path: "/v1/payroll/groups", Tags: []string{"Payroll"}, Summary: "Pay groups",
		Description: "The pay groups: each has a pay day and the cut-offs for inputs and approval (NM-PAY-03). The Payroll administrator and the Owner. It changes nothing.", Errors: []int{401, 403},
	}, func(ctx context.Context, in *lettersInput) (*groupsOutput, error) {
		s, err := payroller(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		list, err := a.d.Payroll.Groups(ctx, s)
		if err != nil {
			return nil, payrollError(err)
		}
		out := &groupsOutput{}
		out.Body.Groups = list
		if list == nil {
			out.Body.Groups = []PayGroup{}
		}
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "createPayGroup", Method: "POST", Path: "/v1/payroll/groups", Tags: []string{"Payroll"}, Summary: "Make a pay group",
		Description: "Makes a pay group with its pay day and the cut-offs for inputs and approval. The Payroll administrator and the Owner. It is audited.", Errors: []int{401, 403, 409, 422}, DefaultStatus: 201,
	}, func(ctx context.Context, in *groupCreateInput) (*createdOutput, error) {
		s, err := payroller(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, err := a.d.Payroll.CreateGroup(ctx, s, in.Body)
		if err != nil {
			return nil, payrollError(err)
		}
		out := &createdOutput{}
		out.Body.ID = id
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "getPersonPay", Method: "GET", Path: "/v1/people/{id}/pay", Tags: []string{"Payroll"}, Summary: "A person's pay",
		Description: "The person's pay record on a day: the pay group, the monthly basic salary, the fund, the allowances and fixed deductions, whether a joint election is in force and the year to date. Pay is for the Payroll administrator and the Owner alone: anyone else is refused, and every read is of one person. It changes nothing.", Errors: []int{401, 403, 404},
	}, func(ctx context.Context, in *payGetInput) (*payOutput, error) {
		s, err := payroller(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		pid, err := person(in.ID)
		if err != nil {
			return nil, err
		}
		on, err := a.asOf(in.AsOf)
		if err != nil {
			return nil, err
		}
		v, ok, err := a.d.Payroll.Pay(ctx, s, pid, on)
		if err != nil {
			return nil, payrollError(err)
		}
		out := &payOutput{}
		out.Body.Recorded, out.Body.Pay = ok, v
		if v.Items == nil {
			out.Body.Pay.Items = []PayItem{}
		}
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "setPersonPay", Method: "POST", Path: "/v1/people/{id}/pay", Tags: []string{"Payroll"}, Summary: "Start a person's pay record",
		Description: "Ends the current pay record the day the new one starts and starts it, with its allowances and fixed deductions. A record is history: it is ended, never edited. The Payroll administrator and the Owner. It is audited, and the audit event says that pay was set and from when, never what it was.", Errors: []int{401, 403, 404, 409, 422}, DefaultStatus: 201,
	}, func(ctx context.Context, in *paySetInput) (*struct{}, error) {
		s, err := payroller(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		pid, err := person(in.ID)
		if err != nil {
			return nil, err
		}
		from, err := date("effective_from", in.Body.From, true)
		if err != nil {
			return nil, err
		}
		return nil, payrollErrorOrNil(a.d.Payroll.SetPay(ctx, s, pid, *from, in.Body.Pay))
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "setPersonElection", Method: "POST", Path: "/v1/people/{id}/pr-election", Tags: []string{"Payroll"}, Summary: "Record a permanent resident's joint election",
		Description: "Records that the CPF Board approved a joint application to contribute at full employer rates in the first two years of permanent residence, from a day, with the approval as a document on the person's file. The Payroll administrator and the Owner. It is audited.", Errors: []int{401, 403, 404, 409, 422}, DefaultStatus: 201,
	}, func(ctx context.Context, in *electionInput) (*struct{}, error) {
		s, err := payroller(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		pid, err := person(in.ID)
		if err != nil {
			return nil, err
		}
		from, err := date("effective_from", in.Body.From, true)
		if err != nil {
			return nil, err
		}
		ev, err := uuid.Parse(in.Body.Evidence)
		if err != nil {
			return nil, Err(CodePayrollInvalid, "evidence_document_id is not a document ID.")
		}
		return nil, payrollErrorOrNil(a.d.Payroll.SetElection(ctx, s, pid, *from, ev))
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "importYearToDate", Method: "POST", Path: "/v1/people/{id}/pay/year-to-date", Tags: []string{"Payroll"}, Summary: "Import a person's year to date of CPF wages",
		Description: "Imports the CPF wages this employer has counted for the person in a year before Nemura's first run (FR-PAY-14): ordinary wages subject to CPF, additional wages subject and paid, and the last month counted. Once for a year; a run adds to it afterwards. A month that pays additional wages with no year to date cannot be calculated. The Payroll administrator and the Owner. It is audited without the figures.", Errors: []int{401, 403, 404, 409, 422}, DefaultStatus: 201,
	}, func(ctx context.Context, in *ytdInput) (*struct{}, error) {
		s, err := payroller(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		pid, err := person(in.ID)
		if err != nil {
			return nil, err
		}
		return nil, payrollErrorOrNil(a.d.Payroll.ImportYTD(ctx, s, pid, in.Body))
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "listPayOneOffs", Method: "GET", Path: "/v1/payroll/one-offs", Tags: []string{"Payroll"}, Summary: "A month's one-off items",
		Description: "The one-off items (bonuses, commissions, allowances) of a wage month with the facts that class them. The Payroll administrator and the Owner. It changes nothing.", Errors: []int{401, 403, 422},
	}, func(ctx context.Context, in *oneOffsInput) (*oneOffsOutput, error) {
		s, err := payroller(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		month, err := date("month", in.Month, true)
		if err != nil {
			return nil, err
		}
		list, err := a.d.Payroll.OneOffs(ctx, s, *month)
		if err != nil {
			return nil, payrollError(err)
		}
		out := &oneOffsOutput{}
		out.Body.Items = list
		if list == nil {
			out.Body.Items = []OneOff{}
		}
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "addPayOneOff", Method: "POST", Path: "/v1/people/{id}/pay/one-offs", Tags: []string{"Payroll"}, Summary: "Add a one-off item to a month's pay",
		Description: "Adds a one-off item to the wage month with the month of employment it is for and the day it is payable, which decide whether it is ordinary or additional wages (FR-PAY-17). The Payroll administrator and the Owner. It is audited without the amount.", Errors: []int{401, 403, 404, 409, 422}, DefaultStatus: 201,
	}, func(ctx context.Context, in *oneOffAddInput) (*createdOutput, error) {
		s, err := payroller(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		pid, err := person(in.ID)
		if err != nil {
			return nil, err
		}
		month, err := date("month", in.Body.Month, true)
		if err != nil {
			return nil, err
		}
		id, err := a.d.Payroll.AddOneOff(ctx, s, pid, *month, in.Body.Item)
		if err != nil {
			return nil, payrollError(err)
		}
		out := &createdOutput{}
		out.Body.ID = id
		return out, nil
	})
}

type runStartInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Body   struct {
		Month string `json:"month" format:"date" doc:"The wage month, the first day." example:"2026-06-01"`
		Group string `json:"pay_group_id" format:"uuid" doc:"The pay group." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f30"`
	}
}

type runOutput struct{ Body Run }

type runsOutput struct {
	Body struct {
		Runs []Run `json:"runs" doc:"The month's runs."`
	}
}

type runDetailOutput struct{ Body RunDetail }

type runIDInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The run." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f43"`
}

type runVersionInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The run." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f43"`
	Body   struct {
		Reason string `json:"reason,omitempty" maxLength:"500" doc:"Why: required for a correction of an approved run." example:"A bonus was missed."`
	}
}

type runOverrideInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The run." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f43"`
	Body   struct {
		Person string `json:"person_id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
		Field  string `json:"field" enum:"cpf_employee,cpf_employer,sdl,shg" doc:"The figure to replace." example:"cpf_employee"`
		Value  string `json:"value" pattern:"^[0-9]{1,14}(\\.[0-9]{1,2})?$" doc:"The figure to put in place, in dollars." example:"999.00"`
		Reason string `json:"reason" minLength:"10" maxLength:"500" doc:"Why the calculated figure is wrong." example:"The CPF Board's own figure for the month."`
	}
}

type overrideWithdrawInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The override." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f42"`
}

func (a *API) registerPayRuns() {
	payroller := func(ctx context.Context, cookie string) (Session, error) {
		s, err := a.signedIn(ctx, cookie)
		if err != nil {
			return s, err
		}
		p, err := a.letterPowers(ctx, s)
		if err != nil {
			return s, err
		}
		if !p.Pay {
			return s, Err(CodeRoleRequired, "Only the Payroll administrator and the Owner see and change pay.")
		}
		return s, nil
	}
	huma.Register(a.API, huma.Operation{
		OperationID: "startPayRun", Method: "POST", Path: "/v1/payroll/runs", Tags: []string{"Payroll"}, Summary: "Start a pay run and calculate its first version",
		Description: "Makes the run of a wage month for a pay group, once, and calculates version 1: every person with pay in the group, from their pay record and the inputs (one-off items, unpaid leave, approved overtime), each linked to its source. A person's missing fact (a legal classification, a date of birth, a year to date for additional wages) refuses the run and names them. The version is read-only; nothing is posted or paid. The Payroll administrator and the Owner. It is audited without any amount.", Errors: []int{401, 403, 409, 422}, DefaultStatus: 201,
	}, func(ctx context.Context, in *runStartInput) (*runOutput, error) {
		s, err := payroller(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		month, err := date("month", in.Body.Month, true)
		if err != nil {
			return nil, err
		}
		g, err := uuid.Parse(in.Body.Group)
		if err != nil {
			return nil, Err(CodePayrollInvalid, "pay_group_id is not a pay group ID.")
		}
		r, err := a.d.Payroll.StartRun(ctx, s, *month, g)
		if err != nil {
			return nil, payrollError(err)
		}
		return &runOutput{Body: r}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "listPayRuns", Method: "GET", Path: "/v1/payroll/runs", Tags: []string{"Payroll"}, Summary: "A month's pay runs",
		Description: "The runs of a wage month, each with its newest version's state, digest, totals and flags. The Payroll administrator and the Owner. It changes nothing.", Errors: []int{401, 403, 422},
	}, func(ctx context.Context, in *oneOffsInput) (*runsOutput, error) {
		s, err := payroller(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		month, err := date("month", in.Month, true)
		if err != nil {
			return nil, err
		}
		list, err := a.d.Payroll.Runs(ctx, s, *month)
		if err != nil {
			return nil, payrollError(err)
		}
		out := &runsOutput{}
		out.Body.Runs = list
		if list == nil {
			out.Body.Runs = []Run{}
		}
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "getPayRun", Method: "GET", Path: "/v1/payroll/runs/{id}", Tags: []string{"Payroll"}, Summary: "A pay run with its newest version",
		Description: "The run with its newest version's lines (each person's figures, items, flags and working), every version and the overrides in force. The Payroll administrator and the Owner. It changes nothing.", Errors: []int{401, 403, 404},
	}, func(ctx context.Context, in *runIDInput) (*runDetailOutput, error) {
		s, err := payroller(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such pay run.")
		}
		d, err := a.d.Payroll.RunDetail(ctx, s, id)
		if err != nil {
			return nil, payrollError(err)
		}
		if d.Lines == nil {
			d.Lines = []RunLine{}
		}
		if d.Overrides == nil {
			d.Overrides = []RunOverride{}
		}
		return &runDetailOutput{Body: d}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "calculatePayRunVersion", Method: "POST", Path: "/v1/payroll/runs/{id}/versions", Tags: []string{"Payroll"}, Summary: "Calculate the next version of a pay run",
		Description: "Calculates the next version from the pay records, the inputs and the overrides now: a calculated version that is not approved is superseded; after an approved version it is a correction, which needs a reason and states the full corrected figures of its month. The Payroll administrator and the Owner. It is audited without any amount.", Errors: []int{401, 403, 404, 409, 422}, DefaultStatus: 201,
	}, func(ctx context.Context, in *runVersionInput) (*runOutput, error) {
		s, err := payroller(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such pay run.")
		}
		r, err := a.d.Payroll.Recalculate(ctx, s, id, in.Body.Reason)
		if err != nil {
			return nil, payrollError(err)
		}
		return &runOutput{Body: r}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "addPayRunOverride", Method: "POST", Path: "/v1/payroll/runs/{id}/overrides", Tags: []string{"Payroll"}, Summary: "Override a calculated figure of a person's month",
		Description: "Replaces the CPF employee or employer share, the SDL or the self-help group fund of one person's month with a figure and a reason of at least ten characters. It applies from the next calculation, is flagged ⚠ on the approval and is part of the version's digest. The Payroll administrator and the Owner. It is audited with the field and the person, never the figure.", Errors: []int{401, 403, 404, 422}, DefaultStatus: 201,
	}, func(ctx context.Context, in *runOverrideInput) (*createdOutput, error) {
		s, err := payroller(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such pay run.")
		}
		pid, err := uuid.Parse(in.Body.Person)
		if err != nil {
			return nil, Err(CodePersonNotFound, "There is no such person in this organisation.")
		}
		oid, err := a.d.Payroll.AddOverride(ctx, s, id, pid, in.Body.Field, in.Body.Value, in.Body.Reason)
		if err != nil {
			return nil, payrollError(err)
		}
		out := &createdOutput{}
		out.Body.ID = oid
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "withdrawPayRunOverride", Method: "POST", Path: "/v1/payroll/overrides/{id}/withdrawal", Tags: []string{"Payroll"}, Summary: "Withdraw an override",
		Description: "Ends an override from the next calculation. The Payroll administrator and the Owner. It is audited.", Errors: []int{401, 403, 404}, DefaultStatus: 204,
	}, func(ctx context.Context, in *overrideWithdrawInput) (*struct{}, error) {
		s, err := payroller(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such override that is still in force.")
		}
		return nil, payrollErrorOrNil(a.d.Payroll.WithdrawOverride(ctx, s, id))
	})
}
