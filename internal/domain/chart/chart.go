// Package chart is the standard chart of accounts an organisation loads once, before it has any account
// (FR-COA-01, PRD-001 rev 0.31). The accounts are data, standard-chart.csv, which the accountant reviews: a
// correction is a change to the file and its test, never to code.
package chart

import (
	"bytes"
	_ "embed"
	"encoding/csv"
	"fmt"
	"slices"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

//go:embed standard-chart.csv
var standard []byte

// Account is one account of the standard chart: what the organisation's account is created with, and where
// it came from (workbook, workbook-moved or prd-b5).
type Account struct {
	storage.NewAccount
	Source string
}

var header = []string{"code", "name", "type", "report_group", "cash_flow_line", "function", "money_account", "default_gst_code",
	"control_account", "payroll_cost", "role", "source", "guidance", "revalue"}

var types = []storage.AccountType{storage.Asset, storage.Liability, storage.Equity, storage.Revenue, storage.Expense}

// Standard returns the standard chart, by code; an error names the row the file gets wrong.
func Standard() ([]Account, error) {
	r := csv.NewReader(bytes.NewReader(standard))
	r.Comment = '#'
	r.FieldsPerRecord = len(header)
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("standard-chart.csv: %w", err)
	}
	if len(rows) == 0 || !slices.Equal(rows[0], header) {
		return nil, fmt.Errorf("standard-chart.csv: the header is not %v", header)
	}
	flag := func(code, field, v string) (bool, error) {
		switch v {
		case "yes":
			return true, nil
		case "no":
			return false, nil
		}
		return false, fmt.Errorf("standard-chart.csv: account %s: %s is %q, not yes or no", code, field, v)
	}
	out := make([]Account, 0, len(rows)-1)
	for _, f := range rows[1:] {
		a := Account{NewAccount: storage.NewAccount{Code: f[0], Name: f[1], Type: storage.AccountType(f[2]), Role: storage.AccountRole(f[10]),
			AccountAttributes: storage.AccountAttributes{ReportGroup: f[3], CashFlowLine: f[4], Function: f[5], DefaultGSTCode: f[7], Guidance: f[12]}},
			Source: f[11]}
		if !slices.Contains(types, a.Type) {
			return nil, fmt.Errorf("standard-chart.csv: account %s: %q is not an account type", a.Code, f[2])
		}
		if a.MoneyAccount, err = flag(a.Code, "money_account", f[6]); err != nil {
			return nil, err
		}
		if a.Control, err = flag(a.Code, "control_account", f[8]); err != nil {
			return nil, err
		}
		if a.PayrollCost, err = flag(a.Code, "payroll_cost", f[9]); err != nil {
			return nil, err
		}
		if a.Revalue, err = flag(a.Code, "revalue", f[13]); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}
