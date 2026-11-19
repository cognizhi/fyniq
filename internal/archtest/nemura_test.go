package archtest

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	nemurapacks "github.com/cognizhi/fyniq/internal/nemura/packs"
	fyniqpacks "github.com/cognizhi/fyniq/packs"
)

// Nemura stays in its lane: its packages import only their own code and the shared list, and nothing outside Nemura
// imports them, in both build flavours. No requirement tag: this proves one clause of NM-ARC-05 (shared rules, not
// shared code paths) and of ADR-N4, and NM-ARC-05 is tagged with payroll.
func TestNemuraStaysInItsLane(t *testing.T) {
	for _, tags := range []string{"", "local"} {
		if v := NemuraViolations(goList(t, tags)); len(v) > 0 {
			t.Errorf("tags %q:\n%s", tags, strings.Join(v, "\n"))
		}
	}
}

func TestNemuraRulesCatchPlantedViolations(t *testing.T) {
	planted := []Pkg{
		{ImportPath: Module + "/cmd/nemura", Imports: []string{Module + "/internal/app", Module + "/internal/nemura/role", "fmt"}}, // fine
		{ImportPath: Module + "/internal/nemura/role", Imports: []string{Module + "/internal/runtime", Module + "/internal/domain/money"}},
		{ImportPath: Module + "/internal/nemura/x", Imports: []string{Module + "/internal/api"}},                         // Fyniq's API
		{ImportPath: Module + "/internal/nemura/y", Imports: []string{Module + "/internal/domain/journal"}},              // Fyniq's ledger
		{ImportPath: Module + "/internal/nemura/z", Imports: []string{Module + "/internal/ports/storage"}},               // a sub-package is not shared
		{ImportPath: Module + "/internal/nemura/w", Imports: []string{Module + "/internal/adapters/postgres"}},           // Fyniq's store
		{ImportPath: Module + "/internal/adapters/postgres/nemura", Imports: []string{Module + "/internal/nemura/role"}}, // fine
		{ImportPath: Module + "/internal/api", Imports: []string{Module + "/internal/nemura/role"}},                      // Fyniq reaching in
	}
	got := NemuraViolations(planted)
	if len(got) != 5 {
		t.Fatalf("want 5 planted violations (api, journal, ports/storage, postgres, and Fyniq's api importing Nemura), got %d:\n%s", len(got), strings.Join(got, "\n"))
	}
	for _, want := range []string{"internal/nemura/x imports", "internal/nemura/y imports", "internal/nemura/z imports", "internal/nemura/w imports", "internal/api imports"} {
		if !slices.ContainsFunc(got, func(s string) bool { return strings.Contains(s, want) }) {
			t.Errorf("no violation reported for %s", want)
		}
	}
}

// Every Nemura service of the local stack is in the `people` profile, so `make up` and the smoke test of QA-LOCAL-01 run
// the core stack alone, and none shares a data volume (PRD-002 NM-ARC-01: Nemura runs with no Fyniq, and Fyniq with no
// Nemura).
func TestNemuraServicesAreInThePeopleProfile(t *testing.T) {
	var compose struct {
		Services map[string]struct {
			Image    string   `yaml:"image"`
			Profiles []string `yaml:"profiles"`
			Volumes  []string `yaml:"volumes"`
		} `yaml:"services"`
	}
	src, err := os.ReadFile(filepath.Join(repoRoot(t), "deploy", "local", "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(src, &compose); err != nil {
		t.Fatal(err)
	}
	found := 0
	for name, s := range compose.Services {
		if !strings.HasPrefix(name, "nemura") && s.Image != "fyniq-nemura:local" {
			continue
		}
		found++
		if !slices.Equal(s.Profiles, []string{"people"}) {
			t.Errorf("compose service %s is in profiles %v, want only people", name, s.Profiles)
		}
		for _, v := range s.Volumes { // a read-only config file is no shared data; a named volume is
			if !strings.HasPrefix(v, "./") || !strings.HasSuffix(v, ":ro") {
				t.Errorf("compose service %s mounts %s: Nemura shares no data volume with Fyniq (only a read-only file of its own)", name, v)
			}
		}
	}
	if found == 0 {
		t.Fatal("no Nemura service in the local stack")
	}
}

// Both test databases, Fyniq's (pgtest) and Nemura's (nemuratest), run the PostgreSQL image of the local stack, so a test
// proves what `make up` runs. The pin is a string in each file because the Nemura helper cannot import Fyniq's.
func TestTheTestDatabasesRunTheLocalStacksPostgres(t *testing.T) {
	root := repoRoot(t)
	pin := regexp.MustCompile(`postgres:[0-9][^"\s]*@sha256:[0-9a-f]{64}`)
	var found []string
	for _, f := range []string{"deploy/local/compose.yaml", "internal/adapters/postgres/pgtest/pgtest.go", "internal/adapters/postgres/nemura/nemuratest/nemuratest.go"} {
		src, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			t.Fatal(err)
		}
		m := pin.Find(src)
		if m == nil {
			t.Fatalf("%s pins no PostgreSQL image", f)
		}
		found = append(found, string(m))
	}
	if found[0] != found[1] || found[0] != found[2] {
		t.Errorf("the PostgreSQL pins differ: compose %s, pgtest %s, nemuratest %s", found[0], found[1], found[2])
	}
}

// The pack validator Nemura shares with Fyniq stays pure: it imports nothing of the module and no I/O package, so a change that gives it a database,
// a clock or a network fails here (owner decision O1, p1-nemura-records-packs). NM-ARC-05: shared rules, not shared code paths.
func TestTheSharedPackLibraryStaysPure(t *testing.T) {
	for _, tags := range []string{"", "local"} {
		if v := PackViolations(goList(t, tags)); len(v) > 0 {
			t.Errorf("tags %q:\n%s", tags, strings.Join(v, "\n"))
		}
	}
	got := PackViolations([]Pkg{
		{ImportPath: Module + "/internal/domain/pack", Imports: []string{"fmt", "encoding/csv", "time"}}, // fine
		{ImportPath: Module + "/internal/domain/pack", Imports: []string{"net/http", "os", Module + "/internal/ports"}},
		{ImportPath: Module + "/internal/domain/other", Imports: []string{"os"}}, // not the pack library
	})
	if len(got) != 3 {
		t.Errorf("want the 3 planted impurities, got %d: %v", len(got), got)
	}
}

// Nemura's copies of the statutory packs payroll reads (sg-cpf, sg-sdl, sg-shg) are byte for byte Fyniq's: one set of researched values, two places to read it from, so the two
// apps and the oracle cannot disagree because a copy was edited alone (p1-nemura-payroll D1, owner decision O1 of 19 Nov 2026).
// req: NM-PACK-01
func TestNemurasStatutoryPackCopiesEqualFyniqs(t *testing.T) {
	for _, name := range []string{"sg-cpf", "sg-sdl", "sg-shg"} {
		a, err := fs.Sub(fyniqpacks.FS, name)
		if err != nil {
			t.Fatal(err)
		}
		b, err := fs.Sub(nemurapacks.Statutory, name)
		if err != nil {
			t.Fatal(err)
		}
		read := func(f fs.FS) map[string]string {
			out := map[string]string{}
			if err := fs.WalkDir(f, ".", func(p string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return err
				}
				raw, err := fs.ReadFile(f, p)
				out[p] = string(raw)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			return out
		}
		fa, fb := read(a), read(b)
		if len(fa) != len(fb) {
			t.Errorf("%s: %d files in Fyniq's and %d in Nemura's copy", name, len(fa), len(fb))
		}
		for p, want := range fa {
			if got, ok := fb[p]; !ok || got != want {
				t.Errorf("%s/%s differs between Fyniq's pack and Nemura's copy: copy it again", name, p)
			}
		}
	}
}
