package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func toFeed(r sqlcdb.BankFeedRow) storage.BankFeed {
	out := storage.BankFeed{MoneyAccountID: r.MoneyAccountID, ConnectorID: r.ConnectorID, ProviderAccount: r.ProviderAccount, Cursor: r.Cursor,
		SyncedAt: r.SyncedAt, Version: int(r.Version)}
	if r.LastError != nil {
		out.LastError = *r.LastError
	}
	return out
}

func (r banking) Feed(ctx context.Context, moneyAccountID uuid.UUID) (storage.BankFeed, error) {
	if err := r.u.check(); err != nil {
		return storage.BankFeed{}, err
	}
	row, err := r.u.q.BankFeed(ctx, moneyAccountID)
	if err != nil {
		return storage.BankFeed{}, mapError(err)
	}
	return toFeed(row), nil
}

func (r banking) Feeds(ctx context.Context) ([]storage.BankFeed, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.BankFeeds(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.BankFeed, 0, len(rows))
	for _, x := range rows {
		out = append(out, toFeed(sqlcdb.BankFeedRow(x)))
	}
	return out, nil
}

func (r banking) SaveFeed(ctx context.Context, n storage.NewBankFeed, version int) (storage.BankFeed, error) {
	if err := r.u.check(); err != nil {
		return storage.BankFeed{}, err
	}
	var last *string
	if n.LastError != "" {
		last = &n.LastError
	}
	var row sqlcdb.BankFeedRow
	var err error
	if version == 0 {
		var x sqlcdb.InsertBankFeedRow
		x, err = r.u.q.InsertBankFeed(ctx, sqlcdb.InsertBankFeedParams{MoneyAccountID: n.MoneyAccountID, ConnectorID: n.ConnectorID,
			ProviderAccount: n.ProviderAccount, Cursor: n.Cursor, SyncedAt: n.SyncedAt, LastError: last})
		row = sqlcdb.BankFeedRow(x)
	} else {
		var x sqlcdb.UpdateBankFeedRow
		x, err = r.u.q.UpdateBankFeed(ctx, sqlcdb.UpdateBankFeedParams{ConnectorID: n.ConnectorID, ProviderAccount: n.ProviderAccount, Cursor: n.Cursor,
			SyncedAt: n.SyncedAt, LastError: last, MoneyAccountID: n.MoneyAccountID, Version: int32(version)}) // #nosec G115 -- a version number, the feed's own
		row = sqlcdb.BankFeedRow(x)
		if errors.Is(err, pgx.ErrNoRows) {
			return storage.BankFeed{}, &storage.Error{Code: storage.CodeVersion, Message: fmt.Sprintf("the feed of money account %s moved since version %d", n.MoneyAccountID, version)}
		}
	}
	if err != nil {
		return storage.BankFeed{}, mapError(err)
	}
	out := toFeed(row)
	return out, r.u.event(ctx, "bank_feed.synced", "bank_feed", out.MoneyAccountID, nil, map[string][2]any{
		"connector_id": {nil, out.ConnectorID.String()}, "provider_account": {nil, out.ProviderAccount}, "version": {nil, out.Version},
		"last_error": {nil, out.LastError}})
}

// OrganisationConnectorRoute implements storage.ConnectorRoutes, before any tenant is set.
func (s *Store) OrganisationConnectorRoute(ctx context.Context, id uuid.UUID) (storage.ConnectorRoute, error) {
	row, err := s.q().OrganisationConnectorRoute(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.ConnectorRoute{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.ConnectorRoute{}, mapError(err)
	}
	return storage.ConnectorRoute{Organisation: row.OrganisationID, Connector: row.Connector, State: row.State}, nil
}
