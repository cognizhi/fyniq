package aiexclusion_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/domain/aiexclusion"
)

// What never reaches a model (AI-02): identity numbers, full bank account numbers, secrets and authentication data, and
// home addresses go from any read, at any depth and under any name ending in theirs; pay goes unless AI payroll access
// is on for the person; the Owner's own fields go too; what is left keeps its shape.
// req: AI-02
// asvs: V14.2.1
func TestTheNeverSendFieldsNeverLeave(t *testing.T) {
	var v any
	if err := json.Unmarshal([]byte(`{
		"id": "0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b", "legal_name": "Ah Kow Tan", "individual": true, "address": "12 Jalan Kayu",
		"nric": "S1234567D", "director_nric": "T0123456G", "bank_account": {"number": "0123456789", "bank": "DBS"},
		"client_secret": "x", "api_key": "y", "recovery_codes": ["a", "b"], "residential_address": "12 Jalan Kayu",
		"monthly_salary": "8000.00", "memo": "October fees",
		"lines": [{"description": "Hosting", "notes": "see attached", "net": {"amount": "100.00", "currency": "SGD"}}],
		"supplier": {"legal_name": "Acme Pte Ltd", "individual": false, "address": "1 Raffles Place"}
	}`), &v); err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(aiexclusion.Apply(v, aiexclusion.Policy{Extra: []string{"notes"}}))
	got := string(out)
	for _, gone := range []string{"S1234567D", "T0123456G", "0123456789", "client_secret", "api_key", "recovery_codes", "residential_address",
		"12 Jalan Kayu", "monthly_salary", "8000.00", `"notes"`} {
		if strings.Contains(got, gone) {
			t.Errorf("%s reached the AI: %s", gone, got)
		}
	}
	for _, kept := range []string{"Ah Kow Tan", "October fees", "Hosting", "1 Raffles Place", `"amount":"100.00"`, "0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b"} {
		if !strings.Contains(got, kept) {
			t.Errorf("%s was taken out: %s", kept, got)
		}
	}
	paid, _ := json.Marshal(aiexclusion.Apply(v, aiexclusion.Policy{Payroll: true}))
	if !strings.Contains(string(paid), `"monthly_salary":"8000.00"`) || strings.Contains(string(paid), "S1234567D") {
		t.Errorf("with AI payroll access: %s", paid)
	}
	// The Owner adds fields; the never-send list stays whatever they add.
	if p := (aiexclusion.Policy{Payroll: true, Extra: []string{"memo"}}); strings.Contains(string(must(aiexclusion.Apply(v, p))), "October fees") ||
		strings.Contains(string(must(aiexclusion.Apply(v, p))), "api_key") {
		t.Error("the Owner's exclusion or the never-send list")
	}
}

// The output filter (AI-20): an NRIC or FIN with its check letter right, and a bank-number shape of 9 to 18 digits, are
// masked wherever they are in text, keeping their last characters; a wrong check letter, an amount, a date, an invoice
// number, a UEN, a phone number's eight digits and a UUID are left alone.
// req: AI-20
// asvs: V14.2.1
func TestTheOutputFilterMasksNRICAndBankNumbers(t *testing.T) {
	for in, want := range map[string]string{
		"Refund to S1234567D please":                "Refund to S****567D please",
		"FIN g1234567a is not his":                  "FIN g1234567a is not his",
		"FIN g1234567x is his":                      "FIN g****567x is his",
		"Paid into 0123456789 at DBS":               "Paid into ****6789 at DBS",
		"Account 012-345678-9.":                     "Account ****6789.",
		"Acct 1234 5678 9012 34 for the transfer":   "Acct ****1234 for the transfer",
		"Two: 123456789 and 987654321":              "Two: ****6789 and ****4321",
		"Total 1000000.00 on 2026-10-15":            "Total 1000000.00 on 2026-10-15",
		"INV-000123456 and PO 2026-00012":           "INV-000123456 and PO 2026-00012",
		"UEN 201912345K":                            "UEN 201912345K",
		"Call 91234567":                             "Call 91234567",
		"1,234,567,890.00":                          "1,234,567,890.00",
		"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b":      "0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b",
		"Card 4111111111111111 expires":             "Card ****1111 expires",
		"too long 1234567890123456789012 to be one": "too long 1234567890123456789012 to be one",
	} {
		if got := aiexclusion.Filter(in); got != want {
			t.Errorf("Filter(%q) = %q, want %q", in, got, want)
		}
	}
}

func must(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
