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
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/leave"
	"github.com/cognizhi/fyniq/internal/nemura/people"
)

func (lt *leaveTeam) partIV(class string) {
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.SetClassification(ctx, tx, lt.c.org, today.Add(9*time.Hour), lt.c.owner, lt.ben, d("2026-06-01"), people.Classification{ContractType: "service", EACovered: true, PartIVClass: class, Residency: "citizen"})
	})
}

// usedByARun is what a pay run does to an input: mark it used, with its own audit event in the same transaction. It runs as the owner of the schema until the pay run
// has a role of its own.
func (lt *leaveTeam) usedByARun(objType string, obj uuid.UUID, query string, args ...any) error {
	ctx := context.Background()
	tx, err := connect(lt.c.t, db.SuperURL()).Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, "SELECT set_config('app.org_id', $1, true)", lt.c.org.String()); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, query, args...); err != nil {
		return err
	}
	if err := nemura.Audit(ctx, tx, lt.c.org, today, actor, "nemura.pay_input.used", audit.Object{Type: objType, ID: obj}, nil, ""); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (lt *leaveTeam) overtime(a nemura.OvertimeAsk) (uuid.UUID, bool, error) {
	var id uuid.UUID
	var over bool
	err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		id, over, err = nemura.RecordOvertime(ctx, tx, lt.c.org, today.Add(10*time.Hour), lt.benLogin, peoplePack(lt.c.t), a)
		return err
	})
	return id, over, err
}

// Overtime, rest-day work and public-holiday work are recorded per day for a Part IV employee only, approved by someone other than the person, used once by a pay run,
// and never deleted; work beyond the monthly limit from the pack stays recorded and is flagged, never refused (NM-TIM-02, NM-TIM-04).
// req: NM-TIM-02, NM-TIM-04
// asvs: V8.2.1
func TestOvertimeIsRecordedPerDayForPartIVAndUsedOnce(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	ot := func(date string, hours leave.Days) nemura.OvertimeAsk {
		return nemura.OvertimeAsk{Person: lt.ben, Date: d(date), Kind: "overtime", Hours: hours}
	}
	// Ben's class is none: not a Part IV employee.
	if _, _, err := lt.overtime(ot("2026-09-01", leave.Whole(2))); leaveCode(err) != leave.CodeInvalid {
		t.Fatalf("a person outside Part IV: %v", err)
	}
	lt.partIV("non_workman")
	id, over, err := lt.overtime(ot("2026-09-01", 250))
	if err != nil || over {
		t.Fatalf("overtime: %v %v", over, err)
	}
	for _, bad := range []nemura.OvertimeAsk{
		{Person: lt.ben, Date: d("2026-09-02"), Kind: "overtime", Hours: 0}, {Person: lt.ben, Date: d("2026-09-02"), Kind: "overtime", Hours: leave.Whole(25)},
		{Person: lt.ben, Date: d("2026-09-02"), Kind: "overtime", Hours: 130}, {Person: lt.ben, Date: d("2026-09-02"), Kind: "night", Hours: 100},
		{Person: lt.ben, Date: d("2026-09-05"), Kind: "rest_day", Hours: 400}, {Person: lt.ben, Date: d("2026-09-05"), Kind: "rest_day", Hours: 400, Branch: "boss"},
		{Person: lt.ben, Date: d("2026-09-02"), Kind: "overtime", Hours: 100, Branch: "employer_request"}, {Person: lt.ben, Date: d("2025-01-02"), Kind: "overtime", Hours: 100},
	} {
		if _, _, err := lt.overtime(bad); leaveCode(err) != leave.CodeInvalid {
			t.Errorf("%+v: %v", bad, err)
		}
	}
	if _, _, err := lt.overtime(ot("2026-09-01", 100)); leaveCode(err) != leave.CodeConflict {
		t.Errorf("two records of one kind on one day: %v", err)
	}
	if _, _, err := lt.overtime(nemura.OvertimeAsk{Person: lt.ben, Date: d("2026-09-05"), Kind: "rest_day", Hours: 400, Branch: "employer_request"}); err != nil {
		t.Errorf("rest-day work: %v", err)
	}
	if _, _, err := lt.overtime(nemura.OvertimeAsk{Person: lt.ben, Date: d("2026-09-09"), Kind: "public_holiday", Hours: 800}); err != nil {
		t.Errorf("public-holiday work: %v", err)
	}
	// The monthly limit: 70 hours in October, then 3 more passes 72: recorded and flagged. Rest-day hours do not count towards it.
	for i := 1; i <= 7; i++ {
		date := time.Date(2026, 10, i, 0, 0, 0, 0, time.UTC).Format(time.DateOnly)
		if _, over, err := lt.overtime(ot(date, leave.Whole(10))); err != nil || over {
			t.Fatalf("hours %d: %v %v", i*10, over, err)
		}
	}
	if _, over, err := lt.overtime(ot("2026-10-08", leave.Whole(3))); err != nil || !over {
		t.Errorf("73 hours: %v %v", over, err)
	}
	if _, over, err := lt.overtime(ot("2026-11-02", leave.Whole(3))); err != nil || over {
		t.Errorf("the limit is for the month, not for ever: %v %v", over, err)
	}
	// Decisions: not by the person, a refusal needs a reason, once.
	decide := func(by uuid.UUID, approve bool, reason string) error {
		return lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
			return nemura.DecideOvertime(ctx, tx, lt.c.org, today.Add(11*time.Hour), by, id, approve, reason)
		})
	}
	if err := decide(lt.benLogin, true, ""); leaveCode(err) != leave.CodeRole {
		t.Errorf("deciding one's own: %v", err)
	}
	if err := decide(lt.c.owner, false, ""); leaveCode(err) != leave.CodeInvalid {
		t.Errorf("a refusal with no reason: %v", err)
	}
	if err := decide(lt.c.owner, true, ""); err != nil {
		t.Fatal(err)
	}
	if err := decide(lt.c.owner, false, "changed my mind"); leaveCode(err) != leave.CodeConflict {
		t.Errorf("deciding twice: %v", err)
	}
	// Open pay inputs: approved hours only; a pending record is not an input.
	open := func() []nemura.PayInput {
		var in []nemura.PayInput
		lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			in, err = nemura.OpenPayInputs(ctx, tx, d("2026-12-31"))
			return err
		})
		return in
	}
	if in := open(); len(in) != 1 || in[0].Kind != "overtime" || in[0].Quantity != 250 || in[0].Unit != "hours" || in[0].ID != id || in[0].SourceType != "overtime_entry" {
		t.Fatalf("open inputs: %+v", in)
	}
	// Scopes: the person reads their own, the manager their report's, a stranger none.
	list := func(s nemura.LeaveScope) int {
		var n int
		lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
			rows, err := nemura.Overtime(ctx, tx, s, "")
			n = len(rows)
			return err
		})
		return n
	}
	if list(lt.scope(lt.benLogin, false, false)) != 12 || list(lt.scope(lt.c.owner, false, true)) != 12 || list(lt.scope(lt.c.pay, false, true)) != 0 || list(lt.scope(lt.c.hr, true, false)) != 12 {
		t.Errorf("scopes: %d %d %d", list(lt.scope(lt.benLogin, false, false)), list(lt.scope(lt.c.owner, false, true)), list(lt.scope(lt.c.pay, false, true)))
	}
	// Used once by a pay run; evidence; never deleted.
	su := connect(t, db.SuperURL())
	run := uuid.New()
	if err := lt.usedByARun("overtime_entry", id, "UPDATE nemura.overtime_entry SET used_in_run = $1, used_at = now() WHERE id = $2", run, id); err != nil {
		t.Fatal(err)
	}
	if err := lt.usedByARun("overtime_entry", id, "UPDATE nemura.overtime_entry SET used_in_run = $1, used_at = now() WHERE id = $2", uuid.New(), id); err == nil {
		t.Error("an input was used twice")
	}
	if in := open(); len(in) != 0 {
		t.Errorf("a used record is still open: %+v", in)
	}
	for _, q := range []string{"DELETE FROM nemura.overtime_entry", "UPDATE nemura.overtime_entry SET hours = 1", "UPDATE nemura.overtime_entry SET state = 'pending' WHERE state = 'approved'"} {
		if _, err := su.Exec(ctx, q); err == nil {
			t.Errorf("%s was allowed", q)
		}
	}
}

// Unpaid leave and the encashment of annual leave become pay-run inputs linked to their source, once: the days of a sick notification beyond the ceiling are decided once as
// unpaid or taken from annual leave, an encashment takes days from the ledger (never beyond the balance) and nothing is ever edited or deleted (NM-LV-08, NM-PAY-02).
// req: NM-LV-08
// asvs: V8.2.1
func TestUnpaidLeaveAndEncashmentBecomePayInputsOnce(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	// Ben has 14 days of annual leave for 2026. A sick day before three months' service is unpaid.
	r, err := lt.notify(lt.sick(leave.Outpatient, "2026-03-02", "2026-03-03"), "2026-03-02")
	if err != nil || r.Unpaid != leave.Whole(2) {
		t.Fatalf("unpaid sick days: %+v %v", r, err)
	}
	decide := func(treatment string) (leave.Days, error) {
		var days leave.Days
		err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			days, err = nemura.DecideUnpaidSick(ctx, tx, lt.c.org, today.Add(11*time.Hour), lt.c.owner, r.ID, treatment)
			return err
		})
		return days, err
	}
	if _, err := decide("sometimes"); leaveCode(err) != leave.CodeInvalid {
		t.Errorf("a bad treatment: %v", err)
	}
	if days, err := decide("unpaid"); err != nil || days != leave.Whole(2) {
		t.Fatalf("unpaid: %s %v", days, err)
	}
	if _, err := decide("annual"); leaveCode(err) != leave.CodeConflict {
		t.Errorf("deciding twice: %v", err)
	}
	// A notification with no unpaid days, and one that does not exist.
	paid, err := lt.notify(lt.sick(leave.Outpatient, "2026-05-04", "2026-05-04"), "2026-05-04")
	if err != nil || paid.Unpaid != 0 {
		t.Fatal(err)
	}
	if err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.DecideUnpaidSick(ctx, tx, lt.c.org, today, lt.c.owner, paid.ID, "unpaid")
		return err
	}); leaveCode(err) != leave.CodeConflict {
		t.Errorf("nothing to decide: %v", err)
	}
	if err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.DecideUnpaidSick(ctx, tx, lt.c.org, today, lt.c.owner, uuid.New(), "unpaid")
		return err
	}); leaveCode(err) != leave.CodeNotFound {
		t.Errorf("no such notification: %v", err)
	}
	// Taken from annual leave instead.
	r2, err := lt.notify(lt.sick(leave.Outpatient, "2026-03-09", "2026-03-09"), "2026-03-09")
	if err != nil || r2.Unpaid != leave.Whole(1) {
		t.Fatalf("a second unpaid day: %+v %v", r2, err)
	}
	if err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.DecideUnpaidSick(ctx, tx, lt.c.org, today, lt.c.owner, r2.ID, "annual")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if b := lt.balance(); b.Taken != leave.Whole(1) || b.Left != leave.Whole(13) {
		t.Errorf("annual leave after the decision: %+v", b)
	}
	// Encashment: 2 days; never more than the balance.
	encash := func(days leave.Days) error {
		return lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
			_, err := nemura.EncashLeave(ctx, tx, lt.c.org, today.Add(12*time.Hour), lt.c.owner, lt.ben, 2026, days, d("2026-11-30"), "Agreed on leaving.")
			return err
		})
	}
	if err := encash(leave.Whole(2)); err != nil {
		t.Fatal(err)
	}
	if err := encash(leave.Whole(20)); leaveCode(err) != leave.CodeBalance {
		t.Errorf("more than the balance: %v", err)
	}
	if err := encash(0); leaveCode(err) != leave.CodeInvalid {
		t.Errorf("no days: %v", err)
	}
	if b := lt.balance(); b.Encashed != leave.Whole(2) || b.Left != leave.Whole(11) {
		t.Errorf("annual leave after encashment: %+v", b)
	}
	var in []nemura.PayInput
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		in, err = nemura.OpenPayInputs(ctx, tx, d("2026-12-31"))
		return err
	})
	if len(in) != 2 || in[0].Kind != "unpaid_leave" || in[0].Quantity != leave.Whole(2) || in[0].SourceType != "sick_notification" || in[0].SourceID != r.ID || in[1].Kind != "encashment" || in[1].Unit != "days" || in[1].SourceType != "leave_ledger" {
		t.Errorf("open inputs: %+v", in)
	}
	// A pay run uses an input once; nothing is edited or deleted.
	su := connect(t, db.SuperURL())
	if err := lt.usedByARun("pay_input", in[1].ID, "UPDATE nemura.pay_input SET used_in_run = $1, used_at = now() WHERE id = $2", uuid.New(), in[1].ID); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"UPDATE nemura.pay_input SET used_in_run = gen_random_uuid() WHERE kind = 'encashment'", "UPDATE nemura.pay_input SET days = 9", "DELETE FROM nemura.pay_input", "UPDATE nemura.sick_unpaid_decision SET treatment = 'annual'", "DELETE FROM nemura.sick_unpaid_decision"} {
		if _, err := su.Exec(ctx, q); err == nil {
			t.Errorf("%s was allowed", q)
		}
	}
}

// The time service records for the signed-in Part IV employee, lets a manager decide on a report's record only and never their own, and lists the open inputs.
// req: NM-TIM-04, NM-LV-08
func TestTheTimeServiceRecordsDecidesAndListsInputs(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	lt.partIV("workman")
	now := d("2026-11-20").Add(9 * time.Hour)
	packs, err := nemura.NewPacksService(store(t, "nemura_api"), clock.NewFrozen(now))
	if err != nil {
		t.Fatal(err)
	}
	svc := nemura.TimeService{Store: store(t, "nemura_api"), Clock: clock.NewFrozen(now), Packs: packs}
	ben := api.Session{Organisation: lt.c.org, Member: lt.benLogin}
	ana := api.Session{Organisation: lt.c.org, Member: lt.c.owner}
	id, over, err := svc.RecordOvertime(ctx, ben, api.OvertimeAsk{Date: "2026-11-02", Kind: "overtime", Hours: "3.50"})
	if err != nil || over {
		t.Fatalf("record: %s %v %v", id, over, err)
	}
	if _, _, err := svc.RecordOvertime(ctx, ben, api.OvertimeAsk{Date: "tomorrow", Kind: "overtime", Hours: "3"}); leaveCode(err) != leave.CodeInvalid {
		t.Errorf("a bad date: %v", err)
	}
	if _, _, err := svc.RecordOvertime(ctx, ben, api.OvertimeAsk{Date: "2026-11-03", Kind: "overtime", Hours: "lots"}); leaveCode(err) != leave.CodeInvalid {
		t.Errorf("bad hours: %v", err)
	}
	if _, _, err := svc.RecordOvertime(ctx, api.Session{Organisation: lt.c.org, Member: lt.c.pay}, api.OvertimeAsk{Date: "2026-11-03", Kind: "overtime", Hours: "1"}); personRefusal(err) != people.CodeNotFound {
		t.Errorf("a login with no person: %v", err)
	}
	rid := uuid.MustParse(id)
	if err := svc.DecideOvertime(ctx, ben, api.LeaveScope{Reports: true, Decide: true}, rid, true, ""); leaveCode(err) == "" {
		t.Error("a person decided their own overtime")
	}
	if err := svc.DecideOvertime(ctx, api.Session{Organisation: lt.c.org, Member: lt.c.pay}, api.LeaveScope{Reports: true, Decide: true}, rid, true, ""); leaveCode(err) != leave.CodeNotFound {
		t.Errorf("a stranger decided: %v", err)
	}
	if err := svc.DecideOvertime(ctx, ana, api.LeaveScope{Reports: true, Decide: true}, rid, true, ""); err != nil {
		t.Fatal(err)
	}
	mine, err := svc.Overtime(ctx, ben, api.LeaveScope{}, "approved")
	if err != nil || len(mine) != 1 || mine[0].Hours != "3.50" || mine[0].State != "approved" || mine[0].Used {
		t.Errorf("own list: %+v %v", mine, err)
	}
	inputs, err := svc.PayInputs(ctx, ana, d("2026-12-31"))
	if err != nil || len(inputs) != 1 || inputs[0].Kind != "overtime" || inputs[0].Quantity != "3.50" || inputs[0].Unit != "hours" {
		t.Errorf("inputs: %+v %v", inputs, err)
	}
	if err := svc.Encash(ctx, ana, lt.ben, 2026, "2.00", d("2026-11-30"), "On leaving."); err != nil {
		t.Fatal(err)
	}
	if err := svc.Encash(ctx, ana, lt.ben, 2026, "many", d("2026-11-30"), ""); leaveCode(err) != leave.CodeInvalid {
		t.Errorf("bad days: %v", err)
	}
	if inputs, _ := svc.PayInputs(ctx, ana, d("2026-12-31")); len(inputs) != 2 {
		t.Errorf("inputs after encashment: %d", len(inputs))
	}
}
