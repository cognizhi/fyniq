package main

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"go.yaml.in/yaml/v3"
)

// The typed models of the Go and Python clients (PRD-001 FR-INT-17, rev 0.611): a Go struct and a Python TypedDict for each schema of the contract a client's operations send or
// receive, with the schemas they name. Money stays a decimal string, an integer an int64 and a number a json.Number (never a float), an optional field a pointer (Go) or a
// key that may be absent (Python), and what the contract leaves open (a one-of, an open object) stays raw JSON. The methods keep taking and returning JSON: a model is what a
// body is built from (any value that marshals to the contract's JSON) and what Response.Decode reads into.

// schemaSet is the contract's components/schemas, and the names a set of operations reaches.
type schemaSet struct {
	all   map[string]map[string]any
	names []string // the reached schemas, sorted
}

// readSchemas reads components.schemas and the schemas the operations' request bodies and successful answers name, with everything they name in turn.
func readSchemas(raw []byte, ops map[string]bool) (*schemaSet, error) {
	var doc struct {
		Paths      map[string]map[string]any `yaml:"paths"`
		Components struct {
			Schemas map[string]map[string]any `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	set := &schemaSet{all: doc.Components.Schemas}
	reached := map[string]bool{}
	var visit func(n any)
	visit = func(n any) {
		switch v := n.(type) {
		case map[string]any:
			if ref, ok := v["$ref"].(string); ok {
				if name, ok := strings.CutPrefix(ref, "#/components/schemas/"); ok && !reached[name] {
					reached[name] = true
					visit(set.all[name])
				}
			}
			for _, c := range v {
				visit(c)
			}
		case []any:
			for _, c := range v {
				visit(c)
			}
		}
	}
	for _, methods := range doc.Paths {
		for _, m := range methods {
			op, ok := m.(map[string]any)
			if !ok {
				continue
			}
			id, _ := op["operationId"].(string)
			if !ops[id] {
				continue
			}
			if body, ok := op["requestBody"].(map[string]any); ok {
				visit(body["content"])
			}
			if res, ok := op["responses"].(map[string]any); ok {
				for code, r := range res {
					if strings.HasPrefix(code, "2") {
						visit(r)
					}
				}
			}
		}
	}
	for n := range reached {
		if validName(n) && set.all[n] != nil {
			set.names = append(set.names, n)
		}
	}
	slices.Sort(set.names)
	return set, nil
}

// reservedGo are the names of the hand-written client: a schema of the same name is the model of it, with Model after.
var reservedGo = map[string]bool{"Problem": true, "FieldProblem": true, "Operation": true, "Operations": true, "Client": true, "Response": true, "CallOption": true, "Paginate": true, "NewKey": true}

func goName(n string) string {
	if reservedGo[n] {
		return n + "Model"
	}
	return n
}

func validName(n string) bool {
	for i, r := range n {
		if !(unicode.IsLetter(r) || (i > 0 && unicode.IsDigit(r))) {
			return false
		}
	}
	return n != ""
}

// typeOf is a schema's type names: its kind, whether null is allowed, and its $ref's name.
func typeOf(s map[string]any) (kind string, nullable bool) {
	switch t := s["type"].(type) {
	case string:
		kind = t
	case []any:
		for _, x := range t {
			if xs, _ := x.(string); xs == "null" {
				nullable = true
			} else if kind == "" {
				kind, _ = x.(string)
			}
		}
	}
	if n, _ := s["nullable"].(bool); n {
		nullable = true
	}
	return kind, nullable
}

func refName(s map[string]any) (string, bool) {
	ref, _ := s["$ref"].(string)
	name, ok := strings.CutPrefix(ref, "#/components/schemas/")
	return name, ok
}

var initialisms = map[string]string{"id": "ID", "url": "URL", "uri": "URI", "ip": "IP", "api": "API", "uuid": "UUID", "json": "JSON", "sha256": "SHA256", "html": "HTML", "pdf": "PDF", "csv": "CSV"}

// goField is a JSON property's Go field name.
func goField(prop string) string {
	var b strings.Builder
	for _, w := range strings.FieldsFunc(prop, func(r rune) bool { return !(unicode.IsLetter(r) || unicode.IsDigit(r)) }) {
		if i, ok := initialisms[strings.ToLower(w)]; ok {
			b.WriteString(i)
			continue
		}
		b.WriteString(strings.ToUpper(w[:1]) + w[1:])
	}
	out := b.String()
	if out == "" || unicode.IsDigit(rune(out[0])) {
		out = "F" + out
	}
	return out
}

// goType is the Go type of a schema; optional says a missing key is allowed, so a scalar or a struct is a pointer.
func (set *schemaSet) goType(s map[string]any, optional bool) string {
	if s == nil {
		return "json.RawMessage"
	}
	ptr := func(t string) string {
		if optional || func() bool { _, n := typeOf(s); return n }() {
			return "*" + t
		}
		return t
	}
	if name, ok := refName(s); ok {
		if !validName(name) || set.all[name] == nil {
			return "json.RawMessage"
		}
		if k, _ := typeOf(set.all[name]); k == "object" || k == "" && set.all[name]["properties"] != nil {
			return ptr(goName(name))
		}
		return goName(name)
	}
	for _, k := range []string{"oneOf", "anyOf", "allOf"} {
		if s[k] != nil {
			return "json.RawMessage"
		}
	}
	kind, _ := typeOf(s)
	switch kind {
	case "string":
		return ptr("string")
	case "integer":
		return ptr("int64")
	case "number":
		return ptr("json.Number")
	case "boolean":
		return ptr("bool")
	case "array":
		items, _ := s["items"].(map[string]any)
		return "[]" + set.goType(items, false)
	case "object":
		props, _ := s["properties"].(map[string]any)
		if len(props) == 0 {
			if ap, ok := s["additionalProperties"].(map[string]any); ok {
				return "map[string]" + set.goType(ap, false)
			}
			return "map[string]json.RawMessage"
		}
		return ptr(set.goStruct(s))
	}
	return "json.RawMessage"
}

// goStruct is an object schema as a struct type literal: its fields in the order of their names, a required one without omitempty.
func (set *schemaSet) goStruct(s map[string]any) string {
	props, _ := s["properties"].(map[string]any)
	required := map[string]bool{}
	if rs, ok := s["required"].([]any); ok {
		for _, r := range rs {
			if rn, ok := r.(string); ok {
				required[rn] = true
			}
		}
	}
	names := make([]string, 0, len(props))
	for n := range props {
		names = append(names, n)
	}
	slices.Sort(names)
	var b strings.Builder
	b.WriteString("struct {\n")
	used := map[string]bool{}
	for _, n := range names {
		field := goField(n)
		for used[field] {
			field += "_"
		}
		used[field] = true
		ps, _ := props[n].(map[string]any)
		tag := n
		if !required[n] {
			tag += ",omitempty"
		}
		fmt.Fprintf(&b, "\t%s %s `json:%q`\n", field, set.goType(ps, !required[n]), tag)
	}
	b.WriteString("}")
	return b.String()
}

// goModels is the Go file of the models.
func (set *schemaSet) goModels() []byte {
	var b bytes.Buffer
	b.WriteString("// Code generated by cmd/sdkgen from contracts/openapi/fyniq.v1.yaml; DO NOT EDIT.\n\npackage fyniq\n\nimport \"encoding/json\"\n\n")
	b.WriteString("// The models of the contract (PRD-001 FR-INT-17): a body is built from them and Response.Decode reads into them. Money stays a decimal string, a number a json.Number.\n\nvar _ json.RawMessage\n")
	for _, n := range set.names {
		s := set.all[n]
		if d, _ := s["description"].(string); d != "" {
			fmt.Fprintf(&b, "\n// %s: %s\n", goName(n), strings.Join(strings.Fields(d), " "))
		} else {
			fmt.Fprintf(&b, "\n// %s is a schema of the contract.\n", goName(n))
		}
		kind, _ := typeOf(s)
		if kind == "object" || (kind == "" && s["properties"] != nil) {
			if props, _ := s["properties"].(map[string]any); len(props) > 0 {
				fmt.Fprintf(&b, "type %s %s\n", goName(n), set.goStruct(s))
				continue
			}
		}
		fmt.Fprintf(&b, "type %s %s\n", goName(n), strings.TrimPrefix(set.goType(s, false), "*"))
	}
	return b.Bytes()
}

// pyType is the Python annotation of a schema, as a string, so models may name each other in any order.
func (set *schemaSet) pyType(s map[string]any) string {
	if s == nil {
		return "Any"
	}
	_, nullable := typeOf(s)
	t := "Any"
	if name, ok := refName(s); ok {
		if validName(name) && set.all[name] != nil {
			t = name
			if k, _ := typeOf(set.all[name]); k != "object" && set.all[name]["properties"] == nil {
				t = set.pyType(set.all[name])
			}
		}
	} else if s["oneOf"] == nil && s["anyOf"] == nil && s["allOf"] == nil {
		kind, _ := typeOf(s)
		switch kind {
		case "string":
			t = "str"
		case "integer":
			t = "int"
		case "number":
			t = "int | float"
		case "boolean":
			t = "bool"
		case "array":
			items, _ := s["items"].(map[string]any)
			t = "list[" + set.pyType(items) + "]"
		case "object":
			if ap, ok := s["additionalProperties"].(map[string]any); ok && s["properties"] == nil {
				t = "dict[str, " + set.pyType(ap) + "]"
			} else if s["properties"] == nil {
				t = "dict[str, Any]"
			} else {
				t = "dict[str, Any]" // an inline object: its fields are in the contract
			}
		}
	}
	if nullable && t != "Any" {
		t += " | None"
	}
	return t
}

// pyModels is the Python module of the models: a TypedDict for each object schema, its required keys apart from the optional ones.
func (set *schemaSet) pyModels() []byte {
	var b bytes.Buffer
	b.WriteString("# Code generated by cmd/sdkgen from contracts/openapi/fyniq.v1.yaml; DO NOT EDIT.\n#\n")
	b.WriteString("# The models of the contract (PRD-001 FR-INT-17): the shape of a body to send and of an answer's JSON. Money stays a decimal string.\n\n")
	b.WriteString("from __future__ import annotations\n\nfrom typing import Any, TypedDict\n")
	for _, n := range set.names {
		s := set.all[n]
		kind, _ := typeOf(s)
		props, _ := s["properties"].(map[string]any)
		if !(kind == "object" || kind == "") || len(props) == 0 {
			fmt.Fprintf(&b, "\n%s = %q  # %s\n", n, set.pyType(s), "an alias of the contract's schema")
			continue
		}
		required := map[string]bool{}
		if rs, ok := s["required"].([]any); ok {
			for _, r := range rs {
				if rn, ok := r.(string); ok {
					required[rn] = true
				}
			}
		}
		names := make([]string, 0, len(props))
		for p := range props {
			names = append(names, p)
		}
		slices.Sort(names)
		var req, opt []string
		for _, p := range names {
			ps, _ := props[p].(map[string]any)
			entry := fmt.Sprintf("%q: %q", p, set.pyType(ps))
			if required[p] {
				req = append(req, entry)
			} else {
				opt = append(opt, entry)
			}
		}
		fmt.Fprintf(&b, "\n_%sRequired = TypedDict(%q, {%s})\n", n, "_"+n+"Required", strings.Join(req, ", "))
		fmt.Fprintf(&b, "_%sOptional = TypedDict(%q, {%s}, total=False)\n", n, "_"+n+"Optional", strings.Join(opt, ", "))
		fmt.Fprintf(&b, "\n\nclass %s(_%sRequired, _%sOptional):\n    \"\"\"%s\"\"\"\n", n, n, n, pyDoc(s, n))
	}
	return b.Bytes()
}

func pyDoc(s map[string]any, n string) string {
	d, _ := s["description"].(string)
	d = strings.Join(strings.Fields(strings.ReplaceAll(d, `"`, "'")), " ")
	if d == "" {
		return n + " is a schema of the contract."
	}
	return strings.ReplaceAll(d, `\`, "/")
}
