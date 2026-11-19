//go:build local

package nemura_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/nemura/leave"
	"github.com/cognizhi/fyniq/internal/nemura/outbox"
)

// The month-end accrual is drafted once from the carried-forward annual leave still untaken, approved once by someone other than the drafter, and never edited or deleted; its
// amounts and journal are the pay structure's and the link's, so Nemura keeps days only (NM-LV-09).
// req: NM-LV-09
// asvs: V8.2.1
func TestTheMonthEndAccrualIsDraftedOnceFromCarriedForwardLeaveAndApprovedByAnotherPerson(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := today.Add(9 * time.Hour)
	// Ben carries 7 days from 2025 into 2026 (to 31 December 2026), and has 14 days of 2026 of his own.
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		if _, err := nemura.AppendLeave(ctx, tx, lt.c.org, now, lt.c.owner, lt.ben, nemura.AnnualLeave, 2025, nil, leave.Entry{Kind: leave.Grant, Amount: leave.Whole(7), Source: "test"}, ""); err != nil {
			return err
		}
		_, err := nemura.CarryForwardLeave(ctx, tx, lt.c.org, now, lt.c.owner, lt.ben, 2025, 12)
		return err
	})
	draft := func(month string) (int, error) {
		var n int
		err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			n, err = nemura.DraftLeaveAccrual(ctx, tx, lt.c.org, now, lt.c.owner, d(month))
			return err
		})
		return n, err
	}
	n, err := draft("2026-03-15")
	if err != nil || n != 1 {
		t.Fatalf("draft: %d %v", n, err)
	}
	if _, err := draft("2026-03-01"); leaveCode(err) != leave.CodeConflict {
		t.Errorf("drafting a month twice: %v", err)
	}
	list := func(month string) []nemura.AccrualRow {
		var rows []nemura.AccrualRow
		lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			rows, err = nemura.LeaveAccruals(ctx, tx, d(month))
			return err
		})
		return rows
	}
	if rows := list("2026-03-01"); len(rows) != 1 || rows[0].PersonName != "Ben Lim" || rows[0].Carried != leave.Whole(7) || rows[0].AnnualLeft != leave.Whole(21) || rows[0].State != "draft" {
		t.Errorf("draft: %+v", rows)
	}
	// Three days are booked: April's draft carries the 4 untaken; May after the carry lapsed carries none.
	if _, err := lt.apply(lt.ask("2026-04-06", "2026-04-08")); err != nil {
		t.Fatal(err)
	}
	if n, err := draft("2026-04-01"); err != nil || n != 1 {
		t.Fatalf("April: %d %v", n, err)
	}
	if rows := list("2026-04-01"); rows[0].Carried != leave.Whole(4) {
		t.Errorf("April's carried days: %+v", rows)
	}
	if n, err := draft("2027-01-01"); err != nil || n != 0 {
		t.Errorf("a month after the carried days lapsed: %d %v", n, err)
	}
	// Approval: not by the drafter, once; then evidence.
	if err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.ApproveLeaveAccrual(ctx, tx, lt.c.org, now, lt.c.owner, d("2026-03-01"))
		return err
	}); leaveCode(err) != leave.CodeRole {
		t.Errorf("approving one's own draft: %v", err)
	}
	var approved int
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		approved, err = nemura.ApproveLeaveAccrual(ctx, tx, lt.c.org, now.Add(time.Hour), lt.c.hr, d("2026-03-01"))
		return err
	})
	if approved != 1 || list("2026-03-01")[0].State != "approved" {
		t.Errorf("approval: %d %+v", approved, list("2026-03-01"))
	}
	if err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.ApproveLeaveAccrual(ctx, tx, lt.c.org, now, lt.c.hr, d("2026-03-01"))
		return err
	}); leaveCode(err) != leave.CodeConflict {
		t.Errorf("approving twice: %v", err)
	}
	su := connect(t, db.SuperURL())
	for _, q := range []string{"UPDATE nemura.leave_accrual SET carried_days = 1", "DELETE FROM nemura.leave_accrual", "UPDATE nemura.leave_accrual SET state = 'draft', approved_by = NULL, approved_at = NULL WHERE state = 'approved'"} {
		if _, err := su.Exec(ctx, q); err == nil {
			t.Errorf("%s was allowed", q)
		}
	}
}

// Availability is the working days by pattern less public holidays and the days away on approved leave, sick leave and government-paid leave, at the hours of a working day
// (NM-TIM-01, NM-LV-09).
// req: NM-TIM-01
func TestAvailableHoursAreTheWorkingDaysLessHolidaysAndLeave(t *testing.T) {
	lt := newLeaveTeam(t)
	v := peoplePack(t)
	avail := func(person, from, to string) (nemura.Availability, error) {
		var a nemura.Availability
		err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			a, err = nemura.AvailableHours(ctx, tx, v, lt.ben, d(from), d(to))
			return err
		})
		return a, err
	}
	// April 2026: 22 weekdays, less Good Friday on the 3rd: 21 working days, at 8.8 hours.
	a, err := avail("", "2026-04-01", "2026-04-30")
	if err != nil || a.WorkingDays != leave.Whole(21) || a.AwayDays != 0 || a.HoursPerDay != 880 || a.Hours != 18480 {
		t.Fatalf("April: %+v %v", a, err)
	}
	// Three days of approved leave and a half day.
	id, err := lt.apply(lt.ask("2026-04-06", "2026-04-08"))
	if err != nil {
		t.Fatal(err)
	}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.DecideLeave(ctx, tx, lt.c.org, today, lt.c.owner, id, true, "")
	})
	half := lt.ask("2026-04-20", "2026-04-20")
	half.StartHalf = true
	id2, err := lt.apply(half)
	if err != nil {
		t.Fatal(err)
	}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.DecideLeave(ctx, tx, lt.c.org, today, lt.c.owner, id2, true, "")
	})
	if a, _ := avail("", "2026-04-01", "2026-04-30"); a.AwayDays != 350 || a.Hours != (leave.Whole(21)-350)*880/100 {
		t.Errorf("with leave: %+v", a)
	}
	// A pending request is not away yet; sick leave is.
	if _, err := lt.apply(lt.ask("2026-04-27", "2026-04-27")); err != nil {
		t.Fatal(err)
	}
	if _, err := lt.notify(lt.sick(leave.Outpatient, "2026-04-14", "2026-04-14"), "2026-04-14"); err != nil {
		t.Fatal(err)
	}
	if a, _ := avail("", "2026-04-01", "2026-04-30"); a.AwayDays != 450 {
		t.Errorf("with sick leave: %+v", a)
	}
	// A shorter period clips the leave; a period backwards, or with no holidays published, is refused.
	if a, _ := avail("", "2026-04-07", "2026-04-09"); a.WorkingDays != leave.Whole(3) || a.AwayDays != leave.Whole(2) {
		t.Errorf("a clipped period: %+v", a)
	}
	if _, err := avail("", "2026-04-30", "2026-04-01"); leaveCode(err) != leave.CodeInvalid {
		t.Errorf("backwards: %v", err)
	}
	if _, err := avail("", "2028-04-01", "2028-04-30"); leaveCode(err) != leave.CodeInvalid {
		t.Errorf("no holidays published: %v", err)
	}
}

// The worker drafts the previous month's accrual by itself once a month while Leave is on: the row is enqueued once whatever the sweeps, drafting it twice is done and
// not an error, the draft says no member made it, a person approves it, and the worker's role holds no other write on leave (NM-LV-09).
// req: NM-LV-09
// asvs: V8.2.1, V8.3.1
func TestTheWorkerDraftsTheAccrualOnceAMonthWhileLeaveIsOn(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := d("2026-04-02").Add(3 * time.Hour)
	worker := store(t, "nemura_worker")
	// Carried-forward days for Ben, so the March draft has a line.
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		if _, err := nemura.AppendLeave(ctx, tx, lt.c.org, now, lt.c.owner, lt.ben, nemura.AnnualLeave, 2025, nil, leave.Entry{Kind: leave.Grant, Amount: leave.Whole(7), Source: "test"}, ""); err != nil {
			return err
		}
		_, err := nemura.CarryForwardLeave(ctx, tx, lt.c.org, now, lt.c.owner, lt.ben, 2025, 12)
		return err
	})
	// Leave is off: nothing is enqueued.
	if n, err := worker.RaiseLeaveAccrual(ctx, lt.c.org, now); err != nil || n != 0 {
		t.Fatalf("with Leave off: %d %v", n, err)
	}
	if err := store(t, "nemura_api").InOrg(ctx, lt.c.org, func(ctx context.Context, tx pgx.Tx) error {
		return nemura.SetFeature(ctx, tx, lt.c.org, now, lt.c.owner, "leave", "on", "", nil, nil)
	}); err != nil {
		t.Fatal(err)
	}
	if n, err := worker.RaiseLeaveAccrual(ctx, lt.c.org, now); err != nil || n != 1 {
		t.Fatalf("with Leave on: %d %v", n, err)
	}
	if n, err := worker.RaiseLeaveAccrual(ctx, lt.c.org, now.Add(24*time.Hour)); err != nil || n != 0 {
		t.Errorf("the same month enqueued twice: %d %v", n, err)
	}
	var payload []byte
	if err := connect(t, db.SuperURL()).QueryRow(ctx, "SELECT payload FROM nemura.outbox WHERE organisation_id = $1 AND kind = $2", lt.c.org, nemura.KindLeaveAccrual).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	work := worker.LeaveAccrualWork(clock.NewFrozen(now))
	row := outbox.Row{ID: uuid.New(), Kind: nemura.KindLeaveAccrual, Payload: payload}
	if err := work(ctx, lt.c.org, row); err != nil {
		t.Fatal(err)
	}
	if err := work(ctx, lt.c.org, row); err != nil {
		t.Errorf("a second delivery: %v", err)
	}
	var rows []nemura.AccrualRow
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		rows, err = nemura.LeaveAccruals(ctx, tx, d("2026-03-01"))
		return err
	})
	if len(rows) != 1 || rows[0].Carried != leave.Whole(7) || rows[0].State != "draft" {
		t.Fatalf("the worker's draft: %+v", rows)
	}
	var system bool
	var by *uuid.UUID
	if err := connect(t, db.SuperURL()).QueryRow(ctx, "SELECT system_drafted, drafted_by FROM nemura.leave_accrual WHERE organisation_id = $1", lt.c.org).Scan(&system, &by); err != nil || !system || by != nil {
		t.Errorf("who drafted it: %v %v (%v)", system, by, err)
	}
	// A person approves it; the worker cannot, and cannot touch the ledger or a request.
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		n, err := nemura.ApproveLeaveAccrual(ctx, tx, lt.c.org, now.Add(time.Hour), lt.c.owner, d("2026-03-01"))
		if err != nil || n != 1 {
			t.Errorf("approving the system draft: %d %v", n, err)
		}
		return nil
	})
	for _, q := range []string{"UPDATE nemura.leave_accrual SET state = 'approved'", "INSERT INTO nemura.leave_ledger (person_id, type_key, leave_year, kind, amount, source, created_by) SELECT person_id, 'annual', 2026, 'grant', 1, 'x', created_by FROM nemura.leave_ledger LIMIT 1", "UPDATE nemura.leave_request SET state = 'cancelled'"} {
		err := worker.InOrg(ctx, lt.c.org, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, q)
			return err
		})
		if err == nil {
			t.Errorf("the worker ran %s", q)
		}
	}
}
