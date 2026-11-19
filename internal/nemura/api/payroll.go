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

// Payroll is pay records behind the API.
type Payroll interface {
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
