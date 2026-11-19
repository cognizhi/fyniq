//go:build local

package nemura_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/leave"
	"github.com/cognizhi/fyniq/internal/nemura/letters"
	"github.com/cognizhi/fyniq/internal/nemura/onboarding"
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
	if err := su.QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE organisation_id = $1 AND body ~ '(^|[^0-9a-fA-F-])(9250|20000|999\.00|29395|9600|1702|204\.55)([^0-9a-fA-F-]|$)'`, lt.c.org).Scan(&leaks); err != nil || leaks != 0 {
		var body string
		_ = su.QueryRow(ctx, `SELECT body FROM nemura.audit_event WHERE organisation_id = $1 AND body ~ '(^|[^0-9a-fA-F-])(9250|20000|999\.00|29395|9600|1702|204\.55)([^0-9a-fA-F-]|$)' LIMIT 1`, lt.c.org).Scan(&body)
		t.Errorf("the audit trail holds an amount in %d events: %v %s", leaks, err, body)
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
	if err := su.QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE organisation_id = $1 AND body ~ '(^|[^0-9a-fA-F-])(8300|8000\.00|300\.00)([^0-9a-fA-F-]|$)'`, lt.c.org).Scan(&leaks); err != nil || leaks != 0 {
		t.Errorf("amounts in the audit trail: %d %v", leaks, err)
	}
}

// A payslip is made once for each person of an approved version, with MOM's twelve items as data, the PDF drawn from them and its SHA-256; an unapproved or superseded version has none; a person reads
// their own and no one else's; a corrected version has payslips of its own and the one before says it is corrected; a changed byte is never served (NM-PAY-04).
// req: NM-PAY-04
// asvs: V8.2.1, V8.3.1, V13.2.1
func TestPayslipsAreMadeOnceFromAnApprovedVersionAndReadOnlyByTheirOwner(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := d("2026-11-20").Add(10 * time.Hour)
	st := store(t, "nemura_api")
	packsSvc, err := nemura.NewPacksService(st, clock.NewFrozen(now))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	blobs, err := local.NewDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	svc := nemura.PayrollService{Store: st, Clock: clock.NewFrozen(now), Packs: packsSvc, Blobs: blobs, Renderer: letters.Local{}}
	bare := nemura.PayrollService{Store: st, Clock: clock.NewFrozen(now), Packs: packsSvc}
	prep := api.Session{Organisation: lt.c.org, Member: lt.c.pay}
	approver := api.Session{Organisation: lt.c.org, Member: lt.c.owner, StepUpAt: &now}
	ben := api.Session{Organisation: lt.c.org, Member: lt.benLogin}
	stranger := api.Session{Organisation: lt.c.org, Member: lt.c.owner}
	birth := d("1990-05-20")
	gid, _ := svc.CreateGroup(ctx, prep, api.PayGroupInput{Name: "Monthly", PayDay: 25, CutoffDays: 7, ApprovalDays: 3})
	if err := svc.SetPay(ctx, prep, lt.ben, d("2026-01-05"), api.PersonPayInput{GroupID: gid, BasicMonthly: "4500.00", SHGFund: "CDAC", Items: []api.PayItem{
		{Key: "transport", Name: "Transport allowance", Kind: "allowance", Nature: "allowance", Amount: "100.00", CPFWages: true}, {Key: "dorm", Name: "Dormitory", Kind: "fixed_deduction", Nature: "accommodation", Amount: "200.00"}}}); err != nil {
		t.Fatal(err)
	}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.SetClassification(ctx, tx, lt.c.org, now, lt.c.owner, lt.ben, d("2026-01-06"), people.Classification{ContractType: "service", EACovered: true, PartIVClass: "non_workman", Residency: "citizen", BirthDate: &birth})
	})
	var otID uuid.UUID
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		otID, _, err = nemura.RecordOvertime(ctx, tx, lt.c.org, now, lt.benLogin, peoplePack(lt.c.t), nemura.OvertimeAsk{Person: lt.ben, Date: d("2026-06-10"), Kind: "overtime", Hours: leave.Whole(3)})
		if err != nil {
			return err
		}
		return nemura.DecideOvertime(ctx, tx, lt.c.org, now, lt.c.owner, otID, true, "")
	})
	if _, err := svc.AddOneOff(ctx, prep, lt.ben, d("2026-06-01"), api.OneOffInput{Key: "bonus", Name: "Monthly bonus", Nature: "bonus", Amount: "500.00", CPFWages: true, ForMonth: "2026-06-01", PayableOn: "2026-06-25"}); err != nil {
		t.Fatal(err)
	}
	run, err := svc.StartRun(ctx, prep, d("2026-06-01"), mustUUID(gid))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PublishPayslips(ctx, prep, mustUUID(run.ID), 1); payCode(err) != payroll.CodeConflict {
		t.Errorf("payslips of a version nobody approved: %v", err)
	}
	if err := svc.Approve(ctx, approver, mustUUID(run.ID), 1, run.Latest.Digest); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PublishPayslips(ctx, prep, mustUUID(run.ID), 1); payCode(err) != payroll.CodeInvalid || !strings.Contains(err.Error(), "letterhead") {
		t.Errorf("no employer name yet: %v", err)
	}
	if err := (nemura.LettersService{Store: st, Clock: clock.NewFrozen(now)}).SetSettings(ctx, api.Session{Organisation: lt.c.org, Member: lt.c.hr}, api.LetterSettings{Name: "Acme Pte. Ltd.", UEN: "201912345A", Address: "1 Raffles Place"}); err != nil {
		t.Fatal(err)
	}
	if _, err := bare.PublishPayslips(ctx, prep, mustUUID(run.ID), 1); err == nil || !strings.Contains(err.Error(), "NM_UNAVAILABLE") {
		t.Errorf("no renderer or file store: %v", err)
	}
	// The payment is confirmed paid on 26 June, a day after the pay day: the payslip's date of payment is the confirmed one.
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.RecordPaymentState(ctx, tx, lt.c.org, now, audit.Actor{Type: "service", ID: "link"}, nemura.PaymentInput{Run: mustUUID(run.ID), Person: lt.ben, State: "paid", Instruction: "I-1", Amount: "3963.21", PaidOn: d("2026-06-26"), Source: "fyniq"})
	})
	n, err := svc.PublishPayslips(ctx, prep, mustUUID(run.ID), 1)
	if err != nil || n != 1 {
		t.Fatalf("publish: %d %v", n, err)
	}
	if _, err := svc.PublishPayslips(ctx, prep, mustUUID(run.ID), 1); payCode(err) != payroll.CodeConflict {
		t.Errorf("publishing twice: %v", err)
	}
	// (Overtime is paid to Ben as a Part IV non-workman: see the payslip's overtime items.)
	// Gross 4,500 + 100 + 500 + overtime 106.21 (3 hours at 12 × 4,500 / (52 × 44) = 23.60 an hour, 1.5 times: RND-PAY-RATIO) = 5,206.21; the employee's CPF is 20% of it, 1,041.24 taken as 1,041; the CDAC
	// fund 2.00; the dormitory 200: net 5,206.21 − 1,041 − 2.00 − 200 = 3,963.21.
	list, err := svc.MyPayslips(ctx, ben)
	if err == nil && (len(list) != 1 || list[0].Reference != "PS-202606-0001" || list[0].Net != "3963.21" || list[0].Corrected || list[0].PayDate != "2026-06-26") {
		t.Errorf("list: %+v", list)
	}
	if err != nil || len(list) != 1 {
		t.Fatalf("my payslips: %+v %v", list, err)
	}
	view, err := svc.MyPayslip(ctx, ben, mustUUID(list[0].ID))
	if err != nil || view.Employer != "Acme Pte. Ltd." || view.Employee != "Ben Lim" || view.Basic != "4500.00" || view.PeriodStart != "2026-06-01" || view.PeriodEnd != "2026-06-30" || len(view.Allowances) != 1 || len(view.Additional) != 1 ||
		view.Additional[0].Name != "Monthly bonus" || len(view.SHA256) != 64 || view.OvertimePay != "106.21" || view.OvertimeHours != "3.00" || view.OvertimePeriod != "" {
		t.Fatalf("the payslip: %+v %v", view, err)
	}
	names := []string{}
	for _, d := range view.Deductions {
		names = append(names, d.Name)
	}
	if strings.Join(names, "|") != "accommodation|Employee's CPF contribution|Self-help group fund" {
		t.Errorf("deductions: %v", names)
	}
	ref, pdf, err := svc.MyPayslipFile(ctx, ben, mustUUID(list[0].ID))
	if err != nil || ref != "PS-202606-0001" || !bytes.HasPrefix(pdf, []byte("%PDF")) {
		t.Fatalf("file: %s %v", ref, err)
	}
	if sum := sha256.Sum256(pdf); hex.EncodeToString(sum[:]) != view.SHA256 {
		t.Error("the PDF does not hash to the payslip's hash")
	}
	// Nobody else reads it: another login finds none and no such payslip.
	if other, _ := svc.MyPayslips(ctx, stranger); len(other) != 0 {
		t.Errorf("someone else's list: %+v", other)
	}
	if _, err := svc.MyPayslip(ctx, stranger, mustUUID(list[0].ID)); payCode(err) != payroll.CodeNotFound {
		t.Errorf("someone else's payslip: %v", err)
	}
	if _, _, err := svc.MyPayslipFile(ctx, stranger, mustUUID(list[0].ID)); payCode(err) != payroll.CodeNotFound {
		t.Errorf("someone else's file: %v", err)
	}
	// Evidence: the row never changes, and a changed byte is never served.
	su := connect(t, db.SuperURL())
	if _, err := su.Exec(ctx, "UPDATE nemura.payslip SET data = '{}'::jsonb"); err == nil || !strings.Contains(err.Error(), "NM127") {
		t.Errorf("a payslip changed: %v", err)
	}
	var key string
	_ = su.QueryRow(ctx, "SELECT blob_key FROM nemura.payslip").Scan(&key)
	if err := filepath.WalkDir(dir, func(p string, e os.DirEntry, err error) error {
		if err == nil && !e.IsDir() && strings.HasSuffix(p, ".pdf") {
			return os.WriteFile(p, []byte("%PDF tampered"), 0o600)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.MyPayslipFile(ctx, ben, mustUUID(list[0].ID)); payCode(err) != payroll.CodeConflict {
		t.Errorf("a changed byte was served: %v", err)
	}
	// A correction has its own payslip and the first says it is corrected.
	c2, err := svc.Recalculate(ctx, prep, mustUUID(run.ID), "A bonus was missed.")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Approve(ctx, approver, mustUUID(run.ID), 2, c2.Latest.Digest); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PublishPayslips(ctx, prep, mustUUID(run.ID), 1); payCode(err) != payroll.CodeConflict {
		t.Errorf("payslips of a superseded version: %v", err)
	}
	if n, err := svc.PublishPayslips(ctx, prep, mustUUID(run.ID), 2); err != nil || n != 1 {
		t.Fatalf("the corrected version's payslips: %d %v", n, err)
	}
	both, _ := svc.MyPayslips(ctx, ben)
	if len(both) != 2 || both[0].Version != 2 || both[0].Corrected || both[1].Version != 1 || !both[1].Corrected {
		t.Errorf("corrected payslips: %+v", both)
	}
	var leaks int
	if err := su.QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE organisation_id = $1 AND body ~ '(^|[^0-9a-fA-F-])(4500\.00|3500\.00|5100\.00|1020|500\.00)([^0-9a-fA-F-]|$)'`, lt.c.org).Scan(&leaks); err != nil || leaks != 0 {
		t.Errorf("amounts in the audit trail: %d %v", leaks, err)
	}
}

// A person's payment moves through states one legal step at a time; "paid" and a return need evidence or Fyniq's instruction; an unknown outcome blocks a new payment of that person in that
// run until it is resolved; the database holds the same moves as the Go list, and an event is never changed (NM-PAY-10).
// req: NM-PAY-10
// asvs: V8.2.1, V8.3.1
func TestAPaymentMovesThroughLegalStatesAndAnUnknownOutcomeBlocksANewPayment(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := d("2026-11-20").Add(10 * time.Hour)
	st := store(t, "nemura_api")
	packsSvc, err := nemura.NewPacksService(st, clock.NewFrozen(now))
	if err != nil {
		t.Fatal(err)
	}
	blobs, _ := local.NewDir(t.TempDir())
	svc := nemura.PayrollService{Store: st, Clock: clock.NewFrozen(now), Packs: packsSvc, Blobs: blobs}
	rs := nemura.RestrictedService{Store: st, Blobs: blobs, Clock: clock.NewFrozen(now)}
	prep := api.Session{Organisation: lt.c.org, Member: lt.c.pay}
	approver := api.Session{Organisation: lt.c.org, Member: lt.c.owner, StepUpAt: &now}
	birth := d("1990-05-20")
	gid, _ := svc.CreateGroup(ctx, prep, api.PayGroupInput{Name: "Monthly", PayDay: 25, CutoffDays: 7, ApprovalDays: 3})
	if err := svc.SetPay(ctx, prep, lt.ben, d("2026-01-05"), api.PersonPayInput{GroupID: gid, BasicMonthly: "4500.00", SHGFund: "CDAC"}); err != nil {
		t.Fatal(err)
	}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.SetClassification(ctx, tx, lt.c.org, now, lt.c.owner, lt.ben, d("2026-01-06"), people.Classification{ContractType: "service", EACovered: true, PartIVClass: "none", Residency: "citizen", BirthDate: &birth})
	})
	run, err := svc.StartRun(ctx, prep, d("2026-06-01"), mustUUID(gid))
	if err != nil {
		t.Fatal(err)
	}
	runID := mustUUID(run.ID)
	owed := run.Latest.Totals.Disburse
	pay := func(source, state string, mod func(*nemura.PaymentInput)) error {
		return lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
			in := nemura.PaymentInput{Run: runID, Person: lt.ben, State: state, Amount: owed, PaidOn: d("2026-06-25"), Source: source}
			if source == "fyniq" {
				in.Instruction = "INSTR-1"
			}
			if mod != nil {
				mod(&in)
			}
			actor := audit.Actor{Type: "service", ID: "link"}
			if in.By != nil {
				actor = audit.Actor{Type: "user", ID: in.By.String()}
			}
			return nemura.RecordPaymentState(ctx, tx, lt.c.org, now, actor, in)
		})
	}
	if err := pay("fyniq", "scheduled", nil); payCode(err) != payroll.CodeConflict {
		t.Errorf("a payment in a run nobody approved: %v", err)
	}
	if err := svc.Approve(ctx, approver, runID, 1, run.Latest.Digest); err != nil {
		t.Fatal(err)
	}
	doc, err := rs.AddDocument(ctx, api.Session{Organisation: lt.c.org, Member: lt.c.hr}, lt.ben, "other", "Bank statement 25 Jun", "employment_record", "application/pdf", []byte("%PDF statement"), []string{"payroll_admin"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := rs.AddDocument(ctx, api.Session{Organisation: lt.c.org, Member: lt.c.hr}, lt.cy, "other", "Somebody else's", "employment_record", "application/pdf", []byte("%PDF other"), []string{"payroll_admin"})
	if err != nil {
		t.Fatal(err)
	}
	manual := func(c *nemura.PaymentInput) { c.By, c.Evidence = &lt.c.pay, &doc }
	// What is refused, in words.
	for name, tc := range map[string]struct {
		source, state string
		mod           func(*nemura.PaymentInput)
		code, text    string
	}{
		"a bare word from a person":              {"manual", "paid", func(c *nemura.PaymentInput) { c.By = &lt.c.pay }, payroll.CodeInvalid, "evidence"},
		"a state from Fyniq with no instruction": {"fyniq", "sent", func(c *nemura.PaymentInput) { c.Instruction = "" }, payroll.CodeInvalid, "instruction_id"},
		"an unknown state":                       {"fyniq", "paidish", nil, payroll.CodeInvalid, "state"},
		"a person owed nothing":                  {"fyniq", "sent", func(c *nemura.PaymentInput) { c.Person = lt.cy }, payroll.CodeInvalid, "nothing is paid"},
		"the evidence of somebody else":          {"manual", "paid", func(c *nemura.PaymentInput) { c.By, c.Evidence = &lt.c.pay, &other }, payroll.CodeInvalid, "document of the person"},
		"a payment dated in the future":          {"fyniq", "paid", func(c *nemura.PaymentInput) { c.PaidOn = d("2027-01-01") }, payroll.CodeInvalid, "future"},
		"an amount of 4000.005":                  {"fyniq", "sent", func(c *nemura.PaymentInput) { c.Amount = "4000.005" }, payroll.CodeInvalid, "amount"},
	} {
		if err := pay(tc.source, tc.state, tc.mod); payCode(err) != tc.code || !strings.Contains(err.Error(), tc.text) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// The ordinary path, then the illegal moves.
	for _, s := range []string{"scheduled", "released", "sent", "accepted"} {
		if err := pay("fyniq", s, nil); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	if err := pay("fyniq", "scheduled", nil); payCode(err) != payroll.CodeConflict || !strings.Contains(err.Error(), "from accepted to scheduled") {
		t.Errorf("a new payment while one is on its way: %v", err)
	}
	if err := pay("fyniq", "accepted", nil); payCode(err) != payroll.CodeConflict {
		t.Errorf("the same state twice: %v", err)
	}
	if err := pay("fyniq", "unknown", nil); err != nil {
		t.Fatal(err)
	}
	view := func() nemura.PaymentsRow {
		var v nemura.PaymentsRow
		lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			v, err = nemura.PaymentsOf(ctx, tx, runID)
			return err
		})
		return v
	}
	if v := view(); v.State != "approved" || len(v.Lines) != 1 || v.Lines[0].State != "unknown" || !v.Lines[0].Attention || len(v.Lines[0].Events) != 5 {
		t.Errorf("an unknown outcome: %+v", v)
	}
	payChecks := func() string {
		all, err := packsSvc.Checks(ctx, prep, now)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, f := range all {
			if f.Check == 9 || f.Check == 14 {
				out = append(out, fmt.Sprintf("#%d %s", f.Check, f.Status))
			}
		}
		return strings.Join(out, ", ")
	}
	if got := payChecks(); got != "#14 fail" {
		t.Errorf("Checks after an unknown outcome: %q", got)
	}
	var blocked bool
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		blocked, err = nemura.PaymentBlocked(ctx, tx, runID, lt.ben)
		return err
	})
	if !blocked {
		t.Error("an unknown outcome does not block")
	}
	if err := pay("fyniq", "scheduled", nil); payCode(err) != payroll.CodeConflict || !strings.Contains(err.Error(), "from unknown to scheduled") {
		t.Errorf("a new payment after an unknown outcome: %v", err)
	}
	// A corrected version does not clear it: the events are of the run and the person.
	c2, err := svc.Recalculate(ctx, prep, runID, "Checked.")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Approve(ctx, approver, runID, 2, c2.Latest.Digest); err != nil {
		t.Fatal(err)
	}
	if err := pay("fyniq", "scheduled", nil); payCode(err) != payroll.CodeConflict {
		t.Errorf("a new payment after a corrected version: %v", err)
	}
	// Resolved by the person with evidence: paid, but not the amount owed, which Checks will show.
	if err := pay("manual", "paid", func(c *nemura.PaymentInput) { manual(c); c.Amount = "3999.00" }); err != nil {
		t.Fatal(err)
	}
	if v := view(); v.State != "paid" || !v.Lines[0].AmountMismatch || v.Lines[0].Attention || v.Version != 2 {
		t.Errorf("paid: %+v", v)
	}
	// Paid on 25 June for the wrong amount, and five months on no payslip: #14 warns and #9 fails.
	if got := payChecks(); got != "#14 warn, #9 fail" && got != "#9 fail, #14 warn" {
		t.Errorf("Checks after a payment confirmed for the wrong amount: %q", got)
	}
	if err := pay("fyniq", "returned", nil); err != nil {
		t.Fatal(err)
	}
	if v := view(); v.State != "approved" || !v.Lines[0].Attention {
		t.Errorf("returned: %+v", v)
	}
	for _, s := range []string{"scheduled", "sent", "paid"} {
		if err := pay("fyniq", s, nil); err != nil {
			t.Fatalf("a new payment after a return, %s: %v", s, err)
		}
	}
	if v := view(); v.State != "paid" || v.Lines[0].AmountMismatch || len(v.Lines[0].Events) != 10 {
		t.Errorf("paid again: %+v", v)
	}
	// Evidence: nothing changes an event, no step is skipped, and the audit trail has the states and never the amounts.
	su := connect(t, db.SuperURL())
	for _, q := range []string{"UPDATE nemura.pay_payment_event SET state = 'paid'", "DELETE FROM nemura.pay_payment_event"} {
		if _, err := su.Exec(ctx, q); err == nil || !strings.Contains(err.Error(), "NM127") {
			t.Errorf("%s: %v", q, err)
		}
	}
	if _, err := su.Exec(ctx, "INSERT INTO nemura.pay_payment_event (organisation_id, run_id, person_id, seq, state, instruction_id, amount, paid_on, source, recorded_at) VALUES ($1, $2, $3, 20, 'paid', 'X', 1, '2026-06-25', 'fyniq', now())", lt.c.org, runID, lt.ben); err == nil || !strings.Contains(err.Error(), "NM175") {
		t.Errorf("an event number skipped: %v", err)
	}
	var leaks int
	if err := su.QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE organisation_id = $1 AND body ~ '(^|[^0-9a-fA-F-])(3999\.00|3999|4000\.00)([^0-9a-fA-F-]|$)'`, lt.c.org).Scan(&leaks); err != nil || leaks != 0 {
		t.Errorf("amounts in the audit trail: %d %v", leaks, err)
	}
	// The database holds the same moves as the Go list: every pair of states, in a transaction that is rolled back.
	for _, from := range payroll.PaymentStates {
		for _, to := range payroll.PaymentStates {
			tx, err := su.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			ins := func(seq int, state string) error {
				_, err := tx.Exec(ctx, "INSERT INTO nemura.pay_payment_event (organisation_id, run_id, person_id, seq, state, instruction_id, amount, paid_on, source, recorded_at) VALUES ($1, $2, $3, $4, $5, 'X', 1, '2026-06-25', 'fyniq', now())", lt.c.org, runID, lt.cy, seq, state)
				return err
			}
			if err := ins(1, from); err != nil {
				t.Fatal(err)
			}
			err = ins(2, to)
			if got := err == nil; got != payroll.LegalMove(from, to) {
				t.Errorf("%s to %s: the database allows %v, the Go list %v (%v)", from, to, got, payroll.LegalMove(from, to), err)
			}
			_ = tx.Rollback(ctx)
		}
	}
}

// A leaver's final pay: its deadline by the case from the pack, the leave left and its worth as a proposal, leave pay and a retrenchment benefit as items (a retrenchment benefit is never CPF wages, in the
// API and in the database), and a tax-clearance hold that holds every dollar due, is released only with a document of the person as evidence, and is history; Checks shows the final pay as late until it is
// paid (NM-PAY-06, NM-PAY-08, FR-PAY-10, N.1 #15).
// req: NM-PAY-06, NM-PAY-08, NM-INT-08
// asvs: V8.2.1, V8.3.1
func TestAFinalPayHasItsDeadlineItsItemsAndATaxClearanceHoldThatHoldsEverything(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := d("2026-11-20").Add(10 * time.Hour)
	st := store(t, "nemura_api")
	packsSvc, err := nemura.NewPacksService(st, clock.NewFrozen(now))
	if err != nil {
		t.Fatal(err)
	}
	blobs, _ := local.NewDir(t.TempDir())
	svc := nemura.PayrollService{Store: st, Clock: clock.NewFrozen(now), Packs: packsSvc, Blobs: blobs}
	rs := nemura.RestrictedService{Store: st, Blobs: blobs, Clock: clock.NewFrozen(now)}
	prep := api.Session{Organisation: lt.c.org, Member: lt.c.pay}
	approver := api.Session{Organisation: lt.c.org, Member: lt.c.owner, StepUpAt: &now}
	birth := d("1990-05-20")
	gid, _ := svc.CreateGroup(ctx, prep, api.PayGroupInput{Name: "Monthly", PayDay: 25, CutoffDays: 7, ApprovalDays: 3})
	if err := svc.SetPay(ctx, prep, lt.ben, d("2026-01-05"), api.PersonPayInput{GroupID: gid, BasicMonthly: "4500.00", SHGFund: "CDAC"}); err != nil {
		t.Fatal(err)
	}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.SetClassification(ctx, tx, lt.c.org, now, lt.c.owner, lt.ben, d("2026-01-06"), people.Classification{ContractType: "service", EACovered: true, PartIVClass: "none", Residency: "citizen", BirthDate: &birth})
	})
	if _, ok, _ := svc.FinalPayOf(ctx, prep, lt.ben); ok {
		t.Error("final pay of a person nobody has recorded leaving")
	}
	last := d("2026-06-30")
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.SetOffboarding(ctx, tx, lt.c.org, now, lt.c.hr, lt.ben, onboarding.Leaving{Kind: "termination", LastDay: last})
		return err
	})
	fp, ok, err := svc.FinalPayOf(ctx, prep, lt.ben)
	if err != nil || !ok || fp.Due != "2026-06-30" || fp.Latest != "2026-07-03" || fp.NoticeServed || !strings.Contains(fp.Rule, "3 working days") || !strings.Contains(fp.Rule, "sg-people@2026.9") || fp.Month != "2026-06-01" || fp.Hold.Held {
		t.Fatalf("final pay: %+v %v", fp, err)
	}
	// 3 days untaken at the unpaid-leave day rate: 4,500 × 3 ÷ 22 working days of June 2026 = 613.636, rounded once to 613.64 (RND-PAY-RATIO).
	if fp.UntakenDays != "3.00" || fp.LeavePay != "613.64" {
		t.Errorf("the leave left and its worth: %q %q", fp.UntakenDays, fp.LeavePay)
	}
	// Checks: the final pay was due by 3 July and is not paid.
	payChecks := func() string {
		all, err := packsSvc.Checks(ctx, prep, now)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, f := range all {
			if f.Check == 15 || f.Check == 2 {
				out = append(out, fmt.Sprintf("#%d %s", f.Check, f.Status))
			}
		}
		return strings.Join(out, ", ")
	}
	if got := payChecks(); got != "#15 fail" {
		t.Errorf("Checks, final pay not paid: %q", got)
	}
	// Items: leave pay and a pay in lieu of notice; a retrenchment benefit is never CPF wages.
	add := func(key, name, nature string, cpf bool) error {
		_, err := svc.AddOneOff(ctx, prep, lt.ben, d("2026-06-01"), api.OneOffInput{Key: key, Name: name, Nature: nature, Amount: "1000.00", CPFWages: cpf, ForMonth: "2026-06-01", PayableOn: "2026-06-30"})
		return err
	}
	if err := add("retrench", "Retrenchment benefit", "retrenchment_benefit", true); payCode(err) != payroll.CodeInvalid || !strings.Contains(err.Error(), "not CPF wages") {
		t.Errorf("a retrenchment benefit as CPF wages: %v", err)
	}
	su := connect(t, db.SuperURL())
	if _, err := su.Exec(ctx, "INSERT INTO nemura.pay_one_off (organisation_id, person_id, month, key, name, nature, amount, cpf_wages, created_by, created_at) VALUES ($1, $2, '2026-06-01', 'x', 'x', 'retrenchment_benefit', 1, true, $3, now())", lt.c.org, lt.ben, lt.c.pay); err == nil || !strings.Contains(err.Error(), "retrenchment_not_wages") {
		t.Errorf("the database lets a retrenchment benefit be CPF wages: %v", err)
	}
	if err := add("retrench", "Retrenchment benefit", "retrenchment_benefit", false); err != nil {
		t.Fatal(err)
	}
	if err := add("leave", "Leave pay on leaving", "leave_encashment", true); err != nil {
		t.Fatal(err)
	}
	// The hold: put on, once; every dollar due is held; a run flags it.
	doc, err := rs.AddDocument(ctx, api.Session{Organisation: lt.c.org, Member: lt.c.hr}, lt.ben, "other", "IRAS clearance letter", "employment_record", "application/pdf", []byte("%PDF clearance"), []string{"payroll_admin"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := rs.AddDocument(ctx, api.Session{Organisation: lt.c.org, Member: lt.c.hr}, lt.cy, "other", "Somebody else's", "employment_record", "application/pdf", []byte("%PDF other"), []string{"payroll_admin"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetTaxHold(ctx, prep, lt.ben, api.TaxHoldInput{Action: "release", From: "2026-07-10", EvidenceID: doc.String()}); payCode(err) != payroll.CodeConflict {
		t.Errorf("releasing a hold that is not there: %v", err)
	}
	if err := svc.SetTaxHold(ctx, prep, lt.ben, api.TaxHoldInput{Action: "hold", From: "2026-06-01", Reason: "IR21 filed."}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetTaxHold(ctx, prep, lt.ben, api.TaxHoldInput{Action: "hold", From: "2026-06-01"}); payCode(err) != payroll.CodeConflict {
		t.Errorf("a second hold while one is in force: %v", err)
	}
	if h, _ := svc.TaxHold(ctx, prep, lt.ben); !h.Held || h.Version != 1 || h.Reason != "IR21 filed." {
		t.Errorf("hold: %+v", h)
	}
	run, err := svc.StartRun(ctx, prep, d("2026-06-01"), mustUUID(gid))
	if err != nil {
		t.Fatal(err)
	}
	detail, err := svc.RunDetail(ctx, prep, mustUUID(run.ID))
	if err != nil {
		t.Fatal(err)
	}
	line := detail.Lines[0]
	if line.Disburse != "0.00" || line.Held == "0.00" || line.Held != line.Net {
		t.Errorf("a hold holds everything: net %s held %s disburse %s", line.Net, line.Held, line.Disburse)
	}
	codes := []string{}
	for _, f := range line.Flags {
		codes = append(codes, f.Code)
	}
	if !strings.Contains(strings.Join(codes, ","), "CLEARANCE_HOLD") || !strings.Contains(strings.Join(codes, ","), "FINAL_PAY_CPF") {
		t.Errorf("flags: %v", codes)
	}
	if err := svc.Approve(ctx, approver, mustUUID(run.ID), 1, run.Latest.Digest); err != nil {
		t.Fatal(err)
	}
	if got := payChecks(); got != "#15 warn" {
		t.Errorf("Checks, final pay held: %q", got)
	}
	// Release: with a document of the person only; then the next version pays.
	if err := svc.SetTaxHold(ctx, prep, lt.ben, api.TaxHoldInput{Action: "release", From: "2026-07-10"}); payCode(err) != payroll.CodeInvalid {
		t.Errorf("a release with no evidence: %v", err)
	}
	if err := svc.SetTaxHold(ctx, prep, lt.ben, api.TaxHoldInput{Action: "release", From: "2026-07-10", EvidenceID: other.String()}); payCode(err) != payroll.CodeInvalid || !strings.Contains(err.Error(), "document of the person") {
		t.Errorf("a release with somebody else's document: %v", err)
	}
	if err := svc.SetTaxHold(ctx, prep, lt.ben, api.TaxHoldInput{Action: "release", From: "2026-07-10", EvidenceID: doc.String()}); err != nil {
		t.Fatal(err)
	}
	if h, _ := svc.TaxHold(ctx, prep, lt.ben); h.Held || h.Version != 2 {
		t.Errorf("after the release: %+v", h)
	}
	for _, q := range []string{"UPDATE nemura.tax_hold SET status = 'held'", "DELETE FROM nemura.tax_hold"} {
		if _, err := su.Exec(ctx, q); err == nil || !strings.Contains(err.Error(), "NM127") {
			t.Errorf("%s: %v", q, err)
		}
	}
	if _, err := su.Exec(ctx, "INSERT INTO nemura.tax_hold (organisation_id, id, hold_id, person_id, version, status, effective_from, recorded_by, recorded_at) VALUES ($1, uuidv7(), uuidv7(), $2, 1, 'released', '2026-07-11', $3, now())", lt.c.org, lt.ben, lt.c.pay); err == nil {
		t.Error("a release with no evidence got in")
	}
	c2, err := svc.Recalculate(ctx, prep, mustUUID(run.ID), "The hold is released.")
	if err != nil {
		t.Fatal(err)
	}
	d2, err := svc.RunDetail(ctx, prep, mustUUID(run.ID))
	if err != nil {
		t.Fatal(err)
	}
	if l := d2.Lines[0]; l.Held != "0.00" || l.Disburse != l.Net {
		t.Errorf("after the release: net %s held %s disburse %s", l.Net, l.Held, l.Disburse)
	}
	if err := svc.Approve(ctx, approver, mustUUID(run.ID), 2, c2.Latest.Digest); err != nil {
		t.Fatal(err)
	}
	if got := payChecks(); got != "#15 fail" {
		t.Errorf("Checks, hold released and not paid: %q", got)
	}
	// Paid on 2 July, within the 3 working days: nothing to say. Paid on 6 July would be ⚠.
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.RecordPaymentState(ctx, tx, lt.c.org, now, audit.Actor{Type: "service", ID: "link"}, nemura.PaymentInput{Run: mustUUID(run.ID), Person: lt.ben, State: "paid", Instruction: "I-9", Amount: d2.Lines[0].Disburse, PaidOn: d("2026-07-02"), Source: "fyniq"})
	})
	if got := payChecks(); got != "" {
		t.Errorf("Checks, final pay paid in time: %q", got)
	}
	var leaks int
	if err := su.QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE organisation_id = $1 AND body ~ '(^|[^0-9a-fA-F-])(4500\.00|1000\.00|IR21 clearance)([^0-9a-fA-F-]|$)'`, lt.c.org).Scan(&leaks); err != nil || leaks != 0 {
		t.Errorf("amounts in the audit trail: %d %v", leaks, err)
	}
}
