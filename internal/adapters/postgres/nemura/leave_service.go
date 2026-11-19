package nemura

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/leave"
	"github.com/cognizhi/fyniq/internal/nemura/people"
	"github.com/cognizhi/fyniq/internal/ports"
)

// LeaveService is leave behind the API (NM-LV-01, NM-LV-02, NM-LV-13, NM-TIM-01): each call is one transaction for the caller's organisation, with the audit
// events of its changes. The statute comes from the packs, the company's own days from its policy.
type LeaveService struct {
	Store *Store
	Clock ports.Clock
	Packs *PacksService
}

var _ api.Leave = LeaveService{}

func (l LeaveService) in(ctx context.Context, s api.Session, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return l.Store.InOrg(ctx, s.Organisation, fn)
}

func startOf(year int) time.Time { return time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC) }

// rules are the pack in force at the start of a leave year and the company's annual days from its policy then, or a plain refusal when no pack covers the year.
func (l LeaveService) rules(ctx context.Context, tx pgx.Tx, year int) (pack.Version, []int, error) {
	v, ok := l.Packs.inForce("sg-people", startOf(year))
	if !ok {
		return v, nil, &leave.Refusal{Code: leave.CodeInvalid, Message: fmt.Sprintf("No country pack covers %d: choose a year from 2026 on.", year)}
	}
	r, _, _, found, err := PolicyOn(ctx, tx, startOf(year))
	if err != nil || !found {
		return v, nil, err
	}
	return v, r.AnnualLeaveDays, nil
}

func balanceOf(b leave.Balance) api.LeaveBalance {
	return api.LeaveBalance{Entitled: b.Entitled.String(), Taken: b.Taken.String(), Booked: b.Booked.String(), Expired: b.Expired.String(), Encashed: b.Encashed.String(), Left: b.Left.String()}
}

func (l LeaveService) Own(ctx context.Context, s api.Session) (uuid.UUID, error) {
	var id uuid.UUID
	err := l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		err := tx.QueryRow(ctx, "SELECT id FROM nemura.person WHERE member_id = $1", s.Member).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return &people.Refusal{Code: people.CodeNotFound, Message: "Your login is linked to no person record: ask the people team to link it."}
		}
		return err
	})
	return id, err
}

func (l LeaveService) View(ctx context.Context, s api.Session, id uuid.UUID, year int) (api.LeaveView, error) {
	view := api.LeaveView{PersonID: id.String(), Year: year, Types: []api.LeaveType{}}
	err := l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		p, err := PersonOn(ctx, tx, id, l.Clock.Now())
		if err != nil {
			return err
		}
		on := people.Day(l.Clock.Now())
		if end := time.Date(year, time.December, 31, 0, 0, 0, 0, time.UTC); on.After(end) {
			on = end
		}
		if on.Before(p.Start) {
			on = p.Start
		}
		if wp, ok, err := WorkingPatternOn(ctx, tx, id, on); err != nil {
			return err
		} else if ok {
			var from time.Time
			if err := tx.QueryRow(ctx, "SELECT effective_from FROM nemura.working_pattern WHERE person_id = $1 AND effective_from <= $2 AND (effective_to IS NULL OR effective_to > $2)", id, day(on)).Scan(&from); err != nil {
				return err
			}
			view.Pattern = &api.WorkingPattern{From: day(from), Weekdays: wp.Weekdays, HoursPerWeek: fmt.Sprintf("%d.%d", wp.HoursPerWeek/10, wp.HoursPerWeek%10)}
		}
		entries, err := LeaveEntries(ctx, tx, id, AnnualLeave, year)
		if err != nil {
			return err
		}
		t := api.LeaveType{Key: AnnualLeave, Name: "Annual leave", Counting: "working_days", Balance: balanceOf(leave.Sum(entries)), Basis: []string{}, Sources: []string{}}
		for _, e := range entries {
			if e.Expires != "" && (t.CarryEnd == "" || e.Expires > t.CarryEnd) {
				t.CarryEnd = e.Expires
			}
		}
		v, policyDays, err := l.rules(ctx, tx, year)
		if err == nil {
			var g leave.Entitlement
			if g, err = AnnualEntitlement(ctx, tx, id, year, l.Clock.Now(), v, policyDays); err == nil {
				t.Basis, t.Sources = g.Basis, g.Sources
			}
		}
		switch e := err.(type) {
		case nil:
		case *people.Refusal, *leave.Refusal:
			t.Basis = []string{e.Error()[len(refusalCode(e))+2:]}
		default:
			return err
		}
		if len(entries) > 0 || len(t.Sources) > 0 {
			view.Types = append(view.Types, t)
		}
		return nil
	})
	return view, err
}

func refusalCode(err error) string {
	switch e := err.(type) {
	case *people.Refusal:
		return e.Code
	case *leave.Refusal:
		return e.Code
	}
	return ""
}

func (l LeaveService) SetPattern(ctx context.Context, s api.Session, id uuid.UUID, from time.Time, p leave.Pattern) error {
	return l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := PersonOn(ctx, tx, id, l.Clock.Now()); err != nil {
			return err
		}
		return SetWorkingPattern(ctx, tx, s.Organisation, l.Clock.Now(), s.Member, id, from, p)
	})
}

func (l LeaveService) Grant(ctx context.Context, s api.Session, id uuid.UUID, year int) (api.LeaveGrant, error) {
	var out api.LeaveGrant
	err := l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		v, policyDays, err := l.rules(ctx, tx, year)
		if err != nil {
			return err
		}
		g, wrote, err := GrantAnnualLeave(ctx, tx, s.Organisation, l.Clock.Now(), s.Member, id, year, v, policyDays)
		out = api.LeaveGrant{Days: g.Days.String(), Written: wrote.String(), Basis: g.Basis, Sources: g.Sources}
		return err
	})
	if out.Basis == nil {
		out.Basis, out.Sources = []string{}, []string{}
	}
	return out, err
}

func (l LeaveService) CarryForward(ctx context.Context, s api.Session, id uuid.UUID, year int) (string, error) {
	var carried leave.Days
	err := l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := PersonOn(ctx, tx, id, l.Clock.Now()); err != nil {
			return err
		}
		var err error
		carried, err = CarryForwardLeave(ctx, tx, s.Organisation, l.Clock.Now(), s.Member, id, year, leave.DefaultCarryMonths)
		return err
	})
	return carried.String(), err
}
