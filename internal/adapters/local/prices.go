//go:build local

package local

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/cognizhi/fyniq/internal/ports"
)

// PriceFiles is the local price catalogue (AI-85, ADR-T4): a directory holding model_prices_and_context_window.json
// and LICENSE, in LiteLLM's formats, synthetic or copied there by a developer. Without them the daily read finds no
// catalogue and every model stays unpriced, as it would with no source.
type PriceFiles struct {
	Dir string
}

var _ ports.PriceCatalogue = PriceFiles{}

const priceCatalogueFile = "model_prices_and_context_window.json"

// Fetch implements ports.PriceCatalogue.
func (f PriceFiles) Fetch(context.Context) (ports.PriceDocument, error) {
	body, err := os.ReadFile(filepath.Join(f.Dir, priceCatalogueFile)) // #nosec G304 -- the configured directory: local mode only
	if errors.Is(err, fs.ErrNotExist) {
		return ports.PriceDocument{}, ports.ErrNoCatalogue
	}
	if err != nil {
		return ports.PriceDocument{}, err
	}
	licence, err := os.ReadFile(filepath.Join(f.Dir, "LICENSE")) // #nosec G304 -- the configured directory: local mode only
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return ports.PriceDocument{}, err
	}
	return ports.PriceDocument{Source: "litellm", URL: "file://" + filepath.ToSlash(filepath.Join(f.Dir, priceCatalogueFile)),
		Path: priceCatalogueFile, Body: body, LicenceURL: "file://" + filepath.ToSlash(filepath.Join(f.Dir, "LICENSE")), Licence: licence}, nil
}
