package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/cashflow"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the short-term cash flow (FR-PLN-09, FR-PLN-05)

// CashMovement is one expected movement of the money accounts.
type CashMovement struct {
	Key         string `json:"key" doc:"Names it in a what-if: the document's or batch's ID, or a recurring document's ID and occurrence as <id>@YYYY-MM-DD." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d70"`
	Kind        string `json:"kind" enum:"invoice,bill,recurring_invoice,recurring_bill,payment_batch" doc:"What it is: an open invoice or bill, a recurring invoice's or bill's occurrence, or an approved payment batch not yet released." example:"invoice"`
	ID          string `json:"id" format:"uuid" doc:"The document, the recurring document or the batch." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d70"`
	Occurrence  string `json:"occurrence,omitempty" format:"date" doc:"A recurring document's occurrence." example:"2026-12-15"`
	Number      string `json:"number,omitempty" doc:"The document's or batch's number, or the recurring document's name." example:"INV-0142"`
	ContactID   string `json:"contact_id,omitempty" format:"uuid" doc:"The customer or supplier." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d02"`
	ContactName string `json:"contact_name,omitempty" doc:"Its name." example:"Lumen Health Pte Ltd"`
	DueDate     string `json:"due_date,omitempty" format:"date" doc:"Its due date; absent for a batch." example:"2026-11-10"`
	ExpectedOn  string `json:"expected_on" format:"date" doc:"The day it is expected." example:"2026-11-17"`
	Basis       string `json:"basis" enum:"history,due,planned,overdue,scheduled,moved" doc:"Why that day: history, the due date + the contact's average days late over the last 12 months; due, its due date (no history); planned, a bill's planned payment date (no history); overdue, expected before today, so today; scheduled, a batch's payment date; moved, a what-if moved it." example:"history"`
	DaysLate    int    `json:"days_late,omitempty" doc:"history: the contact's average days late, rounded half away from zero." example:"7"`
	Amount      Money  `json:"amount" doc:"In the base currency: money in above zero, money out below. An open item's is its carrying amount."`
}

// CashDay is one day of the projection.
type CashDay struct {
	Date  string `json:"date" format:"date" doc:"The day." example:"2026-11-17"`
	In    Money  `json:"in" doc:"Money in on it."`
	Out   Money  `json:"out" doc:"Money out on it, above zero."`
	Close Money  `json:"close" doc:"The balance at its end."`
}

// CashWeek is seven days of the projection, the first from today (FR-PLN-05).
type CashWeek struct {
	From  string `json:"from" format:"date" doc:"Its first day." example:"2026-11-02"`
	To    string `json:"to" format:"date" doc:"Its last day." example:"2026-11-08"`
	In    Money  `json:"in" doc:"Money in over it."`
	Out   Money  `json:"out" doc:"Money out over it, above zero."`
	Close Money  `json:"close" doc:"The balance at its end."`
}

// CashBuffer is the buffer line.
type CashBuffer struct {
	Months      int    `json:"months" doc:"planning.buffer_months, the months of burn kept." example:"6"`
	AverageBurn *Money `json:"average_burn,omitempty" doc:"The average monthly net burn (the KPI's net_burn) of the last three closed months, fewer while there are fewer; absent while no month is closed."`
	Line        *Money `json:"line,omitempty" doc:"months × the average burn, rounded once (RND-PLAN); absent when the company is not burning or no month is closed, which why says."`
	Why         string `json:"why,omitempty" doc:"Why there is no line." example:"no month is closed yet"`
}

// CashExcluded is an input the projection does not hold.
type CashExcluded struct {
	What string `json:"what" doc:"What is left out." example:"Pay runs and CPF"`
	Why  string `json:"why" doc:"Why." example:"they join with payroll"`
}

// CashProjection is the short-term cash flow (FR-PLN-09, FR-PLN-05; p1-planning design D7).
type CashProjection struct {
	Today         string         `json:"today" format:"date" doc:"The organisation's today, the projection's first day." example:"2026-11-02"`
	Days          int            `json:"days" doc:"How many days it runs." example:"60"`
	Currency      string         `json:"currency" doc:"The base currency." example:"SGD"`
	Opening       Money          `json:"opening" doc:"The money accounts' balance today, each valued at today's rate."`
	Lowest        Money          `json:"lowest" doc:"The lowest balance at a day's end, today's included."`
	LowestOn      string         `json:"lowest_on" format:"date" doc:"The first day it occurs." example:"2026-12-01"`
	BelowBufferOn string         `json:"below_buffer_on,omitempty" format:"date" doc:"The first day whose balance closes below the buffer line; absent when none does, or there is no line." example:"2026-12-01"`
	Buffer        CashBuffer     `json:"buffer"`
	Movements     []CashMovement `json:"movements" nullable:"false" doc:"Every expected movement in the days, by day."`
	Daily         []CashDay      `json:"daily" nullable:"false" doc:"Each day from today."`
	Weekly        []CashWeek     `json:"weekly" nullable:"false" doc:"The first 13 weeks from today, when the projection runs that far (91 days or more)."`
	NotIncluded   []CashExcluded `json:"not_included" nullable:"false" doc:"What the projection does not hold yet, so its lowest point is never taken as complete: pay runs and CPF, GST returns and corporate tax until their changes, and any input it could not read."`
}

// CashMove is a what-if's change to one movement.
type CashMove struct {
	Key      string `json:"key" minLength:"1" maxLength:"60" doc:"The movement's key, from getCashProjection." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d70"`
	To       string `json:"to,omitempty" format:"date" doc:"Move it to this day: delay a bill, chase an invoice." example:"2026-12-01"`
	LeaveOut bool   `json:"leave_out,omitempty" doc:"Leave it out of the projection." example:"false"`
}

// CashWhatIf is a what-if: the projection with some movements moved or left out. Nothing is saved (FR-PLN-08).
type CashWhatIf struct {
	Days  int        `json:"days,omitempty" enum:"30,60,90,180,91" doc:"How many days; 60 when left out; 91 is the 13 weeks." example:"60"`
	Moves []CashMove `json:"moves" maxItems:"200" nullable:"false" doc:"The changes, each to one movement."`
}

type (
	cashProjectionInput struct {
		Days int `query:"days" enum:"30,60,90,180,91" default:"60" doc:"How many days from today: 30, 60 (the default), 90 or 180; 91 is the 13-week view." example:"60"`
	}
	cashProjectionOutput struct{ Body CashProjection }
	cashWhatIfInput      struct{ Body CashWhatIf }
)

// cashProjectionOf is a projection as the API shows it.
func cashProjectionOf(p cashflow.Projection) CashProjection {
	out := CashProjection{Today: dateOf(p.Today), Days: p.Days, Currency: p.Opening.Currency().Code, Opening: moneyOut(p.Opening),
		Lowest: moneyOut(p.Lowest), LowestOn: dateOf(p.LowestOn), Buffer: CashBuffer{Months: p.Buffer.Months, Why: p.Buffer.Why},
		Movements: []CashMovement{}, Daily: []CashDay{}, Weekly: []CashWeek{}, NotIncluded: []CashExcluded{}}
	if p.BelowOn != nil {
		out.BelowBufferOn = dateOf(*p.BelowOn)
	}
	if b := p.Buffer.Burn; b != nil {
		m := moneyOut(*b)
		out.Buffer.AverageBurn = &m
	}
	if l := p.Buffer.Line; l != nil {
		m := moneyOut(*l)
		out.Buffer.Line = &m
	}
	for _, it := range p.Items {
		m := CashMovement{Key: it.Key(), Kind: it.Kind, ID: it.ID.String(), Number: it.Number, ContactName: it.ContactName,
			ExpectedOn: dateOf(it.On), Basis: it.Basis, DaysLate: it.DaysLate, Amount: moneyOut(it.Amount)}
		if !it.Occurrence.IsZero() {
			m.Occurrence = dateOf(it.Occurrence)
		}
		if it.Contact != [16]byte{} {
			m.ContactID = it.Contact.String()
		}
		if !it.DueDate.IsZero() {
			m.DueDate = dateOf(it.DueDate)
		}
		out.Movements = append(out.Movements, m)
	}
	for _, d := range p.Daily {
		out.Daily = append(out.Daily, CashDay{Date: dateOf(d.On), In: moneyOut(d.In), Out: moneyOut(d.Out), Close: moneyOut(d.Close)})
	}
	for _, w := range p.Weekly {
		out.Weekly = append(out.Weekly, CashWeek{From: dateOf(w.From), To: dateOf(w.To), In: moneyOut(w.In), Out: moneyOut(w.Out), Close: moneyOut(w.Close)})
	}
	for _, x := range p.Excluded {
		out.NotIncluded = append(out.NotIncluded, CashExcluded{What: x.What, Why: x.Why})
	}
	return out
}

func registerCashProjection(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	read := func(ctx context.Context, u storage.UnitOfWork, days int, moves []CashMove) (CashProjection, error) {
		if days == 0 {
			days = cashflow.DefaultHorizon
		}
		today, err := orgToday(ctx, a, u)
		if err != nil {
			return CashProjection{}, err
		}
		in, err := cashflow.Read(ctx, u, today, days)
		if err != nil {
			return CashProjection{}, err
		}
		if len(moves) > 0 {
			var ms []cashflow.Move
			var bad []FieldProblem
			for i, m := range moves {
				x := cashflow.Move{Key: m.Key, Leave: m.LeaveOut}
				if m.To != "" {
					to, err := time.Parse(time.DateOnly, m.To)
					if err != nil {
						bad = append(bad, FieldProblem{Code: CodeInvalid, Field: fmt.Sprintf("body.moves[%d].to", i), Message: "is not a date", Fix: "Send the day as YYYY-MM-DD"})
						continue
					}
					x.To = &to
				}
				if x.To == nil && !x.Leave {
					bad = append(bad, FieldProblem{Code: CodeInvalid, Field: fmt.Sprintf("body.moves[%d]", i), Message: "neither moves nor leaves out its movement",
						Fix: "Send to, a day to move it to, or leave_out"})
				}
				ms = append(ms, x)
			}
			items, unknown := cashflow.WhatIf(in.Items, ms)
			for i, m := range moves {
				if slices.Contains(unknown, m.Key) {
					bad = append(bad, FieldProblem{Code: CodeInvalid, Field: fmt.Sprintf("body.moves[%d].key", i), Message: "names no movement of the projection",
						Fix: "Use a key getCashProjection lists for the same days"})
				}
			}
			if len(bad) > 0 {
				return CashProjection{}, invalid(bad)
			}
			in.Items = items
		}
		p, err := in.Project(days)
		if err != nil {
			return CashProjection{}, err
		}
		return cashProjectionOf(p), nil
	}

	register(a, authz.GetCashProjection, huma.Operation{
		OperationID: "getCashProjection", Method: http.MethodGet, Path: "/v1/cash-projection", Tags: []string{"Planning"},
		Summary: "Get the short-term cash flow",
		Description: "The money accounts' balance day by day from today for 30, 60, 90 or 180 days (FR-PLN-09), and in 13 weeks (FR-PLN-05; " +
			"days=91), p1-planning design D7: today's balance valued at today's rates; each open invoice and bill on its expected day " +
			"(its due date + the contact's average days late over the last 12 months, else its due date or a bill's planned payment " +
			"date, and today when that has passed); each active recurring invoice's and bill's occurrences; each approved payment " +
			"batch not yet released on its payment day, its bills not counted again. It gives the lowest balance and its day, the " +
			"buffer line (planning.buffer_months × the average monthly net burn of the last three closed months) and the first day " +
			"below it, and what it does not hold yet. Read-only; no side effects.",
	}, append([]string{CodeInvalid}, append(locks, ForecastCodes...)...), func(ctx context.Context, in *cashProjectionInput) (*cashProjectionOutput, error) {
		p, _ := authn.From(ctx)
		var out cashProjectionOutput
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out.Body, err = read(ctx, u, in.Days, nil)
			return err
		})
		return &out, err
	})

	// A what-if saves nothing, so a repeat is harmless and return=preview would answer the same.
	register(a, authz.WhatIfCashProjection, exempt(exempt(huma.Operation{
		OperationID: "whatIfCashProjection", Method: http.MethodPost, Path: "/v1/cash-projection:what-if", Tags: []string{"Planning"},
		Summary: "Ask a what-if of the short-term cash flow",
		Description: "The projection getCashProjection gives, with movements moved to another day (delay this bill, chase that invoice) " +
			"or left out (FR-PLN-09, design D7). A what-if never changes a document, a planned payment date or a batch: it saves " +
			"nothing (FR-PLN-08). A move naming no movement of the projection is refused with the others. Saves nothing; no side effects.",
	}, "Idempotency-Key", "A cash what-if saves nothing, so a repeat is harmless and answers the same (FR-PLN-08)."),
		"Prefer", "A cash what-if saves nothing: return=preview would answer the same as the request itself (FR-INT-14)."),
		append([]string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}, append(locks, ForecastCodes...)...),
		func(ctx context.Context, in *cashWhatIfInput) (*cashProjectionOutput, error) {
			p, _ := authn.From(ctx)
			var out cashProjectionOutput
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
				out.Body, err = read(ctx, u, in.Body.Days, in.Body.Moves)
				return err
			})
			return &out, err
		})
}
