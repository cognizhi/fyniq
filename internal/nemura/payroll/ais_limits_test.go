package payroll_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/nemura/payroll"
)

func rules(fs []payroll.Finding) string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Rule)
	}
	return strings.Join(out, ",")
}

// Each limit of IRAS's data items holds at the limit and is broken one past it (AIS-API 2.0 version 2.6, section 3.4): the income year from four years back to one advance year, text
// of 100 and 60 characters, 2,000 records, an amount of nine digits, and a period that may start and end on the same day.
// req: NM-PAY-14
func TestTheDataItemLimitsHoldAtTheLimitAndBreakOnePast(t *testing.T) {
	type change func(*payroll.AISOrg, *payroll.AISPerson)
	for name, tc := range map[string]struct {
		at, past change
		rule     string
	}{
		"income year back": {func(o *payroll.AISOrg, _ *payroll.AISPerson) { o.IncomeYear = 2021; o.CurrentYear = 2025 },
			func(o *payroll.AISOrg, _ *payroll.AISPerson) { o.IncomeYear = 2020; o.CurrentYear = 2025 }, "A3"},
		"income year ahead": {func(o *payroll.AISOrg, _ *payroll.AISPerson) { o.IncomeYear = 2026; o.CurrentYear = 2025 },
			func(o *payroll.AISOrg, _ *payroll.AISPerson) { o.IncomeYear = 2027; o.CurrentYear = 2025 }, "A3"},
		"authorised person": {func(o *payroll.AISOrg, _ *payroll.AISPerson) { o.AuthorisedPerson = strings.Repeat("a", 100) },
			func(o *payroll.AISOrg, _ *payroll.AISPerson) { o.AuthorisedPerson = strings.Repeat("a", 101) }, "A5"},
		"software company": {func(o *payroll.AISOrg, _ *payroll.AISPerson) { o.SoftwareCompany = strings.Repeat("a", 60) },
			func(o *payroll.AISOrg, _ *payroll.AISPerson) { o.SoftwareCompany = strings.Repeat("a", 61) }, "A9"},
		"software name": {func(o *payroll.AISOrg, _ *payroll.AISPerson) { o.SoftwareName = strings.Repeat("a", 60) },
			func(o *payroll.AISOrg, _ *payroll.AISPerson) { o.SoftwareName = strings.Repeat("a", 61) }, "A9"},
		"name": {func(_ *payroll.AISOrg, p *payroll.AISPerson) { p.Name = strings.Repeat("a", 100) },
			func(_ *payroll.AISOrg, p *payroll.AISPerson) { p.Name = strings.Repeat("a", 101) }, "B4"},
		"amount": {func(_ *payroll.AISOrg, p *payroll.AISPerson) { p.Salary = n(999999999) },
			func(_ *payroll.AISOrg, p *payroll.AISPerson) { p.Salary = n(1000000000) }, "C"},
		"negative amount": {func(_ *payroll.AISOrg, p *payroll.AISPerson) { p.Salary = n(0) },
			func(_ *payroll.AISOrg, p *payroll.AISPerson) { p.Salary = n(-1) }, "C"},
		"foreign address with a country": {func(_ *payroll.AISOrg, p *payroll.AISPerson) {
			p.AddressType, p.Address1, p.Country = "F", "1 High St", "GB"
		},
			func(_ *payroll.AISOrg, p *payroll.AISPerson) {
				p.AddressType, p.Address1, p.Country, p.PostalCode = "F", "1 High St", "GB", "SW1"
			}, "B10"},
		"foreign address with no country": {func(_ *payroll.AISOrg, p *payroll.AISPerson) {
			p.AddressType, p.Address1, p.Country = "F", "1 High St", "GB"
		},
			func(_ *payroll.AISOrg, p *payroll.AISPerson) { p.AddressType, p.Address1 = "F", "1 High St" }, "B10"},
		"address line 2 after line 1": {func(_ *payroll.AISOrg, p *payroll.AISPerson) {
			p.AddressType, p.Address1, p.PostalCode, p.Address2 = "L", "1 Raffles Place", "048616", "Level 3"
		}, func(_ *payroll.AISOrg, p *payroll.AISPerson) {
			p.AddressType, p.Address1, p.PostalCode, p.Address3 = "L", "1 Raffles Place", "048616", "Unit 4"
		}, "B8"},
		"cessation on the commencement day": {func(_ *payroll.AISOrg, p *payroll.AISPerson) {
			p.Commencement, p.Cessation, p.From, p.To = "2024-03-01", "2024-03-01", "2024-03-01", "2024-03-01"
		}, func(_ *payroll.AISOrg, p *payroll.AISPerson) {
			p.Commencement, p.Cessation, p.From, p.To = "2024-03-02", "2024-03-01", "2024-03-02", "2024-03-01"
		}, "B14"},
		"non-resident director's fees": {func(_ *payroll.AISOrg, p *payroll.AISPerson) {
			p.IDType, p.IDNumber, p.Designation, p.DirectorFees, p.DirectorFeesApprovedOn, p.Section45 = "12", "P123", "Director", n(1), "2024-03-01", "Y"
		}, func(_ *payroll.AISOrg, p *payroll.AISPerson) {
			p.IDType, p.IDNumber, p.Designation, p.DirectorFees, p.DirectorFeesApprovedOn = "12", "P123", "Director", n(1), "2024-03-01"
		}, "C8"},
	} {
		run := func(c change) string {
			o, p := aisOrg(), samplePeople()
			c(&o, &p[0])
			return rules(payroll.ValidateAIS(o, p[:1]))
		}
		has := func(got string) bool { return slices.Contains(strings.Split(got, ","), tc.rule) }
		if got := run(tc.at); has(got) {
			t.Errorf("%s: at the limit the findings are %q, want no %s", name, got, tc.rule)
		}
		if got := run(tc.past); !has(got) {
			t.Errorf("%s: one past the limit the findings are %q, want %s", name, got, tc.rule)
		}
	}
	// the 2,000-record limit, and the record number a finding names
	many := make([]payroll.AISPerson, payroll.MaxAISRecords)
	for i := range many {
		many[i] = samplePeople()[0]
	}
	if got := rules(payroll.ValidateAIS(aisOrg(), many)); got != "" {
		t.Errorf("2,000 records: %q", got)
	}
	if got := rules(payroll.ValidateAIS(aisOrg(), append(many, samplePeople()[0]))); got != "A11" {
		t.Errorf("2,001 records: %q", got)
	}
	people := samplePeople()
	people[1].Sex = "X"
	if fs := payroll.ValidateAIS(aisOrg(), people); len(fs) != 1 || fs[0].Record != 2 || !strings.Contains(fs[0].String(), " record 2 ") {
		t.Errorf("the second record's finding is %v", fs)
	}
}
