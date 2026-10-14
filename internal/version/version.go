// Package version holds the build's version, commit and build time. The release build stamps
// them with -ldflags -X (deploy/docker/Dockerfile.go-service); /version serves them (NFR-IMG-06,
// REL-05).
package version

// Stamped at build time; the defaults mark an unstamped developer build.
var (
	Version   = "0.0.0-dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

// Info is the build identity a service reports.
type Info struct {
	Service   string `json:"service"`
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time"`
	Flavour   string `json:"flavour"`
}
