// Package litellm reads LiteLLM's public model price catalogue (PRD-001 AI-82, AI-85; p1-ai-providers design D4): the
// file model_prices_and_context_window.json at the root of BerriAI/litellm, MIT-licensed outside the repository's
// enterprise/ folder, and the repository's LICENSE, which the daily read checks before using the prices. Plain HTTPS
// through the platform's egress guard to the one host; never a provider's credential.
package litellm

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/cognizhi/fyniq/internal/ports"
)

// The catalogue and its licence, and the catalogue's path in the repository.
const (
	Host       = "raw.githubusercontent.com"
	Path       = "model_prices_and_context_window.json"
	URL        = "https://" + Host + "/BerriAI/litellm/main/" + Path
	LicenceURL = "https://" + Host + "/BerriAI/litellm/main/LICENSE"
)

// maxBody is the largest answer read: the catalogue is about 4 MB.
const maxBody = 32 << 20

// Catalogue reads the catalogue with HTTP, a client through the egress guard to Host only. URL and LicenceURL are
// the package's constants unless a test sets them.
type Catalogue struct {
	HTTP            *http.Client
	URL, LicenceURL string
}

var _ ports.PriceCatalogue = Catalogue{}

// Fetch implements ports.PriceCatalogue.
func (c Catalogue) Fetch(ctx context.Context) (ports.PriceDocument, error) {
	url, licenceURL := c.URL, c.LicenceURL
	if url == "" {
		url, licenceURL = URL, LicenceURL
	}
	body, err := c.get(ctx, url)
	if err != nil {
		return ports.PriceDocument{}, err
	}
	licence, err := c.get(ctx, licenceURL)
	if err != nil {
		return ports.PriceDocument{}, err
	}
	return ports.PriceDocument{Source: "litellm", URL: url, Path: Path, Body: body, LicenceURL: licenceURL, Licence: licence}, nil
}

func (c Catalogue) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("reading %s: the server answered %s", url, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", url, err)
	}
	if len(b) > maxBody {
		return nil, fmt.Errorf("reading %s: more than %d bytes", url, maxBody)
	}
	return b, nil
}
