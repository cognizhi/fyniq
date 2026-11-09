package compliance

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"

	"github.com/cognizhi/fyniq/internal/domain/checks"
	"github.com/cognizhi/fyniq/internal/outbox"
)

// What the checks of the registry may name, read from the repository.
type names struct {
	makeTargets map[string]bool
	jobs        map[string]bool
	tests       map[string]bool
	ciJobs      map[string]bool              // by name and by ID
	catalogue   map[string]checks.Definition // the checks of the catalogue, by key
	tofuRuns    map[string]bool              // <module>/<run> of every infra/ module test
	changes     map[string]bool              // the changes of the P1–P3 plan, which a planned check arrives with
}

func repoNames(t *testing.T) names {
	t.Helper()
	n := names{makeTargets: map[string]bool{}, jobs: map[string]bool{}, tests: map[string]bool{}, ciJobs: map[string]bool{},
		catalogue: map[string]checks.Definition{}, tofuRuns: map[string]bool{}, changes: map[string]bool{}}
	for _, d := range checks.Catalogue() {
		n.catalogue[d.Key] = d
	}
	tftests, _ := filepath.Glob("../infra/*/modules/*/tests/*.tftest.hcl")
	more, _ := filepath.Glob("../infra/modules/*/tests/*.tftest.hcl")
	for _, f := range append(tftests, more...) {
		module := filepath.Base(filepath.Dir(filepath.Dir(f)))
		src, _ := os.ReadFile(f)
		for _, m := range regexp.MustCompile(`(?m)^run "([a-z0-9_]+)"`).FindAllSubmatch(src, -1) {
			n.tofuRuns[module+"/"+string(m[1])] = true
		}
	}
	roadmap, err := os.ReadFile("../scripts/roadmap.py")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range regexp.MustCompile(`(?m)^ch\("([a-z0-9-]+)"`).FindAllSubmatch(roadmap, -1) {
		n.changes[string(m[1])] = true
	}
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
		for id, j := range wf.Jobs {
			n.ciJobs[j.Name], n.ciJobs[id] = true, true
		}
	}
	return n
}

// The registry is Appendix F as files (scripts/controls.py checks the PRD's text in make check): each
// file is valid, its checks cover the PRD's frequencies in order, one or more each, each mapping names
// known frameworks, and every check names what exists (problems) (PRD-001 §14.1, item 1).
// req: CMP-01
// req: compliance/compliance-center Control registry as code
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
	byStatus := map[string]int{}
	for _, ctl := range controls {
		var got []string
		for _, ch := range ctl.Checks {
			got = append(got, ch.Frequency)
			byStatus[ch.Status]++
		}
		if want := Frequencies(ctl.Frequency); !slices.Equal(slices.Compact(got), want) {
			t.Errorf("%s: checks for %v, but the PRD's frequency %q is %v", ctl.ID, got, ctl.Frequency, want)
		}
		if len(ctl.Frameworks) == 0 {
			t.Errorf("%s: its mapping %q names no framework", ctl.ID, ctl.Mapping)
		}
		for _, bad := range problems(ctl, repo) {
			t.Error(bad)
		}
	}
	t.Logf("%d controls; checks by status: %v", len(controls), byStatus)

	// Each kind of mistake is found.
	for _, planted := range []struct {
		why string
		ctl Control
	}{
		{"a running make target that does not exist", Control{Checks: []Check{{Frequency: "CI", Status: "running", How: "make no-such-target"}}}},
		{"a running job that does not exist", Control{Checks: []Check{{Frequency: "N", Status: "running", How: "job no.such.job"}}}},
		{"a check key not in the catalogue", Control{Checks: []Check{{Frequency: "D", Status: "running", How: "check no.such-check"}}}},
		{"an undefined verification", Control{Checks: []Check{{Frequency: "N", Status: "planned", How: "verify no.such-verification", Arrives: "p1-compliance-center"}}}},
		{"an undefined attestation", Control{Checks: []Check{{Frequency: "Q", Status: "running", How: "attestation no-such-review"}}}},
		{"a cloud check naming no module test", Control{Checks: []Check{{Frequency: "D", Status: "cloud", How: "tofu environment/no_such_run"}}}},
		{"a cloud check naming no test at all", Control{Checks: []Check{{Frequency: "D", Status: "cloud", How: "make infra-check"}}}},
		{"a module test that is not a cloud check", Control{Checks: []Check{{Frequency: "D", Status: "running", How: "tofu environment/spec"}}}},
		{"a planned check arriving with no change", Control{Checks: []Check{{Frequency: "N", Status: "planned", Arrives: "P1"}}}},
		{"two jobs for one frequency", Control{Checks: []Check{{Frequency: "N", Status: "running", How: "job ledger.verify"},
			{Frequency: "N", Status: "running", How: "job audit.verify"}}}},
	} {
		planted.ctl.ID = "CTL-X-01"
		if got := problems(planted.ctl, repo); len(got) != 1 {
			t.Errorf("%s: want it reported once, got %v", planted.why, got)
		}
	}
	for _, fine := range []Check{{Frequency: "N", Status: "planned", How: "job not.built.yet", Arrives: "p1-compliance-center (slice 3)"},
		{Frequency: "D", Status: "cloud", How: "tofu environment/spec"}, {Frequency: "D", Status: "running", How: "check ledger.sub-ledgers"}} {
		if got := problems(Control{ID: "CTL-X-01", Checks: []Check{fine}}, repo); len(got) != 0 {
			t.Errorf("%+v: want no problem, got %v", fine, got)
		}
	}
}

// problems returns what is wrong with what the checks of ctl name: a running check's make target, job, test or CI job
// that does not exist; a check key the catalogue lacks; an undefined verification or attestation, whatever the status;
// a cloud check that names no infra/ module test or CI job; a module test named by anything but a cloud check; a
// planned check whose arrival names no change of the plan; and a second job for one frequency, whose evidence record
// (<control>-<frequency>.json) would take the first one's place.
func problems(ctl Control, repo names) []string {
	var out []string
	jobs := map[string]bool{}
	for _, ch := range ctl.Checks {
		kind, target := ch.Kind()
		where := ctl.ID + " (" + ch.Frequency + "): "
		if kind == KindJob {
			if jobs[ch.Frequency] {
				out = append(out, where+"a second job for one frequency, whose evidence would replace the first's")
			}
			jobs[ch.Frequency] = true
		}
		var known bool
		switch kind {
		case KindCheck:
			_, known = repo.catalogue[target]
		case KindVerify:
			_, known = Verifications[target]
		case KindAttestation:
			_, known = Attestations[target]
		case KindTofu:
			known = repo.tofuRuns[target]
			if ch.Status != "cloud" {
				out = append(out, where+ch.How+" is a module test, which only a cloud check names")
				continue
			}
		case KindMake, KindJob, KindTest, KindCI:
			known = ch.Status != "running" && ch.Status != "cloud" ||
				map[string]map[string]bool{KindMake: repo.makeTargets, KindJob: repo.jobs, KindTest: repo.tests, KindCI: repo.ciJobs}[kind][target]
		default:
			known = ch.How == "" && ch.Status != "running" && ch.Status != "cloud"
		}
		switch {
		case ch.Status == "cloud" && kind != KindTofu && kind != KindCI:
			out = append(out, where+"a cloud check names an infra/ module test (tofu <module>/<run>) or a CI job, not "+strconv.Quote(ch.How))
		case !known:
			out = append(out, where+ch.How+" names no "+kind+" that exists")
		}
		if ch.Status == "planned" {
			change, _, _ := strings.Cut(ch.Arrives, " ")
			if !repo.changes[strings.TrimRight(change, ":;,")] {
				out = append(out, where+"it arrives with "+strconv.Quote(ch.Arrives)+", which names no change of the plan")
			}
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

// What a ⚠ or ⏰ of each catalogue check that proves a control means for it. The dashboard reads either as overdue
// (design D1), so each check is read here before a control relies on it, and a check that only gives ✖ says so.
var catalogueWarnings = map[string]string{
	"ledger.sub-ledgers":                  "an amount on a control account that no document supports awaits review; a sub-ledger that differs is ✖",
	"assets.register":                     "an amount on an asset account that no asset supports, or a purchase not in the register, awaits review; a register that differs is ✖",
	"close.closed-unchanged":              "it does not warn: a closed month that changed is ✖",
	"close.checklist":                     "it does not warn: an unfinished checklist at the close is ✖",
	"close.month-unclosed":                "a month is still open 15 days after its end: its close is overdue",
	"controls.sod-conflicts":              "it does not warn: a conflict the team mode does not cover is ✖",
	"controls.review-log-overdue":         "a logged action has waited more than 14 days for another founder's review: overdue",
	"controls.access-review-overdue":      "a quarter ended more than 14 days ago without its access review: overdue",
	"controls.accountant-review-overdue":  "a quarter ended more than 60 days ago without the accountant's review: overdue",
	"controls.break-glass-unreviewed":     "it does not warn: a break-glass unreviewed by its due day is ✖",
	"controls.external-users-past-expiry": "it does not warn: an external user still a member past their end date is ✖",
	"parameters.packs-current":            "a pack in force is past its review date, or a later version waits to be activated: its review is overdue",
	"founders.conversion-registered":      "⏰ an approved conversion is not registered by ACRA in the days set, or its approval lapses within 30 days",
	"founders.share-capital-registered":   "it does not warn: share capital that differs from the register is ✖",
	"equity.acra-register":                "the register differs from the latest ACRA profile typed in: a reconciliation with ACRA is due",
	"gst.return-due":                      "⏰ a GST return is due within 7 days or past its due day with no filing recorded",
	"compliance.deadlines":                "⏰ a statutory deadline is due within 14 days or past with nothing recorded",
	"scheduler.jobs-on-time":              "it does not warn: a job late or failed is ✖",
}

// The catalogue checks that prove controls are each listed with what their warnings mean, and the list names no check
// that proves none (design D1, D2).
// req: CMP-01
// req: CTL-FIN-03, CTL-FIN-04, CTL-FIN-06, CTL-FIN-07, CTL-FIN-08, CTL-FIN-09, CTL-FIN-10, CTL-FIN-11, CTL-FIN-12
// req: CTL-IAM-02, CTL-STAT-01, CTL-STAT-02, CTL-STAT-03, CTL-STAT-04
func TestTheCatalogueChecksThatProveControls(t *testing.T) {
	controls, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	proves := map[string][]string{}
	for _, ctl := range controls {
		for _, ch := range ctl.Checks {
			if kind, key := ch.Kind(); kind == KindCheck && !slices.Contains(proves[key], ctl.ID) {
				proves[key] = append(proves[key], ctl.ID)
			}
		}
	}
	for _, key := range slices.Sorted(maps.Keys(proves)) {
		means, ok := catalogueWarnings[key]
		if !ok {
			t.Errorf("%s proves %v: say in catalogueWarnings what its warning means for them", key, proves[key])
			continue
		}
		t.Logf("%-36s %-28s %s", key, strings.Join(proves[key], ","), means)
	}
	for key := range catalogueWarnings {
		if proves[key] == nil {
			t.Errorf("%s is in catalogueWarnings but proves no control", key)
		}
	}
}

// req: CMP-01
func TestMappingFrameworks(t *testing.T) {
	for mapping, want := range map[string][]string{
		"SOC2 PI1; COSO P10":                          {"soc2", "coso"},
		"ISO 8.9, 8.19; SOC2 CC7.1; CE":               {"iso27001", "soc2", "csa-ce"},
		"ISO 5.18; SOC2 CC6.2–6.3; CT":                {"iso27001", "soc2", "csa-ct"},
		"AG (testing); IMDA Starter Kit; OWASP LLM01": {"imda-ai", "owasp"},
		"ISO 8.29; ASVS":                              {"iso27001", "owasp"},
		"ISA 240 expectations; COSO P16":              {"coso", "isa"},
		"PDPA (PDPC/CSA advisory)":                    {"pdpa"},
		"Income Tax Act (IRAS)":                       {"sg-statute"},
		"COSO P10; Companies Act; (rev 0.19) ACRA's register (no section; checked 14 Oct 2026)": {"sg-statute", "coso"},
	} {
		got, err := MappingFrameworks(mapping)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("MappingFrameworks(%q) = %v, %v; want %v", mapping, got, err, want)
		}
	}
	for _, unknown := range []string{"NIST 800-53", "SOC2 CC6.1; CEO approval", "ISO; "} {
		if got, err := MappingFrameworks(unknown); err == nil {
			t.Errorf("MappingFrameworks(%q) = %v, want an error", unknown, got)
		}
	}
}

// Each check that runs in CI names a release asset that the release's evidence job attaches: an artefact or the logs of
// a workflow of release-assets.json (design D3; scripts/release-evidence.py self-test checks the workflows upload them).
// req: CMP-01
func TestEveryCICheckHasItsReleaseAsset(t *testing.T) {
	controls, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	assets, err := LoadReleaseAssets()
	if err != nil {
		t.Fatal(err)
	}
	held := map[string]bool{}
	for _, wf := range assets.Workflows {
		for _, name := range append(slices.Clone(wf.Artefacts), wf.Logs) {
			held[name+".zip"] = true
		}
	}
	n := 0
	for _, c := range controls {
		for _, ch := range c.Checks {
			if ch.InCI() {
				n++
				if a := assets.Asset(ch); !held[a] {
					t.Errorf("%s (%s) %s: its evidence is %s, which the release does not hold", c.ID, ch.Frequency, ch.How, a)
				}
			}
		}
	}
	if n < 20 {
		t.Errorf("only %d checks run in CI", n)
	}
}

// Fyniq's starting drafts are CMP-07's eight policies, each with its front matter, its official sources with the day they
// were read, the controls it points to, and its text; a draft that bears on the law marks those clauses for counsel
// (design D8).
// req: CMP-07
func TestThePolicyDrafts(t *testing.T) {
	drafts, err := PolicyDrafts()
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	controls, _ := Load()
	known := map[string]bool{}
	for _, c := range controls {
		known[c.ID] = true
	}
	for _, d := range drafts {
		keys = append(keys, d.Key)
		if d.Title == "" || d.Draft != "fyniq-policy-draft@1" || len(d.Sources) == 0 || len(d.Controls) == 0 || len(d.SHA256) != 64 ||
			!strings.HasPrefix(d.Body, "# "+d.Title) {
			t.Errorf("%s: %+v", d.Key, d)
		}
		for _, s := range d.Sources {
			if !strings.HasPrefix(s.URL, "https://www.") && !strings.HasPrefix(s.URL, "https://csa.gov.sg/") || s.Title == "" ||
				!regexp.MustCompile(`^20[0-9]{2}-[0-9]{2}-[0-9]{2}$`).MatchString(s.Retrieved) {
				t.Errorf("%s: a source without an official URL, a title or its day: %+v", d.Key, s)
			}
			host := strings.Split(strings.TrimPrefix(s.URL, "https://"), "/")[0]
			if !strings.HasSuffix(host, ".gov.sg") && host != "csa.gov.sg" {
				t.Errorf("%s: %s is not an official Singapore source", d.Key, s.URL)
			}
		}
		for _, c := range d.Controls {
			if !known[c] {
				t.Errorf("%s points to %s, which the registry does not hold", d.Key, c)
			}
		}
	}
	if !slices.Equal(keys, PolicyKeys) {
		t.Errorf("the drafts are %v, want CMP-07's %v", keys, PolicyKeys)
	}
	for _, d := range drafts {
		if (d.Key == "data-protection" || d.Key == "incident-response") && !strings.Contains(d.Body, "[counsel to confirm]") {
			t.Errorf("%s bears on the law and marks no clause for counsel", d.Key)
		}
	}
}
