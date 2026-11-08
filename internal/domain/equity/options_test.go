package equity

import (
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
