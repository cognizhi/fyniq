package connector_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/cognizhi/fyniq/internal/connector"
)

// schema is contracts/connectors/manifest.schema.json, compiled.
func schema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	raw, err := os.ReadFile("../../contracts/connectors/manifest.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("manifest.schema.json", doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile("manifest.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func manifests(t *testing.T) map[string][]byte {
	t.Helper()
	paths, err := filepath.Glob("../../connectors/*/manifest.json")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no manifests: %v", err)
	}
	out := map[string][]byte{}
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		out[filepath.Base(filepath.Dir(p))] = raw
	}
	return out
}

// Every first-party manifest validates against the published schema and passes the platform's own checks, and its id
// is its directory's name.
// req: FR-INT-25
func TestEveryManifestValidates(t *testing.T) {
	s := schema(t)
	for dir, raw := range manifests(t) {
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		if err := s.Validate(doc); err != nil {
			t.Errorf("%s does not match the manifest schema: %v", dir, err)
		}
		m, err := connector.Parse(raw)
		if err != nil {
			t.Errorf("%s: %v", dir, err)
			continue
		}
		if m.ID != dir {
			t.Errorf("connectors/%s holds the manifest of %s", dir, m.ID)
		}
		if len(m.Hash()) != 64 {
			t.Errorf("%s: hash %q", dir, m.Hash())
		}
	}
}

// valid is a manifest that passes, which each case below breaks in one way.
const valid = `{
  "id": "example", "name": "Example", "publisher": "Fyniq", "version": "1.0.0", "min_platform": "0.1.0",
  "summary": "An example.", "scope": "installation",
  "capabilities": [{"kind": "outbound-email", "trigger": "system-rule", "trigger_rule": "A message the outbox holds.",
    "on_pause": "hold", "on_disable": "hold", "in_flight": "reconcile"}],
  "configuration": {"type": "object", "additionalProperties": false, "required": ["address"],
    "properties": {"address": {"type": "string", "pattern": "^[a-z.]+:[0-9]+$"}, "note": {"type": "string"}}},
  "secrets": [{"name": "key", "title": "Key", "description": "The key.", "used_by": ["worker"], "rotation_overlap_hours": 24}],
  "provider_scopes": [], "fyniq_scopes": [], "data_classes": ["personal-data"], "regions": ["SG"],
  "destinations": [{"host": "api.example.com", "port": 443}, {"configured": "address"}],
  "webhooks": [{"path": "/api/webhooks/example", "events": ["sent"], "registered_by": "operator", "secret": "key"}],
  "health_check": "Counts failures.", "test_action": "Sends a test.", "local_substitute": "A fake.",
  "limits": {"requests_per_minute": 60, "breaker_failures": 5, "breaker_cooldown_seconds": 60}
}`

// edit returns valid with change applied to its decoded form.
func edit(t *testing.T, change func(m map[string]any)) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(valid), &m); err != nil {
		t.Fatal(err)
	}
	change(m)
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func capability(m map[string]any) map[string]any {
	return m["capabilities"].([]any)[0].(map[string]any)
}

// A malformed manifest is refused with what is wrong, so a release carrying one does not start; where the schema can
// say it too, the schema refuses the same manifest, so the published format and the platform agree.
// req: FR-INT-25
// req: FR-INT-26
// req: FR-INT-27
func TestManifestRefusals(t *testing.T) {
	s := schema(t)
	if _, err := connector.Parse([]byte(valid)); err != nil {
		t.Fatalf("the valid manifest: %v", err)
	}
	cases := []struct {
		name, want string
		schemaToo  bool
		change     func(m map[string]any)
	}{
		{"unknown field", "unknown field", true, func(m map[string]any) { m["extra"] = true }},
		{"bad id", "id \"Bad\"", true, func(m map[string]any) { m["id"] = "Bad" }},
		{"bad version", "version", true, func(m map[string]any) { m["version"] = "1.0" }},
		{"bad scope", "scope", true, func(m map[string]any) { m["scope"] = "everyone" }},
		{"no capabilities", "capabilities is empty", true, func(m map[string]any) { m["capabilities"] = []any{} }},
		{"unknown kind", "not a kind", true, func(m map[string]any) { capability(m)["kind"] = "fax" }},
		{"bad trigger", "trigger", true, func(m map[string]any) { capability(m)["trigger"] = "anyone" }},
		{"pause cancels", "on_pause must be hold", true, func(m map[string]any) { capability(m)["on_pause"] = "cancel" }},
		{"in flight dropped", "in_flight", true, func(m map[string]any) { capability(m)["in_flight"] = "drop" }},
		{"configuration not closed", "additionalProperties false", true, func(m map[string]any) {
			m["configuration"].(map[string]any)["additionalProperties"] = true
		}},
		{"a secret in the configuration", "is a secret", false, func(m map[string]any) {
			m["configuration"].(map[string]any)["properties"].(map[string]any)["key"] = map[string]any{"type": "string"}
		}},
		{"configuration does not compile", "does not compile", false, func(m map[string]any) {
			m["configuration"].(map[string]any)["properties"].(map[string]any)["note"] = map[string]any{"type": "text"}
		}},
		{"unknown data class", "data class", true, func(m map[string]any) { m["data_classes"] = []any{"everything"} }},
		{"no regions", "regions is empty", true, func(m map[string]any) { m["regions"] = []any{} }},
		{"no destinations", "destinations is empty", true, func(m map[string]any) { m["destinations"] = []any{} }},
		{"bad host", "destination host", true, func(m map[string]any) {
			m["destinations"] = []any{map[string]any{"host": "http://api.example.com"}}
		}},
		{"configured destination not a property", "not a string property", false, func(m map[string]any) {
			m["destinations"] = []any{map[string]any{"configured": "server"}}
		}},
		{"webhook signed by no secret", "not one of the connector's secrets", false, func(m map[string]any) {
			m["webhooks"].([]any)[0].(map[string]any)["secret"] = "other"
		}},
		{"secret used by nobody", "used_by", true, func(m map[string]any) {
			m["secrets"].([]any)[0].(map[string]any)["used_by"] = []any{}
		}},
		{"limits out of range", "limits", true, func(m map[string]any) {
			m["limits"].(map[string]any)["requests_per_minute"] = 0
		}},
	}
	for _, c := range cases {
		raw := edit(t, c.change)
		_, err := connector.Parse(raw)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v; want %q", c.name, err, c.want)
		}
		if c.schemaToo {
			doc, _ := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
			if s.Validate(doc) == nil {
				t.Errorf("%s: the schema accepts what the platform refuses", c.name)
			}
		}
	}
}

// A configuration is what the manifest's schema accepts, and never holds a secret.
// req: FR-INT-20
// req: FR-INT-25
func TestValidateConfiguration(t *testing.T) {
	m, err := connector.Parse([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Validate(map[string]any{"address": "smtp.example.com:587"}); err != nil {
		t.Fatalf("a valid configuration: %v", err)
	}
	for name, config := range map[string]map[string]any{
		"missing":  {},
		"unknown":  {"address": "a.b:1", "colour": "teal"},
		"pattern":  {"address": "not an address"},
		"a secret": {"address": "a.b:1", "key": "s3cret"},
	} {
		if err := m.Validate(config); !errors.Is(err, connector.ErrConfiguration) {
			t.Errorf("%s: %v; want ErrConfiguration", name, err)
		}
	}
	err = m.Validate(map[string]any{"address": "a.b:1", "key": "s3cret"})
	if strings.Contains(err.Error(), "s3cret") {
		t.Fatalf("the refusal repeats the secret: %v", err)
	}
}

// The hash is of the manifest's canonical form: layout does not change it, a declared field does.
// req: FR-INT-26
func TestManifestHashIsCanonical(t *testing.T) {
	a, err := connector.Parse([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	b, err := connector.Parse(edit(t, func(map[string]any) {}))
	if err != nil {
		t.Fatal(err)
	}
	c, err := connector.Parse(edit(t, func(m map[string]any) { m["data_classes"] = []any{"personal-data", "business-figures"} }))
	if err != nil {
		t.Fatal(err)
	}
	if a.Hash() != b.Hash() || a.Hash() == c.Hash() {
		t.Fatalf("hashes %s %s %s", a.Hash(), b.Hash(), c.Hash())
	}
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
