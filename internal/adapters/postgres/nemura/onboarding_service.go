package nemura

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/onboarding"
	"github.com/cognizhi/fyniq/internal/ports"
)

// OnboardingService is the joiner's task and the leaver's dates behind the API (NM-ONB-02, NM-ONB-03).
type OnboardingService struct {
	Store *Store
	Clock ports.Clock
	Packs *PacksService
}

var _ api.Onboarding = OnboardingService{}

func (o OnboardingService) in(ctx context.Context, s api.Session, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return o.Store.InOrg(ctx, s.Organisation, fn)
}

func (o OnboardingService) Settings(ctx context.Context, s api.Session) (bool, error) {
	var on bool
	err := o.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		on, err = InviteToFyniq(ctx, tx)
		return err
	})
	return on, err
}

func (o OnboardingService) SetSettings(ctx context.Context, s api.Session, inviteToFyniq bool) error {
	return o.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		return SetInviteToFyniq(ctx, tx, s.Organisation, o.Clock.Now(), s.Member, inviteToFyniq)
	})
}

func (o OnboardingService) Tasks(ctx context.Context, s api.Session) ([]api.JoinTask, error) {
	out := []api.JoinTask{}
	err := o.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := JoinTasks(ctx, tx)
		for _, r := range rows {
			out = append(out, api.JoinTask{ID: r.ID.String(), PersonID: r.Person.String(), Person: r.PersonName, Start: day(r.Start), State: r.State, Raised: r.RaisedAt.UTC().Format(time.RFC3339), Note: r.Note})
		}
		return err
	})
	return out, err
}

func (o OnboardingService) DecideTask(ctx context.Context, s api.Session, id uuid.UUID, state, note string) error {
	return o.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		return DecideJoinTask(ctx, tx, s.Organisation, o.Clock.Now(), s.Member, id, state, note)
	})
}

func (o OnboardingService) view(ctx context.Context, tx pgx.Tx, r OffboardingRow) (api.Offboarding, error) {
	now := o.Clock.Now()
	out := api.Offboarding{ID: r.ID.String(), PersonID: r.Person.String(), Person: r.PersonName, Kind: r.Kind, NoticeGiven: dayStr(r.NoticeGiven), LastDay: day(r.LastDay),
		RetrenchmentNotified: dayStr(r.Retrenchment), EquipmentReturned: dayStr(r.EquipmentReturned), ExitInterview: dayStr(r.ExitInterview), FyniqAccessRemoved: dayStr(r.FyniqAccessRemoved),
		NemuraAccessNarrows: day(onboarding.AccessNarrowsOn(r.LastDay))}
	// The service letter is the letter type that states the facts of a person's service; its state is the latest request's.
	var st *string
	if err := tx.QueryRow(ctx, "SELECT (SELECT state FROM nemura.letter_request WHERE person_id = $1 AND type_key = 'service_letter' ORDER BY requested_at DESC LIMIT 1)", r.Person).Scan(&st); err != nil {
		return out, err
	}
	out.ServiceLetter = "none"
	if st != nil {
		out.ServiceLetter = *st
	}
	if o.Packs != nil {
		year := r.LastDay.Year()
		if v, ok := o.Packs.inForce("sg-people", startOf(year)); ok {
			rules, _, _, found, err := PolicyOn(ctx, tx, startOf(year))
			if err != nil {
				return out, err
			}
			var days []int
			if found {
				days = rules.AnnualLeaveDays
			}
			if st, err := LeaveSettlement(ctx, tx, r.Person, r.LastDay, now, v, days); err == nil {
				out.Settlement = &api.Settlement{Year: st.Year, Entitled: st.Entitled.String(), Taken: st.Taken.String(), Booked: st.Booked.String(), Untaken: st.Untaken.String(), Basis: st.Basis}
			}
		}
	}
	return out, nil
}

func (o OnboardingService) Offboardings(ctx context.Context, s api.Session) ([]api.Offboarding, error) {
	out := []api.Offboarding{}
	err := o.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := Offboardings(ctx, tx)
		if err != nil {
			return err
		}
		for _, r := range rows {
			v, err := o.view(ctx, tx, r)
			if err != nil {
				return err
			}
			out = append(out, v)
		}
		return nil
	})
	return out, err
}

func (o OnboardingService) Offboarding(ctx context.Context, s api.Session, person uuid.UUID) (api.Offboarding, bool, error) {
	var out api.Offboarding
	var ok bool
	err := o.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		r, found, err := OffboardingOf(ctx, tx, person)
		if err != nil || !found {
			return err
		}
		ok = true
		out, err = o.view(ctx, tx, r)
		return err
	})
	return out, ok, err
}

func parseDay(name, v string) (*time.Time, error) {
	if v == "" {
		return nil, nil
	}
	t, err := time.Parse(time.DateOnly, v)
	if err != nil {
		return nil, &onboarding.Refusal{Code: onboarding.CodeInvalid, Message: name + " is not a date: write it as 2026-11-30."}
	}
	return &t, nil
}

func (o OnboardingService) SetOffboarding(ctx context.Context, s api.Session, person uuid.UUID, in api.OffboardingInput) error {
	var l onboarding.Leaving
	var err error
	l.Kind = in.Kind
	last, err := parseDay("last_day", in.LastDay)
	if err != nil || last == nil {
		if err == nil {
			err = &onboarding.Refusal{Code: onboarding.CodeInvalid, Message: "The last day is required: enter the last day of employment, the notice period included."}
		}
		return err
	}
	l.LastDay = *last
	for _, f := range []struct {
		name, v string
		to      **time.Time
	}{{"notice_given_on", in.NoticeGiven, &l.NoticeGiven}, {"retrenchment_notified_on", in.RetrenchmentNotified, &l.Retrenchment}, {"equipment_returned_on", in.EquipmentReturned, &l.EquipmentReturned},
		{"exit_interview_on", in.ExitInterview, &l.ExitInterview}, {"fyniq_access_removed_on", in.FyniqAccessRemoved, &l.FyniqAccessRemoved}} {
		if *f.to, err = parseDay(f.name, f.v); err != nil {
			return err
		}
	}
	return o.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		_, err := SetOffboarding(ctx, tx, s.Organisation, o.Clock.Now(), s.Member, person, l)
		return err
	})
}

func (o OnboardingService) CancelOffboarding(ctx context.Context, s api.Session, person uuid.UUID) error {
	return o.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		return CancelOffboarding(ctx, tx, s.Organisation, o.Clock.Now(), s.Member, person)
	})
}
