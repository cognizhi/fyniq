package breakglass

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

var (
	start   = time.Date(2026, 11, 6, 1, 0, 0, 0, time.UTC)
	weiLing = uuid.MustParse("01928f6e-7a3b-7c41-9d2e-5f8a1b3c0001")
	ahKow   = uuid.MustParse("01928f6e-7a3b-7c41-9d2e-5f8a1b3c0002")
)

func glass(uses ...string) storage.BreakGlass {
	g := storage.BreakGlass{ID: uuid.New(), StartedBy: weiLing, StartedAt: start, ExpiresAt: start.Add(Lasts), Currency: "SGD", Cap: "20000.00"}
	for _, total := range uses {
		g.Uses = append(g.Uses, storage.BreakGlassUse{Currency: "SGD", Total: storage.Decimal(total)})
	}
	return g
}

func sgd(t *testing.T, s string) money.Money {
	t.Helper()
	m, err := money.Parse(s, money.MustLookup("SGD"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func codes(ps []Problem) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Code)
	}
	return out
}

// A break-glass lasts 24 hours unless an Owner ends it sooner, and its review is due 7 days after it ends (FM-8; the
// owner's decision of 6 Nov 2026).
// req: FM-8
// req: access/team-modes Post-review
func TestABreakGlassLastsADayAndIsDueAWeekAfter(t *testing.T) {
	g := glass()
	for _, c := range []struct {
		at     time.Time
		active bool
	}{
		{start.Add(-time.Second), false}, {start, true}, {start.Add(Lasts - time.Second), true}, {start.Add(Lasts), false},
	} {
		if got := Active(g, c.at); got != c.active {
			t.Errorf("active at %s = %v, want %v", c.at, got, c.active)
		}
	}
	if want := start.Add(Lasts + ReviewDue); !Due(g).Equal(want) {
		t.Errorf("due %s, want %s", Due(g), want)
	}
	ended := start.Add(2 * time.Hour)
	g.EndedAt = &ended
	if Active(g, ended) || !Due(g).Equal(ended.Add(ReviewDue)) {
		t.Errorf("ended sooner: active %v, due %s", Active(g, ended), Due(g))
	}
}

// Break-glass releases a batch waiting for approval only as its Owner, while it is active, never repaying a director,
// never paying the Owner using it (FM-9), only into accounts in force for 30 days, and within what is left of its cap;
// every problem is said at once (FM-8, G4-062, G4-063).
// req: FM-8
// req: FM-9
// req: G4-062
// req: G4-063
// req: access/team-modes Releasing a batch under break-glass
// req: access/team-modes Payees with a recent bank change
// req: access/team-modes Total per break-glass
func TestBreakGlassReleasesWithinItsLimits(t *testing.T) {
	now := start.Add(time.Hour)
	settled, recent := now.Add(-Settled), now.Add(-Settled+time.Hour)
	supplier := Payment{Payee: "Northwind", InForce: &settled}
	g := glass("15000.00")
	if left, err := Left(g); err != nil || left.String() != "5000.00" {
		t.Fatalf("left = %v, %v", left, err)
	}
	if used, err := Used(g); err != nil || used.String() != "15000.00" {
		t.Fatalf("used = %v, %v", used, err)
	}
	for name, c := range map[string]struct {
		owner uuid.UUID
		at    time.Time
		total string
		pays  []Payment
		want  []string
	}{
		"a supplier unchanged for 30 days, within the cap": {weiLing, now, "5000.00", []Payment{supplier}, nil},
		"by another Owner":                                 {ahKow, now, "10.00", []Payment{supplier}, []string{CodeNotActive}},
		"once it expired":                                  {weiLing, start.Add(Lasts), "10.00", []Payment{supplier}, []string{CodeNotActive}},
		"a director's repayment":                           {weiLing, now, "10.00", []Payment{{Payee: "Ah Kow", Director: true, InForce: &settled}}, []string{CodeDirector}},
		"the Owner's own claim":                            {weiLing, now, "10.00", []Payment{{Payee: "Wei Ling", Member: weiLing, InForce: &settled}}, []string{CodeOwn}},
		"an account in force for less than 30 days":        {weiLing, now, "10.00", []Payment{{Payee: "Fresh", InForce: &recent}}, []string{CodeNewAccount}},
		"an account whose start is not known":              {weiLing, now, "10.00", []Payment{{Payee: "Unknown"}}, []string{CodeNewAccount}},
		"above what is left of the cap":                    {weiLing, now, "5000.01", []Payment{supplier}, []string{CodeCap}},
		"several problems at once":                         {weiLing, now, "6000.00", []Payment{{Payee: "Ah Kow", Director: true}, {Payee: "Fresh", InForce: &recent}}, []string{CodeDirector, CodeNewAccount, CodeCap}},
		"another member, as a claimant, unchanged 30 days": {weiLing, now, "10.00", []Payment{{Payee: "Ah Kow", Member: ahKow, InForce: &settled}}, nil},
	} {
		got, err := Refusals(g, c.owner, sgd(t, c.total), c.pays, c.at)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(codes(got), c.want) {
			t.Errorf("%s: %v, want %v", name, codes(got), c.want)
		}
	}
}

// Its review comes once it has ended: from another Owner at any time, from the Accountant only from its due date, and
// never from the Owner who used it (FM-8; the owner's decision of 6 Nov 2026).
// req: FM-8
// req: FM-7
// req: access/team-modes Post-review
func TestWhoReviewsABreakGlassAndWhen(t *testing.T) {
	g := glass()
	ended, due := g.Ends(), Due(g)
	accountant := uuid.New()
	for name, c := range map[string]struct {
		who   uuid.UUID
		roles []authz.Role
		at    time.Time
		ok    bool
		code  string
	}{
		"its Owner":                          {weiLing, []authz.Role{authz.Owner}, due, false, CodeReviewer},
		"another Owner while it is active":   {ahKow, []authz.Role{authz.Owner}, ended.Add(-time.Second), false, CodeTooEarly},
		"another Owner once it ended":        {ahKow, []authz.Role{authz.Owner}, ended, true, ""},
		"the Accountant before its due date": {accountant, []authz.Role{authz.Accountant}, due.Add(-time.Second), false, CodeReviewer},
		"the Accountant from its due date":   {accountant, []authz.Role{authz.Accountant}, due, true, ""},
		"a Controller":                       {accountant, []authz.Role{authz.Controller}, due, false, CodeReviewer},
	} {
		ok, why := Reviewer(g, c.who, c.roles, c.at)
		code := ""
		if why != nil {
			code = why.Code
		}
		if ok != c.ok || code != c.code {
			t.Errorf("%s: %v %q, want %v %q", name, ok, code, c.ok, c.code)
		}
	}
}
