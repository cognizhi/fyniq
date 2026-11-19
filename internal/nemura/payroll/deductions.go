package payroll

import (
	"github.com/cockroachdb/apd/v3"
)

// Kinds of salary deduction MOM allows (FR-PAY-16).
const (
	DedAbsence       = "absence"
	DedDamage        = "damage_or_loss"
	DedAccommodation = "accommodation"
	DedRecovery      = "recovery" // advances, loans, overpaid salary or unearned benefits
	DedCPF           = "cpf"
	DedIncomeTax     = "income_tax" // as IRAS's agent
	DedCourtOrder    = "court_order"
	DedCooperative   = "cooperative" // co-operative society payments
	DedOther         = "other"       // any other purpose, with the employee's written consent
)

// Deduction is one deduction from a month's salary, linked to its source and used once.
type Deduction struct {
	Key        string
	Kind       string
	Amount     *apd.Decimal
	Consent    bool // written consent recorded (evidence), which the employee can withdraw at any time
	Instalment int  // for a recovery of an advance: which instalment this is, from 1
	IsAdvance  bool // a recovery of an advance, which has instalment limits
}

// Violation is a deduction rule the month breaks: ✖ with the rule named.
type Violation struct {
	Key  string
	Rule string
	Text string
}

// MaxAdvanceMonths and the shares of the salary the limits allow (FR-PAY-16, MOM).
const (
	MaxAdvanceMonths = 12
)

var (
	totalLimit      = D("0.5")  // at most 50% of the salary payable in the period
	instalmentLimit = D("0.25") // an advance instalment at most 25% of that period's salary
)

// exempt50 are the kinds the 50% limit does not count: absence, recovery of advances, loans, overpaid salary or unearned benefits, and co-operative society payments.
func exempt50(kind string) bool {
	return kind == DedAbsence || kind == DedRecovery || kind == DedCooperative
}

// CheckDeductions checks a month's deductions apart from the journal (FR-PAY-16): only the kinds MOM allows; any other purpose needs written consent; the deductions, not counting
// the exempt kinds, are at most 50% of the salary payable, except on termination; an advance is recovered in at most 12 instalments, each at most 25% of the period's salary; and
// on termination an advance is recovered only as far as the employee consented in writing. salary is the salary payable in the period; terminated is true for final pay.
func CheckDeductions(salary *apd.Decimal, ds []Deduction, terminated bool) []Violation {
	var out []Violation
	counted := zero()
	for _, d := range ds {
		switch d.Kind {
		case DedAbsence, DedDamage, DedAccommodation, DedRecovery, DedCPF, DedIncomeTax, DedCourtOrder, DedCooperative:
		case DedOther:
			if !d.Consent {
				out = append(out, Violation{d.Key, "FR-PAY-16", "A deduction for any other purpose needs the employee's written consent, which they can withdraw at any time: record it as evidence."})
			}
		default:
			out = append(out, Violation{d.Key, "FR-PAY-16", "That kind of deduction is not one MOM allows."})
			continue
		}
		if d.Amount.Sign() <= 0 {
			out = append(out, Violation{d.Key, "FR-PAY-16", "A deduction is more than zero."})
			continue
		}
		if !exempt50(d.Kind) {
			counted = add(counted, d.Amount)
		}
		if d.IsAdvance {
			if terminated {
				if !d.Consent {
					out = append(out, Violation{d.Key, "FR-PAY-16", "On termination an advance is recovered from final pay only as far as the employee consented in writing: record the consent, or leave the advance open for the people team to decide."})
				}
				continue
			}
			if d.Instalment > MaxAdvanceMonths {
				out = append(out, Violation{d.Key, "FR-PAY-16", "An advance is recovered in at most 12 monthly instalments."})
			}
			if d.Amount.Cmp(mul(salary, instalmentLimit)) > 0 {
				out = append(out, Violation{d.Key, "FR-PAY-16", "An advance instalment is at most 25% of the salary of the period."})
			}
		}
	}
	if !terminated && counted.Cmp(mul(salary, totalLimit)) > 0 {
		out = append(out, Violation{"", "FR-PAY-16", "Deductions, not counting absence, recovery of advances, loans, overpaid salary or unearned benefits, or co-operative society payments, are at most 50% of the salary payable in the period."})
	}
	return out
}
