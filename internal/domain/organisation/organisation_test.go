package organisation

import (
	"slices"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func date(s string) *time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return &t
}

func profile() storage.OrganisationProfile {
	return storage.OrganisationProfile{LegalName: "Northwind Analytics Pte. Ltd.", UEN: "202312345K",
		RegisteredAddress: "1 Raffles Place, #20-01, Singapore 048616", Email: "accounts@northwind.example", Phone: "+65 6123 4567",
		BankName: "DBS Bank", BankAccountName: "Northwind Analytics Pte. Ltd.", BankAccountNumber: "012-345678-9", PayNowUEN: "202312345K",
		DocumentFooter: "Thank you for your business.", FYStartMonth: 4, BooksStart: date("2026-03-15"), FirstYearEnd: date("2027-03-31"),
		TimeZone: "Asia/Singapore"}
}

func codes(p Problems) []string {
	var out []string
	for _, x := range p {
		out = append(out, x.Code+" "+x.Field)
	}
	slices.Sort(out)
	return out
}

// req: FR-ORG-01
func TestAProfileWithEveryDetailIsAccepted(t *testing.T) {
	if p := Check(profile()); p != nil {
		t.Fatalf("a complete profile: %v", p)
	}
	empty := storage.OrganisationProfile{FYStartMonth: 1, TimeZone: "Asia/Singapore"} // a new organisation's
	if p := Check(empty); p != nil {
		t.Fatalf("an empty profile: %v", p)
	}
}

// The financial year may start in any month, and the first one may be shorter or longer than a year.
// req: FR-ORG-02
func TestTheFinancialYearStartsInAnyMonthAndTheFirstMayBeShortOrLong(t *testing.T) {
	for month := 1; month <= 12; month++ {
		p := profile()
		p.FYStartMonth = month
		end := time.Date(2027, time.Month(month), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
		p.FirstYearEnd = &end
		if got := Check(p); got != nil {
			t.Errorf("a year starting in %s: %v", time.Month(month), got)
		}
	}
	for name, end := range map[string]string{"short (17 days)": "2026-03-31", "long (12½ months)": "2027-03-31", "18 months": "2027-09-30"} {
		p := profile()
		p.FYStartMonth = int(date(end).AddDate(0, 0, 1).Month())
		p.FirstYearEnd = date(end)
		if got := Check(p); got != nil {
			t.Errorf("a %s first year: %v", name, got)
		}
	}
}

// req: FR-ORG-01
// req: FR-ORG-02
func TestEveryBrokenRuleIsReportedAtOnce(t *testing.T) {
	p := profile()
	p.UEN, p.PayNowUEN, p.Email = "12345", "abc", "not an address"
	p.BankAccountNumber, p.TimeZone = "12-AB", "Mars/Olympus"
	p.LegalName = "   "
	p.FirstYearEnd = date("2027-03-30") // not the day before April
	got := codes(Check(p))
	want := []string{"ORG-01 legal_name", "ORG-02 paynow_uen", "ORG-02 uen", "ORG-03 email", "ORG-04 time_zone",
		"ORG-06 first_year_end", "ORG-07 bank_account_number"}
	if !slices.Equal(got, want) {
		t.Errorf("problems:\n got %v\nwant %v", got, want)
	}

	p = profile()
	p.FYStartMonth = 13
	if got := codes(Check(p)); !slices.Equal(got, []string{"ORG-05 fy_start_month"}) {
		t.Errorf("month 13: %v", got)
	}
	p = profile()
	p.FYStartMonth, p.FirstYearEnd = 3, date("2026-02-28") // the day before a March year, but before the books start
	if got := codes(Check(p)); !slices.Equal(got, []string{"ORG-06 first_year_end"}) {
		t.Errorf("a first year ending before the books start: %v", got)
	}
	p = profile()
	p.BooksStart = nil
	if got := codes(Check(p)); !slices.Equal(got, []string{"ORG-06 books_start"}) {
		t.Errorf("a first year end without a books start: %v", got)
	}
	p = profile()
	long := make([]rune, 201)
	for i := range long {
		long[i] = 'a'
	}
	p.LegalName = string(long)
	if got := codes(Check(p)); !slices.Equal(got, []string{"ORG-01 legal_name"}) {
		t.Errorf("a legal name of 201 characters: %v", got)
	}
}
