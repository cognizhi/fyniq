package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

var _ storage.AIPriceCatalogues = (*Store)(nil)

func toAIPriceCatalogue(r sqlcdb.FyniqAiPriceCatalogues) storage.AIPriceCatalogue {
	c := storage.AIPriceCatalogue{ID: r.ID, Source: r.Source, URL: r.Url, FetchedAt: r.FetchedAt, SHA256: r.Sha256, Size: r.Size,
		LicenceURL: r.LicenceUrl, LicenceSHA256: r.LicenceSha256, Outcome: r.Outcome, Entries: int(r.Entries), Document: r.Document,
		RecordedAt: r.RecordedAt}
	if r.Licence != nil {
		c.Licence = *r.Licence
	}
	if r.Failure != nil {
		c.Failure = *r.Failure
	}
	return c
}

// RecordAIPriceCatalogue implements storage.AIPriceCatalogues: through record_ai_price_catalogue, the one way to write
// the global table.
func (s *Store) RecordAIPriceCatalogue(ctx context.Context, c storage.AIPriceCatalogue) (storage.AIPriceCatalogue, error) {
	id, err := s.q().RecordAIPriceCatalogue(ctx, sqlcdb.RecordAIPriceCatalogueParams{Source: c.Source, Url: c.URL, FetchedAt: c.FetchedAt,
		Sha256: c.SHA256, Size: c.Size, Licence: optional(c.Licence), LicenceUrl: c.LicenceURL, LicenceSha256: c.LicenceSHA256,
		Outcome: c.Outcome, Failure: optional(c.Failure), Entries: int32(min(max(c.Entries, 0), 1<<30)), Document: c.Document}) // #nosec G115 -- capped
	if err != nil {
		return storage.AIPriceCatalogue{}, mapError(err)
	}
	row, err := s.q().GetAIPriceCatalogue(ctx, id)
	if err != nil {
		return storage.AIPriceCatalogue{}, mapError(err)
	}
	return toAIPriceCatalogue(row), nil
}

func (u *unitOfWork) AIPrices() storage.AIPrices { return aiPrices{u} }

type aiPrices struct{ u *unitOfWork }

func toAIPrice(r sqlcdb.FyniqAiPrice) storage.AIPrice {
	p := storage.AIPrice{ID: r.ID, ConnectionID: r.ConnectionID, Model: r.Model, Number: int(r.Number), Source: r.Source, CatalogueID: r.CatalogueID,
		Match: r.Match, Currency: r.Currency, Ignored: r.Ignored, Status: r.Status, RecordedBy: r.RecordedBy, RecordedAt: r.RecordedAt,
		Approval: r.ApprovalID, DecidedAt: r.DecidedAt}
	for _, s := range []struct {
		to   *string
		from *string
	}{{&p.CatalogueKey, r.CatalogueKey}, {&p.Review, r.Review}, {&p.ReviewDetail, r.ReviewDetail}, {&p.DecidedBy, r.DecidedBy}} {
		if s.from != nil {
			*s.to = *s.from
		}
	}
	if p.Ignored == nil {
		p.Ignored = []string{}
	}
	return p
}

// withRates reads the rates of the prices.
func (r aiPrices) withRates(ctx context.Context, rows []sqlcdb.FyniqAiPrice) ([]storage.AIPrice, error) {
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	rates, err := r.u.q.AIPriceRates(ctx, ids)
	if err != nil {
		return nil, mapError(err)
	}
	by := map[uuid.UUID][]storage.AIPriceRate{}
	for _, rate := range rates {
		by[rate.PriceID] = append(by[rate.PriceID], storage.AIPriceRate{Kind: rate.Kind, Above: rate.Above, Unit: rate.Unit, Amount: rate.Amount})
	}
	out := make([]storage.AIPrice, 0, len(rows))
	for _, row := range rows {
		p := toAIPrice(row)
		p.Rates = by[row.ID]
		out = append(out, p)
	}
	return out, nil
}

func (r aiPrices) Current(ctx context.Context, connection uuid.UUID) ([]storage.AIPrice, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.CurrentAIPrices(ctx, connection)
	if err != nil {
		return nil, mapError(err)
	}
	return r.withRates(ctx, rows)
}

func (r aiPrices) Get(ctx context.Context, id uuid.UUID) (storage.AIPrice, error) {
	if err := r.u.check(); err != nil {
		return storage.AIPrice{}, err
	}
	row, err := r.u.q.GetAIPrice(ctx, id)
	if err != nil {
		return storage.AIPrice{}, mapError(err)
	}
	out, err := r.withRates(ctx, []sqlcdb.FyniqAiPrice{row})
	if err != nil {
		return storage.AIPrice{}, err
	}
	return out[0], nil
}

// priceChange is a version as its audit event shows it.
func priceChange(p storage.AIPrice) map[string]any {
	rates := make([]map[string]any, 0, len(p.Rates))
	for _, r := range p.Rates {
		rates = append(rates, map[string]any{"kind": r.Kind, "above": r.Above, "unit": r.Unit, "amount": r.Amount})
	}
	out := map[string]any{"connection_id": p.ConnectionID.String(), "model": p.Model, "number": p.Number, "source": p.Source,
		"catalogue_key": p.CatalogueKey, "match": p.Match, "currency": p.Currency, "rates": rates, "status": p.Status,
		"review": p.Review, "review_detail": p.ReviewDetail}
	if p.Approval != nil {
		out["approval"] = p.Approval.String()
	}
	return out
}

func (r aiPrices) Record(ctx context.Context, p storage.AIPrice) (storage.AIPrice, error) {
	if err := r.u.check(); err != nil {
		return storage.AIPrice{}, err
	}
	ignored := p.Ignored
	if ignored == nil {
		ignored = []string{}
	}
	var row sqlcdb.FyniqAiPrice
	var err error
	if p.Approval != nil { // an Owner's price, as its approval executes (only the API may name one)
		row, err = r.u.q.InsertOwnerAIPrice(ctx, sqlcdb.InsertOwnerAIPriceParams{ConnectionID: p.ConnectionID, Model: p.Model, Currency: p.Currency,
			RecordedBy: p.RecordedBy, RecordedAt: p.RecordedAt, ApprovalID: p.Approval})
	} else {
		row, err = r.u.q.InsertAIPrice(ctx, sqlcdb.InsertAIPriceParams{ConnectionID: p.ConnectionID, Model: p.Model, Source: p.Source,
			CatalogueID: p.CatalogueID, CatalogueKey: optional(p.CatalogueKey), Match: p.Match, Currency: p.Currency, Ignored: ignored,
			Status: p.Status, Review: optional(p.Review), ReviewDetail: optional(p.ReviewDetail), RecordedBy: p.RecordedBy, RecordedAt: p.RecordedAt})
	}
	if err != nil {
		return storage.AIPrice{}, mapError(err)
	}
	for _, rate := range p.Rates {
		if err := r.u.q.InsertAIPriceRate(ctx, sqlcdb.InsertAIPriceRateParams{PriceID: row.ID, Kind: rate.Kind, Above: rate.Above, Unit: rate.Unit,
			Amount: rate.Amount}); err != nil {
			return storage.AIPrice{}, mapError(err)
		}
	}
	out := toAIPrice(row)
	out.Rates = p.Rates
	version := out.Number
	return out, r.u.event(ctx, "ai_price.changed", "ai_price", out.ID, &version, map[string][2]any{"price": {nil, priceChange(out)}})
}

func (r aiPrices) Checked(ctx context.Context, connection uuid.UUID, model, unpriced string, at time.Time) error {
	if err := r.u.check(); err != nil {
		return err
	}
	if len(unpriced) > 500 {
		unpriced = unpriced[:497] + "…"
	}
	n, err := r.u.q.CheckAIModelPrice(ctx, sqlcdb.CheckAIModelPriceParams{PriceCheckedAt: &at, Unpriced: optional(unpriced),
		ConnectionID: connection, Model: model})
	if err == nil && n == 0 {
		return ports.ErrNotFound
	}
	return mapError(err)
}

func (r aiPrices) Decide(ctx context.Context, id uuid.UUID, confirm bool, by string, at time.Time) (storage.AIPrice, error) {
	if err := r.u.check(); err != nil {
		return storage.AIPrice{}, err
	}
	status, action := "declined", "ai_price.declined"
	if confirm {
		status, action = "active", "ai_price.confirmed"
	}
	row, err := r.u.q.DecideAIPrice(ctx, sqlcdb.DecideAIPriceParams{Status: status, DecidedBy: &by, DecidedAt: &at, ID: id})
	if err != nil {
		return storage.AIPrice{}, mapError(err)
	}
	out, err := r.withRates(ctx, []sqlcdb.FyniqAiPrice{row})
	if err != nil {
		return storage.AIPrice{}, err
	}
	version := out[0].Number
	return out[0], r.u.event(ctx, action, "ai_price", id, &version, map[string][2]any{"status": {"pending", status}})
}

func (r aiPrices) Withdraw(ctx context.Context, id uuid.UUID, by string, at time.Time) (storage.AIPrice, error) {
	if err := r.u.check(); err != nil {
		return storage.AIPrice{}, err
	}
	row, err := r.u.q.DecideAIPrice(ctx, sqlcdb.DecideAIPriceParams{Status: "withdrawn", DecidedBy: &by, DecidedAt: &at, ID: id})
	if err != nil {
		return storage.AIPrice{}, mapError(err)
	}
	out, err := r.withRates(ctx, []sqlcdb.FyniqAiPrice{row})
	if err != nil {
		return storage.AIPrice{}, err
	}
	version := out[0].Number
	return out[0], r.u.event(ctx, "ai_price.withdrawn", "ai_price", id, &version, map[string][2]any{"status": {"active", "withdrawn"}})
}

func (r aiPrices) Catalogue(ctx context.Context, id uuid.UUID) (storage.AIPriceCatalogue, error) {
	if err := r.u.check(); err != nil {
		return storage.AIPriceCatalogue{}, err
	}
	row, err := r.u.q.GetAIPriceCatalogue(ctx, id)
	if err != nil {
		return storage.AIPriceCatalogue{}, mapError(err)
	}
	return toAIPriceCatalogue(row), nil
}

func (r aiPrices) LatestCatalogue(ctx context.Context, source string) (storage.AIPriceCatalogue, error) {
	if err := r.u.check(); err != nil {
		return storage.AIPriceCatalogue{}, err
	}
	row, err := r.u.q.LatestAIPriceCatalogue(ctx, source)
	if err != nil {
		return storage.AIPriceCatalogue{}, mapError(err)
	}
	return toAIPriceCatalogue(row), nil
}

func (r aiPrices) Catalogues(ctx context.Context, limit int) ([]storage.AIPriceCatalogue, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.AIPriceCatalogues(ctx, int32(min(max(limit, 1), 1000))) // #nosec G115 -- 1 to 1,000
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.AIPriceCatalogue, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAIPriceCatalogue(row))
	}
	return out, nil
}
