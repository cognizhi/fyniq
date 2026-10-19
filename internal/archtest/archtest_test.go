package archtest

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
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

// goFiles calls fn with every non-test Go file under dir (relative to the repository root).
func goFiles(t *testing.T, dir string, fn func(rel string, src []byte)) {
	t.Helper()
	root := repoRoot(t)
	err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "testdata", "node_modules", ".git", ".data":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		fn(rel, src)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Every posting goes through one path: only the posting service (internal/domain/journal) appends to
// the ledger, and the database's triggers are the backstop, not the logic (§11.4). The storage port's
// conformance suite and the adapter's own tests are the other callers.
// req: data/data-model Single posting path
func TestOnlyThePostingServiceAppendsToTheLedger(t *testing.T) {
	root := repoRoot(t)
	allowed := []string{"internal/domain/journal/", "internal/ports/porttest/", "internal/adapters/postgres/"}
	var found []string
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".go" {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if slices.ContainsFunc(allowed, func(p string) bool { return strings.HasPrefix(rel, p) }) || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(b, []byte("Ledger().Append(")) {
			found = append(found, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) > 0 {
		t.Fatalf("these append to the ledger outside the posting service: %v", found)
	}
}

// req: ENG-10
func TestNoFloatingPointInMoneyCode(t *testing.T) {
	var found []string
	for _, dir := range MoneyPackages {
		goFiles(t, dir, func(rel string, src []byte) {
			uses, err := FloatUses(rel, src)
			if err != nil {
				t.Fatal(err)
			}
			found = append(found, uses...)
		})
	}
	if len(found) > 0 {
		t.Fatalf("binary floating point in money code:\n%s", strings.Join(found, "\n"))
	}
	planted := []byte("package x\nimport (\"math\"; s \"strconv\")\nvar a float64 = 1.5\nfunc f() { _ = math.Pi; _, _ = s.ParseFloat(\"1\", 64); var b float32; _ = b }\n")
	uses, err := FloatUses("x.go", planted)
	if err != nil || len(uses) != 5 {
		t.Fatalf("want 5 planted uses (math, float64, 1.5, ParseFloat, float32), got %d: %v", len(uses), uses)
	}
}

// req: ENG-04
func TestOnlyTheBalanceServiceReadsBalances(t *testing.T) {
	var found []string
	goFiles(t, ".", func(rel string, src []byte) {
		for _, allowed := range BalanceReaders {
			if strings.HasPrefix(filepath.ToSlash(rel), allowed+"/") {
				return
			}
		}
		calls, err := BalanceCalls(rel, src)
		if err != nil {
			t.Fatal(err)
		}
		found = append(found, calls...)
	})
	if len(found) > 0 {
		t.Fatalf("balances read outside the balance service:\n%s", strings.Join(found, "\n"))
	}
	// No query of the adapter sums journal lines: the balances come from the cache the database keeps.
	queries, err := filepath.Glob(filepath.Join(repoRoot(t), "internal", "adapters", "postgres", "queries", "*.sql"))
	if err != nil || len(queries) == 0 {
		t.Fatalf("no queries found: %v", err)
	}
	for _, q := range queries {
		src, _ := os.ReadFile(q)
		for _, stmt := range strings.Split(string(src), "-- name:") {
			lower := strings.ToLower(stmt)
			if strings.Contains(lower, "journal_line") && strings.Contains(lower, "sum(") {
				t.Errorf("%s sums journal lines: ledger figures come from the balance service (ENG-04)", filepath.Base(q))
			}
		}
	}
	if calls, _ := BalanceCalls("x.go", []byte("package x\nfunc f(u U) { u.Balances().ForPeriod(nil, x) }\n")); len(calls) != 1 {
		t.Fatalf("want 1 planted call, got %v", calls)
	}
}

// req: QA-ENG-04
func TestMutationTestingCoversMoneyRoundingAndTheEngine(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(repoRoot(t), "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	mk := string(src)
	for _, want := range []string{"internal/domain/money", "internal/domain/rounding", "internal/domain/ledger", "--threshold-efficacy 90"} {
		if !strings.Contains(mk, want) {
			t.Errorf("make mutation no longer has %q: QA-ENG-04 needs money, rounding and the engine at 90%% or more", want)
		}
	}
	if !strings.Contains(mk, "@$(MAKE) --no-print-directory mutation") {
		t.Error("make ci no longer runs make mutation (QA-ENG-04)")
	}
}

// req: QA-API-03
// req: FR-INT-01
func TestTheWebAppsCallTheAPIOnlyThroughTheGeneratedClient(t *testing.T) {
	root := repoRoot(t)
	apps, err := filepath.Glob(filepath.Join(root, "apps", "*", "src"))
	if err != nil || len(apps) == 0 {
		t.Fatalf("no web apps found: %v", err)
	}
	var found []string
	clients := 0
	for _, src := range apps {
		err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(src, path)
			if d.IsDir() {
				if rel == "api" { // the generated client and the module around it
					clients++
					return filepath.SkipDir
				}
				return nil
			}
			if ext := filepath.Ext(path); ext != ".ts" && ext != ".tsx" {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			r, _ := filepath.Rel(root, path)
			found = append(found, NetworkCalls(r, b)...)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(found) > 0 {
		t.Fatalf("hand-written network calls:\n%s", strings.Join(found, "\n"))
	}
	if clients == 0 {
		t.Fatal("no web app has its generated client in src/api")
	}
	planted := []byte("// fetch(\"/in/a/comment\") is fine\n/* new WebSocket(x) */\nconst a = await fetch(\"/v1/x\");\nconst b = new XMLHttpRequest();\nimport ky from \"ky\";\nnavigator.sendBeacon(\"/x\");\nconst c = refetch(1);\n")
	if got := NetworkCalls("planted.ts", planted); len(got) != 4 {
		t.Fatalf("want 4 planted calls (fetch, XMLHttpRequest, ky, sendBeacon), got %d: %v", len(got), got)
	}
}

// req: SEC-28
// req: CTL-CHG-11
// asvs: V15.2.4, V13.3.2
func TestWorkflowsPinActionsAndLimitTokens(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(repoRoot(t), ".github", "workflows", "*.yml"))
	if err != nil || len(files) < 3 {
		t.Fatalf("workflows: %v %v", files, err)
	}
	seen := map[string]bool{}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range WorkflowViolations(filepath.Base(f), src) {
			t.Error(v)
		}
		for key := range WriteJobs {
			if strings.HasPrefix(key, filepath.Base(f)+"/") {
				seen[key] = true
			}
		}
	}
	for key := range WriteJobs {
		if !seen[key] {
			t.Errorf("WriteJobs names %s, whose workflow does not exist: remove it", key)
		}
	}
	planted := []byte(`
on:
  pull_request_target:
  push:
    branches: [feature]
permissions: write-all
jobs:
  build:
    runs-on: ubuntu-latest
    permissions:
      contents: write
    steps:
      - uses: actions/checkout@v4
      - uses: docker://alpine:3
      - run: echo ${{ secrets.DEPLOY_KEY }}
  nothing:
    runs-on: ubuntu-latest
`)
	got := WorkflowViolations("planted.yml", planted)
	// top-level token, write not allowed, write for unreviewed code, unpinned action, checkout under
	// pull_request_target, unpinned image, no permissions block, a secret under pull_request_target
	if len(got) != 8 {
		t.Fatalf("want 8 planted violations, got %d:\n%s", len(got), strings.Join(got, "\n"))
	}
}

// req: REL-06
func TestReleasePleaseRunsWithTheAppToken(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(repoRoot(t), ".github", "workflows", "release-please.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if got := ReleaseAppTokenViolations(src); len(got) > 0 {
		t.Fatalf("release-please.yml:\n%s", strings.Join(got, "\n"))
	}
	planted := []byte(`
jobs:
  release-please:
    permissions:
      contents: write
    steps:
      - uses: actions/create-github-app-token@0000000000000000000000000000000000000000
        id: app-token
        with:
          owner: cognizhi
          permission-contents: write
          permission-administration: write
      - uses: googleapis/release-please-action@0000000000000000000000000000000000000000
        with:
          config-file: release-please-config.json
`)
	// a GITHUB_TOKEN with write, scopes beyond the three, a token for more than this repository,
	// release-please on the default token
	if got := ReleaseAppTokenViolations(planted); len(got) != 4 {
		t.Fatalf("want 4 planted violations, got %d:\n%s", len(got), strings.Join(got, "\n"))
	}
}

// Every step of `make ci` runs in a CI job, so CI is exactly what `make ci` runs (p0-ci-images-release 1.1).
// req: SEC-21
// asvs: V15.2.1
func TestCIRunsEveryStepOfMakeCI(t *testing.T) {
	root := repoRoot(t)
	mk, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	recipe := regexp.MustCompile(`(?ms)^ci:[^\n]*\n((?:\t[^\n]*\n)+)`).FindSubmatch(mk)
	if recipe == nil {
		t.Fatal("no ci target in the Makefile")
	}
	steps := regexp.MustCompile(`--no-print-directory ([a-z0-9-]+)`).FindAllSubmatch(recipe[1], -1)
	ci, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) < 6 {
		t.Fatalf("make ci has only %d steps", len(steps))
	}
	for _, s := range steps {
		if !regexp.MustCompile(`(?m)run: make ` + regexp.QuoteMeta(string(s[1])) + `\b`).Match(ci) {
			t.Errorf("make ci runs `make %s`, but no CI job does", s[1])
		}
	}
}

// Security tests carry the OWASP ASVS 5.0 requirements they verify (SEC-20, docs/security/README.md).
// req: SEC-20
func TestSecurityTestsCarryASVSIDs(t *testing.T) {
	root := repoRoot(t)
	tsv, err := os.ReadFile(filepath.Join(root, "docs", "security", "asvs-5.0.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	valid := map[string]bool{}
	for _, line := range strings.Split(string(tsv), "\n") {
		if id, _, ok := strings.Cut(line, "\t"); ok && strings.HasPrefix(id, "V") {
			valid[id] = true
		}
	}
	if len(valid) != 345 {
		t.Fatalf("docs/security/asvs-5.0.tsv lists %d requirements; ASVS 5.0.0 has 345", len(valid))
	}
	var found []string
	tagged := 0
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == ".git" || d.Name() == ".data") {
			return filepath.SkipDir
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		found = append(found, ASVSViolations(rel, src, valid)...)
		tagged += strings.Count(string(src), "\n// asvs: ")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) > 0 {
		t.Fatalf("security tests and ASVS IDs:\n%s", strings.Join(found, "\n"))
	}
	if tagged < 30 {
		t.Fatalf("only %d tests carry ASVS IDs", tagged)
	}
	planted := []byte("package x\n\n// req: SEC-02\nfunc TestNoASVS(t *T) {}\n\n// req: QA-ENG-05\n// asvs: V8.4.1, V99.1.1\nfunc TestBadID(t *T) {}\n\n// req: FR-GL-01\nfunc TestNotSecurity(t *T) {}\n")
	if got := ASVSViolations("planted_test.go", planted, valid); len(got) != 2 {
		t.Fatalf("want 2 planted violations (no ID, an unknown ID), got %v", got)
	}
}
