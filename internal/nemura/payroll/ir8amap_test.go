package payroll

import (
	"strconv"
	"testing"
)

func box(n *int64) string {
	if n == nil {
		return "-"
	}
	return strconv.FormatInt(*n, 10)
}

// Each pay item of a year lands in the IR8A box its nature names, a box is summed before it is rounded to whole dollars (half away from zero), and a box with nothing in it is blank.
// Hand-worked: salary 3,000.40 + 3,000.40 + unpaid leave -100.30 = 5,900.50 -> 5,901; bonus 1,000.49 -> 1,000; an allowance 120.50 -> 121; a commission 0.49 -> 0 (a box with items in it shows the rounded sum, even 0); notice pay 2,000.00;
// retrenchment 5,000.00; leave encashment 400.00 joins the salary (6,300.50 -> 6,301). A box with no items is blank: here none is, but see the empty year below (NM-PAY-14) [accountant to confirm the mapping].
// req: NM-PAY-14
func TestIR8ABoxesAreTheItemsOfTheYearRoundedOnce(t *testing.T) {
	items := []MapItem{
		{Nature: "basic", Class: Ordinary, Amount: D("3000.40")},
		{Nature: "basic", Class: Ordinary, Amount: D("3000.40")},
		{Nature: "unpaid_leave", Class: Ordinary, Amount: D("-100.30")},
		{Nature: "bonus", Class: Additional, Amount: D("1000.49")},
		{Nature: "allowance", Class: Ordinary, Amount: D("120.50")},
		{Nature: "commission", Class: Additional, Amount: D("0.49")},
		{Nature: NatureNoticePay, Class: NotWages, Amount: D("2000.00")},
		{Nature: NatureRetrenchment, Class: NotWages, Amount: D("5000.00")},
		{Nature: NatureLeavePay, Class: NotWages, Amount: D("400.00")},
		{Nature: "basic", Amount: nil},
	}
	b := IR8AFrom(items)
	got := []string{box(b.Salary), box(b.Bonus), box(b.Allowances), box(b.Commission), box(b.Gratuity), box(b.CompensationForLoss)}
	want := []string{"6301", "1000", "121", "0", "2000", "5000"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("box %d is %s, want %s (all: %v)", i, got[i], want[i], got)
		}
	}
	if e := IR8AFrom(nil); e.Salary != nil || e.Bonus != nil || e.Allowances != nil || e.Commission != nil || e.Gratuity != nil || e.CompensationForLoss != nil {
		t.Errorf("a year of nothing has blank boxes: %+v", e)
	}
}

// An item with no nature of its own is classed by its CPF class, and a half dollar rounds away from zero, a negative sum too.
// req: NM-PAY-14
func TestIR8AAnItemWithNoNatureFollowsItsCPFClass(t *testing.T) {
	b := IR8AFrom([]MapItem{
		{Nature: "other", Class: Ordinary, Amount: D("10.50")},
		{Nature: "", Class: Additional, Amount: D("20.50")},
		{Nature: "other", Class: NotWages, Amount: D("30.50")},
		{Nature: "other", Class: NotWages, Amount: D("-30.50")},
		{Nature: "allowance", Class: Ordinary, Amount: D("-0.50")},
		{Nature: "allowance", Class: Ordinary, Amount: D("-1.00")},
	})
	if box(b.Salary) != "11" || box(b.Bonus) != "21" || box(b.Allowances) != "-2" {
		t.Errorf("salary %s bonus %s allowances %s, want 11, 21, -2", box(b.Salary), box(b.Bonus), box(b.Allowances))
	}
}
