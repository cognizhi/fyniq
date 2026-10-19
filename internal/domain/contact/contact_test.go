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
