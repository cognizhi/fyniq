package chart

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The standard chart is the workbook's 83 accounts, one moved, B.5's three (PRD-001 rev 0.31) and 2330 withholding tax
// payable (rev 0.90, FR-AP-19) and 1160 Asset disposal clearing (rev 0.151, FR-FA-03): every
// code once, every system account B.5 names, and the flags as the PRD reads.
// req: FR-COA-01
// req: FR-COA-02
// req: ledger/chart-of-accounts Default useful life (rev 0.151)
func TestTheStandardChart(t *testing.T) {
	accounts, err := Standard()
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 88 {
		t.Fatalf("%d accounts; want 88", len(accounts))
	}
	byCode := map[string]Account{}
	sources := map[string]int{}
	for _, a := range accounts {
		if _, dup := byCode[a.Code]; dup {
			t.Errorf("account %s appears twice", a.Code)
		}
		byCode[a.Code] = a
		sources[a.Source]++
		if a.Name == "" || a.ReportGroup == "" || a.Guidance == "" {
			t.Errorf("account %s lacks its name, report group or guidance", a.Code)
		}
		if a.Function != "" && !slices.Contains([]string{"COGS", "R&D", "S&M", "G&A", "Other"}, a.Function) {
			t.Errorf("account %s: function %q", a.Code, a.Function)
		}
		if strings.Contains(strings.ToLower(a.Guidance), "sheet") {
			t.Errorf("account %s's guidance names a workbook sheet: %s", a.Code, a.Guidance)
		}
	}
	if want := map[string]int{"workbook": 82, "workbook-moved": 1, "prd-b5": 4, "prd-151": 1}; !maps.Equal(sources, want) {
		t.Errorf("sources %v; want %v", sources, want)
	}

	// Every system account of B.5, and the accounts rev 0.31 assigns.
	for code, name := range map[string]string{"1100": "Accounts receivable", "1210": "Prepayments", "1590": "Accumulated depreciation",
		"2010": "Accounts payable", "2100": "Salaries payable", "2110": "CPF & SDL payable", "2150": "Expense claims payable (staff)",
		"2200": "GST control", "2300": "Income tax payable", "2400": "Deferred revenue", "2999": "Suspense - ask accountant",
		"3100": "Retained earnings", "6020": "Employer CPF contributions", "6030": "Skills development levy & levies", "6610": "Depreciation",
		"7030": "FX & rounding differences", "8010": "Income tax expense", "2500": "Loan from director / shareholder",
		"2120": "Deferred remuneration payable", "1250": "Staff & director advances", "3010": "Share capital - Ordinary",
		"3020": "Share capital - Preference", "3300": "Capital contribution", "2700": "Convertible notes / SAFE", "6050": "Directors' fees",
		"7040": "Unrealised FX gain or loss", "7050": "Gain / (loss) on disposal of assets", "1050": "Payment gateway (e.g. Stripe)",
		"5040": "Payment processing fees", "2020": "Accrued expenses", "2330": "Withholding tax payable", "1160": "Asset disposal clearing"} {
		if a, ok := byCode[code]; !ok || a.Name != name {
			t.Errorf("account %s: %q; want %q", code, a.Name, name)
		}
	}
	if byCode["7050"].Source != "workbook-moved" || byCode["7040"].Source != "prd-b5" {
		t.Errorf("7040 and 7050: %+v / %+v", byCode["7040"], byCode["7050"])
	}

	// The default useful lives of the register's accounts (rev 0.151) [verify], and none elsewhere; every account meets
	// the chart's rules.
	for _, a := range accounts {
		want := map[string]int{"1510": 36, "1520": 60, "1530": 36, "1540": 36}[a.Code]
		if a.UsefulLife != want {
			t.Errorf("account %s: a useful life of %d months; want %d", a.Code, a.UsefulLife, want)
		}
		if p := CheckAccount(a.NewAccount); p != nil {
			t.Errorf("account %s breaks the chart's rules: %v", a.Code, p)
		}
	}

	// The flags, exactly.
	flagged := func(pick func(Account) bool) []string {
		var out []string
		for _, a := range accounts {
			if pick(a) {
				out = append(out, a.Code)
			}
		}
		return out
	}
	for name, c := range map[string]struct {
		got, want []string
	}{
		"payroll cost": {flagged(func(a Account) bool { return a.PayrollCost }), []string{"5050", "6011", "6012", "6013", "6020", "6030", "6050"}},
		"control":      {flagged(func(a Account) bool { return a.Control }), []string{"1100", "2010", "2120", "2500"}},
		"revalue": {flagged(func(a Account) bool { return a.Revalue }), []string{"1010", "1020", "1030", "1040", "1050", "1100", "1250", "2010", "2030",
			"2120", "2500"}},
		"share capital": {flagged(func(a Account) bool { return a.Role == storage.RoleShareCapital }), []string{"3010", "3020"}},
		"system": {flagged(func(a Account) bool { return a.Role != "" }), []string{"1100", "1210", "1590", "2010", "2100", "2110", "2120",
			"2150", "2200", "2300", "2330", "2400", "2410", "2500", "2999", "3010", "3020", "3100", "3300", "6020", "6030", "6610", "7030", "7040", "8010"}},
		"money": {flagged(func(a Account) bool { return a.MoneyAccount }), []string{"1010", "1020", "1030", "1040", "1050", "2030", "2150"}},
	} {
		if !slices.Equal(c.got, c.want) {
			t.Errorf("%s accounts: %v; want %v", name, c.got, c.want)
		}
	}
	types := map[storage.AccountType]int{}
	for _, a := range accounts {
		types[a.Type]++
	}
	if types[storage.Expense] == 0 || types[storage.Revenue] == 0 || len(types) != 5 {
		t.Errorf("types: %v", types)
	}
}
