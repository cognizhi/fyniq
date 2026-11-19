//go:build local

package nemura_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/nemura/leave"
	"github.com/cognizhi/fyniq/internal/nemura/people"
)

// kinds is what a person's month holds, by date.
func kinds(p nemura.CalendarPerson) map[string]string {
	m := map[string]string{}
	for _, e := range p.Entries {
		m[e.Date.Format(time.DateOnly)] = e.Kind + ":" + e.Label
	}
	return m
}

func find(people []nemura.CalendarPerson, name string) (nemura.CalendarPerson, bool) {
	for _, p := range people {
		if p.Name == name {
			return p, true
		}
	}
	return nemura.CalendarPerson{}, false
}

// The team calendar shows each person's month: their leave and sick leave as "Away" to anyone without the right to the type, the type to the person, their line
// manager and the people team, sick leave as such only to the person and the HR officer and administrator, a request waiting only to those who decide on it, and each
// person's public holidays as their working pattern observes them (NM-LV-05, NM-LV-04, NM-LV-12).
// req: NM-LV-05
// asvs: V8.2.1, V8.3.1
func TestTheCalendarShowsAwayToThoseWithoutTheRightToTheType(t *testing.T) {
	lt := newLeaveTeam(t)
	now := today.Add(9 * time.Hour)
	// Dee has a login and works in the same department under the same manager: a teammate, not a report.
	var deeLogin uuid.UUID
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		if deeLogin, err = nemura.AddMember(ctx, tx, lt.c.org, now, actor, "Dee Dale"); err != nil {
			return err
		}
		dee, err := nemura.AddPerson(ctx, tx, lt.c.org, now, lt.c.owner, people.Person{LegalName: "Dee Dale", EmploymentStart: d("2026-01-05"), MemberID: &deeLogin},
			people.Job{Title: "Engineer", Department: "Engineering", Worksite: "SG", EmploymentType: "full_time", Manager: &lt.ana},
			&people.Classification{ContractType: "service", EACovered: true, PartIVClass: "none", Residency: "citizen"})
		if err != nil {
			return err
		}
		return nemura.SetWorkingPattern(ctx, tx, lt.c.org, now, lt.c.owner, dee, d("2026-01-05"), leave.Pattern{Weekdays: []int{1, 2, 3, 4}, HoursPerWeek: 350})
	})
	// Ben: approved annual leave Mon 6 to Wed 8 April, a pending request on 13 April, and sick on Mon 4 May.
	id, err := lt.apply(lt.ask("2026-04-06", "2026-04-08"))
	if err != nil {
		t.Fatal(err)
	}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.DecideLeave(ctx, tx, lt.c.org, now, lt.c.owner, id, true, "")
	})
	if _, err := lt.apply(lt.ask("2026-04-13", "2026-04-13")); err != nil {
		t.Fatal(err)
	}
	if _, err := lt.notify(lt.sick(leave.Outpatient, "2026-05-04", "2026-05-04"), "2026-05-04"); err != nil {
		t.Fatal(err)
	}
	v := peoplePack(t)
	view := func(vw nemura.CalendarView, month string) []nemura.CalendarPerson {
		vw.On = d("2026-04-01")
		var got []nemura.CalendarPerson
		lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			got, err = nemura.LeaveCalendar(ctx, tx, v, vw, d(month))
			return err
		})
		return got
	}
	// A teammate sees Ben away with no type, no pending request, and only the teammates (Ben, Ana, Cy and Dee share the department).
	dee := view(nemura.CalendarView{Member: deeLogin}, "2026-04-01")
	ben, ok := find(dee, "Ben Lim")
	if !ok {
		t.Fatalf("a teammate does not see Ben: %d people", len(dee))
	}
	if k := kinds(ben); k["2026-04-06"] != "away:Away" || k["2026-04-08"] != "away:Away" || k["2026-04-13"] != "" || k["2026-04-03"] != "holiday:Good Friday" {
		t.Errorf("a teammate's view of Ben: %v", k)
	}
	if b, _ := find(view(nemura.CalendarView{Member: deeLogin}, "2026-05-01"), "Ben Lim"); kinds(b)["2026-05-04"] != "away:Away" {
		t.Errorf("sick leave is Away to a teammate: %v", kinds(b))
	}
	// A stranger with a login and no person sees nobody.
	if got := view(nemura.CalendarView{Member: lt.c.pay}, "2026-04-01"); len(got) != 0 {
		t.Errorf("a stranger sees %d people", len(got))
	}
	// The manager sees the type and the pending request, but sick leave only as Away.
	ana := view(nemura.CalendarView{Member: lt.c.owner, Reports: true}, "2026-04-01")
	b, _ := find(ana, "Ben Lim")
	if k := kinds(b); k["2026-04-06"] != "leave:Annual leave" || k["2026-04-13"] != "pending:Annual leave" {
		t.Errorf("the manager's view of Ben: %v", k)
	}
	if b, _ := find(view(nemura.CalendarView{Member: lt.c.owner, Reports: true}, "2026-05-01"), "Ben Lim"); kinds(b)["2026-05-04"] != "away:Away" {
		t.Errorf("the manager sees the type of sick leave: %v", kinds(b))
	}
	// The people team sees everyone, with sick leave as such for the HR officer and administrator.
	hr := view(nemura.CalendarView{Member: lt.c.hr, All: true, SickAll: true}, "2026-05-01")
	if b, _ := find(hr, "Ben Lim"); kinds(b)["2026-05-04"] != "sick:Sick leave" {
		t.Errorf("the people team's view of sick leave: %v", kinds(b))
	}
	if b, _ := find(view(nemura.CalendarView{Member: lt.c.pay, All: true}, "2026-05-01"), "Ben Lim"); kinds(b)["2026-05-04"] != "away:Away" {
		t.Errorf("Payroll sees sick leave as such: %v", kinds(b))
	}
	// A person's own sick leave is theirs to see.
	if b, _ := find(view(nemura.CalendarView{Member: lt.benLogin}, "2026-05-01"), "Ben Lim"); kinds(b)["2026-05-04"] != "sick:Sick leave" {
		t.Errorf("Ben's own sick leave: %v", kinds(b))
	}
	// Public holidays follow the pattern: a four-day week observes Friday's holiday on the next working day, Monday 6 April for Good Friday 3 April.
	deeSelf := view(nemura.CalendarView{Member: deeLogin}, "2026-04-01")
	me, _ := find(deeSelf, "Dee Dale")
	if k := kinds(me); k["2026-04-06"] != "holiday:Good Friday (observed)" || k["2026-04-03"] != "" {
		t.Errorf("Dee's holidays: %v", k)
	}
	if len(me.Weekdays) != 4 {
		t.Errorf("Dee's working days: %v", me.Weekdays)
	}
}
