package nemura

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/leave"
	"github.com/cognizhi/fyniq/internal/ports"
)

// GovernmentService is government-paid leave behind the API (NM-LV-07, NM-LV-14): each call is one transaction for the caller's organisation, with the audit events
// of its changes. It tracks claims and moves no money.
type GovernmentService struct {
	Store *Store
	Clock ports.Clock
	Packs *PacksService
}

var _ api.Government = GovernmentService{}

func (g GovernmentService) in(ctx context.Context, s api.Session, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return g.Store.InOrg(ctx, s.Organisation, fn)
}

func (g GovernmentService) Record(ctx context.Context, s api.Session, person uuid.UUID, a api.GovAsk) (string, error) {
	var id uuid.UUID
	err := g.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		from, err := time.Parse(time.DateOnly, a.From)
		if err != nil {
			return &leave.Refusal{Code: leave.CodeInvalid, Message: "from is not a date: write it as 2026-06-10."}
		}
		to, err := time.Parse(time.DateOnly, a.To)
		if err != nil {
			return &leave.Refusal{Code: leave.CodeInvalid, Message: "to is not a date: write it as 2026-07-07."}
		}
		dep, err := uuid.Parse(a.DependantID)
		if err != nil {
			return &leave.Refusal{Code: leave.CodeInvalid, Message: "dependant_id is not an ID: choose the child from the person's dependants."}
		}
		v, ok := g.Packs.inForce("sg-people", from)
		if !ok {
			return &leave.Refusal{Code: leave.CodeInvalid, Message: "No country pack covers those dates: choose days from 2026 on."}
		}
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM nemura.person WHERE id = $1)", person).Scan(&exists); err != nil || !exists {
			return &leave.Refusal{Code: leave.CodeNotFound, Message: "There is no such person in this organisation."}
		}
		id, _, err = RecordGovLeave(ctx, tx, s.Organisation, g.Clock.Now(), s.Member, v, GovAsk{Person: person, Dependant: dep, Scheme: a.Scheme, ChildOrder: a.ChildOrder, From: from, To: to})
		return err
	})
	return id.String(), err
}

func (g GovernmentService) List(ctx context.Context, s api.Session) ([]api.GovBlock, error) {
	out := []api.GovBlock{}
	err := g.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := GovLeaves(ctx, tx, g.Packs.find)
		for _, r := range rows {
			b := api.GovBlock{ID: r.ID.String(), PersonID: r.Person.String(), Person: r.PersonName, Scheme: r.Scheme, From: day(r.From), To: day(r.To), Days: r.Days, Deadline: day(r.Deadline), Claims: []api.GovClaim{}}
			if r.GuideMaximum != nil {
				b.Guide = r.GuideMaximum.String()
			}
			for _, c := range r.Claims {
				b.Claims = append(b.Claims, api.GovClaim{ID: c.ID.String(), Amount: c.Amount.String(), Received: c.Received.String(), FiledOn: day(c.FiledOn), Reference: c.Reference, State: c.State(), OverGuide: c.OverGuide})
			}
			out = append(out, b)
		}
		return err
	})
	return out, err
}

func (g GovernmentService) File(ctx context.Context, s api.Session, block uuid.UUID, amount string, filedOn time.Time, reference string) (string, bool, error) {
	cents, err := leave.ParseCents(amount)
	if err != nil {
		return "", false, err
	}
	var id uuid.UUID
	var over bool
	err = g.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		id, over, err = FileGovClaim(ctx, tx, s.Organisation, g.Clock.Now(), s.Member, block, g.Packs.find, cents, filedOn, reference)
		return err
	})
	return id.String(), over, err
}

func (g GovernmentService) Event(ctx context.Context, s api.Session, claim uuid.UUID, kind, amount string, on time.Time, note string) error {
	var cents leave.Cents
	if amount != "" {
		var err error
		if cents, err = leave.ParseCents(amount); err != nil {
			return err
		}
	}
	return g.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		_, err := RecordClaimEvent(ctx, tx, s.Organisation, g.Clock.Now(), s.Member, claim, kind, cents, on, note)
		return err
	})
}
