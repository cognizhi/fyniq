package payroll

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// IR8A and the Auto-Inclusion Scheme (PRD-002 NM-PAY-14; PRD-001 FR-PAY-10; p2-payroll-outputs design D2). Source: IRAS, "API Services Interface Specifications:
// Auto-Inclusion Scheme (AIS) for Employment Income, Submission of Employment Income Records 2.0 (AIS-API 2.0)", version 2.6 of 11 Sep 2026
// (file.go.gov.sg/aisapi2-specs.pdf) and its "List of Data Items and Validation Rules", version 2.5 (go.gov.sg/aisapi2-dataitems), retrieved 20 Nov 2026. The payload
// has the object names, the property names and the types of the specification's sample request ("IR8A only", section 3.7.2), whose first two employees the tests
// rebuild. Appendix 8A (benefits in kind) and 8B (the share schemes) are written as the empty objects the specification recommends when none applies: Nemura holds
// no data for them, so a person who needs one is refused (AISPerson.NeedsAppendix), never sent without it.

// MaxAISRecords is the most IR8A records in a payload (item A11); a bigger year is sent in several payloads.
const MaxAISRecords = 2000

// AISOrg is the organisation's block of a submission (data items A1 to A14, but the totals, which are counted).
type AISOrg struct {
	CurrentYear      int    // the year today falls in: the income year may be four back, this one or one ahead (A3)
	IncomeYear       int    // A3
	EmployerTaxRef   string // A4: the employer's UEN
	AuthorisedPerson string // A5
	ContactNumber    string // A6: digits only
	Email            string // A7
	SubmissionType   string // A8: O original, A amendment, R revision
	SoftwareCompany  string // A9
	SoftwareName     string // A10
}

// AISPerson is an employee's particulars and IR8A. Amounts are whole dollars (Number(9)): nil is blank, which the payload writes as null.
type AISPerson struct {
	IDType, IDNumber, Name, DateOfBirth, Sex string // B2 (1 NRIC, 2 FIN, 4 WP, 11 MIC, 12 passport), B3, B4, B5 (YYYY-MM-DD), B6 (M or F)
	AddressType                              string // B7: L, F or empty
	Address1, Address2, Address3, PostalCode string // B8, B9
	Country                                  string // B10
	Designation, BankCode                    string // B11, B12
	Commencement, Cessation                  string // B13, B14 (YYYY-MM-DD) or empty

	From, To string // C1, C2 (YYYY-MM-DD)

	Salary, Bonus, DirectorFees, Allowances, Commission, Gratuity, CompensationForLoss, Pension, OverseasFund, ExcessCPF, ShareOptions101b, Gains101g, BenefitsInKind *int64
	BonusPaidOn, DirectorFeesApprovedOn                                                                                                                               string
	Section45                                                                                                                                                         string
	CompensationIndicator, PensionFundName                                                                                                                            string
	Exempt                                                                                                                                                            *int64
	ExemptIndicator, TaxBorneIndicator                                                                                                                                string
	TaxBorneByEmployer, FixedTaxBorneByEmployee                                                                                                                       *int64
	EmployeeCPF, Donations, MosqueFund, LifeInsurance                                                                                                                 *int64
	// NeedsAppendix marks a person with benefits in kind or a share scheme, whose Appendix 8A or 8B Nemura cannot yet write.
	NeedsAppendix bool
}

// Finding is a rule of IRAS's data items that the payload breaks: the section and field in IRAS's own names, the record it is on (0 for the organisation's block)
// and what to correct, as IRAS's own answer names them.
type Finding struct {
	Section, Field string
	Record         int
	Rule, Message  string
}

func (f Finding) String() string {
	at := ""
	if f.Record > 0 {
		at = fmt.Sprintf(" record %d", f.Record)
	}
	return fmt.Sprintf("%s %s%s (%s): %s", f.Section, f.Field, at, f.Rule, f.Message)
}

var (
	taxRef    = regexp.MustCompile(`^([0-9]{8}[A-Z]|[0-9]{9}[A-Z]|[TS][0-9]{2}[A-Z]{2}[0-9]{4}[A-Z]|A[0-9]{7}[A-Z])$`)
	nric      = regexp.MustCompile(`^[ST][0-9]{7}[A-Z]$`)
	fin       = regexp.MustCompile(`^[FGM][0-9]{7}[A-Z]$`)
	emailForm = regexp.MustCompile(`^\w+([-+.']\w+)*@\w+([-.]\w+)*\.\w+([-.]\w+)*$`)
	phoneForm = regexp.MustCompile(`^[0-9]{1,12}$`)
	post6     = regexp.MustCompile(`^[0-9]{6}$`)
	noAddress = map[string]bool{"NIL": true, "N.A.": true, "NA": true, "NO ADDRESS": true, "NOT APPLICABLE": true, "NON": true, "NO": true, "NOT AVAILABLE": true}
)

func sum(xs ...*int64) int64 {
	var t int64
	for _, x := range xs {
		if x != nil {
			t += *x
		}
	}
	return t
}

func has(x *int64) bool { return x != nil }

// OthersIncome is item C21: the sum of C9, C10, C11, C14, C16, C17, C18 and C20.
func (p AISPerson) OthersIncome() int64 {
	return sum(p.Allowances, p.Commission, p.Gratuity, p.Pension, p.OverseasFund, p.ExcessCPF, p.ShareOptions101b, p.BenefitsInKind)
}

// TotalIncome is item C22: C3, C4, C6 and C21 (0 when they are all blank).
func (p AISPerson) TotalIncome() int64 {
	return sum(p.Salary, p.Bonus, p.DirectorFees) + p.OthersIncome()
}

func parseDay(s string) (time.Time, bool) {
	t, err := time.Parse(time.DateOnly, s)
	return t, err == nil
}

// ValidateAIS lists the data-item rules of IRAS's specification the payload breaks, so a person corrects them before anything is sent. IRAS's own validation, with
// validateOnly, has the last word and is [live-verify].
func ValidateAIS(o AISOrg, people []AISPerson) []Finding {
	var out []Finding
	org := func(field, rule, msg string) { out = append(out, Finding{"OrgAndSubmissionInfo", field, 0, rule, msg}) }
	if o.IncomeYear < o.CurrentYear-4 || o.IncomeYear > o.CurrentYear+1 {
		org("incomeYear", "A3", "the income year is one of four back years, the current year or one advance year")
	}
	if !taxRef.MatchString(o.EmployerTaxRef) {
		org("employerTaxRefNumber", "A4", "the employer's tax reference is its UEN, in IRAS's forms (such as 202312345K or T08LL1234A)")
	}
	if n := len(strings.TrimSpace(o.AuthorisedPerson)); n == 0 || n > 100 {
		org("authorisedPersonName", "A5", "the authorised person's name is needed, at most 100 characters")
	}
	if !phoneForm.MatchString(o.ContactNumber) {
		org("contactNumber", "A6", "the contact number is digits only, at most 12")
	}
	if len(o.Email) > 320 || !emailForm.MatchString(o.Email) {
		org("emailAddress", "A7", "the email address is not in the form IRAS takes")
	}
	if o.SubmissionType != "O" && o.SubmissionType != "A" && o.SubmissionType != "R" {
		org("submissionType", "A8", "O (original), A (amendment) or R (revision)")
	}
	if o.SoftwareCompany == "" || len(o.SoftwareCompany) > 60 || o.SoftwareName == "" || len(o.SoftwareName) > 60 {
		org("softwareCompanyName", "A9", "the software's company and name are needed, at most 60 characters each")
	}
	if len(people) == 0 || len(people) > MaxAISRecords {
		org("totalNumberIR8A", "A11", fmt.Sprintf("a payload holds 1 to %d IR8A records", MaxAISRecords))
	}
	yearStart, yearEnd := fmt.Sprintf("%04d-01-01", o.IncomeYear), fmt.Sprintf("%04d-12-31", o.IncomeYear)
	for i, p := range people {
		n := i + 1
		add := func(sec, field, rule, msg string) { out = append(out, Finding{sec, field, n, rule, msg}) }
		sec := "EmployeePersonalParticulars"
		switch p.IDType {
		case "1":
			if !nric.MatchString(p.IDNumber) {
				add(sec, "idNumber", "B3", "an NRIC is S or T, seven digits and a letter")
			}
		case "2":
			if !fin.MatchString(p.IDNumber) {
				add(sec, "idNumber", "B3", "a FIN is F, G or M, seven digits and a letter")
			}
		case "4", "11", "12":
			if p.IDNumber == "" || len(p.IDNumber) > 25 {
				add(sec, "idNumber", "B3", "the identification number is needed, at most 25 characters")
			}
			if p.IDType != "4" && p.Designation == "" {
				add(sec, "designation", "B11", "a non-resident director or seaman's designation is needed")
			}
		default:
			add(sec, "idType", "B2", "1 (NRIC), 2 (FIN), 4 (work permit), 11 (Malaysian IC) or 12 (passport)")
		}
		if n := len(strings.TrimSpace(p.Name)); n == 0 || n > 100 {
			add(sec, "name", "B4", "the employee's name is needed, at most 100 characters")
		}
		if _, ok := parseDay(p.DateOfBirth); !ok {
			add(sec, "dateOfBirth", "B5", "the date of birth is YYYY-MM-DD")
		}
		if p.Sex != "M" && p.Sex != "F" {
			add(sec, "sex", "B6", "M or F")
		}
		switch p.AddressType {
		case "":
			if p.Address1 != "" || p.PostalCode != "" {
				add(sec, "addressType", "B7", "an address needs its type, L (local) or F (foreign)")
			}
		case "L", "F":
			if p.Address1 == "" || noAddress[strings.ToUpper(strings.TrimSpace(p.Address1))] {
				add(sec, "addressLine1", "B8", "the address line 1 is a real address, not NIL or N.A.")
			}
			if p.AddressType == "L" && (!post6.MatchString(p.PostalCode) || p.PostalCode == "000000" || p.PostalCode == "999999" || p.Country != "") {
				add(sec, "postalCode", "B9", "a local address has a six-digit post code and no country")
			}
			if p.AddressType == "F" && (p.PostalCode != "" || p.Country == "") {
				add(sec, "countryCodeForForeignAddress", "B10", "a foreign address has a country code and no post code")
			}
		default:
			add(sec, "addressType", "B7", "L or F")
		}
		if p.Address2 != "" && p.Address1 == "" || p.Address3 != "" && p.Address2 == "" {
			add(sec, "addressLine2", "B8", "an address line is blank only after the lines before it")
		}
		if p.Commencement != "" {
			if d, ok := parseDay(p.Commencement); !ok || d.Year() > o.IncomeYear {
				add(sec, "commencementDate", "B13", "the commencement date is YYYY-MM-DD and no later than the income year")
			}
		}
		if p.Cessation != "" {
			if !strings.HasPrefix(p.Cessation, fmt.Sprintf("%04d-", o.IncomeYear)) {
				add(sec, "cessationDate", "B14", "the cessation date falls within the income year")
			}
			if p.Commencement != "" && p.Cessation < p.Commencement {
				add(sec, "cessationDate", "B14", "the cessation date is not before the commencement date")
			}
		}
		// the IR8A
		ir := "IR8A"
		wantFrom := yearStart
		if strings.HasPrefix(p.Commencement, fmt.Sprintf("%04d-", o.IncomeYear)) {
			wantFrom = p.Commencement
		}
		if p.From != wantFrom {
			add(ir, "periodPaymentFromDate", "C1", "the period starts "+yearStart+", or on the commencement date when that is within the income year")
		}
		wantTo := yearEnd
		if p.Cessation != "" {
			wantTo = p.Cessation
		}
		if p.To != wantTo {
			add(ir, "periodPaymentToDate", "C2", "the period ends "+yearEnd+", or on the cessation date when that is within the income year")
		}
		if p.From > p.To {
			add(ir, "periodPaymentFromDate", "C1", "the period starts before it ends")
		}
		amounts := map[string]*int64{"salary": p.Salary, "bonus": p.Bonus, "directorFees": p.DirectorFees, "allowances": p.Allowances, "grossCommission": p.Commission,
			"gratuityNoticePayExGratiaPayment": p.Gratuity, "compensationForLossOfOffice": p.CompensationForLoss, "pensionRetirementBenefitsAccruedFrom1993": p.Pension,
			"employerContributionToPensionOrProvidentFundOutsideSg": p.OverseasFund, "excessOrVoluntaryCpfContributionByEmployer": p.ExcessCPF,
			"gainsAndProfitsFromSOGS101b": p.ShareOptions101b, "gainsAndProfitsUnderS101g": p.Gains101g, "valueOfBenefitsInKind": p.BenefitsInKind,
			"exemptIncomeOrIncomeSubjectToTaxRemission": p.Exempt, "cpfOrDesignatedFund": p.EmployeeCPF, "donationsDeductedThroughSalaries": p.Donations,
			"contributionsToMosqueBuildingFund": p.MosqueFund, "lifeInsurancePremiumThroughSalaries": p.LifeInsurance}
		for field, v := range amounts {
			if v != nil && (*v > 999999999 || (o.SubmissionType != "A" && *v < 0)) {
				add(ir, field, "C", "an amount is whole dollars of at most nine digits, and positive unless it amends")
			}
		}
		if p.BonusPaidOn != "" && (!has(p.Bonus) || !strings.HasPrefix(p.BonusPaidOn, fmt.Sprintf("%04d-", o.IncomeYear))) {
			add(ir, "nonContractualBonusPaidDate", "C5", "the day a non-contractual bonus was paid is in the income year, with the bonus")
		}
		if (p.DirectorFees != nil) != (p.DirectorFeesApprovedOn != "") {
			add(ir, "directorsFeesApprovalDate", "C7", "director's fees and the day they were approved go together")
		}
		if p.IDType != "1" && sum(p.DirectorFees) > 0 && p.Section45 == "" {
			add(ir, "section45Indicator", "C8", "Y or N is needed for a non-resident director's fees")
		}
		if (p.CompensationForLoss != nil) != (p.CompensationIndicator == "Y") {
			add(ir, "compensationForLossOfOfficeIndicator", "C13", "the amount for loss of office and the indicator Y go together")
		}
		if (p.Exempt != nil) != (p.ExemptIndicator != "") {
			add(ir, "exemptOrRemissionIncomeIndicator", "C24", "exempt income and its indicator go together")
		}
		switch p.TaxBorneIndicator {
		case "", "F", "P", "H", "N":
		default:
			add(ir, "incomeTaxBorneByEmployerIndicator", "C25", "F, P, H or N")
		}
		if (p.TaxBorneIndicator == "P") != has(p.TaxBorneByEmployer) || (p.TaxBorneIndicator == "H") != has(p.FixedTaxBorneByEmployee) {
			add(ir, "amountOfIncomeForTaxBorneByEmployer", "C26", "the amount for which the employer bears the tax goes with P, and the fixed amount the employee bears with H")
		}
		if p.NeedsAppendix || has(p.BenefitsInKind) || has(p.ShareOptions101b) {
			add("Appendix", "appendix8A", "D/E", "this person has benefits in kind or a share scheme, whose Appendix 8A or 8B Nemura does not yet write: prepare their record by hand")
		}
	}
	// the organisation's total of every person's total income (A12) is counted, never typed: nothing to break here.
	return out
}

// null is a blank amount in the payload.
func amt(x *int64) any {
	if x == nil {
		return nil
	}
	return *x
}

type orgJSON struct {
	ValidateOnly    bool        `json:"validateOnly"`
	BypassWarning   bool        `json:"bypassWarning"`
	IncomeYear      int         `json:"incomeYear"`
	EmployerTaxRef  string      `json:"employerTaxRefNumber"`
	Authorised      string      `json:"authorisedPersonName"`
	ContactNumber   json.Number `json:"contactNumber"`
	Email           string      `json:"emailAddress"`
	SubmissionType  string      `json:"submissionType"`
	SoftwareCompany string      `json:"softwareCompanyName"`
	SoftwareName    string      `json:"softwareName"`
	TotalIR8A       int         `json:"totalNumberIR8A"`
	TotalIncome     int64       `json:"amountTotalIncomeOfIR8A"`
	TotalApp8A      int         `json:"totalNumberOfApp8A"`
	TotalApp8B      int         `json:"totalNumberOfApp8B"`
}

type ir8aJSON struct {
	From              string `json:"periodPaymentFromDate"`
	To                string `json:"periodPaymentToDate"`
	Salary            any    `json:"salary"`
	Bonus             any    `json:"bonus"`
	BonusPaidOn       string `json:"nonContractualBonusPaidDate"`
	DirectorFees      any    `json:"directorFees"`
	FeesApprovedOn    string `json:"directorsFeesApprovalDate"`
	Section45         string `json:"section45Indicator"`
	Allowances        any    `json:"allowances"`
	Commission        any    `json:"grossCommission"`
	Gratuity          any    `json:"gratuityNoticePayExGratiaPayment"`
	CompLoss          any    `json:"compensationForLossOfOffice"`
	CompLossIndicator string `json:"compensationForLossOfOfficeIndicator"`
	Pension           any    `json:"pensionRetirementBenefitsAccruedFrom1993"`
	PensionFundName   string `json:"designatedPensionOrProvidentFundName"`
	OverseasFund      any    `json:"employerContributionToPensionOrProvidentFundOutsideSg"`
	ExcessCPF         any    `json:"excessOrVoluntaryCpfContributionByEmployer"`
	Options101b       any    `json:"gainsAndProfitsFromSOGS101b"`
	Gains101g         any    `json:"gainsAndProfitsUnderS101g"`
	Benefits          any    `json:"valueOfBenefitsInKind"`
	Others            any    `json:"othersIncome"`
	Total             int64  `json:"totalIncome"`
	Exempt            any    `json:"exemptIncomeOrIncomeSubjectToTaxRemission"`
	ExemptIndicator   string `json:"exemptOrRemissionIncomeIndicator"`
	TaxBorneIndicator string `json:"incomeTaxBorneByEmployerIndicator"`
	TaxBorneEmployer  any    `json:"amountOfIncomeForTaxBorneByEmployer"`
	FixedTaxEmployee  any    `json:"fixedAmountOfIncomeForTaxBorneByEmployee"`
	EmployeeCPF       any    `json:"cpfOrDesignatedFund"`
	Donations         any    `json:"donationsDeductedThroughSalaries"`
	Mosque            any    `json:"contributionsToMosqueBuildingFund"`
	LifeInsurance     any    `json:"lifeInsurancePremiumThroughSalaries"`
}

type personJSON struct {
	RecordID    int      `json:"recordId"`
	IDType      string   `json:"idType"`
	IDNumber    string   `json:"idNumber"`
	Name        string   `json:"name"`
	DOB         string   `json:"dateOfBirth"`
	Sex         string   `json:"sex"`
	AddressType string   `json:"addressType"`
	Line1       string   `json:"addressLine1"`
	Line2       string   `json:"addressLine2"`
	Line3       string   `json:"addressLine3"`
	PostalCode  string   `json:"postalCode"`
	Country     string   `json:"countryCodeForForeignAddress"`
	Designation string   `json:"designation"`
	BankCode    string   `json:"bankCode"`
	Commenced   string   `json:"commencementDate"`
	Ceased      string   `json:"cessationDate"`
	IR8A        ir8aJSON `json:"ir8a"`
	App8A       struct{} `json:"appendix8A"`
	App8B       struct{} `json:"appendix8B"`
}

// AISPayload is the JSON request of one submission: validateOnly asks IRAS to validate and not to submit, and bypassWarning to submit past warnings. It is refused with the
// findings while ValidateAIS finds any, and never holds more than MaxAISRecords people (the caller chunks a bigger year).
func AISPayload(o AISOrg, people []AISPerson, validateOnly, bypassWarning bool) ([]byte, []Finding) {
	if f := ValidateAIS(o, people); len(f) > 0 {
		return nil, f
	}
	var total int64
	req := struct {
		Org    orgJSON      `json:"orgAndSubmissionInfo"`
		People []personJSON `json:"employeePersonalParticulars"`
	}{}
	for i, p := range people {
		total += p.TotalIncome()
		req.People = append(req.People, personJSON{RecordID: i + 1, IDType: p.IDType, IDNumber: p.IDNumber, Name: p.Name, DOB: p.DateOfBirth, Sex: p.Sex, AddressType: p.AddressType,
			Line1: p.Address1, Line2: p.Address2, Line3: p.Address3, PostalCode: p.PostalCode, Country: p.Country, Designation: p.Designation, BankCode: p.BankCode,
			Commenced: p.Commencement, Ceased: p.Cessation,
			IR8A: ir8aJSON{From: p.From, To: p.To, Salary: amt(p.Salary), Bonus: amt(p.Bonus), BonusPaidOn: p.BonusPaidOn, DirectorFees: amt(p.DirectorFees),
				FeesApprovedOn: p.DirectorFeesApprovedOn, Section45: p.Section45, Allowances: amt(p.Allowances), Commission: amt(p.Commission), Gratuity: amt(p.Gratuity),
				CompLoss: amt(p.CompensationForLoss), CompLossIndicator: p.CompensationIndicator, Pension: amt(p.Pension), PensionFundName: p.PensionFundName,
				OverseasFund: amt(p.OverseasFund), ExcessCPF: amt(p.ExcessCPF), Options101b: amt(p.ShareOptions101b), Gains101g: amt(p.Gains101g), Benefits: amt(p.BenefitsInKind),
				Others: othersOrNil(p), Total: p.TotalIncome(), Exempt: amt(p.Exempt), ExemptIndicator: p.ExemptIndicator, TaxBorneIndicator: p.TaxBorneIndicator,
				TaxBorneEmployer: amt(p.TaxBorneByEmployer), FixedTaxEmployee: amt(p.FixedTaxBorneByEmployee), EmployeeCPF: amt(p.EmployeeCPF), Donations: amt(p.Donations),
				Mosque: amt(p.MosqueFund), LifeInsurance: amt(p.LifeInsurance)}})
	}
	req.Org = orgJSON{ValidateOnly: validateOnly, BypassWarning: bypassWarning, IncomeYear: o.IncomeYear, EmployerTaxRef: o.EmployerTaxRef, Authorised: o.AuthorisedPerson,
		ContactNumber: json.Number(o.ContactNumber), Email: o.Email, SubmissionType: o.SubmissionType, SoftwareCompany: o.SoftwareCompany, SoftwareName: o.SoftwareName,
		TotalIR8A: len(people), TotalIncome: total}
	b, err := json.MarshalIndent(req, "", "    ")
	if err != nil {
		return nil, []Finding{{Section: "payload", Rule: "json", Message: err.Error()}}
	}
	return b, nil
}

func othersOrNil(p AISPerson) any {
	if o := p.OthersIncome(); o != 0 || p.Allowances != nil || p.Commission != nil || p.Gratuity != nil || p.Pension != nil || p.OverseasFund != nil || p.ExcessCPF != nil ||
		p.ShareOptions101b != nil || p.BenefitsInKind != nil {
		return o
	}
	return nil
}

// Chunks splits a year's people into payloads of at most size records (item A11's limit is MaxAISRecords), in order, so a failed chunk is corrected and sent
// again without the ones that passed (section 3.3).
func Chunks[T any](people []T, size int) [][]T {
	if size <= 0 || size > MaxAISRecords {
		size = MaxAISRecords
	}
	var out [][]T
	for len(people) > 0 {
		n := min(size, len(people))
		out = append(out, people[:n])
		people = people[n:]
	}
	return out
}
