package contact

import (
	"slices"
	"testing"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// req: FR-CT-01
func TestAContactsRules(t *testing.T) {
	thirty := 30
	ok := storage.ContactDetails{LegalName: "Northwind Analytics Pte. Ltd.", Customer: true, UENOrTaxID: "202312345K", PeppolID: "0195:SGUEN202312345K",
		BillingEmail: "accounts@northwind.example", Country: "SG", Currency: "SGD", PaymentTermsDays: &thirty, DefaultGSTCode: "SR"}
	if p := Check(ok); p != nil {
		t.Fatalf("a valid customer: %v", p)
	}
	minus := -1
	bad := storage.ContactDetails{LegalName: " ", BillingEmail: "not an address", Country: "Singapore", Currency: "XYZ",
		PaymentTermsDays: &minus, DefaultGSTCode: "sr", PeppolID: "SGUEN202312345K", Phone: "  "}
	var got []string
	for _, x := range Check(bad) {
		got = append(got, x.Code+" "+x.Field)
	}
	slices.Sort(got)
	want := []string{"CT-01 legal_name", "CT-02 customer", "CT-03 billing_email", "CT-04 country", "CT-05 currency", "CT-06 payment_terms_days",
		"CT-07 default_gst_code", "CT-08 peppol_id", "CT-09 phone"}
	if !slices.Equal(got, want) {
		t.Errorf("problems:\n got %v\nwant %v", got, want)
	}
	if p := CheckCategory(storage.Account{Code: "6000", Group: true, Active: true}); len(p) != 1 || p[0].Code != CodeCategory {
		t.Errorf("a group as the default category: %v", p)
	}
}

// A supplier's early-payment discount terms: a percentage above 0 and below 100 and 1 to 365 days, both or neither.
// req: FR-AP-13
func TestDiscountTerms(t *testing.T) {
	days := func(n int) *int { return &n }
	vendor := storage.ContactDetails{LegalName: "Atlassian Pty Ltd", Vendor: true}
	for name, c := range map[string]struct {
		pct  string
		days *int
		ok   bool
	}{
		"2% in 10 days":   {"2", days(10), true},
		"2.5% in 30 days": {"2.5", days(30), true},
		"0.5%":            {"0.5", days(7), true},
		"none":            {"", nil, true},
		"a percent alone": {"2", nil, false},
		"days alone":      {"", days(10), false},
		"0%":              {"0", days(10), false},
		"100%":            {"100", days(10), false},
		"three decimals":  {"2.125", days(10), false},
		"a sign":          {"2%", days(10), false},
		"0 days":          {"2", days(0), false},
		"a year and more": {"2", days(366), false},
	} {
		d := vendor
		d.DiscountPercent, d.DiscountDays = c.pct, c.days
		p := Check(d)
		if (len(p) == 0) != c.ok || (!c.ok && p[0].Code != CodeDiscount) {
			t.Errorf("%s: %v", name, p)
		}
	}
}
