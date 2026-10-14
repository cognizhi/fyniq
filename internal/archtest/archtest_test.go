package archtest

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func goList(t *testing.T, tags string) []Pkg {
	t.Helper()
	args := []string{"list", "-deps", "-json"}
	if tags != "" {
		args = append(args, "-tags", tags)
	}
	cmd := exec.Command("go", append(args, "./...")...)
	cmd.Dir = repoRoot(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	var pkgs []Pkg
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p Pkg
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		pkgs = append(pkgs, p)
	}
	return pkgs
}

// req: NFR-PORT-03
func TestNoCloudSDKOutsideAdapters(t *testing.T) {
	for _, tags := range []string{"", "local"} {
		if v := ImportViolations(goList(t, tags)); len(v) > 0 {
			t.Errorf("tags %q:\n%s", tags, strings.Join(v, "\n"))
		}
	}
}

// req: NFR-PORT-03
func TestImportRuleCatchesAPlantedViolation(t *testing.T) {
	planted := []Pkg{
		{ImportPath: Module + "/internal/domain/ledger", Imports: []string{"cloud.google.com/go/storage"}, Deps: []string{"cloud.google.com/go/storage"}},
		{ImportPath: Module + "/internal/ports", Deps: []string{Module + "/internal/adapters/gcp"}},
		{ImportPath: Module + "/internal/adapters/gcp", Imports: []string{"cloud.google.com/go/storage"}},
		{ImportPath: Module + "/cmd/api", Imports: []string{Module + "/internal/adapters/gcp"}},
	}
	v := ImportViolations(planted)
	if len(v) != 3 {
		t.Fatalf("want 3 violations (SDK import, SDK dep and adapter dep in the domain or ports), got %d:\n%s", len(v), strings.Join(v, "\n"))
	}
}

// req: DEV-05
func TestNoWallClockOutsideTheClockPackage(t *testing.T) {
	root := repoRoot(t)
	var found []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "testdata", ".data", "vendor":
				return filepath.SkipDir
			}
			if rel == filepath.Join("internal", "clock") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		calls, err := WallClockCalls(rel, src)
		found = append(found, calls...)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) > 0 {
		t.Fatalf("wall-clock reads outside internal/clock:\n%s", strings.Join(found, "\n"))
	}
}

// req: DEV-05
func TestWallClockRuleCatchesAPlantedCall(t *testing.T) {
	src := []byte("package x\nimport t \"time\"\nfunc f() { _ = t.Now(); _ = t.Since(t.Time{}); _ = t.Second }\n")
	calls, err := WallClockCalls("x.go", src)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 {
		t.Fatalf("want 2 calls (Now, Since), got %v", calls)
	}
}

// req: NFR-PORT-01
func TestSQLImportRuleCatchesAPlantedViolation(t *testing.T) {
	planted := []Pkg{
		{ImportPath: Module + "/internal/domain/ledger", Imports: []string{"github.com/jackc/pgx/v5"}},
		{ImportPath: Module + "/internal/app", Imports: []string{"database/sql"}},
		{ImportPath: Module + "/internal/adapters/local", Imports: []string{"github.com/pressly/goose/v3"}},
		{ImportPath: Module + "/cmd/api", Imports: []string{Module + "/internal/adapters/postgres/sqlcdb"}},
		{ImportPath: Module + "/internal/adapters/postgres", Imports: []string{"github.com/jackc/pgx/v5", "database/sql"}},
	}
	if v := ImportViolations(planted); len(v) != 4 {
		t.Fatalf("want 4 violations (all but the adapter itself), got %d:\n%s", len(v), strings.Join(v, "\n"))
	}
}

// req: NFR-PORT-07
func TestOnlyTheUnitOfWorkBeginsOrEndsTransactions(t *testing.T) {
	dir := filepath.Join(repoRoot(t), "internal", "adapters", "postgres")
	var found []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		if rel == "store.go" || strings.HasPrefix(rel, "pgtest"+string(filepath.Separator)) {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		calls, err := TransactionCalls(rel, src)
		found = append(found, calls...)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) > 0 {
		t.Fatalf("transactions begun or ended outside the unit of work:\n%s", strings.Join(found, "\n"))
	}
	planted, err := TransactionCalls("repo.go", []byte("package x\nfunc f(p, tx T) { tx, _ = p.Begin(nil); _ = tx.Commit(nil); _ = tx.Exec() }\n"))
	if err != nil || len(planted) != 2 {
		t.Fatalf("want 2 planted calls (Begin, Commit), got %v, %v", planted, err)
	}
}
