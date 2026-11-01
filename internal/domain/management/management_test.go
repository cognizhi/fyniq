package management

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// The top expenses keep the ten largest, ties by name, and add up the rest as one row; ten or fewer are all kept.
// req: FR-RPT-03
func TestTheTopExpensesKeepTenAndAddUpTheRest(t *testing.T) {
	sgd := money.MustLookup("SGD")
	m := func(s string) money.Money { x, _ := money.Parse(s, sgd); return x }
	var rows []Row
	for i := 1; i <= 12; i++ {
		rows = append(rows, Row{ID: uuid.New(), Code: fmt.Sprint(6000 + i), Name: fmt.Sprintf("Expense %02d", i), Amount: m(fmt.Sprintf("%d.00", i*10))})
	}
	rows = append(rows, Row{ID: uuid.New(), Code: "6100", Name: "A tie", Amount: m("120.00")})
	got, err := top(rows, sgd)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range got {
		names = append(names, r.Name+" "+r.Amount.String())
	}
	want := "A tie 120.00, Expense 12 120.00, Expense 11 110.00, Expense 10 100.00, Expense 09 90.00, Expense 08 80.00, Expense 07 70.00, " +
		"Expense 06 60.00, Expense 05 50.00, Expense 04 40.00, All other expenses 60.00"
	if strings.Join(names, ", ") != want {
		t.Errorf("the top expenses:\n got %s\nwant %s", strings.Join(names, ", "), want)
	}
	three := []Row{{Name: "Expense 01", Amount: m("10.00")}, {Name: "Expense 03", Amount: m("30.00")}, {Name: "Expense 02", Amount: m("20.00")}}
	if few, _ := top(three, sgd); len(few) != 3 || few[0].Name != "Expense 03" || few[2].Name != "Expense 01" {
		t.Errorf("three expenses: %+v", few)
	}
}
