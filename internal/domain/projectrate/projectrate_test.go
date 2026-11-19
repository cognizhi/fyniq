package projectrate_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/projectrate"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func day(s string) time.Time { t, _ := time.Parse(time.DateOnly, s); return t }

func person(role, from, rate string) (storage.CostRate, uuid.UUID) {
	id := uuid.New()
	return storage.CostRate{UserID: &id, Role: role, From: day(from), Hourly: storage.Decimal(rate)}, id
}

// A viewer without payroll rights is given the role average over at least three people, else the standard rate, else nothing;
// never a person's own rate. A rate changed mid-month applies from its day (FR-PRJ-06, G4-050).
// req: FR-PRJ-06
// req: FR-PAY-13
func TestTheRateAViewerMayValueTimeAt(t *testing.T) {
	sgd := money.MustLookup("SGD")
	a, ua := person("Consultant", "2026-01-01", "60")
	b, _ := person("Consultant", "2026-01-01", "80")
	c, _ := person("Consultant", "2026-01-01", "100.50")
	std := storage.CostRate{Role: storage.StandardRole, From: day("2026-01-01"), Hourly: "70"}
	rs := []storage.CostRate{a, b, c, std}
	got := func(tier projectrate.Tier, rs []storage.CostRate, who uuid.UUID, on string) (string, projectrate.Basis) {
		p, basis, err := projectrate.Rate(rs, tier, who, day(on), sgd)
		if err != nil {
			t.Fatal(err)
		}
		if basis == projectrate.BasisNone {
			return "", basis
		}
		return p.String(), basis
	}
	if r, b := got(projectrate.Payroll, rs, ua, "2026-06-01"); r != "60.00" || b != projectrate.BasisPerson {
		t.Errorf("the payroll tier: %s %s", r, b)
	}
	if r, b := got(projectrate.Averaged, rs, ua, "2026-06-01"); r != "80.166667" || b != projectrate.BasisRoleAverage {
		t.Errorf("three people: the average of 60, 80 and 100.50 to six decimals: %s %s", r, b)
	}
	// Two in the role: the standard rate, never the average of two (which would tell one rate from the other).
	if r, b := got(projectrate.Averaged, []storage.CostRate{a, b, std}, ua, "2026-06-01"); r != "70.00" || b != projectrate.BasisStandard {
		t.Errorf("two people: %s %s", r, b)
	}
	if r, b := got(projectrate.Averaged, []storage.CostRate{a, b}, ua, "2026-06-01"); r != "" || b != projectrate.BasisNone {
		t.Errorf("two people and no standard rate: hours only: %s %s", r, b)
	}
	if _, b := got(projectrate.Hours, rs, ua, "2026-06-01"); b != projectrate.BasisNone {
		t.Error("the hours tier is given no rate")
	}
	// A rate from a later day applies from that day; before it the earlier one does; before any, none.
	later := a
	later.ID, later.From, later.Hourly = uuid.New(), day("2026-07-01"), "90"
	rs2 := []storage.CostRate{a, later}
	if r, _ := got(projectrate.Payroll, rs2, ua, "2026-06-30"); r != "60.00" {
		t.Errorf("the day before the change: %s", r)
	}
	if r, _ := got(projectrate.Payroll, rs2, ua, "2026-07-01"); r != "90.00" {
		t.Errorf("the day of the change: %s", r)
	}
	if _, b := got(projectrate.Payroll, rs2, ua, "2025-12-31"); b != projectrate.BasisNone {
		t.Error("a day before any rate")
	}
}

// The billing rate in force: the project's for the person, then their role, then everyone; then the customer's (FR-PRJ-06).
// req: FR-PRJ-06
func TestTheBillingRateInForceFollowsItsPrecedence(t *testing.T) {
	proj, cust, user := uuid.New(), uuid.New(), uuid.New()
	rate := func(p, c, u *uuid.UUID, role, from, h string) storage.BillingRate {
		return storage.BillingRate{ID: uuid.New(), ProjectID: p, CustomerID: c, UserID: u, Role: role, From: day(from), Hourly: storage.Decimal(h)}
	}
	rs := []storage.BillingRate{rate(nil, &cust, nil, "", "2026-01-01", "100"), rate(&proj, nil, nil, "", "2026-01-01", "150"),
		rate(&proj, nil, nil, "Consultant", "2026-01-01", "180"), rate(&proj, nil, &user, "", "2026-03-01", "200"), rate(&proj, nil, &user, "", "2026-01-01", "190")}
	for _, c := range []struct{ on, role, want string }{
		{"2026-02-01", "", "150"}, {"2026-02-01", "Consultant", "180"}, {"2026-02-10", "Analyst", "150"},
	} {
		other := uuid.New()
		if r, ok := projectrate.Billing(rs, proj, &cust, other, c.role, day(c.on)); !ok || string(r.Hourly) != c.want {
			t.Errorf("%s %s: %v %v, want %s", c.on, c.role, r.Hourly, ok, c.want)
		}
	}
	if r, _ := projectrate.Billing(rs, proj, &cust, user, "Consultant", day("2026-02-01")); r.Hourly != "190" {
		t.Errorf("the person's own: %s", r.Hourly)
	}
	if r, _ := projectrate.Billing(rs, proj, &cust, user, "", day("2026-03-01")); r.Hourly != "200" {
		t.Errorf("the person's, from its day: %s", r.Hourly)
	}
	if r, ok := projectrate.Billing(rs, uuid.New(), &cust, user, "", day("2026-02-01")); !ok || r.Hourly != "100" {
		t.Errorf("another project falls back to the customer's: %v %v", r.Hourly, ok)
	}
	if _, ok := projectrate.Billing(rs, proj, &cust, user, "", day("2025-12-01")); ok {
		t.Error("a day before any rate")
	}
}
