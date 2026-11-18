// Command scanner is the private scanner service (FR-DOC-08, SEC-24; PRD-001 rev 0.247): POST /scan asks clamd, beside
// it in the same Cloud Run service (FYNIQ_CLAMD_ADDR, 127.0.0.1:3310 by default), whether a file is clean, for the api
// and the worker only. It holds no database role and no secret, and keeps nothing of a file.
package main

import (
	"cmp"
	"fmt"
	"os"

	"github.com/cognizhi/fyniq/internal/adapters/clamav"
	"github.com/cognizhi/fyniq/internal/app"
	"github.com/cognizhi/fyniq/internal/httpserver"
	"github.com/cognizhi/fyniq/internal/scanservice"
)

func main() {
	os.Exit(app.Serve("scanner", func(s *httpserver.Server, deps app.Deps) error {
		clamd := clamav.Scanner{Addr: cmp.Or(os.Getenv("FYNIQ_CLAMD_ADDR"), "127.0.0.1:3310")}
		s.AddReadiness("clamd", clamd.Ping)
		tokens, err := callers(deps.Clock)
		if err != nil {
			fmt.Fprintln(os.Stderr, "scanner: scanning nothing:", err)
			return nil
		}
		s.Handle("POST /scan", scanservice.Handler{Scanner: clamd, Tokens: tokens})
		return nil
	}))
}
