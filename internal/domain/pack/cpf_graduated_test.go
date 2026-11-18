package pack_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/pack"
)

// req: ENG-30
// req: ENG-31
// req: QA-ENG-08
// req: FR-PLN-02
// The first two years of permanent residence and the graduated range, worked by hand from the CPF Board's tables: the
// formulas below are the printed ones, evaluated here without the pack.
func TestTheCPFPackHoldsTheGraduatedRangeAndEveryResidencyYear(t *testing.T) {
	v := shipped(t, "sg-cpf/2026.2")
	for _, c := range []struct {
		description, month, group string
		age                       int
		tw, total, employee       string
	}{
		// 55 & below, Table 1: 17% (TW) + 0.6 (TW - 500).
		{"nothing up to $50", "2026-06", pack.GroupSCSPR3, 30, "50.00", "0", "0"},
		{"the flat rate, all the employer's", "2026-06", pack.GroupSCSPR3, 30, "500.00", "85.00", "0"},
		{"17% x 600 = 102, plus 0.6 x 100 = 60", "2026-06", pack.GroupSCSPR3, 30, "600.00", "162.00", "60.00"},
		{"at $750 the formula meets 37% x 750 = 277.50", "2026-06", pack.GroupSCSPR3, 30, "750.00", "277.50", "150.00"},
		// Above 55 to 60, Table 1: 16% (TW) + 0.54 (TW - 500) in 2026, 16.5% (TW) + 0.57 (TW - 500) from 2027.
		{"16% x 600 = 96, plus 0.54 x 100 = 54", "2026-06", pack.GroupSCSPR3, 57, "600.00", "150.00", "54.00"},
		{"16.5% x 600 = 99, plus 0.57 x 100 = 57", "2027-06", pack.GroupSCSPR3, 57, "600.00", "156.00", "57.00"},
		// Table 2, 1st year G/G: above 60 to 65: 3.5% (TW) + 0.15 (TW - 500).
		{"3.5% x 650 = 22.75, plus 0.15 x 150 = 22.50", "2026-06", pack.GroupSPR1GG, 62, "650.00", "45.25", "22.50"},
		// Table 3, 2nd year G/G: 55 & below: 9% (TW) + 0.45 (TW - 500).
		{"9% x 700 = 63, plus 0.45 x 200 = 90", "2026-06", pack.GroupSPR2GG, 40, "700.00", "153.00", "90.00"},
		// Table 4, 1st year F/G: 55 & below: 17% (TW) + 0.15 (TW - 500).
		{"17% x 700 = 119, plus 0.15 x 200 = 30", "2026-06", pack.GroupSPR1FG, 40, "700.00", "149.00", "30.00"},
		// Table 5, 2nd year F/G: above 60 to 65: 12.5% (TW) + 0.225 (TW - 500) in 2026, 13% (TW) + 0.225 (TW - 500) in 2027.
		{"12.5% x 600 = 75, plus 0.225 x 100 = 22.50", "2026-06", pack.GroupSPR2FG, 62, "600.00", "97.50", "22.50"},
		{"13% x 600 = 78, plus 0.225 x 100 = 22.50", "2027-06", pack.GroupSPR2FG, 62, "600.00", "100.50", "22.50"},
	} {
		got, err := pack.CPFLowWagesOn(v, month(t, c.month), c.group, c.age, dec(t, c.tw))
		if err != nil || got.Total.Cmp(dec(t, c.total)) != 0 || got.Employee.Cmp(dec(t, c.employee)) != 0 {
			t.Errorf("%s: got %v, %v; want %s in all, %s by the employee", c.description, got, err, c.total, c.employee)
		}
	}
	for _, c := range []struct{ month, group, tw string }{
		{"2026-06", pack.GroupSCSPR3, "750.01"}, // above the range: the rates table's
		{"2025-12", pack.GroupSCSPR3, "600.00"}, // before 2026
		{"2026-06", "spr3", "600.00"},           // no such group
	} {
		if _, err := pack.CPFLowWagesOn(v, month(t, c.month), c.group, 30, dec(t, c.tw)); !errors.Is(err, pack.ErrNotHeld) {
			t.Errorf("%+v: got %v; want ErrNotHeld", c, err)
		}
	}
	// A wage of exactly $750 is still the graduated range: the rates start a cent above.
	if _, err := pack.CPFRatesOn(v, month(t, "2026-06"), pack.GroupSPR1GG, 30, dec(t, "750.00")); !errors.Is(err, pack.ErrNotHeld) {
		t.Errorf("$750 is not above $750: got %v; want ErrNotHeld", err)
	}
	// Rates above $750, employer and employee, as the printed totals and employee shares give them.
	for _, c := range []struct {
		month, group string
		age          int
		er, ee       string
	}{
		{"2026-06", pack.GroupSPR1GG, 30, "0.04", "0.05"},   // 9% in all, 5% by the employee
		{"2026-06", pack.GroupSPR2GG, 30, "0.09", "0.15"},   // 24% in all, 15% by the employee
		{"2026-06", pack.GroupSPR1FG, 30, "0.17", "0.05"},   // 22% in all, 5% by the employee
		{"2027-02", pack.GroupSPR2FG, 57, "0.165", "0.125"}, // 29% in all, 12.5% by the employee, from 2027
		{"2026-06", pack.GroupSPR2FG, 57, "0.16", "0.125"},  // 28.5% in all in 2026
	} {
		got, err := pack.CPFRatesOn(v, month(t, c.month), c.group, c.age, dec(t, "5000.00"))
		if err != nil || got.Employer.Cmp(dec(t, c.er)) != 0 || got.Employee.Cmp(dec(t, c.ee)) != 0 {
			t.Errorf("%+v: got %v, %v; want %s / %s", c, got, err, c.er, c.ee)
		}
	}
	// The Max. the CPF Board prints under a band is the band's total rate and the employee's rate at the $8,000 ceiling.
	for _, c := range []struct {
		month, group string
		age          int
		total, ee    string
	}{
		{"2026-06", pack.GroupSCSPR3, 30, "2960.00", "1600.00"},
		{"2027-06", pack.GroupSCSPR3, 57, "2840.00", "1520.00"},
		{"2026-06", pack.GroupSPR1GG, 62, "680.00", "400.00"},
		{"2026-06", pack.GroupSPR2FG, 62, "1600.00", "600.00"},
	} {
		got, err := pack.CPFOWMax(v, month(t, c.month), c.group, c.age)
		if err != nil || got.Total.Cmp(dec(t, c.total)) != 0 || got.Employee.Cmp(dec(t, c.ee)) != 0 {
			t.Errorf("max %+v: got %v, %v; want %s / %s", c, got, err, c.total, c.ee)
		}
	}
	// sg-cpf@2026.1 still loads and still holds no graduated figure and no first-year rate.
	old := shipped(t, "sg-cpf/2026.1")
	if _, err := pack.CPFLowWagesOn(old, month(t, "2026-06"), pack.GroupSCSPR3, 30, dec(t, "600.00")); !errors.Is(err, pack.ErrNotHeld) {
		t.Errorf("2026.1 has no graduated rates: got %v; want ErrNotHeld", err)
	}
	if _, err := pack.CPFRatesOn(old, month(t, "2026-06"), pack.GroupSPR1GG, 30, dec(t, "5000.00")); !errors.Is(err, pack.ErrNotHeld) {
		t.Errorf("2026.1 has no first-year rates: got %v; want ErrNotHeld", err)
	}
}

// req: ENG-30
// req: ENG-32
// req: QA-ENG-08
// A graduated formula must meet its band's full rate at $750, a group is one Fyniq reads, and a wrong rate fails its
// hand-pinned case, so none of them can be activated.
func TestTheCPFGraduatedTablesAreHeldToTheirFullRates(t *testing.T) {
	const dir = "sg-cpf/2026.2"
	// A slope mistyped by 0.01, a flat rate mistyped, a full rate mistyped: the formula and the full rate no longer meet.
	refused(t, dir, map[string]string{dir + "/low_wages.csv": "sc_spr3,,55,2026-01-01,,50.00,500.00,750.00,0.17,0.6→sc_spr3,,55,2026-01-01,,50.00,500.00,750.00,0.17,0.61"}, pack.CodeValue)
	refused(t, dir, map[string]string{dir + "/low_wages.csv": "spr2_gg,55,60,2026-01-01,,50.00,500.00,750.00,0.06,0.375→spr2_gg,55,60,2026-01-01,,50.00,500.00,750.00,0.07,0.375"}, pack.CodeValue)
	refused(t, dir, map[string]string{dir + "/rates.csv": "spr1_fg,,55,750.00,2026-01-01,,0.17,0.05→spr1_fg,,55,750.00,2026-01-01,,0.18,0.05"}, pack.CodeValue)
	refused(t, dir, map[string]string{dir + "/low_wages.csv": "sc_spr3,,55,2026-01-01,,50.00,500.00,750.00→spr3,,55,2026-01-01,,50.00,500.00,750.00"}, pack.CodeValue)
	refused(t, dir, map[string]string{dir + "/low_wages.csv": "sc_spr3,,55,2026-01-01,,50.00,500.00,750.00→sc_spr3,,55,2026-01-01,,50.00,750.00,500.00"}, pack.CodeValue)
	refused(t, dir, map[string]string{dir + "/rates.csv": "spr1_gg,,55,750.00→spr1_gg,55,,750.00"}, pack.CodeValue)
	// A wrong rate that still meets its own formula (both edited alike) fails the hand-pinned case.
	wrong, err := pack.Load(fixtureOf(t, dir, map[string]string{
		dir + "/rates.csv":     "spr2_gg,60,65,750.00,2026-01-01,,0.035,0.075→spr2_gg,60,65,750.00,2026-01-01,,0.04,0.075",
		dir + "/low_wages.csv": "spr2_gg,60,65,2026-01-01,,50.00,500.00,750.00,0.035,0.225→spr2_gg,60,65,2026-01-01,,50.00,500.00,750.00,0.04,0.225",
	}), dir)
	if err != nil {
		t.Fatal(err) // the two tables agree, so it loads; only the hand-pinned cases can catch it
	}
	if len(pack.Failed(pack.RunPinned(wrong))) == 0 {
		t.Error("a second-year rate of 4% instead of 3.5% passed the pinned cases")
	}
	if ps := pack.CheckActivation(wrong, pack.Activation{ProposedBy: uuid.New(), ApprovedBy: uuid.New(), Approval: uuid.New()}); len(ps) == 0 {
		t.Error("a version failing its pinned cases was activated")
	}
}
