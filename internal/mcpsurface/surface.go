// Package mcpsurface is the tool surface an MCP client sees (PRD-001 §9.7, AI-16, AI-17, AI-37, AI-40, CTL-MCP-02;
// p1-mcp-first-party design D4 and D5): the registry's read tools, each with the scope it needs, generated into a
// manifest whose every definition is pinned by a SHA-256 that the server checks before it serves one.
//
// The surface is default-deny. A tool, or a value of the input that selects what a tool reads, that
// contracts/ai/mcp-scopes.json does not name is not exposed and not callable, so a new tool is invisible to MCP until a
// reviewer maps it. A generic tool such as records.get is mapped by its input (the kind of record), so the scope for
// reports cannot reach a bill or a journal. Build refuses what an MCP client may never have: a tool that is not a read,
// a write verb or a verb on the never-list (§9.6), a tool that does not run through the API, or a scope outside the read
// areas (payroll and equity are never mapped in P1).
package mcpsurface

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	contracts "github.com/cognizhi/fyniq/contracts/ai"
	"github.com/cognizhi/fyniq/internal/aitool"
	"github.com/cognizhi/fyniq/internal/proposal"
)

// ReadScope is the scope shape of a read tool over MCP: fyniq.read.<area>, for the areas P1 has a tool for. Payroll and
// equity are never mapped here (AI-02, AI-37); the proposals' read scope belongs to the proposal tools (task 7.2).
var ReadScope = regexp.MustCompile(`^fyniq\.read\.(reports|sales|purchases|banking|accounting|planning|compliance)$`)

// InputRule maps the values of one input of a tool to the scope reading that value needs.
type InputRule struct {
	Field    string `json:"field"`
	Required bool   `json:"required"` // the input must be given: leaving it out would read across every value
	Optional bool   `json:"optional"` // when left out, it needs nothing more
	// Describe replaces the input's description over MCP, when the registry's would mislead there (an input the
	// registry leaves optional, but MCP requires, since leaving it out would read every kind).
	Describe string            `json:"describe,omitempty"`
	Map      map[string]string `json:"map"`
}

// Rule is what a tool needs: a scope of its own, and a scope for the value of each input that selects what it reads. Every
// scope that applies is needed.
type Rule struct {
	Scope  string      `json:"scope,omitempty"`
	Inputs []InputRule `json:"inputs,omitempty"`
}

// Scopes is mcp-scopes.json.
type Scopes struct {
	Version   int               `json:"version"`
	Note      string            `json:"note"`
	Tools     map[string]Rule   `json:"tools"`
	Unexposed map[string]string `json:"unexposed"`
}

// LoadScopes reads the scope map strictly.
func LoadScopes(raw []byte) (Scopes, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var s Scopes
	if err := d.Decode(&s); err != nil {
		return Scopes{}, fmt.Errorf("mcp-scopes.json: %w", err)
	}
	if s.Version != 1 {
		return Scopes{}, fmt.Errorf("mcp-scopes.json: version %d is not one this release reads", s.Version)
	}
	for tool, r := range s.Tools {
		if r.Scope == "" && len(r.Inputs) == 0 {
			return Scopes{}, fmt.Errorf("mcp-scopes.json: %s needs a scope and names none", tool)
		}
		if r.Scope != "" && !ReadScope.MatchString(r.Scope) {
			return Scopes{}, fmt.Errorf("mcp-scopes.json: %s needs %q, which is not a read scope an MCP client may hold in P1", tool, r.Scope)
		}
		for _, in := range r.Inputs {
			if in.Field == "" || len(in.Map) == 0 || (in.Required && in.Optional) {
				return Scopes{}, fmt.Errorf("mcp-scopes.json: %s names an input with no field, no values or both required and optional", tool)
			}
			for v, sc := range in.Map {
				if !ReadScope.MatchString(sc) {
					return Scopes{}, fmt.Errorf("mcp-scopes.json: %s.%s=%s needs %q, which is not a read scope an MCP client may hold in P1", tool, in.Field, v, sc)
				}
			}
		}
	}
	return s, nil
}

// Needs is every scope a call of tool with input needs, sorted, or an error when the tool is not exposed, a value is not
// one the map names, or an input that must be given is not. The error names what to do: a refusal is a refusal to expose,
// never a guess at a scope.
func (s Scopes) Needs(tool string, input map[string]any) ([]string, error) {
	r, ok := s.Tools[tool]
	if !ok {
		return nil, fmt.Errorf("%s is not a tool this server exposes", tool)
	}
	var need []string
	if r.Scope != "" {
		need = append(need, r.Scope)
	}
	for _, in := range r.Inputs {
		v, given := input[in.Field]
		switch {
		case !given || v == nil:
			if !in.Optional {
				return nil, fmt.Errorf("%s needs %s: leaving it out would read every kind", tool, in.Field)
			}
			continue
		}
		str, isString := v.(string)
		sc, mapped := in.Map[str]
		if !isString || !mapped {
			return nil, fmt.Errorf("%s does not expose %s=%v", tool, in.Field, v)
		}
		need = append(need, sc)
	}
	slices.Sort(need)
	return slices.Compact(need), nil
}

// Definition is a tool as an MCP client is shown it, and the pin of it.
type Definition struct {
	Name        string             `json:"name"`
	Title       string             `json:"title"`
	Description string             `json:"description"`
	InputSchema json.RawMessage    `json:"inputSchema"`
	Annotations aitool.Annotations `json:"annotations"`
	// Scopes are the scopes the tool may need, over every value of its inputs: a client without any of them cannot call it
	// at all, so it is not listed to that client.
	Scopes []string `json:"scopes"`
	// Version is the registry's version of the tool; a change to the tool is a new version (AI-17).
	Version int `json:"version"`
	// SHA256 is the hash of the definition without it: name, title, description, input schema, annotations, scopes and
	// version, as canonical JSON.
	SHA256 string `json:"sha256"`
}

// Manifest is contracts/ai/mcp-manifest.json.
type Manifest struct {
	Version int          `json:"version"`
	Tools   []Definition `json:"tools"`
}

// ManifestVersion is the manifest's own format.
const ManifestVersion = 1

// hash is the definition's SHA-256, from canonical JSON (object keys sorted) of everything a client is shown.
func (d Definition) hash() (string, error) {
	d.SHA256 = ""
	b, err := json.Marshal(d)
	if err != nil {
		return "", err
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return "", err
	}
	delete(v.(map[string]any), "sha256")
	canonical, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// Build is the manifest of the tools the scope map exposes, or an error naming the first tool that may not be exposed. It
// never exposes a tool the map does not name. Tools are in the registry's order.
func Build(tools []aitool.Tool, s Scopes) (Manifest, error) {
	m := Manifest{Version: ManifestVersion}
	named := map[string]bool{}
	for _, t := range tools {
		r, ok := s.Tools[t.Name]
		if !ok {
			continue // default deny
		}
		named[t.Name] = true
		if err := mayExpose(t); err != nil {
			return Manifest{}, fmt.Errorf("%s may not be exposed over MCP: %w", t.Name, err)
		}
		schema, err := tighten(t.InputSchema, r)
		if err != nil {
			return Manifest{}, fmt.Errorf("%s: %w", t.Name, err)
		}
		d := Definition{Name: t.Name, Title: t.Title, Description: t.Description, InputSchema: schema, Annotations: t.Annotations,
			Scopes: scopesOf(r), Version: t.Version}
		if d.SHA256, err = d.hash(); err != nil {
			return Manifest{}, err
		}
		m.Tools = append(m.Tools, d)
	}
	for name := range s.Tools {
		if !named[name] {
			return Manifest{}, fmt.Errorf("mcp-scopes.json names %s, which is not a tool of the registry", name)
		}
	}
	return m, nil
}

// mayExpose is what an MCP client may never be given (AI-16, AI-40; the never-list, §9.6): only a bounded read that runs
// through the API, never a write, an approval, a payment, a setting, an export, a filing or the audit trail.
func mayExpose(t aitool.Tool) error {
	verb := t.Name[strings.LastIndex(t.Name, ".")+1:]
	switch {
	case t.Annotations.ReadOnly == nil || !*t.Annotations.ReadOnly:
		return errors.New("it is not annotated read-only")
	case t.Annotations.OpenWorld == nil || *t.Annotations.OpenWorld:
		return errors.New("it is not annotated closed-world")
	case t.Runs != aitool.RunsAPI:
		return errors.New("it does not run through the API as the person")
	}
	if err := aitool.ReadName(t.Name); err != nil {
		return err
	}
	if what := proposal.Refuse(verb, ""); what != "" {
		return fmt.Errorf("its verb %q is on the never-list: %s", verb, what)
	}
	return nil
}

func scopesOf(r Rule) []string {
	set := map[string]bool{}
	if r.Scope != "" {
		set[r.Scope] = true
	}
	for _, in := range r.Inputs {
		for _, sc := range in.Map {
			set[sc] = true
		}
	}
	out := make([]string, 0, len(set))
	for sc := range set {
		out = append(out, sc)
	}
	slices.Sort(out)
	return out
}

// tighten adds to a tool's input schema what the scope map requires of it over MCP: an input the map says must be given
// becomes required, so a client cannot read every kind at once.
func tighten(raw json.RawMessage, r Rule) (json.RawMessage, error) {
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, err
	}
	props, _ := schema["properties"].(map[string]any)
	required, _ := schema["required"].([]any)
	for _, in := range r.Inputs {
		if _, ok := props[in.Field]; !ok {
			return nil, fmt.Errorf("the scope map names the input %s, which the tool does not take", in.Field)
		}
		if in.Describe != "" {
			props[in.Field].(map[string]any)["description"] = in.Describe
		}
		if in.Required && !slices.Contains(required, any(in.Field)) {
			required = append(required, in.Field)
		}
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return json.Marshal(schema)
}

// Verify checks a manifest, as the server does before it serves any: its definitions hash to their pins, its tools are
// unique, and it is the manifest Build gives from the tools and the scope map, so an altered definition, an added tool or a
// dropped scope is refused (CTL-MCP-02).
func Verify(m Manifest, tools []aitool.Tool, s Scopes) error {
	if m.Version != ManifestVersion {
		return fmt.Errorf("the manifest is version %d, not %d", m.Version, ManifestVersion)
	}
	seen := map[string]bool{}
	for _, d := range m.Tools {
		got, err := d.hash()
		if err != nil {
			return err
		}
		if got != d.SHA256 {
			return fmt.Errorf("%s's definition does not match its pinned hash", d.Name)
		}
		if seen[d.Name] {
			return fmt.Errorf("%s is in the manifest twice", d.Name)
		}
		seen[d.Name] = true
	}
	want, err := Build(tools, s)
	if err != nil {
		return err
	}
	a, _ := json.Marshal(want)
	b, _ := json.Marshal(m)
	if !bytes.Equal(a, b) {
		return errors.New("the manifest is not what the tools and the scope map give: regenerate it and review the difference (go test ./internal/mcpsurface -update)")
	}
	return nil
}

// Default is the manifest the release ships, verified against the registry and the scope map: it panics when it is not,
// as a build that never passed.
func Default() (Manifest, Scopes) {
	s, err := LoadScopes(contracts.MCPScopes)
	if err != nil {
		panic("the MCP scope map: " + err.Error())
	}
	var m Manifest
	if err := json.Unmarshal(contracts.MCPManifest, &m); err != nil {
		panic("the MCP manifest: " + err.Error())
	}
	if err := Verify(m, aitool.Default().Tools(), s); err != nil {
		panic("the MCP manifest: " + err.Error())
	}
	return m, s
}
