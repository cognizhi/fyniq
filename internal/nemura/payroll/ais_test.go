package payroll_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/nemura/payroll"
)

func n(v int64) *int64 { return &v }

func aisOrg() payroll.AISOrg {
	return payroll.AISOrg{CurrentYear: 2025, IncomeYear: 2024, EmployerTaxRef: "180011032C", AuthorisedPerson: "John Adams", ContactNumber: "91234567",
		Email: "abc@email.com.sg", SubmissionType: "O", SoftwareCompany: "SWD Organisation Name", SoftwareName: "Application Name"}
}

// The first two employees of IRAS's sample request (AIS-API 2.0 version 2.6, section 3.7.2, "IR8A only"): A, a chef who joined on 2 January 2024, and B, who left on 30 June
// 2024 and lives in Choa Chu Kang.
func samplePeople() []payroll.AISPerson {
	return []payroll.AISPerson{
		{IDType: "1", IDNumber: "S1234567A", Name: "Employee A", DateOfBirth: "1981-01-02", Sex: "M", Designation: "Chef", BankCode: "7232", Commencement: "2024-01-02",
			From: "2024-01-02", To: "2024-12-31", Salary: n(36000), Bonus: n(3000), BonusPaidOn: "2024-12-10", EmployeeCPF: n(7800), Donations: n(12)},
		{IDType: "1", IDNumber: "S9876543A", Name: "Employee B", DateOfBirth: "1990-11-02", Sex: "F", AddressType: "L", Address1: "Blk 123 Choa Chu Kang Rd #10-01", PostalCode: "670123",
			Cessation: "2024-06-30", From: "2024-01-01", To: "2024-06-30", Salary: n(24000), Bonus: n(2000), EmployeeCPF: n(5200), Donations: n(6)},
	}
}

// The payload has the sample request's object and property names, in its order, with blanks as null and an empty object for each appendix, and the totals the sample
// states: A earns 39,000 and B 26,000, and the organisation's total is their sum.
// req: FR-PAY-10
// req: NM-PAY-14
func TestThePayloadHasTheSampleRequestsShape(t *testing.T) {
	b, findings := payroll.AISPayload(aisOrg(), samplePeople(), false, false)
	if len(findings) > 0 {
		t.Fatal(findings)
	}
	var got struct {
		Org    map[string]any   `json:"orgAndSubmissionInfo"`
		People []map[string]any `json:"employeePersonalParticulars"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	wantOrg := []string{"validateOnly", "bypassWarning", "incomeYear", "employerTaxRefNumber", "authorisedPersonName", "contactNumber", "emailAddress", "submissionType",
		"softwareCompanyName", "softwareName", "totalNumberIR8A", "amountTotalIncomeOfIR8A", "totalNumberOfApp8A", "totalNumberOfApp8B"}
	for _, k := range wantOrg {
		if _, ok := got.Org[k]; !ok {
			t.Errorf("the organisation's block lacks %s", k)
		}
	}
	if len(got.Org) != len(wantOrg) || got.Org["totalNumberIR8A"] != float64(2) || got.Org["amountTotalIncomeOfIR8A"] != float64(65000) || got.Org["contactNumber"] != float64(91234567) {
		t.Errorf("the organisation's block is %v", got.Org)
	}
	wantIR8A := []string{"periodPaymentFromDate", "periodPaymentToDate", "salary", "bonus", "nonContractualBonusPaidDate", "directorFees", "directorsFeesApprovalDate", "section45Indicator",
		"allowances", "grossCommission", "gratuityNoticePayExGratiaPayment", "compensationForLossOfOffice", "compensationForLossOfOfficeIndicator", "pensionRetirementBenefitsAccruedFrom1993",
		"designatedPensionOrProvidentFundName", "employerContributionToPensionOrProvidentFundOutsideSg", "excessOrVoluntaryCpfContributionByEmployer", "gainsAndProfitsFromSOGS101b",
		"gainsAndProfitsUnderS101g", "valueOfBenefitsInKind", "othersIncome", "totalIncome", "exemptIncomeOrIncomeSubjectToTaxRemission", "exemptOrRemissionIncomeIndicator",
		"incomeTaxBorneByEmployerIndicator", "amountOfIncomeForTaxBorneByEmployer", "fixedAmountOfIncomeForTaxBorneByEmployee", "cpfOrDesignatedFund", "donationsDeductedThroughSalaries",
		"contributionsToMosqueBuildingFund", "lifeInsurancePremiumThroughSalaries"}
	ir := got.People[0]["ir8a"].(map[string]any)
	for _, k := range wantIR8A {
		if _, ok := ir[k]; !ok {
			t.Errorf("the IR8A lacks %s", k)
		}
	}
	if len(ir) != len(wantIR8A) || ir["totalIncome"] != float64(39000) || ir["directorFees"] != nil || ir["othersIncome"] != nil || ir["salary"] != float64(36000) || ir["cpfOrDesignatedFund"] != float64(7800) {
		t.Errorf("A's IR8A is %v", ir)
	}
	if got.People[1]["ir8a"].(map[string]any)["totalIncome"] != float64(26000) || got.People[1]["recordId"] != float64(2) {
		t.Errorf("B is %v", got.People[1])
	}
	for _, k := range []string{"appendix8A", "appendix8B"} {
		if m, ok := got.People[0][k].(map[string]any); !ok || len(m) != 0 {
			t.Errorf("%s is %v, want the empty object the specification recommends", k, got.People[0][k])
		}
	}
}

// Others income (C21) is the sum of its eight items and the total (C22) adds salary, bonus and director's fees: worked by hand: allowances 1,200 + commission 800 +
// gratuity 500 + excess CPF 300 = 2,800 others; with salary 60,000, bonus 5,000 and director's fees 10,000, the total is 77,800.
// req: FR-PAY-10
func TestOthersIncomeAndTheTotalAreSumsOfTheirItems(t *testing.T) {
	p := samplePeople()[0]
	p.From, p.Commencement = "2024-01-01", ""
	p.Salary, p.Bonus, p.DirectorFees, p.DirectorFeesApprovedOn = n(60000), n(5000), n(10000), "2024-03-31"
	p.Allowances, p.Commission, p.Gratuity, p.ExcessCPF = n(1200), n(800), n(500), n(300)
	if p.OthersIncome() != 2800 || p.TotalIncome() != 77800 {
		t.Fatalf("others %d total %d", p.OthersIncome(), p.TotalIncome())
	}
	b, f := payroll.AISPayload(aisOrg(), []payroll.AISPerson{p}, true, false)
	if len(f) > 0 {
		t.Fatal(f)
	}
	var got map[string]any
	_ = json.Unmarshal(b, &got)
	ir := got["employeePersonalParticulars"].([]any)[0].(map[string]any)["ir8a"].(map[string]any)
	if ir["othersIncome"] != float64(2800) || ir["totalIncome"] != float64(77800) || got["orgAndSubmissionInfo"].(map[string]any)["validateOnly"] != true {
		t.Errorf("the IR8A is %v", ir)
	}
}

// Each rule of IRAS's data items the validator states is broken by one change to a good record, and only the rule's own finding appears.
// req: FR-PAY-10
func TestEachDataItemRuleRefusesItsRecord(t *testing.T) {
	for name, tc := range map[string]struct {
		change func(*payroll.AISOrg, *payroll.AISPerson)
		rule   string
	}{
		"an income year too far back":  {func(o *payroll.AISOrg, _ *payroll.AISPerson) { o.IncomeYear = 2019 }, "A3"},
		"a tax reference of no form":   {func(o *payroll.AISOrg, _ *payroll.AISPerson) { o.EmployerTaxRef = "12345" }, "A4"},
		"a contact number with a plus": {func(o *payroll.AISOrg, _ *payroll.AISPerson) { o.ContactNumber = "+6591234567" }, "A6"},
		"an email of no form":          {func(o *payroll.AISOrg, _ *payroll.AISPerson) { o.Email = "abc" }, "A7"},
		"a submission type of its own": {func(o *payroll.AISOrg, _ *payroll.AISPerson) { o.SubmissionType = "X" }, "A8"},
		"an NRIC of no form":           {func(_ *payroll.AISOrg, p *payroll.AISPerson) { p.IDNumber = "X1234567A" }, "B3"},
		"no name":                      {func(_ *payroll.AISOrg, p *payroll.AISPerson) { p.Name = " " }, "B4"},
		"a date of birth of no form":   {func(_ *payroll.AISOrg, p *payroll.AISPerson) { p.DateOfBirth = "02/01/1981" }, "B5"},
		"a sex of its own":             {func(_ *payroll.AISOrg, p *payroll.AISPerson) { p.Sex = "X" }, "B6"},
		"a local address with no post code": {func(_ *payroll.AISOrg, p *payroll.AISPerson) {
			p.AddressType, p.Address1 = "L", "1 Raffles Place"
		}, "B9"},
		"an address of N.A.": {func(_ *payroll.AISOrg, p *payroll.AISPerson) {
			p.AddressType, p.Address1, p.PostalCode = "L", "N.A.", "048616"
		}, "B8"},
		"a period that starts late":          {func(_ *payroll.AISOrg, p *payroll.AISPerson) { p.From = "2024-02-01" }, "C1"},
		"a period that ends early":           {func(_ *payroll.AISOrg, p *payroll.AISPerson) { p.To = "2024-11-30" }, "C2"},
		"a bonus day with no bonus":          {func(_ *payroll.AISOrg, p *payroll.AISPerson) { p.Bonus = nil }, "C5"},
		"director's fees with no day":        {func(_ *payroll.AISOrg, p *payroll.AISPerson) { p.DirectorFees = n(100) }, "C7"},
		"loss of office with no indicator":   {func(_ *payroll.AISOrg, p *payroll.AISPerson) { p.CompensationForLoss = n(100) }, "C13"},
		"an exempt amount with no indicator": {func(_ *payroll.AISOrg, p *payroll.AISPerson) { p.Exempt = n(100) }, "C24"},
		"an amount of ten digits":            {func(_ *payroll.AISOrg, p *payroll.AISPerson) { p.Salary = n(1000000000) }, "C"},
		"a negative amount in an original":   {func(_ *payroll.AISOrg, p *payroll.AISPerson) { p.Allowances = n(-5) }, "C"},
		"benefits in kind with no appendix":  {func(_ *payroll.AISOrg, p *payroll.AISPerson) { p.BenefitsInKind = n(500) }, "D/E"},
	} {
		t.Run(name, func(t *testing.T) {
			o, p := aisOrg(), samplePeople()[0]
			tc.change(&o, &p)
			b, f := payroll.AISPayload(o, []payroll.AISPerson{p}, false, false)
			if b != nil {
				t.Fatal("a payload was made")
			}
			var rules []string
			for _, x := range f {
				rules = append(rules, x.Rule)
			}
			if !slices.Contains(rules, tc.rule) {
				t.Fatalf("want %s among %v", tc.rule, f)
			}
		})
	}
	// an amendment may carry a negative amount; a good record breaks nothing
	o, p := aisOrg(), samplePeople()[0]
	o.SubmissionType = "A"
	p.Allowances = n(-5)
	if f := payroll.ValidateAIS(o, []payroll.AISPerson{p}); len(f) != 0 {
		t.Errorf("an amendment with a negative amount: %v", f)
	}
	if f := payroll.ValidateAIS(aisOrg(), samplePeople()); len(f) != 0 {
		t.Errorf("the sample's own employees: %v", f)
	}
}

// A year with more people than a payload holds is sent in chunks, in order, of at most 2,000 (3,000 people: 2,000 then 1,000, the specification's own example).
// req: FR-PAY-10
func TestAYearIsSentInChunksOfAtMost2000(t *testing.T) {
	people := make([]int, 3000)
	c := payroll.Chunks(people, 5000)
	if len(c) != 2 || len(c[0]) != 2000 || len(c[1]) != 1000 {
		t.Fatalf("the chunks are %d, %d", len(c[0]), len(c[1]))
	}
	if len(payroll.Chunks(people, 700)) != 5 {
		t.Error("700 a chunk is 5 chunks of 3,000")
	}
}

// The stub answers as IRAS's samples do: a good payload is acknowledged (and a validation takes nothing), a payload whose totals disagree comes back with 30, "Arguments
// error" and each field to correct, and nothing is accepted without an acknowledgement code.
// req: NM-PAY-14
func TestTheStubAnswersAsIRASsSamplesDo(t *testing.T) {
	stub := &payroll.AISStub{}
	good, _ := payroll.AISPayload(aisOrg(), samplePeople(), false, false)
	r, err := stub.Submit(context.Background(), good)
	if err != nil || r.ReturnCode != "10" || !r.Accepted() || !strings.HasPrefix(r.Data.AcknowledgementCode, "AIS-") || len(r.Info.FieldInfo) != 0 {
		t.Fatalf("a good payload: %+v, %v", r, err)
	}
	check, _ := payroll.AISPayload(aisOrg(), samplePeople(), true, false)
	if r, _ := stub.Submit(context.Background(), check); r.ReturnCode != "10" || r.Accepted() || r.Data.AcknowledgementCode != "" {
		t.Errorf("a validation takes nothing: %+v", r)
	}
	// the totals disagree: the payload is edited after it is made, as a defect would
	var m map[string]any
	_ = json.Unmarshal(good, &m)
	m["orgAndSubmissionInfo"].(map[string]any)["amountTotalIncomeOfIR8A"] = 1
	bad, _ := json.Marshal(m)
	r, _ = stub.Submit(context.Background(), bad)
	if r.ReturnCode != "30" || r.Info.MessageCode != "850301" || r.Info.Message != "Arguments error" || len(r.Info.FieldInfo) != 1 || r.Info.FieldInfo[0].Field != "AmountTotalIncomeOfIR8A" || r.Accepted() {
		t.Errorf("a payload whose total disagrees: %+v", r)
	}
	if !strings.Contains(r.Describe(), "OrgAndSubmissionInfo AmountTotalIncomeOfIR8A") {
		t.Errorf("the answer in words is %q", r.Describe())
	}
	if r, _ := stub.Submit(context.Background(), []byte("not json")); r.ReturnCode != "30" || r.Info.MessageCode != "850302" {
		t.Errorf("a payload that is not JSON: %+v", r)
	}
}
