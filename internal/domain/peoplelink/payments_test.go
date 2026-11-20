package peoplelink

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func paymentsPayload(edit func(*PaymentsReady)) []byte {
	p := PaymentsReady{RunID: uuid.New(), RunVersion: 1, PayDate: "2026-10-31", Preparer: Party{"https://a", "1"}, Approver: Party{"https://a", "2"}, Lines: []PaymentLine{{LineID: uuid.New(), PersonID: uuid.New(),
		PayeeName: "Ana Lim", BankBIC: "DBSSSGSG", AccountName: "Ana Lim", AccountNumber: "0123456789", Amount: "100.00", BankDetailID: uuid.New(), BankDetailVersion: 1, BankDetailDigest: strings.Repeat("c", 64)}}}
	if edit != nil {
		edit(&p)
	}
	b, _ := json.Marshal(p)
	return b
}

// The payment lines of a run are read strictly: a field Appendix E does not list, an amount that is not dollars and cents, a repeated line, a bank that is not a BIC and a digest that is not
// a SHA-256 are each refused with a code and a message that names no payee, account or amount (FR-PPL-14, FR-PPL-17; PRD-002 NM-INT-05, NM-INT-09).
// req: FR-PPL-14
func TestPaymentLinesAreReadStrictlyAndRefusalsNameNoPerson(t *testing.T) {
	if _, err := ParsePayments(paymentsPayload(nil)); err != nil {
		t.Fatalf("good lines: %v", err)
	}
	for name, edit := range map[string]func(*PaymentsReady){
		"an amount of one decimal":  func(p *PaymentsReady) { p.Lines[0].Amount = "100.5" },
		"an amount of zero":         func(p *PaymentsReady) { p.Lines[0].Amount = "0.00" },
		"a negative amount":         func(p *PaymentsReady) { p.Lines[0].Amount = "-5.00" },
		"a line twice":              func(p *PaymentsReady) { p.Lines = append(p.Lines, p.Lines[0]) },
		"no lines":                  func(p *PaymentsReady) { p.Lines = nil },
		"a bank that is not a BIC":  func(p *PaymentsReady) { p.Lines[0].BankBIC = "DBS" },
		"a digest of the wrong len": func(p *PaymentsReady) { p.Lines[0].BankDetailDigest = "abc" },
		"a bank detail version 0":   func(p *PaymentsReady) { p.Lines[0].BankDetailVersion = 0 },
		"no approver":               func(p *PaymentsReady) { p.Approver = Party{} },
		"a date that is not":        func(p *PaymentsReady) { p.PayDate = "31/10/2026" },
	} {
		_, err := ParsePayments(paymentsPayload(edit))
		r, ok := err.(*Refusal)
		if !ok || r.Code != CodePayLines {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if strings.Contains(r.Message, "Ana") || strings.Contains(r.Message, "0123456789") || strings.Contains(r.Message, "100.") {
			t.Errorf("%s: the refusal names a person, an account or an amount: %q", name, r.Message)
		}
	}
	extra := strings.Replace(string(paymentsPayload(nil)), `"lines":[{`, `"lines":[{"nric":"S1234567D",`, 1)
	if _, err := ParsePayments([]byte(extra)); err == nil || err.(*Refusal).Code != CodeMalformed {
		t.Errorf("a field Appendix E does not list: %v", err)
	}
}
