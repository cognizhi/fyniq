package nemura

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/leave"
	"github.com/cognizhi/fyniq/internal/nemura/people"
	"github.com/cognizhi/fyniq/internal/ports"
)

// LeaveRequestService is leave requests behind the API (NM-LV-03, NM-LV-04): each call is one transaction for the caller's organisation, with the audit events of its
// changes. A person asks for themselves only; who reads and decides is the scope the API computed from the caller's roles.
type LeaveRequestService struct {
	Store *Store
	Clock ports.Clock
	Packs *PacksService
}

var _ api.LeaveRequests = LeaveRequestService{}

func (l LeaveRequestService) in(ctx context.Context, s api.Session, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return l.Store.InOrg(ctx, s.Organisation, fn)
}

func (l LeaveRequestService) ask(ctx context.Context, tx pgx.Tx, s api.Session, a api.LeaveAsk) (LeaveAsk, pack.Version, error) {
	var out LeaveAsk
	var v pack.Version
	from, err := time.Parse(time.DateOnly, a.From)
	if err != nil {
		return out, v, &leave.Refusal{Code: leave.CodeInvalid, Message: "from is not a date: write it as 2026-04-06."}
	}
	to, err := time.Parse(time.DateOnly, a.To)
	if err != nil {
		return out, v, &leave.Refusal{Code: leave.CodeInvalid, Message: "to is not a date: write it as 2026-04-10."}
	}
	if err := tx.QueryRow(ctx, "SELECT id FROM nemura.person WHERE member_id = $1", s.Member).Scan(&out.Person); err != nil {
		return out, v, &people.Refusal{Code: people.CodeNotFound, Message: "Your login is linked to no person record: ask the people team to link it."}
	}
	var ok bool
	if v, ok = l.Packs.inForce("sg-people", from); !ok {
		return out, v, &leave.Refusal{Code: leave.CodeInvalid, Message: "No country pack covers those dates: choose days from 2026 on."}
	}
	out.Type, out.From, out.To, out.StartHalf, out.EndHalf, out.Note = a.Type, from, to, a.StartHalf, a.EndHalf, a.Note
	return out, v, nil
}

func (l LeaveRequestService) Preview(ctx context.Context, s api.Session, a api.LeaveAsk) (api.RequestPreview, error) {
	out := api.RequestPreview{Counts: []api.DayCost{}, Away: []api.AwayPerson{}}
	err := l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		ask, v, err := l.ask(ctx, tx, s, a)
		if err != nil {
			return err
		}
		pv, err := PreviewLeave(ctx, tx, v, ask, people.Day(l.Clock.Now()))
		if err != nil {
			return err
		}
		out.Days, out.Left, out.Enough, out.Basis = pv.Charge.Days.String(), pv.Left.String(), pv.Enough, pv.Basis
		for _, c := range pv.Charge.Counted {
			out.Counts = append(out.Counts, api.DayCost{Date: c.Date, Cost: c.Cost.String()})
		}
		for _, k := range pv.Charge.Skipped {
			out.Counts = append(out.Counts, api.DayCost{Date: k.Date, Cost: "0.00", Why: k.Why})
		}
		sortCosts(out.Counts)
		for _, w := range pv.Away {
			out.Away = append(out.Away, api.AwayPerson{Name: w.Name, From: day(w.From), To: day(w.To)})
		}
		return nil
	})
	return out, err
}

func sortCosts(c []api.DayCost) {
	for i := 1; i < len(c); i++ {
		for j := i; j > 0 && c[j].Date < c[j-1].Date; j-- {
			c[j], c[j-1] = c[j-1], c[j]
		}
	}
}

func (l LeaveRequestService) Apply(ctx context.Context, s api.Session, a api.LeaveAsk) (string, string, error) {
	var id uuid.UUID
	var ch leave.Charge
	err := l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		ask, v, err := l.ask(ctx, tx, s, a)
		if err != nil {
			return err
		}
		id, ch, err = ApplyLeave(ctx, tx, s.Organisation, l.Clock.Now(), s.Member, v, ask)
		return err
	})
	if err != nil {
		return "", "", err
	}
	return id.String(), ch.Days.String(), nil
}

func scopeOf(s api.Session, sc api.LeaveScope, on time.Time) LeaveScope {
	return LeaveScope{Member: s.Member, On: on, All: sc.All, Reports: sc.Reports}
}

func requestView(r LeaveRequestRow) api.LeaveRequest {
	out := api.LeaveRequest{ID: r.ID.String(), PersonID: r.Person.String(), Person: r.PersonName, Type: r.Type, From: day(r.From), To: day(r.To), StartHalf: r.StartHalf, EndHalf: r.EndHalf,
		Days: r.Days.String(), Note: r.Note, State: r.State, Requested: r.RequestedAt.UTC().Format(time.RFC3339), Reason: r.Reason}
	if r.DecidedAt != nil {
		out.Decided = r.DecidedAt.UTC().Format(time.RFC3339)
	}
	return out
}

func (l LeaveRequestService) List(ctx context.Context, s api.Session, sc api.LeaveScope, state string) ([]api.LeaveRequest, error) {
	out := []api.LeaveRequest{}
	err := l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := LeaveRequests(ctx, tx, scopeOf(s, sc, people.Day(l.Clock.Now())), state, nil)
		for _, r := range rows {
			out = append(out, requestView(r))
		}
		return err
	})
	return out, err
}

func (l LeaveRequestService) Get(ctx context.Context, s api.Session, sc api.LeaveScope, id uuid.UUID) (api.LeaveRequest, error) {
	var out api.LeaveRequest
	err := l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		r, err := LeaveRequestFor(ctx, tx, scopeOf(s, sc, people.Day(l.Clock.Now())), id)
		out = requestView(r)
		return err
	})
	return out, err
}

func (l LeaveRequestService) Decide(ctx context.Context, s api.Session, sc api.LeaveScope, id uuid.UUID, approve bool, reason string) error {
	return l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		r, err := LeaveRequestFor(ctx, tx, scopeOf(s, sc, people.Day(l.Clock.Now())), id)
		if err != nil {
			return err
		}
		var own bool
		if err := tx.QueryRow(ctx, "SELECT member_id = $2 FROM nemura.person WHERE id = $1", r.Person, s.Member).Scan(&own); err == nil && own {
			return api.ErrOwnRequest
		}
		return DecideLeave(ctx, tx, s.Organisation, l.Clock.Now(), s.Member, id, approve, reason)
	})
}

func (l LeaveRequestService) Cancel(ctx context.Context, s api.Session, id uuid.UUID) error {
	return l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		// Only the person's own: a request outside the caller's own is not found.
		if _, err := LeaveRequestFor(ctx, tx, LeaveScope{Member: s.Member, On: people.Day(l.Clock.Now())}, id); err != nil {
			return err
		}
		return CancelLeave(ctx, tx, s.Organisation, l.Clock.Now(), s.Member, id, people.Day(l.Clock.Now()))
	})
}

func (l LeaveRequestService) Settle(ctx context.Context, s api.Session, on time.Time) (int, string, error) {
	var n int
	var days leave.Days
	err := l.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		n, days, err = SettleLeave(ctx, tx, s.Organisation, l.Clock.Now(), s.Member, on)
		return err
	})
	return n, days.String(), err
}
