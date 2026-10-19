package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
)

// The machine-readable contracts (FR-INT-13), generated from the same document as the contract, so they
// never drift from the code: a JSON Schema per resource in contracts/schemas/, the API reference and
// llms.txt in docs/api/. `make openapi` rewrites them; this test fails while any is stale. The agent
// guide (docs/api/agent-guide.md) is written by hand and checked here for its three parts.

const (
	schemasDir    = "../../contracts/schemas"
	referencePath = "../../docs/api/reference.md"
	llmsPath      = "../../docs/api/llms.txt"
	agentGuide    = "../../docs/api/agent-guide.md"
	repoURL       = "https://github.com/cognizhi/fyniq/blob/main/"
)

var componentRef = regexp.MustCompile(`"#/components/schemas/([A-Za-z0-9_]+)"`)

// schemaFiles are the JSON Schema 2020-12 documents of every resource, by file name: the document's
// component schemas, with their references pointing at each other's files.
func schemaFiles(t *testing.T, o *huma.OpenAPI) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for name, s := range o.Components.Schemas.Map() {
		b, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(componentRef.ReplaceAll(b, []byte(`"$1.schema.json"`)), &doc); err != nil {
			t.Fatal(err)
		}
		doc["$schema"] = "https://json-schema.org/draft/2020-12/schema"
		doc["title"] = name
		pretty, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		out[name+".schema.json"] = append(pretty, '\n')
	}
	return out
}

func schemaName(s *huma.Schema) string {
	if s == nil {
		return ""
	}
	if s.Ref != "" {
		return strings.TrimPrefix(s.Ref, "#/components/schemas/")
	}
	if s.Items != nil {
		if n := schemaName(s.Items); n != "" {
			return n + "[]"
		}
	}
	return ""
}

func schemaLink(name string) string {
	base := strings.TrimSuffix(name, "[]")
	return fmt.Sprintf("[`%s`](../../contracts/schemas/%s.schema.json)", name, base)
}

type documented struct {
	method, path string
	op           *huma.Operation
}

func documentedOperations(o *huma.OpenAPI) []documented {
	var out []documented
	for _, path := range slices.Sorted(maps.Keys(o.Paths)) {
		item := o.Paths[path]
		for _, m := range []struct {
			name string
			op   *huma.Operation
		}{{"GET", item.Get}, {"POST", item.Post}, {"PUT", item.Put}, {"PATCH", item.Patch}, {"DELETE", item.Delete}} {
			if m.op != nil {
				out = append(out, documented{m.name, path, m.op})
			}
		}
	}
	return out
}

var rightNames = map[string]string{"C": "Create", "A": "Approve", "P": "Post", "R": "Release", "V": "View", "M": "View masked"}

// reference is docs/api/reference.md: every operation by tag, with its permission, parameters, bodies,
// responses and error codes.
func reference(o *huma.OpenAPI) []byte {
	var b bytes.Buffer
	b.WriteString("# Fyniq API reference (v1)\n\n")
	b.WriteString("<!-- Generated from the API's code with `make openapi`, like contracts/openapi/fyniq.v1.yaml; never edit by hand. -->\n\n")
	b.WriteString("The operations of Fyniq's public API, generated from the same code as the contract " +
		"([`contracts/openapi/fyniq.v1.yaml`](../../contracts/openapi/fyniq.v1.yaml)). Every resource has a JSON Schema in " +
		"[`contracts/schemas/`](../../contracts/schemas/). AI agents start with the [agent guide](agent-guide.md); " +
		"[`llms.txt`](llms.txt) lists everything for a model.\n\n")
	b.WriteString("## Conventions\n\n" + o.Info.Description + "\n")
	byTag := map[string][]documented{}
	for _, d := range documentedOperations(o) {
		tag := "Other"
		if len(d.op.Tags) > 0 {
			tag = d.op.Tags[0]
		}
		byTag[tag] = append(byTag[tag], d)
	}
	for _, tag := range o.Tags {
		ops := byTag[tag.Name]
		if len(ops) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n## %s\n\n%s\n", tag.Name, tag.Description)
		for _, d := range ops {
			op := d.op
			fmt.Fprintf(&b, "\n### `%s %s`\n\n**%s** (`%s`)", d.method, d.path, op.Summary, op.OperationID)
			if op.Deprecated {
				fmt.Fprintf(&b, ". **Deprecated**: sunset %v", op.Extensions["x-sunset"])
			}
			fmt.Fprintf(&b, "\n\n%s\n\n", op.Description)
			if perm, ok := op.Extensions["x-fyniq-permission"].(map[string]any); ok {
				right := fmt.Sprint(perm["right"])
				fmt.Fprintf(&b, "- **Permission:** %s on %v", rightNames[right], perm["capability"])
				if note, ok := perm["note"].(string); ok && note != "" {
					fmt.Fprintf(&b, ". %s", note)
				}
				b.WriteString("\n")
			}
			if len(op.Parameters) > 0 {
				b.WriteString("- **Parameters:**\n")
				for _, p := range op.Parameters {
					req := ""
					if p.Required {
						req = ", required"
					}
					fmt.Fprintf(&b, "  - `%s` (%s%s): %s\n", p.Name, p.In, req, p.Description)
				}
			}
			if op.RequestBody != nil {
				if mt := op.RequestBody.Content["application/json"]; mt != nil {
					fmt.Fprintf(&b, "- **Request body:** %s\n", schemaLink(schemaName(mt.Schema)))
				}
			}
			b.WriteString("- **Responses:**\n")
			for _, status := range slices.Sorted(maps.Keys(op.Responses)) {
				r := op.Responses[status]
				line := fmt.Sprintf("  - `%s` %s", status, strings.TrimSpace(r.Description))
				for _, ct := range slices.Sorted(maps.Keys(r.Content)) {
					if n := schemaName(r.Content[ct].Schema); n != "" && strings.HasPrefix(status, "2") {
						line += " " + schemaLink(n)
					}
				}
				b.WriteString(line + "\n")
			}
			if codes, ok := op.Extensions["x-fyniq-error-codes"].([]string); ok {
				fmt.Fprintf(&b, "- **Error codes:** `%s`\n", strings.Join(codes, "`, `"))
			}
		}
	}
	return b.Bytes()
}

// llmsTxt is docs/api/llms.txt (llmstxt.org): what the API is, and where a model finds the rest.
func llmsTxt(o *huma.OpenAPI) []byte {
	var b bytes.Buffer
	b.WriteString("# Fyniq API\n\n")
	b.WriteString("> Fyniq's public REST API (/v1) for the accounting, compliance and reporting of a Singapore company. The web app " +
		"uses only this API. AI agents and MCP clients read through it and may only propose changes, which a named person " +
		"approves in the web app; they never approve, pay, file or administer anything.\n\n")
	b.WriteString("Errors are RFC 9457 problem details with a stable code and a fix. Writes take an Idempotency-Key and " +
		"Prefer: return=preview; versioned records need If-Match; lists are cursor-paginated; money is a decimal string " +
		"with its currency, never a JSON number.\n\n")
	b.WriteString("## Docs\n\n")
	fmt.Fprintf(&b, "- [Agent guide](%sdocs/api/agent-guide.md): how to read, how to propose, and what is never allowed\n", repoURL)
	fmt.Fprintf(&b, "- [API reference](%sdocs/api/reference.md): every operation with its permission, parameters, bodies and error codes\n", repoURL)
	fmt.Fprintf(&b, "- [OpenAPI contract](%scontracts/openapi/fyniq.v1.yaml): OpenAPI 3.1, exactly as the API serves it at /v1/openapi.json\n", repoURL)
	fmt.Fprintf(&b, "- [JSON Schemas](%scontracts/schemas/): one per resource (JSON Schema 2020-12)\n", repoURL)
	b.WriteString("\n## Operations\n\n")
	for _, d := range documentedOperations(o) {
		fmt.Fprintf(&b, "- `%s %s` (%s): %s\n", d.method, d.path, d.op.OperationID, d.op.Summary)
	}
	return b.Bytes()
}

// req: FR-INT-13
func TestTheReferenceIsTheContract(t *testing.T) {
	o := testAPI(t).OpenAPI()
	want := schemaFiles(t, o)
	want["../../docs/api/reference.md"] = reference(o)
	want["../../docs/api/llms.txt"] = llmsTxt(o)
	path := func(name string) string {
		if strings.HasPrefix(name, "../") {
			return name
		}
		return filepath.Join(schemasDir, name)
	}
	if *update {
		if err := os.MkdirAll(schemasDir, 0o755); err != nil {
			t.Fatal(err)
		}
		old, _ := filepath.Glob(filepath.Join(schemasDir, "*.schema.json"))
		for _, f := range old {
			if _, keep := want[filepath.Base(f)]; !keep {
				_ = os.Remove(f)
			}
		}
		for name, b := range want {
			if err := os.WriteFile(path(name), b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
	for name, b := range want {
		got, err := os.ReadFile(path(name))
		if err != nil || !bytes.Equal(got, b) {
			t.Errorf("%s is stale or missing (run make openapi): %v", name, err)
		}
	}
	files, _ := filepath.Glob(filepath.Join(schemasDir, "*.schema.json"))
	for _, f := range files {
		if _, ok := want[filepath.Base(f)]; !ok {
			t.Errorf("%s describes no resource of the API (run make openapi)", f)
		}
	}
	if len(files) < 20 {
		t.Errorf("only %d schemas in %s", len(files), schemasDir)
	}

	// The agent guide says how to read, how to propose and what is never allowed (FR-INT-13).
	g, err := os.ReadFile(agentGuide)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"## How to read", "## How to propose", "## What is never allowed"} {
		if !bytes.Contains(g, []byte(part)) {
			t.Errorf("the agent guide has no %q", part)
		}
	}
}
