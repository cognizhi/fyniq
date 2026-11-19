package api

import (
	"context"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
)

// OvertimeAsk is a day's overtime, rest-day work or public-holiday work, entered without clocking (NM-TIM-04).
type OvertimeAsk struct {
	Date   string `json:"date" format:"date" doc:"The day worked." example:"2026-09-01"`
	Kind   string `json:"kind" enum:"overtime,rest_day,public_holiday" doc:"Overtime beyond normal hours, work on a rest day, or work on a public holiday." example:"overtime"`
	Hours  string `json:"hours" doc:"Hours worked, in quarter hours, as a decimal string." example:"2.50"`
	Branch string `json:"branch,omitempty" enum:"employer_request,employee_request" doc:"For rest-day work, at whose request it was: the pay differs." example:"employer_request"`
	Note   string `json:"note,omitempty" maxLength:"500" doc:"A note for the approver." example:"Month-end close."`
}

// OvertimeRecord is a record as it is listed.
type OvertimeRecord struct {
	ID        string `json:"id" format:"uuid" doc:"The record." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1eb0"`
	PersonID  string `json:"person_id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Person    string `json:"person_name" doc:"Their name." example:"Ben Lim"`
	Date      string `json:"date" format:"date" doc:"The day worked." example:"2026-09-01"`
	Kind      string `json:"kind" enum:"overtime,rest_day,public_holiday" doc:"The kind of work." example:"overtime"`
	Hours     string `json:"hours" doc:"Hours worked." example:"2.50"`
	Branch    string `json:"branch,omitempty" doc:"For rest-day work, at whose request." example:"employer_request"`
	OverLimit bool   `json:"over_limit" doc:"The month's overtime hours pass the country pack's limit with this record: it stays recorded and HR is to act on it." example:"false"`
	Note      string `json:"note,omitempty" doc:"The person's note." example:"Month-end close."`
	State     string `json:"state" enum:"pending,approved,refused" doc:"Where the record is." example:"pending"`
	Reason    string `json:"decision_reason,omitempty" doc:"The approver's reason; a refusal always has one." example:"Not requested."`
	Used      bool   `json:"used_by_pay_run" doc:"Whether a pay run has used it, once." example:"false"`
}

// PayInput is something a pay run takes from leave and time.
type PayInput struct {
	ID       string `json:"id" format:"uuid" doc:"The input." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ec0"`
	PersonID string `json:"person_id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Person   string `json:"person_name" doc:"Their name." example:"Ben Lim"`
	Kind     string `json:"kind" enum:"unpaid_leave,encashment,overtime,rest_day,public_holiday" doc:"What it is." example:"unpaid_leave"`
	Quantity string `json:"quantity" doc:"Days or hours, as a decimal string." example:"2.00"`
	Unit     string `json:"unit" enum:"days,hours" doc:"The unit of the quantity." example:"days"`
	From     string `json:"from" format:"date" doc:"The first day." example:"2026-03-02"`
	To       string `json:"to" format:"date" doc:"The last day." example:"2026-03-03"`
	Source   string `json:"source_type" enum:"sick_notification,leave_ledger,overtime_entry" doc:"The record it comes from." example:"sick_notification"`
	SourceID string `json:"source_id" format:"uuid" doc:"That record." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e90"`
}

// AccrualLine is one person's line of a month's leave liability accrual.
type AccrualLine struct {
	ID          string `json:"id" format:"uuid" doc:"The line." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ed0"`
	PersonID    string `json:"person_id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Person      string `json:"person_name" doc:"Their name." example:"Ben Lim"`
	State       string `json:"state" enum:"draft,approved" doc:"A draft waiting for approval, or approved and ready for the Fyniq link." example:"draft"`
	CarriedDays string `json:"carried_days" doc:"Carried-forward annual leave untaken at the month's end: what the accrual recognises by default." example:"4.00"`
	AnnualLeft  string `json:"annual_left" doc:"All annual leave left, for the accountant's information." example:"18.00"`
}

// Availability is what a person has available to work in a period.
type Availability struct {
	WorkingDays string `json:"working_days" doc:"Working days by the person's pattern, public holidays left out." example:"21.00"`
	AwayDays    string `json:"away_days" doc:"Of them, the days away on approved leave, sick leave or government-paid leave." example:"3.50"`
	HoursPerDay string `json:"hours_per_day" doc:"Hours in a working day: the week's hours over the days worked." example:"8.80"`
	Hours       string `json:"available_hours" doc:"The hours available: working days less days away, at the hours of a day." example:"154.00"`
}

// Time is hours worked on special days and what leave gives a pay run, behind the API.
type Time interface {
	RecordOvertime(ctx context.Context, s Session, a OvertimeAsk) (id string, overLimit bool, err error)
	Overtime(ctx context.Context, s Session, sc LeaveScope, state string) ([]OvertimeRecord, error)
	DecideOvertime(ctx context.Context, s Session, sc LeaveScope, id uuid.UUID, approve bool, reason string) error
	PayInputs(ctx context.Context, s Session, to time.Time) ([]PayInput, error)
	DecideUnpaid(ctx context.Context, s Session, notification uuid.UUID, treatment string) (days string, err error)
	Encash(ctx context.Context, s Session, person uuid.UUID, year int, days string, on time.Time, note string) error
	DraftAccrual(ctx context.Context, s Session, month time.Time) (lines int, err error)
	Accruals(ctx context.Context, s Session, month time.Time) ([]AccrualLine, error)
	ApproveAccrual(ctx context.Context, s Session, month time.Time) (lines int, err error)
	Availability(ctx context.Context, s Session, person uuid.UUID, from, to time.Time) (Availability, error)
}

func init() {
	IdempotencyExempt["POST /v1/leave/accruals"] = "a month is drafted once: a repeat is refused (NM_LEAVE_CONFLICT)"
	IdempotencyExempt["POST /v1/leave/accruals/approval"] = "a draft is approved once: a repeat is refused (NM_LEAVE_CONFLICT)"
	IdempotencyExempt["POST /v1/me/overtime"] = "a repeat is a second record for the same person, day and kind, which the database refuses (NM_LEAVE_CONFLICT)"
	IdempotencyExempt["POST /v1/overtime/{id}/decision"] = "a decision is made once: a repeat is refused (NM_LEAVE_CONFLICT)"
	IdempotencyExempt["POST /v1/sick-leave/{id}/unpaid-decision"] = "the days beyond the ceiling are decided once: a repeat is refused (NM_LEAVE_CONFLICT)"
	IdempotencyExempt["POST /v1/people/{id}/leave/{year}/encashment"] = "each encashment is its own ledger entry and pay input; the balance stops an excess"
}

type overtimeInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Body   OvertimeAsk
}

type overtimeRecordedOutput struct {
	Body struct {
		ID        string `json:"id" format:"uuid" doc:"The new record." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1eb0"`
		OverLimit bool   `json:"over_limit" doc:"The month's overtime hours pass the limit with this record: it is recorded and flagged, never refused." example:"false"`
	}
}

type overtimeListOutput struct {
	Body struct {
		Records []OvertimeRecord `json:"records" doc:"The records the caller may read, newest first."`
	}
}

type payInputsInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	To     string `query:"to" format:"date" doc:"Inputs with a day on or before this one; today when absent." example:"2026-12-31"`
}

type payInputsOutput struct {
	Body struct {
		Inputs []PayInput `json:"inputs" doc:"The inputs no pay run has used yet, oldest first."`
	}
}

type unpaidDecisionInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The sick leave notification." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e90"`
	Body   struct {
		Treatment string `json:"treatment" enum:"unpaid,annual" doc:"Unpaid leave (a pay input) or taken from annual leave." example:"unpaid"`
	}
}

type unpaidDecisionOutput struct {
	Body struct {
		Days string `json:"days" doc:"The days decided." example:"2.00"`
	}
}

type encashInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Year   int    `path:"year" minimum:"2000" maximum:"2200" doc:"The leave year." example:"2026"`
	Body   struct {
		Days string `json:"days" doc:"Days of annual leave to pay out, as a decimal string." example:"2.00"`
		On   string `json:"on" format:"date" doc:"The day the encashment is for; the pay run that covers it pays it." example:"2026-11-30"`
		Note string `json:"note,omitempty" maxLength:"500" doc:"Why, for the record." example:"Agreed on leaving."`
	}
}

func (a *API) registerTime() {
	huma.Register(a.API, huma.Operation{
		OperationID: "recordOvertime", Method: "POST", Path: "/v1/me/overtime", Tags: []string{"Leave"}, Summary: "Record a day's overtime or special-day work",
		Description: "Records the signed-in Part IV employee's overtime, rest-day work or public-holiday work for a day, without clocking. It starts pending. Overtime beyond the country pack's monthly limit stays recorded and is flagged for HR, never refused. A person who is not a Part IV employee on that day is refused. It is audited.",
		Errors:      []int{401, 404, 409, 422}, DefaultStatus: 201,
	}, func(ctx context.Context, in *overtimeInput) (*overtimeRecordedOutput, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if err := a.leaveOn(ctx, s); err != nil {
			return nil, err
		}
		id, over, err := a.d.Time.RecordOvertime(ctx, s, in.Body)
		if err != nil {
			return nil, requestsError(err)
		}
		out := &overtimeRecordedOutput{}
		out.Body.ID, out.Body.OverLimit = id, over
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "listOvertime", Method: "GET", Path: "/v1/overtime", Tags: []string{"Leave"}, Summary: "Overtime and special-day records the caller may read",
		Description: "Lists the caller's own records, their reports' for a line manager, and everyone's for the people team, the Owner and the Payroll administrator. It changes nothing.",
		Errors:      []int{401, 409},
	}, func(ctx context.Context, in *requestListInput) (*overtimeListOutput, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if err := a.leaveOn(ctx, s); err != nil {
			return nil, err
		}
		sc, err := a.leaveScope(ctx, s)
		if err != nil {
			return nil, err
		}
		if in.Mine {
			sc = LeaveScope{}
		}
		list, err := a.d.Time.Overtime(ctx, s, sc, in.State)
		if err != nil {
			return nil, requestsError(err)
		}
		out := &overtimeListOutput{}
		out.Body.Records = list
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "decideOvertime", Method: "POST", Path: "/v1/overtime/{id}/decision", Tags: []string{"Leave"}, Summary: "Approve or refuse an overtime record",
		Description: "The person's line manager decides on their reports' records and the HR officer, the HR administrator and the Owner on anyone's; nobody decides their own. A refusal needs a reason. A decision is made once. It is audited.",
		Errors:      []int{401, 403, 404, 409, 422},
	}, func(ctx context.Context, in *decisionInput) (*struct{}, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if err := a.leaveOn(ctx, s); err != nil {
			return nil, err
		}
		sc, err := a.leaveScope(ctx, s)
		if err != nil {
			return nil, err
		}
		if !sc.Decide {
			return nil, Err(CodeRoleRequired, "Only a line manager for their reports, the people team and the Owner decide overtime.")
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such overtime record in your view.")
		}
		return nil, requestsErrorOrNil(a.d.Time.DecideOvertime(ctx, s, sc, id, in.Body.Decision == "approve", in.Body.Reason))
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "listPayInputs", Method: "GET", Path: "/v1/pay-inputs", Tags: []string{"Leave"}, Summary: "What leave and time give the next pay run",
		Description: "Lists the inputs no pay run has used yet: unpaid leave, encashed annual leave, and approved overtime, rest-day and public-holiday hours, each linked to its source record. The people team, the Owner and the Payroll administrator. A pay run marks each used once. It changes nothing.",
		Errors:      []int{401, 403, 409, 422},
	}, func(ctx context.Context, in *payInputsInput) (*payInputsOutput, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if err := a.leaveOn(ctx, s); err != nil {
			return nil, err
		}
		sc, err := a.leaveScope(ctx, s)
		if err != nil {
			return nil, err
		}
		if !sc.All {
			return nil, Err(CodeRoleRequired, "Only the people team, the Owner and the Payroll administrator see what leave and time give a pay run.")
		}
		to, err := a.asOf(in.To)
		if err != nil {
			return nil, err
		}
		list, err := a.d.Time.PayInputs(ctx, s, to)
		if err != nil {
			return nil, requestsError(err)
		}
		out := &payInputsOutput{}
		out.Body.Inputs = list
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "decideUnpaidSickDays", Method: "POST", Path: "/v1/sick-leave/{id}/unpaid-decision", Tags: []string{"Leave"}, Summary: "Decide how the days beyond the sick leave ceiling are handled",
		Description: "The HR officer or the HR administrator decides, once, whether the days of a sick leave notification beyond the paid ceiling are unpaid leave (a pay input) or taken from annual leave. It is audited.",
		Errors:      []int{401, 403, 404, 409, 422},
	}, func(ctx context.Context, in *unpaidDecisionInput) (*unpaidDecisionOutput, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if err := a.mayCreate(ctx, s); err != nil {
			return nil, err
		}
		if err := a.leaveOn(ctx, s); err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such sick leave notification.")
		}
		days, err := a.d.Time.DecideUnpaid(ctx, s, id, in.Body.Treatment)
		if err != nil {
			return nil, requestsError(err)
		}
		out := &unpaidDecisionOutput{}
		out.Body.Days = days
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "encashLeave", Method: "POST", Path: "/v1/people/{id}/leave/{year}/encashment", Tags: []string{"Leave"}, Summary: "Pay out days of annual leave",
		Description: "The HR officer or the HR administrator takes days of the person's annual leave from the ledger as an encashment, never beyond the balance, and gives the pay run an input to pay them. Encashment is off unless the company agreed it: nothing happens by itself. It is audited.",
		Errors:      []int{401, 403, 404, 409, 422}, DefaultStatus: 201,
	}, func(ctx context.Context, in *encashInput) (*struct{}, error) {
		s, id, err := a.writer(ctx, in.Cookie, in.ID)
		if err != nil {
			return nil, err
		}
		if err := a.leaveOn(ctx, s); err != nil {
			return nil, err
		}
		on, err := date("on", in.Body.On, true)
		if err != nil {
			return nil, Err(CodeLeaveInvalid, "on is not a date: write it as 2026-11-30.")
		}
		return nil, requestsErrorOrNil(a.d.Time.Encash(ctx, s, id, in.Year, in.Body.Days, *on, in.Body.Note))
	})
}

type accrualInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Body   struct {
		Month string `json:"month" pattern:"^[0-9]{4}-(0[1-9]|1[0-2])$" doc:"The month, as 2026-05." example:"2026-05"`
	}
}

type accrualCountOutput struct {
	Body struct {
		Lines int `json:"lines" doc:"How many people have days on the accrual." example:"3"`
	}
}

type accrualListInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Month  string `query:"month" pattern:"^[0-9]{4}-(0[1-9]|1[0-2])$" doc:"The month, as 2026-05; this month when absent." example:"2026-05"`
}

type accrualListOutput struct {
	Body struct {
		Lines []AccrualLine `json:"lines" doc:"The month's accrual by person."`
	}
}

type availabilityInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	From   string `query:"from" format:"date" doc:"The first day." example:"2026-04-01"`
	To     string `query:"to" format:"date" doc:"The last day." example:"2026-04-30"`
}

type availabilityOutput struct{ Body Availability }

func (a *API) registerAccrual() {
	huma.Register(a.API, huma.Operation{
		OperationID: "draftLeaveAccrual", Method: "POST", Path: "/v1/leave/accruals", Tags: []string{"Leave"}, Summary: "Draft a month's leave liability accrual",
		Description: "Drafts the month-end accrual: for each person, the carried-forward annual leave still untaken. Nemura keeps the days; the amounts come from the pay structure and the journal from the Fyniq link, which a person approves there. A month is drafted once. The HR officer and the HR administrator. It is audited.",
		Errors:      []int{401, 403, 409, 422}, DefaultStatus: 201,
	}, func(ctx context.Context, in *accrualInput) (*accrualCountOutput, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if err := a.mayCreate(ctx, s); err != nil {
			return nil, err
		}
		if err := a.leaveOn(ctx, s); err != nil {
			return nil, err
		}
		month, err := time.Parse("2006-01", in.Body.Month)
		if err != nil {
			return nil, Err(CodeLeaveInvalid, "month is not a month: write it as 2026-05.")
		}
		n, err := a.d.Time.DraftAccrual(ctx, s, month)
		if err != nil {
			return nil, requestsError(err)
		}
		out := &accrualCountOutput{}
		out.Body.Lines = n
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "listLeaveAccrual", Method: "GET", Path: "/v1/leave/accruals", Tags: []string{"Leave"}, Summary: "A month's leave liability accrual",
		Description: "Lists the month's lines. The people team, the Owner and the Payroll administrator. It changes nothing.",
		Errors:      []int{401, 403, 409, 422},
	}, func(ctx context.Context, in *accrualListInput) (*accrualListOutput, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if err := a.leaveOn(ctx, s); err != nil {
			return nil, err
		}
		sc, err := a.leaveScope(ctx, s)
		if err != nil {
			return nil, err
		}
		if !sc.All {
			return nil, Err(CodeRoleRequired, "Only the people team, the Owner and the Payroll administrator see the leave accrual.")
		}
		month := a.d.Clock.Now()
		if in.Month != "" {
			if month, err = time.Parse("2006-01", in.Month); err != nil {
				return nil, Err(CodeLeaveInvalid, "month is not a month: write it as 2026-05.")
			}
		}
		lines, err := a.d.Time.Accruals(ctx, s, month)
		if err != nil {
			return nil, requestsError(err)
		}
		out := &accrualListOutput{}
		out.Body.Lines = lines
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "approveLeaveAccrual", Method: "POST", Path: "/v1/leave/accruals/approval", Tags: []string{"Leave"}, Summary: "Approve a month's leave liability accrual",
		Description: "The HR officer, the HR administrator or the Owner approves the month's draft, once, and never the one they drafted. It is then ready for the Fyniq link. It is audited.",
		Errors:      []int{401, 403, 409, 422},
	}, func(ctx context.Context, in *accrualInput) (*accrualCountOutput, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if err := a.leaveOn(ctx, s); err != nil {
			return nil, err
		}
		sc, err := a.leaveScope(ctx, s)
		if err != nil {
			return nil, err
		}
		if !(sc.All && sc.Decide) {
			return nil, Err(CodeRoleRequired, "Only the HR officer, the HR administrator and the Owner approve the leave accrual.")
		}
		month, err := time.Parse("2006-01", in.Body.Month)
		if err != nil {
			return nil, Err(CodeLeaveInvalid, "month is not a month: write it as 2026-05.")
		}
		n, err := a.d.Time.ApproveAccrual(ctx, s, month)
		if err != nil {
			return nil, requestsError(err)
		}
		out := &accrualCountOutput{}
		out.Body.Lines = n
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "getAvailability", Method: "GET", Path: "/v1/people/{id}/availability", Tags: []string{"Leave"}, Summary: "A person's available hours in a period",
		Description: "Works out the hours a person has available between two days: their working days by pattern less public holidays, less the days away on approved leave, sick leave and government-paid leave, at the hours of a working day. This is what Fyniq plans against. The person, their line manager and the people team. It changes nothing.",
		Errors:      []int{401, 404, 409, 422},
	}, func(ctx context.Context, in *availabilityInput) (*availabilityOutput, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if err := a.leaveOn(ctx, s); err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodePersonNotFound, "There is no such person in this organisation.")
		}
		team, err := a.peopleTeam(ctx, s)
		if err != nil {
			return nil, err
		}
		if !team {
			own, report, err := a.d.People.Relation(ctx, s, id)
			if err != nil {
				return nil, personError(err)
			}
			if !own && !report {
				return nil, Err(CodePersonNotFound, "There is no such person in this organisation.")
			}
		}
		from, err := date("from", in.From, true)
		if err != nil {
			return nil, Err(CodeLeaveInvalid, "from is not a date: write it as 2026-04-01.")
		}
		to, err := date("to", in.To, true)
		if err != nil {
			return nil, Err(CodeLeaveInvalid, "to is not a date: write it as 2026-04-30.")
		}
		av, err := a.d.Time.Availability(ctx, s, id, *from, *to)
		if err != nil {
			return nil, requestsError(err)
		}
		return &availabilityOutput{Body: av}, nil
	})
}
