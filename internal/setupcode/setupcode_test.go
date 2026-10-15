package setupcode

import (
	"bytes"
	"strings"
	"testing"
)

// req: FR-ADM-06
func TestCodesAreRandomAndReadAsTyped(t *testing.T) {
	a, ha := New()
	b, hb := New()
	if a == b || bytes.Equal(ha, hb) || len(strings.ReplaceAll(a, "-", "")) != 26 {
		t.Fatalf("codes %q and %q", a, b)
	}
	if !bytes.Equal(Hash(strings.ToLower(strings.ReplaceAll(a, "-", " "))), ha) {
		t.Error("the code typed in lower case with spaces does not match")
	}
	if bytes.Equal(Hash(a+"X"), ha) {
		t.Error("a different code matches")
	}
}
