package checks

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// onProviders are the audit object types whose change reruns the payment providers' checks.
var onProviders = []string{"psp_payout", "psp_txn", "psp_balance", "psp_account", "bank_line", "bank_match", "bank_transfer", "money_account"}

// The checks of the payment providers (p1-payment-providers task 2.5; FR-PSP-06).
var pspChecks = []Definition{
	{Key: "payments.payout-nets", Ref: "C.2 #42", Area: "Bank", Worst: Error, Title: "Every settled provider payout nets to zero in clearing",
		Explain: "A payout's charges, refunds, disputes and fees, with the payout itself, must come to nothing in the provider's clearing " +
			"account. When they do not, a charge or a fee is missing, or posted twice, or a payout went to the wrong place.",
		Rules: "FR-PSP-06", Owner: "bookkeeper", View: viewBank, Triggers: onProviders, Run: payoutsNetToZero},
	{Key: "payments.clearing-balance", Ref: "C.2 #50", Area: "Bank", Worst: Warning, Title: "Provider clearing equals the provider's balance",
		Explain: "What the books hold in a provider's clearing account should equal what the provider says it holds for you and has not " +
			"paid out. A difference is a charge, refund, dispute or fee the books have not taken in yet, or took twice.",
		Rules: "FR-PSP-05, FR-PSP-06", Owner: "bookkeeper", View: viewBank, Triggers: onProviders, Run: clearingEqualsProviderBalance},
}

// posted is whether a line has posted.
func posted(status string) bool {
	return status == storage.LineCategorised || status == storage.LineMatched || status == storage.LineTransferred
}

// payoutsNetToZero (C.2 #42): for each paid payout whose own line has posted, its transactions' lines, with the payout's,
// come to nothing in the clearing account. It reads the posted lines (each posts exactly its amount through the account,
// FY228), so a transaction the provider names that has no posted line shows as short.
func payoutsNetToZero(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	accounts, err := u.PSP().Accounts(ctx)
	if err != nil {
		return Result{}, err
	}
	if len(accounts) == 0 {
		return Fine("No payment provider is set up"), nil
	}
	monies, err := moneyAccounts(ctx, u)
	if err != nil {
		return Result{}, err
	}
	var items []Item
	checked := 0
	for _, a := range accounts {
		m := monies[a.MoneyAccountID]
		cur, err := money.Lookup(m.Currency)
		if err != nil {
			return Result{}, err
		}
		lines, err := u.Banking().Lines(ctx, storage.BankLineFilter{MoneyAccountID: &a.MoneyAccountID})
		if err != nil {
			return Result{}, err
		}
		byRef := map[string]storage.BankLine{}
		for _, l := range lines {
			byRef[l.Reference] = l
		}
		payouts, err := u.PSP().Payouts(ctx, a.ConnectorID, 500)
		if err != nil {
			return Result{}, err
		}
		for _, p := range payouts {
			if p.Currency != m.Currency || p.Status != "paid" {
				continue
			}
			pt, err := u.PSP().TxnOfSource(ctx, a.ConnectorID, "payout", p.PayoutID)
			if errors.Is(err, ports.ErrNotFound) {
				continue
			}
			if err != nil {
				return Result{}, err
			}
			pl, ok := byRef[pt.TxnID]
			if !ok || !posted(pl.Status) {
				continue // its own line has not posted: nothing to prove yet
			}
			checked++
			total, err := money.ParseStored(string(pl.Amount), cur)
			if err != nil {
				return Result{}, err
			}
			contents, err := u.PSP().TxnsOfPayout(ctx, a.ConnectorID, p.PayoutID)
			if err != nil {
				return Result{}, err
			}
			var missing []string
			for _, t := range contents {
				refs := []string{t.TxnID}
				if f, err := money.ParseStored(string(t.Fee), cur); err == nil && !f.IsZero() {
					refs = append(refs, t.TxnID+":fee")
				}
				for _, ref := range refs {
					l, ok := byRef[ref]
					if !ok || !posted(l.Status) {
						missing = append(missing, ref)
						continue
					}
					amt, err := money.ParseStored(string(l.Amount), cur)
					if err != nil {
						return Result{}, err
					}
					if total, err = total.Add(amt); err != nil {
						return Result{}, err
					}
				}
			}
			if total.IsZero() && len(missing) == 0 {
				continue
			}
			detail := fmt.Sprintf("Its transactions and the payout come to %s %s, not nothing", total, cur.Code)
			if len(missing) > 0 {
				slices.Sort(missing)
				detail += "; not posted in " + m.Name + ": " + strings.Join(missing, ", ")
			}
			items = append(items, Item{Kind: "psp_payout", ID: p.ID.String(), Label: fmt.Sprintf("%s payout %s", m.Name, p.PayoutID), Detail: detail})
		}
	}
	if len(items) == 0 {
		return Fine(fmt.Sprintf("%d settled payout(s) net to zero", checked)), nil
	}
	return Found(Error, items, fmt.Sprintf("%d payout(s) do not net to zero", len(items)),
		"Post the transactions the payout holds (Banking › Categorise), then check the payout's page"), nil
}

// clearingEqualsProviderBalance (C.2 #50): each provider account's book balance equals the provider's own balance not yet
// paid out, as the latest sweep read it.
func clearingEqualsProviderBalance(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	accounts, err := u.PSP().Accounts(ctx)
	if err != nil {
		return Result{}, err
	}
	if len(accounts) == 0 {
		return Fine("No payment provider is set up"), nil
	}
	monies, err := moneyAccounts(ctx, u)
	if err != nil {
		return Result{}, err
	}
	var items []Item
	read := 0
	for _, a := range accounts {
		m := monies[a.MoneyAccountID]
		cur, err := money.Lookup(m.Currency)
		if err != nil {
			return Result{}, err
		}
		pb, err := u.PSP().LatestBalance(ctx, a.ConnectorID, m.Currency)
		if errors.Is(err, ports.ErrNotFound) {
			continue
		}
		if err != nil {
			return Result{}, err
		}
		read++
		provider, err := money.ParseStored(string(pb.Amount), cur)
		if err != nil {
			return Result{}, err
		}
		book, _, err := banking.BookBalance(ctx, u, m.AccountID, cur, e.Base, e.Today)
		if err != nil {
			return Result{}, err
		}
		diff, err := book.Sub(provider)
		if err != nil {
			return Result{}, err
		}
		if diff.IsZero() {
			continue
		}
		items = append(items, relabel(m, a.MoneyAccountID, fmt.Sprintf("The books hold %s %s; %s says %s %s is held for you and not paid out (a difference of %s)",
			book, cur.Code, "the provider", provider, cur.Code, diff)))
	}
	if len(items) == 0 {
		return Fine(fmt.Sprintf("%d provider balance(s) agree with the books", read)), nil
	}
	return Found(Warning, items, fmt.Sprintf("%d provider account(s) differ from the provider's balance", len(items)),
		"Find the charge, refund, dispute or fee the books lack: the account's unreconciled lines and its payouts show it"), nil
}

func relabel(m storage.MoneyAccount, id uuid.UUID, detail string) Item {
	return Item{Kind: "money_account", ID: id.String(), Label: m.Name, Detail: detail}
}
