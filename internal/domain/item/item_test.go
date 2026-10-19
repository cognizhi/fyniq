package item

import (
	"slices"
	"testing"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func codes(ps []Problem) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Code+" "+p.Field)
	}
	return out
}

// An item with every field passes; one that breaks every rule is told each at once, prices by their index.
// req: FR-ITM-01
func TestAnItemIsCheckedWholeAndEveryRuleReportedAtOnce(t *testing.T) {
	ok := storage.ItemDetails{Name: "HR consulting", Description: "HR consulting, by the hour", Type: storage.ItemService, Unit: "hour",
		GSTCode: "SR", Prices: []storage.ItemPrice{{Currency: "SGD", UnitPrice: "180"}, {Currency: "USD", UnitPrice: "0.000001"},
			{Currency: "JPY", UnitPrice: "0"}}}
	if p := Check(ok); p != nil {
		t.Fatalf("a valid item: %v", p)
	}
	if p := CheckCode("CONSULT-HR"); p != nil {
		t.Errorf("a valid code: %v", p)
	}
	for _, c := range []string{"", "-HR", "HR CONSULT", "A234567890123456789012345678901"} {
		if got := codes(CheckCode(c)); !slices.Equal(got, []string{"ITM-01 code"}) {
			t.Errorf("code %q: %v", c, got)
		}
	}
	bad := storage.ItemDetails{Name: " ", Description: "  ", Type: "subscription", Unit: "a unit longer than twenty", GSTCode: "sr",
		Prices: []storage.ItemPrice{{Currency: "SGD", UnitPrice: "-1"}, {Currency: "XYZ", UnitPrice: "1"}, {Currency: "SGD", UnitPrice: "2"},
			{Currency: "USD", UnitPrice: "1.0000001"}, {Currency: "EUR", UnitPrice: "1e3"}}}
	want := []string{"ITM-02 name", "ITM-03 type", "ITM-04 description", "ITM-04 unit", "ITM-07 gst_code", "ITM-06 prices[0].unit_price",
		"ITM-05 prices[1].currency", "ITM-05 prices[2].currency", "ITM-06 prices[3].unit_price", "ITM-06 prices[4].unit_price"}
	if got := codes(Check(bad)); !slices.Equal(got, want) {
		t.Errorf("problems:\n got %v\nwant %v", got, want)
	}
}

// Sales post to an active revenue account that takes postings, never a group, an inactive account or another type.
// req: FR-ITM-01
func TestTheRevenueAccountIsAnActiveRevenueLeaf(t *testing.T) {
	sales := storage.Account{Code: "4000", Type: storage.Revenue, Active: true}
	if p := CheckAccount(sales); p != nil {
		t.Errorf("an active revenue leaf: %v", p)
	}
	group, inactive, expense := sales, sales, sales
	group.Group, inactive.Active, expense.Type = true, false, storage.Expense
	for name, a := range map[string]storage.Account{"a group": group, "inactive": inactive, "an expense account": expense} {
		if got := codes(CheckAccount(a)); !slices.Equal(got, []string{"ITM-08 revenue_account_id"}) {
			t.Errorf("%s: %v", name, got)
		}
	}
}
