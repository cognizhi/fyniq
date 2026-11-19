package compliance

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// req: CTL-AI-08
func TestKBPagesCheckFindsWhatBreaksTheControl(t *testing.T) {
	src := func(refs ...string) json.RawMessage {
		var s []map[string]string
		for _, r := range refs {
			k, ref, _ := strings.Cut(r, ":")
			s = append(s, map[string]string{"kind": k, "ref": ref})
		}
		b, _ := json.Marshal(s)
		return b
	}
	notes := []storage.KBNote{{Key: "expense", Body: "Claims are made within 14 days."}}
	good := storage.KBVersion{Key: "org.expense", Number: 1, Origin: "compiled", AuthorKind: "compiler", Summary: "How claims are made.", Sources: src("note:expense"),
		Body: "# Expense\n\n## Claims\n\nClaims are made within 14 days. (src: note:expense#claims)\n"}
	if f, n := kbPageFindings(map[string]int{"org.expense": 1}, notes, map[string][]storage.KBVersion{"org.expense": {good}}); len(f) != 0 || n != 1 {
		t.Fatalf("a good page: %+v %d", f, n)
	}
	invented := good
	invented.Body = strings.Replace(good.Body, "14", "30", 1)
	gone := good
	gone.Sources = src("note:missing")
	person := good
	person.Number, person.AuthorKind = 2, "person"
	edit := good
	edit.Number, edit.Origin, edit.AuthorKind = 3, "human_edited", "compiler"
	for name, c := range map[string]struct {
		vs   []storage.KBVersion
		want string
	}{
		"an invented number":             {[]storage.KBVersion{invented}, "KB-ORG-NUMBER"},
		"a note that is gone":            {[]storage.KBVersion{gone}, "KB-ORG-SOURCE-GONE"},
		"a compiled version by a person": {[]storage.KBVersion{good, person}, "written under a person's name"},
		"a human edit by the compiler":   {[]storage.KBVersion{good, edit}, "not written by a person"},
	} {
		f, _ := kbPageFindings(map[string]int{"org.expense": 1}, notes, map[string][]storage.KBVersion{"org.expense": c.vs})
		if len(f) == 0 || !strings.Contains(f[0].Detail, c.want) {
			t.Errorf("%s: %+v", name, f)
		}
	}
	// A published version that is not among the versions is found too.
	if f, _ := kbPageFindings(map[string]int{"org.expense": 9}, notes, map[string][]storage.KBVersion{"org.expense": {good}}); len(f) != 1 {
		t.Errorf("a missing version: %+v", f)
	}
}
