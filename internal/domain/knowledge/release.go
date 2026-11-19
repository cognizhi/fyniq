package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Release is a platform release's knowledge, validated and ready for the migrator to load (AI-50, AI-56): its pages and the
// guide they are compiled from, stamped with the release's version and the page format, and one digest over all of it.
type Release struct {
	Version string
	Schema  string
	Digest  string
	Pages   []Page
	Guide   []Guide
}

// NewRelease lints a corpus and stamps it. A corpus with any lint error is refused, so what is loaded is what the
// repository's own `make kb-lint` accepted; the findings are in the error.
func NewRelease(version string, c Corpus) (Release, error) {
	var errs []string
	for _, f := range Lint(c) {
		if !f.Warning {
			errs = append(errs, f.String())
		}
	}
	if len(errs) > 0 {
		return Release{}, fmt.Errorf("the knowledge base does not pass its lint:\n  %s", strings.Join(errs, "\n  "))
	}
	var parts []string
	for _, p := range c.Pages {
		parts = append(parts, "page "+p.File+" "+p.Digest)
	}
	for _, g := range c.Guide {
		parts = append(parts, "guide "+g.Path+" "+g.SHA256)
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(Schema + "\n" + strings.Join(parts, "\n")))
	return Release{Version: version, Schema: Schema, Digest: hex.EncodeToString(sum[:]), Pages: c.Pages, Guide: c.Guide}, nil
}
