package banking

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/banktransaction"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A payment provider's lines (FR-PSP-04, FR-PSP-05, FR-PSP-06; p1-payment-providers design D5): the lines of a provider's
// money account carry the provider's own reference (a balance transaction's ID, and ":fee" for its fee), and the sweep
// mirrors each balance transaction (psp_txn) with what it is of. With both, a line is certain: a fee is a fee, a dispute's
// money is a dispute's, a payout is a payout. Auto-reconcile posts those like any other exact line: within the per-line
// cap, as the system principal for whoever pressed Reconcile now, audited with the provider's reference, into an open
// period (the worker posts nothing: it only mirrors). A charge or a refund waits for the document it settles; a kind of
// movement no rule posts waits for a person.

// FeeSuffix ends the reference of a fee's statement line.
const FeeSuffix = ":fee"

// The GST codes the provider's lines post with. A fee carries no claimable GST: only the provider's monthly tax invoice
// claims any, by a reclassification that never posts a second fee (FR-PSP-05), so the fee is out of scope here and not
// reported in the return (OP) [accountant to confirm; whether Stripe's or HitPay's processing fees carry GST is [verify]
// in PRD-001]. A dispute's money is not a supply or a purchase (NA).
const (
	gstFee     = "OP"
	gstDispute = "NA"
)

// providerStep is what the provider's own data says to do with a line.
type providerStep struct {
	categorise *banktransaction.Part // post the whole line to this account
	transferTo *uuid.UUID            // or record it as a payout to this money account
}

// providerPlan is the step for a line of a payment provider's money account, nil when the line is not one, the provider has
// not reported it yet, or no rule posts it by itself (then the other paths judge it).
func providerPlan(ctx context.Context, u storage.UnitOfWork, line storage.BankLine, amount money.Money) (*providerStep, error) {
	a, err := u.PSP().Account(ctx, line.MoneyAccountID)
	if errors.Is(err, ports.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	base := strings.TrimSuffix(line.Reference, FeeSuffix)
	isFee := base != line.Reference
	txn, err := u.PSP().Txn(ctx, a.ConnectorID, base)
	if errors.Is(err, ports.ErrNotFound) {
		return nil, nil // the sweep has not mirrored it yet
	}
	if err != nil {
		return nil, err
	}
	switch {
	case txn.Category == storage.PSPCatDispute, txn.Category == storage.PSPCatDisputeReversal:
		if a.DisputesAccountID == nil {
			return nil, nil
		}
		return &providerStep{categorise: &banktransaction.Part{AccountID: *a.DisputesAccountID, GSTCode: gstDispute,
			Description: "Disputed with the provider: waits until it is won or lost (FR-PSP-04)"}}, nil
	case isFee, txn.Category == storage.PSPCatFee:
		return &providerStep{categorise: &banktransaction.Part{AccountID: a.FeeAccountID, GSTCode: gstFee, Description: "Payment provider fee (FR-PSP-05)"}}, nil
	case txn.Category == storage.PSPCatPayout:
		if a.PayoutAccountID == nil || amount.Sign() >= 0 {
			return nil, nil
		}
		return &providerStep{transferTo: a.PayoutAccountID}, nil
	}
	return nil, nil
}

// applyProvider makes a provider step: the categorisation, or the payout recorded as a transfer in transit to the bank
// account it lands in (its own line completes it, FR-BNK-09).
func applyProvider(ctx context.Context, txns journal.BankTransactions, org uuid.UUID, line storage.BankLine, s providerStep, at time.Time) (storage.BankMatch, error) {
	if s.categorise != nil {
		c, err := txns.Categorise(ctx, org, journal.Categorisation{Basis: storage.BasisExact, At: at, Draft: banktransaction.Draft{LineID: line.ID,
			Reference: line.Reference, Automatic: true, Parts: []banktransaction.Part{*s.categorise}}})
		return c.Match, err
	}
	cur, err := money.Lookup(line.Currency)
	if err != nil {
		return storage.BankMatch{}, err
	}
	amount, err := money.ParseStored(string(line.Amount), cur)
	if err != nil {
		return storage.BankMatch{}, err
	}
	paid := amount.Neg()
	id := line.ID
	t, err := txns.RecordTransfer(ctx, org, journal.TransferRequest{FromLineID: &id, FromAccountID: line.MoneyAccountID, ToAccountID: *s.transferTo,
		Paid: &paid, Received: &paid, Date: line.BookingDate, Basis: storage.BasisExact, At: at})
	if err != nil {
		return storage.BankMatch{}, err
	}
	return t.Matches[0], nil
}
