package datacat

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

const goldenPath = "../../adapters/postgres/nemura/testdata/schema.golden"

// schemaColumns reads every "table.column" of Nemura's schema from its golden file.
func schemaColumns(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	var cols []string
	in := false
	for _, l := range strings.Split(string(b), "\n") {
		switch {
		case strings.HasPrefix(l, "## columns"):
			in = true
		case strings.HasPrefix(l, "## "):
			in = false
		case in:
			if f := strings.Split(l, " | "); len(f) > 2 {
				cols = append(cols, f[0]+"."+f[1])
			}
		}
	}
	if len(cols) < 20 {
		t.Fatalf("read %d columns from the schema's golden file", len(cols))
	}
	return cols
}

// looksPersonal is the naming rule: a column named for a person's name, contact, identity, pay, health, a free-text reason or
// outcome, a country, or a payload or body that might carry them.
var looksPersonal = regexp.MustCompile(`(^|_)(name|email|phone|address|nric|fin|dob|birth|bank|salary|wage|pay|medical|note|notes|reason|outcome|exception|country|body|payload|error|action)($|_)`)

// Every column whose name looks personal is in a category or marked not personal with a reason, so a record type cannot land
// without its inventory entry; every column a category or the not-personal list names exists, so nothing there goes stale; and a
// column is in one place only (NM-SEC-03).
// req: NM-SEC-03
func TestTheInventoryIsComplete(t *testing.T) {
	cols := schemaColumns(t)
	for _, col := range cols {
		name := col[strings.Index(col, ".")+1:]
		if !looksPersonal.MatchString(name) {
			continue
		}
		_, inCat := Of(col)
		why, notPersonal := NotPersonal[col]
		switch {
		case !inCat && !notPersonal:
			t.Errorf("%s looks personal and is in no category and not marked not personal: add it to a category in internal/nemura/datacat and to compliance/nemura-data-inventory.yaml, or to NotPersonal with a reason", col)
		case inCat && notPersonal:
			t.Errorf("%s is in a category and marked not personal", col)
		case notPersonal && len(strings.TrimSpace(why)) < 15:
			t.Errorf("%s is not personal, with no reason a reviewer can read: %q", col, why)
		}
	}
	for _, c := range Categories {
		for _, col := range c.Columns {
			if !slices.Contains(cols, col) {
				t.Errorf("category %s names %s, which is not in the schema", c.Key, col)
			}
		}
		if c.What == "" || c.Purpose == "" || c.Retention.Reason == "" || len(c.Columns) == 0 {
			t.Errorf("category %s is missing its description, purpose, retention reason or columns", c.Key)
		}
	}
	for col := range NotPersonal {
		if !slices.Contains(cols, col) {
			t.Errorf("NotPersonal names %s, which is not in the schema", col)
		}
	}
}

// compliance/nemura-data-inventory.yaml and the code's registry are the same: the same classes, with the same columns.
// req: NM-SEC-03
func TestTheInventoryFileIsTheRegistry(t *testing.T) {
	b, err := os.ReadFile("../../../compliance/nemura-data-inventory.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Schema  string `yaml:"schema"`
		Classes []struct {
			Key       string   `yaml:"key"`
			Why       string   `yaml:"why"`
			Retention string   `yaml:"retention"`
			Columns   []string `yaml:"columns"`
		} `yaml:"classes"`
	}
	if err := yaml.Unmarshal(b, &file); err != nil {
		t.Fatal(err)
	}
	if file.Schema != "nemura-data-inventory@1" || len(file.Classes) != len(Categories) {
		t.Fatalf("the inventory file has %d classes (%s), the registry %d", len(file.Classes), file.Schema, len(Categories))
	}
	for i, c := range file.Classes {
		r := Categories[i]
		if c.Key != r.Key || !slices.Equal(c.Columns, r.Columns) || c.Why != r.Purpose {
			t.Errorf("class %d: the file says %s %v, the registry %s %v", i, c.Key, c.Columns, r.Key, r.Columns)
		}
		if strings.TrimSpace(c.Retention) == "" {
			t.Errorf("class %s has no retention", c.Key)
		}
	}
}
