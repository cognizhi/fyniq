package api

import (
	"context"
	"encoding/base64"
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
	Nature    string `json:"nature" enum:"bonus,commission,allowance,other,leave_encashment,notice_pay,retrenchment_benefit" doc:"What it is." example:"bonus"`
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
	Nature    string `json:"nature" enum:"bonus,commission,allowance,other,leave_encashment,notice_pay,retrenchment_benefit" doc:"What it is." example:"bonus"`
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

// RunChange is one difference from last month with its cause.
type RunChange struct {
	PersonID   string `json:"person_id" doc:"The person's ID, or the person for a change that has none." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Person     string `json:"person" doc:"Their legal name." example:"Ben Lim"`
	Cause      string `json:"cause" enum:"joiner,leaver,pay_change,unpaid_leave,overtime,one_off,allowance,deduction,cpf" doc:"What caused it." example:"one_off"`
	Item       string `json:"item,omitempty" doc:"The pay item it comes from." example:"bonus-2026"`
	SourceType string `json:"source_type,omitempty" doc:"The kind of record the item came from." example:"pay_one_off"`
	Amount     string `json:"amount" doc:"More or less than last month, in dollars; a joiner's or a leaver's whole gross pay." example:"5000.00"`
}

// RunUnexplained is a difference with no input behind it.
type RunUnexplained struct {
	PersonID string `json:"person_id" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Person   string `json:"person" doc:"Their legal name." example:"Ben Lim"`
	Why      string `json:"why" doc:"What to check." example:"CPF changed with no change in wages."`
}

// RunChanges is the run against the previous month's approved run (NM-PAY-11).
type RunChanges struct {
	HasPrevious   bool             `json:"has_previous" doc:"Whether the pay group has an approved run for the month before." example:"true"`
	GrossNow      string           `json:"gross_now,omitempty" doc:"This run's gross pay." example:"10000.00"`
	GrossPrevious string           `json:"gross_previous,omitempty" doc:"Last month's." example:"5000.00"`
	NetNow        string           `json:"net_now,omitempty" doc:"This run's net pay." example:"8000.00"`
	NetPrevious   string           `json:"net_previous,omitempty" doc:"Last month's." example:"4000.00"`
	CostNow       string           `json:"employer_cost_now,omitempty" doc:"This run's employer cost." example:"11000.00"`
	CostPrevious  string           `json:"employer_cost_previous,omitempty" doc:"Last month's." example:"5500.00"`
	Changes       []RunChange      `json:"changes" doc:"Every difference with its cause."`
	Unexplained   []RunUnexplained `json:"unexplained" doc:"What has no input behind it: for the approver to check."`
}

// PayslipItem is a named amount of a payslip.
type PayslipItem struct {
	Name   string `json:"name" doc:"As the payslip shows it." example:"Transport allowance"`
	Amount string `json:"amount" doc:"Dollars." example:"100.00"`
}

// PayslipSummary is a payslip on the person's list.
type PayslipSummary struct {
	ID        string `json:"id" format:"uuid" doc:"The payslip." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f50"`
	Month     string `json:"month" format:"date" doc:"The wage month." example:"2026-06-01"`
	PayDate   string `json:"payment_date" format:"date" doc:"The date of payment." example:"2026-06-25"`
	Reference string `json:"reference" doc:"Its reference." example:"PS-202606-0001"`
	Net       string `json:"net_salary" doc:"The net salary paid." example:"4000.00"`
	Version   int    `json:"version" doc:"The version of the pay run it was made from." example:"1"`
	Corrected bool   `json:"corrected" doc:"True when a corrected version of the month has a payslip of its own: this one is superseded." example:"false"`
}

// PayslipView is a payslip with MOM's twelve items.
type PayslipView struct {
	PayslipSummary
	SHA256         string        `json:"sha256" doc:"The SHA-256 of the exact PDF." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	Employer       string        `json:"employer" doc:"Item 1: the full name of the employer." example:"Acme Pte. Ltd."`
	Employee       string        `json:"employee" doc:"Item 2: the full name of the employee." example:"Ben Lim"`
	Basic          string        `json:"basic_salary" doc:"Item 4: the basic salary." example:"4500.00"`
	PeriodStart    string        `json:"period_start" format:"date" doc:"Item 5: the start of the salary period." example:"2026-06-01"`
	PeriodEnd      string        `json:"period_end" format:"date" doc:"Item 5: the end of the salary period." example:"2026-06-30"`
	Allowances     []PayslipItem `json:"allowances" doc:"Item 6: fixed and ad-hoc allowances."`
	Additional     []PayslipItem `json:"additional_payments" doc:"Item 7: bonuses, rest day pay, public holiday pay and other additional payments."`
	Deductions     []PayslipItem `json:"deductions" doc:"Item 8: fixed deductions (the employee's CPF) and ad-hoc deductions (no-pay leave, absence)."`
	OvertimeHours  string        `json:"overtime_hours,omitempty" doc:"Item 9: overtime hours worked." example:"3"`
	OvertimePay    string        `json:"overtime_pay,omitempty" doc:"Item 10: overtime pay." example:"106.21"`
	OvertimePeriod string        `json:"overtime_period,omitempty" doc:"Item 11: the overtime payment period, only if different from the salary period." example:"2026-06-01 to 2026-06-30"`
	Reimbursements string        `json:"reimbursements,omitempty" doc:"Reimbursements paid with the salary, which are not part of it." example:"45.00"`
}

// PaymentEvent is one change of a person's payment.
type PaymentEvent struct {
	Seq           int    `json:"seq" doc:"The event's number among the payment's events." example:"1"`
	State         string `json:"state" enum:"scheduled,released,sent,accepted,paid,rejected,returned,unknown" doc:"The state it moved the payment to." example:"paid"`
	InstructionID string `json:"instruction_id,omitempty" doc:"The payment instruction, from Fyniq." example:"INSTR-2026-06-0001"`
	Amount        string `json:"amount" doc:"The amount in dollars." example:"3963.21"`
	PaidOn        string `json:"date" format:"date" doc:"The date of the payment." example:"2026-06-25"`
	Source        string `json:"source" enum:"fyniq,manual" doc:"Fyniq's status event, or a person with evidence." example:"fyniq"`
	EvidenceID    string `json:"evidence_id,omitempty" format:"uuid" doc:"The document of the person paid that evidences a manual record." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f60"`
	Note          string `json:"note,omitempty" doc:"A note of up to 500 characters." example:"Seen on the 25 June statement."`
	RecordedAt    string `json:"recorded_at" format:"date-time" doc:"When it was recorded." example:"2026-06-25T10:00:00Z"`
}

// PaymentLine is a person's payment in a run.
type PaymentLine struct {
	PersonID       string         `json:"person_id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e45"`
	Person         string         `json:"person" doc:"Their name." example:"Ben Lim"`
	Owed           string         `json:"owed" doc:"The amount to pay, in dollars." example:"3963.21"`
	State          string         `json:"state,omitempty" doc:"The latest state; empty while nothing is recorded." example:"paid"`
	Attention      bool           `json:"attention" doc:"True when the payment was rejected, returned or has an unknown outcome: ✖ on Checks, and an unknown outcome blocks a new payment." example:"false"`
	AmountMismatch bool           `json:"amount_mismatch" doc:"True when it was confirmed paid for an amount other than the one owed." example:"false"`
	Events         []PaymentEvent `json:"events" doc:"Every change, oldest first."`
}

// RunPayments is a run's payments.
type RunPayments struct {
	Version int           `json:"version" minimum:"1" doc:"The approved version being paid." example:"2"`
	State   string        `json:"state" enum:"approved,paid in part,paid" doc:"The run's payment state from its people's." example:"approved"`
	Lines   []PaymentLine `json:"lines" doc:"Each person owed money in the version."`
}

// PaymentRecord is what a person records of a payment's outcome.
type PaymentRecord struct {
	PersonID   string `json:"person_id" format:"uuid" doc:"The person paid." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e45"`
	State      string `json:"state" enum:"scheduled,released,sent,accepted,paid,rejected,returned,unknown" doc:"The new state; only a legal move after the last one." example:"paid"`
	Amount     string `json:"amount" pattern:"^[0-9]+(\\.[0-9]{1,2})?$" doc:"The amount in dollars and cents." example:"3963.21"`
	PaidOn     string `json:"date" format:"date" doc:"The date of the payment; not in the future for paid, rejected or returned." example:"2026-06-25"`
	EvidenceID string `json:"evidence_id" format:"uuid" doc:"A document of the person paid that shows the outcome, such as a bank statement line. Required: a bare word is never enough." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f60"`
	Note       string `json:"note,omitempty" maxLength:"500" doc:"A note." example:"Seen on the 25 June statement."`
}

// TaxHoldView is a person's tax-clearance hold.
type TaxHoldView struct {
	Held    bool   `json:"held" doc:"True while a hold is in force: every dollar due to the person in a pay run is held and none is disbursed." example:"false"`
	Version int    `json:"version,omitempty" doc:"The latest version of the hold; Fyniq is told each version." example:"1"`
	From    string `json:"effective_from,omitempty" format:"date" doc:"The day the latest version took effect." example:"2026-10-15"`
	Reason  string `json:"reason,omitempty" doc:"The reason recorded." example:"IR21 filed on 15 Oct 2026."`
}

// TaxHoldInput puts a hold on or releases it.
type TaxHoldInput struct {
	Action     string `json:"action" enum:"hold,release" doc:"Put a hold on, or release the one in force." example:"hold"`
	From       string `json:"effective_from" format:"date" doc:"The day it takes effect." example:"2026-10-15"`
	EvidenceID string `json:"evidence_id,omitempty" format:"uuid" doc:"A document of the person that shows IRAS's clearance: required to release." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f60"`
	Reason     string `json:"reason,omitempty" maxLength:"300" doc:"A reason." example:"IR21 filed on 15 Oct 2026."`
}

// FinalPay is a leaver's final pay: when it is due, what leave is left to pay, the hold and the final pay items already added.
type FinalPay struct {
	PersonID     string        `json:"person_id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Person       string        `json:"person" doc:"Their legal name." example:"Ben Lim"`
	Kind         string        `json:"kind" doc:"How they leave." example:"resignation"`
	LastDay      string        `json:"last_day" format:"date" doc:"The last day of employment." example:"2026-11-04"`
	NoticeServed bool          `json:"notice_served" doc:"Whether notice was given: a resignation with it is due on the last day, without it within 7 days. The required notice period is not compared with the contract." example:"true"`
	Due          string        `json:"due" format:"date" doc:"When the final salary is due." example:"2026-11-04"`
	Latest       string        `json:"latest" format:"date" doc:"The latest day it may be paid under MOM's rule for the case." example:"2026-11-10"`
	Rule         string        `json:"rule" doc:"The rule, in words, with the pack version it came from." example:"On the last day, or within 3 working days of it."`
	Month        string        `json:"month" format:"date" doc:"The wage month of the final run: the month of the last day." example:"2026-11-01"`
	UntakenDays  string        `json:"untaken_days,omitempty" doc:"Annual leave left to the last day, in days, negative when more was taken than earned." example:"3.00"`
	LeavePay     string        `json:"proposed_leave_pay,omitempty" doc:"What the untaken leave is worth at the day rate of an unpaid-leave deduction (basic monthly pay over the month's working days, RND-PAY-RATIO); empty when none is untaken or the person has no pay record. A proposal to add as an item, never added by itself." example:"692.31"`
	Hold         TaxHoldView   `json:"hold" doc:"The tax-clearance hold."`
	Items        []FinalPayRow `json:"items" doc:"The final pay items already added to the month."`
}

// FinalPayRow is a final pay item.
type FinalPayRow struct {
	ID     string `json:"id" format:"uuid" doc:"The item." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f31"`
	Name   string `json:"name" doc:"As the payslip shows it." example:"Leave pay on leaving"`
	Nature string `json:"nature" doc:"Leave pay, pay in lieu of notice or a retrenchment benefit." example:"leave_encashment"`
	Amount string `json:"amount" doc:"Dollars." example:"692.31"`
	Used   bool   `json:"used" doc:"Whether a run used it." example:"false"`
}

// ExportSkipped is a person left out of a payment file.
type ExportSkipped struct {
	PersonID string `json:"person_id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e45"`
	Person   string `json:"person" doc:"Their name." example:"Ben Lim"`
	Reason   string `json:"reason" doc:"Why they are not in the file." example:"No salary bank account is on record."`
}

// ExportFile is a hand-off file with its manifest.
type ExportFile struct {
	ID            string          `json:"id" format:"uuid" doc:"The manifest's ID." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f70"`
	Kind          string          `json:"kind" enum:"journal,payments" doc:"The journal file or the bank payment file." example:"payments"`
	Version       int             `json:"version" minimum:"1" doc:"The approved version it was made from." example:"2"`
	Month         string          `json:"wage_month" format:"date" doc:"The wage month." example:"2026-06-01"`
	Lines         int             `json:"lines" doc:"How many lines the file has." example:"12"`
	Total         string          `json:"total" doc:"The payment file's total in dollars, or the journal's debit total." example:"42000.00"`
	SHA256        string          `json:"sha256" doc:"The SHA-256 of the exact bytes handed out; kept in the manifest." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	Note          string          `json:"note" doc:"Always says the file is not proof of payment." example:"This file is not proof of payment."`
	Reference     string          `json:"reference,omitempty" doc:"The payment reference in a payment file." example:"Salary 2026-06"`
	Skipped       []ExportSkipped `json:"skipped" doc:"The people left out of a payment file, with why."`
	ContentBase64 string          `json:"content_base64" doc:"The CSV, base64-encoded. It is not kept: only its manifest is."`
}

// ExportInput asks for a file.
type ExportInput struct {
	Version int    `json:"version" minimum:"1" doc:"The approved version to make the file from." example:"2"`
	Kind    string `json:"kind" enum:"journal,payments" doc:"The journal file or the bank payment file." example:"payments"`
}

// Payroll is pay records behind the API.
type Payroll interface {
	Export(ctx context.Context, s Session, run uuid.UUID, in ExportInput) (ExportFile, error)
	FinalPayOf(ctx context.Context, s Session, person uuid.UUID) (FinalPay, bool, error)
	TaxHold(ctx context.Context, s Session, person uuid.UUID) (TaxHoldView, error)
	SetTaxHold(ctx context.Context, s Session, person uuid.UUID, in TaxHoldInput) error
	Payments(ctx context.Context, s Session, run uuid.UUID) (RunPayments, error)
	RecordPayment(ctx context.Context, s Session, run uuid.UUID, in PaymentRecord) error
	PublishPayslips(ctx context.Context, s Session, run uuid.UUID, version int) (int, error)
	MyPayslips(ctx context.Context, s Session) ([]PayslipSummary, error)
	MyNextPayDay(ctx context.Context, s Session) (string, error)
	MyPayslip(ctx context.Context, s Session, id uuid.UUID) (PayslipView, error)
	MyPayslipFile(ctx context.Context, s Session, id uuid.UUID) (reference string, pdf []byte, err error)
	Approve(ctx context.Context, s Session, id uuid.UUID, version int, digest string) error
	Changes(ctx context.Context, s Session, id uuid.UUID) (RunChanges, error)
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
	IdempotencyExempt["POST /v1/payroll/runs/{id}/approval"] = "a version is approved once: a repeat is refused (NM_PAYROLL_CONFLICT)"
	IdempotencyExempt["POST /v1/payroll/runs/{id}/payslips"] = "a version's payslips are published once: a repeat is refused (NM_PAYROLL_CONFLICT)"
	IdempotencyExempt["POST /v1/payroll/runs/{id}/payments"] = "a payment's state moves one legal step at a time: a repeat of the same state is refused (NM_PAYROLL_CONFLICT)"
	IdempotencyExempt["POST /v1/people/{id}/tax-hold"] = "a hold is put on once and released once: a repeat is refused (NM_PAYROLL_CONFLICT)"
	IdempotencyExempt["POST /v1/payroll/runs/{id}/exports"] = "each request is a file with a manifest of its own: a repeat is another file and another manifest, never a second payment"
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

type runApprovalInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The run." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f43"`
	Body   struct {
		Version int    `json:"version" minimum:"1" doc:"The version approved: the newest, while it is calculated." example:"2"`
		Digest  string `json:"digest" pattern:"^[0-9a-f]{64}$" doc:"The digest of the version as the approver read it." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	}
}

type runChangesOutput struct{ Body RunChanges }

type runPaymentsOutput struct{ Body RunPayments }

type exportInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The run." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f43"`
	Body   ExportInput
}

type exportOutput struct{ Body ExportFile }

type finalPayOutput struct{ Body FinalPay }

type taxHoldOutput struct{ Body TaxHoldView }

type taxHoldInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Body   TaxHoldInput
}

type paymentRecordInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The run." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f43"`
	Body   PaymentRecord
}

type payslipsPublishInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The run." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f43"`
	Body   struct {
		Version int `json:"version" minimum:"1" doc:"The approved version to publish payslips for." example:"1"`
	}
}

type payslipsPublishOutput struct {
	Body struct {
		Published int `json:"published" doc:"How many payslips were made: one for each person of the version." example:"12"`
	}
}

type myPayslipsOutput struct {
	Body struct {
		NextPayDay string           `json:"next_pay_day,omitempty" format:"date" doc:"The next day the person is paid, from their pay group's calendar; left out when they have no pay record in force. No amount." example:"2026-11-25"`
		Payslips   []PayslipSummary `json:"payslips" doc:"The person's payslips, newest month first."`
	}
}

type payslipIDInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The payslip." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f50"`
}

type payslipOutput struct{ Body PayslipView }

type payslipFileOutput struct {
	Body struct {
		Reference     string `json:"reference" doc:"The payslip's reference." example:"PS-202606-0001"`
		ContentType   string `json:"content_type" doc:"Always application/pdf." example:"application/pdf"`
		ContentBase64 string `json:"content_base64" doc:"The PDF, base64-encoded; its SHA-256 is the payslip's."`
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
		OperationID: "getFinalPay", Method: "GET", Path: "/v1/people/{id}/final-pay", Tags: []string{"Payroll"}, Summary: "A leaver's final pay",
		Description: "When the leaver's final salary is due and the latest day to pay it under MOM's rule for the case (resignation with or without the notice served; termination, dismissal or retrenchment), the annual leave left to the last day and what it is worth as a proposal, the tax-clearance hold, and the final pay items already added to the month of the last day. 404 when nobody has recorded the person leaving. The Payroll administrator and the Owner. It changes nothing.", Errors: []int{401, 403, 404},
	}, func(ctx context.Context, in *payIDInput) (*finalPayOutput, error) {
		s, err := payroller(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such person.")
		}
		f, ok, err := a.d.Payroll.FinalPayOf(ctx, s, id)
		if err != nil {
			return nil, payrollError(err)
		}
		if !ok {
			return nil, Err(CodeNotFound, "Nobody has recorded that person leaving: record how and when they leave first.")
		}
		return &finalPayOutput{Body: f}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "getTaxHold", Method: "GET", Path: "/v1/people/{id}/tax-hold", Tags: []string{"Payroll"}, Summary: "A person's tax-clearance hold",
		Description: "Whether a tax-clearance hold (IRAS Form IR21) is in force for the person, with its latest version. The Payroll administrator and the Owner. It changes nothing.", Errors: []int{401, 403, 404},
	}, func(ctx context.Context, in *payIDInput) (*taxHoldOutput, error) {
		s, err := payroller(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such person.")
		}
		h, err := a.d.Payroll.TaxHold(ctx, s, id)
		if err != nil {
			return nil, payrollError(err)
		}
		return &taxHoldOutput{Body: h}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "setTaxHold", Method: "POST", Path: "/v1/people/{id}/tax-hold", Tags: []string{"Payroll"}, Summary: "Put a tax-clearance hold on, or release it",
		Description: "Puts a hold on the person (every dollar due to them in a pay run is held and none is disbursed) or releases the one in force, with a document of the person that shows IRAS's clearance as evidence. Each is a version that is written once; Fyniq is told each. The Payroll administrator and the Owner. It is audited, never with an amount.", Errors: []int{401, 403, 404, 409, 422}, DefaultStatus: 201,
	}, func(ctx context.Context, in *taxHoldInput) (*struct{}, error) {
		s, err := payroller(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such person.")
		}
		if err := a.d.Payroll.SetTaxHold(ctx, s, id, in.Body); err != nil {
			return nil, payrollError(err)
		}
		return nil, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "exportPayRunFile", Method: "POST", Path: "/v1/payroll/runs/{id}/exports", Tags: []string{"Payroll"}, Summary: "Make a hand-off file for an approved version",
		Description: "Makes the standalone journal file (the run's balanced entry, one line for each account) or the bank payment file (one row for each person owed money with a bank account on record) of an approved version, as generic CSV with a manifest of the run, the version, the totals and the SHA-256 of the exact bytes. The file is not kept, only its manifest is. A person is left out of a payment file, and listed with why, when nothing is owed (a tax-clearance hold), no bank account is on record, the payment is on its way or paid, or its outcome is unknown. A file is never proof of payment. The payment file holds bank account numbers, so it needs a passkey confirmation in the last five minutes. The Payroll administrator and the Owner. It is audited, never with an amount or an account.", Errors: []int{401, 403, 404, 409, 422, 503}, DefaultStatus: 201,
	}, func(ctx context.Context, in *exportInput) (*exportOutput, error) {
		s, err := payroller(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if in.Body.Kind == "payments" && !s.Fresh(a.d.Clock.Now()) {
			return nil, Err(CodeStepUp, "Confirm it is you with your passkey, then try again: the payment file holds bank account numbers.")
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such pay run.")
		}
		f, err := a.d.Payroll.Export(ctx, s, id, in.Body)
		if err != nil {
			return nil, payrollError(err)
		}
		return &exportOutput{Body: f}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "getPayRunPayments", Method: "GET", Path: "/v1/payroll/runs/{id}/payments", Tags: []string{"Payroll"}, Summary: "A run's payments",
		Description: "Each person owed money in the run's approved version with the state of their payment and every change to it (scheduled, released, sent, accepted, paid, rejected, returned or unknown), what is owed and what was confirmed, and the run's own payment state (approved, paid in part, paid). The Payroll administrator and the Owner. It changes nothing.", Errors: []int{401, 403, 404, 409},
	}, func(ctx context.Context, in *runIDInput) (*runPaymentsOutput, error) {
		s, err := payroller(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such pay run.")
		}
		p, err := a.d.Payroll.Payments(ctx, s, id)
		if err != nil {
			return nil, payrollError(err)
		}
		return &runPaymentsOutput{Body: p}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "recordPayment", Method: "POST", Path: "/v1/payroll/runs/{id}/payments", Tags: []string{"Payroll"}, Summary: "Record a payment's outcome",
		Description: "Records a change of a person's payment as a person, with a document of the person paid as evidence: a bank statement line or a transfer receipt. 'Paid' is never a bare word, and an exported file or a posted journal is never 'paid'. Only the legal moves after the last state are accepted, and an unknown outcome is resolved as paid, rejected or returned before a new payment can be scheduled. Fyniq's status events arrive through the link and use the same rules. The Payroll administrator and the Owner. It is audited with the state, never the amount.", Errors: []int{401, 403, 404, 409, 422}, DefaultStatus: 201,
	}, func(ctx context.Context, in *paymentRecordInput) (*struct{}, error) {
		s, err := payroller(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such pay run.")
		}
		if err := a.d.Payroll.RecordPayment(ctx, s, id, in.Body); err != nil {
			return nil, payrollError(err)
		}
		return nil, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "publishPayslips", Method: "POST", Path: "/v1/payroll/runs/{id}/payslips", Tags: []string{"Payroll"}, Summary: "Publish a run version's payslips",
		Description: "Makes the itemised payslip of each person of an approved version: MOM's twelve items as data, the PDF drawn from them and its SHA-256, published to each person's My pay. Once for a version; a corrected version has its own. The employer's name is the letterhead's. The Payroll administrator and the Owner. It is audited without any amount.", Errors: []int{401, 403, 404, 409, 422, 503}, DefaultStatus: 201,
	}, func(ctx context.Context, in *payslipsPublishInput) (*payslipsPublishOutput, error) {
		s, err := payroller(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such pay run.")
		}
		n, err := a.d.Payroll.PublishPayslips(ctx, s, id, in.Body.Version)
		if err != nil {
			return nil, payrollError(err)
		}
		out := &payslipsPublishOutput{}
		out.Body.Published = n
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "listMyPayslips", Method: "GET", Path: "/v1/me/payslips", Tags: []string{"Payroll"}, Summary: "My payslips",
		Description: "The signed-in person's own payslips, newest month first; a payslip a corrected version has replaced says so. A leaver's narrow account keeps this. It changes nothing.", Errors: []int{401, 404},
	}, func(ctx context.Context, in *lettersInput) (*myPayslipsOutput, error) {
		s, err := a.signedInOrLeaver(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		list, err := a.d.Payroll.MyPayslips(ctx, s)
		if err != nil {
			return nil, payrollError(err)
		}
		out := &myPayslipsOutput{}
		next, err := a.d.Payroll.MyNextPayDay(ctx, s)
		if err != nil {
			return nil, payrollError(err)
		}
		out.Body.NextPayDay = next
		out.Body.Payslips = list
		if list == nil {
			out.Body.Payslips = []PayslipSummary{}
		}
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "getMyPayslip", Method: "GET", Path: "/v1/me/payslips/{id}", Tags: []string{"Payroll"}, Summary: "One of my payslips",
		Description: "One of the signed-in person's own payslips with MOM's twelve items. Anyone else's is as if it did not exist. A leaver's narrow account keeps this. It changes nothing.", Errors: []int{401, 404},
	}, func(ctx context.Context, in *payslipIDInput) (*payslipOutput, error) {
		s, err := a.signedInOrLeaver(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such payslip of yours.")
		}
		v, err := a.d.Payroll.MyPayslip(ctx, s, id)
		if err != nil {
			return nil, payrollError(err)
		}
		return &payslipOutput{Body: v}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "getMyPayslipFile", Method: "GET", Path: "/v1/me/payslips/{id}/file", Tags: []string{"Payroll"}, Summary: "My payslip as a PDF",
		Description: "The PDF of one of the signed-in person's own payslips, after the stored bytes are checked against the SHA-256 recorded when it was made: a changed byte is never served. A leaver's narrow account keeps this. It changes nothing.", Errors: []int{401, 404, 409, 503},
	}, func(ctx context.Context, in *payslipIDInput) (*payslipFileOutput, error) {
		s, err := a.signedInOrLeaver(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such payslip of yours.")
		}
		ref, pdf, err := a.d.Payroll.MyPayslipFile(ctx, s, id)
		if err != nil {
			return nil, payrollError(err)
		}
		out := &payslipFileOutput{}
		out.Body.Reference, out.Body.ContentType, out.Body.ContentBase64 = ref, "application/pdf", base64.StdEncoding.EncodeToString(pdf)
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "getPayRunChanges", Method: "GET", Path: "/v1/payroll/runs/{id}/changes", Tags: []string{"Payroll"}, Summary: "A pay run against last month's",
		Description: "The newest version against the previous month's approved run of the same pay group: gross pay, net pay and employer cost, and every difference with its cause and the item it comes from (a joiner, a leaver, a pay change, unpaid leave, overtime, a one-off item, an allowance or a deduction), with what has no input behind it listed for the approver. The Payroll administrator and the Owner. It changes nothing.", Errors: []int{401, 403, 404},
	}, func(ctx context.Context, in *runIDInput) (*runChangesOutput, error) {
		s, err := payroller(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such pay run.")
		}
		c, err := a.d.Payroll.Changes(ctx, s, id)
		if err != nil {
			return nil, payrollError(err)
		}
		return &runChangesOutput{Body: c}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "approvePayRunVersion", Method: "POST", Path: "/v1/payroll/runs/{id}/approval", Tags: []string{"Payroll"}, Summary: "Approve a pay run version",
		Description: "Approves the newest version, while it is calculated, after a passkey confirmation in the last five minutes, over the digest the approver read. The approver is the Payroll administrator or the Owner and never the person who prepared the version (a database constraint refuses it too); a version with a ✖ is refused. The approval is written once with what the passkey confirmed, older approved versions of the run are superseded, and the inputs the version used are marked used by the run. It is audited with the run, the version and the digest, never an amount.", Errors: []int{401, 403, 404, 409, 422}, DefaultStatus: 204,
	}, func(ctx context.Context, in *runApprovalInput) (*struct{}, error) {
		s, err := payroller(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if !s.Fresh(a.d.Clock.Now()) {
			return nil, Err(CodeStepUp, "Confirm it is you with your passkey, then try again: approving a pay run moves money.")
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such pay run.")
		}
		return nil, payrollErrorOrNil(a.d.Payroll.Approve(ctx, s, id, in.Body.Version, in.Body.Digest))
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
