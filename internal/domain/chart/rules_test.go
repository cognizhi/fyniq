package chart

import (
	"slices"
	"testing"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func codes(p []Problem) []string {
	var out []string
	for _, x := range p {
		out = append(out, x.Code+" "+x.Field)
	}
	slices.Sort(out)
	return out
}

// req: FR-COA-02
func TestANewAccountsRules(t *testing.T) {
	ok := storage.NewAccount{Code: "6115", Name: "Co-working memberships", Type: storage.Expense,
		AccountAttributes: storage.AccountAttributes{ReportGroup: "Premises & office", Function: "G&A", DefaultGSTCode: "TX", Currency: "SGD"}}
	if p := CheckAccount(ok); p != nil {
		t.Fatalf("a valid account: %v", p)
	}
	bad := storage.NewAccount{Code: "-6115", Name: " ", Type: "income", Role: "treasury",
		AccountAttributes: storage.AccountAttributes{Function: "Ops", DefaultGSTCode: "tx", Currency: "XYZ"}}
	want := []string{"COA-01 code", "COA-02 name", "COA-03 type", "COA-04 function", "COA-05 default_gst_code", "COA-06 currency", "COA-07 role"}
	if got := codes(CheckAccount(bad)); !slices.Equal(got, want) {
		t.Errorf("problems:\n got %v\nwant %v", got, want)
	}
}

// The control and payroll-cost flags change only with an approval; a system account stays active.
// req: FR-COA-02
// req: FR-COA-03
func TestAChangesRules(t *testing.T) {
	before := storage.Account{Code: "1100", Name: "Accounts receivable", Type: storage.Asset, Active: true, Role: "accounts-receivable",
		AccountAttributes: storage.AccountAttributes{Control: true}}
	change := storage.AccountChange{Name: "Trade receivables", Active: true, AccountAttributes: before.AccountAttributes}
	if p := CheckChange(before, change); p != nil {
		t.Fatalf("renaming a system account: %v", p)
	}
	change.Control, change.PayrollCost, change.Active = false, true, false
	want := []string{"COA-08 control", "COA-08 payroll_cost", "COA-09 active"}
	if got := codes(CheckChange(before, change)); !slices.Equal(got, want) {
		t.Errorf("problems:\n got %v\nwant %v", got, want)
	}
	plain := before
	plain.Role, plain.Control = "", false
	if p := CheckChange(plain, storage.AccountChange{Name: "Trade receivables", Active: false}); p != nil {
		t.Errorf("deactivating an account that is not a system account: %v", p)
	}
}
