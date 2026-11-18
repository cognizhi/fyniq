package evidence

import (
	"slices"
	"testing"

	"github.com/cognizhi/fyniq/internal/domain/settings"
)

// The table's rows are keyed once, each names what it requires or recommends, a rule with required evidence starts
// required and one without starts recommended, and a rule held in an organisation setting starts at that setting's
// recommended value, which is one of the three levels (FR-DOC-06, FR-ORG-08).
// req: FR-DOC-06
func TestTheCatalogueIsConsistentWithItsSettings(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range Catalogue {
		if seen[r.Key] {
			t.Errorf("%s is in the table twice", r.Key)
		}
		seen[r.Key] = true
		if len(r.Required)+len(r.Recommended) == 0 {
			t.Errorf("%s names no evidence", r.Key)
		}
		if want := map[bool]string{true: LevelRequired, false: LevelRecommended}[len(r.Required) > 0]; r.Level != want {
			t.Errorf("%s starts %s, want %s", r.Key, r.Level, want)
		}
		if !slices.Contains(Levels, r.Level) {
			t.Errorf("%s: %q is not a level", r.Key, r.Level)
		}
		if r.Setting != "" {
			d, ok := settings.Lookup(r.Setting)
			if !ok || d.Recommended != r.Level || !r.Built {
				t.Errorf("%s: its setting %s is missing, starts at another level, or its module is not built", r.Key, r.Setting)
			}
		}
		for _, it := range append(slices.Clone(r.Required), r.Recommended...) {
			if it.Label == "" {
				t.Errorf("%s has an item with no label", r.Key)
			}
		}
	}
	for _, a := range Activities {
		if r, ok := RuleOf(a.Key); !ok || r.Setting != a.Setting {
			t.Errorf("activity %s is not the table's row", a.Key)
		}
	}
}
