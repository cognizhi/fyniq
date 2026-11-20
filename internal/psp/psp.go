// Package psp is the worker's side of the payment-provider connectors (PRD-001 FR-PSP-01, FR-PSP-06; p1-payment-providers
// design D1, D4, D7). A provider is a money account its connector feeds with the provider's balance transactions as
// statement lines (the existing bank sync). The sweep mirrors what the lines cannot say on their own: the provider's
// balance transactions with what each is of, its payouts with what they settle, and its balance (psp_txn, psp_payout,
// psp_balance), over a window that reaches back past Stripe's three days of retries, so a webhook that never came is made up
// for by ID, and it records the day each account is complete to.
//
// The worker posts nothing: it holds no right to make a document (the owner's decision of 26 Oct 2026, rev 0.143). The
// lines are reconciled by auto-reconcile in the API (banking.AutoReconcile, which reads this mirror): a fee, a dispute's money
// and a payout post once, as the system principal for whoever pressed Reconcile now, within the per-line cap; a charge or a
// refund waits for the document it settles. Nothing here writes to a provider.
package psp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Kind is the outbox kind of one sweep for one connector; Job is the tick's, for every organisation.
const (
	Kind = "psp.run"
	Job  = "psp.sweep"
)

// Lookback is how far before the day it is complete to a sweep reads again: past Stripe's three days of webhook retries
// (FR-PSP-01), so what an outage lost is fetched by ID. History is how far back a first sweep reads.
const (
	Lookback = 4 * 24 * time.Hour
	History  = 90 * 24 * time.Hour
	window   = 7 * 24 * time.Hour
)

// Runner sweeps the payment-provider connectors.
type Runner struct {
	Store   storage.Store
	Runtime *connector.Runtime
	Clock   ports.Clock
}

// Result is what a run did.
type Result struct {
	Transactions int // balance transactions recorded
	Payouts      int // payouts recorded or updated
	Errors       []string
}

type payload struct {
	ConnectorID uuid.UUID `json:"connector_id"`
}

// QueueRun queues a sweep for one connector, at most one a minute (the verified webhooks and the sweep use it).
func QueueRun(ctx context.Context, d outbox.Dispatcher, u storage.UnitOfWork, org, id uuid.UUID, now time.Time) error {
	b, err := json.Marshal(payload{ConnectorID: id})
	if err != nil {
		return err
	}
	key := "psp-run:" + id.String() + ":" + now.UTC().Truncate(time.Minute).Format(time.RFC3339)
	if row, err := u.Outbox().ByKey(ctx, Kind, key); err != nil || row != nil {
		return err // already queued for the minute: a burst of events sweeps once
	}
	_, err = d.Enqueue(ctx, u, org, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: Kind, Payload: b, BusinessKey: key})
	return err
}

// Work is the worker's handler of Kind.
func (r Runner) Work() outbox.Work {
	return func(ctx context.Context, org uuid.UUID, row storage.OutboxRow) error {
		var p payload
		if err := json.Unmarshal(row.Payload, &p); err != nil || p.ConnectorID == uuid.Nil {
			return fmt.Errorf("psp: outbox row %s is not a run", row.ID)
		}
		res, err := r.Run(ctx, org, p.ConnectorID)
		if errors.Is(err, connector.ErrNotEnabled) {
			return nil
		}
		return errors.Join(err, joined(res.Errors))
	}
}

func joined(msgs []string) error {
	var errs []error
	for _, m := range msgs {
		errs = append(errs, errors.New(m))
	}
	return errors.Join(errs...)
}

// RunAll runs every organisation's enabled payment-provider connectors that have a provider account set up (the tick's
// job): one organisation's or one connector's failure never stops the others.
func (r Runner) RunAll(ctx context.Context) error {
	orgs, err := r.Store.Organisations(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, org := range orgs {
		var ids []uuid.UUID
		if err := r.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.Connectors().Organisation(ctx)
			if err != nil {
				return err
			}
			accounts, err := u.PSP().Accounts(ctx)
			if err != nil {
				return err
			}
			for _, c := range all {
				b, ok := r.Runtime.Catalogue.Get(c.Connector)
				if !ok || b.PaymentProvider == nil || c.State != storage.ConnectorEnabled {
					continue
				}
				if slices.ContainsFunc(accounts, func(a storage.PSPAccount) bool { return a.ConnectorID == c.ID }) {
					ids = append(ids, c.ID)
				}
			}
			return nil
		}); err != nil {
			errs = append(errs, fmt.Errorf("organisation %s: %w", org, err))
			continue
		}
		for _, id := range ids {
			res, err := r.Run(ctx, org, id)
			if err != nil {
				errs = append(errs, fmt.Errorf("organisation %s, connector %s: %w", org, id, err))
			}
			for _, e := range res.Errors {
				errs = append(errs, fmt.Errorf("organisation %s, connector %s: %s", org, id, e))
			}
		}
	}
	return errors.Join(errs...)
}

// Run sweeps connector id of organisation org.
func (r Runner) Run(ctx context.Context, org, id uuid.UUID) (Result, error) {
	var res Result
	now := r.Clock.Now()
	var (
		pp       ports.PaymentProvider
		c        storage.InstalledConnector
		accounts []storage.PSPAccount
	)
	if err := r.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		if pp, c, err = r.Runtime.PaymentProvider(ctx, u, id, connector.ProviderWork); err != nil {
			return err
		}
		all, err := u.PSP().Accounts(ctx)
		for _, a := range all {
			if a.ConnectorID == id {
				accounts = append(accounts, a)
			}
		}
		return err
	}); err != nil {
		return res, err
	}
	if len(accounts) == 0 {
		return res, nil
	}
	ctx = connector.Principal(ctx, c, nil)
	ctx = storage.WithReason(ctx, "Provider sweep (FR-PSP-06)")
	if err := r.sweep(ctx, org, id, pp, accounts, now, &res); err != nil {
		res.Errors = append(res.Errors, err.Error())
		for _, a := range accounts {
			if serr := r.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
				return u.PSP().Swept(ctx, a.MoneyAccountID, time.Time{}, now, clipWhy(err))
			}); serr != nil {
				res.Errors = append(res.Errors, serr.Error())
			}
		}
	}
	return res, nil
}

func clipWhy(err error) string {
	s := err.Error()
	if len(s) > 400 {
		s = s[:400]
	}
	return s
}

// sweep mirrors the provider's balance transactions and payouts from the earliest day any of its accounts is complete to,
// less Lookback, in windows of a week, and its balance now; then records the day each account is complete to.
func (r Runner) sweep(ctx context.Context, org, id uuid.UUID, pp ports.PaymentProvider, accounts []storage.PSPAccount, now time.Time, res *Result) error {
	from, first := now, true // the earliest day any account is complete to, less Lookback; History when one never was
	for _, a := range accounts {
		if a.SweptThrough == nil {
			from, first = now.Add(-History), false
			break
		}
		if t := a.SweptThrough.Add(-Lookback); first || t.Before(from) {
			from, first = t, false
		}
	}
	for start := from; start.Before(now); start = start.Add(window) {
		end := start.Add(window)
		if end.After(now) {
			end = now
		}
		txns, err := pp.List(ctx, ports.ProviderBalanceTransaction, start, end, "")
		if err != nil {
			return fmt.Errorf("balance transactions of %s: %w", start.Format(time.DateOnly), err)
		}
		payouts, err := pp.List(ctx, ports.ProviderPayout, start, end, "")
		if err != nil {
			return fmt.Errorf("payouts of %s: %w", start.Format(time.DateOnly), err)
		}
		contents := map[string][]string{}
		for _, po := range payouts.Objects {
			full, err := pp.Object(ctx, ports.ProviderPayout, po.ID) // a payout's contents are of its own object (FR-PSP-06)
			if err != nil {
				return fmt.Errorf("payout %s: %w", po.ID, err)
			}
			contents[po.ID] = full.Links[ports.ProviderBalanceTransaction]
		}
		if err := r.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			return r.mirror(ctx, u, id, txns.Objects, payouts.Objects, contents, now, res)
		}); err != nil {
			return err
		}
	}
	balances, err := pp.Balance(ctx)
	if err != nil {
		return fmt.Errorf("balance: %w", err)
	}
	return r.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		for _, b := range balances {
			if _, err := u.PSP().RecordBalance(ctx, id, b.Currency, storage.Decimal(b.Amount), now); err != nil {
				return err
			}
		}
		for _, a := range accounts {
			if err := u.PSP().Swept(ctx, a.MoneyAccountID, day(now), now, ""); err != nil {
				return err
			}
		}
		return nil
	})
}

func day(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// mirror records the provider's objects in u: each balance transaction once, each payout and its standing, and the payout
// that settles each transaction it holds, said once.
func (r Runner) mirror(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, txns, payouts []ports.ProviderObject, contents map[string][]string, now time.Time, res *Result) error {
	for _, t := range txns {
		n := storage.NewPSPTxn{ConnectorID: id, TxnID: t.ID, Type: t.Type, Category: t.Category, Currency: t.Currency, Gross: storage.Decimal(t.Amount),
			Fee: storage.Decimal(t.Fee), Net: storage.Decimal(t.Net), ProviderAt: t.Created, SeenAt: now}
		for _, k := range []ports.ProviderKind{ports.ProviderCharge, ports.ProviderRefund, ports.ProviderDispute, ports.ProviderPayout} {
			if l := t.Links[k]; len(l) > 0 {
				n.SourceKind, n.SourceID = string(k), l[0]
			}
		}
		if !t.AvailableOn.IsZero() {
			a := day(t.AvailableOn)
			n.AvailableOn = &a
		}
		if _, isNew, err := u.PSP().RecordTxn(ctx, n); err != nil {
			return fmt.Errorf("balance transaction %s: %w", t.ID, err)
		} else if isNew {
			res.Transactions++
		}
		if err := recordLinkPayment(ctx, u, id, t, now); err != nil {
			return err
		}
	}
	for _, po := range payouts {
		amount := po.Amount
		if amount == "" {
			continue
		}
		var arrival *time.Time
		if !po.Created.IsZero() {
			d := day(po.Created)
			arrival = &d
		}
		if _, changed, err := u.PSP().RecordPayout(ctx, storage.NewPSPPayout{ConnectorID: id, PayoutID: po.ID, Currency: po.Currency, Amount: storage.Decimal(amount),
			Status: po.Status, ArrivalDate: arrival, Automatic: true, ProviderAt: po.Created, SeenAt: now}); err != nil {
			return fmt.Errorf("payout %s: %w", po.ID, err)
		} else if changed {
			res.Payouts++
		}
		for _, txn := range contents[po.ID] {
			if _, err := u.PSP().SettleTxn(ctx, id, txn, po.ID); err != nil {
				return fmt.Errorf("payout %s settles %s: %w", po.ID, txn, err)
			}
		}
	}
	return nil
}

// Handler works the events of provider objects that change what the sweep reads: it records the object where it says so
// (a payout and what it settles) and queues a sweep of its connector, so the mirror follows the event without waiting for
// the hour (FR-PSP-01). It posts nothing.
type Handler struct {
	Dispatcher outbox.Dispatcher
	Clock      ports.Clock
}

// Handle implements pspevents.Handler.
func (h Handler) Handle(ctx context.Context, u storage.UnitOfWork, c storage.InstalledConnector, ev storage.PSPEvent, obj ports.ProviderObject) (string, error) {
	now := h.Clock.Now()
	if obj.Kind == ports.ProviderPayout && obj.Amount != "" {
		var arrival *time.Time
		if !obj.Created.IsZero() {
			d := day(obj.Created)
			arrival = &d
		}
		if _, _, err := u.PSP().RecordPayout(ctx, storage.NewPSPPayout{ConnectorID: c.ID, PayoutID: obj.ID, Currency: obj.Currency, Amount: storage.Decimal(obj.Amount),
			Status: obj.Status, ArrivalDate: arrival, Automatic: true, ProviderAt: obj.Created, SeenAt: now}); err != nil {
			return "", err
		}
		for _, txn := range obj.Links[ports.ProviderBalanceTransaction] {
			if _, err := u.PSP().SettleTxn(ctx, c.ID, txn, obj.ID); err != nil {
				return "", err
			}
		}
	}
	o, err := u.Organisation(ctx)
	if err != nil {
		return "", err
	}
	return "", QueueRun(ctx, h.Dispatcher, u, o.ID, c.ID, now)
}

// recordLinkPayment notes that a charge was made under one of the organisation's payment links (FR-PSP-02): the provider's
// charge names the payment request it was made under, and a link of that request in the charge's currency takes it as a payment.
// Only the mirror is written; the receipt is auto-reconcile's, which also checks the amount against what the invoice has open.
func recordLinkPayment(ctx context.Context, u storage.UnitOfWork, connector uuid.UUID, t ports.ProviderObject, now time.Time) error {
	if t.Category != storage.PSPCatCharge || t.Amount == "" {
		return nil
	}
	for _, request := range t.Links[ports.ProviderInvoice] {
		link, err := u.PaymentLinks().ByRequest(ctx, connector, request)
		if errors.Is(err, ports.ErrNotFound) {
			continue
		}
		if err != nil {
			return fmt.Errorf("charge %s: %w", t.ID, err)
		}
		if link.Currency != t.Currency {
			continue // a charge in another currency than the link asked for is no payment of it: a person looks at the line
		}
		if _, _, err := u.PaymentLinks().RecordPayment(ctx, storage.NewPaymentLinkPayment{LinkID: link.ID, ConnectorID: connector, ProviderPaymentID: t.ID,
			Currency: t.Currency, Amount: storage.Decimal(t.Amount), PaidAt: t.Created, SeenAt: now}); err != nil {
			return fmt.Errorf("charge %s under link %s: %w", t.ID, link.ID, err)
		}
	}
	return nil
}
