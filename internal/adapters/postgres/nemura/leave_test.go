//go:build local

package nemura_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/leave"
	"github.com/cognizhi/fyniq/internal/nemura/packs"
	"github.com/cognizhi/fyniq/internal/nemura/people"
)

func leaveCode(err error) string {
	var r *leave.Refusal
	if errors.As(err, &r) {
		return r.Code
	}
	return ""
}

func peoplePack(t *testing.T) pack.Version {
	t.Helper()
	v, err := pack.Load(packs.Files(), "sg-people/2026.2")
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// addEmployee is a person covered by the Employment Act who started on a day.
func addEmployee(t *testing.T, c *company, name, start string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		id, err = nemura.AddPerson(ctx, tx, c.org, today.Add(9*time.Hour), c.owner, people.Person{LegalName: name, EmploymentStart: d(start)},
			people.Job{Title: "Engineer", Worksite: "SG", EmploymentType: "full_time"}, &people.Classification{ContractType: "service", EACovered: true, PartIVClass: "none", Residency: "citizen"})
		return err
	})
	return id
}

// A working pattern is effective-dated history: setting a new one ends the old the day before, the pattern on a day is the one in force, nothing is edited
// or deleted, two never overlap, and one organisation never sees another's.
// req: NM-TIM-01
// asvs: V8.2.1
func TestAWorkingPatternIsEffectiveDatedHistory(t *testing.T) {
	ctx := context.Background()
	c := newCompany(t)
	other := newCompany(t)
	now := today.Add(9 * time.Hour)
	ana := addEmployee(t, c, "Ana Tan", "2099-01-05")
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.SetWorkingPattern(ctx, tx, c.org, now, c.owner, ana, d("2099-01-05"), leave.Pattern{Weekdays: []int{1, 2, 3, 4, 5}, HoursPerWeek: 440})
	})
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.SetWorkingPattern(ctx, tx, c.org, now, c.owner, ana, d("2099-07-01"), leave.Pattern{Weekdays: []int{1, 2, 3, 4}, HoursPerWeek: 350})
	})
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		for on, want := range map[string]int{"2099-03-01": 5, "2099-06-30": 5, "2099-07-01": 4} {
			p, ok, err := nemura.WorkingPatternOn(ctx, tx, ana, d(on))
			if err != nil || !ok || p.DaysPerWeek() != want {
				t.Errorf("%s: %+v %v %v", on, p, ok, err)
			}
		}
		if p, _, _ := nemura.WorkingPatternOn(ctx, tx, ana, d("2099-07-01")); p.HoursPerWeek != 350 {
			t.Errorf("hours %d", p.HoursPerWeek)
		}
		if _, ok, _ := nemura.WorkingPatternOn(ctx, tx, ana, d("2099-01-01")); ok {
			t.Error("a pattern before the start")
		}
		return nil
	})
	// A pattern starting on or before the current one's start is refused as an overlap; an unknown day is refused with its field named.
	err := c.do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.SetWorkingPattern(ctx, tx, c.org, now, c.owner, ana, d("2099-07-01"), leave.Pattern{Weekdays: []int{1}, HoursPerWeek: 80})
	})
	if leaveCode(err) != leave.CodeConflict {
		t.Errorf("overlap: %v", err)
	}
	err = c.do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.SetWorkingPattern(ctx, tx, c.org, now, c.owner, ana, d("2099-09-01"), leave.Pattern{Weekdays: []int{8}, HoursPerWeek: 80})
	})
	if leaveCode(err) != leave.CodeInvalid {
		t.Errorf("bad weekday: %v", err)
	}
	// History is never edited or deleted, even by the owner of the schema.
	su := connect(t, db.SuperURL())
	if _, err := su.Exec(ctx, "UPDATE nemura.working_pattern SET weekdays = '{1}' WHERE organisation_id = $1", c.org); err == nil {
		t.Error("a pattern was edited")
	}
	if _, err := su.Exec(ctx, "DELETE FROM nemura.working_pattern WHERE organisation_id = $1", c.org); err == nil {
		t.Error("a pattern was deleted")
	}
	// Another organisation sees none of it.
	other.must(func(ctx context.Context, tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM nemura.working_pattern").Scan(&n); err != nil || n != 0 {
			t.Errorf("another organisation sees %d patterns (%v)", n, err)
		}
		return nil
	})
}

// The ledger is the balance and is append-only: no entry is edited or deleted or truncated, whatever the role; a type that forbids it never goes below zero;
// the sign of an entry is its kind's; and two requests for the last day cannot both have it, because the person's head row is locked first (design D1, D2).
// req: NM-LV-13, NM-LV-02
// asvs: V8.2.1, V11.1.2
func TestTheLeaveLedgerIsAppendOnlyAndTwoRequestsCannotBothTakeTheLastDay(t *testing.T) {
	ctx := context.Background()
	c := newCompany(t)
	other := newCompany(t)
	now := today.Add(9 * time.Hour)
	ana := addEmployee(t, c, "Ana Tan", "2099-01-05")
	add := func(kind string, amount leave.Days) error {
		return c.do(func(ctx context.Context, tx pgx.Tx) error {
			_, err := nemura.AppendLeave(ctx, tx, c.org, now, c.owner, ana, "annual", 2099, nil, leave.Entry{Kind: kind, Amount: amount, Source: "test"}, "")
			return err
		})
	}
	if err := add(leave.Grant, leave.Whole(2)); err != nil {
		t.Fatal(err)
	}
	if err := add(leave.Use, -leave.Whole(1)); err != nil {
		t.Fatal(err)
	}
	if err := add(leave.Use, -leave.Whole(3)); leaveCode(err) != leave.CodeBalance {
		t.Errorf("below zero: %v", err)
	}
	if err := add(leave.Grant, -leave.Whole(1)); leaveCode(err) != leave.CodeInvalid {
		t.Errorf("a negative grant: %v", err)
	}
	// An entry that is not in the ledger's kinds never reaches the database.
	err := c.do(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.AppendLeave(ctx, tx, c.org, now, c.owner, ana, "Annual Leave!", 2099, nil, leave.Entry{Kind: leave.Grant, Amount: 100, Source: "test"}, "")
		return err
	})
	if err == nil {
		t.Error("a type key with spaces was accepted")
	}
	// The last day: ten requests at once for one day; exactly one is booked.
	var wg sync.WaitGroup
	var mu sync.Mutex
	won, refused := 0, 0
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := add(leave.Reservation, -leave.Whole(1))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				won++
			case leaveCode(err) == leave.CodeBalance:
				refused++
			default:
				t.Errorf("concurrent request: %v", err)
			}
		}()
	}
	wg.Wait()
	if won != 1 || refused != 9 {
		t.Errorf("won %d, refused %d", won, refused)
	}
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		b, err := nemura.LeaveBalance(ctx, tx, ana, "annual", 2099)
		if err != nil || b.Entitled != 200 || b.Taken != 100 || b.Booked != 100 || b.Left != 0 {
			t.Errorf("balance %+v %v", b, err)
		}
		return nil
	})
	// Not edited, not deleted, not truncated; the API role cannot even try; another organisation sees none of it.
	su := connect(t, db.SuperURL())
	for _, q := range []string{"UPDATE nemura.leave_ledger SET amount = 9999", "DELETE FROM nemura.leave_ledger", "TRUNCATE nemura.leave_ledger"} {
		if _, err := su.Exec(ctx, q); err == nil {
			t.Errorf("%s was allowed", q)
		}
	}
	err = c.do(func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE nemura.leave_ledger SET amount = 9999")
		return err
	})
	if err == nil {
		t.Error("the API role updated the ledger")
	}
	other.must(func(ctx context.Context, tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM nemura.leave_ledger").Scan(&n); err != nil || n != 0 {
			t.Errorf("another organisation sees %d entries (%v)", n, err)
		}
		return nil
	})
}

// Annual leave is granted from the pack and the policy once; running it again writes nothing, a changed policy writes only the difference as a correction, and
// the ledger shows where each figure came from. A person with no legal classification is refused with the field named.
// req: NM-LV-02, NM-LV-01
func TestAnnualLeaveIsGrantedOnceAndCorrectedByTheDifference(t *testing.T) {
	c := newCompany(t)
	now := today.Add(9 * time.Hour)
	v := peoplePack(t)
	ana := addEmployee(t, c, "Ana Tan", "2099-07-01")
	var none uuid.UUID
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		none, err = nemura.AddPerson(ctx, tx, c.org, now, c.owner, people.Person{LegalName: "No Class", EmploymentStart: d("2099-01-05")}, people.Job{Title: "X", Worksite: "SG", EmploymentType: "full_time"}, nil)
		return err
	})
	// The pack only holds 2026 rules in force from 1 Jan 2026: the person starts in 2099, so the days come from the last row.
	grant := func(person uuid.UUID, year int, policy []int) (leave.Entitlement, leave.Days, error) {
		var g leave.Entitlement
		var wrote leave.Days
		err := c.do(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			g, wrote, err = nemura.GrantAnnualLeave(ctx, tx, c.org, now, c.owner, person, year, v, policy)
			return err
		})
		return g, wrote, err
	}
	g, wrote, err := grant(ana, 2099, nil)
	if err != nil || g.Days != leave.Whole(4) || wrote != leave.Whole(4) || len(g.Basis) < 2 {
		t.Fatalf("first grant: %+v %s %v", g, wrote, err)
	}
	if _, wrote, err := grant(ana, 2099, nil); err != nil || wrote != 0 {
		t.Errorf("a second run wrote %s (%v)", wrote, err)
	}
	// A policy of 14 days: six months of 14 is 7.
	if g, wrote, err := grant(ana, 2099, []int{14}); err != nil || g.Days != leave.Whole(7) || wrote != leave.Whole(3) {
		t.Errorf("policy: %s wrote %s (%v)", g.Days, wrote, err)
	}
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		es, err := nemura.LeaveEntries(ctx, tx, ana, nemura.AnnualLeave, 2099)
		if err != nil || len(es) != 2 || es[0].Kind != leave.Grant || es[1].Kind != leave.Correction || es[1].Amount != leave.Whole(3) || es[1].Source != "annual-leave:2099" {
			t.Errorf("entries: %+v %v", es, err)
		}
		return nil
	})
	if _, _, err := grant(none, 2099, nil); personRefusal(err) != people.CodeInvalid {
		t.Errorf("no classification: %v", err)
	}
}

// Unused annual leave carries into the next year with its last day and lapses after it: days used in the new year use the carried days first, nothing lapses
// twice, and the carry is a transfer out and in that leaves the old year at zero.
// req: NM-LV-02
func TestUnusedAnnualLeaveCarriesForwardAndLapses(t *testing.T) {
	c := newCompany(t)
	now := today.Add(9 * time.Hour)
	ana := addEmployee(t, c, "Ana Tan", "2098-01-05")
	put := func(year int, kind string, amount leave.Days, expires string) {
		c.must(func(ctx context.Context, tx pgx.Tx) error {
			_, err := nemura.AppendLeave(ctx, tx, c.org, now, c.owner, ana, nemura.AnnualLeave, year, nil, leave.Entry{Kind: kind, Amount: amount, Source: "test", Expires: expires}, "")
			return err
		})
	}
	put(2098, leave.Grant, leave.Whole(7), "")
	put(2098, leave.Use, -leave.Whole(2), "")
	var carried leave.Days
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		carried, err = nemura.CarryForwardLeave(ctx, tx, c.org, now, c.owner, ana, 2098, 12)
		return err
	})
	if carried != leave.Whole(5) {
		t.Fatalf("carried %s", carried)
	}
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		old, _ := nemura.LeaveBalance(ctx, tx, ana, nemura.AnnualLeave, 2098)
		es, _ := nemura.LeaveEntries(ctx, tx, ana, nemura.AnnualLeave, 2099)
		if old.Left != 0 || len(es) != 1 || es[0].Expires != "2099-12-31" || es[0].Kind != leave.Transfer {
			t.Errorf("old %+v new %+v", old, es)
		}
		again, err := nemura.CarryForwardLeave(ctx, tx, c.org, now, c.owner, ana, 2098, 12)
		if err != nil || again != 0 {
			t.Errorf("carried twice: %s %v", again, err)
		}
		return nil
	})
	put(2099, leave.Grant, leave.Whole(8), "")
	put(2099, leave.Use, -leave.Whole(3), "")
	lapse := func(on string) leave.Days {
		var l leave.Days
		c.must(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			l, err = nemura.ExpireCarriedLeave(ctx, tx, c.org, now, c.owner, ana, 2099, d(on))
			return err
		})
		return l
	}
	if l := lapse("2099-12-31"); l != 0 {
		t.Errorf("lapsed on the last day: %s", l)
	}
	if l := lapse("2100-01-01"); l != leave.Whole(2) {
		t.Errorf("lapsed %s, want 2: 5 carried less 3 used", l)
	}
	if l := lapse("2100-02-01"); l != 0 {
		t.Errorf("lapsed twice: %s", l)
	}
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		b, _ := nemura.LeaveBalance(ctx, tx, ana, nemura.AnnualLeave, 2099)
		if b.Entitled != leave.Whole(13) || b.Taken != leave.Whole(3) || b.Expired != leave.Whole(2) || b.Left != leave.Whole(8) {
			t.Errorf("balance %+v", b)
		}
		return nil
	})
}

// The leave service shows a person's pattern and balance with the steps and sources of the entitlement, grants once, carries forward, and says plainly when a year
// has no pack or a person has no classification.
// req: NM-LV-02, NM-TIM-01
func TestTheLeaveServiceShowsTheBalanceWithItsWorking(t *testing.T) {
	ctx := context.Background()
	c := newCompany(t)
	now := today.Add(9 * time.Hour)
	packs, err := nemura.NewPacksService(store(t, "nemura_api"), clock.NewFrozen(now))
	if err != nil {
		t.Fatal(err)
	}
	svc := nemura.LeaveService{Store: store(t, "nemura_api"), Clock: clock.NewFrozen(now), Packs: packs}
	s := api.Session{Organisation: c.org, Member: c.owner}
	ana := addEmployee(t, c, "Ana Tan", "2099-01-01")
	year := today.Year()
	if err := svc.SetPattern(ctx, s, ana, d("2099-01-01"), leave.Pattern{Weekdays: []int{1, 2, 3, 4, 5}, HoursPerWeek: 440}); err != nil {
		t.Fatal(err)
	}
	v, err := svc.View(ctx, s, ana, year)
	if err != nil || v.Pattern == nil || v.Pattern.HoursPerWeek != "44.0" || len(v.Types) != 1 || len(v.Types[0].Basis) == 0 || v.Types[0].Balance.Left != "0.00" {
		t.Fatalf("before the grant: %+v %v", v, err)
	}
	g, err := svc.Grant(ctx, s, ana, year)
	if err != nil || g.Days != "7.00" || g.Written != "7.00" || len(g.Sources) == 0 {
		t.Fatalf("grant: %+v %v", g, err)
	}
	if g, _ := svc.Grant(ctx, s, ana, year); g.Written != "0.00" {
		t.Errorf("a second grant wrote %s", g.Written)
	}
	v, _ = svc.View(ctx, s, ana, year)
	if v.Types[0].Balance.Entitled != "7.00" || v.Types[0].Balance.Left != "7.00" {
		t.Errorf("balance %+v", v.Types[0].Balance)
	}
	if carried, err := svc.CarryForward(ctx, s, ana, year); err != nil || carried != "7.00" {
		t.Errorf("carry: %s %v", carried, err)
	}
	if v, _ = svc.View(ctx, s, ana, year+1); v.Types[0].CarryEnd != time.Date(year+2, time.January, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1).Format(time.DateOnly) {
		t.Errorf("carried until %s", v.Types[0].CarryEnd)
	}
	if _, err := svc.Grant(ctx, s, ana, 2025); leaveCode(err) != leave.CodeInvalid {
		t.Errorf("a year before any pack: %v", err)
	}
	if _, err := svc.Own(ctx, s); personRefusal(err) != people.CodeNotFound {
		t.Errorf("an owner with no person record: %v", err)
	}
	var none uuid.UUID
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		none, err = nemura.AddPerson(ctx, tx, c.org, now, c.owner, people.Person{LegalName: "No Class", EmploymentStart: d("2099-01-05")}, people.Job{Title: "X", Worksite: "SG", EmploymentType: "full_time"}, nil)
		return err
	})
	if v, err := svc.View(ctx, s, none, year); err != nil || len(v.Types) != 0 {
		t.Errorf("a person with no classification: %+v %v", v, err)
	}
	if _, err := svc.View(ctx, s, uuid.New(), year); personRefusal(err) != people.CodeNotFound {
		t.Errorf("no such person: %v", err)
	}
}
