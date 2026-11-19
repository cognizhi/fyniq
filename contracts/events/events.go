// Package events holds the event catalogue (PRD-001 §12.10, FR-INT-12; p1-public-api task 3.4): one JSON Schema per
// event version in schemas/<type>.schema.json, and asyncapi.yaml, the AsyncAPI 3 document listing them. An event's type
// is fyniq.<object>.<event>.v<N>; a breaking change to its data is a new version, a new file, and the old file stays
// until the old type is retired. The files are embedded so that the code that emits an event, and the tests that
// check it, read what the release ships.
package events

import (
	"embed"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Schemas are the event schemas, as schemas/<type>.schema.json.
//
//go:embed schemas/*.json
var Schemas embed.FS

// AsyncAPI is the AsyncAPI 3 document that lists every event type and its data schema.
//
//go:embed asyncapi.yaml
var AsyncAPI []byte

// TypePattern is the shape of an event type: fyniq.<object>.<event>.v<N>, N from 1.
var TypePattern = regexp.MustCompile(`^fyniq\.([a-z][a-z0-9_]*)\.([a-z][a-z0-9_]*)\.v([1-9][0-9]*)$`)

// Type is the event type of an event key (<object>.<event>) at a version.
func Type(key string, version int) string { return fmt.Sprintf("fyniq.%s.v%d", key, version) }

// Parse splits an event type into its key and version; false when it is not an event type.
func Parse(eventType string) (key string, version int, ok bool) {
	m := TypePattern.FindStringSubmatch(eventType)
	if m == nil {
		return "", 0, false
	}
	version, _ = strconv.Atoi(m[3])
	return m[1] + "." + m[2], version, true
}

// Types are the event types that have a schema, sorted.
func Types() ([]string, error) {
	entries, err := Schemas.ReadDir("schemas")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		out = append(out, strings.TrimSuffix(e.Name(), ".schema.json"))
	}
	slices.Sort(out)
	return out, nil
}

// Current is the newest version of an event key that has a schema; false when the key has none.
func Current(key string) (int, bool) {
	types, err := Types()
	if err != nil {
		return 0, false
	}
	best := 0
	for _, t := range types {
		if k, v, ok := Parse(t); ok && k == key && v > best {
			best = v
		}
	}
	return best, best > 0
}

// Schema is the JSON Schema of an event type, or false when there is none.
func Schema(eventType string) ([]byte, bool) {
	b, err := Schemas.ReadFile("schemas/" + eventType + ".schema.json")
	return b, err == nil
}
