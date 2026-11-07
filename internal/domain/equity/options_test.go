package equity

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
)

// A grant's vesting, worked by hand (FR-EQ-19, design D9): 48,000 options granted on 15 Jan 2026 over 48 months with a
// 12-month cliff vest nothing until 15 Jan 2027, then 12,000 (12 of 48 months), 1,000 more each month, and all 48,000
// from 15 Jan 2030. 10,000 over 36 months with no cliff vest 277 after one month (277.77…, rounded down); a grant on
// 31 Jan vests its first month on 28 Feb; one with no vesting months vests whole on its grant date.
// req: FR-EQ-19
func TestAGrantVests(t *testing.T) {
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
	grant := Grant{Number: "48000", GrantedOn: day(2026, 1, 15), Vesting: Vesting{CliffMonths: 12, Months: 48}}
	for _, c := range []struct {
		g    Grant
		on   time.Time
		want string
	}{
		{grant, day(2026, 1, 14), "0"},
		{grant, day(2026, 12, 31), "0"},
		{grant, day(2027, 1, 14), "0"},
		{grant, day(2027, 1, 15), "12000"},
		{grant, day(2027, 2, 15), "13000"},
		{grant, day(2028, 7, 20), "30000"},
		{grant, day(2030, 1, 15), "48000"},
		{grant, day(2035, 1, 1), "48000"},
		{Grant{Number: "10000", GrantedOn: day(2026, 3, 1), Vesting: Vesting{Months: 36}}, day(2026, 4, 1), "277"},
		{Grant{Number: "1200", GrantedOn: day(2026, 1, 31), Vesting: Vesting{Months: 12}}, day(2026, 2, 27), "0"},
		{Grant{Number: "1200", GrantedOn: day(2026, 1, 31), Vesting: Vesting{Months: 12}}, day(2026, 2, 28), "100"},
		{Grant{Number: "500", GrantedOn: day(2026, 5, 5), Vesting: Vesting{}}, day(2026, 5, 5), "500"},
	} {
		got, err := Vested(c.g, c.on)
		if err != nil {
			t.Fatal(err)
		}
		if got.Text('f') != c.want {
			t.Errorf("%s of %+v granted %s, on %s: %s, want %s", c.g.Number, c.g.Vesting, c.g.GrantedOn.Format(time.DateOnly), c.on.Format(time.DateOnly),
				got.Text('f'), c.want)
		}
	}
}

// A pool resolution's and a grant's terms (FR-EQ-19): every term outside its rules is named, all at once.
// req: FR-EQ-19
func TestThePoolAndAGrantKeepTheirTerms(t *testing.T) {
	sgd, err := money.Lookup("SGD")
	if err != nil {
		t.Fatal(err)
	}
	capital, sales := uuid.New(), uuid.New()
	s := ledger.Settings{Base: sgd, Accounts: map[uuid.UUID]ledger.Account{capital: {ID: capital, Code: "3010", Active: true, ShareCapital: true},
		sales: {ID: sales, Code: "4000", Active: true}}}
	today := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	pool := PoolResolution{Size: "1000000", ClassAccountID: capital, ResolutionRef: "Ordinary resolution 1 Oct 2026", ResolvedOn: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
	if p := CheckPool(pool, s, today); len(p) > 0 {
		t.Fatalf("the pool: %v", p)
	}
	bad := PoolResolution{Size: "1.5", ClassAccountID: sales, ResolvedOn: today.AddDate(0, 0, 1)}
	if got := fields(CheckPool(bad, s, today)); got != "size,class_account_id,resolution_ref,resolved_on" {
		t.Errorf("a bad pool: %s", got)
	}
	g := Grant{HolderID: uuid.New(), Number: "48000", ExercisePrice: "0.10", GrantedOn: today, ExpiresOn: today.AddDate(10, 0, 0), ClassAccountID: capital,
		Vesting: Vesting{CliffMonths: 12, Months: 48, Acceleration: "double"}}
	if p := CheckGrant(g, s, today); len(p) > 0 {
		t.Fatalf("the grant: %v", p)
	}
	if p := CheckGrant(Grant{Number: "0", ExercisePrice: "0", GrantedOn: today, ExpiresOn: today.AddDate(1, 0, 0), ClassAccountID: capital, Vesting: Vesting{Acceleration: "none"}},
		s, today); fields(p) != "holder_id,number" {
		t.Errorf("a grant of nothing at a price of zero: %s", fields(p))
	}
	for name, c := range map[string]struct {
		change func(*Grant)
		fields string
	}{
		"a negative price":           {func(x *Grant) { x.ExercisePrice = "-0.10" }, "exercise_price"},
		"a grant after today":        {func(x *Grant) { x.GrantedOn = today.AddDate(0, 0, 1); x.ExpiresOn = today.AddDate(5, 0, 0) }, "granted_on"},
		"a cliff past the vesting":   {func(x *Grant) { x.Vesting.CliffMonths = 60 }, "cliff_months"},
		"vesting of 121 months":      {func(x *Grant) { x.Vesting.Months, x.Vesting.CliffMonths = 121, 0 }, "vesting_months"},
		"an unknown acceleration":    {func(x *Grant) { x.Vesting.Acceleration = "triple" }, "acceleration"},
		"an expiry on the grant day": {func(x *Grant) { x.ExpiresOn = x.GrantedOn }, "expires_on"},
		"a class on sales":           {func(x *Grant) { x.ClassAccountID = sales }, "class_account_id"},
	} {
		x := g
		c.change(&x)
		if got := fields(CheckGrant(x, s, today)); got != c.fields {
			t.Errorf("%s: %q, want %q", name, got, c.fields)
		}
	}
}

// A grant's events (FR-EQ-19): Priya's 48,000 options from 15 Oct 2025, a 12-month cliff over 48 months. She leaves on
// 15 Apr 2027, 18 months in, with 18,000 vested: 30,000 are forfeited then, and the 18,000 vested lapse on 15 Jul 2027
// when her window to exercise closes. A forfeiture of more than is not vested, or of more than the grant leaves, is
// refused, as is a day before the grant.
// req: FR-EQ-19
func TestAGrantsEventsTakeItsOptions(t *testing.T) {
	day := func(s string) time.Time { d, _ := time.Parse(time.DateOnly, s); return d }
	g := Grant{Number: "48000", GrantedOn: day("2025-10-15"), Vesting: Vesting{CliffMonths: 12, Months: 48, Acceleration: "none"}}
	forfeit := GrantEvent{Kind: EventForfeiture, Number: "30000", On: day("2027-04-15")}
	if probs, err := CheckEventOf(forfeit, g, nil); err != nil || len(probs) > 0 {
		t.Fatalf("forfeiting the 30,000 not vested: %v %v", probs, err)
	}
	if probs, _ := CheckEventOf(GrantEvent{Kind: EventForfeiture, Number: "30001", On: day("2027-04-15")}, g, nil); len(probs) != 1 ||
		probs[0].Field != "number" || !strings.Contains(probs[0].Message, "30000 options are not vested on 2027-04-15") {
		t.Errorf("forfeiting one vested option: %v", probs)
	}
	lapse := GrantEvent{Kind: EventExpiry, Number: "18000", On: day("2027-07-15")}
	if probs, err := CheckEventOf(lapse, g, []GrantEvent{forfeit}); err != nil || len(probs) > 0 {
		t.Fatalf("the vested 18,000 lapsing: %v %v", probs, err)
	}
	if probs, _ := CheckEventOf(GrantEvent{Kind: EventExpiry, Number: "18001", On: day("2027-07-15")}, g, []GrantEvent{forfeit}); len(probs) != 1 ||
		!strings.Contains(probs[0].Message, "leave 18000 options") {
		t.Errorf("more than the grant leaves: %v", probs)
	}
	if probs, _ := CheckEventOf(GrantEvent{Kind: EventExpiry, Number: "1", On: day("2025-10-14")}, g, nil); len(probs) != 1 || probs[0].Field != "on" {
		t.Errorf("a day before the grant: %v", probs)
	}
	for _, c := range []struct {
		on                                      string
		forfeited, expired, outstanding, vested string
	}{
		{"2027-04-14", "0", "0", "48000", "17000"}, // 17 whole months: the 15th has not come
		{"2027-04-15", "30000", "0", "18000", "18000"},
		{"2027-06-15", "30000", "0", "18000", "18000"}, // vesting would say 20,000: the leaver keeps what vested
		{"2027-07-15", "30000", "18000", "0", "0"},
	} {
		s, err := StandingOf(g, []GrantEvent{forfeit, lapse}, day(c.on))
		if err != nil {
			t.Fatal(err)
		}
		if got := [4]string{s.Forfeited.Text('f'), s.Expired.Text('f'), s.Outstanding.Text('f'), s.Vested.Text('f')}; got != [4]string{c.forfeited, c.expired, c.outstanding, c.vested} {
			t.Errorf("on %s: %v", c.on, got)
		}
	}
	book := OptionBook{Pools: []PoolResolution{{Size: "100000", ResolvedOn: day("2025-01-02")}}, Grants: []Grant{g},
		Returns: []Return{{On: forfeit.On, Number: forfeit.Number}}}
	for on, want := range map[string]string{"2027-04-14": "52000", "2027-04-15": "82000"} {
		if p, _, err := book.At(day(on)); err != nil || p.Unallocated.Text('f') != want {
			t.Errorf("the pool on %s: %s %v, want %s", on, p.Unallocated.Text('f'), err, want)
		}
	}
}
