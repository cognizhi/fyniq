package ports

import (
	"context"
	"errors"
)

// PriceCatalogue is a public model price catalogue Fyniq reads daily in the background, never when a page loads (PRD-001
// AI-82, AI-85; p1-ai-providers design D4). Production: LiteLLM's model_prices_and_context_window.json and its
// repository's licence, through the platform's egress guard. Local: a directory holding the two files, synthetic or
// copied by a developer.
type PriceCatalogue interface {
	// Fetch downloads the catalogue and its licence as they are now.
	Fetch(ctx context.Context) (PriceDocument, error)
}

// PriceDocument is a downloaded catalogue: where it came from, its bytes as downloaded, and its licence's.
type PriceDocument struct {
	Source string // "litellm"
	URL    string
	Path   string // the catalogue's path in its repository, which its licence check reads
	Body   []byte
	// LicenceURL and Licence are the repository's licence as downloaded with it (AI-85).
	LicenceURL string
	Licence    []byte
}

// ErrNoCatalogue is a price catalogue that is not there: none configured, or a local directory without one.
var ErrNoCatalogue = errors.New("no price catalogue is available")
