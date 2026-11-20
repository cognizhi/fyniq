//go:build local

package postgres_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// A later migration that redefines document_type_known from an older list would silently drop the types added since, and the
// first insert of one would fail only at run time. This test reads every document type the code names (a constant of type
// DocumentType holding a string) and checks the live constraint accepts each one.
// req: data/data-model Logical data model
func TestEveryDocumentTypeInCodeIsAcceptedByTheDatabaseCheck(t *testing.T) {
	var def string
	for _, r := range rows(t, conn(t, "fyniq"), `SELECT pg_get_constraintdef(k.oid) FROM pg_constraint k
 JOIN pg_class c ON c.oid = k.conrelid JOIN pg_namespace n ON n.oid = c.relnamespace
 WHERE n.nspname = 'fyniq' AND c.relname = 'document' AND k.conname = 'document_type_known'`) {
		def = r[0]
	}
	if def == "" {
		t.Fatal("the document table has no document_type_known constraint")
	}
	allowed := map[string]bool{}
	for _, m := range regexp.MustCompile(`'([a-z_0-9]+)'::text`).FindAllStringSubmatch(def, -1) {
		allowed[m[1]] = true
	}
	if len(allowed) < 30 {
		t.Fatalf("read only %d types from the constraint: %s", len(allowed), def)
	}
	re := regexp.MustCompile(`\bDocumentType\s*=\s*"([a-z_0-9]+)"`)
	root := filepath.Join("..", "..", "..", "internal")
	var found []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			found = append(found, m[1])
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) < 30 {
		t.Fatalf("found only %d document type constants in the code", len(found))
	}
	slices.Sort(found)
	for _, name := range slices.Compact(found) {
		if !allowed[name] {
			t.Errorf("the code names the document type %q but document_type_known does not accept it: a later migration redefined the check from an older list", name)
		}
	}
}
