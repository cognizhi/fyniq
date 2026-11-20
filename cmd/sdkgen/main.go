// Command sdkgen writes the operation tables and per-operation methods of the Go and Python SDKs from contracts/openapi/fyniq.v1.yaml
// (PRD-001 FR-INT-17; p2-api-third-parties task 4). The clients are for integrators, so they carry only the operations a client's scopes can reach
// (authz.ScopeAllows with the scopes of authz.ClientScopes, and the organisation's own record): an operation no scope reaches is a person's, in the web
// app, and has no method here. The conventions (idempotency key kept across retries, Retry-After, problem details, ETag, preview, cursor paging) are
// the hand-written cores in sdk/go and sdk/python, the way sdk/typescript's are; this file generates what depends on the contract.
//
//	go run ./cmd/sdkgen         # rewrite sdk/go/fyniq/operations_gen.go and sdk/python/fyniq/operations_gen.py
//	go run ./cmd/sdkgen -check  # fail while either is out of date (make api-check)
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"
	"unicode"

	"go.yaml.in/yaml/v3"

	"github.com/cognizhi/fyniq/internal/authz"
)

type operation struct {
	ID, Method, Path, Summary   string
	PathParams                  []string
	QueryParams                 []string
	Body                        bool
	Scopes                      []string
	Paged, Preview, Idem, Match bool
}

func main() {
	check := flag.Bool("check", false, "fail while the generated files differ from the contract")
	contract := flag.String("contract", "contracts/openapi/fyniq.v1.yaml", "the OpenAPI document")
	flag.Parse()
	ops, err := read(*contract)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sdkgen:", err)
		os.Exit(1)
	}
	files := map[string][]byte{
		"sdk/go/fyniq/operations_gen.go":     goFile(ops),
		"sdk/python/fyniq/operations_gen.py": pyFile(ops),
	}
	stale := false
	for _, path := range slices.Sorted(mapKeys(files)) {
		if *check {
			if cur, _ := os.ReadFile(path); !bytes.Equal(cur, files[path]) {
				fmt.Fprintf(os.Stderr, "sdkgen: %s is stale: run go run ./cmd/sdkgen\n", path)
				stale = true
			}
			continue
		}
		if err := os.WriteFile(path, files[path], 0o644); err != nil { // #nosec G306 -- generated source
			fmt.Fprintln(os.Stderr, "sdkgen:", err)
			os.Exit(1)
		}
	}
	if stale {
		os.Exit(1)
	}
	if !*check {
		fmt.Printf("✔ %d operations in sdk/go and sdk/python\n", len(ops))
	}
}

func mapKeys[K comparable, V any](m map[K]V) func(func(K) bool) {
	return func(yield func(K) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

func read(path string) ([]operation, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Paths map[string]map[string]yaml.Node `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	var out []operation
	for p, methods := range doc.Paths {
		for m, node := range methods {
			if !slices.Contains([]string{"get", "post", "put", "patch", "delete"}, m) {
				continue
			}
			var o struct {
				ID         string `yaml:"operationId"`
				Summary    string `yaml:"summary"`
				Parameters []struct {
					Name string `yaml:"name"`
					In   string `yaml:"in"`
				} `yaml:"parameters"`
				Body yaml.Node `yaml:"requestBody"`
			}
			if err := node.Decode(&o); err != nil {
				return nil, fmt.Errorf("%s %s: %w", m, p, err)
			}
			pol, ok := authz.Policies[o.ID]
			if !ok || pol.Public || pol.NoOrganisation || pol.SingleFactor {
				continue
			}
			op := operation{ID: o.ID, Method: strings.ToUpper(m), Path: p, Summary: strings.TrimSpace(o.Summary), Body: o.Body.Kind != 0}
			for _, s := range authz.ClientScopes() {
				if authz.ScopeAllows([]string{s}, pol) {
					op.Scopes = append(op.Scopes, s)
				}
			}
			if len(op.Scopes) == 0 {
				continue
			}
			for _, prm := range o.Parameters {
				switch {
				case prm.In == "path":
					op.PathParams = append(op.PathParams, prm.Name)
				case prm.In == "query":
					op.QueryParams = append(op.QueryParams, prm.Name)
					op.Paged = op.Paged || prm.Name == "cursor"
				case prm.In == "header" && strings.EqualFold(prm.Name, "Prefer"):
					op.Preview = true
				case prm.In == "header" && strings.EqualFold(prm.Name, "Idempotency-Key"):
					op.Idem = true
				case prm.In == "header" && strings.EqualFold(prm.Name, "If-Match"):
					op.Match = true
				}
			}
			// The path's own order of its parameters, which a method takes as its arguments.
			var ordered []string
			for _, seg := range strings.Split(p, "/") {
				if n, ok := strings.CutPrefix(strings.TrimSuffix(seg, "}"), "{"); ok && strings.HasSuffix(seg, "}") {
					ordered = append(ordered, n)
				}
			}
			op.PathParams = ordered
			slices.Sort(op.QueryParams)
			out = append(out, op)
		}
	}
	slices.SortFunc(out, func(a, b operation) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}

func upper(id string) string { return strings.ToUpper(id[:1]) + id[1:] }

func snake(id string) string {
	var b strings.Builder
	for i, r := range id {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}

func ident(name string) string { return strings.ReplaceAll(name, "-", "_") }

func goFile(ops []operation) []byte {
	var b bytes.Buffer
	b.WriteString("// Code generated by cmd/sdkgen from contracts/openapi/fyniq.v1.yaml; DO NOT EDIT.\n\npackage fyniq\n\nimport \"context\"\n\n")
	b.WriteString("// Operations are the operations a client's scopes can reach, by operation ID (PRD-001 FR-INT-17).\nvar Operations = map[string]Operation{\n")
	for _, o := range ops {
		fmt.Fprintf(&b, "\t%q: {ID: %q, Method: %q, Path: %q, Summary: %q, PathParams: %s, Query: %s, Body: %v, Scopes: %s, Paged: %v, Preview: %v, IdempotencyKey: %v, IfMatch: %v},\n",
			o.ID, o.ID, o.Method, o.Path, o.Summary, goList(o.PathParams), goList(o.QueryParams), o.Body, goList(o.Scopes), o.Paged, o.Preview, o.Idem, o.Match)
	}
	b.WriteString("}\n")
	for _, o := range ops {
		fmt.Fprintf(&b, "\n// %s: %s (%s %s; scopes %s).\n", upper(o.ID), strings.TrimSuffix(o.Summary, "."), o.Method, o.Path, strings.Join(o.Scopes, ", "))
		args := "ctx context.Context"
		for _, p := range o.PathParams {
			args += ", " + goParam(p) + " string"
		}
		call := fmt.Sprintf("c.Do(ctx, Operations[%q], []string{%s}, ", o.ID, goArgs(o.PathParams))
		if o.Body {
			args += ", body any"
			call += "body, opts"
		} else {
			call += "nil, opts"
		}
		fmt.Fprintf(&b, "func (c *Client) %s(%s, opts ...CallOption) (*Response, error) {\n\treturn %s)\n}\n", upper(o.ID), args, call)
	}
	return b.Bytes()
}

func goList(xs []string) string {
	if len(xs) == 0 {
		return "nil"
	}
	q := make([]string, len(xs))
	for i, x := range xs {
		q[i] = fmt.Sprintf("%q", x)
	}
	return "[]string{" + strings.Join(q, ", ") + "}"
}

// goParam is a path parameter as a Go argument: journal_id is journalID, personal_access_token_id personalAccessTokenID.
func goParam(p string) string {
	parts := strings.Split(strings.ToLower(p), "_")
	out := parts[0]
	for _, w := range parts[1:] {
		if w == "id" {
			out += "ID"
		} else if w != "" {
			out += strings.ToUpper(w[:1]) + w[1:]
		}
	}
	if !strings.HasSuffix(out, "ID") {
		out += "Value"
	}
	return out
}

func goArgs(ps []string) string {
	q := make([]string, len(ps))
	for i, p := range ps {
		q[i] = goParam(p)
	}
	return strings.Join(q, ", ")
}

func pyFile(ops []operation) []byte {
	var b bytes.Buffer
	b.WriteString("# Code generated by cmd/sdkgen from contracts/openapi/fyniq.v1.yaml; DO NOT EDIT.\n\"\"\"The operations a client's scopes can reach, by operation ID, and a method for each (PRD-001 FR-INT-17).\"\"\"\n\n")
	b.WriteString("OPERATIONS = {\n")
	for _, o := range ops {
		fmt.Fprintf(&b, "    %q: {\"method\": %q, \"path\": %q, \"summary\": %q, \"path_params\": %s, \"query\": %s, \"body\": %s, \"scopes\": %s, \"paged\": %s, \"preview\": %s, \"idempotency_key\": %s, \"if_match\": %s},\n",
			o.ID, o.Method, o.Path, o.Summary, pyList(o.PathParams), pyList(o.QueryParams), pyBool(o.Body), pyList(o.Scopes), pyBool(o.Paged), pyBool(o.Preview), pyBool(o.Idem), pyBool(o.Match))
	}
	b.WriteString("}\n\n\nclass OperationsMixin:\n    \"\"\"A method for each operation: path parameters first, then the body, then keyword options (query, if_match, preview, idempotency_key).\"\"\"\n")
	for _, o := range ops {
		args := "self"
		for _, p := range o.PathParams {
			args += ", " + ident(p)
		}
		pass := "[" + strings.Join(mapStr(o.PathParams, ident), ", ") + "]"
		if o.Body {
			args += ", body=None"
		}
		fmt.Fprintf(&b, "\n    def %s(%s, **options):\n        \"\"\"%s (%s %s; scopes %s).\"\"\"\n", snake(o.ID), args, strings.ReplaceAll(strings.TrimSuffix(o.Summary, "."), "\"", "'"), o.Method, o.Path, strings.Join(o.Scopes, ", "))
		body := "None"
		if o.Body {
			body = "body"
		}
		fmt.Fprintf(&b, "        return self.call(%q, %s, %s, **options)\n", o.ID, pass, body)
	}
	return b.Bytes()
}

func mapStr(xs []string, f func(string) string) []string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = f(x)
	}
	return out
}

func pyBool(v bool) string {
	if v {
		return "True"
	}
	return "False"
}

func pyList(xs []string) string {
	q := make([]string, len(xs))
	for i, x := range xs {
		q[i] = fmt.Sprintf("%q", x)
	}
	return "[" + strings.Join(q, ", ") + "]"
}
