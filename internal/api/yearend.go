package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ year end on the close (FR-GL-10)

// RetainedEarnings is what a financial year does to the account with the retained-earnings role.
type RetainedEarnings struct {
	AccountID string `json:"account_id" format:"uuid" doc:"The account with the retained-earnings role." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d70"`
	Code      string `json:"code" doc:"Its code." example:"3100"`
	Name      string `json:"name" doc:"Its name." example:"Retained earnings"`
	Start     Money  `json:"start" doc:"Its balance on the year's first day: every earlier year's result and what was posted to it before. A credit, so negative, is accumulated profit."`
	Posted    Money  `json:"posted" doc:"What was posted to it in the year, such as the opening balances at the books start."`
	End       Money  `json:"end" doc:"start + posted + the year's result: its balance on the next year's first day, which the balance service works out whenever it reads one."`
}

// YearEnd is a financial year's year end: its months, its result and the retained earnings it carries forward.
type YearEnd struct {
	Number           int               `json:"number" doc:"The financial year, 1 for the first." example:"1"`
	StartsOn         string            `json:"starts_on" format:"date" doc:"Its first day: the books start for the first year." example:"2026-03-15"`
	EndsOn           string            `json:"ends_on" format:"date" doc:"Its last day." example:"2027-03-31"`
	Periods          int               `json:"periods" doc:"How many of its periods there are: a year opens whole, so all of its months." example:"13"`
	PeriodsClosed    int               `json:"periods_closed" doc:"How many of them are closed." example:"6"`
	Closed           bool              `json:"closed" doc:"Every period of the year is closed: its last month's close closes the year, as periods close in order." example:"false"`
	LastPeriod       Period            `json:"last_period" doc:"The year's last period, whose close is the year-end close."`
	Income           Money             `json:"income" doc:"The year's revenue so far in the base currency, as in the trial balance: credits, so negative."`
	Expenses         Money             `json:"expenses" doc:"The year's expenses so far: debits, so positive."`
	Result           Money             `json:"result" doc:"income + expenses: negative is a profit, which credits retained earnings; positive is a loss."`
	RetainedEarnings *RetainedEarnings `json:"retained_earnings,omitempty" doc:"Absent while no account has the retained-earnings role: then nothing folds, and revenue and expense accounts carry on from year to year."`
}

type yearEndOutput struct {
	Body YearEnd
}

func registerYearEnd(a *API) {
	register(a, authz.GetYearEnd, huma.Operation{
		OperationID: "getYearEnd", Method: http.MethodGet, Path: "/v1/periods/{period_id}/year-end", Tags: []string{"Periods"},
		Summary: "Get the year end of a period's financial year",
		Description: "The year end of the financial year that holds the period (FR-GL-10; PRD-001 rev 0.69, provisional): how many of its " +
			"periods are closed, its income, expenses and result so far from the balance service, and the retained earnings it carries " +
			"forward. Year end is computed, never posted: on the next year's first day revenue and expense accounts start from nothing " +
			"and the account with the retained-earnings role opens with every earlier year's result, so there is no closing entry and " +
			"nothing to carry forward; the optional closing entry for statutory presentation waits for the accountant. Amounts are in the " +
			"base currency as in the trial balance, debits positive. Refused while the financial years are not set (PERIOD-01). " +
			"Read-only; no side effects.",
	}, []string{CodeNotFound, period.CodeNoYear}, func(ctx context.Context, in *PeriodPath) (*yearEndOutput, error) {
		id, err := parseID("period_id", in.PeriodID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out yearEndOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			out.Body, err = yearEndOf(ctx, u, id)
			return err
		})
		return &out, err
	})
}

func yearEndOf(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (YearEnd, error) {
	p, err := u.Periods().Get(ctx, id)
	if err != nil {
		return YearEnd{}, err
	}
	o, err := u.Organisation(ctx)
	if err != nil {
		return YearEnd{}, err
	}
	cal, err := period.CalendarOf(o.Profile)
	if err != nil {
		return YearEnd{}, err
	}
	base, err := money.Lookup(o.BaseCurrency)
	if err != nil {
		return YearEnd{}, err
	}
	n := cal.Number(p.StartsOn)
	if n == 0 {
		return YearEnd{}, &period.Error{Code: period.CodeNoYear, Message: "the period starts before the books start, so no financial year holds it",
			Fix: "Check the books start in Settings › Organisation"}
	}
	y := cal.Year(n)
	all, err := u.Periods().List(ctx)
	if err != nil {
		return YearEnd{}, err
	}
	out := YearEnd{Number: n, StartsOn: y.StartsOn.Format(time.DateOnly), EndsOn: y.EndsOn.Format(time.DateOnly)}
	var last storage.Period
	for _, q := range all {
		if q.StartsOn.Before(y.StartsOn) || q.EndsOn.After(y.EndsOn) {
			continue
		}
		out.Periods++
		if q.Status == storage.PeriodClosed {
			out.PeriodsClosed++
		}
		if q.EndsOn.After(last.EndsOn) {
			last = q
		}
	}
	out.Closed = out.PeriodsClosed == out.Periods
	out.LastPeriod = periodOf(last, &cal)
	r, err := balance.Year(ctx, u, base, all, y.Span)
	if err != nil {
		return YearEnd{}, err
	}
	out.Income, out.Expenses, out.Result = amountOf(r.Income), amountOf(r.Expenses), amountOf(r.Result)
	if r.Folds {
		re, err := u.Accounts().Get(ctx, r.RetainedID)
		if err != nil {
			return YearEnd{}, err
		}
		out.RetainedEarnings = &RetainedEarnings{AccountID: re.ID.String(), Code: re.Code, Name: re.Name,
			Start: amountOf(r.RetainedStart), Posted: amountOf(r.RetainedPosted), End: amountOf(r.RetainedEnd)}
	}
	return out, nil
}
