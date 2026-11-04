package differential

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/chart"
	"github.com/cognizhi/fyniq/internal/seed"
)

// accountType is a sample account's type from its code's first digit, the workbook's ranges.
func accountType(code string) string {
	switch code[0] {
	case '1':
		return "asset"
	case '2':
		return "liability"
	case '3':
		return "equity"
	case '4':
		return "revenue"
	}
	return "expense"
}

// statementRoles are the standard chart's roles the scenario format carries (README, "Scenario").
var statementRoles = map[string]bool{"share-capital": true, "retained-earnings": true, "depreciation": true, "income-tax-expense": true,
	"director-loan": true, "deferred-remuneration": true}

// FromSeed turns a generated sample company into a scenario (DEV-04: one generator for seed data,
// fixtures and golden sets). The first journal is reversed the day after it, so every golden set
// exercises a reversal. Its accounts carry the standard chart's report groups, cash-flow lines, functions and roles,
// and it asks for every month's statements and KPIs (p1-reports 1.5, p1-dashboard-kpis 1.3): the sample company's
// figures are compared on the engine's own chart.
func FromSeed(c seed.Company, name string) Scenario {
	s := Scenario{Schema: ScenarioSchema, Name: name, BaseCurrency: c.Organisation.BaseCurrency,
		Currencies: map[string]int{c.Organisation.BaseCurrency: 2}, Documents: []Document{}, Reversals: []Reversal{},
		Statements: true, KPIs: json.RawMessage(`{"year_start_month":1,"counts":{}}`)}
	standard := map[string]chart.Account{}
	if all, err := chart.Standard(); err == nil {
		for _, a := range all {
			standard[a.Code] = a
		}
	}
	for _, a := range c.Accounts {
		x := Account{Code: a.Code, Type: accountType(a.Code), Active: true, Role: a.Role}
		if std, ok := standard[a.Code]; ok {
			x.Type, x.ReportGroup, x.CashFlowLine, x.Function = string(std.Type), std.ReportGroup, std.CashFlowLine, std.Function
			if x.Role == "" && statementRoles[string(std.Role)] {
				x.Role = string(std.Role)
			}
		} else if p, ok := seed.ExtraPlacements[a.Code]; ok {
			x.ReportGroup, x.CashFlowLine = p.ReportGroup, p.CashFlowLine
		}
		s.Accounts = append(s.Accounts, x)
	}
	for _, p := range c.People { // the directors, whom a line on a director's account names (FR-EQ-01)
		if p.Director {
			s.People = append(s.People, p.ID)
		}
	}
	for _, j := range c.Journals {
		d := Document{Type: "manual_journal", ID: j.ID, Version: 1, Date: j.Date}
		for _, l := range j.Lines {
			amount := l.Debit
			if l.Debit == "0.00" {
				amount = "-" + l.Credit
			}
			d.Lines = append(d.Lines, Line{Account: l.Account, Currency: c.Organisation.BaseCurrency, Amount: strings.TrimPrefix(amount, "+"),
				Party: l.Person, Origin: l.Origin})
		}
		s.Documents = append(s.Documents, d)
	}
	if len(c.Journals) > 0 {
		first := c.Journals[0]
		day, err := time.Parse(time.DateOnly, first.Date)
		if err == nil {
			s.Reversals = append(s.Reversals, Reversal{Document: first.ID, Date: day.AddDate(0, 0, 1).Format(time.DateOnly)})
		}
	}
	return s
}
