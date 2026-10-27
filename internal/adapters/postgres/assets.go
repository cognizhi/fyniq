package postgres

import (
	"context"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the sources of registered assets (FR-FA-01)

func (u *unitOfWork) AssetSources() storage.AssetSources { return assetSources{u} }

type assetSources struct{ u *unitOfWork }

func toAssetSource(r sqlcdb.FyniqFixedAssetSource) storage.AssetSource {
	return storage.AssetSource{AssetID: r.AssetID, SourceID: r.SourceID, SourceVersion: int(r.SourceVersion), Line: int(r.Line), AccountID: r.AccountID}
}

func (r assetSources) Add(ctx context.Context, s storage.AssetSource) error {
	if err := r.u.check(); err != nil {
		return err
	}
	return mapError(r.u.q.InsertAssetSource(ctx, sqlcdb.InsertAssetSourceParams{AssetID: s.AssetID, SourceID: s.SourceID,
		SourceVersion: small(s.SourceVersion), Line: small(s.Line), AccountID: s.AccountID}))
}

func (r assetSources) Of(ctx context.Context, assetID uuid.UUID) (storage.AssetSource, error) {
	if err := r.u.check(); err != nil {
		return storage.AssetSource{}, err
	}
	row, err := r.u.q.AssetSource(ctx, assetID)
	if err != nil {
		return storage.AssetSource{}, mapError(err)
	}
	return toAssetSource(row), nil
}

func (r assetSources) OfDocument(ctx context.Context, sourceID uuid.UUID) ([]storage.AssetSource, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.AssetSourcesOfDocument(ctx, sourceID)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.AssetSource, len(rows))
	for i, x := range rows {
		out[i] = toAssetSource(x)
	}
	return out, nil
}

func (r assetSources) List(ctx context.Context) ([]storage.AssetSource, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.AssetSources(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.AssetSource, len(rows))
	for i, x := range rows {
		out[i] = toAssetSource(x)
	}
	return out, nil
}

func (r ledger) OfSource(ctx context.Context, sourceID uuid.UUID) ([]storage.Entry, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	ids, err := r.u.q.EntryIDsOfSource(ctx, sourceID.String())
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.Entry, 0, len(ids))
	for _, id := range ids {
		e, err := r.Entry(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}
