// Package projectreport is what projects earn and cost (PRD-001 FR-PRJ-12; design D6): revenue and direct costs from the posted
// ledger through the balance service (balance.BySet), staff cost only as approved time at the viewer's rate tier, so salary
// tagged to a project is never counted twice, and utilisation and realisation from approved time. Time posts nothing: it is a
// management view. Money is exact; a percentage is shown to one decimal (RND-PERCENT).
package projectreport

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/projectrate"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// StandardDayMinutes is a working day's hours in minutes until a person's working pattern exists (FR-PPL-07): 40 hours a week
// is 8 hours a weekday (FR-PRJ-12) [verify the standard hours with the accountant].
const StandardDayMinutes = 480

// Ledger is what the posted ledger holds for a project, in the base currency, from revenue and expense accounts only.
type Ledger struct {
	Revenue       money.Money // credits less debits on revenue accounts
	DirectCosts   money.Money // debits less credits on expense accounts without the payroll-cost flag
	PayrollTagged money.Money // the payroll-cost accounts' lines tagged to it: listed to reconcile, never in direct costs (G4-049)
}

func zero(cur money.Currency) Ledger {
	return Ledger{Revenue: money.Zero(cur), DirectCosts: money.Zero(cur), PayrollTagged: money.Zero(cur)}
}

// LedgerByProject sums the months from..to (first days of months) of the revenue and expense accounts by project: each project's
// own lines, and those tagged with none. Every line is in exactly one of them, so the projects plus the untagged equal the
// accounts' totals to the cent (QA-RPT-01).
func LedgerByProject(ctx context.Context, u storage.UnitOfWork, base money.Currency, from, to time.Time) (map[uuid.UUID]Ledger, Ledger, error) {
	ps, err := u.Projects().List(ctx)
	if err != nil {
		return nil, Ledger{}, err
	}
	isProject := map[uuid.UUID]bool{}
	for _, p := range ps {
		isProject[p.ID] = true
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return nil, Ledger{}, err
	}
	byID := map[uuid.UUID]storage.Account{}
	for _, a := range accounts {
		byID[a.ID] = a
	}
	periods, err := u.Periods().List(ctx)
	if err != nil {
		return nil, Ledger{}, err
	}
	out := map[uuid.UUID]Ledger{}
	untagged := zero(base)
	for _, p := range periods {
		if p.StartsOn.Before(from) || p.StartsOn.After(to) {
			continue
		}
		rows, err := balance.BySet(ctx, u, base, p.ID)
		if err != nil {
			return nil, Ledger{}, err
		}
		for _, r := range rows {
			a, ok := byID[r.AccountID]
			if !ok || (a.Type != storage.Revenue && a.Type != storage.Expense) {
				continue
			}
			var project *uuid.UUID
			for _, v := range r.Values {
				if isProject[v] {
					v := v
					project = &v
				}
			}
			l := untagged
			if project != nil {
				var had bool
				if l, had = out[*project]; !had {
					l = zero(base)
				}
			}
			switch {
			case a.Type == storage.Revenue:
				l.Revenue, err = l.Revenue.Sub(r.Base)
			case a.PayrollCost:
				l.PayrollTagged, err = l.PayrollTagged.Add(r.Base)
			default:
				l.DirectCosts, err = l.DirectCosts.Add(r.Base)
			}
			if err != nil {
				return nil, Ledger{}, err
			}
			if project != nil {
				out[*project] = l
			} else {
				untagged = l
			}
		}
	}
	return out, untagged, nil
}

// Time is a project's approved time in a window, at a viewer's tier.
type Time struct {
	Minutes, Billable int
	Cost              *money.Money // nil when no rate may be used
	Basis             map[projectrate.Basis]bool
	Standard          *money.Money // billable time at billing rates, in the project's currency; nil when it has none or is not in the base currency
}

// TimeOf values a project's approved entries in a window: cost at the tier by RND-TIME per entry, standard value at billing rates.
func TimeOf(entries []storage.TimeEntry, costRates []storage.CostRate, billing []storage.BillingRate, project storage.Project, tier projectrate.Tier, base money.Currency) (Time, error) {
	t := Time{Basis: map[projectrate.Basis]bool{}}
	cost := money.Zero(base)
	std := money.Zero(base)
	costed, valued := false, false
	roleOf := func(user uuid.UUID, day time.Time) string {
		best, found := storage.CostRate{}, false
		for _, r := range costRates {
			if r.UserID != nil && *r.UserID == user && !r.From.After(day) && (!found || r.From.After(best.From)) {
				best, found = r, true
			}
		}
		return best.Role
	}
	for _, e := range entries {
		t.Minutes += e.Minutes
		if e.Billable {
			t.Billable += e.Minutes
		}
		rate, basis, err := projectrate.Rate(costRates, tier, e.UserID, e.WorkDate, base)
		if err != nil {
			return t, err
		}
		t.Basis[basis] = true
		if basis != projectrate.BasisNone {
			c, err := rounding.Time(e.Minutes, rate)
			if err != nil {
				return t, err
			}
			if cost, err = cost.Add(c); err != nil {
				return t, err
			}
			costed = true
		}
		if e.Billable && project.Currency == base.Code {
			if br, ok := projectrate.Billing(billing, project.ID, project.CustomerID, e.UserID, roleOf(e.UserID, e.WorkDate), e.WorkDate); ok {
				bp, err := money.ParseUnitPrice(string(br.Hourly), base, "billing_rate")
				if err != nil {
					return t, err
				}
				v, err := rounding.Time(e.Minutes, bp)
				if err != nil {
					return t, err
				}
				if std, err = std.Add(v); err != nil {
					return t, err
				}
				valued = true
			}
		}
	}
	if costed {
		t.Cost = &cost
	}
	if valued {
		t.Standard = &std
	}
	return t, nil
}

// Margin is revenue less the time at cost less the direct costs (FR-PRJ-12); nil when the time has no cost to use.
func Margin(l Ledger, t Time) (*money.Money, error) {
	if t.Cost == nil && t.Minutes > 0 {
		return nil, nil
	}
	m := l.Revenue
	var err error
	if t.Cost != nil {
		if m, err = m.Sub(*t.Cost); err != nil {
			return nil, err
		}
	}
	if m, err = m.Sub(l.DirectCosts); err != nil {
		return nil, err
	}
	return &m, nil
}

// Realisation is billed (the revenue posted to the project) over the standard value of its billable time, as a percentage to one
// decimal; "" when there is no standard value.
func Realisation(billed money.Money, t Time) (string, error) {
	if t.Standard == nil || t.Standard.IsZero() {
		return "", nil
	}
	return rounding.Percent(billed, *t.Standard)
}

// Available are the minutes a person could have worked from..to inclusive: the weekdays less the public holidays, at the standard
// day. A working pattern and leave join with FR-PPL-07.
func Available(from, to time.Time, holidays map[time.Time]string) int {
	n := 0
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
			continue
		}
		if _, off := holidays[d]; off {
			continue
		}
		n += StandardDayMinutes
	}
	return n
}

// Utilisation is billable over available minutes as a percentage to one decimal ("" when none is available).
func Utilisation(billable, available int, cur money.Currency) (string, error) {
	if available <= 0 {
		return "", nil
	}
	return rounding.Percent(money.FromMinor(int64(billable), cur), money.FromMinor(int64(available), cur))
}

// Months are the first days of the months from..to inclusive.
func Months(from, to time.Time) []time.Time {
	var out []time.Time
	for d := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC); !d.After(to); d = d.AddDate(0, 1, 0) {
		out = append(out, d)
	}
	return slices.Clone(out)
}
