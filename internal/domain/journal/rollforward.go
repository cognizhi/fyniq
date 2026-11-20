package journal

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// RollRow is one counterparty's line of a deferral roll-forward (PRD-001 FR-RPT-13; p2-reporting-planning design D11): what its schedules held at the start
// of the run, what was added, what was released to the profit and loss, what was reversed or cancelled, and what they hold at the end.
// Closing = Opening + Added - Released + Reversed, exactly.
type RollRow struct {
	Party    uuid.UUID // the customer or supplier; uuid.Nil for a spread with none (a bank line)
	Opening  money.Money
	Added    money.Money // schedules whose document is dated inside the run
	Released money.Money // recognised inside the run: a deferred revenue release, or a prepayment's expense
	Reversed money.Money // reversed releases, credit notes, edits and cancellations: it moves the balance in either direction
	Closing  money.Money
}

// RollForward is a holding account's roll-forward over a run, with what the account holds in the ledger at the end (INV-07).
type RollForward struct {
	Holding string // ledger.RolePrepayments or ledger.RoleDeferredRevenue
	Account storage.Account
	Rows    []RollRow
	Total   RollRow
	// Ledger is the account's balance at the end of the run, read positive, from the balance service; Direct what documents put on it with
	// no schedule, Unexplained what is left of it (zero when it ties): the same reconciliation getSchedules gives.
	Ledger      money.Money
	Direct      money.Money
	Unexplained money.Money
}

type rollKey struct {
	doc  uuid.UUID
	line int
}

func (r *RollRow) add(f func(*RollRow) *money.Money, m money.Money) error {
	p := f(r)
	var err error
	*p, err = p.Add(m)
	return err
}

// partyOf is the customer or supplier a document names, or uuid.Nil.
func partyOf(doc storage.Document) uuid.UUID {
	var p struct {
		Customer string `json:"customer_id"`
		Supplier string `json:"supplier_id"`
	}
	if json.Unmarshal(doc.Payload, &p) != nil {
		return uuid.Nil
	}
	for _, s := range []string{p.Customer, p.Supplier} {
		if id, err := uuid.Parse(s); err == nil {
			return id
		}
	}
	return uuid.Nil
}

// DeferralRollForward works out the roll-forward of deferred revenue and of prepayments from the first day of a run to its last (FR-RPT-13): the schedules
// as they stood on the day before the run and on its last day (DeferralsAt), compared per spread line, so that every cent that moved is in a column.
func DeferralRollForward(ctx context.Context, u storage.UnitOfWork, from, to time.Time) ([]RollForward, error) {
	opening, err := DeferralsAt(ctx, u, from.AddDate(0, 0, -1))
	if err != nil {
		return nil, err
	}
	closing, err := DeferralsAt(ctx, u, to)
	if err != nil {
		return nil, err
	}
	var out []RollForward
	for _, h := range closing.Holdings {
		base := h.Reconciliation.Base.Control
		zero := money.Zero(base.Currency())
		before := map[rollKey]Schedule{}
		for _, s := range opening.Schedules {
			if s.Holding == h.Role {
				before[rollKey{s.Document.ID, s.Line}] = s
			}
		}
		after := map[rollKey]Schedule{}
		for _, s := range closing.Schedules {
			if s.Holding == h.Role {
				after[rollKey{s.Document.ID, s.Line}] = s
			}
		}
		keys := map[rollKey]bool{}
		for k := range before {
			keys[k] = true
		}
		for k := range after {
			keys[k] = true
		}
		byParty := map[uuid.UUID]*RollRow{}
		parties := func(doc storage.Document) *RollRow {
			p := partyOf(doc)
			if byParty[p] == nil {
				byParty[p] = &RollRow{Party: p, Opening: zero, Added: zero, Released: zero, Reversed: zero, Closing: zero}
			}
			return byParty[p]
		}
		for k := range keys {
			o, hadBefore := before[k]
			c, hasAfter := after[k]
			doc := c.Document
			if !hasAfter {
				doc = o.Document
			}
			row := parties(doc)
			oRem, cRem := zero, zero
			if hadBefore {
				oRem = o.Remaining
			}
			if hasAfter {
				cRem = c.Remaining
			}
			if err := row.add(func(r *RollRow) *money.Money { return &r.Opening }, oRem); err != nil {
				return nil, err
			}
			if err := row.add(func(r *RollRow) *money.Money { return &r.Closing }, cRem); err != nil {
				return nil, err
			}
			switch {
			case !hadBefore && hasAfter: // a spread that came in during the run
				if err := row.add(func(r *RollRow) *money.Money { return &r.Added }, c.Total); err != nil {
					return nil, err
				}
				if err := row.add(func(r *RollRow) *money.Money { return &r.Released }, c.Recognised); err != nil {
					return nil, err
				}
			case hadBefore && !hasAfter: // cancelled or voided: what it still held leaves as a reversal
				if err := row.add(func(r *RollRow) *money.Money { return &r.Reversed }, oRem.Neg()); err != nil {
					return nil, err
				}
			default:
				dTotal, err := c.Total.Sub(o.Total)
				if err != nil {
					return nil, err
				}
				dRec, err := c.Recognised.Sub(o.Recognised)
				if err != nil {
					return nil, err
				}
				released := dRec // a release adds to Released; a reversed release, a credit note or an edit adjusts the balance (reversedOf)
				if dRec.Sign() < 0 {
					released = zero
				}
				reversed, err := reversedOf(dTotal, dRec)
				if err != nil {
					return nil, err
				}
				if err := row.add(func(r *RollRow) *money.Money { return &r.Released }, released); err != nil {
					return nil, err
				}
				if err := row.add(func(r *RollRow) *money.Money { return &r.Reversed }, reversed); err != nil {
					return nil, err
				}
			}
		}
		rf := RollForward{Holding: h.Role, Account: h.Account, Total: RollRow{Opening: zero, Added: zero, Released: zero, Reversed: zero, Closing: zero},
			Ledger: h.Reconciliation.Base.Control, Unexplained: h.Reconciliation.Base.Unexplained, Direct: zero}
		for _, it := range h.Direct {
			if rf.Direct, err = rf.Direct.Add(it.Carrying); err != nil {
				return nil, err
			}
		}
		for _, r := range byParty {
			rf.Rows = append(rf.Rows, *r)
			for _, f := range []struct {
				into *money.Money
				x    money.Money
			}{{&rf.Total.Opening, r.Opening}, {&rf.Total.Added, r.Added}, {&rf.Total.Released, r.Released}, {&rf.Total.Reversed, r.Reversed}, {&rf.Total.Closing, r.Closing}} {
				if *f.into, err = f.into.Add(f.x); err != nil {
					return nil, err
				}
			}
		}
		sort.Slice(rf.Rows, func(i, j int) bool { return rf.Rows[i].Party.String() < rf.Rows[j].Party.String() })
		out = append(out, rf)
	}
	return out, nil
}

// reversedOf is the part of a spread line's change that is not a release: its total changing (a credit note, an edit) and a release that was
// reversed. With the release as dRec (the change in what is recognised) and dTotal the change in what is scheduled, the balance moves by dTotal - dRec,
// of which a positive dRec is a release (the Released column), so the rest is dTotal when dRec is not negative, and dTotal - dRec when it is.
func reversedOf(dTotal, dRec money.Money) (money.Money, error) {
	if dRec.Sign() < 0 {
		return dTotal.Sub(dRec)
	}
	return dTotal, nil
}
