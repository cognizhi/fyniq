// Command render is the private render service (PRD-001 §12.3, rev 0.96; ADR-T1): POST /render compiles a template
// version and a document's data to PDF/A-2a and PDF/UA-1 with the pinned Typst binary, for the api and the worker
// only. It holds no database role and no secret; the render image (distroless/cc) ships Typst and the fonts.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/cognizhi/fyniq/internal/app"
	"github.com/cognizhi/fyniq/internal/httpserver"
	"github.com/cognizhi/fyniq/internal/render/service"
	"github.com/cognizhi/fyniq/internal/render/typst"
)

func main() {
	os.Exit(app.Serve("render", func(s *httpserver.Server, deps app.Deps) error {
		c, err := typst.New(os.Getenv("FYNIQ_TYPST"), "") // the image sets FYNIQ_TYPST=/typst; make dev uses mise's
		if err != nil {
			return err
		}
		s.AddReadiness("typst", func(ctx context.Context) error {
			_, err := c.Version(ctx)
			return err
		})
		tokens, err := callers(deps.Clock)
		if err != nil {
			fmt.Fprintln(os.Stderr, "render: serving no renders:", err)
			return nil
		}
		s.Handle("POST /render", service.New(c, tokens))
		return nil
	}))
}
