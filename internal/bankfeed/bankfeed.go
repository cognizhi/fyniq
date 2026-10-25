// Package bankfeed syncs the organisations' bank-data connectors (PRD-001 FR-BNK-15, FR-BNK-16; p1-bank-import-reconcile
// task 8.2, design D11): each money account whose source is a connector pulls its transactions since its feed's cursor,
// as the connector's own principal, through the egress guard, and imports them as statements with the continuity and
// duplicate checks, moving the cursor in the same unit of work. The tick runs it hourly; a provider's verified webhook
// only prompts an early sync of its connector, and its body is never data. The worker posts nothing: the lines wait for
// Reconcile now (FR-BNK-14, rev 0.143).
package bankfeed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/bankfile"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Kind is the outbox kind of an early sync of one connector, which its verified webhook prompts; Job is the tick's.
const (
	Kind = "bank.sync"
	Job  = "bank.sync"
)

// MaxPages is how many pages of one account a run pulls: the rest wait for the next run, from where it stopped.
const MaxPages = 20

type payload struct {
	ConnectorID uuid.UUID `json:"connector_id"`
}

// Syncer pulls the bank-data connectors' accounts.
type Syncer struct {
	Store   storage.Store
	Runtime *connector.Runtime
	Clock   ports.Clock
}

// Synced is what a sync of one connector did.
type Synced struct {
	Accounts int // the money accounts it pulled
	Added    int // lines added
	Errors   []string
}

// fed is a money account a connector feeds, and the provider's account it is.
type fed struct {
	account  storage.MoneyAccount
	provider string
	cur      money.Currency
}

// feeds are the money accounts whose source in force on day is connector id.
func feeds(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, day time.Time) ([]fed, error) {
	accounts, err := u.Banking().MoneyAccounts(ctx)
	if err != nil {
		return nil, err
	}
	var out []fed
	for _, a := range accounts {
		if a.ClosedOn != nil && a.ClosedOn.Before(day) {
			continue
		}
		sources, err := u.Banking().Sources(ctx, a.ID) // earliest first
		if err != nil {
			return nil, err
		}
		var now *storage.MoneyAccountSource
		for i := range sources {
			if !sources[i].FromDate.After(day) {
				now = &sources[i]
			}
		}
		if now == nil || now.Kind != storage.MoneySourceConnector || now.ConnectorID == nil || *now.ConnectorID != id || a.Identifier == "" {
			continue
		}
		cur, err := money.Lookup(a.Currency)
		if err != nil {
			return nil, err
		}
		out = append(out, fed{account: a, provider: a.Identifier, cur: cur})
	}
	return out, nil
}

// statement is a page of transactions as a statement of money account f.
func statement(f fed, page []ports.BankDataTransaction) (bankfile.Statement, error) {
	st := bankfile.Statement{Account: f.account.Identifier, Currency: f.cur}
	for i, t := range page {
		if t.Currency != f.cur.Code {
			return st, fmt.Errorf("transaction %s is in %s; %s is in %s", t.ID, t.Currency, f.account.Name, f.cur.Code)
		}
		amount, err := money.Parse(t.Amount, f.cur)
		if err != nil {
			return st, fmt.Errorf("transaction %s: %w", t.ID, err)
		}
		y, m, d := t.BookingDate.UTC().Date()
		booked := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
		l := bankfile.Line{Position: i + 1, Booked: booked, Amount: amount, Reference: t.ID, Counterparty: t.Counterparty, Description: t.Description}
		if t.ValueDate != nil {
			y, m, d := t.ValueDate.UTC().Date()
			v := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
			l.Value = &v
		}
		if t.RunningBalance != "" {
			b, err := money.Parse(t.RunningBalance, f.cur)
			if err != nil {
				return st, fmt.Errorf("transaction %s's balance: %w", t.ID, err)
			}
			l.Balance = &b
		}
		if st.From.IsZero() || booked.Before(st.From) {
			st.From = booked
		}
		if booked.After(st.To) {
			st.To = booked
		}
		st.Lines = append(st.Lines, l)
	}
	return st, nil
}

// Sync pulls every money account connector id of organisation org feeds, page by page from each feed's cursor: each page
// imported and its cursor moved in one unit of work, so a failure leaves the feed where the last page left it. A
// provider's refusal stops that account, records why on its feed, and goes on to the next.
func (s Syncer) Sync(ctx context.Context, org, id uuid.UUID) (Synced, error) {
	var out Synced
	now := s.Clock.Now()
	var (
		bd      ports.BankData
		c       storage.InstalledConnector
		targets []fed
	)
	if err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		if bd, c, err = s.Runtime.BankData(ctx, u, id, connector.SyncSchedule); err != nil {
			return err
		}
		targets, err = feeds(ctx, u, id, now)
		return err
	}); err != nil {
		return out, err
	}
	ctx = connector.Principal(ctx, c, nil)
	for _, f := range targets {
		out.Accounts++
		added, err := s.pull(ctx, org, id, bd, f)
		out.Added += added
		if err != nil {
			out.Errors = append(out.Errors, fmt.Sprintf("%s: %v", f.account.Name, err))
		}
	}
	return out, nil
}

// pull pulls one account, at most MaxPages pages.
func (s Syncer) pull(ctx context.Context, org, id uuid.UUID, bd ports.BankData, f fed) (int, error) {
	added := 0
	for range MaxPages {
		var feed storage.BankFeed
		if err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			feed, err = u.Banking().Feed(ctx, f.account.ID)
			if errors.Is(err, ports.ErrNotFound) {
				feed, err = storage.BankFeed{}, nil
			}
			return err
		}); err != nil {
			return added, err
		}
		cursor := feed.Cursor
		if feed.ProviderAccount != "" && feed.ProviderAccount != f.provider {
			cursor = "" // the money account now names another of the provider's accounts: from its start
		}
		page, err := bd.Transactions(ctx, f.provider, cursor)
		if err != nil {
			return added, s.stopped(ctx, org, id, f, feed, err)
		}
		st, err := statement(f, page.Transactions)
		if err != nil {
			return added, s.stopped(ctx, org, id, f, feed, err)
		}
		err = s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			if len(st.Lines) > 0 {
				got, err := banking.ImportStatement(ctx, u, banking.ImportRequest{Account: &f.account.ID, Source: storage.MoneySourceConnector,
					ConnectorID: &id, At: s.Clock.Now()}, st)
				if err != nil {
					return err
				}
				added += got.Added
			}
			_, err := u.Banking().SaveFeed(ctx, storage.NewBankFeed{MoneyAccountID: f.account.ID, ConnectorID: id, ProviderAccount: f.provider,
				Cursor: page.Cursor, SyncedAt: s.Clock.Now()}, feed.Version)
			return err
		})
		var refusal *banking.Error
		if errors.As(err, &refusal) {
			return added, s.stopped(ctx, org, id, f, feed, err)
		}
		if err != nil || !page.More {
			return added, err
		}
	}
	return added, nil
}

// stopped records on a feed why its sync stopped, and returns that.
func (s Syncer) stopped(ctx context.Context, org, id uuid.UUID, f fed, feed storage.BankFeed, cause error) error {
	why := cause.Error()
	if len(why) > 500 {
		why = why[:500]
	}
	if err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Banking().SaveFeed(ctx, storage.NewBankFeed{MoneyAccountID: f.account.ID, ConnectorID: id, ProviderAccount: f.provider,
			Cursor: feed.Cursor, SyncedAt: s.Clock.Now(), LastError: why}, feed.Version)
		return err
	}); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

// SyncAll syncs every organisation's enabled bank-data connectors (the tick's hourly job): one organisation's or one
// connector's failure never stops the others.
func (s Syncer) SyncAll(ctx context.Context) error {
	orgs, err := s.Store.Organisations(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, org := range orgs {
		var ids []uuid.UUID
		if err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.Connectors().Organisation(ctx)
			for _, c := range all {
				if b, ok := s.Runtime.Catalogue.Get(c.Connector); ok && b.BankData != nil && c.State == storage.ConnectorEnabled {
					ids = append(ids, c.ID)
				}
			}
			return err
		}); err != nil {
			errs = append(errs, fmt.Errorf("organisation %s: %w", org, err))
			continue
		}
		for _, id := range ids {
			got, err := s.Sync(ctx, org, id)
			if err != nil {
				errs = append(errs, fmt.Errorf("organisation %s, connector %s: %w", org, id, err))
			}
			for _, e := range got.Errors {
				errs = append(errs, fmt.Errorf("organisation %s, connector %s: %s", org, id, e))
			}
		}
	}
	return errors.Join(errs...)
}

// Queue queues an early sync of connector id, in u: one a minute at most, whatever the provider sends.
func Queue(ctx context.Context, d outbox.Dispatcher, u storage.UnitOfWork, org, id uuid.UUID, now time.Time) error {
	b, err := json.Marshal(payload{ConnectorID: id})
	if err != nil {
		return err
	}
	_, err = d.Enqueue(ctx, u, org, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: Kind, Payload: b,
		BusinessKey: "bank-sync:" + id.String() + ":" + now.UTC().Truncate(time.Minute).Format(time.RFC3339)})
	return err
}

// Work is the worker's handler of Kind: a sync of the connector its row names. A connector no longer enabled syncs
// nothing; a provider's refusal is on its feeds, not a failure to retry.
func (s Syncer) Work() outbox.Work {
	return func(ctx context.Context, org uuid.UUID, row storage.OutboxRow) error {
		var p payload
		if err := json.Unmarshal(row.Payload, &p); err != nil || p.ConnectorID == uuid.Nil {
			return fmt.Errorf("bankfeed: outbox row %s is not a sync", row.ID)
		}
		_, err := s.Sync(ctx, org, p.ConnectorID)
		if errors.Is(err, connector.ErrNotEnabled) {
			return nil
		}
		return err
	}
}
