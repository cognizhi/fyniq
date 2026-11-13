//go:build local

package local

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cognizhi/fyniq/internal/ports"
)

// Locally the price catalogue is a directory holding LiteLLM's two files: without the catalogue there is none, and
// every model stays unpriced (AI-85, ADR-T4).
// req: AI-85
func TestThePriceCatalogueIsLocalFiles(t *testing.T) {
	dir := t.TempDir()
	f := PriceFiles{Dir: dir}
	if _, err := f.Fetch(context.Background()); !errors.Is(err, ports.ErrNoCatalogue) {
		t.Fatalf("an empty directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "model_prices_and_context_window.json"), []byte(`{"gpt-test": {}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err := f.Fetch(context.Background())
	if err != nil || doc.Source != "litellm" || doc.Path != "model_prices_and_context_window.json" || string(doc.Body) != `{"gpt-test": {}}` ||
		doc.Licence != nil || doc.URL == "" {
		t.Fatalf("the catalogue without its licence: %v %+v", err, doc)
	}
	if err := os.WriteFile(filepath.Join(dir, "LICENSE"), []byte("MIT License"), 0o600); err != nil {
		t.Fatal(err)
	}
	if doc, err = f.Fetch(context.Background()); err != nil || string(doc.Licence) != "MIT License" {
		t.Fatalf("with its licence: %v %+v", err, doc)
	}
}
