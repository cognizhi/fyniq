package knowledge_test

import (
	"testing"

	"github.com/cognizhi/fyniq/docs/user-guide"
	"github.com/cognizhi/fyniq/internal/domain/knowledge"
	"github.com/cognizhi/fyniq/kb"
)

// The release's own pages and guide, as they ship, lint clean in Go as scripts/kb.py finds them in make check, and the
// index shipped is the one the pages generate.
func TestTheShippedKnowledgeBaseLintsClean(t *testing.T) {
	c, err := knowledge.Load(kb.FS, userguide.FS)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Pages) < 90 || len(c.Guide) < 70 {
		t.Fatalf("read %d pages and %d guide pages: the release ships 90 and 77", len(c.Pages), len(c.Guide))
	}
	for _, f := range knowledge.Lint(c) {
		if !f.Warning {
			t.Errorf("%v", f)
		}
	}
	if c.Index != knowledge.RenderIndex(c.Pages) {
		t.Error("the shipped index is not the one the pages generate")
	}
}
