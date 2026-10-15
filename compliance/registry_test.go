package compliance

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"

	"github.com/cognizhi/fyniq/internal/outbox"
)

// What the checks of the registry may name, read from the repository.
type names struct {
	makeTargets map[string]bool
	jobs        map[string]bool
	tests       map[string]bool
	ciJobs      map[string]bool
}

func repoNames(t *testing.T) names {
	t.Helper()
	n := names{makeTargets: map[string]bool{}, jobs: map[string]bool{}, tests: map[string]bool{}, ciJobs: map[string]bool{}}
	mk, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range regexp.MustCompile(`(?m)^([a-z0-9-]+):`).FindAllSubmatch(mk, -1) {
		n.makeTargets[string(m[1])] = true
	}
	for _, s := range outbox.PlatformSchedules {
		n.jobs[s.Job] = true
	}
	testFunc := regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(`)
	err = filepath.WalkDir("..", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == ".git" || d.Name() == ".data") {
			return filepath.SkipDir
		}
		if strings.HasSuffix(path, "_test.go") {
			src, _ := os.ReadFile(path)
			for _, m := range testFunc.FindAllSubmatch(src, -1) {
				n.tests[string(m[1])] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	workflows, _ := filepath.Glob("../.github/workflows/*.yml")
	for _, w := range workflows {
		var wf struct {
			Jobs map[string]struct {
				Name string `yaml:"name"`
			} `yaml:"jobs"`
		}
		src, _ := os.ReadFile(w)
		if err := yaml.Unmarshal(src, &wf); err != nil {
			t.Fatal(err)
		}
		for _, j := range wf.Jobs {
			n.ciJobs[j.Name] = true
		}
	}
	return n
}

// The registry is Appendix F as files (scripts/controls.py checks the PRD's text in make check): each
// file is valid, its checks cover the PRD's frequencies once each, and every check that runs names a
// make target, platform job, Go test or CI job that exists (PRD-001 §14.1, item 1).
func TestTheControlRegistry(t *testing.T) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(Schema()))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("control.schema.json", doc); err != nil {
		t.Fatal(err)
	}
	schema, err := c.Compile("control.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := Raw()
	if err != nil {
		t.Fatal(err)
	}
	for name, b := range raw {
		var v any
		if err := yaml.Unmarshal(b, &v); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		j, _ := json.Marshal(v) // through JSON, so the validator sees JSON types
		inst, _ := jsonschema.UnmarshalJSON(bytes.NewReader(j))
		if err := schema.Validate(inst); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	controls, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(controls) < 67 {
		t.Fatalf("only %d controls", len(controls))
	}
	repo := repoNames(t)
	running := 0
	for _, ctl := range controls {
		var got []string
		for _, ch := range ctl.Checks {
			got = append(got, ch.Frequency)
		}
		if want := Frequencies(ctl.Frequency); !slices.Equal(got, want) {
			t.Errorf("%s: checks for %v, but the PRD's frequency %q is %v", ctl.ID, got, ctl.Frequency, want)
		}
		for _, ch := range ctl.Checks {
			if ch.Status == "running" {
				running++
			}
		}
		for _, bad := range missingTargets(ctl, repo) {
			t.Error(bad)
		}
	}
	t.Logf("%d controls; %d checks running", len(controls), running)
	planted := Control{ID: "CTL-X-01", Checks: []Check{{Frequency: "CI", Status: "running", How: "make no-such-target"},
		{Frequency: "N", Status: "running", How: "job no.such.job"}, {Frequency: "D", Status: "planned", How: "make nothing"}}}
	if got := missingTargets(planted, repo); len(got) != 2 {
		t.Errorf("want the 2 running checks with missing targets reported, got %v", got)
	}
}

// missingTargets returns the running checks of ctl whose make target, job, test or CI job does not exist.
func missingTargets(ctl Control, repo names) []string {
	var out []string
	for _, ch := range ctl.Checks {
		if ch.Status != "running" {
			continue
		}
		kind, target, _ := strings.Cut(ch.How, " ")
		if !map[string]map[string]bool{"make": repo.makeTargets, "job": repo.jobs, "test": repo.tests, "ci": repo.ciJobs}[kind][target] {
			out = append(out, ctl.ID+" ("+ch.Frequency+"): "+ch.How+" names no "+kind+" that exists")
		}
	}
	return out
}

func TestFrequencies(t *testing.T) {
	for cell, want := range map[string][]string{
		"CI": {"CI"},
		"CI, N ((rev 0.11) N: the nightly grant snapshot, §10.7)":                {"CI", "N"},
		"E ((rev 0.11) each breach or suspected breach), semi-annual (tabletop)": {"E", "S"},
		"D, CI": {"D", "CI"},
	} {
		if got := Frequencies(cell); !slices.Equal(got, want) {
			t.Errorf("Frequencies(%q) = %v, want %v", cell, got, want)
		}
	}
}
