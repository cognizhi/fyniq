package differential

import (
	"encoding/json"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
)

// writtenOff is a posted write-off as a claim of GST bad debt relief reads it: its invoice (the scenario's ID), the amount in the invoice's
// currency, the account the debt went to, and whether it is claimed.
type writtenOff struct {
	invoice string
	amount  money.Money
	account string
	claimed bool
}

// outputGST is the output GST an invoice's entry posted to the GST control account, in the base currency (the credit it left there).
func (r *p1) outputGST(e ledger.Entry) money.Money {
	out := money.Zero(r.base)
	for _, l := range e.Lines {
		if r.settings.Accounts[l.AccountID].Role == ledger.RoleGSTControl {
			out, _ = out.Sub(l.Base)
		}
	}
	return out
}

type reliefDoc struct {
	WriteOff string `json:"write_off"`
}

// badDebtRelief posts the claim of GST bad debt relief for a write-off (bad_debt_relief@1) as the claim service prepares it: the output tax of
// the invoice in the part written off, once per write-off, never for an invoice a credit note credits, and never of what is not left.
func (r *p1) badDebtRelief(d Document, date time.Time) (ledger.Entry, bool) {
	var x reliefDoc
	if json.Unmarshal(d.Raw, &x) != nil {
		return ledger.Entry{}, false
	}
	wo := r.writtenOff[x.WriteOff]
	if wo == nil || wo.claimed {
		return ledger.Entry{}, false
	}
	inv, open := r.issued[wo.invoice], r.open[wo.invoice]
	if inv == nil || open == nil || !inv.credited.IsZero() {
		return ledger.Entry{}, false
	}
	c := ledger.BadDebtRelief{ID: stableID("document", d.ID), Version: max(d.Version, 1), Date: date, WriteOffID: stableID("document", x.WriteOff),
		InvoiceID: stableID("document", wo.invoice), Written: wo.amount, Owed: open.total, WrittenBefore: inv.reliefWritten, Output: inv.output,
		ClaimedBefore: inv.reliefClaimed, Account: account(wo.account)}
	e, err := ledger.PostBadDebtRelief(c, r.settings)
	if err != nil || !r.fits(e) {
		return ledger.Entry{}, false
	}
	share, _ := ledger.ReliefOf(c, r.base)
	wo.claimed = true
	inv.reliefWritten, _ = inv.reliefWritten.Add(wo.amount)
	inv.reliefClaimed, _ = inv.reliefClaimed.Add(share)
	return e, true
}
