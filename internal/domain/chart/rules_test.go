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

// Only a monetary asset or liability is revalued at month end: never an equity, income or expense account, prepayments
// or deferred revenue (FR-GL-04; rev 0.72).
// req: FR-GL-04
func TestOnlyAMonetaryItemIsRevalued(t *testing.T) {
	for _, ok := range []storage.NewAccount{
		{Code: "1030", Name: "Wise USD", Type: storage.Asset, AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "USD", Revalue: true}},
		{Code: "2500", Name: "Loan from director", Type: storage.Liability, Role: "director-loan", AccountAttributes: storage.AccountAttributes{Revalue: true}},
	} {
		if p := CheckAccount(ok); p != nil {
			t.Errorf("%s revalued: %v", ok.Code, p)
		}
	}
	for _, bad := range []storage.NewAccount{
		{Code: "6110", Name: "Rent", Type: storage.Expense, AccountAttributes: storage.AccountAttributes{Revalue: true}},
		{Code: "3010", Name: "Share capital", Type: storage.Equity, AccountAttributes: storage.AccountAttributes{Revalue: true}},
		{Code: "1210", Name: "Prepayments", Type: storage.Asset, Role: "prepayments", AccountAttributes: storage.AccountAttributes{Revalue: true}},
		{Code: "2400", Name: "Deferred revenue", Type: storage.Liability, Role: "deferred-revenue", AccountAttributes: storage.AccountAttributes{Revalue: true}},
	} {
		if got := codes(CheckAccount(bad)); !slices.Equal(got, []string{"COA-10 revalue"}) {
			t.Errorf("%s revalued: %v", bad.Code, got)
		}
	}
	prepaid := storage.Account{Code: "1210", Name: "Prepayments", Type: storage.Asset, Active: true, Role: "prepayments"}
	if got := codes(CheckChange(prepaid, storage.AccountChange{Name: "Prepayments", Active: true, AccountAttributes: storage.AccountAttributes{Revalue: true}})); !slices.Equal(got, []string{"COA-10 revalue"}) {
		t.Errorf("marking prepayments: %v", got)
	}
}

// A default useful life is whole months from 1 to 600, on an asset account of the register only: a heading, an expense
// account or one of another report group refuses it, and changing the report group away from the register needs the
// life cleared first (rev 0.151).
// req: FR-COA-02
// req: ledger/chart-of-accounts Default useful life (rev 0.151)
func TestOnlyARegisterAccountHasAUsefulLife(t *testing.T) {
	ppe := storage.AccountAttributes{ReportGroup: "Property, plant & equipment"}
	life := func(a storage.AccountAttributes, months int) storage.AccountAttributes {
		a.UsefulLife = months
		return a
	}
	for _, ok := range []storage.NewAccount{
		{Code: "1520", Name: "Office equipment & furniture", Type: storage.Asset, AccountAttributes: life(ppe, 60)},
		{Code: "1540", Name: "Software", Type: storage.Asset, AccountAttributes: life(storage.AccountAttributes{ReportGroup: "Intangible assets"}, 1)},
		{Code: "1530", Name: "Renovation & fit-out", Type: storage.Asset, AccountAttributes: life(ppe, 600)},
		{Code: "6110", Name: "Rent", Type: storage.Expense},
	} {
		if p := CheckAccount(ok); p != nil {
			t.Errorf("%s with %d months: %v", ok.Code, ok.UsefulLife, p)
		}
	}
	for _, bad := range []storage.NewAccount{
		{Code: "1520", Name: "Office equipment & furniture", Type: storage.Asset, AccountAttributes: life(ppe, 601)},
		{Code: "1520", Name: "Office equipment & furniture", Type: storage.Asset, AccountAttributes: life(ppe, -1)},
		{Code: "1500", Name: "Fixed assets", Type: storage.Asset, Group: true, AccountAttributes: life(ppe, 36)},
		{Code: "6110", Name: "Rent", Type: storage.Expense, AccountAttributes: life(storage.AccountAttributes{}, 36)},
		{Code: "1220", Name: "Deposits", Type: storage.Asset, AccountAttributes: life(storage.AccountAttributes{ReportGroup: "Other current assets"}, 36)},
	} {
		if got := codes(CheckAccount(bad)); !slices.Equal(got, []string{"COA-11 useful_life_months"}) {
			t.Errorf("%s with %d months: %v", bad.Code, bad.UsefulLife, got)
		}
	}
	equipment := storage.Account{Code: "1510", Name: "Computer equipment", Type: storage.Asset, Active: true, AccountAttributes: life(ppe, 36)}
	moved := storage.AccountChange{Name: "Computer equipment", Active: true, AccountAttributes: life(storage.AccountAttributes{ReportGroup: "Other current assets"}, 36)}
	if got := codes(CheckChange(equipment, moved)); !slices.Equal(got, []string{"COA-11 useful_life_months"}) {
		t.Errorf("moving 1510 out of the register with its life: %v", got)
	}
	moved.UsefulLife = 0
	if p := CheckChange(equipment, moved); p != nil {
		t.Errorf("moving 1510 out of the register, its life cleared: %v", p)
	}
}
