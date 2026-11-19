package aitool_test

import (
	"encoding/json"
	"flag"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cognizhi/fyniq/internal/aitool"
	"github.com/cognizhi/fyniq/internal/semantic"
)

var update = flag.Bool("update", false, "rewrite contracts/ai/tools.lock.json from the tools as they are, each changed one with a new version")

const lockPath = "../../contracts/ai/tools.lock.json"

// The registry is the launch tools (design D5), each a bounded read the model is offered under a name every provider
// takes, found again by that name.
// req: AI-19
// req: AI-P2
// req: AI-49
func TestTheRegistryIsTheLaunchTools(t *testing.T) {
	r := aitool.Default()
	var names []string
	for _, tool := range r.Tools() {
		names = append(names, tool.Name)
		m := tool.Model()
		back, ok := r.ByModelName(m.Name)
		if !ok || back.Name != tool.Name || strings.Contains(m.Name, ".") || m.Description != tool.Description || !json.Valid(m.Schema) {
			t.Errorf("%s as the model is offered it: %+v", tool.Name, m)
		}
	}
	want := []string{"calc", "checks.explain", "checks.list", "kb.index", "kb.read", "kb.search", "lineage.explain", "metrics.query", "packs.get", "records.get", "records.search"}
	if !slices.Equal(names, want) {
		t.Fatalf("the registry's tools: %v, want %v", names, want)
	}
	if calc, _ := r.Get("calc"); calc.Runs != aitool.RunsAI {
		t.Errorf("calc runs in %q: it runs in the AI service (AI-P6)", calc.Runs)
	}
	for _, n := range want[1:] {
		if tool, _ := r.Get(n); tool.Runs != aitool.RunsAPI {
			t.Errorf("%s runs in %q: it reads through the API as the person (AI-01)", n, tool.Runs)
		}
	}
	got, err := r.Model("metrics.query", "calc", "metrics.query")
	if err != nil || len(got) != 2 || got[0].Name != "metrics_query" || got[1].Name != "calc" {
		t.Errorf("Model(metrics.query, calc, metrics.query) = %+v, %v", got, err)
	}
	if _, err := r.Model("records.update"); err == nil {
		t.Error("a tool not in the registry was offered")
	}
}

// A definition that writes, widens its scope or takes an open input is refused as the registry loads (AI-19, CMP-AI-01,
// AI-04): the registry fails the build rather than offer it.
// req: AI-19
// req: AI-P2
// req: AI-04
func TestTheRegistryRefusesAnythingButABoundedRead(t *testing.T) {
	base := map[string]any{
		"name": "records.peek", "title": "Peek", "version": 1, "description": "Reads a record.",
		"inputSchema": map[string]any{"type": "object", "additionalProperties": false, "required": []any{"id"},
			"properties": map[string]any{"id": map[string]any{"type": "string", "description": "Its ID.", "maxLength": 36}}},
		"annotations": map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false},
		"runs":        "api", "scope": "ai.read", "limits": map[string]any{"rows": 50, "timeout_ms": 5000, "result_tokens": 4000},
	}
	load := func(change func(map[string]any)) error {
		var def map[string]any
		b, _ := json.Marshal(base)
		_ = json.Unmarshal(b, &def)
		change(def)
		b, _ = json.Marshal(def)
		name, _ := def["name"].(string)
		_, err := aitool.Load(fstest.MapFS{"tools/" + name + ".json": {Data: b}})
		return err
	}
	if err := load(func(map[string]any) {}); err != nil {
		t.Fatalf("a bounded read: %v", err)
	}
	input := func(d map[string]any) map[string]any { return d["inputSchema"].(map[string]any) }
	id := func(d map[string]any) map[string]any {
		return input(d)["properties"].(map[string]any)["id"].(map[string]any)
	}
	for what, change := range map[string]func(map[string]any){
		"not read-only":           func(d map[string]any) { d["annotations"].(map[string]any)["readOnlyHint"] = false },
		"no read-only hint":       func(d map[string]any) { delete(d["annotations"].(map[string]any), "readOnlyHint") },
		"destructive":             func(d map[string]any) { d["annotations"].(map[string]any)["destructiveHint"] = true },
		"open to the world":       func(d map[string]any) { d["annotations"].(map[string]any)["openWorldHint"] = true },
		"a write's name":          func(d map[string]any) { d["name"] = "records.update" },
		"a payment's name":        func(d map[string]any) { d["name"] = "payments.pay_bill" },
		"another scope":           func(d map[string]any) { d["scope"] = "fyniq.write" },
		"run elsewhere":           func(d map[string]any) { d["runs"] = "worker" },
		"no version":              func(d map[string]any) { delete(d, "version") },
		"an unknown field":        func(d map[string]any) { d["command"] = "PostJournal" },
		"too many rows":           func(d map[string]any) { d["limits"].(map[string]any)["rows"] = 500 },
		"too long a timeout":      func(d map[string]any) { d["limits"].(map[string]any)["timeout_ms"] = 60000 },
		"no result budget":        func(d map[string]any) { delete(d["limits"].(map[string]any), "result_tokens") },
		"an open input":           func(d map[string]any) { delete(input(d), "additionalProperties") },
		"an unbounded string":     func(d map[string]any) { delete(id(d), "maxLength") },
		"a reference":             func(d map[string]any) { id(d)["$ref"] = "#/defs/id" },
		"an input that is no map": func(d map[string]any) { input(d)["type"] = "string" },
		"a required ghost":        func(d map[string]any) { input(d)["required"] = []any{"id", "ghost"} },
		"an unbounded array": func(d map[string]any) {
			input(d)["properties"].(map[string]any)["ids"] = map[string]any{"type": "array", "description": "IDs.", "items": id(d)}
		},
		"a nested open object": func(d map[string]any) {
			input(d)["properties"].(map[string]any)["filter"] = map[string]any{"type": "object", "description": "A filter.", "properties": map[string]any{"q": id(d)}}
		},
		"a number, not a decimal": func(d map[string]any) { id(d)["type"] = "number" },
		"an undescribed property": func(d map[string]any) { delete(id(d), "description") },
	} {
		if err := load(change); err == nil {
			t.Errorf("%s: loaded", what)
		}
	}
	b, _ := json.Marshal(base)
	if _, err := aitool.Load(fstest.MapFS{"tools/records.other.json": {Data: b}}); err == nil {
		t.Error("a tool in another's file loaded")
	}
	if _, err := aitool.Load(fstest.MapFS{"tools/records.peek.json": {Data: b}, "tools/records_peek.json": {Data: []byte(strings.Replace(string(b), `"records.peek"`, `"records_peek"`, 1))}}); err == nil {
		t.Error("two tools with one model name loaded")
	}
}

// Every change to a tool is a new version (design D5): its definition's hash is pinned beside its version in
// contracts/ai/tools.lock.json, so a turn's record of the tools it offered names exactly what the model saw. A changed
// definition without a new version fails here, and -update refuses to pin it.
// req: AI-19
func TestEveryChangeToAToolIsANewVersion(t *testing.T) {
	type pin struct {
		Version int    `json:"version"`
		SHA256  string `json:"sha256"`
	}
	pinned := map[string]pin{}
	if b, err := os.ReadFile(lockPath); err == nil {
		if err := json.Unmarshal(b, &pinned); err != nil {
			t.Fatal(err)
		}
	} else if !*update {
		t.Fatal(err)
	}
	now := map[string]pin{}
	for _, tool := range aitool.Default().Tools() {
		now[tool.Name] = pin{tool.Version, tool.SHA256}
		was, ok := pinned[tool.Name]
		switch {
		case ok && was.Version == tool.Version && was.SHA256 != tool.SHA256:
			t.Errorf("%s changed without a new version: raise its version from %d", tool.Name, tool.Version)
		case ok && tool.Version < was.Version:
			t.Errorf("%s went back from version %d to %d", tool.Name, was.Version, tool.Version)
		}
	}
	if t.Failed() || maps.Equal(pinned, now) {
		return
	}
	if !*update {
		t.Fatalf("contracts/ai/tools.lock.json is not the tools as they are: run go test ./internal/aitool -run %s -update", t.Name())
	}
	b, _ := json.MarshalIndent(now, "", "  ")
	if err := os.WriteFile(lockPath, append(b, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The tools that read through an operation take what it takes (design D5): metrics.query the semantic layer's measures
// and the query's fields as the API contract has them; records.search the search's kinds; checks.list the checks'
// statuses; packs.get the packs there are. A measure or kind added to the API without the tool fails here.
// req: AI-03
// req: AI-49
func TestTheToolsTakeWhatTheirOperationsTake(t *testing.T) {
	r := aitool.Default()
	props := func(name string) map[string]map[string]any {
		tool, _ := r.Get(name)
		var s struct {
			Properties map[string]map[string]any `json:"properties"`
		}
		if err := json.Unmarshal(tool.InputSchema, &s); err != nil {
			t.Fatal(err)
		}
		return s.Properties
	}
	contract := func(schema string) map[string]map[string]any {
		var s struct {
			Properties map[string]map[string]any `json:"properties"`
		}
		b, err := os.ReadFile(filepath.Join("../../contracts/schemas", schema+".schema.json"))
		if err == nil {
			err = json.Unmarshal(b, &s)
		}
		if err != nil {
			t.Fatal(err)
		}
		return s.Properties
	}
	// The words that bound a value, as the model and the API must agree on them.
	bounds := func(p map[string]any) map[string]any {
		out := map[string]any{}
		for _, k := range []string{"type", "enum", "pattern", "maxLength", "minLength", "minimum", "maximum", "default"} {
			if v, ok := p[k]; ok {
				out[k] = v
			}
		}
		return out
	}

	metrics, query := props("metrics.query"), contract("MetricsQuery")
	if !slices.Equal(slices.Sorted(maps.Keys(metrics)), slices.Sorted(maps.Keys(query))) {
		t.Errorf("metrics.query takes %v; the query has %v", slices.Sorted(maps.Keys(metrics)), slices.Sorted(maps.Keys(query)))
	}
	for k, p := range query {
		if !reflect.DeepEqual(bounds(metrics[k]), bounds(p)) {
			t.Errorf("metrics.query's %s is %v; the query's is %v", k, bounds(metrics[k]), bounds(p))
		}
	}
	var measures []any
	for _, m := range semantic.Measures {
		measures = append(measures, m.Key)
	}
	if !reflect.DeepEqual(metrics["measure"]["enum"], measures) {
		t.Errorf("metrics.query's measures are not the semantic layer's: %v", metrics["measure"]["enum"])
	}

	search, results := props("records.search"), contract("SearchResult")
	if !reflect.DeepEqual(search["kind"]["enum"], results["group"]["enum"]) || search["q"]["maxLength"] != float64(200) {
		t.Errorf("records.search takes %v and q up to %v; the search's kinds are %v", search["kind"]["enum"], search["q"]["maxLength"], results["group"]["enum"])
	}
	if checks, check := props("checks.list"), contract("CheckOut"); !reflect.DeepEqual(checks["status"]["enum"], check["status"]["enum"]) {
		t.Errorf("checks.list's statuses %v; a check's %v", checks["status"]["enum"], check["status"]["enum"])
	}
	dirs, _ := filepath.Glob("../../packs/sg-*")
	var packs []string
	for _, d := range dirs {
		packs = append(packs, filepath.Base(d))
	}
	var offered []string
	for _, p := range props("packs.get")["pack"]["enum"].([]any) {
		offered = append(offered, p.(string))
	}
	if !slices.Equal(slices.Sorted(slices.Values(offered)), packs) {
		t.Errorf("packs.get offers %v; the packs are %v", offered, packs)
	}
}
