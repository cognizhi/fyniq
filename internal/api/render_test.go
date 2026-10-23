//go:build local

package api

import (
	"context"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/render"
	"github.com/cognizhi/fyniq/internal/render/service"
	"github.com/cognizhi/fyniq/internal/render/typst"
)

// renderTokens accept "render-test" as the render class's token only.
type renderTokens struct{}

func (renderTokens) Token(string) (string, error) { return "render-test", nil }
func (renderTokens) Verify(token, class string) error {
	if token != "render-test" || class != render.Class {
		return ports.ErrUnauthorised
	}
	return nil
}

var (
	rendererOnce sync.Once
	renderer     render.Renderer
	rendererErr  error
)

// testRenderer is the render service in this process, as the api calls it: the real handler and Typst, behind HTTP,
// shared by every test of the package.
func testRenderer(t *testing.T) render.Renderer {
	t.Helper()
	rendererOnce.Do(func() {
		dir, err := os.MkdirTemp("", "fyniq-api-render-")
		if err != nil {
			rendererErr = err
			return
		}
		c, err := typst.New(os.Getenv("FYNIQ_TYPST"), dir)
		if err != nil {
			rendererErr = err
			return
		}
		srv := httptest.NewServer(service.New(c, renderTokens{})) // lives as long as the test binary
		renderer = &render.Client{URL: srv.URL, HTTP: srv.Client(), Token: func(context.Context) (string, error) { return "render-test", nil }}
	})
	if rendererErr != nil {
		t.Fatal(rendererErr) // make setup installs Typst; the tests never skip the render service
	}
	return renderer
}

// dropping is a render service that lays out a document without the particulars named: what publication's test
// documents exist to catch (FR-TPL-03).
type dropping struct {
	render.Renderer
	missing []string
}

func (d dropping) Render(ctx context.Context, r render.Request) (render.Response, error) {
	res, err := d.Renderer.Render(ctx, r)
	if err == nil && r.Check {
		res.Missing = d.missing
	}
	return res, err
}
