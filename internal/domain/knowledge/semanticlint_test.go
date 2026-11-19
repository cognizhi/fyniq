package knowledge

import (
	"reflect"
	"testing"
)

// The weekly lint's deterministic half finds what a person should look at across an organisation's knowledge base, and nothing
// that a model would have to judge (AI-52, AI-56).
// (AI-52, part proved: no req tag until the compiler's model half is recorded)
func TestTheSemanticLintFindsWhatAPersonShouldLookAt(t *testing.T) {
	notes := []SemNote{
		{Key: "expense", Version: 3, PageVersion: 2, PageNoteVersion: 2}, // changed since its page was compiled
		{Key: "leave", Version: 1},                                       // no page
		{Key: "travel", Version: 2, PageVersion: 1, PageNoteVersion: 2},  // up to date
		{Key: "written", Version: 4, PageVersion: 1},                     // a person's page: no note version, nothing to compare
	}
	live := []SemLive{
		{Key: "org.expense", Body: "# E\n\n## A\n\nSee [[org.travel]] and [[org.nowhere]]. (src: note:expense#a)\n"},
		{Key: "org.travel", Body: "# T\n\n## A\n\nBack to [[org.expense]]. (src: note:travel#a)\n"},
		{Key: "org.founders.balance", Body: "# F\n\n## A\n\nOverlay. (src: platform:founders.balance#a)\n", BaseKey: "founders.balance", BaseDigest: "aa", PlatformDigest: "bb"},
		{Key: "org.gone", Body: "# G\n\n## A\n\nOverlay. (src: platform:x#a)\n", BaseKey: "x", BaseDigest: "aa", PlatformGone: true},
		{Key: "org.alone", Body: "# A\n\n## A\n\nLinks to none. (src: note:written#a)\n"},
	}
	exists := func(k string) bool { return k != "org.nowhere" }
	var got [][3]string
	for _, f := range SemanticLint(notes, live, exists) {
		got = append(got, [3]string{f.PageKey, f.Kind, ""})
	}
	want := [][3]string{
		{"org.alone", SemOrphan, ""}, {"org.expense", SemMissingLink, ""}, {"org.expense", SemStale, ""},
		{"org.founders.balance", SemOrphan, ""}, {"org.founders.balance", SemSuperseded, ""},
		{"org.gone", SemOrphan, ""}, {"org.gone", SemSuperseded, ""}, {"org.leave", SemNoPage, ""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("findings:\n got %v\nwant %v", got, want)
	}
	// Fewer than four published pages: no page is an orphan, as a handful of pages need no web of links.
	for _, f := range SemanticLint(nil, live[:3], exists) {
		if f.Kind == SemOrphan {
			t.Errorf("an orphan among three pages: %+v", f)
		}
	}
	// A clean knowledge base has nothing to say.
	if got := SemanticLint([]SemNote{{Key: "travel", Version: 2, PageVersion: 1, PageNoteVersion: 2}}, live[1:2], exists); len(got) != 0 {
		t.Errorf("a clean knowledge base: %+v", got)
	}
}

// The weekly pass's advice never stops a publication, and a conflict, contradiction or bad citation does.
// (AI-56, part proved: no req tag)
func TestOnlyBlockingFindingsStopAPublication(t *testing.T) {
	for _, k := range []string{SemStale, SemNoPage, SemSuperseded, SemMissingLink, SemOrphan} {
		if BlocksPublication(k) {
			t.Errorf("%s stops a publication", k)
		}
	}
	for _, k := range []string{"human_edit_conflict", "contradiction", "number_not_in_source", "citation", "front_matter"} {
		if !BlocksPublication(k) {
			t.Errorf("%s does not stop a publication", k)
		}
	}
}
