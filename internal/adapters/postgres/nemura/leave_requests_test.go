//go:build local

package nemura_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/leave"
	"github.com/cognizhi/fyniq/internal/nemura/people"
)

// team is Ana, the Owner's person, and Ben who reports to her (both have logins, so the reporting line of members follows), Cy who works in the same department
// and a stranger with a login and no line to Ana. Everyone works Monday to Friday from the start of 2026 and Ben has 14 days of annual leave for 2026.
type leaveTeam struct {
	c               *company
	ana, ben, cy    uuid.UUID
	benLogin, cyLog uuid.UUID
}

func newLeaveTeam(t *testing.T) *leaveTeam {
	t.Helper()
	c := newCompany(t)
	now := today.Add(9 * time.Hour)
	lt := &leaveTeam{c: c, benLogin: c.hr}
	job := func(mgr *uuid.UUID) people.Job {
		return people.Job{Title: "Engineer", Department: "Engineering", Worksite: "SG", EmploymentType: "full_time", Manager: mgr}
	}
	class := &people.Classification{ContractType: "service", EACovered: true, PartIVClass: "none", Residency: "citizen"}
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		if lt.ana, err = nemura.AddPerson(ctx, tx, c.org, now, c.owner, people.Person{LegalName: "Ana Tan", EmploymentStart: d("2026-01-05"), MemberID: &c.owner}, job(nil), class); err != nil {
			return err
		}
		if lt.ben, err = nemura.AddPerson(ctx, tx, c.org, now, c.owner, people.Person{LegalName: "Ben Lim", EmploymentStart: d("2026-01-05"), MemberID: &c.hr}, job(&lt.ana), class); err != nil {
			return err
		}
		if lt.cy, err = nemura.AddPerson(ctx, tx, c.org, now, c.owner, people.Person{LegalName: "Cy Wong", EmploymentStart: d("2026-01-05")}, job(&lt.ana), class); err != nil {
			return err
		}
		for _, p := range []uuid.UUID{lt.ana, lt.ben, lt.cy} {
			if err := nemura.SetWorkingPattern(ctx, tx, c.org, now, c.owner, p, d("2026-01-05"), leave.Pattern{Weekdays: []int{1, 2, 3, 4, 5}, HoursPerWeek: 440}); err != nil {
				return err
			}
		}
		_, err = nemura.AppendLeave(ctx, tx, c.org, now, c.owner, lt.ben, nemura.AnnualLeave, 2026, nil, leave.Entry{Kind: leave.Grant, Amount: leave.Whole(14), Source: "test"}, "")
		return err
	})
	return lt
}

func (lt *leaveTeam) ask(from, to string) nemura.LeaveAsk {
	return nemura.LeaveAsk{Person: lt.ben, Type: nemura.AnnualLeave, From: d(from), To: d(to)}
}

func (lt *leaveTeam) apply(a nemura.LeaveAsk) (uuid.UUID, error) {
	var id uuid.UUID
	err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		id, _, err = nemura.ApplyLeave(ctx, tx, lt.c.org, today.Add(10*time.Hour), lt.benLogin, peoplePack(lt.c.t), a)
		return err
	})
	return id, err
}

func (lt *leaveTeam) balance() leave.Balance {
	var b leave.Balance
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		b, err = nemura.LeaveBalance(ctx, tx, lt.ben, nemura.AnnualLeave, 2026)
		return err
	})
	return b
}

func (lt *leaveTeam) scope(member uuid.UUID, all, reports bool) nemura.LeaveScope {
	return nemura.LeaveScope{Member: member, On: d("2026-11-20"), All: all, Reports: reports}
}

func (lt *leaveTeam) list(s nemura.LeaveScope, state string) []nemura.LeaveRequestRow {
	var rows []nemura.LeaveRequestRow
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		rows, err = nemura.LeaveRequests(ctx, tx, s, state, nil)
		return err
	})
	return rows
}

// Applying costs the chargeable days, reserves them in the same transaction, refuses a request beyond the balance with the days left, one that overlaps leave
// already asked for, one before the person started and one with no working pattern; the preview shows who else in the team is away, never the type; and a
// request that was refused or cancelled gives the days back.
// req: NM-LV-03, NM-LV-04
// asvs: V8.2.1
func TestAPersonAppliesForLeaveAndTheDaysAreReservedAtOnce(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	// Monday 6 to Friday 10 April 2026, with Good Friday on the 3rd outside it: five working days.
	id, err := lt.apply(lt.ask("2026-04-06", "2026-04-10"))
	if err != nil {
		t.Fatal(err)
	}
	if b := lt.balance(); b.Booked != leave.Whole(5) || b.Left != leave.Whole(9) {
		t.Errorf("after five days: %+v", b)
	}
	// Over the balance: 10 working days against 9 left, and the message names both.
	_, err = lt.apply(lt.ask("2026-05-04", "2026-05-15"))
	if leaveCode(err) != leave.CodeBalance || !strings.Contains(err.Error(), "10.00 days") || !strings.Contains(err.Error(), "9.00 left") {
		t.Errorf("over the balance: %v", err)
	}
	if _, err := lt.apply(lt.ask("2026-04-09", "2026-04-14")); leaveCode(err) != leave.CodeConflict {
		t.Errorf("overlapping: %v", err)
	}
	if _, err := lt.apply(lt.ask("2025-12-01", "2025-12-02")); leaveCode(err) != leave.CodeInvalid {
		t.Errorf("before the start: %v", err)
	}
	if _, err := lt.apply(lt.ask("2026-12-30", "2027-01-04")); leaveCode(err) != leave.CodeInvalid {
		t.Errorf("over the new year: %v", err)
	}
	if _, err := lt.apply(lt.ask("2026-07-04", "2026-07-05")); leaveCode(err) != leave.CodeInvalid {
		t.Errorf("a weekend: %v", err)
	}
	cyAsk := nemura.LeaveAsk{Person: lt.cy, Type: nemura.AnnualLeave, From: d("2026-06-08"), To: d("2026-06-09")}
	if err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
		_, _, err := nemura.ApplyLeave(ctx, tx, lt.c.org, today, lt.c.owner, peoplePack(t), cyAsk)
		return err
	}); leaveCode(err) != leave.CodeBalance {
		t.Errorf("Cy has no grant: %v", err)
	}
	// Half days: the first day half costs half.
	half := lt.ask("2026-06-08", "2026-06-09")
	half.StartHalf = true
	var pv nemura.LeavePreview
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		pv, err = nemura.PreviewLeave(ctx, tx, peoplePack(t), half, today)
		return err
	})
	if pv.Charge.Days != 150 || !pv.Enough || pv.Left != leave.Whole(9)-150 {
		t.Errorf("preview: %+v", pv)
	}
	// Refusal gives the days back; so does cancelling.
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.DecideLeave(ctx, tx, lt.c.org, today.Add(11*time.Hour), lt.c.owner, id, false, "The release freezes that week.")
	})
	if b := lt.balance(); b.Booked != 0 || b.Left != leave.Whole(14) {
		t.Errorf("after the refusal: %+v", b)
	}
	id2, err := lt.apply(lt.ask("2026-04-06", "2026-04-07"))
	if err != nil {
		t.Fatal(err)
	}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.CancelLeave(ctx, tx, lt.c.org, today.Add(11*time.Hour), lt.benLogin, id2, d("2026-10-15"))
	})
	if b := lt.balance(); b.Booked != 0 || b.Left != leave.Whole(14) {
		t.Errorf("after the cancellation: %+v", b)
	}
	if err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.CancelLeave(ctx, tx, lt.c.org, today, lt.benLogin, id2, d("2026-10-15"))
	}); leaveCode(err) != leave.CodeConflict {
		t.Errorf("cancelling twice: %v", err)
	}
	// A notice was queued for each request and each decision, naming identifiers only.
	var n int
	if err := connect(t, db.SuperURL()).QueryRow(ctx, "SELECT count(*) FROM nemura.outbox WHERE organisation_id = $1 AND kind = 'people.notice' AND payload->>'notice' LIKE 'leave_%'", lt.c.org).Scan(&n); err != nil || n != 3 {
		t.Errorf("notices %d (%v)", n, err)
	}
}

// Ten people's worth of requests for the last two days cannot overbook: the person's head is locked first, so exactly the requests that fit are reserved.
// req: NM-LV-03
// asvs: V11.1.2
func TestTwoRequestsCannotBothTakeTheLastDays(t *testing.T) {
	lt := newLeaveTeam(t)
	if _, err := lt.apply(lt.ask("2026-03-02", "2026-03-13")); err != nil { // ten days of fourteen
		t.Fatal(err)
	}
	days := []string{"2026-05-04", "2026-05-05", "2026-05-06", "2026-05-07", "2026-05-08", "2026-06-08", "2026-06-09", "2026-06-10"}
	var wg sync.WaitGroup
	var mu sync.Mutex
	won, refused := 0, 0
	for _, day := range days {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a := lt.ask(day, day)
			a.StartHalf, a.EndHalf = false, false
			_, err := func() (uuid.UUID, error) {
				var id uuid.UUID
				err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
					var err error
					id, _, err = nemura.ApplyLeave(ctx, tx, lt.c.org, today, lt.benLogin, peoplePack(t), a)
					return err
				})
				return id, err
			}()
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				won++
			case leaveCode(err) == leave.CodeBalance:
				refused++
			default:
				t.Errorf("%v", err)
			}
		}()
	}
	wg.Wait()
	if won != 4 || refused != 4 {
		t.Errorf("won %d, refused %d, with 4 days left", won, refused)
	}
	if b := lt.balance(); b.Left != 0 || b.Booked != leave.Whole(14) {
		t.Errorf("balance %+v", b)
	}
}

// A line manager decides on their reports' requests and the people team on anyone's; nobody decides their own (the database refuses it); a decision is made once; a
// refusal needs a reason; a person who is neither reads nothing of it, and a stranger's decision is not found; a request is never edited or deleted; and the team's
// "away" shows names and dates without the type.
// req: NM-LV-04, NM-LV-05
// asvs: V8.2.1, V8.3.1
func TestOnlyThePersonsManagerOrThePeopleTeamSeesAndDecides(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	id, err := lt.apply(lt.ask("2026-04-06", "2026-04-08"))
	if err != nil {
		t.Fatal(err)
	}
	managers := lt.scope(lt.c.owner, false, true)
	if rows := lt.list(managers, "pending"); len(rows) != 1 || rows[0].ID != id || rows[0].PersonName != "Ben Lim" || rows[0].Days != leave.Whole(3) {
		t.Fatalf("the manager's inbox: %+v", rows)
	}
	if rows := lt.list(lt.scope(lt.c.pay, false, false), ""); len(rows) != 0 {
		t.Errorf("a stranger reads %d requests", len(rows))
	}
	if rows := lt.list(lt.scope(lt.c.pay, false, true), ""); len(rows) != 0 {
		t.Errorf("a manager of nobody reads %d requests", len(rows))
	}
	if rows := lt.list(lt.scope(lt.benLogin, false, false), ""); len(rows) != 1 {
		t.Errorf("the person reads their own: %d", len(rows))
	}
	if rows := lt.list(lt.scope(lt.c.hr, true, false), ""); len(rows) != 1 {
		t.Errorf("the people team reads all: %d", len(rows))
	}
	if err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.LeaveRequestFor(ctx, tx, lt.scope(lt.c.pay, false, true), id)
		return err
	}); leaveCode(err) != leave.CodeNotFound {
		t.Errorf("a stranger's read of the request: %v", err)
	}
	// The requester cannot approve their own request, even as the people team: the database refuses.
	if err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.DecideLeave(ctx, tx, lt.c.org, today, lt.benLogin, id, true, "")
	}); err == nil {
		t.Error("a person approved their own request")
	}
	// A refusal needs a reason; an approval is made once.
	if err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.DecideLeave(ctx, tx, lt.c.org, today, lt.c.owner, id, false, "")
	}); leaveCode(err) != leave.CodeInvalid {
		t.Errorf("a refusal with no reason: %v", err)
	}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.DecideLeave(ctx, tx, lt.c.org, today.Add(time.Hour), lt.c.owner, id, true, "")
	})
	if err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.DecideLeave(ctx, tx, lt.c.org, today, lt.c.owner, id, false, "changed my mind")
	}); leaveCode(err) != leave.CodeConflict {
		t.Errorf("deciding twice: %v", err)
	}
	// Evidence: dates never change, nothing is deleted, an approval cannot go back to pending.
	su := connect(t, db.SuperURL())
	for _, q := range []string{"UPDATE nemura.leave_request SET end_date = end_date + 1", "DELETE FROM nemura.leave_request", "UPDATE nemura.leave_request SET state = 'pending', decided_by = NULL, decided_at = NULL"} {
		if _, err := su.Exec(ctx, q); err == nil {
			t.Errorf("%s was allowed", q)
		}
	}
	// A teammate's preview shows that Ben is away, with no type and no state.
	var pv nemura.LeavePreview
	cy := nemura.LeaveAsk{Person: lt.cy, Type: nemura.AnnualLeave, From: d("2026-04-07"), To: d("2026-04-07")}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		pv, err = nemura.PreviewLeave(ctx, tx, peoplePack(t), cy, today)
		return err
	})
	if len(pv.Away) != 1 || pv.Away[0].Name != "Ben Lim" || pv.Away[0].From != d("2026-04-06") {
		t.Errorf("away: %+v", pv.Away)
	}
	// Another organisation's request is not found, by id or by list.
	other := newCompany(t)
	other.must(func(ctx context.Context, tx pgx.Tx) error {
		if rows, err := nemura.LeaveRequests(ctx, tx, nemura.LeaveScope{Member: other.owner, On: d("2026-11-20"), All: true}, "", nil); err != nil || len(rows) != 0 {
			t.Errorf("another organisation reads %d requests (%v)", len(rows), err)
		}
		return nil
	})
}

// Taken leave is settled once: each approved request whose last day has passed turns its reservation into a use, and a second run changes nothing.
// req: NM-LV-02, NM-LV-13
func TestTakenLeaveIsSettledOnce(t *testing.T) {
	lt := newLeaveTeam(t)
	id, err := lt.apply(lt.ask("2026-04-06", "2026-04-08"))
	if err != nil {
		t.Fatal(err)
	}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.DecideLeave(ctx, tx, lt.c.org, today, lt.c.owner, id, true, "")
	})
	settle := func(on string) (int, leave.Days) {
		var n int
		var days leave.Days
		lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			n, days, err = nemura.SettleLeave(ctx, tx, lt.c.org, today.Add(time.Hour), lt.c.owner, d(on))
			return err
		})
		return n, days
	}
	if n, _ := settle("2026-04-08"); n != 0 {
		t.Errorf("settled on the last day: %d", n)
	}
	if n, days := settle("2026-04-09"); n != 1 || days != leave.Whole(3) {
		t.Errorf("settled %d, %s", n, days)
	}
	if n, _ := settle("2026-05-01"); n != 0 {
		t.Errorf("settled twice: %d", n)
	}
	if b := lt.balance(); b.Taken != leave.Whole(3) || b.Booked != 0 || b.Left != leave.Whole(11) {
		t.Errorf("balance %+v", b)
	}
	// A settled request can no longer be cancelled, nor can one that has started.
	if err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.CancelLeave(ctx, tx, lt.c.org, today, lt.benLogin, id, d("2026-05-01"))
	}); leaveCode(err) != leave.CodeConflict {
		t.Errorf("cancelling taken leave: %v", err)
	}
}

// The service asks for the signed-in person only, previews with the day-by-day cost and the colleagues away, lists by scope, lets the manager approve a report's
// request but not their own, and cancels only one's own.
// req: NM-LV-03, NM-LV-04, NM-LV-05
func TestTheLeaveRequestServiceAsksListsDecidesAndCancels(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := d("2026-10-15").Add(9 * time.Hour) // before the December leave starts
	packs, err := nemura.NewPacksService(store(t, "nemura_api"), clock.NewFrozen(now))
	if err != nil {
		t.Fatal(err)
	}
	svc := nemura.LeaveRequestService{Store: store(t, "nemura_api"), Clock: clock.NewFrozen(now), Packs: packs}
	ben := api.Session{Organisation: lt.c.org, Member: lt.benLogin}
	ana := api.Session{Organisation: lt.c.org, Member: lt.c.owner}
	pv, err := svc.Preview(ctx, ben, api.LeaveAsk{Type: "annual", From: "2026-12-21", To: "2026-12-27"})
	if err != nil || pv.Days != "4.00" || pv.Left != "10.00" || !pv.Enough || len(pv.Counts) != 7 || pv.Counts[4].Why != "Christmas Day" || pv.Counts[5].Why != "a rest day" {
		t.Fatalf("preview: %+v %v", pv, err)
	}
	id, days, err := svc.Apply(ctx, ben, api.LeaveAsk{Type: "annual", From: "2026-12-21", To: "2026-12-27", Note: "Family"})
	if err != nil || days != "4.00" {
		t.Fatalf("apply: %s %s %v", id, days, err)
	}
	if _, _, err := svc.Apply(ctx, ben, api.LeaveAsk{Type: "annual", From: "nonsense", To: "2026-04-07"}); leaveCode(err) != leave.CodeInvalid {
		t.Errorf("a bad date: %v", err)
	}
	if _, _, err := svc.Apply(ctx, ben, api.LeaveAsk{Type: "sick", From: "2026-05-04", To: "2026-05-04"}); leaveCode(err) != leave.CodeInvalid {
		t.Errorf("a type that is not asked for: %v", err)
	}
	if _, _, err := svc.Apply(ctx, api.Session{Organisation: lt.c.org, Member: lt.c.pay}, api.LeaveAsk{Type: "annual", From: "2026-05-04", To: "2026-05-04"}); personRefusal(err) != people.CodeNotFound {
		t.Errorf("a login with no person: %v", err)
	}
	mine, err := svc.List(ctx, ben, api.LeaveScope{}, "")
	if err != nil || len(mine) != 1 || mine[0].Days != "4.00" || mine[0].Note != "Family" || mine[0].State != "pending" {
		t.Fatalf("own list: %+v %v", mine, err)
	}
	if list, _ := svc.List(ctx, api.Session{Organisation: lt.c.org, Member: lt.c.pay}, api.LeaveScope{}, ""); len(list) != 0 {
		t.Errorf("a stranger lists %d", len(list))
	}
	inbox, err := svc.List(ctx, ana, api.LeaveScope{Reports: true, Decide: true}, "pending")
	if err != nil || len(inbox) != 1 {
		t.Fatalf("the manager's inbox: %+v %v", inbox, err)
	}
	rid := uuid.MustParse(id)
	if err := svc.Decide(ctx, ben, api.LeaveScope{Reports: true, Decide: true}, rid, true, ""); err == nil {
		t.Error("a person decided their own request")
	}
	if err := svc.Decide(ctx, ana, api.LeaveScope{Reports: true, Decide: true}, rid, true, ""); err != nil {
		t.Fatal(err)
	}
	if r, err := svc.Get(ctx, ana, api.LeaveScope{Reports: true}, rid); err != nil || r.State != "approved" || r.Decided == "" {
		t.Errorf("get: %+v %v", r, err)
	}
	if err := svc.Cancel(ctx, ana, rid); personRefusal(err) != "" || leaveCode(err) != leave.CodeNotFound {
		t.Errorf("the manager cancelled the report's request: %v", err)
	}
	if err := svc.Cancel(ctx, ben, rid); err != nil {
		t.Errorf("the person's own cancellation: %v", err)
	}
	n, taken, err := svc.Settle(ctx, ana, d("2026-12-01"))
	if err != nil || n != 0 || taken != "0.00" {
		t.Errorf("settle: %d %s %v", n, taken, err)
	}
}
