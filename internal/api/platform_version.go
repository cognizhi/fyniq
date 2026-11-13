package api

import (
	"cmp"
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/sheet"
)

// PlatformBuild is the build this API is (REL-05): the wiring passes internal/version's values, which this package never
// imports.
type PlatformBuild struct {
	Commit, BuildTime, Flavour string
}

// PlatformVersion is the platform's version and build, as the service's /version answers it (REL-05).
type PlatformVersion struct {
	Service   string `json:"service" doc:"The service that answered: api." example:"api"`
	Version   string `json:"version" doc:"The platform version, set by release-please (REL-02)." example:"0.12.0"`
	Commit    string `json:"commit" doc:"The git commit the build is of: quote it with the version when you report a problem." example:"3f2c1a9d"`
	BuildTime string `json:"build_time" doc:"When the build was made, RFC 3339." example:"2026-11-11T02:00:00Z"`
	Flavour   string `json:"flavour" doc:"The build's flavour: release, or local for local mode (ADR-T4)." example:"release"`
	Release   string `json:"release,omitempty" doc:"The release tag, for the build of a published release only." example:"v0.12.0"`
}

type platformVersionOutput struct{ Body PlatformVersion }

func registerPlatformVersion(a *API) {
	register(a, authz.GetPlatformVersion, huma.Operation{
		OperationID: "getPlatformVersion", Method: http.MethodGet, Path: "/v1/platform/version", Tags: []string{"Installation"},
		Summary: "Get the platform version",
		Description: "The platform version and the git commit this API was built from, as its /version answers (REL-05): the web app's " +
			"About dialog shows them, and every exported report carries the same in its footer. Public: it needs no credential. " +
			"Read-only; no side effects.",
		Security: []map[string][]string{{}}, // no credential: an empty requirement
	}, nil, func(context.Context, *struct{}) (*platformVersionOutput, error) {
		b := a.deps.Build
		return &platformVersionOutput{Body: PlatformVersion{Service: "api", Version: a.deps.Version, Commit: cmp.Or(b.Commit, "unknown"),
			BuildTime: cmp.Or(b.BuildTime, "unknown"), Flavour: cmp.Or(b.Flavour, "unknown"), Release: a.deps.Release.Tag}}, nil
	})
}

// stamp is the platform version and the build's commit, as every exported report names them (REL-05).
func (a *API) stamp() string { return sheet.Stamp(a.deps.Version, a.deps.Build.Commit) }

// footed is sheets with the footer every exported report carries: who prepared it, and when (REL-05).
func (a *API) footed(at time.Time, sheets ...sheet.Sheet) []sheet.Sheet {
	return sheet.Footed(sheets, sheet.Prepared(a.stamp(), at))
}
