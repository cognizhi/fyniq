package ledger

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// The vendor credit's source type and rule version (FR-AP-03; PRD-001 §10.2's "Vendor credits reverse").
const (
	SourceVendorCredit = "vendor_credit"
	RuleVendorCredit   = "vendor_credit@1"
)

// VendorCreditIdentity is the posting identity of a vendor credit version under a rule version (§11.4).
func VendorCreditIdentity(id uuid.UUID, version int, rule string) string {
	return identity(SourceVendorCredit, id, version, rule)
}

// asCredit words a bill rule's problems for a supplier's credit note.
var asCredit = strings.NewReplacer(
	"A bill's total is more than zero; record what a supplier owes the company as a vendor credit", "A credit note's total is more than zero",
	"the bill's", "the credit note's",
	"the bill", "the credit note",
	"bill's", "credit note's",
)

// PostVendorCredit turns one version of a supplier's credit note into its entry (vendor_credit@1): the reverse of the
// legs a bill of the same content posts (bill@1), line for line: Dr 2010 with its total, Cr each line's account with its
// net (with its GST when the input tax is not claimable), Cr 2200 with the claimable GST as stated, and for a credit in
// another currency the difference the other way on 7030. RND-FX rounds half away from zero, so each base amount is
// exactly the bill's, negated. Every refusal is the bill rule's, worded for a credit note (ENG-22).
func PostVendorCredit(b Bill, s Settings) (Entry, error) {
	e, err := PostBill(b, s)
	if probs, ok := err.(Problems); ok {
		for i := range probs {
			probs[i].Message, probs[i].Fix = asCredit.Replace(probs[i].Message), asCredit.Replace(probs[i].Fix)
		}
		return Entry{}, probs
	}
	if err != nil {
		return Entry{}, err
	}
	for i := range e.Lines {
		e.Lines[i].Amount, e.Lines[i].Base = e.Lines[i].Amount.Neg(), e.Lines[i].Base.Neg()
	}
	e.Source = Source{Type: SourceVendorCredit, ID: b.ID.String(), Version: b.Version}
	e.RuleVersion = RuleVendorCredit
	e.PostingIdentity = VendorCreditIdentity(b.ID, b.Version, RuleVendorCredit)
	return e, nil
}

// VendorCreditRules are every vendor credit rule version the engine has posted with, by name: like Rules, a version is
// never removed or changed while an entry names it.
var VendorCreditRules = map[string]BillRule{
	RuleVendorCredit: PostVendorCredit,
}

// ReplayVendorCredit re-runs the rule version that posted a vendor credit's entry over the same version (ENG-06).
func ReplayVendorCredit(rule string, b Bill, s Settings) (Entry, error) {
	f, ok := VendorCreditRules[rule]
	if !ok {
		return Entry{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("the engine has no rule version %s", rule),
			"Restore the rule version: an old version is never removed while an entry names it (ENG-06)"}}
	}
	return f(b, s)
}
