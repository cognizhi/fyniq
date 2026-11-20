package journal

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Every posting source of the ledger package belongs to a module for the per-module soft locks, or is listed as belonging to none: a source added later cannot be forgotten, because
// it would be refused by no module lock and the report of this test names it.
// req: FR-GL-09
func TestEveryPostingSourceHasAModuleOrIsListedWithNone(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, "../ledger", func(fi os.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, 0)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			for _, d := range f.Decls {
				g, ok := d.(*ast.GenDecl)
				if !ok || g.Tok != token.CONST {
					continue
				}
				for _, s := range g.Specs {
					v := s.(*ast.ValueSpec)
					for i, n := range v.Names {
						if !strings.HasPrefix(n.Name, "Source") || i >= len(v.Values) {
							continue
						}
						lit, ok := v.Values[i].(*ast.BasicLit)
						if !ok || lit.Kind != token.STRING {
							continue
						}
						source, _ := strconv.Unquote(lit.Value)
						seen++
						if ModuleOf(source) == "" && !slices.Contains(NoModule, source) {
							t.Errorf("the source %q (%s) has no module and is not in NoModule: say which module's lock stops it", source, n.Name)
						}
						if ModuleOf(source) != "" && slices.Contains(NoModule, source) {
							t.Errorf("the source %q is in a module and in NoModule", source)
						}
					}
				}
			}
		}
	}
	if seen < 30 {
		t.Fatalf("read only %d posting sources: the test no longer sees the ledger package", seen)
	}
}

// The domain writes an entry only through appendEntry, which takes the ledger lock and the soft locks first (FR-GL-09): a new posting source that appended directly would skip the lock.
// req: FR-GL-09
func TestNothingInTheDomainAppendsToTheLedgerButAppendEntry(t *testing.T) {
	err := filepath.Walk("..", func(path string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(b), "\n") {
			code, _, _ := strings.Cut(line, "//")
			if strings.Contains(code, "Ledger().Append(") && !strings.HasSuffix(path, "journal/softlock.go") {
				t.Errorf("%s:%d writes an entry without the soft locks: use appendEntry (FR-GL-09)", path, i+1)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
