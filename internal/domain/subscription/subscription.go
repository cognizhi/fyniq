// Package subscription is a customer's subscription contract and its dated amendments (PRD-001 FR-AR-15, rev 0.550; p2-customer-portal-subscriptions
// design D3). A contract and its amendments are records, not postings: invoices and credit notes still post through the existing rules. The monthly
// value in force on a day, the month-by-month schedule and the contracted MRR (FR-KPI-02) are derived from the records, never stored, so they
// cannot disagree with them. An amendment dated in a closed period is refused, and amendments only go forward in time, so a closed period's
// figures are never rewritten (FR-GL-12).
package subscription

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Stable codes (ENG-23).
const (
	CodeContact  = "AR-SUB-01" // the contract's contact is not an active customer
	CodeTerms    = "AR-SUB-02" // a name, currency, amount or term that is not valid
	CodeKind     = "AR-SUB-03" // the amendment's kind does not fit what the contract is on that day
	CodeDate     = "AR-SUB-04" // the amendment's day is before the contract starts, not after the last amendment, or in a closed period
	CodeAmount   = "AR-SUB-05" // the amendment's monthly value is not valid for its kind
	CodeReason   = "AR-SUB-06" // a reason that is empty or too long
	CodeNotFound = "AR-SUB-07"
)

// Error is a refusal in plain words.
type Error struct{ Code, Message, Fix string }

func (e *Error) Error() string { return e.Code + ": " + e.Message + " (" + e.Fix + ")" }

// The states of a contract on a day.
const (
	NotStarted = "not_started"
	Active     = "active"
	Ended      = "ended"
)

// State is what a contract is on a day: its state and the monthly value then (zero unless active).
type State struct {
	Status string
	MRR    money.Money
}

// On derives a contract's state on day from the contract and its amendments (any order). Before its start it is not started; from the start it is
// active at the contract's value; each amendment dated on or before the day then sets the value from its day (a cancellation ends it, a renewal
// starts it again); and unless a renewal has taken effect, the agreed end day is its last day.
func On(c storage.SubscriptionContract, ams []storage.SubscriptionAmendment, day time.Time) (State, error) {
	cur, err := money.Lookup(c.Currency)
	if err != nil {
		return State{}, err
	}
	zero := money.Zero(cur)
	if day.Before(c.StartsOn) {
		return State{NotStarted, zero}, nil
	}
	mrr, err := money.ParseStored(string(c.MRR), cur)
	if err != nil {
		return State{}, err
	}
	status, renewed := Active, false
	for _, a := range sorted(ams) {
		if a.EffectiveOn.After(day) {
			break
		}
		if mrr, err = money.ParseStored(string(a.MRR), cur); err != nil {
			return State{}, err
		}
		switch a.Kind {
		case storage.AmendCancel:
			status = Ended
		case storage.AmendRenew:
			status, renewed = Active, true
		default:
			status = Active
		}
	}
	if status == Active && !renewed && c.EndsOn != nil && day.After(*c.EndsOn) {
		status = Ended
	}
	if status == Ended {
		return State{Ended, zero}, nil
	}
	return State{status, mrr}, nil
}

func sorted(ams []storage.SubscriptionAmendment) []storage.SubscriptionAmendment {
	out := append([]storage.SubscriptionAmendment(nil), ams...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].EffectiveOn.Before(out[j-1].EffectiveOn); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// Month is one month of a contract's schedule.
type Month struct {
	Month     time.Time // the first day
	MRR       money.Money
	Status    string
	Amendment *storage.SubscriptionAmendment // the amendment taking effect in the month, if any (the last one)
}

// Schedule is the contract month by month from the month it starts through the month of through: the monthly value in force on each month's last day,
// which is the contract's contracted MRR at month end (FR-KPI-02).
func Schedule(c storage.SubscriptionContract, ams []storage.SubscriptionAmendment, through time.Time) ([]Month, error) {
	var out []Month
	first := time.Date(c.StartsOn.Year(), c.StartsOn.Month(), 1, 0, 0, 0, 0, time.UTC)
	for m := first; !m.After(through); m = m.AddDate(0, 1, 0) {
		last := m.AddDate(0, 1, -1)
		s, err := On(c, ams, last)
		if err != nil {
			return nil, err
		}
		row := Month{Month: m, MRR: s.MRR, Status: s.Status}
		for i := range ams {
			if !ams[i].EffectiveOn.Before(m) && !ams[i].EffectiveOn.After(last) {
				a := ams[i]
				if row.Amendment == nil || a.EffectiveOn.After(row.Amendment.EffectiveOn) {
					row.Amendment = &a
				}
			}
		}
		out = append(out, row)
	}
	return out, nil
}

// Total is the contracted MRR at one day: the monthly value of the contracts active then, by currency, and how many are.
type Total struct {
	Currency string
	MRR      money.Money
	Active   int
}

// ContractedMRR sums the monthly value of every contract active on day, by currency (FR-KPI-02's contracted MRR). Currencies are not converted: a
// base-currency total waits for the FX policy for KPIs (FR-KPI-04), so each currency is shown as it is.
func ContractedMRR(ctx context.Context, u storage.UnitOfWork, day time.Time) ([]Total, error) {
	var contracts []storage.SubscriptionContract
	after := uuid.Nil
	for {
		page, err := u.Subscriptions().List(ctx, uuid.Nil, after, 500)
		if err != nil {
			return nil, err
		}
		contracts = append(contracts, page...)
		if len(page) < 500 {
			break
		}
		after = page[len(page)-1].ID
	}
	all, err := u.Subscriptions().AllAmendments(ctx)
	if err != nil {
		return nil, err
	}
	by := map[uuid.UUID][]storage.SubscriptionAmendment{}
	for _, a := range all {
		by[a.ContractID] = append(by[a.ContractID], a)
	}
	return Totals(contracts, by, day)
}

// Totals is ContractedMRR over contracts and their amendments (by contract) already read: the monthly value of those active on day, by currency in the
// order each is first met, and how many.
func Totals(contracts []storage.SubscriptionContract, by map[uuid.UUID][]storage.SubscriptionAmendment, day time.Time) ([]Total, error) {
	var out []Total
	index := map[string]int{}
	for _, c := range contracts {
		s, err := On(c, by[c.ID], day)
		if err != nil {
			return nil, err
		}
		if s.Status != Active {
			continue
		}
		i, ok := index[c.Currency]
		if !ok {
			cur, _ := money.Lookup(c.Currency)
			index[c.Currency] = len(out)
			i = len(out)
			out = append(out, Total{Currency: c.Currency, MRR: money.Zero(cur)})
		}
		if out[i].MRR, err = out[i].MRR.Add(s.MRR); err != nil {
			return nil, err
		}
		out[i].Active++
	}
	return out, nil
}

// Request is a contract to record.
type Request struct {
	ContactID uuid.UUID
	Name      string
	Currency  string
	StartsOn  time.Time
	EndsOn    *time.Time
	MRR       string
}

// Make records a contract for an active customer.
func Make(ctx context.Context, u storage.UnitOfWork, r Request, now time.Time) (storage.SubscriptionContract, error) {
	c, err := u.Contacts().Get(ctx, r.ContactID)
	if err != nil || !c.Customer || !c.Active {
		return storage.SubscriptionContract{}, &Error{CodeContact, "A subscription is for an active customer.", "Pick a contact that is marked as a customer and is active"}
	}
	name := strings.TrimSpace(r.Name)
	if name == "" || len(name) > 140 {
		return storage.SubscriptionContract{}, &Error{CodeTerms, "A subscription needs a name of up to 140 characters.", "Say what the customer subscribes to"}
	}
	cur, err := money.Lookup(r.Currency)
	if err != nil {
		return storage.SubscriptionContract{}, &Error{CodeTerms, "The currency is not one Fyniq knows.", "Use a code such as SGD"}
	}
	mrr, err := money.Parse(r.MRR, cur)
	if err != nil || mrr.Sign() < 0 {
		return storage.SubscriptionContract{}, &Error{CodeTerms, "The monthly value is not an amount of zero or more in " + cur.Code + ".", "Enter what the customer pays each month, such as 100.00"}
	}
	if r.StartsOn.IsZero() || (r.EndsOn != nil && r.EndsOn.Before(r.StartsOn)) {
		return storage.SubscriptionContract{}, &Error{CodeTerms, "The start day is needed and the end day is not before it.", "Enter the day the subscription starts and, if agreed, ends"}
	}
	return u.Subscriptions().Create(ctx, storage.NewSubscriptionContract{ContactID: r.ContactID, Name: name, Currency: cur.Code, StartsOn: r.StartsOn, EndsOn: r.EndsOn,
		MRR: storage.Decimal(mrr.String()), At: now})
}

// Amendment is an amendment to record: the monthly value from the effective day (left out for a cancellation).
type Amendment struct {
	Kind        string
	EffectiveOn time.Time
	MRR         string
	Reason      string
}

// Amend records an amendment of the contract at version. The day is on or after the contract's start, after the contract's last amendment, and
// not in a closed period; an upgrade raises the value in force on the day, a downgrade lowers it to more than nothing, a cancellation ends an
// active contract, and a renewal starts an ended one again.
func Amend(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, version int, a Amendment, now time.Time) (storage.SubscriptionAmendment, error) {
	c, err := u.Subscriptions().Lock(ctx, id)
	if err != nil {
		return storage.SubscriptionAmendment{}, err
	}
	if c.Version != version {
		return storage.SubscriptionAmendment{}, &storage.Error{Code: storage.CodeVersion, Message: "the contract was changed by someone else", Hint: "Reload it and try again"}
	}
	reason := strings.TrimSpace(a.Reason)
	if reason == "" || len(reason) > 500 {
		return storage.SubscriptionAmendment{}, &Error{CodeReason, "An amendment needs its reason, in up to 500 characters.", "Say why the contract changes"}
	}
	if a.EffectiveOn.IsZero() || a.EffectiveOn.Before(c.StartsOn) {
		return storage.SubscriptionAmendment{}, &Error{CodeDate, "An amendment takes effect on or after the day the contract starts.", "Enter the day the change takes effect"}
	}
	ams, err := u.Subscriptions().Amendments(ctx, id)
	if err != nil {
		return storage.SubscriptionAmendment{}, err
	}
	if n := len(ams); n > 0 && !a.EffectiveOn.After(ams[n-1].EffectiveOn) {
		return storage.SubscriptionAmendment{}, &Error{CodeDate, "An amendment takes effect after the contract's last amendment, which is dated " + ams[n-1].EffectiveOn.Format("2 Jan 2006") + ".", "Enter a later day"}
	}
	periods, err := u.Periods().List(ctx)
	if err != nil {
		return storage.SubscriptionAmendment{}, err
	}
	for _, p := range periods {
		if p.Status == storage.PeriodClosed && !a.EffectiveOn.Before(p.StartsOn) && !a.EffectiveOn.After(p.EndsOn) {
			return storage.SubscriptionAmendment{}, &Error{CodeDate, "The month of that day is closed, and a closed month's figures are not rewritten.", "Enter a day in an open month, or reopen the month first"}
		}
	}
	cur, err := money.Lookup(c.Currency)
	if err != nil {
		return storage.SubscriptionAmendment{}, err
	}
	before, err := On(c, ams, a.EffectiveOn.AddDate(0, 0, -1))
	if err != nil {
		return storage.SubscriptionAmendment{}, err
	}
	if a.EffectiveOn.Equal(c.StartsOn) {
		before = State{Active, money.Zero(cur)}
		if before.MRR, err = money.ParseStored(string(c.MRR), cur); err != nil {
			return storage.SubscriptionAmendment{}, err
		}
	}
	mrr := money.Zero(cur)
	if a.Kind != storage.AmendCancel {
		if mrr, err = money.Parse(a.MRR, cur); err != nil || mrr.Sign() <= 0 {
			return storage.SubscriptionAmendment{}, &Error{CodeAmount, "The new monthly value is not an amount above nothing in " + cur.Code + ".", "Enter what the customer pays each month from that day, such as 150.00"}
		}
	}
	switch a.Kind {
	case storage.AmendUpgrade, storage.AmendDowngrade:
		if before.Status != Active {
			return storage.SubscriptionAmendment{}, &Error{CodeKind, "The contract is not active on that day, so it is renewed, not upgraded or downgraded.", "Choose renew, or a day it is active"}
		}
		cmp, _ := mrr.Cmp(before.MRR)
		if (a.Kind == storage.AmendUpgrade && cmp <= 0) || (a.Kind == storage.AmendDowngrade && cmp >= 0) {
			return storage.SubscriptionAmendment{}, &Error{CodeAmount, "An " + a.Kind + " " + map[string]string{storage.AmendUpgrade: "raises", storage.AmendDowngrade: "lowers"}[a.Kind] + " the " + before.MRR.String() + " in force that day.", "Check the new monthly value"}
		}
	case storage.AmendCancel:
		if before.Status != Active {
			return storage.SubscriptionAmendment{}, &Error{CodeKind, "The contract is not active on that day, so there is nothing to cancel.", "Choose a day it is active"}
		}
	case storage.AmendRenew:
		if before.Status != Ended {
			return storage.SubscriptionAmendment{}, &Error{CodeKind, "The contract has not ended by that day, so it is upgraded or downgraded, not renewed.", "Choose upgrade or downgrade, or the day after it ends"}
		}
	default:
		return storage.SubscriptionAmendment{}, &Error{CodeKind, "The kind is upgrade, downgrade, cancel or renew.", "Choose one of them"}
	}
	out, err := u.Subscriptions().Amend(ctx, storage.NewSubscriptionAmendment{ContractID: id, Kind: a.Kind, EffectiveOn: a.EffectiveOn, MRR: storage.Decimal(mrr.String()), Reason: reason, At: now}, version)
	if err != nil {
		var se *storage.Error
		if errors.As(err, &se) && se.Code == storage.CodeInvalid {
			return storage.SubscriptionAmendment{}, &Error{CodeDate, "The contract has an amendment for that day already.", "Enter a later day"}
		}
		return storage.SubscriptionAmendment{}, err
	}
	return out, nil
}

// ConvertedRate is the rate one currency's total was converted at.
type ConvertedRate struct {
	Currency string
	Rate     string    // base currency per one unit, as the rate cascade found it
	Source   string    // where it came from, such as "MAS daily feed of 2026-10-30"
	Of       time.Time // the date the rate is of: not always the day asked for
}

// BaseTotal is the contracted MRR converted into the base currency as a figure of its own, beside the totals in each contract's currency (FR-KPI-04, rev 0.579): each currency's total is
// converted once under RND-FX at the rate the cascade finds for the day (the organisation's, the daily feed's, the month's), and the converted totals are added. A currency with no rate is
// left out and named in Missing, so the figure is never a guess; it is then not the whole.
type BaseTotal struct {
	Amount  money.Money
	Rates   []ConvertedRate
	Missing []string
}

// InBase converts totals into base on day.
func InBase(ctx context.Context, u storage.UnitOfWork, totals []Total, base money.Currency, day time.Time) (BaseTotal, error) {
	out := BaseTotal{Amount: money.Zero(base)}
	for _, t := range totals {
		cur, err := money.Lookup(t.Currency)
		if err != nil {
			return BaseTotal{}, err
		}
		found, err := fx.Lookup(ctx, u, base, cur, day)
		var fe *fx.Error
		if errors.As(err, &fe) && fe.Code == fx.CodeNoRate {
			out.Missing = append(out.Missing, t.Currency)
			continue
		}
		if err != nil {
			return BaseTotal{}, err
		}
		converted := t.MRR
		if cur != base {
			if converted, err = rounding.FX(t.MRR, found.Rate); err != nil {
				return BaseTotal{}, err
			}
		}
		if out.Amount, err = out.Amount.Add(converted); err != nil {
			return BaseTotal{}, err
		}
		out.Rates = append(out.Rates, ConvertedRate{Currency: t.Currency, Rate: found.Rate.String(), Source: found.Rate.Source, Of: found.Of})
	}
	return out, nil
}
