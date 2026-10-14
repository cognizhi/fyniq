// Command render produces PDFs with Typst (PRD-001 §12.2, the render image on distroless/cc).
// It is a shell until P1's documents; the image and its budget come with p0-ci-images-release.
package main

import (
	"os"

	"github.com/cognizhi/fyniq/internal/app"
)

func main() { os.Exit(app.Serve("render", nil)) }
