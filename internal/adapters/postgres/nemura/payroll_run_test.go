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
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/payroll"
	"github.com/cognizhi/fyniq/internal/nemura/people"
)

// A pay run is calculated from the pay records and the inputs into a read-only version with a digest; a missing fact refuses it and names the person; an additional wage needs a year to date;
// unpaid leave is the day's share of the working days; a change, an override or a correction is the next version, which supersedes the one before; an override is flagged and keeps the
// calculated figure; a version keeps its figures and cannot be approved by its preparer (a constraint); no audit event holds an amount (FR-PAY-01, FR-PAY-06, FR-PAY-14, NM-PAY-02, NM-PAY-09).
// req: FR-PAY-01, FR-PAY-06, FR-PAY-14, NM-PAY-02, NM-PAY-09, NM-PAY-12
// asvs: V8.2.1, V8.3.1
func TestAPayRunIsCalculatedFromThePayRecordsIntoAReadOnlyVersionWithADigest(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := d("2026-11-20").Add(10 * time.Hour)
	st := store(t, "nemura_api")
	packsSvc, err := nemura.NewPacksService(st, clock.NewFrozen(now))
	if err != nil {
		t.Fatal(err)
	}
	svc := nemura.PayrollService{Store: st, Clock: clock.NewFrozen(now), Packs: packsSvc}
	pay := api.Session{Organisation: lt.c.org, Member: lt.c.pay}
	su := connect(t, db.SuperURL())
	june := d("2026-06-01")

	gid, err := svc.CreateGroup(ctx, pay, api.PayGroupInput{Name: "Monthly", PayDay: 25, CutoffDays: 7, ApprovalDays: 3})
	if err != nil {
		t.Fatal(err)
	}
	set := func(person uuid.UUID, basic string, items []api.PayItem) {
		if err := svc.SetPay(ctx, pay, person, d("2026-01-05"), api.PersonPayInput{GroupID: gid, BasicMonthly: basic, Items: items}); err != nil {
			t.Fatal(err)
		}
	}
	set(lt.ben, "5000.00", nil)
	set(lt.cy, "4500.00", []api.PayItem{{Key: "transport", Name: "Transport allowance", Kind: "allowance", Nature: "allowance", Amount: "100.00", CPFWages: true}})

	// The legal classifications have no date of birth: the run is refused and names the person and the field.
	if _, err := svc.StartRun(ctx, pay, june, mustUUID(gid)); payCode(err) != payroll.CodeInvalid || !strings.Contains(err.Error(), "date of birth") {
		t.Fatalf("a run with no date of birth: %v", err)
	}
	birth := d("1990-05-20")
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		for _, p := range []uuid.UUID{lt.ben, lt.cy} {
			if err := nemura.SetClassification(ctx, tx, lt.c.org, now, lt.c.owner, p, d("2026-01-06"), people.Classification{ContractType: "service", EACovered: true, PartIVClass: "none", Residency: "citizen", BirthDate: &birth}); err != nil {
				return err
			}
		}
		return nil
	})
	// A run is refused for a group that is not the company's, and for a month that is not the first.
	if _, err := svc.StartRun(ctx, pay, june, uuid.New()); payCode(err) != payroll.CodeInvalid {
		t.Errorf("a group that is not the company's: %v", err)
	}
	if _, err := svc.StartRun(ctx, pay, d("2026-06-15"), mustUUID(gid)); payCode(err) != payroll.CodeInvalid {
		t.Errorf("a month that is not the first: %v", err)
	}

	run, err := svc.StartRun(ctx, pay, june, mustUUID(gid))
	if err != nil {
		t.Fatal(err)
	}
	if run.Latest.Number != 1 || run.Latest.State != "calculated" || len(run.Latest.Digest) != 64 || run.Latest.People != 2 || run.PayDay != "2026-06-25" {
		t.Fatalf("version 1: %+v", run)
	}
	tot := run.Latest.Totals
	// Ben 5,000: CPF 1,850 (1,000 + 850), SDL 11.25. Cy 4,600: CPF 1,702 (920 + 782), SDL 11.25. SDL payable is 22.50 rounded down to 22.
	if tot.Gross != "9600.00" || tot.CPFEmployee != "1920.00" || tot.CPFEmployer != "1632.00" || tot.SDLPayable != "22.00" || tot.Net != "7680.00" || tot.Disburse != "7680.00" || tot.EmployerCost != "11254.00" {
		t.Errorf("totals: %+v", tot)
	}
	if _, err := svc.StartRun(ctx, pay, june, mustUUID(gid)); payCode(err) != payroll.CodeConflict {
		t.Errorf("a second run of the month: %v", err)
	}
	detail, err := svc.RunDetail(ctx, pay, mustUUID(run.ID))
	if err != nil || len(detail.Lines) != 2 || len(detail.Versions) != 1 {
		t.Fatalf("detail: %+v %v", detail, err)
	}
	var cyLine api.RunLine
	for _, l := range detail.Lines {
		if l.Person == "Cy Wong" {
			cyLine = l
		}
	}
	if cyLine.Gross != "4600.00" || cyLine.CPFTotal != "1702.00" || cyLine.CPFEmployee != "920.00" || len(cyLine.Items) != 2 || cyLine.Items[1].SourceType != "person_pay_item" || len(cyLine.Working) == 0 {
		t.Errorf("Cy's month: %+v", cyLine)
	}

	// Unpaid leave: a day of the 22 working days of June is 4,500 ÷ 22 = 204.55 (RND-PAY-RATIO), from its source row.
	unpaid := uuid.Must(uuid.NewV7())
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO nemura.pay_input (id, person_id, kind, days, from_date, to_date, source_type, source_id, created_by, created_at) VALUES ($1, $2, 'unpaid_leave', 1, '2026-06-10', '2026-06-10', 'leave_ledger', $3, $4, $5)`,
			unpaid, lt.cy, uuid.New(), lt.c.pay, now); err != nil {
			return err
		}
		return nemura.Audit(ctx, tx, lt.c.org, now, audit.Actor{Type: "user", ID: lt.c.pay.String()}, "nemura.pay_input.created", audit.Object{Type: "pay_input", ID: unpaid}, nil, "")
	})
	r2, err := svc.Recalculate(ctx, pay, mustUUID(run.ID), "")
	if err != nil || r2.Latest.Number != 2 || r2.Latest.Totals.Gross != "9395.45" {
		t.Fatalf("version 2: %+v %v", r2.Latest, err)
	}
	if d2, _ := svc.RunDetail(ctx, pay, mustUUID(run.ID)); len(d2.Versions) != 2 || d2.Versions[1].State != "superseded" || d2.Versions[0].State != "calculated" {
		t.Errorf("the first version was not superseded: %+v", d2.Versions)
	}

	// An additional wage needs the year to date: refused, naming Ben; imported, the bonus is subject to CPF: 25,000 at 37% is 9,250, the employee 5,000.
	bonus := api.OneOffInput{Key: "bonus-2026", Name: "Annual bonus", Nature: "bonus", Amount: "20000.00", CPFWages: true, ForMonth: "2025-12-01", PayableOn: "2026-06-30"}
	if _, err := svc.AddOneOff(ctx, pay, lt.ben, june, bonus); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Recalculate(ctx, pay, mustUUID(run.ID), ""); payCode(err) != payroll.CodeInvalid || !strings.Contains(err.Error(), "Ben Lim") || !strings.Contains(err.Error(), "year to date") {
		t.Fatalf("a bonus with no year to date: %v", err)
	}
	if err := svc.ImportYTD(ctx, pay, lt.ben, api.YTDInput{Year: 2026, OW: "25000.00", AWSubject: "0.00", AWPaid: "0.00", Through: "2026-05-01"}); err != nil {
		t.Fatal(err)
	}
	r3, err := svc.Recalculate(ctx, pay, mustUUID(run.ID), "")
	if err != nil || r3.Latest.Number != 3 || r3.Latest.Totals.Gross != "29395.45" {
		t.Fatalf("version 3: %+v %v", r3.Latest, err)
	}
	d3, _ := svc.RunDetail(ctx, pay, mustUUID(run.ID))
	var benLine api.RunLine
	for _, l := range d3.Lines {
		if l.Person == "Ben Lim" {
			benLine = l
		}
	}
	if benLine.AWSubject != "20000.00" || benLine.CPFTotal != "9250.00" || benLine.CPFEmployee != "5000.00" || benLine.Items[1].Class != "additional" {
		t.Errorf("Ben's month with the bonus: %+v", benLine)
	}

	// An override replaces one figure with a reason, is flagged and keeps the calculated figure; a short reason is refused; a withdrawn one leaves the next version.
	if _, err := svc.AddOverride(ctx, pay, mustUUID(run.ID), lt.ben, "cpf_employee", "999.00", "short"); payCode(err) != payroll.CodeInvalid {
		t.Errorf("a short reason: %v", err)
	}
	if _, err := svc.AddOverride(ctx, pay, mustUUID(run.ID), lt.ben, "gross", "999.00", "The CPF Board's own figure."); payCode(err) != payroll.CodeInvalid {
		t.Errorf("a figure the engine owns: %v", err)
	}
	oid, err := svc.AddOverride(ctx, pay, mustUUID(run.ID), lt.ben, "cpf_employee", "999.00", "The CPF Board's own figure for the month.")
	if err != nil {
		t.Fatal(err)
	}
	r4, err := svc.Recalculate(ctx, pay, mustUUID(run.ID), "")
	if err != nil || r4.Latest.Number != 4 || r4.Latest.Digest == r3.Latest.Digest {
		t.Fatalf("version 4: %+v %v", r4.Latest, err)
	}
	d4, _ := svc.RunDetail(ctx, pay, mustUUID(run.ID))
	for _, l := range d4.Lines {
		if l.Person == "Ben Lim" {
			flagged := false
			for _, f := range l.Flags {
				flagged = flagged || f.Code == "OVERRIDE"
			}
			if l.CPFEmployee != "999.00" || !flagged || l.Computed["cpf_employee"] != "5000" || len(l.Overridden) != 1 {
				t.Errorf("the overridden month: %+v", l)
			}
		}
	}
	if len(d4.Overrides) != 1 || d4.Overrides[0].Reason == "" {
		t.Errorf("overrides: %+v", d4.Overrides)
	}
	if err := svc.WithdrawOverride(ctx, pay, mustUUID(oid)); err != nil {
		t.Fatal(err)
	}
	if err := svc.WithdrawOverride(ctx, pay, mustUUID(oid)); payCode(err) != payroll.CodeNotFound {
		t.Errorf("withdrawing twice: %v", err)
	}
	r5, err := svc.Recalculate(ctx, pay, mustUUID(run.ID), "")
	if err != nil || r5.Latest.Digest != r3.Latest.Digest {
		t.Errorf("without the override the figures and so the digest are version 3's: %+v %v", r5.Latest, err)
	}

	// A version keeps its figures, its digest and its lines; a preparer cannot be the approver; nothing leaves the trail with an amount.
	for _, sql := range []string{"UPDATE nemura.pay_run_version SET gross = 1", "DELETE FROM nemura.pay_run_version", "UPDATE nemura.pay_run_version SET state = 'paid' WHERE state = 'calculated'",
		"UPDATE nemura.pay_run_line SET gross = 1", "DELETE FROM nemura.pay_run_line", "UPDATE nemura.pay_run_override SET value = 1"} {
		if _, err := su.Exec(ctx, sql); err == nil || !(strings.Contains(err.Error(), "NM173") || strings.Contains(err.Error(), "NM174") || strings.Contains(err.Error(), "NM127")) {
			t.Errorf("%s: %v", sql, err)
		}
	}
	if _, err := su.Exec(ctx, "UPDATE nemura.pay_run_version SET state = 'approved', approved_by = prepared_by, approved_at = now(), approved_digest = digest, envelope = '{}'::jsonb WHERE id = $1", mustUUID(r5.Latest.ID)); err == nil || !strings.Contains(err.Error(), "segregation") {
		t.Errorf("a preparer approving their own run: %v", err)
	}
	var leaks int
	if err := su.QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE organisation_id = $1 AND body ~ '(^|[^0-9a-fA-F])(9250|20000|999\.00|29395|9600|1702|204\.55)([^0-9a-fA-F]|$)'`, lt.c.org).Scan(&leaks); err != nil || leaks != 0 {
		t.Errorf("the audit trail holds an amount in %d events: %v", leaks, err)
	}
	if list, _ := svc.Runs(ctx, pay, june); len(list) != 1 || list[0].Latest.Number != 5 {
		t.Errorf("runs of the month: %+v", list)
	}
}

// A version is approved by someone other than its preparer, over the digest they read, once, when it is the newest and has no ✖; the approval keeps what the passkey confirmed; older approved
// versions are superseded by a corrected one; the inputs it used are marked used by the run so no later run takes them; the next month is set against this one with every difference
// explained (FR-PAY-07, NM-ROLE-04, NM-PAY-09, NM-PAY-11).
// req: FR-PAY-07, NM-ROLE-04, NM-PAY-09, NM-PAY-11
// asvs: V8.2.1, V8.3.1
func TestAVersionIsApprovedByAnotherPersonOverItsDigestAndItsInputsAreUsedOnce(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := d("2026-11-20").Add(10 * time.Hour)
	st := store(t, "nemura_api")
	packsSvc, err := nemura.NewPacksService(st, clock.NewFrozen(now))
	if err != nil {
		t.Fatal(err)
	}
	svc := nemura.PayrollService{Store: st, Clock: clock.NewFrozen(now), Packs: packsSvc}
	prep := api.Session{Organisation: lt.c.org, Member: lt.c.pay}
	approver := api.Session{Organisation: lt.c.org, Member: lt.c.owner, StepUpAt: &now}
	su := connect(t, db.SuperURL())
	birth := d("1990-05-20")
	gid, _ := svc.CreateGroup(ctx, prep, api.PayGroupInput{Name: "Monthly", PayDay: 25, CutoffDays: 7, ApprovalDays: 3})
	for _, p := range []uuid.UUID{lt.ben, lt.cy} {
		if err := svc.SetPay(ctx, prep, p, d("2026-01-05"), api.PersonPayInput{GroupID: gid, BasicMonthly: "4000.00"}); err != nil {
			t.Fatal(err)
		}
	}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		for _, p := range []uuid.UUID{lt.ben, lt.cy} {
			if err := nemura.SetClassification(ctx, tx, lt.c.org, now, lt.c.owner, p, d("2026-01-06"), people.Classification{ContractType: "service", EACovered: true, PartIVClass: "none", Residency: "citizen", BirthDate: &birth}); err != nil {
				return err
			}
		}
		return nil
	})
	if _, err := svc.AddOneOff(ctx, prep, lt.ben, d("2026-06-01"), api.OneOffInput{Key: "gift", Name: "Allowance", Nature: "allowance", Amount: "300.00", CPFWages: true}); err != nil {
		t.Fatal(err)
	}
	may, err := svc.StartRun(ctx, prep, d("2026-05-01"), mustUUID(gid))
	if err != nil {
		t.Fatal(err)
	}
	digestOf := func(h string) string { return h }
	// Nothing is approved by its preparer, on a stale digest, on a version that is not the newest, or a version that does not exist.
	if err := svc.Approve(ctx, prep, mustUUID(may.ID), 1, may.Latest.Digest); payCode(err) != payroll.CodeRole {
		t.Errorf("the preparer approving: %v", err)
	}
	if err := svc.Approve(ctx, approver, mustUUID(may.ID), 1, strings.Repeat("0", 64)); payCode(err) != payroll.CodeConflict {
		t.Errorf("a digest that is not the version's: %v", err)
	}
	if err := svc.Approve(ctx, approver, mustUUID(may.ID), 1, "not hex"); payCode(err) != payroll.CodeInvalid {
		t.Errorf("a malformed digest: %v", err)
	}
	if err := svc.Approve(ctx, approver, mustUUID(may.ID), 9, may.Latest.Digest); payCode(err) != payroll.CodeNotFound {
		t.Errorf("a version that does not exist: %v", err)
	}
	if err := svc.Approve(ctx, approver, mustUUID(may.ID), 1, digestOf(may.Latest.Digest)); err != nil {
		t.Fatal(err)
	}
	if err := svc.Approve(ctx, approver, mustUUID(may.ID), 1, may.Latest.Digest); payCode(err) != payroll.CodeConflict {
		t.Errorf("approving twice: %v", err)
	}
	var by uuid.UUID
	var env string
	if err := su.QueryRow(ctx, "SELECT approved_by, envelope::text FROM nemura.pay_run_version WHERE id = $1", mustUUID(may.Latest.ID)).Scan(&by, &env); err != nil || by != lt.c.owner || !strings.Contains(env, may.Latest.Digest) || !strings.Contains(env, "step_up_at") {
		t.Errorf("the approval: %v %s %v", by, env, err)
	}
	// June: the one-off item is eligible while the run is calculated, is used by the run when approved, and a later run cannot take it again.
	june, err := svc.StartRun(ctx, prep, d("2026-06-01"), mustUUID(gid))
	if err != nil {
		t.Fatal(err)
	}
	if june.Latest.Totals.Gross != "8300.00" {
		t.Fatalf("June gross: %+v", june.Latest.Totals)
	}
	r2, err := svc.Recalculate(ctx, prep, mustUUID(june.ID), "")
	if err != nil || r2.Latest.Totals.Gross != "8300.00" {
		t.Fatalf("recalculating still includes the item: %+v %v", r2.Latest.Totals, err)
	}
	if err := svc.Approve(ctx, approver, mustUUID(june.ID), 1, june.Latest.Digest); payCode(err) != payroll.CodeConflict {
		t.Errorf("approving a superseded version: %v", err)
	}
	if err := svc.Approve(ctx, approver, mustUUID(june.ID), 2, r2.Latest.Digest); err != nil {
		t.Fatal(err)
	}
	var used *uuid.UUID
	_ = su.QueryRow(ctx, "SELECT used_in_run FROM nemura.pay_one_off WHERE key = 'gift'").Scan(&used)
	if used == nil || *used != mustUUID(june.ID) {
		t.Errorf("the one-off item is not marked used by the run: %v", used)
	}
	// A correction of an approved month needs a reason, keeps the used item, supersedes the approved version once approved by another person.
	if _, err := svc.Recalculate(ctx, prep, mustUUID(june.ID), ""); payCode(err) != payroll.CodeInvalid || !strings.Contains(err.Error(), "reason") {
		t.Errorf("a correction without a reason: %v", err)
	}
	c3, err := svc.Recalculate(ctx, prep, mustUUID(june.ID), "A bonus was missed.")
	if err != nil || c3.Latest.Number != 3 || c3.Latest.Totals.Gross != "8300.00" || c3.Latest.Reason != "A bonus was missed." {
		t.Fatalf("the correction: %+v %v", c3.Latest, err)
	}
	if d3, _ := svc.RunDetail(ctx, prep, mustUUID(june.ID)); d3.Versions[1].State != "approved" || d3.Versions[0].State != "calculated" {
		t.Errorf("the approved version stays approved until the correction is: %+v", d3.Versions)
	}
	if err := svc.Approve(ctx, approver, mustUUID(june.ID), 3, c3.Latest.Digest); err != nil {
		t.Fatal(err)
	}
	if d3, _ := svc.RunDetail(ctx, prep, mustUUID(june.ID)); d3.Versions[1].State != "superseded" || d3.Versions[0].State != "approved" {
		t.Errorf("the corrected version supersedes the one before: %+v", d3.Versions)
	}
	// What changed since May: Ben's gift is a one-off item linked to its record; nobody else changed.
	ch, err := svc.Changes(ctx, prep, mustUUID(june.ID))
	if err != nil || !ch.HasPrevious || ch.GrossPrevious != "8000.00" || ch.GrossNow != "8300.00" || len(ch.Unexplained) != 0 {
		t.Fatalf("changes: %+v %v", ch, err)
	}
	if len(ch.Changes) != 1 || ch.Changes[0].Person != "Ben Lim" || ch.Changes[0].Cause != "one_off" || ch.Changes[0].SourceType != "pay_one_off" || ch.Changes[0].Amount != "300.00" {
		t.Errorf("the explained change: %+v", ch.Changes)
	}
	if none, _ := svc.Changes(ctx, prep, mustUUID(may.ID)); none.HasPrevious {
		t.Errorf("May has a month before it: %+v", none)
	}
	// No amount in the trail; the approved lines are evidence.
	var leaks int
	if err := su.QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE organisation_id = $1 AND body ~ '(^|[^0-9a-fA-F])(8300|8000\.00|300\.00)([^0-9a-fA-F]|$)'`, lt.c.org).Scan(&leaks); err != nil || leaks != 0 {
		t.Errorf("amounts in the audit trail: %d %v", leaks, err)
	}
}
