// Package pspnet works out whether a payment provider's payout nets to zero in the provider's clearing account (PRD-001
// FR-PSP-06, check C.2 #42; p1-payment-providers design D7): the payout's line and the lines of the transactions it settles
// (each transaction's gross, and its fee as a line of its own) come to nothing once posted. It reads the statement lines,
// each of which posts exactly its amount through the money account's chart account (FY228), so a transaction the provider
// names whose line has not posted shows as missing, and a wrong sign or a fee not taken in shows as a difference. The check
// and the payouts API both read it, so they cannot disagree.
package pspnet

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FeeSuffix ends the reference of a fee's statement line.
const FeeSuffix = ":fee"

// Posted says a line has posted.
func Posted(status string) bool {
	return status == storage.LineCategorised || status == storage.LineMatched || status == storage.LineTransferred
}

// Lines are a money account's statement lines by their reference.
func Lines(ctx context.Context, u storage.UnitOfWork, moneyAccount storage.MoneyAccount) (map[string]storage.BankLine, error) {
	lines, err := u.Banking().Lines(ctx, storage.BankLineFilter{MoneyAccountID: &moneyAccount.ID})
	if err != nil {
		return nil, err
	}
	out := make(map[string]storage.BankLine, len(lines))
	for _, l := range lines {
		out[l.Reference] = l
	}
	return out, nil
}

// Txn is a transaction a payout settles, with the statement lines of its gross and its fee (nil: not imported yet).
type Txn struct {
	storage.PSPTxn
	Gross, Fee *storage.BankLine
}

// Result is a payout and what it settles.
type Result struct {
	Payout storage.PSPPayout
	// Evaluated is whether the payout's own line has posted: until it has, there is nothing to prove, and Nets is not said.
	Evaluated  bool
	PayoutLine *storage.BankLine
	Txns       []Txn
	// Total is the payout's line and its transactions' posted lines together: nothing when the payout nets to zero.
	Total money.Money
	// Missing are the references of transactions' lines that are not imported or not posted.
	Missing []string
}

// Nets is whether the payout was evaluated and nets to zero with every line posted.
func (r Result) Nets() bool { return r.Evaluated && r.Total.IsZero() && len(r.Missing) == 0 }

// Of works out payout p of provider account a, whose money account is m, over m's lines by reference.
func Of(ctx context.Context, u storage.UnitOfWork, a storage.PSPAccount, cur money.Currency, p storage.PSPPayout, byRef map[string]storage.BankLine) (Result, error) {
	r := Result{Payout: p, Total: money.Zero(cur)}
	contents, err := u.PSP().TxnsOfPayout(ctx, a.ConnectorID, p.PayoutID)
	if err != nil {
		return r, err
	}
	for _, t := range contents {
		x := Txn{PSPTxn: t}
		if l, ok := byRef[t.TxnID]; ok {
			x.Gross = &l
		}
		if l, ok := byRef[t.TxnID+FeeSuffix]; ok {
			x.Fee = &l
		}
		r.Txns = append(r.Txns, x)
	}
	pt, err := u.PSP().TxnOfSource(ctx, a.ConnectorID, "payout", p.PayoutID)
	if errors.Is(err, ports.ErrNotFound) {
		return r, nil
	}
	if err != nil {
		return r, err
	}
	pl, ok := byRef[pt.TxnID]
	if !ok {
		return r, nil
	}
	r.PayoutLine = &pl
	if !Posted(pl.Status) {
		return r, nil
	}
	r.Evaluated = true
	if r.Total, err = money.ParseStored(string(pl.Amount), cur); err != nil {
		return r, err
	}
	for _, t := range r.Txns {
		feeDue := false
		if f, err := money.ParseStored(string(t.PSPTxn.Fee), cur); err == nil && !f.IsZero() {
			feeDue = true
		}
		for _, l := range []struct {
			line *storage.BankLine
			ref  string
			due  bool
		}{{t.Gross, t.TxnID, true}, {t.Fee, t.TxnID + FeeSuffix, feeDue}} {
			if !l.due {
				continue
			}
			if l.line == nil || !Posted(l.line.Status) {
				r.Missing = append(r.Missing, l.ref)
				continue
			}
			amt, err := money.ParseStored(string(l.line.Amount), cur)
			if err != nil {
				return r, err
			}
			if r.Total, err = r.Total.Add(amt); err != nil {
				return r, err
			}
		}
	}
	slices.Sort(r.Missing)
	return r, nil
}

// Settled reports whether a provider's payout status means it has been paid out.
func Settled(status string) bool { return strings.EqualFold(status, "paid") }

// The standing of a dispute.
const (
	DisputeOpen       = "open"        // its money waits in disputes clearing
	DisputeWon        = "won"         // the provider gave it all back: nothing is held
	DisputeWrittenOff = "written_off" // a draft journal was prepared to write it off as a lost dispute
	DisputeCredited   = "credited"    // the customer's credit was settled against it: nothing is left held
)

// Dispute is a dispute's money in disputes clearing, as its posted statement lines hold it.
type Dispute struct {
	DisputeID string
	Currency  string
	// Held is what the dispute's posted lines have moved into disputes clearing and the provider has not given back:
	// positive when money is held.
	Held money.Money
	// Posted is whether every line of the dispute has posted; until then Held is not final.
	Posted bool
	Status string
	// Gross is the disputed amount alone (the withdrawal's lines, without the dispute fee), positive when money was taken.
	Gross money.Money
	// Credited is what a customer credit was settled against it (dispute_settlement@1): Held is net of it.
	Credited money.Money
	// LossDocument is the draft journal prepared to write it off, when one was.
	LossDocument *storage.PSPDisputeLoss
	// Credit is the settlement of a customer credit against it, when there is one.
	Credit *storage.PSPDisputeCredit
}

// Disputes are a provider account's disputes by the provider's ID, oldest first. A dispute's balance transactions are the
// withdrawal (with its fee on a line of its own) and any reinstatement; their posted lines' sum, negated, is held.
func Disputes(ctx context.Context, u storage.UnitOfWork, a storage.PSPAccount, cur money.Currency, byRef map[string]storage.BankLine) ([]Dispute, error) {
	txns, err := u.PSP().DisputeTxns(ctx, a.ConnectorID)
	if err != nil {
		return nil, err
	}
	losses, err := u.PSP().DisputeLosses(ctx, a.ConnectorID)
	if err != nil {
		return nil, err
	}
	lost := map[string]storage.PSPDisputeLoss{}
	for _, l := range losses {
		lost[l.DisputeID] = l
	}
	credits, err := u.PSP().DisputeCredits(ctx, a.ConnectorID)
	if err != nil {
		return nil, err
	}
	credited := map[string]storage.PSPDisputeCredit{}
	for _, c := range credits {
		credited[c.DisputeID] = c
	}
	byID := map[string]*Dispute{}
	var order []string
	for _, t := range txns {
		if t.Currency != cur.Code {
			continue
		}
		id := t.SourceID
		if id == "" {
			id = t.TxnID
		}
		d, ok := byID[id]
		if !ok {
			d = &Dispute{DisputeID: id, Currency: cur.Code, Held: money.Zero(cur), Gross: money.Zero(cur), Credited: money.Zero(cur), Posted: true}
			byID[id] = d
			order = append(order, id)
		}
		refs := []string{t.TxnID}
		if f, err := money.ParseStored(string(t.Fee), cur); err == nil && !f.IsZero() {
			refs = append(refs, t.TxnID+FeeSuffix)
		}
		for i, ref := range refs {
			l, ok := byRef[ref]
			if !ok || !Posted(l.Status) {
				d.Posted = false
				continue
			}
			amt, err := money.ParseStored(string(l.Amount), cur)
			if err != nil {
				return nil, err
			}
			if d.Held, err = d.Held.Sub(amt); err != nil {
				return nil, err
			}
			if i == 0 { // the gross line: the fee is the second
				if d.Gross, err = d.Gross.Sub(amt); err != nil {
					return nil, err
				}
			}
		}
	}
	out := make([]Dispute, 0, len(order))
	for _, id := range order {
		d := *byID[id]
		if c, ok := credited[id]; ok {
			amt, err := money.ParseStored(string(c.Amount), cur)
			if err != nil {
				return nil, err
			}
			d.Credit, d.Credited = &c, amt
			if d.Held, err = d.Held.Sub(amt); err != nil {
				return nil, err
			}
		}
		switch l, isLost := lost[id]; {
		case isLost:
			d.Status, d.LossDocument = DisputeWrittenOff, &l
		case d.Credit != nil && d.Held.Sign() <= 0:
			d.Status = DisputeCredited
		case d.Held.Sign() <= 0:
			d.Status = DisputeWon
		default:
			d.Status = DisputeOpen
		}
		out = append(out, d)
	}
	return out, nil
}
