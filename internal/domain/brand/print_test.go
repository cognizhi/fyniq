package brand

import (
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/doctemplate"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The brand in force at a time is the latest version saved at or before it, none before the first; the platform's
// starter takes the brand's accent, and an organisation's own template keeps the one its author chose.
// req: FR-ORG-09
// req: FR-DOC-04
func TestTheBrandInForceAndItsAccent(t *testing.T) {
	at := func(h int) time.Time { return time.Date(2026, 11, 7, h, 0, 0, 0, time.UTC) }
	versions := []storage.BrandVersion{{Version: 3, CreatedAt: at(12), Accent: Accents[2]}, {Version: 2, CreatedAt: at(10), Accent: Accents[1]},
		{Version: 1, CreatedAt: at(9)}} // newest first, as storage.Brand lists them
	for when, want := range map[time.Time]int{at(8): 0, at(9): 1, at(11): 2, at(12): 3, at(23): 3} {
		v, ok := InForce(versions, when)
		if v.Version != want || ok != (want > 0) {
			t.Errorf("at %s: version %d (%v), want %d", when.Format(time.Kitchen), v.Version, ok, want)
		}
	}

	starter := doctemplate.Default(doctemplate.KindDocument, doctemplate.Invoice)
	if c := Themed(starter, doctemplate.SourcePlatform, versions[1]); c.Theme.Accent != Accents[1] {
		t.Errorf("the platform's starter is in %s", c.Theme.Accent)
	}
	if c := Themed(starter, doctemplate.SourcePlatform, versions[2]); c.Theme.Accent != starter.Theme.Accent {
		t.Errorf("with no accent the starter is in %s", c.Theme.Accent)
	}
	if c := Themed(starter, doctemplate.SourceDefault, versions[1]); c.Theme.Accent != starter.Theme.Accent {
		t.Errorf("the organisation's own template took the brand's accent: %s", c.Theme.Accent)
	}
}
