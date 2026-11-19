package ledger

import (
	"fmt"
	"maps"

	"github.com/google/uuid"
)

// The lost dispute's settlement against a customer credit (PRD-001 FR-PSP-04, rev 0.308; p1-payment-providers task 4.5; the
// owner's decision of 19 Nov 2026: a credit note when the invoice is known). A payment provider took a disputed charge back and
// the amount waits in the provider's disputes clearing account; the customer's invoice is credited by a credit note, which
// leaves a credit with the customer on accounts receivable; this settles the credit against the money the provider took:
// Dr 1100 with the credit's carrying amount, Cr the disputes clearing account. It is the legs of a customer refund (the
// credit is paid back from an account), with the disputes clearing account in the place of the money account the refund
// left, which the refund rule refuses for any account that holds no money: a manual journal cannot do this, because it may not
// touch accounts receivable (INV-07). It is its own rule version of the refund document, as deposit_refund@1 is.
const RuleDisputeSettlement = "dispute_settlement@1"

// DisputeSettlementIdentity is the posting identity of a dispute settlement version under a rule version (§11.4).
func DisputeSettlementIdentity(id uuid.UUID, version int, rule string) string {
	return identity(SourceCustomerRefund, id, version, rule)
}

// PostDisputeSettlement turns one version of a settlement of a customer credit against a provider's disputes clearing account
// into its entry (dispute_settlement@1) and what it settled of the credit. The account is a chart account that holds
// no money, so it is read as the account the amount left; the settlement is in the base currency, since a provider's disputes
// clearing account is kept in it, and every other refusal is the refund rule's (ENG-22).
func PostDisputeSettlement(r CustomerRefund, s Settings) (Entry, Settlement, error) {
	if r.Currency != s.Base {
		return Entry{}, Settlement{}, Problems{{CodeLineCurrency, "currency", fmt.Sprintf("the credit is in %s, but a disputes clearing account is kept in %s", r.Currency, s.Base),
			"Settle the credit by hand: a lost dispute in another currency is written off with a refund of its own"}}
	}
	accounts := maps.Clone(s.Accounts)
	if a, ok := accounts[r.Account]; ok {
		a.Money = true
		accounts[r.Account] = a
	}
	t := s
	t.Accounts = accounts
	e, settled, err := PostCustomerRefund(r, t)
	if err != nil {
		return Entry{}, Settlement{}, err
	}
	for i := range e.Lines {
		if e.Lines[i].Description == "Realised FX and rounding on a refund" {
			e.Lines[i].Description = "Rounding on a dispute settlement"
		}
	}
	e.RuleVersion = RuleDisputeSettlement
	e.PostingIdentity = DisputeSettlementIdentity(r.ID, r.Version, RuleDisputeSettlement)
	return e, settled, nil
}
