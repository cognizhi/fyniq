package api

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/nemura/features"
	"github.com/cognizhi/fyniq/internal/nemura/leave"
	"github.com/cognizhi/fyniq/internal/nemura/people"
)

// Problem codes of leave (the catalogue is in problem.go).
const (
	CodeLeaveInvalid  = leave.CodeInvalid
	CodeLeaveBalance  = leave.CodeBalance
	CodeLeaveConflict = leave.CodeConflict
	CodeFeatureOff    = "NM_FEATURE_OFF" // the feature is switched off for this organisation
)

// LeaveBalance is the four sums of a person's ledger for one leave type and year, in days.
type LeaveBalance struct {
	Entitled string `json:"entitled" doc:"Days granted: the year's entitlement, carried-forward days and corrections, as a decimal string." example:"14.00"`
	Taken    string `json:"taken" doc:"Days used." example:"3.00"`
	Booked   string `json:"booked" doc:"Days reserved for requests not yet taken." example:"2.00"`
	Expired  string `json:"expired" doc:"Carried-forward days that lapsed." example:"0.00"`
	Encashed string `json:"encashed" doc:"Days paid out instead of taken." example:"0.00"`
	Left     string `json:"left" doc:"What is left: entitled less taken, booked, expired and encashed." example:"9.00"`
}

// LeaveType is one leave type a person has in a year: its balance and how the entitlement was worked out.
type LeaveType struct {
	Key      string       `json:"key" doc:"The leave type's key." example:"annual"`
	Name     string       `json:"name" doc:"Its name." example:"Annual leave"`
	Counting string       `json:"counting" enum:"working_days,calendar_days" doc:"Whether the days are counted by the person's working days or by calendar days (NM-LV-11)." example:"working_days"`
	Balance  LeaveBalance `json:"balance" doc:"The balance."`
	Basis    []string     `json:"basis" doc:"The steps of the calculation, one plain line each, so a person can see where the figure comes from."`
	Sources  []string     `json:"sources" doc:"Where the days come from: the pack version and table, and the company's policy."`
	CarryEnd string       `json:"carried_until,omitempty" format:"date" doc:"The last day the carried-forward days may be taken." example:"2027-12-31"`
}

// WorkingPattern is the days and hours a person works from a day.
type WorkingPattern struct {
	From         string `json:"effective_from" format:"date" doc:"The day the pattern starts." example:"2026-01-05"`
	Weekdays     []int  `json:"weekdays" doc:"The days worked, ISO: 1 Monday to 7 Sunday." example:"[1,2,3,4,5]"`
	HoursPerWeek string `json:"hours_per_week" doc:"Hours a week, one decimal place." example:"44.0"`
}

// LeaveView is a person's leave in a year.
type LeaveView struct {
	PersonID string          `json:"person_id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Year     int             `json:"year" doc:"The leave year: the calendar year." example:"2026"`
	Pattern  *WorkingPattern `json:"working_pattern,omitempty" doc:"The working pattern in force on 1 January of the year or the person's first day, when one is set."`
	Types    []LeaveType     `json:"types" doc:"The leave types the person has, with each balance."`
}

// LeaveGrant is what working out a year's annual leave recorded.
type LeaveGrant struct {
	Days    string   `json:"days" doc:"The year's entitlement in days." example:"7.00"`
	Written string   `json:"written" doc:"What this call wrote to the ledger: the whole entitlement the first time, the difference after, nothing when the ledger was right." example:"7.00"`
	Basis   []string `json:"basis" doc:"The steps of the calculation."`
	Sources []string `json:"sources" doc:"Where the days come from."`
}

// Leave is leave behind the API: the API decides who may call, the service reads and writes the ledger in one transaction and audits.
type Leave interface {
	// Own is the person record of the signed-in member, or a person-not-found refusal when their login is linked to none.
	Own(ctx context.Context, s Session) (uuid.UUID, error)
	View(ctx context.Context, s Session, id uuid.UUID, year int) (LeaveView, error)
	SetPattern(ctx context.Context, s Session, id uuid.UUID, from time.Time, p leave.Pattern) error
	Grant(ctx context.Context, s Session, id uuid.UUID, year int) (LeaveGrant, error)
	CarryForward(ctx context.Context, s Session, id uuid.UUID, year int) (carried string, err error)
}

func init() {
}

type leaveInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Year   int    `path:"year" minimum:"2000" maximum:"2200" doc:"The leave year: the calendar year." example:"2026"`
}

type leaveOutput struct{ Body LeaveView }

type myLeaveInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Year   int    `path:"year" minimum:"2000" maximum:"2200" doc:"The leave year: the calendar year." example:"2026"`
}

type patternInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Body   WorkingPattern
}

type grantOutput struct{ Body LeaveGrant }

type carryOutput struct {
	Body struct {
		Carried string `json:"carried" doc:"The days carried into the next year; zero when nothing was unused or all was carried already." example:"5.00"`
	}
}

func leaveError(err error) error {
	var l *leave.Refusal
	var p *people.Refusal
	switch {
	case errors.As(err, &l):
		return Err(l.Code, l.Message)
	case errors.As(err, &p):
		return Err(p.Code, p.Message)
	}
	return huma.Error500InternalServerError("The leave could not be handled.")
}

// parseHours reads hours a week with at most one decimal place into tenths: "44.5" is 445.
func parseHours(s string) (int, error) {
	whole, frac, _ := strings.Cut(strings.TrimSpace(s), ".")
	w, err := strconv.Atoi(whole)
	if err != nil || w < 0 || strings.ContainsAny(whole+frac, "+-") || len(frac) > 1 {
		return 0, Err(CodeLeaveInvalid, "hours_per_week is not hours with one decimal place at most: write it as 44.0.")
	}
	f := 0
	if frac != "" {
		if f, err = strconv.Atoi(frac); err != nil {
			return 0, Err(CodeLeaveInvalid, "hours_per_week is not hours with one decimal place at most: write it as 44.0.")
		}
	}
	return w*10 + f, nil
}

// leaveOn refuses a request while the Leave feature is off for the organisation: its records are kept and its screens are hidden (NM-FEAT-01).
func (a *API) leaveOn(ctx context.Context, s Session) error {
	sw, err := a.d.Features.List(ctx, s)
	if err != nil {
		return huma.Error500InternalServerError("The feature switches could not be read.")
	}
	if sw["leave"].State != features.On {
		return Err(CodeFeatureOff, "Leave is switched off for this organisation: an Owner or the HR administrator switches it on under Settings, Features.")
	}
	return nil
}

func (a *API) registerLeave() {
	huma.Register(a.API, huma.Operation{
		OperationID: "getLeave", Method: "GET", Path: "/v1/people/{id}/leave/{year}", Tags: []string{"Leave"}, Summary: "A person's leave in a year",
		Description: "Returns a person's working pattern and, for each leave type, the balance (entitled, taken, booked, expired, encashed, left) and the steps of the calculation with their sources. The person reads their own; the line manager reads their reports'; the people team reads anyone's (Appendix B: Leave requests). Anyone else is refused as if the person did not exist. It changes nothing.",
		Errors:      []int{401, 404, 409, 422},
	}, func(ctx context.Context, in *leaveInput) (*leaveOutput, error) {
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
		v, err := a.d.Leave.View(ctx, s, id, in.Year)
		if err != nil {
			return nil, leaveError(err)
		}
		return &leaveOutput{Body: v}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "getMyLeave", Method: "GET", Path: "/v1/me/leave/{year}", Tags: []string{"Leave"}, Summary: "My leave in a year",
		Description: "Returns the signed-in person's own working pattern and balances, as GET /v1/people/{id}/leave/{year} does for the person their login is linked to. A login linked to no person record is refused (NM_NOT_FOUND). It changes nothing.",
		Errors:      []int{401, 404, 409, 422},
	}, func(ctx context.Context, in *myLeaveInput) (*leaveOutput, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if err := a.leaveOn(ctx, s); err != nil {
			return nil, err
		}
		id, err := a.d.Leave.Own(ctx, s)
		if err != nil {
			return nil, leaveError(err)
		}
		v, err := a.d.Leave.View(ctx, s, id, in.Year)
		if err != nil {
			return nil, leaveError(err)
		}
		return &leaveOutput{Body: v}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "setWorkingPattern", Method: "PUT", Path: "/v1/people/{id}/working-pattern", Tags: []string{"Leave"}, Summary: "Set a person's working pattern",
		Description: "Ends the person's current working pattern the day before the new one starts and starts it: leave counts days by the pattern, so it is history, never edited. A day on or before the current pattern's start is refused (NM_LEAVE_CONFLICT). The HR officer and the HR administrator. It is audited.",
		Errors:      []int{401, 403, 404, 409, 422},
	}, func(ctx context.Context, in *patternInput) (*struct{}, error) {
		s, id, err := a.writer(ctx, in.Cookie, in.ID)
		if err != nil {
			return nil, err
		}
		if err := a.leaveOn(ctx, s); err != nil {
			return nil, err
		}
		from, err := date("effective_from", in.Body.From, true)
		if err != nil {
			return nil, err
		}
		hours, err := parseHours(in.Body.HoursPerWeek)
		if err != nil {
			return nil, err
		}
		if err := a.d.Leave.SetPattern(ctx, s, id, *from, leave.Pattern{Weekdays: in.Body.Weekdays, HoursPerWeek: hours}); err != nil {
			return nil, leaveError(err)
		}
		return nil, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "grantAnnualLeave", Method: "POST", Path: "/v1/people/{id}/leave/{year}/annual-grant", Tags: []string{"Leave"}, Summary: "Work out and record a year's annual leave",
		Description: "Works out the person's annual leave for the calendar year from the country pack and the company's policy, by completed months of service, and records it in the ledger: the whole entitlement the first time, only the difference afterwards as a correction. The response gives the steps and sources. The HR officer and the HR administrator. It is audited.",
		Errors:      []int{401, 403, 404, 409, 422},
	}, func(ctx context.Context, in *leaveInput) (*grantOutput, error) {
		s, id, err := a.writer(ctx, in.Cookie, in.ID)
		if err != nil {
			return nil, err
		}
		if err := a.leaveOn(ctx, s); err != nil {
			return nil, err
		}
		g, err := a.d.Leave.Grant(ctx, s, id, in.Year)
		if err != nil {
			return nil, leaveError(err)
		}
		return &grantOutput{Body: g}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "carryForwardLeave", Method: "POST", Path: "/v1/people/{id}/leave/{year}/carry-forward", Tags: []string{"Leave"}, Summary: "Carry a year's unused annual leave forward",
		Description: fmt.Sprintf("Moves the person's unused annual leave of the year into the next, to be taken within %d months of the year's end and lapsing after (the default of the company's policy). The HR officer and the HR administrator. It is audited.", leave.DefaultCarryMonths),
		Errors:      []int{401, 403, 404, 409, 422},
	}, func(ctx context.Context, in *leaveInput) (*carryOutput, error) {
		s, id, err := a.writer(ctx, in.Cookie, in.ID)
		if err != nil {
			return nil, err
		}
		if err := a.leaveOn(ctx, s); err != nil {
			return nil, err
		}
		c, err := a.d.Leave.CarryForward(ctx, s, id, in.Year)
		if err != nil {
			return nil, leaveError(err)
		}
		out := &carryOutput{}
		out.Body.Carried = c
		return out, nil
	})
}
