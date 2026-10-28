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
	Released      bool // (rev 0.151) its asset was voided, so the line can be registered again
}

// AssetSources are the lines registered assets came from. One is recorded with its asset, in the asset's unit of work,
// whose creation audits it (AUD-01); the database refuses a second live asset on a line, a line of a document that is
// not a posted bill or bank line at its current version, a line whose entry debits nothing to the account, an account
// that is not an active asset account of the register, any change but a release, and any deletion (CodeAsset).
type AssetSources interface {
	Add(ctx context.Context, s AssetSource) error
	// Of is an asset's source, released or not, or ports.ErrNotFound for an asset bought before the books start.
	Of(ctx context.Context, assetID uuid.UUID) (AssetSource, error)
	// OfDocument are the live assets registered from a document's lines, by line.
	OfDocument(ctx context.Context, sourceID uuid.UUID) ([]AssetSource, error)
	// List is every live asset's source.
	List(ctx context.Context) ([]AssetSource, error)
	// Release frees the line of an asset voided in the same unit of work (rev 0.151), so it can be registered again;
	// the source stays, released. The database refuses it while the asset is not voided (CodeAsset). An asset with no
	// live source (one bought before the books start) releases nothing.
	Release(ctx context.Context, assetID uuid.UUID) error
}
