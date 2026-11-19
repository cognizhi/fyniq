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

// FlexService is flexible work requests behind the API (NM-LV-10): each call is one transaction for the caller's organisation, with the audit events of its changes.
type FlexService struct {
	Store *Store
	Clock ports.Clock
	Packs *PacksService
}

var _ api.Flex = FlexService{}

func (f FlexService) in(ctx context.Context, s api.Session, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return f.Store.InOrg(ctx, s.Organisation, fn)
}

func (f FlexService) Request(ctx context.Context, s api.Session, a api.FlexAsk) (string, string, error) {
	var id uuid.UUID
	var due time.Time
	err := f.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		from, err := time.Parse(time.DateOnly, a.From)
		if err != nil {
			return &leave.Refusal{Code: leave.CodeInvalid, Message: "from is not a date: write it as 2027-01-04."}
		}
		var to *time.Time
		if a.To != "" {
			t, err := time.Parse(time.DateOnly, a.To)
			if err != nil {
				return &leave.Refusal{Code: leave.CodeInvalid, Message: "to is not a date: write it as 2027-06-30."}
			}
			to = &t
		}
		var person uuid.UUID
		if err := tx.QueryRow(ctx, "SELECT id FROM nemura.person WHERE member_id = $1", s.Member).Scan(&person); err != nil {
			return &people.Refusal{Code: people.CodeNotFound, Message: "Your login is linked to no person record: ask the people team to link it."}
		}
		v, ok := f.Packs.inForce("sg-people", f.Clock.Now())
		if !ok {
			return &leave.Refusal{Code: leave.CodeInvalid, Message: "No country pack covers today: ask whoever runs Nemura."}
		}
		id, due, err = RequestFlex(ctx, tx, s.Organisation, f.Clock.Now(), s.Member, v, FlexAsk{Person: person, Arrangement: a.Arrangement, From: from, To: to, Reason: a.Reason})
		return err
	})
	return id.String(), day(due), err
}

func (f FlexService) List(ctx context.Context, s api.Session, all bool) ([]api.FlexRequest, error) {
	out := []api.FlexRequest{}
	err := f.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := FlexRequests(ctx, tx, FlexScope{Member: s.Member, All: all})
		for _, r := range rows {
			v := api.FlexRequest{ID: r.ID.String(), PersonID: r.Person.String(), Person: r.PersonName, Arrangement: r.Arrangement, From: day(r.From), Reason: r.Reason, State: r.State,
				Received: day(r.Received), Due: day(r.Due), Outcome: r.Outcome, Reply: r.Reply}
			if r.To != nil {
				v.To = day(*r.To)
			}
			if r.RepliedAt != nil {
				v.RepliedAt = r.RepliedAt.UTC().Format(time.RFC3339)
			}
			out = append(out, v)
		}
		return err
	})
	return out, err
}

func (f FlexService) Reply(ctx context.Context, s api.Session, id uuid.UUID, outcome, written string) error {
	return f.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		return ReplyFlex(ctx, tx, s.Organisation, f.Clock.Now(), s.Member, id, outcome, written)
	})
}

func (f FlexService) Withdraw(ctx context.Context, s api.Session, id uuid.UUID) error {
	return f.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		// Only the person's own: a request outside it is not found.
		var own bool
		if err := tx.QueryRow(ctx, "SELECT coalesce(p.member_id = $2, false) FROM nemura.flex_request r JOIN nemura.person p ON p.organisation_id = r.organisation_id AND p.id = r.person_id WHERE r.id = $1", id, s.Member).Scan(&own); err != nil || !own {
			return &leave.Refusal{Code: leave.CodeNotFound, Message: "There is no such flexible work request in your view."}
		}
		return WithdrawFlex(ctx, tx, s.Organisation, f.Clock.Now(), s.Member, id)
	})
}
