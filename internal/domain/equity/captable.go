package equity

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Move is a holding movement as the cap table reads it: shares into a holding, or out of it when negative, from a day.
type Move struct {
	ID          uuid.UUID
	Key         HoldingKey
	Shares      apd.Decimal
	On          time.Time
	Transaction *uuid.UUID // the equity transaction it made effective; nil for an issue recorded outside one
}

// VotesOf is the votes per share of a holding's class on the cap table's day, and whether they are assumed: the shares
// of an account with no recorded class count one vote a share until it has one (design D7) [accountant to confirm].
type VotesOf func(k HoldingKey) (votes apd.Decimal, assumed bool, err error)

// CapLine is one holding on the cap table: a holder's shares of a class.
type CapLine struct {
	Key          HoldingKey
	Issued       apd.Decimal
	Votes        apd.Decimal
	FullyDiluted apd.Decimal
	// Ownership, Voting and Diluted are the line's percentages of the table's issued shares, votes and fully diluted
	// shares under RND-DISPLAY; "" when that total is zero.
	Ownership, Voting, Diluted string
	VotesAssumed               bool
	Moves                      []Move // the movements up to the day, oldest first: the drill-down
}

// CapHolder is a holder's lines together, with their percentages computed from the exact sums.
type CapHolder struct {
	Holder                      uuid.UUID
	Issued, Votes, FullyDiluted apd.Decimal
	Ownership, Voting, Diluted  string
}

// CapTable is the cap table at the end of a day (FR-EQ-20).
type CapTable struct {
	At                          time.Time
	Lines                       []CapLine   // by holder, account and class
	Holders                     []CapHolder // by holder
	Issued, Votes, FullyDiluted apd.Decimal
}

// TableAt is the cap table at the end of day, from moves alone (FR-EQ-20, design D7): every holding with shares, its
// votes (shares × the class's votes per share) and its fully diluted shares, which equal the issued ones until the
// instruments (FR-EQ-18) and the option pool (FR-EQ-19) add to them; and each one's percentages of the totals, exact
// and shown to 2 decimals half-up, never forced to sum to 100.00. It is never written, so it cannot be edited.
func TableAt(day time.Time, moves []Move, votes VotesOf) (CapTable, error) {
	out := CapTable{At: day}
	byKey := map[HoldingKey]*CapLine{}
	var keys []HoldingKey
	for _, m := range moves {
		if m.On.After(day) {
			continue
		}
		l, ok := byKey[m.Key]
		if !ok {
			l = &CapLine{Key: m.Key}
			byKey[m.Key] = l
			keys = append(keys, m.Key)
		}
		if _, err := apd.BaseContext.Add(&l.Issued, &l.Issued, &m.Shares); err != nil {
			return out, err
		}
		l.Moves = append(l.Moves, m)
	}
	slices.SortFunc(keys, func(a, b HoldingKey) int {
		return strings.Compare(a.Holder.String()+a.Account.String()+a.Class.String(), b.Holder.String()+b.Account.String()+b.Class.String())
	})
	holders := map[uuid.UUID]*CapHolder{}
	for _, k := range keys {
		l := byKey[k]
		if l.Issued.IsZero() {
			continue
		}
		v, assumed, err := votes(k)
		if err != nil {
			return out, err
		}
		l.VotesAssumed = assumed
		if _, err := apd.BaseContext.Mul(&l.Votes, &l.Issued, &v); err != nil {
			return out, err
		}
		l.FullyDiluted.Set(&l.Issued)
		slices.SortStableFunc(l.Moves, func(a, b Move) int { return a.On.Compare(b.On) })
		h, ok := holders[k.Holder]
		if !ok {
			h = &CapHolder{Holder: k.Holder}
			holders[k.Holder] = h
		}
		for _, sum := range [][2]*apd.Decimal{{&h.Issued, &l.Issued}, {&h.Votes, &l.Votes}, {&h.FullyDiluted, &l.FullyDiluted},
			{&out.Issued, &l.Issued}, {&out.Votes, &l.Votes}, {&out.FullyDiluted, &l.FullyDiluted}} {
			if _, err := apd.BaseContext.Add(sum[0], sum[0], sum[1]); err != nil {
				return out, err
			}
		}
		out.Lines = append(out.Lines, *l)
	}
	share := func(part, whole *apd.Decimal) (string, error) {
		if whole.IsZero() {
			return "", nil
		}
		return rounding.Ownership(part, whole)
	}
	var err error
	for i := range out.Lines {
		l := &out.Lines[i]
		if l.Ownership, err = share(&l.Issued, &out.Issued); err != nil {
			return out, err
		}
		if l.Voting, err = share(&l.Votes, &out.Votes); err != nil {
			return out, err
		}
		if l.Diluted, err = share(&l.FullyDiluted, &out.FullyDiluted); err != nil {
			return out, err
		}
	}
	for _, l := range out.Lines {
		h := holders[l.Key.Holder]
		if h == nil {
			continue
		}
		if h.Ownership, err = share(&h.Issued, &out.Issued); err != nil {
			return out, err
		}
		if h.Voting, err = share(&h.Votes, &out.Votes); err != nil {
			return out, err
		}
		if h.Diluted, err = share(&h.FullyDiluted, &out.FullyDiluted); err != nil {
			return out, err
		}
		out.Holders = append(out.Holders, *h)
		delete(holders, l.Key.Holder)
	}
	return out, nil
}

// singapore is the books' time zone: a class's rights apply from the day they were recorded.
var singapore = time.FixedZone("SGT", 8*3600)

// Moves are every holding movement with the holding it counts in: shares that name no class count under their
// account's first recorded class, or their account while it has none (design D2).
func Moves(ctx context.Context, u storage.UnitOfWork) ([]Move, error) {
	moved, err := u.Holdings().List(ctx)
	if err != nil {
		return nil, err
	}
	defaults := map[uuid.UUID]uuid.UUID{}
	out := make([]Move, 0, len(moved))
	for _, m := range moved {
		k := HoldingKey{Holder: m.HolderID, Account: m.ClassAccountID}
		if m.ClassID != nil {
			k.Class = *m.ClassID
		} else {
			def, ok := defaults[m.ClassAccountID]
			if !ok {
				d, err := DefaultClass(ctx, u, m.ClassAccountID)
				if err != nil {
					return nil, err
				}
				if d != nil {
					def = *d
				}
				defaults[m.ClassAccountID] = def
			}
			k.Class = def
		}
		x := Move{ID: m.ID, Key: k, On: m.EffectiveOn, Transaction: m.TransactionID}
		if _, _, err := x.Shares.SetString(string(m.Shares)); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, nil
}

// TableIn is the cap table at the end of day, from the register's movements, each class counting the votes per share
// of the version of its rights recorded by that day, or its first recorded version for a day before it.
func TableIn(ctx context.Context, u storage.UnitOfWork, day time.Time) (CapTable, error) {
	moves, err := Moves(ctx, u)
	if err != nil {
		return CapTable{}, err
	}
	classes, err := ShareClasses(ctx, u)
	if err != nil {
		return CapTable{}, err
	}
	return TableAt(day, moves, func(k HoldingKey) (apd.Decimal, bool, error) {
		var v apd.Decimal
		if k.Class == uuid.Nil {
			v.SetInt64(1)
			return v, true, nil
		}
		var in *ShareClass
		for i, x := range classes[k.Class] {
			if x.Doc.Status != storage.DocumentPosted {
				continue
			}
			if in == nil || x.RecordedAt.In(singapore).Format(time.DateOnly) <= day.Format(time.DateOnly) {
				in = &classes[k.Class][i].Value
			}
		}
		if in == nil {
			v.SetInt64(1)
			return v, true, nil
		}
		_, _, err := v.SetString(in.VotesPerShare)
		return v, false, err
	})
}
