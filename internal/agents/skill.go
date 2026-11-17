// Package agents is the scheduled agents' framework (PRD-001 §9.12; p1-scheduled-agents): their skills, as the release
// ships them, and what a run of each may read and produce.
package agents

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"sync"

	"go.yaml.in/yaml/v3"

	skillfiles "github.com/cognizhi/fyniq/agents"
	"github.com/cognizhi/fyniq/internal/aitool"
	"github.com/cognizhi/fyniq/internal/proposal"
)

// Schema is the skill files' front matter format.
const Schema = "agent-skill@1"

// RunReport is the output every agent produces: its run's report (AI-65). Every other output is a catalogue task type,
// proposed for a person to approve.
const RunReport = "run_report"

// Release are the agents this release runs, by their skill's name. Nemura's people assistant (PRD-002) is not Fyniq's.
var Release = []string{"company-secretary"}

// Profiles are what an agent may read, by its skill's permission_profile (AI-63): the read tools within reach. A skill's
// tools stay within its profile's; a run reads as the Owner it acts for, within them.
var Profiles = map[string][]string{
	// compliance-read: ledger summaries, the statutory calendar, the equity register, officers and controllers,
	// settings and evidence metadata, through the checks, the semantic layer, records and the packs.
	"compliance-read": {"checks.list", "checks.explain", "metrics.query", "records.get", "records.search", "packs.get", "kb.index",
		"kb.read", "kb.search", "calc"},
}

// Skill is an agent's skill file (AI-61): what it is, its version, the read tools it may call, the outputs it may
// produce, its permission profile, and its instructions, the text below the front matter, which are its run's system
// prompt.
type Skill struct {
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	Version      int      `yaml:"version"`
	Schema       string   `yaml:"schema"`
	Tools        []string `yaml:"tools"`
	Outputs      []string `yaml:"outputs"`
	Profile      string   `yaml:"permission_profile"`
	Instructions string   `yaml:"-"`
	SHA256       string   `yaml:"-"` // of the whole file, as pinned in agents/agents.lock.json
}

// Agent is the skill's agent as the AI routes and budgets name it: company-secretary → company_secretary (AI-74).
func (s Skill) Agent() string { return strings.ReplaceAll(s.Name, "-", "_") }

// Offered are the skill's tools that registry has: what a run offers the model. A tool the release lacks (the
// knowledge base's, until p1-knowledge-base) is not offered.
func (s Skill) Offered(registry *aitool.Registry) []string {
	var out []string
	for _, t := range s.Tools {
		if _, ok := registry.Get(t); ok {
			out = append(out, t)
		}
	}
	return out
}

// Proposes are the skill's outputs that are task types the catalogue has: what a run may propose, once each is built
// and on for the organisation.
func (s Skill) Proposes(catalogue *proposal.Catalogue) []string {
	var out []string
	for _, o := range s.Outputs {
		if _, ok := catalogue.Get(o); ok {
			out = append(out, o)
		}
	}
	return out
}

// Skills are the release's agents' skills.
type Skills struct {
	byName map[string]Skill
	names  []string
}

// Get is the skill named name, if the release runs it.
func (s *Skills) Get(name string) (Skill, bool) {
	k, ok := s.byName[name]
	return k, ok
}

// All are the release's skills, by name.
func (s *Skills) All() []Skill {
	out := make([]Skill, 0, len(s.names))
	for _, n := range s.names {
		out = append(out, s.byName[n])
	}
	return out
}

var defaultSkills = sync.OnceValues(func() (*Skills, error) { return Load(skillfiles.Skills, Release) })

// Default are the skills the release ships, from agents/. The tests check them, so a broken one is a build that never
// passed: it panics.
func Default() *Skills {
	s, err := defaultSkills()
	if err != nil {
		panic("the agents' skills: " + err.Error())
	}
	return s
}

var (
	frontMatter = regexp.MustCompile(`(?s)\A---\n(.*?)\n---\n(.*)\z`)
	skillName   = regexp.MustCompile(`^[a-z][a-z-]{1,40}$`)
	taskType    = regexp.MustCompile(`^[a-z]+\.[a-z]+(_[a-z]+)*$`)
)

// Load reads and checks the skills of names in fsys, each as <name>/SKILL.md.
func Load(fsys fs.FS, names []string) (*Skills, error) {
	out := &Skills{byName: map[string]Skill{}}
	for _, name := range names {
		raw, err := fs.ReadFile(fsys, name+"/SKILL.md")
		if err != nil {
			return nil, fmt.Errorf("agent %s: %w", name, err)
		}
		s, err := parse(raw)
		if err == nil {
			err = check(name, s)
		}
		if err != nil {
			return nil, fmt.Errorf("agents/%s/SKILL.md: %w", name, err)
		}
		out.byName[name], out.names = s, append(out.names, name)
	}
	return out, nil
}

func parse(raw []byte) (Skill, error) {
	m := frontMatter.FindSubmatch(bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n")))
	if m == nil {
		return Skill{}, errors.New("no front matter between --- lines")
	}
	var s Skill
	dec := yaml.NewDecoder(bytes.NewReader(m[1]))
	dec.KnownFields(true)
	if err := dec.Decode(&s); err != nil {
		return Skill{}, fmt.Errorf("its front matter: %w", err)
	}
	sum := sha256.Sum256(raw)
	s.Instructions, s.SHA256 = strings.TrimSpace(string(m[2])), hex.EncodeToString(sum[:])
	return s, nil
}

// check refuses a skill that is not bounded (AI-61, AI-66, CTL-AI-09): its own name and schema, tools that are reads
// within its profile, and outputs that are its report or catalogue task types off the never-list.
func check(name string, s Skill) error {
	reach, known := Profiles[s.Profile]
	switch {
	case s.Name != name || !skillName.MatchString(s.Name):
		return fmt.Errorf("its name %q is not its directory's, %q", s.Name, name)
	case s.Schema != Schema:
		return fmt.Errorf("schema %q: a skill is %s", s.Schema, Schema)
	case s.Version < 1:
		return fmt.Errorf("version %d: a skill's version starts at 1", s.Version)
	case strings.TrimSpace(s.Description) == "" || s.Instructions == "":
		return errors.New("it has no description or no instructions")
	case !known:
		return fmt.Errorf("permission profile %q is not one of the release's", s.Profile)
	case !slices.Contains(s.Outputs, RunReport):
		return fmt.Errorf("its outputs do not include %s: every run reports", RunReport)
	}
	for _, t := range s.Tools {
		if err := aitool.ReadName(t); err != nil {
			return fmt.Errorf("tool %s: %w", t, err)
		}
		if !slices.Contains(reach, t) {
			return fmt.Errorf("tool %s is beyond its permission profile %s", t, s.Profile)
		}
	}
	for _, o := range s.Outputs {
		if o == RunReport {
			continue
		}
		if !taskType.MatchString(o) {
			return fmt.Errorf("output %q is neither %s nor a task type", o, RunReport)
		}
		if why := proposal.Refuse(o, ""); why != "" {
			return fmt.Errorf("output %s can never be proposed: it is %s (§9.6)", o, why)
		}
	}
	return nil
}
