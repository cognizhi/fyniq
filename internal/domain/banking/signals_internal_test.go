package banking

import "testing"

// An incoming-funds alert is read from the plain phrases banks use: an amount in a known currency, money in, and a payer;
// money out, an unknown currency, too many decimals or no payer read as no alert, so the message is left for a person.
// The DBS IDEAL alert's own wording is [verify] until a redacted sample.
// req: FR-BNK-16
func TestAnIncomingFundsAlertIsReadFromItsText(t *testing.T) {
	for _, c := range []struct {
		text string
		want Alert
		ok   bool
	}{
		{"DBS IDEAL Alert: SGD 10,900.00 has been received in your account ending 1234 from ACME ROBOTICS on 26 Oct 2026. Ref: 2610-0001.",
			Alert{Amount: "10900.00", Currency: "SGD", Payer: "ACME ROBOTICS", Reference: "2610-0001"}, true},
		{"Incoming funds: S$10,900.00 from \"ACME ROBOTICS PTE LTD\" credited to Current Account.",
			Alert{Amount: "10900.00", Currency: "SGD", Payer: "ACME ROBOTICS PTE LTD"}, true},
		{"REF 2610 OCT 2026: USD 20 received from Wise Payments Ltd", Alert{Amount: "20", Currency: "USD", Payer: "Wise Payments Ltd", Reference: "2610"}, true},
		{"You have received 10,900.00 from ACME ROBOTICS", Alert{}, false},             // no currency
		{"SGD 10,900.00 has been debited from your account to ACME", Alert{}, false},   // money out
		{"SGD 10,900.005 has been received from ACME ROBOTICS", Alert{}, false},        // three decimals for SGD
		{"SGD 10,900.00 has been received in your account.", Alert{}, false},           // no payer
		{"Your statement for October is ready. SGD 1,000.00 received", Alert{}, false}, // no payer either
		{"XYZ 10.00 received from ACME ROBOTICS", Alert{}, false},                      // no such currency
	} {
		got, ok := ParseAlert(c.text)
		if ok != c.ok || got != c.want {
			t.Errorf("ParseAlert(%q) = %+v, %v; want %+v, %v", c.text, got, ok, c.want, c.ok)
		}
	}
}

// A bank's payer name names a customer when either contains the other, ignoring case, punctuation and the company
// suffixes banks drop; a name of fewer than four characters names nobody.
// req: FR-BNK-16
func TestAPayerNamesItsCustomer(t *testing.T) {
	for _, c := range []struct {
		payer, customer string
		same            bool
	}{
		{"ACME ROBOTICS", "Acme Robotics Pte. Ltd.", true},
		{"ACME ROBOTICS PTE", "Acme Robotics Pte. Ltd.", true},
		{"ACME ROBOTICS PTE LTD", "Acme Robotics Private Limited", true},
		{"ACME ROBOTICS", "Acme Analytics Pte Ltd", false},
		{"ACME", "Acme Robotics Pte. Ltd.", true}, // a shortened name still names it; the amount must match too
		{"AB", "AB Pte Ltd", false},
		{"", "Acme Robotics Pte. Ltd.", false},
	} {
		if got := samePayer(c.payer, c.customer); got != c.same {
			t.Errorf("samePayer(%q, %q) = %v; want %v", c.payer, c.customer, got, c.same)
		}
	}
}
