package postgres

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) PSP() storage.PSPLedger { return pspLedger{u} }

type pspLedger struct{ u *unitOfWork }

func toPSPAccount(r sqlcdb.PSPAccountRow) storage.PSPAccount {
	return storage.PSPAccount{MoneyAccountID: r.MoneyAccountID, ConnectorID: r.ConnectorID, PayoutAccountID: r.PayoutAccountID, FeeAccountID: r.FeeAccountID,
		DisputesAccountID: r.DisputesAccountID, DisputeLossAccountID: r.DisputeLossAccountID, SweptThrough: nullDate(r.SweptThrough), SweptAt: r.SweptAt, SweepError: str(r.SweepError), Version: int(r.Version)}
}

func (l pspLedger) Accounts(ctx context.Context) ([]storage.PSPAccount, error) {
	if err := l.u.check(); err != nil {
		return nil, err
	}
	rows, err := l.u.q.PSPAccounts(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.PSPAccount, 0, len(rows))
	for _, r := range rows {
		out = append(out, toPSPAccount(sqlcdb.PSPAccountRow(r)))
	}
	return out, nil
}

func (l pspLedger) Account(ctx context.Context, moneyAccount uuid.UUID) (storage.PSPAccount, error) {
	if err := l.u.check(); err != nil {
		return storage.PSPAccount{}, err
	}
	r, err := l.u.q.PSPAccount(ctx, moneyAccount)
	if err != nil {
		return storage.PSPAccount{}, mapError(err)
	}
	return toPSPAccount(r), nil
}

func (l pspLedger) SaveAccount(ctx context.Context, n storage.NewPSPAccount, version int) (storage.PSPAccount, error) {
	if err := l.u.check(); err != nil {
		return storage.PSPAccount{}, err
	}
	var before *storage.PSPAccount
	var out storage.PSPAccount
	if version == 0 {
		r, err := l.u.q.InsertPSPAccount(ctx, sqlcdb.InsertPSPAccountParams{MoneyAccountID: n.MoneyAccountID, ConnectorID: n.ConnectorID,
			PayoutAccountID: n.PayoutAccountID, FeeAccountID: n.FeeAccountID, DisputesAccountID: n.DisputesAccountID, DisputeLossAccountID: n.DisputeLossAccountID, CreatedBy: l.u.actor(ctx), CreatedAt: n.At})
		if err != nil {
			return storage.PSPAccount{}, mapError(err)
		}
		out = toPSPAccount(sqlcdb.PSPAccountRow(r))
	} else {
		old, err := l.Account(ctx, n.MoneyAccountID)
		if err != nil {
			return storage.PSPAccount{}, err
		}
		before = &old
		if version > math.MaxInt32 {
			return storage.PSPAccount{}, &storage.Error{Code: storage.CodeVersion, Message: "the account moved on"}
		}
		r, err := l.u.q.UpdatePSPAccount(ctx, sqlcdb.UpdatePSPAccountParams{PayoutAccountID: n.PayoutAccountID, FeeAccountID: n.FeeAccountID,
			DisputesAccountID: n.DisputesAccountID, DisputeLossAccountID: n.DisputeLossAccountID, MoneyAccountID: n.MoneyAccountID, Version: int32(version)}) // #nosec G115 -- a version, checked against MaxInt32 above
		if errors.Is(err, pgx.ErrNoRows) {
			return storage.PSPAccount{}, &storage.Error{Code: storage.CodeVersion, Message: "the provider account was changed by someone else: read it again"}
		}
		if err != nil {
			return storage.PSPAccount{}, mapError(err)
		}
		out = toPSPAccount(sqlcdb.PSPAccountRow(r))
	}
	ptr := func(p *uuid.UUID) any {
		if p == nil {
			return nil
		}
		return p.String()
	}
	changes := map[string][2]any{"connector_id": {nil, out.ConnectorID.String()}, "fee_account_id": {nil, out.FeeAccountID.String()},
		"payout_account_id": {nil, ptr(out.PayoutAccountID)}, "disputes_account_id": {nil, ptr(out.DisputesAccountID)},
		"dispute_loss_account_id": {nil, ptr(out.DisputeLossAccountID)}}
	action := "psp_account.created"
	if before != nil {
		action = "psp_account.changed"
		changes = map[string][2]any{"fee_account_id": {before.FeeAccountID.String(), out.FeeAccountID.String()},
			"payout_account_id": {ptr(before.PayoutAccountID), ptr(out.PayoutAccountID)}, "disputes_account_id": {ptr(before.DisputesAccountID), ptr(out.DisputesAccountID)},
			"dispute_loss_account_id": {ptr(before.DisputeLossAccountID), ptr(out.DisputeLossAccountID)}}
	}
	v := out.Version
	return out, l.u.event(ctx, action, "psp_account", out.MoneyAccountID, &v, changes)
}

func (l pspLedger) Swept(ctx context.Context, moneyAccount uuid.UUID, through, at time.Time, why string) error {
	if err := l.u.check(); err != nil {
		return err
	}
	p := sqlcdb.SweptPSPAccountParams{SweptAt: at, SweepError: opt(clip(why)), MoneyAccountID: moneyAccount}
	if why == "" {
		p.SweptThrough = pgDate(&through)
	}
	n, err := l.u.q.SweptPSPAccount(ctx, p)
	if err != nil {
		return mapError(err)
	}
	if n == 0 {
		return ports.ErrNotFound
	}
	return l.u.event(ctx, "psp_account.swept", "psp_account", moneyAccount, nil, map[string][2]any{
		"swept_through": {nil, nilIfEmpty(dayOf(&through))}, "sweep_error": {nil, nilIfEmpty(clip(why))}})
}

type pspTxnRow = sqlcdb.PSPTxnRow

func toPSPTxn(r pspTxnRow) storage.PSPTxn {
	return storage.PSPTxn{ID: r.ID, ConnectorID: r.ConnectorID, TxnID: r.TxnID, Type: r.Type, Category: r.Category, SourceKind: str(r.SourceKind),
		SourceID: str(r.SourceID), Currency: r.Currency, Gross: storage.Decimal(r.Gross), Fee: storage.Decimal(r.Fee), Net: storage.Decimal(r.Net),
		AvailableOn: nullDate(r.AvailableOn), PayoutID: str(r.PayoutID), ProviderAt: r.ProviderAt, SeenAt: r.SeenAt}
}

func (l pspLedger) RecordTxn(ctx context.Context, n storage.NewPSPTxn) (storage.PSPTxn, bool, error) {
	if err := l.u.check(); err != nil {
		return storage.PSPTxn{}, false, err
	}
	r, err := l.u.q.InsertPSPTxn(ctx, sqlcdb.InsertPSPTxnParams{ConnectorID: n.ConnectorID, TxnID: n.TxnID, Type: n.Type, Category: n.Category,
		SourceKind: opt(n.SourceKind), SourceID: opt(n.SourceID), Currency: n.Currency, Gross: string(n.Gross), Fee: string(n.Fee), Net: string(n.Net),
		AvailableOn: pgDate(n.AvailableOn), PayoutID: opt(n.PayoutID), ProviderAt: n.ProviderAt, SeenAt: n.SeenAt})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.PSPTxn{}, false, nil
	}
	if err != nil {
		return storage.PSPTxn{}, false, mapError(err)
	}
	t := toPSPTxn(pspTxnRow(r))
	return t, true, l.u.event(ctx, "psp_txn.recorded", "psp_txn", t.ID, nil, map[string][2]any{"connector_id": {nil, t.ConnectorID.String()},
		"txn_id": {nil, t.TxnID}, "category": {nil, t.Category}, "currency": {nil, t.Currency}, "gross": {nil, string(t.Gross)}, "fee": {nil, string(t.Fee)}})
}

func (l pspLedger) Txn(ctx context.Context, connector uuid.UUID, txnID string) (storage.PSPTxn, error) {
	if err := l.u.check(); err != nil {
		return storage.PSPTxn{}, err
	}
	r, err := l.u.q.PSPTxn(ctx, sqlcdb.PSPTxnParams{ConnectorID: connector, TxnID: txnID})
	if err != nil {
		return storage.PSPTxn{}, mapError(err)
	}
	return toPSPTxn(r), nil
}

func (l pspLedger) TxnOfSource(ctx context.Context, connector uuid.UUID, kind, id string) (storage.PSPTxn, error) {
	if err := l.u.check(); err != nil {
		return storage.PSPTxn{}, err
	}
	r, err := l.u.q.PSPTxnOfSource(ctx, sqlcdb.PSPTxnOfSourceParams{ConnectorID: connector, SourceKind: &kind, SourceID: &id})
	if err != nil {
		return storage.PSPTxn{}, mapError(err)
	}
	return toPSPTxn(pspTxnRow(r)), nil
}

func (l pspLedger) TxnsOfPayout(ctx context.Context, connector uuid.UUID, payoutID string) ([]storage.PSPTxn, error) {
	if err := l.u.check(); err != nil {
		return nil, err
	}
	rows, err := l.u.q.PSPTxnsOfPayout(ctx, sqlcdb.PSPTxnsOfPayoutParams{ConnectorID: connector, PayoutID: &payoutID})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.PSPTxn, 0, len(rows))
	for _, r := range rows {
		out = append(out, toPSPTxn(pspTxnRow(r)))
	}
	return out, nil
}

func (l pspLedger) SettleTxn(ctx context.Context, connector uuid.UUID, txnID, payoutID string) (bool, error) {
	if err := l.u.check(); err != nil {
		return false, err
	}
	id, err := l.u.q.SettlePSPTxn(ctx, sqlcdb.SettlePSPTxnParams{PayoutID: &payoutID, ConnectorID: connector, TxnID: txnID})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, mapError(err)
	}
	return true, l.u.event(ctx, "psp_txn.settled", "psp_txn", id, nil, map[string][2]any{"payout_id": {nil, payoutID}})
}

type pspPayoutRow = sqlcdb.PSPPayoutRow

func toPSPPayout(r pspPayoutRow) storage.PSPPayout {
	return storage.PSPPayout{ID: r.ID, ConnectorID: r.ConnectorID, PayoutID: r.PayoutID, Currency: r.Currency, Amount: storage.Decimal(r.Amount), Status: r.Status,
		ArrivalDate: nullDate(r.ArrivalDate), Automatic: r.Automatic, ProviderAt: r.ProviderAt, SeenAt: r.SeenAt}
}

func (l pspLedger) RecordPayout(ctx context.Context, n storage.NewPSPPayout) (storage.PSPPayout, bool, error) {
	if err := l.u.check(); err != nil {
		return storage.PSPPayout{}, false, err
	}
	r, err := l.u.q.InsertPSPPayout(ctx, sqlcdb.InsertPSPPayoutParams{ConnectorID: n.ConnectorID, PayoutID: n.PayoutID, Currency: n.Currency, Amount: string(n.Amount),
		Status: n.Status, ArrivalDate: pgDate(n.ArrivalDate), Automatic: n.Automatic, ProviderAt: n.ProviderAt, SeenAt: n.SeenAt})
	switch {
	case err == nil:
		p := toPSPPayout(pspPayoutRow(r))
		return p, true, l.u.event(ctx, "psp_payout.recorded", "psp_payout", p.ID, nil, map[string][2]any{"connector_id": {nil, p.ConnectorID.String()},
			"payout_id": {nil, p.PayoutID}, "amount": {nil, string(p.Amount)}, "currency": {nil, p.Currency}, "status": {nil, p.Status}})
	case !errors.Is(err, pgx.ErrNoRows):
		return storage.PSPPayout{}, false, mapError(err)
	}
	before, err := l.Payout(ctx, n.ConnectorID, n.PayoutID)
	if err != nil {
		return storage.PSPPayout{}, false, err
	}
	u, err := l.u.q.UpdatePSPPayout(ctx, sqlcdb.UpdatePSPPayoutParams{Status: n.Status, ArrivalDate: pgDate(n.ArrivalDate), Automatic: n.Automatic,
		ConnectorID: n.ConnectorID, PayoutID: n.PayoutID})
	if errors.Is(err, pgx.ErrNoRows) {
		return before, false, nil // nothing changed
	}
	if err != nil {
		return storage.PSPPayout{}, false, mapError(err)
	}
	p := toPSPPayout(pspPayoutRow(u))
	return p, true, l.u.event(ctx, "psp_payout.updated", "psp_payout", p.ID, nil, map[string][2]any{"status": {before.Status, p.Status},
		"arrival_date": {nilIfEmpty(dayOf(before.ArrivalDate)), nilIfEmpty(dayOf(p.ArrivalDate))}, "automatic": {before.Automatic, p.Automatic}})
}

func (l pspLedger) Payout(ctx context.Context, connector uuid.UUID, payoutID string) (storage.PSPPayout, error) {
	if err := l.u.check(); err != nil {
		return storage.PSPPayout{}, err
	}
	r, err := l.u.q.PSPPayout(ctx, sqlcdb.PSPPayoutParams{ConnectorID: connector, PayoutID: payoutID})
	if err != nil {
		return storage.PSPPayout{}, mapError(err)
	}
	return toPSPPayout(r), nil
}

func (l pspLedger) Payouts(ctx context.Context, connector uuid.UUID, limit int) ([]storage.PSPPayout, error) {
	if err := l.u.check(); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > math.MaxInt32 {
		limit = 100
	}
	rows, err := l.u.q.PSPPayouts(ctx, sqlcdb.PSPPayoutsParams{ConnectorID: connector, MaxRows: int32(limit)}) // 1 to MaxInt32, checked above
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.PSPPayout, 0, len(rows))
	for _, r := range rows {
		out = append(out, toPSPPayout(pspPayoutRow(r)))
	}
	return out, nil
}

func (l pspLedger) RecordBalance(ctx context.Context, connector uuid.UUID, currency string, amount storage.Decimal, at time.Time) (storage.PSPBalance, error) {
	if err := l.u.check(); err != nil {
		return storage.PSPBalance{}, err
	}
	r, err := l.u.q.InsertPSPBalance(ctx, sqlcdb.InsertPSPBalanceParams{ConnectorID: connector, Currency: currency, Amount: string(amount), TakenAt: at})
	if err != nil {
		return storage.PSPBalance{}, mapError(err)
	}
	b := storage.PSPBalance{ID: r.ID, ConnectorID: r.ConnectorID, Currency: r.Currency, Amount: storage.Decimal(r.Amount), TakenAt: r.TakenAt}
	return b, l.u.event(ctx, "psp_balance.recorded", "psp_balance", b.ID, nil, map[string][2]any{"connector_id": {nil, connector.String()},
		"currency": {nil, currency}, "amount": {nil, string(amount)}})
}

func (l pspLedger) LatestBalance(ctx context.Context, connector uuid.UUID, currency string) (storage.PSPBalance, error) {
	if err := l.u.check(); err != nil {
		return storage.PSPBalance{}, err
	}
	r, err := l.u.q.LatestPSPBalance(ctx, sqlcdb.LatestPSPBalanceParams{ConnectorID: connector, Currency: currency})
	if err != nil {
		return storage.PSPBalance{}, mapError(err)
	}
	return storage.PSPBalance{ID: r.ID, ConnectorID: r.ConnectorID, Currency: r.Currency, Amount: storage.Decimal(r.Amount), TakenAt: r.TakenAt}, nil
}

func (l pspLedger) DisputeTxns(ctx context.Context, connector uuid.UUID) ([]storage.PSPTxn, error) {
	if err := l.u.check(); err != nil {
		return nil, err
	}
	rows, err := l.u.q.PSPDisputeTxns(ctx, connector)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.PSPTxn, 0, len(rows))
	for _, r := range rows {
		out = append(out, toPSPTxn(pspTxnRow(r)))
	}
	return out, nil
}

func toDisputeLoss(r sqlcdb.InsertPSPDisputeLossRow) storage.PSPDisputeLoss {
	return storage.PSPDisputeLoss{ID: r.ID, ConnectorID: r.ConnectorID, DisputeID: r.DisputeID, DocumentID: r.DocumentID, Currency: r.Currency, Amount: storage.Decimal(r.Amount),
		AcceptedOverstatedRevenue: r.AcceptedOverstatedRevenue, Reason: str(r.Reason), CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt}
}

func (l pspLedger) RecordDisputeLoss(ctx context.Context, n storage.NewPSPDisputeLoss) (storage.PSPDisputeLoss, error) {
	if err := l.u.check(); err != nil {
		return storage.PSPDisputeLoss{}, err
	}
	r, err := l.u.q.InsertPSPDisputeLoss(ctx, sqlcdb.InsertPSPDisputeLossParams{ConnectorID: n.ConnectorID, DisputeID: n.DisputeID, DocumentID: n.DocumentID, Currency: n.Currency,
		Amount: string(n.Amount), AcceptedOverstatedRevenue: n.AcceptedOverstatedRevenue, Reason: opt(clip(n.Reason)), CreatedBy: l.u.actor(ctx), CreatedAt: n.At})
	if err != nil {
		return storage.PSPDisputeLoss{}, mapError(err)
	}
	d := toDisputeLoss(r)
	return d, l.u.event(ctx, "psp_dispute_loss.prepared", "psp_dispute_loss", d.ID, nil, map[string][2]any{"connector_id": {nil, d.ConnectorID.String()},
		"dispute_id": {nil, d.DisputeID}, "document_id": {nil, d.DocumentID.String()}, "amount": {nil, string(d.Amount)}, "currency": {nil, d.Currency},
		"accepted_overstated_revenue": {nil, d.AcceptedOverstatedRevenue}, "reason": {nil, nilIfEmpty(d.Reason)}})
}

func (l pspLedger) DisputeLosses(ctx context.Context, connector uuid.UUID) ([]storage.PSPDisputeLoss, error) {
	if err := l.u.check(); err != nil {
		return nil, err
	}
	rows, err := l.u.q.PSPDisputeLosses(ctx, connector)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.PSPDisputeLoss, 0, len(rows))
	for _, r := range rows {
		out = append(out, toDisputeLoss(sqlcdb.InsertPSPDisputeLossRow(r)))
	}
	return out, nil
}
