package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// fxRates are the organisation's exchange rates and its reads of the daily feed (FR-GL-05, ENG-40; PRD-001 rev 0.70).
type fxRates struct{ u *unitOfWork }

func (u *unitOfWork) FXRates() storage.FXRates { return fxRates{u} }

var _ storage.FeedRates = (*Store)(nil)

func fxRateOf(r sqlcdb.GetFXRateRow) storage.FXRate {
	out := storage.FXRate{ID: r.ID, EnteredBy: r.EnteredBy, EnteredAt: r.EnteredAt, WithdrawnBy: r.WithdrawnBy, WithdrawnAt: r.WithdrawnAt,
		WithdrawnReason: r.WithdrawnReason, NewFXRate: storage.NewFXRate{Kind: storage.FXRateKind(r.Kind), Currency: r.Currency, On: r.OnDate,
			Rate: storage.Decimal(r.Rate), Quote: storage.Decimal(r.Quote), QuoteForm: r.QuoteForm}}
	if r.Note != nil {
		out.Note = *r.Note
	}
	return out
}

func (f fxRates) Enter(ctx context.Context, r storage.NewFXRate) (storage.FXRate, error) {
	if err := f.u.check(); err != nil {
		return storage.FXRate{}, err
	}
	var note *string
	if r.Note != "" {
		note = &r.Note
	}
	row, err := f.u.q.InsertFXRate(ctx, sqlcdb.InsertFXRateParams{Kind: string(r.Kind), Currency: r.Currency, OnDate: r.On, Rate: string(r.Rate),
		Quote: string(r.Quote), QuoteForm: r.QuoteForm, Note: note, EnteredBy: f.u.actor(ctx), EnteredAt: f.u.store.clock.Now()})
	if err != nil {
		return storage.FXRate{}, mapError(err)
	}
	out := fxRateOf(sqlcdb.GetFXRateRow(row))
	if err := f.u.event(ctx, "fx_rate.entered", "fx_rate", out.ID, nil, map[string][2]any{
		"kind": {nil, string(out.Kind)}, "currency": {nil, out.Currency}, "on_date": {nil, day(out.On)}, "rate": {nil, string(out.Rate)},
		"quote": {nil, string(out.Quote)}, "quote_form": {nil, out.QuoteForm}, "note": {nil, out.Note}}); err != nil {
		return storage.FXRate{}, err
	}
	return out, nil
}

func (f fxRates) Withdraw(ctx context.Context, id uuid.UUID, reason string) (storage.FXRate, error) {
	if err := f.u.check(); err != nil {
		return storage.FXRate{}, err
	}
	was, err := f.Get(ctx, id)
	if err != nil {
		return storage.FXRate{}, err
	}
	if was.WithdrawnAt != nil {
		return storage.FXRate{}, &storage.Error{Code: storage.CodeFXRate, Message: "the rate is withdrawn already"}
	}
	by := f.u.actor(ctx)
	row, err := f.u.q.WithdrawFXRate(ctx, sqlcdb.WithdrawFXRateParams{ID: id, WithdrawnBy: &by, WithdrawnAt: ptr(f.u.store.clock.Now()),
		WithdrawnReason: &reason})
	if err != nil {
		return storage.FXRate{}, mapError(err)
	}
	out := fxRateOf(sqlcdb.GetFXRateRow(row))
	if err := f.u.event(ctx, "fx_rate.withdrawn", "fx_rate", out.ID, nil, map[string][2]any{
		"withdrawn_by": {nil, by}, "withdrawn_reason": {nil, reason}}); err != nil {
		return storage.FXRate{}, err
	}
	return out, nil
}

func (f fxRates) Get(ctx context.Context, id uuid.UUID) (storage.FXRate, error) {
	if err := f.u.check(); err != nil {
		return storage.FXRate{}, err
	}
	row, err := f.u.q.GetFXRate(ctx, id)
	if err != nil {
		return storage.FXRate{}, mapError(err)
	}
	return fxRateOf(row), nil
}

func (f fxRates) List(ctx context.Context, x storage.FXRateFilter) ([]storage.FXRate, error) {
	if err := f.u.check(); err != nil {
		return nil, err
	}
	rows, err := f.u.q.ListFXRates(ctx, sqlcdb.ListFXRatesParams{Currency: x.Currency, Kind: string(x.Kind), FromOn: optDate(x.From),
		ToOn: optDate(x.To), Withdrawn: x.Withdrawn})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.FXRate, 0, len(rows))
	for _, r := range rows {
		out = append(out, fxRateOf(sqlcdb.GetFXRateRow(r)))
	}
	return out, nil
}

func (f fxRates) Feed(ctx context.Context, base, currency string, from, to time.Time) ([]storage.FeedRate, error) {
	if err := f.u.check(); err != nil {
		return nil, err
	}
	rows, err := f.u.q.ListFeedRates(ctx, sqlcdb.ListFeedRatesParams{Base: base, Currency: currency, FromOn: from, ToOn: to})
	if err != nil {
		return nil, mapError(err)
	}
	return feedRatesOf(rows), nil
}

func feedRatesOf(rows []sqlcdb.FyniqFxFeedRates) []storage.FeedRate {
	out := make([]storage.FeedRate, 0, len(rows))
	for _, r := range rows {
		out = append(out, storage.FeedRate{Base: r.BaseCurrency, Currency: r.Currency, On: r.OnDate, Rate: storage.Decimal(r.Rate),
			Quote: storage.Decimal(r.Quote), QuoteForm: r.QuoteForm, Feed: r.Feed, RecordedAt: r.RecordedAt})
	}
	return out
}

// RecordFeedRate implements storage.FeedRates.
func (s *Store) RecordFeedRate(ctx context.Context, r storage.FeedRate) (bool, error) {
	added, err := s.q().RecordFeedRate(ctx, sqlcdb.RecordFeedRateParams{Base: r.Base, Currency: r.Currency, OnDate: r.On, Rate: string(r.Rate),
		Quote: string(r.Quote), QuoteForm: r.QuoteForm, Feed: r.Feed})
	return added, mapError(err)
}

// FeedDays implements storage.FeedRates.
func (s *Store) FeedDays(ctx context.Context, base string, from, to time.Time) ([]time.Time, error) {
	days, err := s.q().FeedDays(ctx, sqlcdb.FeedDaysParams{Base: base, FromOn: from, ToOn: to})
	return days, mapError(err)
}

// optDate is a date that narrows a query, or null for the zero time.
func optDate(t time.Time) pgtype.Date {
	if t.IsZero() {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: t, Valid: true}
}
