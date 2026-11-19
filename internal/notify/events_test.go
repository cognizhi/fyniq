package notify

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"go.yaml.in/yaml/v3"

	"github.com/cognizhi/fyniq/contracts/events"
)

var updateEvents = flag.Bool("update", false, "write a schema for each event type that has none (version 1), and rewrite contracts/events/asyncapi.yaml")

// The data of an event carries IDs and the time, never what happened in words: a notification's title may hold names and
// amounts, and a subscriber reads details through the API under its own rights (fyniq-api-integrations, SEC-15).
var eventDataFields = []string{"organisation_id", "occurred_at", "record_id", "record_type"}

func eventKeys() []string {
	keys := make([]string, len(Catalogue))
	for i, t := range Catalogue {
		keys[i] = t.Key
	}
	slices.Sort(keys)
	return keys
}

// schemaFor is the version 1 schema of a notification type's event.
func schemaFor(t Type) []byte {
	typ := events.Type(t.Key, 1)
	doc := map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"$id":                  "https://github.com/cognizhi/fyniq/contracts/events/schemas/" + typ + ".schema.json",
		"title":                typ,
		"description":          "Fires when: " + t.Label + ". The data of the event, whose type is " + typ + ": the IDs and the time only, never its title or any detail; the subscriber reads what it needs through the API, under its own rights.",
		"x-fyniq-event":        map[string]any{"type": typ, "version": 1, "category": t.Category},
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"organisation_id", "occurred_at"},
		"properties": map[string]any{
			"organisation_id": map[string]any{"type": "string", "format": "uuid", "description": "The organisation it happened in."},
			"record_type":     map[string]any{"type": []string{"string", "null"}, "maxLength": 40, "description": "What kind of record it is about, when it is about one."},
			"record_id":       map[string]any{"type": []string{"string", "null"}, "format": "uuid", "description": "That record's ID."},
			"occurred_at":     map[string]any{"type": "string", "format": "date-time", "description": "When it happened."},
		},
	}
	b, _ := json.MarshalIndent(doc, "", "  ")
	return append(b, '\n')
}

// asyncAPI is the AsyncAPI 3 document of the schemas in fsys, with each event's reason to fire from its schema.
func asyncAPI(fsys fs.FS) ([]byte, error) {
	entries, err := fs.ReadDir(fsys, "schemas")
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("# The event catalogue (PRD-001 §12.10, FR-INT-12): one channel and one message per event type, the data schema of each\n" +
		"# in schemas/<type>.schema.json. Written by `go test ./internal/notify -run TestTheEventCatalogue -update` from the\n" +
		"# schemas; edit a schema, then rewrite this file. A breaking change to an event's data is a new version, a new file.\n" +
		"asyncapi: 3.0.0\ninfo:\n  title: Fyniq events\n  version: 1.0.0\n  description: Events Fyniq emits from its transactional outbox, as CloudEvents 1.0 whose type is fyniq.<object>.<event>.v<N> and whose data is the schema named here. Delivery to webhook endpoints arrives in P2 (FR-INT-12).\n" +
		"defaultContentType: application/json\nchannels:\n")
	var msgs strings.Builder
	for _, e := range entries {
		name := e.Name()
		typ := strings.TrimSuffix(name, ".schema.json")
		raw, err := fs.ReadFile(fsys, "schemas/"+name)
		if err != nil {
			return nil, err
		}
		var s struct {
			Description string `json:"description"`
		}
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		fmt.Fprintf(&b, "  %s:\n    address: %s\n    messages:\n      %s:\n        $ref: '#/components/messages/%s'\n", typ, typ, typ, typ)
		fmt.Fprintf(&msgs, "    %s:\n      name: %s\n      title: %s\n      summary: %q\n      contentType: application/json\n      payload:\n        $ref: 'schemas/%s'\n",
			typ, typ, typ, strings.SplitN(s.Description, ". The data of", 2)[0], name)
	}
	b.WriteString("components:\n  messages:\n" + msgs.String())
	return []byte(b.String()), nil
}

// verifyEvents lists what is wrong between the event keys the code emits and the schemas in fsys: a key without a schema,
// a schema whose name is not fyniq.<object>.<event>.v<N> (no version), a schema for a key nothing emits, and a schema
// that does not say what its name says or carries more than IDs.
func verifyEvents(keys []string, fsys fs.FS) []string {
	var bad []string
	entries, err := fs.ReadDir(fsys, "schemas")
	if err != nil {
		return []string{err.Error()}
	}
	have := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		typ, ok := strings.CutSuffix(name, ".schema.json")
		key, version, shaped := events.Parse(typ)
		if !ok || !shaped {
			bad = append(bad, fmt.Sprintf("%s: a schema is named <type>.schema.json with the type fyniq.<object>.<event>.v<N>", name))
			continue
		}
		have[key] = true
		if !slices.Contains(keys, key) {
			bad = append(bad, fmt.Sprintf("%s: nothing emits the event %s", name, key))
		}
		raw, err := fs.ReadFile(fsys, "schemas/"+name)
		var s struct {
			ID         string         `json:"$id"`
			Event      map[string]any `json:"x-fyniq-event"`
			Extra      *bool          `json:"additionalProperties"`
			Properties map[string]any `json:"properties"`
		}
		if err != nil || json.Unmarshal(raw, &s) != nil {
			bad = append(bad, name+": the schema is not JSON")
			continue
		}
		if !strings.HasSuffix(s.ID, "/"+name) {
			bad = append(bad, name+": $id does not end with the file's name")
		}
		if s.Event["type"] != typ || s.Event["version"] != float64(version) {
			bad = append(bad, name+": x-fyniq-event does not name the type and its version")
		}
		if s.Extra == nil || *s.Extra {
			bad = append(bad, name+": additionalProperties must be false")
		}
		for p := range s.Properties {
			if !slices.Contains(eventDataFields, p) {
				bad = append(bad, fmt.Sprintf("%s: the data carries %q: events carry IDs and the time only", name, p))
			}
		}
	}
	for _, k := range keys {
		if !have[k] {
			bad = append(bad, fmt.Sprintf("%s: the event %s is emitted and has no schema: write schemas/%s.schema.json (go test ./internal/notify -run TestTheEventCatalogue -update)", k, k, events.Type(k, 1)))
		}
	}
	return bad
}

// Every event type the code emits, through the one event stream (Emit accepts only a catalogue type), has a schema and a
// version in contracts/events/, every schema is for an event the code emits, and the AsyncAPI document lists them all.
// req: platform/release-and-versioning Event schema versions
func TestTheEventCatalogue(t *testing.T) {
	dir := filepath.Join("..", "..", "contracts", "events")
	if *updateEvents {
		for _, ty := range Catalogue {
			p := filepath.Join(dir, "schemas", events.Type(ty.Key, 1)+".schema.json")
			if _, err := os.Stat(p); err != nil {
				if err := os.WriteFile(p, schemaFor(ty), 0o644); err != nil {
					t.Fatal(err)
				}
			}
		}
		doc, err := asyncAPI(os.DirFS(dir))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "asyncapi.yaml"), doc, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if bad := verifyEvents(eventKeys(), events.Schemas); len(bad) > 0 {
		// events.Schemas has a "schemas" directory at its root, as verifyEvents reads it.
		t.Fatalf("the event schemas and the events the code emits disagree:\n%s", strings.Join(bad, "\n"))
	}
	for _, k := range eventKeys() {
		if v, ok := events.Current(k); !ok || v < 1 {
			t.Errorf("%s has no current version", k)
		}
	}
	want, err := asyncAPI(events.Schemas)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(events.AsyncAPI, want) {
		t.Fatal("contracts/events/asyncapi.yaml is stale: go test ./internal/notify -run TestTheEventCatalogue -update")
	}
	var doc struct {
		AsyncAPI string `yaml:"asyncapi"`
		Channels map[string]struct {
			Address string `yaml:"address"`
		} `yaml:"channels"`
	}
	if err := yaml.Unmarshal(events.AsyncAPI, &doc); err != nil {
		t.Fatal(err)
	}
	types, _ := events.Types()
	if doc.AsyncAPI != "3.0.0" || len(doc.Channels) != len(types) {
		t.Fatalf("asyncapi.yaml is not an AsyncAPI 3 document of %d events: %q, %d channels", len(types), doc.AsyncAPI, len(doc.Channels))
	}
	for _, typ := range types {
		if doc.Channels[typ].Address != typ {
			t.Errorf("asyncapi.yaml has no channel for %s", typ)
		}
	}
}

// The check bites: an emitted event without a schema, a schema without a version, one for an event nothing emits, and one
// that carries a title or says another type than its name, each fail it.
// req: platform/release-and-versioning Event schema versions
func TestTheEventCheckRefusesPlantedEvents(t *testing.T) {
	good := func(key string) []byte { return schemaFor(Type{Key: key, Label: "Planted", Category: Operations}) }
	typ := events.Type("planted.thing", 1)
	ok := fstest.MapFS{"schemas/" + typ + ".schema.json": {Data: good("planted.thing")}}
	if bad := verifyEvents([]string{"planted.thing"}, ok); len(bad) != 0 {
		t.Fatalf("a good catalogue is refused: %v", bad)
	}
	withTitle := strings.Replace(string(good("planted.thing")), `"properties": {`, `"properties": {
    "title": {"type": "string"},`, 1)
	wrongType := strings.Replace(string(good("planted.thing")), `"type": "fyniq.planted.thing.v1"`, `"type": "fyniq.planted.thing.v2"`, 1)
	for name, tc := range map[string]struct {
		keys  []string
		files fstest.MapFS
		want  string
	}{
		"no schema":     {[]string{"planted.thing", "planted.missing"}, ok, "has no schema"},
		"no version":    {[]string{"planted.thing"}, fstest.MapFS{"schemas/fyniq.planted.thing.schema.json": {Data: good("planted.thing")}}, "fyniq.<object>.<event>.v<N>"},
		"nothing emits": {nil, ok, "nothing emits"},
		"a title":       {[]string{"planted.thing"}, fstest.MapFS{"schemas/" + typ + ".schema.json": {Data: []byte(withTitle)}}, "IDs and the time only"},
		"another type":  {[]string{"planted.thing"}, fstest.MapFS{"schemas/" + typ + ".schema.json": {Data: []byte(wrongType)}}, "does not name the type"},
	} {
		bad := verifyEvents(tc.keys, tc.files)
		if len(bad) == 0 || !strings.Contains(strings.Join(bad, "\n"), tc.want) {
			t.Errorf("%s: want a refusal naming %q, got %v", name, tc.want, bad)
		}
	}
}
