// Package compliance is the control registry of PRD-001 Appendix F as code: one file per control in
// controls/, validated against control.schema.json. Each control names its owner and, per frequency,
// how it is checked here (a make target, a platform job, a Go test or a CI job) or when its check
// arrives. scripts/controls.py keeps the PRD's text in step; the Compliance Center (P1) reads the same
// files and the evidence the checks leave.
package compliance

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

//go:embed controls/*.yaml control.schema.json
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
}

// Check is how one frequency of a control is checked.
type Check struct {
	Frequency string `yaml:"frequency" json:"frequency"` // CI, D, N, W, M, Q, S (semi-annual) or E (on an event)
	Status    string `yaml:"status" json:"status"`       // running, planned or manual
	How       string `yaml:"how,omitempty" json:"how,omitempty"`
	Evidence  string `yaml:"evidence,omitempty" json:"evidence,omitempty"`
	Arrives   string `yaml:"arrives,omitempty" json:"arrives,omitempty"`
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
