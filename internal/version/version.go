// Package version holds the build's version, commit and build time. The release build stamps
// them with -ldflags -X (deploy/docker/Dockerfile.go-service); /version serves them (NFR-IMG-06,
// REL-05).
package version

// Stamped at build time; the defaults mark an unstamped developer build.
var (
	Version   = "0.0.0-dev"
	Commit    = "unknown"
	BuildTime = "unknown"
	// Release is the release tag, such as v1.2.0, stamped only by the build of a published release's images (RELEASE in
	// deploy/docker/Dockerfile.go-service); every other build leaves it empty. The Compliance Center shows the checks
	// that ran in CI as passed in this release, with the evidence attached to it (PRD-001 CMP-01, §14.1 item 3).
	Release = ""
	// Repository is where the releases and their evidence are published.
	Repository = "https://github.com/cognizhi/fyniq"
)

// Info is the build identity a service reports.
type Info struct {
	Service   string `json:"service"`
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time"`
	Flavour   string `json:"flavour"`
	Release   string `json:"release,omitempty"`
}
