//go:build local

package nemura_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/payroll"
)

func payCode(err error) string {
	var r *payroll.Refusal
	if errors.As(err, &r) {
		return r.Code
	}
	return ""
}

// Pay records are history that is only ended, once: a person's pay, the allowances and fixed deductions with it, the permanent resident's election with its evidence, the year to date imported
// once and the one-off items of a month; the groups' calendar; and no audit event holds an amount (FR-EMP-01, FR-PAY-14, FR-PAY-17, NM-PAY-02, NM-PAY-03, D8).
// req: FR-EMP-01, FR-PAY-14, NM-PAY-02, NM-PAY-03
// asvs: V8.2.1, V8.3.1
func TestPayRecordsAreHistoryThatIsOnlyEndedAndNoAuditEventHoldsAnAmount(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := d("2026-11-20").Add(10 * time.Hour)
	svc := nemura.PayrollService{Store: store(t, "nemura_api"), Clock: clock.NewFrozen(now)}
	pay := api.Session{Organisation: lt.c.org, Member: lt.c.pay}
	su := connect(t, db.SuperURL())

	if _, err := svc.CreateGroup(ctx, pay, api.PayGroupInput{Name: "Monthly", PayDay: 40}); payCode(err) != payroll.CodeInvalid {
		t.Errorf("a pay day that is not a day: %v", err)
	}
	gid, err := svc.CreateGroup(ctx, pay, api.PayGroupInput{Name: "Monthly", PayDay: 25, CutoffDays: 7, ApprovalDays: 3})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateGroup(ctx, pay, api.PayGroupInput{Name: "Monthly", PayDay: 25, CutoffDays: 7, ApprovalDays: 3}); payCode(err) != payroll.CodeConflict {
		t.Errorf("a name used twice: %v", err)
	}
	if groups, _ := svc.Groups(ctx, pay); len(groups) != 1 || groups[0].PayDay != 25 {
		t.Errorf("groups: %+v", groups)
	}
	rec := api.PersonPayInput{GroupID: gid, BasicMonthly: "4500.00", SHGFund: "CDAC", Items: []api.PayItem{
		{Key: "transport", Name: "Transport allowance", Kind: "allowance", Nature: "allowance", Amount: "100.00", CPFWages: true},
		{Key: "dorm", Name: "Dormitory", Kind: "fixed_deduction", Nature: "accommodation", Amount: "200.00"}}}
	if _, ok, _ := svc.Pay(ctx, pay, lt.cy, d("2026-06-01")); ok {
		t.Error("pay before it is recorded")
	}
	if err := svc.SetPay(ctx, pay, lt.cy, d("2026-01-01"), rec); payCode(err) != "" && payCode(err) != payroll.CodeInvalid {
		t.Errorf("before the person started: %v", err)
	}
	if err := svc.SetPay(ctx, pay, lt.cy, d("2026-01-05"), api.PersonPayInput{GroupID: uuid.NewString(), BasicMonthly: "4500.00"}); payCode(err) != payroll.CodeInvalid {
		t.Errorf("a group that is not the company's: %v", err)
	}
	if err := svc.SetPay(ctx, pay, lt.cy, d("2026-01-05"), rec); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetPay(ctx, pay, lt.cy, d("2026-01-05"), rec); payCode(err) != payroll.CodeConflict {
		t.Errorf("a record on the current one's start day: %v", err)
	}
	rec2 := rec
	rec2.BasicMonthly = "5000.00"
	rec2.Items = rec.Items[:1]
	if err := svc.SetPay(ctx, pay, lt.cy, d("2026-07-01"), rec2); err != nil {
		t.Fatal(err)
	}
	first, ok, err := svc.Pay(ctx, pay, lt.cy, d("2026-06-01"))
	if err != nil || !ok || first.BasicMonthly != "4500.00" || first.To != "2026-07-01" || len(first.Items) != 2 || first.SHGFund != "CDAC" || first.Group != "Monthly" {
		t.Errorf("the first record: %+v %v", first, err)
	}
	if second, _, _ := svc.Pay(ctx, pay, lt.cy, d("2026-07-01")); second.BasicMonthly != "5000.00" || second.To != "" || len(second.Items) != 1 {
		t.Errorf("the second record: %+v", second)
	}
	for _, sql := range []string{"UPDATE nemura.person_pay SET basic_monthly = 1 WHERE effective_to IS NOT NULL", "DELETE FROM nemura.person_pay", "UPDATE nemura.person_pay_item SET amount = 1", "DELETE FROM nemura.person_pay_item"} {
		if _, err := su.Exec(ctx, sql); err == nil || !(strings.Contains(err.Error(), "NM171") || strings.Contains(err.Error(), "NM127")) {
			t.Errorf("%s: %v", sql, err)
		}
	}
	if _, err := su.Exec(ctx, "INSERT INTO nemura.person_pay (organisation_id, person_id, effective_from, pay_group_id, basic_monthly, created_by, created_at) VALUES ($1, $2, '2026-03-01', $3, 1, $4, now())", lt.c.org, lt.cy, gid, lt.c.pay); err == nil || !strings.Contains(err.Error(), "NM172") {
		t.Errorf("overlapping pay records: %v", err)
	}

	// The joint election needs a document of the person's file, and is history too.
	blobs, _ := local.NewDir(t.TempDir())
	rs := nemura.RestrictedService{Store: store(t, "nemura_api"), Blobs: blobs, Clock: clock.NewFrozen(now)}
	doc, err := rs.AddDocument(ctx, api.Session{Organisation: lt.c.org, Member: lt.c.hr}, lt.cy, "certificate", "CPF Board approval", "employment_record", "application/pdf", []byte("%PDF approval"), []string{"hr_admin"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetElection(ctx, pay, lt.cy, d("2026-02-10"), uuid.New()); payCode(err) != payroll.CodeInvalid {
		t.Errorf("an election with no evidence on the file: %v", err)
	}
	if err := svc.SetElection(ctx, pay, lt.cy, d("2026-02-10"), doc); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := svc.Pay(ctx, pay, lt.cy, d("2026-06-01")); !got.JointElection {
		t.Error("the election is not in force")
	}
	if err := svc.SetElection(ctx, pay, lt.cy, d("2026-02-10"), doc); payCode(err) != payroll.CodeConflict {
		t.Errorf("an election on the current one's start day: %v", err)
	}

	// The year to date is imported once for a year, whole, and read back.
	y := api.YTDInput{Year: 2026, OW: "25000.00", AWSubject: "0.00", AWPaid: "0.00", Through: "2026-05-01"}
	if err := svc.ImportYTD(ctx, pay, lt.cy, api.YTDInput{Year: 2026, OW: "1.00", AWSubject: "5.00", AWPaid: "4.00", Through: "2026-05-01"}); payCode(err) != payroll.CodeInvalid {
		t.Errorf("subject over paid: %v", err)
	}
	if err := svc.ImportYTD(ctx, pay, lt.cy, y); err != nil {
		t.Fatal(err)
	}
	if err := svc.ImportYTD(ctx, pay, lt.cy, y); payCode(err) != payroll.CodeConflict {
		t.Errorf("a second import: %v", err)
	}
	if got, _, _ := svc.Pay(ctx, pay, lt.cy, d("2026-06-01")); got.YearToDate == nil || got.YearToDate.OW != "25000.00" || got.YearToDate.Source != "import" {
		t.Errorf("year to date: %+v", got.YearToDate)
	}

	// One-off items: a key once per person and month, classed by their facts, and read by month.
	item := api.OneOffInput{Key: "bonus-2026", Name: "Annual bonus", Nature: "bonus", Amount: "5000.00", CPFWages: true, ForMonth: "2025-12-01", PayableOn: "2026-06-30"}
	if _, err := svc.AddOneOff(ctx, pay, lt.cy, d("2026-06-15"), item); payCode(err) != payroll.CodeInvalid {
		t.Errorf("a month that is not the first: %v", err)
	}
	if _, err := svc.AddOneOff(ctx, pay, lt.cy, d("2026-06-01"), item); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddOneOff(ctx, pay, lt.cy, d("2026-06-01"), item); payCode(err) != payroll.CodeConflict {
		t.Errorf("a key used twice: %v", err)
	}
	list, err := svc.OneOffs(ctx, pay, d("2026-06-01"))
	if err != nil || len(list) != 1 || list[0].Person != "Cy Wong" || list[0].Amount != "5000.00" || list[0].ForMonth != "2025-12-01" || list[0].Used {
		t.Errorf("one-offs: %+v %v", list, err)
	}
	if other, _ := svc.OneOffs(ctx, pay, d("2026-07-01")); len(other) != 0 {
		t.Errorf("another month: %d", len(other))
	}

	// No audit event holds an amount, an item or a figure of anyone's pay.
	var leaks int
	if err := su.QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE organisation_id = $1 AND (body LIKE '%4500%' OR body LIKE '%5000%' OR body LIKE '%25000%' OR body LIKE '%Dormitory%' OR body LIKE '%Transport%' OR body LIKE '%Annual bonus%')`, lt.c.org).Scan(&leaks); err != nil || leaks != 0 {
		t.Errorf("the audit trail holds pay in %d events: %v", leaks, err)
	}
	var events int
	if err := su.QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE organisation_id = $1 AND action LIKE 'nemura.person_pay.%' OR action LIKE 'nemura.pay_group.%' OR action LIKE 'nemura.cpf_ytd.%' OR action LIKE 'nemura.pay_one_off.%' OR action LIKE 'nemura.pr_election.%'`, lt.c.org).Scan(&events); err != nil || events < 7 {
		t.Errorf("events: %d %v", events, err)
	}
	_ = pgx.ErrNoRows
}
