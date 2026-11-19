// Package projectrate values time on projects (PRD-001 FR-PRJ-06; design D5): the cost rate in force for a person on a day, the
// rate a viewer is allowed to see time at, and the billing rate in force. A person's own cost rate is payroll data (FR-PAY-13):
// only the payroll tier is given it. Everyone else is given the role-average rate over at least three people, else the standard
// rate, else none (hours only), and which basis it was is said every time, so a rate is never sent to be hidden by a screen.
package projectrate

import (
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// MinGroup is the fewest people a role-average rate is taken over (FR-PRJ-06, FR-PAY-13).
const MinGroup = 3

// Tier is what a viewer may see of cost rates.
type Tier int

const (
	// Hours sees time and no cost (the AR specialist's "revenue, hours").
	Hours Tier = iota
	// Averaged sees time at the role-average rate, else the standard rate.
	Averaged
	// Payroll sees each person's own rate: the Owner and the Payroll administrator.
	Payroll
)

// Basis says where a rate came from.
type Basis string

const (
	BasisPerson      Basis = "person"
	BasisRoleAverage Basis = "role_average"
	BasisStandard    Basis = "standard"
	BasisNone        Basis = "none"
)

var work = apd.BaseContext.WithPrecision(60)

func onOrBefore(a, day time.Time) bool { return !a.After(day) }

// inForce is the latest rate starting on or before day among those of one person (nil: the standard rate).
func inForce(rs []storage.CostRate, user *uuid.UUID, day time.Time) (storage.CostRate, bool) {
	var best storage.CostRate
	found := false
	for _, r := range rs {
		same := (r.UserID == nil && user == nil) || (r.UserID != nil && user != nil && *r.UserID == *user)
		if same && onOrBefore(r.From, day) && (!found || r.From.After(best.From)) {
			best, found = r, true
		}
	}
	return best, found
}

// Rate is the hourly rate a viewer of tier may value a person's time at on a day, and its basis.
func Rate(rs []storage.CostRate, tier Tier, user uuid.UUID, day time.Time, cur money.Currency) (money.UnitPrice, Basis, error) {
	none := money.UnitPrice{}
	if tier == Hours {
		return none, BasisNone, nil
	}
	own, has := inForce(rs, &user, day)
	if tier == Payroll && has {
		p, err := money.ParseUnitPrice(string(own.Hourly), cur, "cost_rate")
		return p, BasisPerson, err
	}
	if has { // the average of their role, over everyone whose rate in force that day is in it
		people := map[uuid.UUID]bool{}
		for _, r := range rs {
			if r.UserID != nil {
				people[*r.UserID] = true
			}
		}
		sum, n := new(apd.Decimal), 0
		for id := range people {
			if r, ok := inForce(rs, &id, day); ok && r.Role == own.Role {
				var d apd.Decimal
				if _, _, err := d.SetString(string(r.Hourly)); err != nil {
					return none, BasisNone, err
				}
				if _, err := work.Add(sum, sum, &d); err != nil {
					return none, BasisNone, err
				}
				n++
			}
		}
		if n >= MinGroup {
			var avg, rounded apd.Decimal
			if _, err := work.Quo(&avg, sum, apd.New(int64(n), 0)); err != nil {
				return none, BasisNone, err
			}
			c := work.WithPrecision(60)
			c.Rounding = apd.RoundHalfUp
			if _, err := c.Quantize(&rounded, &avg, -money.UnitPriceScale); err != nil {
				return none, BasisNone, err
			}
			p, err := money.ParseUnitPrice(rounded.Text('f'), cur, "cost_rate")
			return p, BasisRoleAverage, err
		}
	}
	if s, ok := inForce(rs, nil, day); ok {
		if p, err := money.ParseUnitPrice(string(s.Hourly), cur, "cost_rate"); err != nil || p.Sign() > 0 {
			return p, BasisStandard, err
		}
	}
	return none, BasisNone, nil
}

// Billing is the billing rate in force for a person on a day: the project's own for the person, then for their role, then for
// everyone on it; then the customer's the same way. ok is false when none applies.
func Billing(rs []storage.BillingRate, project uuid.UUID, customer *uuid.UUID, user uuid.UUID, role string, day time.Time) (storage.BillingRate, bool) {
	pick := func(match func(storage.BillingRate) bool) (storage.BillingRate, bool) {
		var best storage.BillingRate
		found := false
		for _, r := range rs {
			if match(r) && onOrBefore(r.From, day) && (!found || r.From.After(best.From)) {
				best, found = r, true
			}
		}
		return best, found
	}
	onProject := func(r storage.BillingRate) bool { return r.ProjectID != nil && *r.ProjectID == project }
	onCustomer := func(r storage.BillingRate) bool {
		return customer != nil && r.CustomerID != nil && *r.CustomerID == *customer
	}
	for _, scope := range []func(storage.BillingRate) bool{onProject, onCustomer} {
		for _, who := range []func(storage.BillingRate) bool{
			func(r storage.BillingRate) bool { return r.UserID != nil && *r.UserID == user },
			func(r storage.BillingRate) bool { return role != "" && r.Role == role },
			func(r storage.BillingRate) bool { return r.UserID == nil && r.Role == "" },
		} {
			if r, ok := pick(func(r storage.BillingRate) bool { return scope(r) && who(r) }); ok {
				return r, true
			}
		}
	}
	return storage.BillingRate{}, false
}
