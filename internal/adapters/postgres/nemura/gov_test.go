//go:build local

package nemura_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/nemura/checks"
	"github.com/cognizhi/fyniq/internal/nemura/leave"
	"github.com/cognizhi/fyniq/internal/nemura/people"
)

func (lt *leaveTeam) child(born string, citizenship string) uuid.UUID {
	var id uuid.UUID
	b := d(born)
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		id, err = nemura.AddDependant(ctx, tx, lt.c.org, today.Add(9*time.Hour), lt.c.owner, lt.ben, people.Dependant{Relationship: "child", BirthDate: &b, Citizenship: citizenship, DeclaredOn: d("2026-07-01")}, d("2026-07-01"))
		return err
	})
	return id
}

func (lt *leaveTeam) gov(a nemura.GovAsk) (uuid.UUID, error) {
	var id uuid.UUID
	err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		id, _, err = nemura.RecordGovLeave(ctx, tx, lt.c.org, today.Add(10*time.Hour), lt.c.owner, peoplePack(lt.c.t), a)
		return err
	})
	return id, err
}

// Government-paid leave is recorded for a declared child from the pack's rule for the birth date: a citizen child and three months' service are needed, the dates must
// fall in the scheme's window and weeks, a block never overlaps other leave, the claim deadline is three months after the last day, and a record is evidence.
// req: NM-LV-07, NM-LV-14, NM-LV-11
// asvs: V8.2.1
func TestGovernmentPaidLeaveIsRecordedFromThePacksRuleForTheBirthDate(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	child := lt.child("2026-06-10", "citizen")
	foreign := lt.child("2026-06-12", "other")
	ask := func(scheme, from, to string) nemura.GovAsk {
		return nemura.GovAsk{Person: lt.ben, Dependant: child, Scheme: scheme, ChildOrder: 1, From: d(from), To: d(to)}
	}
	// Paternity: four weeks from the birth.
	id, err := lt.gov(ask(leave.Paternity, "2026-06-10", "2026-07-07"))
	if err != nil {
		t.Fatal(err)
	}
	var deadline time.Time
	var days int
	if err := connect(t, db.SuperURL()).QueryRow(ctx, "SELECT claim_deadline, days FROM nemura.gov_leave WHERE id = $1", id).Scan(&deadline, &days); err != nil || deadline.Format(time.DateOnly) != "2026-10-07" || days != 28 {
		t.Errorf("deadline %s, days %d (%v)", deadline.Format(time.DateOnly), days, err)
	}
	// Refused: a day more than the scheme's weeks, leave before the birth, a non-citizen child, a child of someone else, overlapping leave, annual leave on those days.
	if _, err := lt.gov(ask(leave.Paternity, "2026-07-08", "2026-07-08")); leaveCode(err) != leave.CodeInvalid || !strings.Contains(err.Error(), "28 days") {
		t.Errorf("a day too many: %v", err)
	}
	if _, err := lt.gov(ask(leave.Paternity, "2026-06-09", "2026-06-09")); leaveCode(err) != leave.CodeInvalid {
		t.Errorf("before the birth: %v", err)
	}
	if _, err := lt.gov(nemura.GovAsk{Person: lt.ben, Dependant: foreign, Scheme: leave.Paternity, ChildOrder: 1, From: d("2026-06-12"), To: d("2026-06-18")}); leaveCode(err) != leave.CodeInvalid || !strings.Contains(err.Error(), "Singapore Citizen") {
		t.Errorf("a non-citizen child: %v", err)
	}
	if _, err := lt.gov(nemura.GovAsk{Person: lt.cy, Dependant: child, Scheme: leave.Paternity, ChildOrder: 1, From: d("2026-06-12"), To: d("2026-06-18")}); leaveCode(err) != leave.CodeInvalid {
		t.Errorf("another person's child: %v", err)
	}
	if _, err := lt.gov(ask(leave.SharedParental, "2026-07-20", "2026-07-26")); err != nil {
		t.Errorf("shared parental leave after paternity: %v", err)
	}
	if _, err := lt.gov(ask(leave.SharedParental, "2026-07-22", "2026-07-23")); leaveCode(err) != leave.CodeConflict {
		t.Errorf("overlapping government-paid leave: %v", err)
	}
	if _, err := lt.apply(lt.ask("2026-06-15", "2026-06-16")); leaveCode(err) != leave.CodeConflict {
		t.Errorf("annual leave inside paternity leave: %v", err)
	}
	if _, err := lt.notify(lt.sick(leave.Outpatient, "2026-06-17", "2026-06-17"), "2026-06-17"); leaveCode(err) != leave.CodeConflict {
		t.Errorf("sick leave inside paternity leave: %v", err)
	}
	// Under three months' service before the birth: Cy started 2026-01-05 too, so a child born in March has no eligible parent.
	early := lt.child("2026-03-20", "citizen")
	if _, err := lt.gov(nemura.GovAsk{Person: lt.ben, Dependant: early, Scheme: leave.Paternity, ChildOrder: 1, From: d("2026-03-20"), To: d("2026-03-26")}); leaveCode(err) != leave.CodeInvalid || !strings.Contains(err.Error(), "fewer than 3 months") {
		t.Errorf("under three months' service: %v", err)
	}
	// Evidence.
	su := connect(t, db.SuperURL())
	for _, q := range []string{"UPDATE nemura.gov_leave SET days = 1", "DELETE FROM nemura.gov_leave"} {
		if _, err := su.Exec(ctx, q); err == nil {
			t.Errorf("%s was allowed", q)
		}
	}
	// No audit event carries the child's date of birth.
	var leaked int
	if err := su.QueryRow(ctx, "SELECT count(*) FROM nemura.audit_event WHERE organisation_id = $1 AND body::text LIKE '%2026-06-10%' AND body::text LIKE '%gov_leave%' AND body::text LIKE '%birth%'", lt.c.org).Scan(&leaked); err != nil || leaked != 0 {
		t.Errorf("a date of birth in an audit event: %d (%v)", leaked, err)
	}
}

// A claim is filed on the portal by a person and tracked here: its amount, the day, the portal's reference; receipts and rejections are separate records and the state is
// worked out from them; an amount above the scheme's cap is recorded and flagged, never refused; and the checks warn before the deadline.
// req: NM-LV-07, NM-LV-14
// asvs: V8.2.1
func TestAClaimIsTrackedFromFilingToReceiptAndTheDeadlineIsChecked(t *testing.T) {
	lt := newLeaveTeam(t)
	v := peoplePack(t)
	child := lt.child("2026-06-10", "citizen")
	leaveID, err := lt.gov(nemura.GovAsk{Person: lt.ben, Dependant: child, Scheme: leave.Paternity, ChildOrder: 1, From: d("2026-06-10"), To: d("2026-07-07")})
	if err != nil {
		t.Fatal(err)
	}
	resolve := func(id string) (pack.Version, bool) { return v, id == v.ID }
	now := today.Add(11 * time.Hour)
	file := func(amount, on string) (uuid.UUID, bool, error) {
		var id uuid.UUID
		var over bool
		c, err := leave.ParseCents(amount)
		if err != nil {
			return id, false, err
		}
		err = lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			id, over, err = nemura.FileGovClaim(ctx, tx, lt.c.org, now, lt.c.owner, leaveID, resolve, c, d(on), "GPL-001")
			return err
		})
		return id, over, err
	}
	list := func() []nemura.GovRow {
		var rows []nemura.GovRow
		lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			rows, err = nemura.GovLeaves(ctx, tx, resolve)
			return err
		})
		return rows
	}
	// Before any claim the check says the deadline is near once within 30 days of 2026-10-07, and not before.
	facts := func() []checks.GovClaim {
		var f []checks.GovClaim
		lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			f, err = nemura.GovClaimFacts(ctx, tx)
			return err
		})
		return f
	}
	if got := checks.GovClaims(facts(), d("2026-09-06")); len(got) != 0 {
		t.Errorf("31 days before: %+v", got)
	}
	if got := checks.GovClaims(facts(), d("2026-09-07")); len(got) != 1 || got[0].Check != 10 || got[0].Status != checks.Due || !strings.Contains(got[0].Message, "in 30 days") {
		t.Errorf("30 days before: %+v", got)
	}
	if got := checks.GovClaims(facts(), d("2026-10-08")); len(got) != 1 || !strings.Contains(got[0].Message, "had to be filed by 2026-10-07") {
		t.Errorf("after the deadline: %+v", got)
	}
	// Filing: S$6,000 is within the S$10,000 for four weeks; S$5,000 more passes the cap and is flagged but recorded.
	first, over, err := file("6000", "2026-07-20")
	if err != nil || over {
		t.Fatalf("first claim: %v %v", over, err)
	}
	if _, over, err := file("5000", "2026-07-21"); err != nil || !over {
		t.Errorf("a claim above the cap: %v %v", over, err)
	}
	if got := checks.GovClaims(facts(), d("2026-09-20")); len(got) != 0 {
		t.Errorf("a filed claim still warns: %+v", got)
	}
	if _, _, err := file("0", "2026-07-20"); leaveCode(err) != leave.CodeInvalid {
		t.Errorf("a zero claim: %v", err)
	}
	if _, _, err := file("12.345", "2026-07-20"); leaveCode(err) != leave.CodeInvalid {
		t.Errorf("a claim of a fraction of a cent: %v", err)
	}
	// Events: a partial receipt, then the rest; a rejection on the second claim.
	event := func(claim uuid.UUID, kind, amount, on string) error {
		c, _ := leave.ParseCents(amount)
		return lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
			_, err := nemura.RecordClaimEvent(ctx, tx, lt.c.org, now, lt.c.owner, claim, kind, c, d(on), "")
			return err
		})
	}
	if err := event(first, "receipt", "2500", "2026-08-10"); err != nil {
		t.Fatal(err)
	}
	row := list()[0]
	if len(row.Claims) != 2 || row.Claims[0].State() != "part_paid" || row.Claims[1].State() != "filed" || !row.Claims[1].OverGuide || row.GuideMaximum == nil || *row.GuideMaximum != 1000000 {
		t.Errorf("after a partial receipt: %+v", row)
	}
	if err := event(first, "receipt", "3500", "2026-08-20"); err != nil {
		t.Fatal(err)
	}
	if err := event(row.Claims[1].ID, "rejection", "0", "2026-08-21"); err != nil {
		t.Fatal(err)
	}
	row = list()[0]
	if row.Claims[0].State() != "paid" || row.Claims[0].Received != 600000 || row.Claims[1].State() != "rejected" {
		t.Errorf("after the rest: %+v", row.Claims)
	}
	if event(first, "receipt", "0", "2026-08-20") == nil || event(first, "rejection", "5", "2026-08-20") == nil || event(first, "refund", "5", "2026-08-20") == nil {
		t.Error("an event with the wrong amount for its kind was accepted")
	}
	if err := event(uuid.New(), "rejection", "0", "2026-08-20"); leaveCode(err) != leave.CodeNotFound {
		t.Errorf("an event on no claim: %v", err)
	}
	su := connect(t, db.SuperURL())
	for _, q := range []string{"UPDATE nemura.gov_claim SET amount = 1", "DELETE FROM nemura.gov_claim", "UPDATE nemura.gov_claim_event SET amount = 1", "DELETE FROM nemura.gov_claim_event"} {
		if _, err := su.Exec(context.Background(), q); err == nil {
			t.Errorf("%s was allowed", q)
		}
	}
}

// A request waiting more than three working days, and a balance below zero where the type forbids it, are N.1 #4 findings; public holidays and weekends do not count.
// req: NM-LV-02, NM-LV-04
func TestARequestWaitingMoreThanThreeWorkingDaysIsFlagged(t *testing.T) {
	lt := newLeaveTeam(t)
	v := peoplePack(t)
	if _, err := lt.apply(lt.ask("2026-12-07", "2026-12-07")); err != nil {
		t.Fatal(err)
	}
	var waiting []checks.Waiting
	var neg []checks.Negative
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		waiting, neg, err = nemura.LeaveFacts(ctx, tx)
		return err
	})
	if len(waiting) != 1 || len(neg) != 0 {
		t.Fatalf("facts: %+v %+v", waiting, neg)
	}
	// The request was made now (real time); judge it as of a day relative to that.
	req := waiting[0].Requested
	if got := checks.LeaveWaiting(v, waiting, neg, req.AddDate(0, 0, 1)); len(got) != 0 {
		t.Errorf("a day later: %+v", got)
	}
	over := checks.LeaveWaiting(v, []checks.Waiting{{Person: "p", Requested: d("2026-04-01")}}, nil, d("2026-04-07")) // Wed to Tue, over Good Friday and a weekend: Mon, Tue = 2 + Thu = 3 (no more than 3)
	if len(over) != 0 {
		t.Errorf("three working days: %+v", over)
	}
	if got := checks.LeaveWaiting(v, []checks.Waiting{{Person: "p", Requested: d("2026-04-01")}}, nil, d("2026-04-08")); len(got) != 1 || got[0].Check != 4 || got[0].Status != checks.Warn {
		t.Errorf("four working days: %+v", got)
	}
	if got := checks.LeaveWaiting(v, nil, []checks.Negative{{Person: "p", Type: "annual", Year: 2026, Left: "-1.00"}}, d("2026-04-08")); len(got) != 1 || !strings.Contains(got[0].Message, "below zero") {
		t.Errorf("a negative balance: %+v", got)
	}
}
