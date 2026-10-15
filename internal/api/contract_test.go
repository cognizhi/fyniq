package api

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"maps"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
)

var update = flag.Bool("update", false, "rewrite contracts/openapi/fyniq.v1.yaml from the code (make openapi)")

const contractPath = "../../contracts/openapi/fyniq.v1.yaml"

const contractHeader = `# The contract of Fyniq's public API (PRD-001 §8.18, ADR-T1): OpenAPI 3.1, as the code serves it at
# /v1/openapi.json. Reviewed in every pull request that changes it: CI fails when the code and this file
# differ. Regenerate it with ` + "`make openapi`" + ` and review the diff; never edit it by hand.
# info.version follows the platform version (release-please updates it).
`

// versionLine is info.version: the only line at that indentation named version.
var versionLine = regexp.MustCompile(`(?m)^  version: .*$`)

func testAPI(t *testing.T) *API {
	t.Helper()
	return New(http.NewServeMux(), Deps{Auth: authn.NotYet{}, Version: "0.0.0"})
}

func document(t *testing.T) []byte {
	t.Helper()
	b, err := testAPI(t).OpenAPI().YAML()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The reviewed contract and the code agree: the document huma generates from the registered
// operations is the file in contracts/, apart from the platform version.
// req: NFR-API-01
// req: QA-API-01
func TestTheContractIsTheCode(t *testing.T) {
	got := document(t)
	if *update {
		version, err := os.ReadFile("../../VERSION")
		if err != nil {
			t.Fatal(err)
		}
		out := versionLine.ReplaceAll(got, []byte("  version: "+strings.TrimSpace(string(version))+" # x-release-please-version"))
		if err := os.WriteFile(contractPath, append([]byte(contractHeader), out...), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatalf("%v (run make openapi)", err)
	}
	normal := func(b []byte) []string {
		b = versionLine.ReplaceAll(bytes.TrimPrefix(b, []byte(contractHeader)), []byte("  version: (platform)"))
		return strings.Split(string(b), "\n")
	}
	g, w := normal(got), normal(want)
	for i := 0; i < max(len(g), len(w)); i++ {
		gl, wl := "", ""
		if i < len(g) {
			gl = g[i]
		}
		if i < len(w) {
			wl = w[i]
		}
		if gl != wl {
			t.Fatalf("the API's code and contracts/openapi/fyniq.v1.yaml differ from line %d:\n  code:     %q\n  contract: %q\n"+
				"Run `make openapi` and review the change to the contract (a breaking change inside /v1 is refused: FR-INT-06).",
				i+1+strings.Count(contractHeader, "\n"), gl, wl)
		}
	}
}

// lintOperation returns what an operation lacks under NFR-API-01: a description, an example request
// and response, its permission and its error codes.
func lintOperation(op *huma.Operation, schemas map[string]*huma.Schema) []string {
	var out []string
	miss := func(format string, args ...any) { out = append(out, op.OperationID+": "+fmt.Sprintf(format, args...)) }
	if op.OperationID == "" {
		miss("no operationId")
	}
	if op.Summary == "" || len(op.Description) < 40 {
		miss("no summary, or a description under 40 characters")
	}
	if len(op.Tags) == 0 {
		miss("no tag")
	}
	perm, _ := op.Extensions["x-fyniq-permission"].(map[string]any)
	if perm["capability"] == nil || perm["right"] == nil {
		miss("no x-fyniq-permission with a capability and a right")
	}
	codes, _ := op.Extensions["x-fyniq-error-codes"].([]string)
	if len(codes) == 0 {
		miss("no x-fyniq-error-codes")
	}
	for _, c := range codes {
		f, ok := codeFacts[c]
		resp := op.Responses[fmt.Sprint(f.Status)]
		if !ok || resp == nil || resp.Content["application/problem+json"] == nil || !strings.Contains(resp.Description, c) {
			miss("error code %s has no documented %d problem response naming it", c, f.Status)
		}
	}
	for _, p := range op.Parameters {
		if p.Description == "" || (p.Example == nil && (p.Schema == nil || len(p.Schema.Examples) == 0)) {
			miss("parameter %s has no description or example", p.Name)
		}
	}
	if op.RequestBody != nil {
		for ct, mt := range op.RequestBody.Content {
			for _, m := range missingExamples(mt.Schema, schemas, "request "+ct, map[string]bool{}) {
				miss("%s has no example", m)
			}
		}
	}
	documented := false
	for status, resp := range op.Responses {
		if !strings.HasPrefix(status, "2") {
			continue
		}
		documented = true
		for ct, mt := range resp.Content {
			for _, m := range missingExamples(mt.Schema, schemas, "response "+status+" "+ct, map[string]bool{}) {
				miss("%s has no example", m)
			}
		}
	}
	if !documented {
		miss("no success response")
	}
	return out
}

// missingExamples returns the leaf properties of s without an example: a client or a model can build a
// complete example from the schema only when every leaf has one.
func missingExamples(s *huma.Schema, schemas map[string]*huma.Schema, at string, seen map[string]bool) []string {
	if s == nil {
		return nil
	}
	if s.Ref != "" {
		name := strings.TrimPrefix(s.Ref, "#/components/schemas/")
		if seen[name] {
			return nil
		}
		seen[name] = true
		return missingExamples(schemas[name], schemas, at, seen)
	}
	switch {
	case s.Type == "object" || len(s.Properties) > 0:
		var out []string
		for _, name := range slices.Sorted(maps.Keys(s.Properties)) {
			out = append(out, missingExamples(s.Properties[name], schemas, at+"."+name, seen)...)
		}
		return out
	case s.Type == "array":
		return missingExamples(s.Items, schemas, at+"[]", seen)
	case len(s.Examples) == 0 && s.Examples == nil:
		return []string{at}
	}
	return nil
}

func operations(a *API) []*huma.Operation {
	var out []*huma.Operation
	for _, path := range slices.Sorted(maps.Keys(a.OpenAPI().Paths)) {
		item := a.OpenAPI().Paths[path]
		for _, op := range []*huma.Operation{item.Get, item.Post, item.Put, item.Patch, item.Delete} {
			if op != nil {
				out = append(out, op)
			}
		}
	}
	return out
}

// req: NFR-API-01
func TestEveryOperationIsDocumented(t *testing.T) {
	a := testAPI(t)
	schemas := a.OpenAPI().Components.Schemas.Map()
	ops := operations(a)
	if len(ops) < 7 {
		t.Fatalf("only %d operations", len(ops))
	}
	for _, op := range ops {
		for _, m := range lintOperation(op, schemas) {
			t.Error(m)
		}
	}
	// The lint catches a planted operation that documents nothing.
	planted := &huma.Operation{OperationID: "planted", Responses: map[string]*huma.Response{
		"200": {Content: map[string]*huma.MediaType{"application/json": {Schema: &huma.Schema{Type: "object",
			Properties: map[string]*huma.Schema{"id": {Type: "string"}}}}}}}}
	if got := lintOperation(planted, schemas); len(got) != 5 {
		t.Errorf("the lint should find 5 gaps in the planted operation (description, tag, permission, codes, example), found %d: %v", len(got), got)
	}
}

// Every operation has its policy from the one table, and every policy its operation (SEC-02).
// req: SEC-02
func TestEveryOperationHasItsPolicy(t *testing.T) {
	a := testAPI(t)
	registered := a.Policies()
	for _, op := range operations(a) {
		p, ok := registered[op.OperationID]
		if !ok {
			t.Errorf("%s is in the document without a policy", op.OperationID)
			continue
		}
		if want := authz.Policies[op.OperationID]; p.Operation != want.Operation || p.Capability != want.Capability || p.Need != want.Need {
			t.Errorf("%s was registered with %+v, not the table's %+v", op.OperationID, p, want)
		}
	}
	for op := range authz.Policies {
		if _, ok := registered[op]; !ok {
			t.Errorf("the policy table names %s, which the API does not register", op)
		}
	}
	defer func() {
		if recover() == nil {
			t.Error("registering an operation under another operation's policy did not fail")
		}
	}()
	register(a, authz.GetJournal, huma.Operation{OperationID: "somethingElse", Method: http.MethodGet, Path: "/v1/else"}, nil,
		func(context.Context, *struct{}) (*struct{}, error) { return nil, nil })
}
