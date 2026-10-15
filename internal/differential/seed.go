package differential

import (
	"strings"
	"time"

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

// FromSeed turns a generated sample company into a scenario (DEV-04: one generator for seed data,
// fixtures and golden sets). The first journal is reversed the day after it, so every golden set
// exercises a reversal.
func FromSeed(c seed.Company, name string) Scenario {
	s := Scenario{Schema: ScenarioSchema, Name: name, BaseCurrency: c.Organisation.BaseCurrency,
		Currencies: map[string]int{c.Organisation.BaseCurrency: 2}, Documents: []Document{}, Reversals: []Reversal{}}
	for _, a := range c.Accounts {
		s.Accounts = append(s.Accounts, Account{Code: a.Code, Type: accountType(a.Code), Active: true, Role: a.Role})
	}
	for _, j := range c.Journals {
		d := Document{Type: "manual_journal", ID: j.ID, Version: 1, Date: j.Date}
		for _, l := range j.Lines {
			amount := l.Debit
			if l.Debit == "0.00" {
				amount = "-" + l.Credit
			}
			d.Lines = append(d.Lines, Line{Account: l.Account, Currency: c.Organisation.BaseCurrency, Amount: strings.TrimPrefix(amount, "+")})
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
