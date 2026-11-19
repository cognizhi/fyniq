package nemura

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/leave"
	"github.com/cognizhi/fyniq/internal/nemura/people"
	"github.com/cognizhi/fyniq/internal/ports"
)

// TimeService is hours worked on special days and what leave gives a pay run, behind the API (NM-TIM-02, NM-TIM-04, NM-LV-08): each call is one transaction for the
// caller's organisation, with the audit events of its changes.
type TimeService struct {
	Store *Store
	Clock ports.Clock
	Packs *PacksService
}

var _ api.Time = TimeService{}

func (g TimeService) in(ctx context.Context, s api.Session, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return g.Store.InOrg(ctx, s.Organisation, fn)
}

func (g TimeService) RecordOvertime(ctx context.Context, s api.Session, a api.OvertimeAsk) (string, bool, error) {
	var id uuid.UUID
	var over bool
	err := g.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		date, err := time.Parse(time.DateOnly, a.Date)
		if err != nil {
			return &leave.Refusal{Code: leave.CodeInvalid, Message: "date is not a date: write it as 2026-09-01."}
		}
		hours, err := leave.ParseDays(a.Hours)
		if err != nil {
			return &leave.Refusal{Code: leave.CodeInvalid, Message: "hours is not hours with at most two decimal places: write it as 2.50."}
		}
		var person uuid.UUID
		if err := tx.QueryRow(ctx, "SELECT id FROM nemura.person WHERE member_id = $1", s.Member).Scan(&person); err != nil {
			return &people.Refusal{Code: people.CodeNotFound, Message: "Your login is linked to no person record: ask the people team to link it."}
		}
		v, ok := g.Packs.inForce("sg-people", date)
		if !ok {
			return &leave.Refusal{Code: leave.CodeInvalid, Message: "No country pack covers that day: choose a day from 2026 on."}
		}
		id, over, err = RecordOvertime(ctx, tx, s.Organisation, g.Clock.Now(), s.Member, v, OvertimeAsk{Person: person, Date: date, Kind: a.Kind, Hours: hours, Branch: a.Branch, Note: a.Note})
		return err
	})
	return id.String(), over, err
}

func (g TimeService) Overtime(ctx context.Context, s api.Session, sc api.LeaveScope, state string) ([]api.OvertimeRecord, error) {
	out := []api.OvertimeRecord{}
	err := g.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := Overtime(ctx, tx, scopeOf(s, sc, people.Day(g.Clock.Now())), state)
		for _, r := range rows {
			out = append(out, api.OvertimeRecord{ID: r.ID.String(), PersonID: r.Person.String(), Person: r.PersonName, Date: day(r.Date), Kind: r.Kind, Hours: r.Hours.String(), Branch: r.Branch,
				OverLimit: r.OverLimit, Note: r.Note, State: r.State, Reason: r.Reason, Used: r.UsedAt != nil})
		}
		return err
	})
	return out, err
}

func (g TimeService) DecideOvertime(ctx context.Context, s api.Session, sc api.LeaveScope, id uuid.UUID, approve bool, reason string) error {
	return g.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		// The record must be in the caller's scope: a manager decides on their reports' only.
		rows, err := Overtime(ctx, tx, scopeOf(s, sc, people.Day(g.Clock.Now())), "pending")
		if err != nil {
			return err
		}
		found := false
		for _, r := range rows {
			found = found || r.ID == id
		}
		if !found {
			return &leave.Refusal{Code: leave.CodeNotFound, Message: "There is no such overtime record waiting in your view."}
		}
		return DecideOvertime(ctx, tx, s.Organisation, g.Clock.Now(), s.Member, id, approve, reason)
	})
}

func (g TimeService) PayInputs(ctx context.Context, s api.Session, to time.Time) ([]api.PayInput, error) {
	out := []api.PayInput{}
	err := g.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := OpenPayInputs(ctx, tx, to)
		for _, r := range rows {
			out = append(out, api.PayInput{ID: r.ID.String(), PersonID: r.Person.String(), Person: r.PersonName, Kind: r.Kind, Quantity: r.Quantity.String(), Unit: r.Unit,
				From: day(r.From), To: day(r.To), Source: r.SourceType, SourceID: r.SourceID.String()})
		}
		return err
	})
	return out, err
}

func (g TimeService) DecideUnpaid(ctx context.Context, s api.Session, notification uuid.UUID, treatment string) (string, error) {
	var days leave.Days
	err := g.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		days, err = DecideUnpaidSick(ctx, tx, s.Organisation, g.Clock.Now(), s.Member, notification, treatment)
		return err
	})
	return days.String(), err
}

func (g TimeService) Encash(ctx context.Context, s api.Session, person uuid.UUID, year int, days string, on time.Time, note string) error {
	n, err := leave.ParseDays(days)
	if err != nil {
		return &leave.Refusal{Code: leave.CodeInvalid, Message: "days is not days with at most two decimal places: write it as 2.00."}
	}
	return g.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := PersonOn(ctx, tx, person, g.Clock.Now()); err != nil {
			return err
		}
		_, err := EncashLeave(ctx, tx, s.Organisation, g.Clock.Now(), s.Member, person, year, n, on, note)
		return err
	})
}

func (g TimeService) DraftAccrual(ctx context.Context, s api.Session, month time.Time) (int, error) {
	var n int
	err := g.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		n, err = DraftLeaveAccrual(ctx, tx, s.Organisation, g.Clock.Now(), s.Member, month)
		return err
	})
	return n, err
}

func (g TimeService) Accruals(ctx context.Context, s api.Session, month time.Time) ([]api.AccrualLine, error) {
	out := []api.AccrualLine{}
	err := g.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := LeaveAccruals(ctx, tx, month)
		for _, r := range rows {
			out = append(out, api.AccrualLine{ID: r.ID.String(), PersonID: r.Person.String(), Person: r.PersonName, State: r.State, CarriedDays: r.Carried.String(), AnnualLeft: r.AnnualLeft.String()})
		}
		return err
	})
	return out, err
}

func (g TimeService) ApproveAccrual(ctx context.Context, s api.Session, month time.Time) (int, error) {
	var n int
	err := g.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		n, err = ApproveLeaveAccrual(ctx, tx, s.Organisation, g.Clock.Now(), s.Member, month)
		return err
	})
	return n, err
}

func (g TimeService) Availability(ctx context.Context, s api.Session, person uuid.UUID, from, to time.Time) (api.Availability, error) {
	var out api.Availability
	err := g.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := PersonOn(ctx, tx, person, g.Clock.Now()); err != nil {
			return err
		}
		v, ok := g.Packs.inForce("sg-people", from)
		if !ok {
			return &leave.Refusal{Code: leave.CodeInvalid, Message: "No country pack covers those days: choose days from 2026 on."}
		}
		a, err := AvailableHours(ctx, tx, v, person, from, to)
		out = api.Availability{WorkingDays: a.WorkingDays.String(), AwayDays: a.AwayDays.String(), HoursPerDay: a.HoursPerDay.String(), Hours: a.Hours.String()}
		return err
	})
	return out, err
}
