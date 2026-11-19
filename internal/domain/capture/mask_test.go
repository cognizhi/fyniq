package capture

import (
	"strings"
	"testing"
)

// Planted bank account numbers, IBANs and NRIC/FIN numbers in a supplier's invoice never survive the masker, while what a bill needs (the UEN, the GST number, the
// number, the date, the amounts) does: AI-02 and AI-20 (the owner's decision of 20 Nov 2026: mask before the model reads it).
// req: AI-02
// req: FR-DOC-09
// asvs: V14.2.1
func TestTheMaskerHidesBankAndIdentityNumbersAndKeepsWhatABillNeeds(t *testing.T) {
	invoice := strings.Join([]string{
		"Tax Invoice AU-1182",
		"Acme Software Pte Ltd, UEN 201912345K, GST Reg M2-8503432-K",
		"Date 2026-10-14  Due 2026-11-13",
		"Confluence, October            100.00   9.00   109.00",
		"Total SGD 1,090.00  GST 90.00",
		"Pay to DBS Bank Account No: 012-345678-9",
		"A/C 0123456789 SWIFT DBSSSGSG",
		"IBAN GB82 WEST 1234 5698 7654 32",
		"PayNow UEN 201912345K",
		"Contact: Ah Kow, S1234567D (director)",
		"Reference 9876543210123",
		"Phone +65 6123 4567",
	}, "\n")
	got, n := MaskText(invoice)
	for _, leaked := range []string{"012-345678-9", "0123456789", "GB82 WEST", "5698 7654 32", "S1234567D", "9876543210123", "6123 4567"} {
		if strings.Contains(got, leaked) {
			t.Errorf("%q survived the masker:\n%s", leaked, got)
		}
	}
	for _, kept := range []string{"AU-1182", "201912345K", "M2-8503432-K", "2026-10-14", "2026-11-13", "100.00", "109.00", "1,090.00", "90.00", "Total SGD", "Confluence, October"} {
		if !strings.Contains(got, kept) {
			t.Errorf("%q was masked, and a bill needs it:\n%s", kept, got)
		}
	}
	if n < 7 || !strings.Contains(got, NRICMask) || !strings.Contains(got, BankMask) {
		t.Errorf("masked %d things: %s", n, got)
	}
	// A bank line is masked down to four digits; an NRIC with a wrong check letter is not an NRIC and is left (it is an invoice number's shape).
	if g, _ := MaskText("Account number 1234"); strings.Contains(g, "1234") {
		t.Errorf("a short account number after a bank word: %q", g)
	}
	if g, _ := MaskText("Order S1234567A"); !strings.Contains(g, "S1234567A") {
		t.Errorf("a number of an NRIC's shape with a wrong check letter was masked: %q", g)
	}
	// Nothing to mask is untouched.
	if g, n := MaskText("Total 12.50"); g != "Total 12.50" || n != 0 {
		t.Errorf("plain text changed: %q %d", g, n)
	}
}
