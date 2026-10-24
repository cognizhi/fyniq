package postgres

import (
	"context"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// quoteOutcomes keeps the customers' answers to quotes and orders and their expiry (FR-AR-01): one per quote, never
// changed.
type quoteOutcomes struct{ u *unitOfWork }

func (u *unitOfWork) QuoteOutcomes() storage.QuoteOutcomes { return quoteOutcomes{u} }

func toQuoteOutcome(r sqlcdb.QuoteOutcomesOfRow) storage.QuoteOutcome {
	return storage.QuoteOutcome{RecordedBy: r.RecordedBy, RecordedAt: r.RecordedAt,
		NewQuoteOutcome: storage.NewQuoteOutcome{DocumentID: r.DocumentID, Outcome: r.Outcome, On: r.OnDate, Name: str(r.Name), Note: str(r.Note)}}
}

func (q quoteOutcomes) Record(ctx context.Context, n storage.NewQuoteOutcome) (storage.QuoteOutcome, error) {
	if err := q.u.check(); err != nil {
		return storage.QuoteOutcome{}, err
	}
	row, err := q.u.q.InsertQuoteOutcome(ctx, sqlcdb.InsertQuoteOutcomeParams{DocumentID: n.DocumentID, Outcome: n.Outcome, OnDate: dateOf(n.On),
		Name: opt(n.Name), Note: opt(n.Note), RecordedBy: q.u.actor(ctx)})
	if err != nil {
		return storage.QuoteOutcome{}, mapError(err)
	}
	out := toQuoteOutcome(sqlcdb.QuoteOutcomesOfRow(row))
	changes := map[string][2]any{"document_id": {nil, out.DocumentID.String()}, "outcome": {nil, out.Outcome}, "on": {nil, out.On.Format("2006-01-02")},
		"name": {nil, nilIfEmpty(out.Name)}, "note": {nil, nilIfEmpty(out.Note)}}
	if err := q.u.event(ctx, "quote_outcome.recorded", "quote_outcome", row.ID, nil, changes); err != nil {
		return storage.QuoteOutcome{}, err
	}
	return out, nil
}

func (q quoteOutcomes) Of(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]storage.QuoteOutcome, error) {
	if err := q.u.check(); err != nil {
		return nil, err
	}
	out := map[uuid.UUID]storage.QuoteOutcome{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.u.q.QuoteOutcomesOf(ctx, ids)
	if err != nil {
		return nil, mapError(err)
	}
	for _, r := range rows {
		out[r.DocumentID] = toQuoteOutcome(r)
	}
	return out, nil
}
