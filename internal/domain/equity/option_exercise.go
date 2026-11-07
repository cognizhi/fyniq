package equity

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Exercise is a grant's exercise as its standing counts it (FR-EQ-19, design D9): the shares an effective exercise
// transaction issued, from its effective day until the effective day of a reversal of it, if any.
type Exercise struct {
	On     time.Time
	Number string
	Until  *time.Time
}

// exercisedOn are the options exercises took by the end of day.
func exercisedOn(exercises []Exercise, day time.Time) (apd.Decimal, error) {
	var out apd.Decimal
	for _, x := range exercises {
		if x.On.After(day) || x.Until != nil && !x.Until.After(day) {
			continue
		}
		var n apd.Decimal
		if _, _, err := n.SetString(x.Number); err != nil {
			return out, err
		}
		if _, err := exact.Add(&out, &out, &n); err != nil {
			return out, err
		}
	}
	return out, nil
}

// ExercisesIn are the exercises of every grant in u, by the grant's document ID: each exercise transaction effective,
// with the effective day of the reversal that undid it, if one did.
func ExercisesIn(ctx context.Context, u storage.UnitOfWork) (map[uuid.UUID][]Exercise, error) {
	docs, err := Transactions(ctx, u)
	if err != nil {
		return nil, err
	}
	effective := map[uuid.UUID]time.Time{}
	reads := map[uuid.UUID]Transaction{}
	for _, d := range docs {
		t, err := ReadTransaction(d)
		if err != nil {
			return nil, err
		}
		if d.Status != storage.DocumentPosted || t.Kind != KindExercise && t.Kind != KindReversal {
			continue
		}
		moved, err := u.Holdings().OfTransaction(ctx, d.ID)
		if err != nil {
			return nil, err
		}
		if len(moved) > 0 {
			effective[d.ID], reads[d.ID] = moved[0].EffectiveOn, t
		}
	}
	undone := map[uuid.UUID]time.Time{}
	for id, t := range reads {
		if t.Kind == KindReversal {
			undone[t.ReversesID] = effective[id]
		}
	}
	out := map[uuid.UUID][]Exercise{}
	for _, d := range docs { // in the register's order
		t, ok := reads[d.ID]
		if !ok || t.Kind != KindExercise {
			continue
		}
		x := Exercise{On: effective[d.ID], Number: t.Shares}
		if until, ok := undone[d.ID]; ok {
			x.Until = &until
		}
		out[t.GrantID] = append(out[t.GrantID], x)
	}
	return out, nil
}

// FitsExercise is whether an exercise of n options on day fits the grant: on day and on each later day an event or
// another exercise of it is dated, the options exercised by then are at most those vested and kept (vested and not
// forfeited or expired). It returns the first day short and what was exercisable then.
func FitsExercise(g Grant, events []GrantEvent, exercises []Exercise, n apd.Decimal, day time.Time) (bool, time.Time, apd.Decimal, error) {
	days := []time.Time{day}
	for _, e := range events {
		if e.On.After(day) {
			days = append(days, e.On)
		}
	}
	for _, x := range exercises {
		if x.On.After(day) {
			days = append(days, x.On)
		}
	}
	slices.SortFunc(days, func(a, b time.Time) int { return a.Compare(b) })
	with := append(slices.Clip(exercises), Exercise{On: day, Number: n.Text('f')})
	for _, d := range days {
		was, err := StandingOf(g, events, exercises, d)
		if err != nil {
			return false, d, apd.Decimal{}, err
		}
		now, err := StandingOf(g, events, with, d)
		if err != nil {
			return false, d, apd.Decimal{}, err
		}
		if now.Exercisable.Sign() < 0 || now.Outstanding.Sign() < 0 {
			return false, d, was.Exercisable, nil
		}
	}
	return true, time.Time{}, apd.Decimal{}, nil
}

// checkExercise checks an exercise's grant and fills in what it takes from it (FR-EQ-19): a recorded grant, its holder
// and class (as given, or left out), its exercise price, which must be above zero, the amount, shares × that price
// under RND-LINE, and at most what is vested and not exercised on the day it was drafted.
func checkExercise(ctx context.Context, u storage.UnitOfWork, t *Transaction, s ledger.Settings, add func(field, msg, fix string)) error {
	if t.GrantID == uuid.Nil {
		add("grant_id", "the exercise names no grant", "Choose the recorded grant whose options are exercised")
		return nil
	}
	doc, err := u.Documents().Get(ctx, t.GrantID)
	switch {
	case errors.Is(err, ports.ErrNotFound) || err == nil && doc.Type != storage.DocumentOptionGrant:
		add("grant_id", "is not a grant of options of this organisation", "Choose the recorded grant whose options are exercised")
		return nil
	case err != nil:
		return err
	case doc.Status != storage.DocumentPosted:
		add("grant_id", "the grant is not recorded", "Record the grant first")
		return nil
	}
	g, err := ReadGrant(doc)
	if err != nil {
		return err
	}
	if t.HolderID == uuid.Nil {
		t.HolderID = g.HolderID
	} else if t.HolderID != g.HolderID {
		add("holder_id", "is not the grant's holder", "Leave it out: the shares go to the grant's holder")
	}
	if t.ClassAccountID == uuid.Nil {
		t.ClassAccountID = g.ClassAccountID
	} else if t.ClassAccountID != g.ClassAccountID {
		add("class_account_id", "is not the grant's class", "Leave it out: the shares are of the grant's class")
	}
	price, err := money.ParseUnitPrice(g.ExercisePrice, s.Base, "exercise_price")
	if err != nil {
		return err
	}
	if t.Price != "" {
		if given, err := money.ParseUnitPrice(t.Price, s.Base, "price"); err != nil || given.String() != price.String() {
			add("price", "is not the grant's exercise price, "+price.String(), "Leave it out: an exercise is at the grant's price")
		}
	}
	if price.Sign() <= 0 {
		add("grant_id", "the grant's exercise price is 0, so its exercise would issue shares for no cash",
			"Ask your accountant how to record it: Fyniq records an exercise for cash only [accountant to confirm]")
		return nil
	}
	t.Price = price.String()
	if !wholeShares.MatchString(t.Shares) {
		return nil // CheckTransaction says so
	}
	q, err := money.ParseQuantity(t.Shares, "shares")
	if err != nil {
		return err
	}
	amount, err := rounding.Line(q, price)
	if err != nil {
		return err
	}
	t.Amount = amount.String()
	events, err := GrantEventsOf(ctx, u, t.GrantID)
	if err != nil {
		return err
	}
	all, err := ExercisesIn(ctx, u)
	if err != nil {
		return err
	}
	var n apd.Decimal
	if _, _, err := n.SetString(t.Shares); err != nil {
		return err
	}
	on := t.Date
	if on.IsZero() {
		on = farFuture
	}
	if ok, day, left, err := FitsExercise(g, events, all[t.GrantID], n, on); err != nil {
		return err
	} else if !ok {
		add("shares", fmt.Sprintf("%s options are vested and not exercised on %s, fewer than the %s exercised", left.Text('f'), day.Format(time.DateOnly), t.Shares),
			"Exercise at most what is vested and not exercised")
	}
	return nil
}

// monthOn is the day k whole months after since: since's day of the month, or a shorter month's last day (the day a
// month passes, as Vested counts them).
func monthOn(since time.Time, k int) time.Time {
	first := time.Date(since.Year(), since.Month(), 1, 0, 0, 0, 0, since.Location()).AddDate(0, k, 0)
	last := first.AddDate(0, 1, -1).Day()
	return first.AddDate(0, 0, min(since.Day(), last)-1)
}

// NextVesting is a grant's next vesting after day (FR-EQ-19): the first month's day after it on which more of what the
// grant keeps vests, and how many; ok is false when nothing more will vest (all vested, or what is left forfeited or
// expired).
func NextVesting(g Grant, events []GrantEvent, exercises []Exercise, day time.Time) (on time.Time, n apd.Decimal, ok bool, err error) {
	now, err := StandingOf(g, events, exercises, day)
	if err != nil {
		return on, n, false, err
	}
	for k := 1; k <= max(g.Vesting.Months, 1); k++ {
		at := monthOn(g.GrantedOn, k)
		if !at.After(day) {
			continue
		}
		then, err := StandingOf(g, events, exercises, at)
		if err != nil {
			return on, n, false, err
		}
		if then.Vested.Cmp(&now.Vested) > 0 {
			_, err := exact.Sub(&n, &then.Vested, &now.Vested)
			return at, n, err == nil, err
		}
	}
	return on, n, false, nil
}

// GrantsStanding is the recorded grants' standing summed at the end of day (FR-EQ-19): what the pool screen shows of
// them, each grant made by the day counted with its events and exercises by then.
func GrantsStanding(ctx context.Context, u storage.UnitOfWork, day time.Time) (Standing, error) {
	var out Standing
	docs, err := all(ctx, u, storage.DocumentOptionGrant)
	if err != nil {
		return out, err
	}
	events, err := recordedEvents(ctx, u)
	if err != nil {
		return out, err
	}
	exercises, err := ExercisesIn(ctx, u)
	if err != nil {
		return out, err
	}
	for _, d := range docs {
		if d.Status != storage.DocumentPosted {
			continue
		}
		g, err := ReadGrant(d)
		if err != nil {
			return out, err
		}
		if g.GrantedOn.After(day) {
			continue
		}
		s, err := StandingOf(g, events[d.ID], exercises[d.ID], day)
		if err != nil {
			return out, err
		}
		for _, sum := range [][2]*apd.Decimal{{&out.Forfeited, &s.Forfeited}, {&out.Expired, &s.Expired}, {&out.Exercised, &s.Exercised},
			{&out.Outstanding, &s.Outstanding}, {&out.Vested, &s.Vested}, {&out.Exercisable, &s.Exercisable}} {
			if _, err := exact.Add(sum[0], sum[0], sum[1]); err != nil {
				return out, err
			}
		}
	}
	return out, nil
}
