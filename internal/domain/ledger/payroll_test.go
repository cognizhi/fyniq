package ledger

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func payrollSettings() (Settings, map[string]uuid.UUID) {
	ids := map[string]uuid.UUID{}
	s := Settings{Base: sgd, Accounts: map[uuid.UUID]Account{}}
	add := func(code, typ, role string) {
		id := uuid.MustParse("0192f1a0-0000-7000-8000-00000000" + code)
		ids[code] = id
		s.Accounts[id] = Account{ID: id, Code: code, Active: true, Type: typ, Role: role}
	}
	add("1250", "asset", "")
	add("2100", "liability", RoleSalariesPayable)
	add("2110", "liability", RoleCPFSDLPayable)
	add("2150", "liability", RoleExpenseClaimsPayable)
	add("6011", "expense", "")
	add("6013", "expense", "")
	add("6020", "expense", RoleEmployerCPF)
	add("6030", "expense", RoleSDL)
	add("1010", "asset", "")
	a := s.Accounts[ids["1010"]]
	a.Money = true
	s.Accounts[ids["1010"]] = a
	return s, ids
}

// Worked by hand (FR-PAY-07, FR-PAY-18, FR-PPL-04, §10.2 and B.3): the October 2026 run of Nemura, with G&A at S$10,000.00 and R&D at
// S$20,000.00 gross; employee CPF S$3,000.00, employer CPF S$5,100.00, SDL S$75.00 and SHG S$20.00; an advance of S$500.00 recovered
// (1250) and S$300.00 of claims paid with salary (2150).
//
//	Net to pay = 30,000.00 - 3,000.00 - 20.00 - 500.00 + 300.00 = 26,780.00 (2100)
//	2110 = 3,000.00 + 5,100.00 + 75.00 + 20.00 = 8,195.00
//	Dr 300.00 + 20,000.00 + 10,000.00 + 5,100.00 + 75.00 = 35,475.00 = Cr 500.00 + 26,780.00 + 8,195.00
//
// Version 2 states the month's full figures again, with R&D at S$20,200.00 and employer CPF S$5,134.00: only the difference posts
// (2100 -200.00, 2110 -34.00, 6011 +200.00, 6020 +34.00), and a version arriving again or older posts nothing.
// part of FR-PAY-07 (the posting rule; the requirement is tagged with the verified path of p1-payroll-link 5.3)
func TestPostPayrollRunAndItsCorrection(t *testing.T) {
	s, id := payrollSettings()
	run := PayrollRun{Source: "nemura", RunID: uuid.MustParse("0192f1a0-0000-7000-8000-000000000a01"), Version: 1,
		WageMonth: time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC), PostingDate: time.Date(2026, time.October, 31, 0, 0, 0, 0, time.UTC),
		Memo:        "Pay run October 2026",
		Departments: []PayrollDepartment{{Account: id["6013"], Gross: amt(t, "10000.00", sgd)}, {Account: id["6011"], Gross: amt(t, "20000.00", sgd)}},
		EmployeeCPF: amt(t, "3000.00", sgd), EmployerCPF: amt(t, "5100.00", sgd), SDL: amt(t, "75.00", sgd), SHG: amt(t, "20.00", sgd),
		Deductions:     []PayrollDeduction{{Account: id["1250"], Amount: amt(t, "500.00", sgd)}},
		Reimbursements: amt(t, "300.00", sgd)}
	e1, err := PostPayrollRun(run, PayrollPosted{}, s)
	if err != nil {
		t.Fatal(err)
	}
	want1 := "1250 -500.00 SGD = -500.00; 2100 -26780.00 SGD = -26780.00; 2110 -8195.00 SGD = -8195.00; 2150 300.00 SGD = 300.00; " +
		"6011 20000.00 SGD = 20000.00; 6013 10000.00 SGD = 10000.00; 6020 5100.00 SGD = 5100.00; 6030 75.00 SGD = 75.00"
	if got := legs(s, e1); got != want1 {
		t.Fatalf("%s\nwant %s", got, want1)
	}
	if e1.RuleVersion != RulePayrollRun || e1.Source.Type != SourcePayrollRun || e1.Source.Version != 1 || !e1.PostingDate.Equal(run.PostingDate) {
		t.Errorf("%+v", e1)
	}
	posted := PayrollTotals(PayrollPosted{}, e1)

	v2 := run
	v2.Version = 2
	v2.Departments = []PayrollDepartment{{Account: id["6013"], Gross: amt(t, "10000.00", sgd)}, {Account: id["6011"], Gross: amt(t, "20200.00", sgd)}}
	v2.EmployerCPF = amt(t, "5134.00", sgd)
	e2, err := PostPayrollRun(v2, posted, s)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := legs(s, e2), "2100 -200.00 SGD = -200.00; 2110 -34.00 SGD = -34.00; 6011 200.00 SGD = 200.00; 6020 34.00 SGD = 34.00"; got != want {
		t.Errorf("the difference: %s, want %s", got, want)
	}
	// Both entries together are version 2's figures: a version that arrives later as a replay of itself posts nothing.
	both := PayrollTotals(posted, e2)
	v3 := v2
	v3.Version = 3
	e3, err := PostPayrollRun(v3, both, s)
	if err != nil || len(e3.Lines) != 0 {
		t.Errorf("an unchanged version posts nothing: %v %v", e3.Lines, err)
	}
	// Whichever way the versions are applied, the totals end the same: version 2 posted first, then nothing for the replayed version 1.
	if _, err := PostPayrollRun(run, both, s); err == nil || !err.(Problems).Has(CodePayroll) {
		t.Errorf("a stale version: %v", err)
	}
	// The identity of a version is its own, and the run's source system is part of it.
	if e1.PostingIdentity == e2.PostingIdentity || e1.PostingIdentity != PayrollIdentity("nemura", run.RunID, 1, RulePayrollRun) {
		t.Errorf("%s %s", e1.PostingIdentity, e2.PostingIdentity)
	}
	if got, err := ReplayPayrollRun(RulePayrollRun, v2, posted, s); err != nil || legs(s, got) != legs(s, e2) {
		t.Errorf("replay: %v", err)
	}
	if _, err := ReplayPayrollRun("payroll_run@0", v2, posted, s); err == nil || !err.(Problems).Has(CodeUnknownRule) {
		t.Errorf("an unknown rule: %v", err)
	}
}

// A pay run that does not fit is refused, every failure together, and never posts a line for a person.
// part of FR-PAY-07 (the posting rule; the requirement is tagged with the verified path of p1-payroll-link 5.3)
func TestPayrollRunRefusals(t *testing.T) {
	s, id := payrollSettings()
	good := PayrollRun{Source: "nemura", RunID: uuid.MustParse("0192f1a0-0000-7000-8000-000000000a02"), Version: 1,
		WageMonth: time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC), PostingDate: time.Date(2026, time.October, 31, 0, 0, 0, 0, time.UTC),
		Departments: []PayrollDepartment{{Account: id["6013"], Gross: amt(t, "1000.00", sgd)}}, EmployeeCPF: amt(t, "200.00", sgd),
		EmployerCPF: amt(t, "170.00", sgd), SDL: amt(t, "2.50", sgd), SHG: amt(t, "0.00", sgd), Reimbursements: amt(t, "0.00", sgd)}
	if _, err := PostPayrollRun(good, PayrollPosted{}, s); err != nil {
		t.Fatal(err)
	}
	refused := func(name, code string, change func(*PayrollRun)) {
		t.Helper()
		x := good
		x.Departments = append([]PayrollDepartment(nil), good.Departments...)
		change(&x)
		if _, err := PostPayrollRun(x, PayrollPosted{}, s); err == nil || !err.(Problems).Has(code) {
			t.Errorf("%s: %v, want %s", name, err, code)
		}
	}
	refused("a department on a money account", CodePayroll, func(x *PayrollRun) { x.Departments[0].Account = id["1010"] })
	refused("a department on 6020", CodePayroll, func(x *PayrollRun) { x.Departments[0].Account = id["6020"] })
	refused("a department on an unknown account", CodeUnknownAccount, func(x *PayrollRun) { x.Departments[0].Account = uuid.MustParse("0192f1a0-0000-7000-8000-000000009999") })
	refused("another currency", CodePayroll, func(x *PayrollRun) { x.SDL = amt(t, "2.50", idr) })
	refused("a negative contribution", CodePayroll, func(x *PayrollRun) { x.EmployerCPF = amt(t, "-1.00", sgd) })
	refused("deductions beyond the pay", CodePayroll, func(x *PayrollRun) {
		x.Deductions = []PayrollDeduction{{Account: id["1250"], Amount: amt(t, "900.00", sgd)}}
	})
	refused("a deduction to salary expense", CodePayroll, func(x *PayrollRun) {
		x.Deductions = []PayrollDeduction{{Account: id["6013"], Amount: amt(t, "10.00", sgd)}}
	})
	refused("a deduction to the money account", CodePayroll, func(x *PayrollRun) {
		x.Deductions = []PayrollDeduction{{Account: id["1010"], Amount: amt(t, "10.00", sgd)}}
	})
	refused("a posting date before the wage month", CodePayroll, func(x *PayrollRun) { x.PostingDate = time.Date(2026, time.September, 30, 0, 0, 0, 0, time.UTC) })
	refused("no wage month", CodeNoDate, func(x *PayrollRun) { x.WageMonth = time.Time{} })
	refused("version 0", CodePayroll, func(x *PayrollRun) { x.Version = 0 })
	delete(s.Accounts, id["2100"])
	refused("no 2100", CodeSystemRole, func(*PayrollRun) {})
}

// The same run on the same month posts to the same account whichever of its department values a line carries, and a run's lines
// carry the department's dimension values (FR-DIM-02) and no person.
// part of FR-PAY-07 (the posting rule; the requirement is tagged with the verified path of p1-payroll-link 5.3)
func TestPayrollLinesCarryTheDepartmentsValuesAndNoPerson(t *testing.T) {
	s, id := payrollSettings()
	dim := uuid.MustParse("0192f1a0-0000-7000-8000-0000000d0001")
	val := uuid.MustParse("0192f1a0-0000-7000-8000-0000000d0002")
	s.Dimensions = map[uuid.UUID]Dimension{dim: {Name: "Department", Active: true}}
	s.Values = map[uuid.UUID]DimensionValue{val: {Dimension: dim, Code: "ENG", Active: true}}
	run := PayrollRun{Source: "nemura", RunID: uuid.MustParse("0192f1a0-0000-7000-8000-000000000a03"), Version: 1,
		WageMonth: time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC), PostingDate: time.Date(2026, time.October, 31, 0, 0, 0, 0, time.UTC),
		Departments: []PayrollDepartment{{Account: id["6011"], Values: []uuid.UUID{val}, Gross: amt(t, "1000.00", sgd)}},
		EmployeeCPF: amt(t, "200.00", sgd), EmployerCPF: amt(t, "170.00", sgd), SDL: amt(t, "2.50", sgd), SHG: amt(t, "0.00", sgd), Reimbursements: amt(t, "0.00", sgd)}
	e, err := PostPayrollRun(run, PayrollPosted{}, s)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range e.Lines {
		if l.Party != uuid.Nil {
			t.Errorf("a pay run holds no per-person line: %+v", l)
		}
	}
	if got := s.Accounts[e.Lines[2].AccountID].Code; got != "6011" || len(e.Lines[2].Values) != 1 || e.Lines[2].Values[0] != val {
		t.Errorf("the department's line: %+v", e.Lines)
	}
	run.Departments[0].Values = []uuid.UUID{uuid.MustParse("0192f1a0-0000-7000-8000-0000000d0099")}
	if _, err := PostPayrollRun(run, PayrollPosted{}, s); err == nil || !err.(Problems).Has(CodeDimensionValue) {
		t.Errorf("a value the organisation does not have: %v", err)
	}
}
