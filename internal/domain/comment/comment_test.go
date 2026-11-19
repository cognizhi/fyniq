package comment

import (
	"strings"
	"testing"
)

// A comment is kept trimmed with its line breaks as \n, and refused when empty, over 2,000 characters or holding a control
// character; it mentions each person once and at most ten.
// req: FR-UX-13
func TestACommentIsCleanedAndChecked(t *testing.T) {
	if got, p := Clean("  Please check the GST.\r\nThanks  "); p != nil || got != "Please check the GST.\nThanks" {
		t.Errorf("a comment: %q %v", got, p)
	}
	for what, body := range map[string]string{"empty": "  \n ", "long": strings.Repeat("a", MaxBody+1), "control": "a\x00b", "escape": "a\x1bb"} {
		if _, p := Clean(body); p == nil || p.Code != CodeBody {
			t.Errorf("%s: %v", what, p)
		}
	}
	if _, p := Clean(strings.Repeat("é", MaxBody)); p != nil {
		t.Errorf("2,000 characters of two bytes each: %v", p)
	}
	if p := CheckMentions([]string{"a", "b"}); p != nil {
		t.Errorf("two people: %v", p)
	}
	if p := CheckMentions([]string{"a", "a"}); p == nil || p.Code != CodeMentions {
		t.Errorf("one person twice: %v", p)
	}
	if p := CheckMentions(make([]string, MaxMentions+1)); p == nil {
		t.Errorf("eleven people")
	}
}
