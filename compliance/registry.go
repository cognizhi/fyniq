// Package compliance is the control registry of PRD-001 Appendix F as code: one file per control in
// controls/, validated against control.schema.json. Each control names its owner and, per frequency,
// how it is checked (a make target, a Go test or a CI job in CI; a platform job; a check of the
// organisation's catalogue; a verification of compliance.run; an attestation; or an infra test of what
// only the cloud runs) or when its check arrives. scripts/controls.py keeps the PRD's text in step; the
// Compliance Center reads the same files and the evidence the checks leave (p1-compliance-center D1).
package compliance

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

//go:embed controls/*.yaml control.schema.json release-assets.json risks.yaml data-inventory.yaml policies/*.md notices/*.md dpias/*.md
var files embed.FS

// Control is one control of the registry.
type Control struct {
	ID             string  `yaml:"id" json:"id"`
	Section        string  `yaml:"section" json:"section"`
	Control        string  `yaml:"control" json:"control"`
	Mapping        string  `yaml:"mapping" json:"mapping"`
	AutomatedCheck string  `yaml:"automated_check" json:"automated_check"`
	Frequency      string  `yaml:"frequency" json:"frequency"`
	Evidence       string  `yaml:"evidence" json:"evidence"`
	Owner          string  `yaml:"owner" json:"owner"`
	Checks         []Check `yaml:"checks" json:"checks"`
	// Frameworks are the keys of the frameworks its mapping names, in the order of Frameworks; Load parses them.
	Frameworks []string `yaml:"-" json:"frameworks"`
}

// Check is how one frequency of a control is checked. A frequency may have several checks.
type Check struct {
	Frequency string `yaml:"frequency" json:"frequency"` // CI, D, N, W, M, Q, S (semi-annual) or E (on an event)
	// Status is running; planned (it arrives later); manual; or cloud: built and tested in infra/, it runs only in the
	// cloud, so it is not assessed until then (design D1).
	Status   string `yaml:"status" json:"status"`
	How      string `yaml:"how,omitempty" json:"how,omitempty"`
	Evidence string `yaml:"evidence,omitempty" json:"evidence,omitempty"`
	Arrives  string `yaml:"arrives,omitempty" json:"arrives,omitempty"`
}

// The kinds of How: what runs a check.
const (
	KindMake        = "make"        // a make target, in CI
	KindTest        = "test"        // a Go test, in CI
	KindCI          = "ci"          // a CI job, by its name or ID
	KindJob         = "job"         // a platform job: its latest run
	KindCheck       = "check"       // a check of the organisation's catalogue (Appendix C): its latest result
	KindVerify      = "verify"      // a verification compliance.run makes for each organisation (Verifications)
	KindAttestation = "attestation" // a review a person signs off (Attestations)
	KindTofu        = "tofu"        // a run of an infra/ module's tests, <module>/<run>: what a cloud check proves
)

// Kind returns the kind of the check's How and what it names.
func (c Check) Kind() (kind, target string) {
	kind, target, _ = strings.Cut(c.How, " ")
	return kind, target
}

// InCI reports whether the check runs in CI: a running make target, Go test or CI job. The release the platform runs
// carries its evidence (design D3).
func (c Check) InCI() bool {
	kind, _ := c.Kind()
	return c.Status == "running" && (kind == KindMake || kind == KindTest || kind == KindCI)
}

// ReleaseAssets is where a release keeps the evidence of the checks that run in CI: release-assets.json, which the
// release's evidence job (scripts/release-evidence.py) reads too.
type ReleaseAssets struct {
	Workflows map[string]struct {
		Artefacts []string `json:"artefacts"`
		Logs      string   `json:"logs"`
	} `json:"workflows"`
	ByHow     map[string]string `json:"by_how"`
	ByKind    map[string]string `json:"by_kind"`
	Otherwise string            `json:"otherwise"`
}

// LoadReleaseAssets reads release-assets.json.
func LoadReleaseAssets() (ReleaseAssets, error) {
	var a ReleaseAssets
	b, err := files.ReadFile("release-assets.json")
	if err == nil {
		err = json.Unmarshal(b, &a)
	}
	return a, err
}

// Asset is the name of the release asset that holds the evidence of a check that runs in CI, such as test-report.zip:
// by its how, then by its kind, otherwise its run's log.
func (a ReleaseAssets) Asset(c Check) string {
	if name, ok := a.ByHow[c.How]; ok {
		return name + ".zip"
	}
	kind, _ := c.Kind()
	if name, ok := a.ByKind[kind]; ok {
		return name + ".zip"
	}
	return a.Otherwise + ".zip"
}

// Verifications are the checks compliance.run makes for each organisation, by name (design D4).
var Verifications = map[string]string{
	"payments.dual-control":    "Every payment batch released, and every payee and claimant bank change approved, in the last day was decided by someone other than whoever prepared it, or falls under the team mode's recorded exception",
	"members.second-factor":    "No session of the last day was used beyond reading itself without a second factor, and every active member has one registered",
	"integrations.owned":       "Every integration client has an owner, a class and an unexpired end date, and holds only its class's scopes",
	"audit.exports-reconciled": "Every report export, evidence opening and reveal of the last day has its audit event",
}

// Attestations are the recurring reviews people sign off, by kind (design D6).
var Attestations = map[string]string{
	"review-log":             "Another founder reviews each logged action (Founder mode, weekly)",
	"access-review":          "An Owner attests the access review (each quarter)",
	"accountant-review":      "The external accountant reviews each quarter's books, journals and unusual entries (Founder mode)",
	"close-signoff":          "Someone other than the preparer signs off the close (monthly)",
	"break-glass-review":     "Each break-glass is reviewed after it ends",
	"policy-acknowledgement": "Each member acknowledges each published policy version",
	"risk-review":            "Each risk is reviewed by its review date",
	"data-inventory":         "The Compliance officer reviews the personal-data inventory (each quarter)",
	"dpia":                   "Each processing that needs one has an approved data protection impact assessment",
	"pd-requests":            "Each access or correction request is answered within 30 days, or by the day its written extension names",
}

// Framework is a framework a control may map to, and the words of Appendix F's Mapping cell that name it.
type Framework struct {
	Key, Title string
	Tokens     []string
}

// Frameworks are the frameworks of Appendix F's mappings, in the order the Compliance Center lists them.
var Frameworks = []Framework{
	{"sg-statute", "Singapore statute", []string{"Companies Act", "Income Tax Act", "GST Act", "GST", "IRAS", "CPF Act", "ETA", "ACRA"}},
	{"pdpa", "PDPA", []string{"PDPA"}},
	{"iso27001", "ISO/IEC 27001:2022 Annex A", []string{"ISO"}},
	{"soc2", "SOC 2", []string{"SOC2"}},
	{"csa-ce", "CSA Cyber Essentials", []string{"CE"}},
	{"csa-ct", "CSA Cyber Trust", []string{"CT"}},
	{"imda-ai", "IMDA AI governance", []string{"AG", "IMDA"}},
	{"owasp", "OWASP", []string{"OWASP", "ASVS"}},
	{"coso", "COSO", []string{"COSO"}},
	{"isa", "International Standards on Auditing", []string{"ISA"}},
}

var revisionTag = regexp.MustCompile(`^\(rev [0-9.]+\)\s*`)

// MappingFrameworks returns the keys of the frameworks a Mapping cell names, in the order of Frameworks. Each part of
// the cell, between semicolons outside brackets, starts with a framework's word, such as "ISO 8.32" or "PDPA (PDPC/CSA advisory)"; a part
// that names none is an error, so a new framework is added here before a control maps to it.
func MappingFrameworks(mapping string) ([]string, error) {
	found := map[string]bool{}
	var parts []string
	depth, start := 0, 0
	for i, r := range mapping { // split at the semicolons outside brackets
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ';':
			if depth == 0 {
				parts, start = append(parts, mapping[start:i]), i+1
			}
		}
	}
	for _, part := range append(parts, mapping[start:]) {
		part = revisionTag.ReplaceAllString(strings.TrimSpace(part), "")
		key := ""
		for _, f := range Frameworks {
			for _, t := range f.Tokens {
				rest, ok := strings.CutPrefix(part, t)
				next, _ := utf8.DecodeRuneInString(rest)
				if ok && (rest == "" || !unicode.IsLetter(next) && !unicode.IsDigit(next)) {
					key = f.Key
					break
				}
			}
			if key != "" {
				break
			}
		}
		if key == "" {
			return nil, fmt.Errorf("the mapping %q names no known framework", part)
		}
		found[key] = true
	}
	var out []string
	for _, f := range Frameworks {
		if found[f.Key] {
			out = append(out, f.Key)
		}
	}
	return out, nil
}

// Schema returns the JSON Schema of a control file.
func Schema() []byte {
	b, _ := files.ReadFile("control.schema.json")
	return b
}

// Raw returns each control file's YAML, by file name.
func Raw() (map[string][]byte, error) {
	out := map[string][]byte{}
	err := fs.WalkDir(files, "controls", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := files.ReadFile(path)
		out[strings.TrimPrefix(path, "controls/")] = b
		return err
	})
	return out, err
}

// Load returns every control, by ID.
func Load() ([]Control, error) {
	raw, err := Raw()
	if err != nil {
		return nil, err
	}
	out := make([]Control, 0, len(raw))
	for name, b := range raw {
		var c Control
		if err := yaml.Unmarshal(b, &c); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if name != c.ID+".yaml" {
			return nil, fmt.Errorf("%s holds %s", name, c.ID)
		}
		if c.Frameworks, err = MappingFrameworks(c.Mapping); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Frequencies returns the frequency codes of the PRD's Freq. cell, without its notes in brackets.
func Frequencies(cell string) []string {
	var plain strings.Builder
	depth := 0
	for _, r := range cell {
		if r == '(' {
			depth++
		}
		if depth == 0 {
			plain.WriteRune(r)
		}
		if r == ')' {
			depth--
		}
	}
	var out []string
	for _, t := range strings.Split(plain.String(), ",") {
		t = strings.TrimSpace(t)
		if t == "semi-annual" {
			t = "S"
		}
		seen := false
		for _, o := range out {
			seen = seen || o == t
		}
		if !seen {
			out = append(out, t)
		}
	}
	return out
}
