// Command nemura is Nemura's one image (PRD-002 ADR-N1, NM-ARC-02): the optional people platform, with its own database
// and sign-in, in the same repository as Fyniq. The same static binary runs as each of its services, chosen by
// NEMURA_ROLE (NM-ARC-06): api, worker, executor, assistant, and the migrate job. Each role is its own service, with
// its own identity, database role and scaling, from one image and one digest.
//
//	NEMURA_ROLE=api nemura      the REST API (this release: the HTTP shell with /healthz, /readyz and /version)
//	nemura --version            what the build is
//	nemura healthcheck          probe the running role's /readyz (a distroless image has no shell or curl)
//
// The deployment's own settings keep their Fyniq names (FYNIQ_ENV, FYNIQ_BUSINESS_DATE, PORT) so the start-up checks of
// internal/app apply unchanged; Nemura's are NEMURA_*. A role that is unset, unknown or not built yet refuses to start
// with NEMURA-ROLE-01 and a plain fix.
package main

import (
	"fmt"
	"os"

	"github.com/cognizhi/fyniq/internal/app"
	"github.com/cognizhi/fyniq/internal/httpserver"
	"github.com/cognizhi/fyniq/internal/nemura/role"
)

func main() { os.Exit(run(os.Args[1:], os.Getenv)) }

// run starts the role the environment names, or reports why it cannot.
func run(args []string, getenv func(string) string) int {
	// The probe and the version need no role: they answer for any container of this image.
	if len(args) > 0 && (args[0] == "--version" || args[0] == "healthcheck") {
		return app.Serve("nemura", nil)
	}
	r, err := role.Parse(getenv(role.Env))
	if err != nil {
		fmt.Fprintf(os.Stderr, "nemura: %v\n", err)
		return 2
	}
	return app.Serve(r.Service(), func(s *httpserver.Server, _ app.Deps) error { return routes(r, s) })
}

// routes adds the role's own handlers. Every role answers /healthz, /readyz and /version from the shell; the API's
// operations arrive with the API foundation (p1-nemura-platform, task 4).
func routes(r role.Role, _ *httpserver.Server) error {
	switch r {
	case role.API:
		return nil
	}
	return fmt.Errorf("the %s role has no routes", r)
}
