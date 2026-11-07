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
	Kind        string     // issue, transfer, class_conversion or reversal (storage.Movement*)
	ShareIssue  *uuid.UUID // the share issue an issue's movement records
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
	// Options are the options the recorded grants made on or before the day granted, and Unallocated what the option
	// pool in force leaves to grant (FR-EQ-19); both count in FullyDiluted.
	Options, Unallocated apd.Decimal
}

// Option is a grant's options as the cap table counts them: the holding they would be shares of, from the grant date,
// or negative from the day an event forfeited them or let them expire.
type Option struct {
	Key    HoldingKey
	Number apd.Decimal
	On     time.Time
}

// Dilution is what the cap table counts fully diluted besides the shares held (FR-EQ-19, design D9): the options of
// each recorded grant and the pool's unallocated options. The instruments (FR-EQ-18) count only once converted.
type Dilution struct {
	Options []Option
	Pool    func(day time.Time) (unallocated apd.Decimal, err error) // nil while no pool is recorded
}

// TableAt is the cap table at the end of day, from moves alone (FR-EQ-20, design D7): every holding with shares, its
// votes (shares × the class's votes per share) and its fully diluted shares; and each one's percentages of the
// totals, exact and shown to 2 decimals half-up, never forced to sum to 100.00. It is never written, so it cannot be
// edited.
func TableAt(day time.Time, moves []Move, votes VotesOf) (CapTable, error) {
	return DilutedAt(day, moves, votes, Dilution{})
}

// DilutedAt is the cap table at the end of day with what dilutes it (FR-EQ-19, FR-EQ-20): each holding's fully diluted
// shares are its shares and the options granted over them by then, a holder with options and no shares has a line of
// its own, and the day's fully diluted total adds what the pool leaves to grant.
func DilutedAt(day time.Time, moves []Move, votes VotesOf, d Dilution) (CapTable, error) {
	out := CapTable{At: day}
	byKey := map[HoldingKey]*CapLine{}
	var keys []HoldingKey
	line := func(k HoldingKey) *CapLine {
		l, ok := byKey[k]
		if !ok {
			l = &CapLine{Key: k}
			byKey[k] = l
			keys = append(keys, k)
		}
		return l
	}
	for _, m := range moves {
		if m.On.After(day) {
			continue
		}
		l := line(m.Key)
		if _, err := apd.BaseContext.Add(&l.Issued, &l.Issued, &m.Shares); err != nil {
			return out, err
		}
		l.Moves = append(l.Moves, m)
	}
	optioned := map[HoldingKey]*apd.Decimal{}
	for _, o := range d.Options {
		if o.On.After(day) {
			continue
		}
		line(o.Key)
		n, ok := optioned[o.Key]
		if !ok {
			n = new(apd.Decimal)
			optioned[o.Key] = n
		}
		for _, sum := range []*apd.Decimal{n, &out.Options} {
			if _, err := apd.BaseContext.Add(sum, sum, &o.Number); err != nil {
				return out, err
			}
		}
	}
	if d.Pool != nil {
		var err error
		if out.Unallocated, err = d.Pool(day); err != nil {
			return out, err
		}
	}
	slices.SortFunc(keys, func(a, b HoldingKey) int {
		return strings.Compare(a.Holder.String()+a.Account.String()+a.Class.String(), b.Holder.String()+b.Account.String()+b.Class.String())
	})
	holders := map[uuid.UUID]*CapHolder{}
	for _, k := range keys {
		l := byKey[k]
		n := optioned[k]
		if l.Issued.IsZero() && (n == nil || n.IsZero()) {
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
		if n != nil {
			if _, err := apd.BaseContext.Add(&l.FullyDiluted, &l.FullyDiluted, n); err != nil {
				return out, err
			}
		}
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
	if _, err := apd.BaseContext.Add(&out.FullyDiluted, &out.FullyDiluted, &out.Unallocated); err != nil {
		return out, err
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
		x := Move{ID: m.ID, Key: k, On: m.EffectiveOn, Transaction: m.TransactionID, Kind: m.Kind, ShareIssue: m.ShareIssueID}
		if _, _, err := x.Shares.SetString(string(m.Shares)); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, nil
}

// DilutionIn is what dilutes the cap table in u (FR-EQ-19): each recorded grant's options under the holding they would
// be shares of (its class account's first recorded class, as Moves counts shares naming none), less those its recorded
// events forfeited or let expire from their day, and the pool.
func DilutionIn(ctx context.Context, u storage.UnitOfWork) (Dilution, error) {
	var d Dilution
	b, _, events, err := recordedOptions(ctx, u)
	if err != nil {
		return d, err
	}
	grants, err := all(ctx, u, storage.DocumentOptionGrant)
	if err != nil {
		return d, err
	}
	exercises, err := ExercisesIn(ctx, u)
	if err != nil {
		return d, err
	}
	defaults := map[uuid.UUID]uuid.UUID{}
	for _, doc := range grants {
		if doc.Status != storage.DocumentPosted {
			continue
		}
		g, err := ReadGrant(doc)
		if err != nil {
			return d, err
		}
		def, ok := defaults[g.ClassAccountID]
		if !ok {
			c, err := DefaultClass(ctx, u, g.ClassAccountID)
			if err != nil {
				return d, err
			}
			if c != nil {
				def = *c
			}
			defaults[g.ClassAccountID] = def
		}
		key := HoldingKey{Holder: g.HolderID, Account: g.ClassAccountID, Class: def}
		o := Option{Key: key, On: g.GrantedOn}
		if _, _, err := o.Number.SetString(g.Number); err != nil {
			return d, err
		}
		d.Options = append(d.Options, o)
		for _, e := range events[doc.ID] {
			gone := Option{Key: key, On: e.On}
			if _, _, err := gone.Number.SetString("-" + e.Number); err != nil {
				return d, err
			}
			d.Options = append(d.Options, gone)
		}
		for _, x := range exercises[doc.ID] { // shares from its effective day, options again from a reversal's
			gone := Option{Key: key, On: x.On}
			if _, _, err := gone.Number.SetString("-" + x.Number); err != nil {
				return d, err
			}
			d.Options = append(d.Options, gone)
			if x.Until != nil {
				back := Option{Key: key, On: *x.Until}
				if _, _, err := back.Number.SetString(x.Number); err != nil {
					return d, err
				}
				d.Options = append(d.Options, back)
			}
		}
	}
	if len(b.Pools) > 0 {
		d.Pool = func(day time.Time) (apd.Decimal, error) {
			p, _, err := b.At(day)
			return p.Unallocated, err
		}
	}
	return d, nil
}

// TableIn is the cap table at the end of day, from the register's movements and the option pool, each class counting
// the votes per share of the version of its rights recorded by that day, or its first recorded version for a day
// before it.
func TableIn(ctx context.Context, u storage.UnitOfWork, day time.Time) (CapTable, error) {
	moves, err := Moves(ctx, u)
	if err != nil {
		return CapTable{}, err
	}
	dilution, err := DilutionIn(ctx, u)
	if err != nil {
		return CapTable{}, err
	}
	classes, err := ShareClasses(ctx, u)
	if err != nil {
		return CapTable{}, err
	}
	return DilutedAt(day, moves, func(k HoldingKey) (apd.Decimal, bool, error) {
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
	}, dilution)
}
