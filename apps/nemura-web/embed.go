// Package nemuraweb carries Nemura's built web app into the binary (PRD-002 NM-ARC-02: a static Go binary with the web app; design D11).
// embedded/ holds a committed placeholder so `go build` works with no web build; the image's web stage (Dockerfile.go-service) puts
// the Vite build over it, and a release image is never built with the placeholder.
package nemuraweb

import (
	"embed"
	"io/fs"
	"strings"
)

//go:embed all:embedded
var files embed.FS

// Files is the app's files, rooted at its index.html.
func Files() fs.FS {
	sub, err := fs.Sub(files, "embedded")
	if err != nil {
		panic(err) // the directory is embedded: it exists
	}
	return sub
}

// IsPlaceholder reports whether f holds only the committed placeholder, not a build of the app.
func IsPlaceholder(f fs.FS) bool {
	b, err := fs.ReadFile(f, "index.html")
	return err != nil || strings.Contains(string(b), "nemura-web-placeholder")
}
