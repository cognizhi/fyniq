//go:build local

package api

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/clock"
)

// The breaking-change gate (QA-API-02, FR-INT-06): oasdiff compares the contract with the last
// release's, and a breaking change inside /v1 fails `make test`, which blocks merge. A planted breaking
// change proves the gate catches one. It runs once per `make test`, in the local build. oasdiff comes
// with the toolchain (mise.toml); the gate fails rather than skips without it, and in a clone without
// the release tags.

// breaking runs `oasdiff breaking` and returns its report and whether it found an error-level change.
func breaking(t *testing.T, base, revision string) (string, bool) {
	t.Helper()
	cmd := exec.Command("oasdiff", "breaking", base, revision, "--fail-on", "ERR",
		"--deprecation-days-stable", strconv.Itoa(sunsetNoticeDays))
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return out.String(), false
	case errors.Is(err, exec.ErrNotFound):
		t.Fatal("oasdiff is not installed: install the toolchain with `make setup` (mise.toml pins it)")
	case errors.As(err, &exit) && exit.ExitCode() == 1:
		return out.String(), true
	}
	t.Fatalf("oasdiff: %v\n%s", err, out.String())
	return "", false
}

func git(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = "../.."
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// lastRelease returns the last release's contract, or ok false when no release has one yet.
func lastRelease(t *testing.T) (tag string, contract []byte, ok bool) {
	t.Helper()
	if shallow, _ := git(t, "rev-parse", "--is-shallow-repository"); shallow == "true" {
		t.Fatal("this clone is shallow, so the last release cannot be found: fetch the full history and tags (actions/checkout: fetch-depth: 0)")
	}
	tag, err := git(t, "describe", "--tags", "--abbrev=0", "--match", "v[0-9]*", "HEAD")
	if err != nil {
		return "", nil, false // no release yet
	}
	b, err := git(t, "show", tag+":contracts/openapi/fyniq.v1.yaml")
	if err != nil {
		return tag, nil, false // the release predates the contract
	}
	return tag, []byte(b + "\n"), true
}

func writeDoc(t *testing.T, name string, b []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// req: QA-API-02
// req: FR-INT-06
func TestNoBreakingChangeSinceTheLastRelease(t *testing.T) {
	current, err := filepath.Abs(contractPath)
	if err != nil {
		t.Fatal(err)
	}
	if tag, base, ok := lastRelease(t); ok {
		if report, broke := breaking(t, writeDoc(t, "base.yaml", base), current); broke {
			t.Fatalf("the contract breaks /v1 since %s (FR-INT-06: changes inside a version are additive only; "+
				"a breaking change needs /v2):\n%s", tag, report)
		}
	} else {
		t.Logf("no release has the contract yet (last tag %q): only the planted changes are checked", tag)
	}

	// Planted: removing an operation and adding a required request field break /v1; adding an
	// operation does not.
	plant := func(name string, change func(o *huma.OpenAPI)) string {
		a := testAPI(t)
		change(a.OpenAPI())
		b, err := a.OpenAPI().YAML()
		if err != nil {
			t.Fatal(err)
		}
		return writeDoc(t, name+".yaml", b)
	}
	removed := plant("removed", func(o *huma.OpenAPI) { delete(o.Paths, "/v1/journals/{journal_id}") })
	if report, broke := breaking(t, current, removed); !broke || !strings.Contains(report, "api-path-removed") {
		t.Errorf("removing getJournal was not reported as breaking:\n%s", report)
	}
	required := plant("required", func(o *huma.OpenAPI) {
		s := o.Components.Schemas.Map()["Reversal"]
		s.Properties["approver"] = &huma.Schema{Type: "string"}
		s.Required = append(s.Required, "approver")
	})
	if report, broke := breaking(t, current, required); !broke || !strings.Contains(report, "required") {
		t.Errorf("a new required request field was not reported as breaking:\n%s", report)
	}
	added := plant("added", func(o *huma.OpenAPI) {
		get := *o.Paths["/v1/organisation"].Get
		get.OperationID = "getOrganisationAgain"
		o.Paths["/v1/organisation-again"] = &huma.PathItem{Get: &get}
	})
	if report, broke := breaking(t, current, added); broke {
		t.Errorf("adding an operation was reported as breaking:\n%s", report)
	}

	// Deprecation (FR-INT-06): a sunset less than six months away breaks /v1, as does removing a
	// deprecated operation before its sunset; deprecating with six months' notice does not.
	sunsetIn := func(days int) func(o *huma.OpenAPI) {
		return func(o *huma.OpenAPI) {
			op := o.Paths["/v1/journals/{journal_id}"].Get
			op.Deprecated = true
			// oasdiff counts from today's date, so the sunset does too.
			op.Extensions["x-sunset"] = clock.System{}.Now().AddDate(0, 0, days).Format(time.DateOnly)
		}
	}
	if report, broke := breaking(t, current, plant("soon", sunsetIn(30))); !broke || !strings.Contains(report, "sunset") {
		t.Errorf("a sunset 30 days away was not reported as breaking:\n%s", report)
	}
	later := plant("later", sunsetIn(sunsetNoticeDays+14))
	if report, broke := breaking(t, current, later); broke {
		t.Errorf("deprecating with six months' notice was reported as breaking:\n%s", report)
	}
	early := plant("early", func(o *huma.OpenAPI) { delete(o.Paths, "/v1/journals/{journal_id}") })
	if report, broke := breaking(t, later, early); !broke || !strings.Contains(report, "sunset") {
		t.Errorf("removing a deprecated operation before its sunset was not reported as breaking:\n%s", report)
	}
}
