package knowledge_test

import (
	"maps"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cognizhi/fyniq/docs/user-guide"
	"github.com/cognizhi/fyniq/internal/domain/knowledge"
	"github.com/cognizhi/fyniq/kb"
	"github.com/cognizhi/fyniq/packs"
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
	if err := c.LoadPacks(packs.FS); err != nil {
		t.Fatal(err)
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

// A page that declares the statutory pack version it read is flagged when a newer version of the pack appears, and the
// release's own pages that state statutory values declare the newest version today (kb-schema@2; AI-53). A pack version the
// page does not name, and one that does not exist, are findings too.
func TestAPageIsFlaggedWhenANewerPackVersionExists(t *testing.T) {
	c, err := knowledge.Load(kb.FS, userguide.FS)
	if err != nil {
		t.Fatal(err)
	}
	declared := map[string][]string{}
	for _, p := range c.Pages {
		if len(p.Packs) > 0 {
			declared[p.Key] = p.Packs
		}
	}
	if len(declared["compliance.gst"]) == 0 || len(declared["compliance.calendar"]) == 0 {
		t.Fatalf("the pages that state statutory values declare no pack: %v", declared)
	}
	// Today's packs: nothing is stale. A planted newer version of each declared pack makes exactly its pages stale.
	real := fstest.MapFS{}
	if err := c.LoadPacks(packs.FS); err != nil {
		t.Fatal(err)
	}
	for name, vs := range c.PackVersions {
		for _, v := range vs {
			real[name+"/"+v.String()+"/pack.toml"] = &fstest.MapFile{Data: []byte("x")}
		}
	}
	rules := func(fsys fstest.MapFS) map[string]string {
		var c2 knowledge.Corpus
		c2, _ = knowledge.Load(kb.FS, userguide.FS)
		if err := c2.LoadPacks(fsys); err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, f := range knowledge.Lint(c2) {
			if strings.HasPrefix(f.Rule, "KB-PACK-") {
				out[f.Where] = f.Rule
			}
		}
		return out
	}
	if got := rules(real); len(got) != 0 {
		t.Errorf("pack findings with today's packs: %v", got)
	}
	newer := fstest.MapFS{}
	maps.Copy(newer, real)
	newer["sg-gst/2099.1/pack.toml"] = &fstest.MapFile{Data: []byte("x")}
	got := rules(newer)
	if len(got) != 1 || got["kb/pages/concept/compliance-gst.md"] != knowledge.RulePackStale {
		t.Errorf("a newer sg-gst flags its page and no other: %v", got)
	}
	gone := fstest.MapFS{}
	maps.Copy(gone, real)
	delete(gone, "sg-deadlines/2026.1/pack.toml")
	if got := rules(gone); got["kb/pages/concept/compliance-calendar.md"] != knowledge.RulePackGone {
		t.Errorf("a pack version that is not there: %v", got)
	}
}
