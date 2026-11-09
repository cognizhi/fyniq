package budget_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/budget"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/sheet"
)

var sgd = money.MustLookup("SGD")

func year(t *testing.T) period.Year {
	t.Helper()
	start, end := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2027, 12, 31, 0, 0, 0, 0, time.UTC)
	return period.Calendar{BooksStart: start, FirstYearEnd: end}.Year(1)
}

// req: FR-PLN-01
// req: RND-PLAN
// A copy with growth rounds each amount once, half away from zero, and keeps the months in order; a year of other
// months is refused, as is a growth that is no fraction.
func TestCopyWithGrowth(t *testing.T) {
	from := budget.Budget{Lines: []budget.BudgetLine{{AccountID: "a", Dimensions: []string{"v"}, Amounts: []string{"1000.00", "333.33", "0.10", "-50.00"}}}}
	got, err := budget.Copy(from, "0.05", 4, sgd)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"1050.00", "350.00", "0.11", "-52.50"}; strings.Join(got[0].Amounts, ",") != strings.Join(want, ",") || got[0].Dimensions[0] != "v" {
		t.Errorf("5%%: got %v, want %v", got[0].Amounts, want) // 333.33 × 1.05 = 349.9965; 0.10 × 1.05 = 0.105
	}
	got, _ = budget.Copy(from, "-0.1", 4, sgd)
	if got[0].Amounts[1] != "300.00" { // 299.997
		t.Errorf("-10%%: got %v", got[0].Amounts)
	}
	var e *budget.Error
	if _, err := budget.Copy(from, "0.05", 12, sgd); !errors.As(err, &e) || e.Code != budget.CodeBudget {
		t.Errorf("into a year of 12 months: %v", err)
	}
	for _, g := range []string{"5%", "-2", "11"} {
		if _, err := budget.Copy(from, g, 4, sgd); !errors.As(err, &e) {
			t.Errorf("growth %q: %v", g, err)
		}
	}
}

// req: FR-PLN-01
// req: ENG-23
// A sheet's header names Account, the dimensions and every month of the year; anything else is refused with its row
// and column before any line is read.
func TestTheSheetsHeader(t *testing.T) {
	acc := storage.Account{ID: uuid.New(), Code: "6200", Name: "Rent", Type: storage.Expense, Active: true}
	chart := budget.Chart{Accounts: map[uuid.UUID]storage.Account{acc.ID: acc}, ByCode: map[string]storage.Account{"6200": acc},
		Dimensions: map[string]uuid.UUID{}, Codes: map[uuid.UUID]map[string]uuid.UUID{}}
	grid := func(header ...string) sheet.Grid {
		row := []string{"6200"}
		for range len(header) - 1 {
			row = append(row, "1.00")
		}
		return sheet.Grid{Rows: [][]string{header, row}, Numeric: []map[int]bool{{}, {}}}
	}
	all := []string{"Account"}
	for m := 1; m <= 12; m++ {
		all = append(all, time.Date(2027, time.Month(m), 1, 0, 0, 0, 0, time.UTC).Format("Jan 2006"))
	}
	lines, probs := budget.FromSheet(grid(all...), year(t), chart)
	if probs != nil || len(lines) != 1 || lines[0].AccountID != acc.ID.String() || lines[0].Amounts[11] != "1.00" {
		t.Fatalf("a full header in Mon YYYY: %v %v", lines, probs)
	}
	for name, header := range map[string][]string{
		"no Account":         append([]string{"Code"}, all[1:]...),
		"a month missing":    all[:12],
		"a month of 2028":    append(append([]string{}, all[:12]...), "2028-01"),
		"a column not known": append(append([]string{}, all...), "Notes"),
	} {
		_, probs := budget.FromSheet(grid(header...), year(t), chart)
		if probs == nil || probs[0].Code != budget.CodeImport || !strings.HasPrefix(probs[0].Field, "row 1") {
			t.Errorf("%s: got %v", name, probs)
		}
	}
}
