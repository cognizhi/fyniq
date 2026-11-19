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
