package storage

import (
	"context"

	"github.com/google/uuid"
)

// AssetSource is the line a registered asset came from (FR-FA-01; p1-fixed-assets design D1): a line of a posted bill
// or categorised bank line, by its document, version and line number (from 1), and the asset account the line debited.
type AssetSource struct {
	AssetID       uuid.UUID
	SourceID      uuid.UUID
	SourceVersion int
	Line          int
	AccountID     uuid.UUID
}

// AssetSources are the lines registered assets came from. One is recorded with its asset, in the asset's unit of work,
// whose creation audits it (AUD-01); the database refuses a second asset on a line, a line of a document that is not a
// posted bill or bank line at its current version, a line whose entry debits nothing to the account, an account that
// is not an active asset account of the register, and any change or deletion (CodeAsset).
type AssetSources interface {
	Add(ctx context.Context, s AssetSource) error
	// Of is an asset's source, or ports.ErrNotFound for an asset bought before the books start.
	Of(ctx context.Context, assetID uuid.UUID) (AssetSource, error)
	// OfDocument are the assets registered from a document's lines, by line.
	OfDocument(ctx context.Context, sourceID uuid.UUID) ([]AssetSource, error)
	// List is every asset's source.
	List(ctx context.Context) ([]AssetSource, error)
}
