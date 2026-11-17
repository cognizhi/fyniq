// Package aitool is the AI's read-tool registry (PRD-001 AI-49, AI-01, AI-19, AI-P1, AI-P2; p1-ai-assistant design
// D5): the tools of contracts/ai/tools, checked as they load. Every tool reads and none writes (CMP-AI-01): each is
// annotated read-only, holds the AI's read scope and nothing else, and takes a strict, bounded input. The registry gives
// the model its tool definitions, under names every provider accepts, and finds a tool again by the name the model
// calls it by. A changed definition is a new version (contracts/ai/tools.lock.json), so a turn records exactly which
// tools it offered.
package aitool

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"

	contracts "github.com/cognizhi/fyniq/contracts/ai"
	"github.com/cognizhi/fyniq/internal/ports"
)

// Where a tool runs: through the API as the person, or in the AI service itself (calc, AI-P6).
const (
	RunsAPI = "api"
	RunsAI  = "ai"
)

// Scope is the one scope a tool may hold: reading as the person, through the turn's delegated token (AI-01, AI-34).
const Scope = "ai.read"

// The most a tool's limits may allow (design D6, AI-04).
const (
	MaxRows         = 50
	MaxTimeoutMS    = 5000
	MaxResultTokens = 4000
)

// Annotations are a tool's hints, as MCP annotates a tool.
type Annotations struct {
	ReadOnly    *bool `json:"readOnlyHint"`
	Destructive *bool `json:"destructiveHint"`
	Idempotent  *bool `json:"idempotentHint"`
	OpenWorld   *bool `json:"openWorldHint"`
}

// Limits are what one call of a tool may return and take (AI-04): rows, time, and the result's token budget, past which
// the tool summarises.
type Limits struct {
	Rows         int `json:"rows"`
	TimeoutMS    int `json:"timeout_ms"`
	ResultTokens int `json:"result_tokens"`
}

// Tool is a read tool's definition.
type Tool struct {
	Name        string          `json:"name"`
	Title       string          `json:"title"`
	Version     int             `json:"version"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Annotations Annotations     `json:"annotations"`
	Runs        string          `json:"runs"`
	Scope       string          `json:"scope"`
	Limits      Limits          `json:"limits"`
	// SHA256 is the hash of the definition's canonical JSON.
	SHA256 string `json:"-"`
}

// Model is the tool as the model is offered it.
func (t Tool) Model() ports.ModelTool {
	return ports.ModelTool{Name: ModelName(t.Name), Description: t.Description, Schema: t.InputSchema}
}

// ModelName is a tool's name as the providers take it: their tool names have no dots (metrics.query is metrics_query).
func ModelName(name string) string { return strings.ReplaceAll(name, ".", "_") }

// Registry is the read tools, by name.
type Registry struct {
	tools   []Tool
	byName  map[string]int
	byModel map[string]int
}

// Tools are the registry's tools, by name.
func (r *Registry) Tools() []Tool { return slices.Clone(r.tools) }

// Get is the tool named name.
func (r *Registry) Get(name string) (Tool, bool) {
	i, ok := r.byName[name]
	if !ok {
		return Tool{}, false
	}
	return r.tools[i], true
}

// ByModelName is the tool the model calls name.
func (r *Registry) ByModelName(name string) (Tool, bool) {
	i, ok := r.byModel[name]
	if !ok {
		return Tool{}, false
	}
	return r.tools[i], true
}

// Model are the named tools as the model is offered them, in the order named, each once.
func (r *Registry) Model(names ...string) ([]ports.ModelTool, error) {
	var out []ports.ModelTool
	var seen []string
	for _, n := range names {
		t, ok := r.Get(n)
		if !ok {
			return nil, fmt.Errorf("%s is not a tool of the registry", n)
		}
		if !slices.Contains(seen, n) {
			out, seen = append(out, t.Model()), append(seen, n)
		}
	}
	return out, nil
}

var defaultRegistry = sync.OnceValues(func() (*Registry, error) { return Load(contracts.Tools) })

// Default is the registry the release ships, from contracts/ai/tools. The tests check it, so a broken one is a build
// that never passed: it panics.
func Default() *Registry {
	r, err := defaultRegistry()
	if err != nil {
		panic("the AI tool registry: " + err.Error())
	}
	return r
}

// Load reads and checks the tools in fsys's tools directory.
func Load(fsys fs.FS) (*Registry, error) {
	paths, err := fs.Glob(fsys, "tools/*.json")
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no tools in tools/*.json")
	}
	r := &Registry{byName: map[string]int{}, byModel: map[string]int{}}
	for _, p := range paths {
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return nil, err
		}
		t, err := parse(b)
		if err == nil && path.Base(p) != t.Name+".json" {
			err = fmt.Errorf("is named %s: a tool's file is its name and .json", t.Name)
		}
		if err == nil {
			err = check(t)
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if _, dup := r.byModel[ModelName(t.Name)]; dup {
			return nil, fmt.Errorf("%s: another tool has the model name %s", p, ModelName(t.Name))
		}
		r.byName[t.Name], r.byModel[ModelName(t.Name)] = len(r.tools), len(r.tools)
		r.tools = append(r.tools, t)
	}
	return r, nil
}

// parse reads a definition strictly and hashes its canonical JSON.
func parse(b []byte) (Tool, error) {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	var t Tool
	if err := d.Decode(&t); err != nil {
		return Tool{}, err
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return Tool{}, err
	}
	canonical, err := json.Marshal(v) // object keys sorted
	if err != nil {
		return Tool{}, err
	}
	sum := sha256.Sum256(canonical)
	t.SHA256 = hex.EncodeToString(sum[:])
	compact := bytes.Buffer{}
	if err := json.Compact(&compact, t.InputSchema); err != nil {
		return Tool{}, err
	}
	t.InputSchema = compact.Bytes()
	return t, nil
}

var (
	toolName = regexp.MustCompile(`^[a-z][a-z]*(\.[a-z][a-z_]*)?$`)
	// modelName is what Anthropic's, OpenAI's and Gemini's tool names all accept.
	modelName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
	// writeVerbs name what a read tool never does (AI-19; the never-list, PRD-001 §9.6). A tool that proposes comes with
	// p1-ai-proposals, through its own rule.
	writeVerbs = []string{"add", "approve", "cancel", "change", "close", "create", "delete", "edit", "execute", "export", "file",
		"import", "insert", "lock", "pay", "post", "publish", "reject", "release", "remove", "reopen", "reverse", "run", "save",
		"send", "set", "submit", "unlock", "update", "upsert", "void", "write"}
)

// check refuses a definition that is not a bounded read: the rules of design D5 and D6.
func check(t Tool) error {
	verb := t.Name[strings.LastIndex(t.Name, ".")+1:]
	f := func(ok bool, format string, args ...any) error {
		if ok {
			return nil
		}
		return fmt.Errorf(format, args...)
	}
	yes := func(b *bool) bool { return b != nil && *b }
	no := func(b *bool) bool { return b != nil && !*b }
	return firstOf(
		f(toolName.MatchString(t.Name), "the name %q is not lower-case words, an area and a dot before the last", t.Name),
		f(modelName.MatchString(ModelName(t.Name)), "the model name %q is not one every provider takes", ModelName(t.Name)),
		f(!slices.ContainsFunc(writeVerbs, func(w string) bool { return verb == w || strings.HasPrefix(verb, w+"_") }),
			"%s names a write: an AI tool only reads (AI-19, CMP-AI-01)", t.Name),
		f(t.Title != "" && len(t.Title) <= 60, "the title is empty or over 60 characters"),
		f(t.Version >= 1, "the version is %d, not 1 or more", t.Version),
		f(t.Description != "" && len(t.Description) <= 1000, "the description is empty or over 1,000 characters"),
		f(yes(t.Annotations.ReadOnly), "an AI tool is read-only (annotations.readOnlyHint true): the AI writes only by a proposal (CMP-AI-01, AI-P2)"),
		f(no(t.Annotations.Destructive), "annotations.destructiveHint is not false"),
		f(yes(t.Annotations.Idempotent), "annotations.idempotentHint is not true: a read is"),
		f(no(t.Annotations.OpenWorld), "annotations.openWorldHint is not false: a tool reads the organisation's books, nothing outside"),
		f(t.Runs == RunsAPI || t.Runs == RunsAI, "runs is %q, not api or ai", t.Runs),
		f(t.Scope == Scope, "the scope is %q: an AI tool holds %s only (AI-01)", t.Scope, Scope),
		f(t.Limits.Rows >= 1 && t.Limits.Rows <= MaxRows, "limits.rows is %d, not 1 to %d (AI-04)", t.Limits.Rows, MaxRows),
		f(t.Limits.TimeoutMS >= 1 && t.Limits.TimeoutMS <= MaxTimeoutMS, "limits.timeout_ms is %d, not 1 to %d (AI-04)", t.Limits.TimeoutMS, MaxTimeoutMS),
		f(t.Limits.ResultTokens >= 1 && t.Limits.ResultTokens <= MaxResultTokens, "limits.result_tokens is %d, not 1 to %d (AI-04)",
			t.Limits.ResultTokens, MaxResultTokens),
		CheckInput(t.InputSchema),
	)
}

func firstOf(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// schemaWords are the JSON Schema keywords an input may use: the subset every provider takes. No $ref, so a definition
// is whole on its own.
var schemaWords = []string{"type", "description", "enum", "pattern", "minLength", "maxLength", "minimum", "maximum", "default",
	"properties", "required", "additionalProperties", "items", "minItems", "maxItems"}

// CheckInput refuses an input schema that is not strict and bounded: an object closed to other properties at every
// level, every string bounded by an enum, a pattern or a length, every array by a length (AI-04). The proposal
// catalogue's task types take the same rules (AI-27).
func CheckInput(raw json.RawMessage) error {
	var s map[string]any
	if err := json.Unmarshal(raw, &s); err != nil {
		return fmt.Errorf("inputSchema: %w", err)
	}
	if s["type"] != "object" {
		return fmt.Errorf("inputSchema: the input is not an object")
	}
	return checkSchema("inputSchema", s)
}

func checkSchema(at string, s map[string]any) error {
	for k := range s {
		if !slices.Contains(schemaWords, k) {
			return fmt.Errorf("%s: %s is not a keyword a tool's input may use", at, k)
		}
	}
	if _, ok := s["description"].(string); !ok && at != "inputSchema" && !strings.HasSuffix(at, ".items") {
		return fmt.Errorf("%s: no description", at)
	}
	switch s["type"] {
	case "object":
		if s["additionalProperties"] != false {
			return fmt.Errorf("%s: additionalProperties is not false", at)
		}
		props, _ := s["properties"].(map[string]any)
		if len(props) == 0 {
			return fmt.Errorf("%s: an object with no properties", at)
		}
		required, _ := s["required"].([]any)
		for _, r := range required {
			if _, ok := props[fmt.Sprint(r)]; !ok {
				return fmt.Errorf("%s: %v is required but not a property", at, r)
			}
		}
		names := make([]string, 0, len(props))
		for n := range props {
			names = append(names, n)
		}
		slices.Sort(names)
		for _, n := range names {
			p, ok := props[n].(map[string]any)
			if !ok {
				return fmt.Errorf("%s.%s: not a schema", at, n)
			}
			if err := checkSchema(at+"."+n, p); err != nil {
				return err
			}
		}
	case "array":
		items, ok := s["items"].(map[string]any)
		if !ok {
			return fmt.Errorf("%s: an array without items", at)
		}
		if _, ok := s["maxItems"]; !ok {
			return fmt.Errorf("%s: an array without maxItems", at)
		}
		return checkSchema(at+".items", items)
	case "string":
		_, enum := s["enum"]
		_, pattern := s["pattern"]
		_, length := s["maxLength"]
		if !enum && !pattern && !length {
			return fmt.Errorf("%s: a string with no enum, pattern or maxLength", at)
		}
	case "integer", "boolean":
	default:
		return fmt.Errorf("%s: the type %v is not object, array, string, integer or boolean", at, s["type"])
	}
	return nil
}
