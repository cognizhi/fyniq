package ledger

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// The credit note's source type and rule version (FR-AR-04; posting rule "Credit documents and opening items", PRD-001
// Appendix B.2's "Credit notes reverse"; p1-sales task 3.1).
const (
	SourceCreditNote = "credit_note"
	RuleCreditNote   = "credit_note@1"
)

// CreditNoteIdentity is the posting identity of a credit note version under a rule version (§11.4).
func CreditNoteIdentity(id uuid.UUID, version int, rule string) string {
	return identity(SourceCreditNote, id, version, rule)
}

// asCreditNote words an invoice rule's problems for a credit note to a customer.
var asCreditNote = strings.NewReplacer(
	"An invoice's total is more than zero; record what the company owes a customer as a credit note", "A credit note's total is more than zero",
	"the invoice's", "the credit note's",
	"the invoice", "the credit note",
	"invoice's", "credit note's",
)

// PostCreditNote turns one version of a credit note to a customer into its entry (credit_note@1): the reverse of the
// legs an invoice of the same content posts (invoice@1), line for line: Cr 1100 with its total, Dr each line's account
// with its net, Dr 2200 with its output tax under RND-GST-EXCL, and for a credit note in another currency the cents of
// the conversion the other way on 7030. RND-FX rounds half away from zero, so each base amount is exactly the
// invoice's, negated. Every refusal is the invoice rule's, worded for a credit note (ENG-22), and a credit note's line
// is never spread (FR-GL-03): what it credits leaves the account the revenue is in when the credit note is dated.
func PostCreditNote(i Invoice, s Settings) (Entry, error) {
	var spread Problems
	if len(i.Deposits) > 0 {
		spread = append(spread, Problem{CodeDeposit, "deposits", "a credit note deducts no deposit", "Remove the deposits; refund a deposit instead"})
	}
	for k, l := range i.Lines {
		if l.Spread != nil {
			spread = append(spread, Problem{CodeSpread, fmt.Sprintf("lines[%d].spread_months", k+1), "a credit note's line is not spread",
				"Remove the spread, and credit the account the revenue is in"})
		}
	}
	e, err := PostInvoice(i, s)
	if probs, ok := err.(Problems); ok || len(spread) > 0 {
		for k := range probs {
			probs[k].Message, probs[k].Fix = asCreditNote.Replace(probs[k].Message), asCreditNote.Replace(probs[k].Fix)
		}
		return Entry{}, append(spread, probs...)
	}
	if err != nil {
		return Entry{}, err
	}
	for k := range e.Lines {
		e.Lines[k].Amount, e.Lines[k].Base = e.Lines[k].Amount.Neg(), e.Lines[k].Base.Neg()
	}
	e.Source = Source{Type: SourceCreditNote, ID: i.ID.String(), Version: i.Version}
	e.RuleVersion = RuleCreditNote
	e.PostingIdentity = CreditNoteIdentity(i.ID, i.Version, RuleCreditNote)
	return e, nil
}

// CreditNoteRules are every credit note rule version the engine has posted with, by name: like Rules, a version is
// never removed or changed while an entry names it.
var CreditNoteRules = map[string]InvoiceRule{
	RuleCreditNote: PostCreditNote,
}

// ReplayCreditNote re-runs the rule version that posted a credit note's entry over the same version (ENG-06).
func ReplayCreditNote(rule string, i Invoice, s Settings) (Entry, error) {
	f, ok := CreditNoteRules[rule]
	if !ok {
		return Entry{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("the engine has no rule version %s", rule),
			"Restore the rule version: an old version is never removed while an entry names it (ENG-06)"}}
	}
	return f(i, s)
}
