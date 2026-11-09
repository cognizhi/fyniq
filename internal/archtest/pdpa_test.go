package archtest

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// minimisedWords are the words that name an NRIC or FIN, or a payment card's number or security code (CMP-PD-07,
// SEC-14): Fyniq holds none of them. Nemura holds NRIC for payroll; card payments go through a provider's hosted pages.
var minimisedWords = []string{"nric", "fin", "cvv", "cvv2", "cvc", "cvc2", "pan"}

// minimisedPairs are two-word names for the same.
var minimisedPairs = [][2]string{{"card", "number"}, {"card", "no"}, {"card", "num"}, {"security", "code"}, {"id", "number"}}

// namesMinimised reports whether a snake_case name is named for data Fyniq never holds.
func namesMinimised(name string) bool {
	w := strings.Split(strings.ToLower(name), "_")
	for _, x := range w {
		for _, m := range minimisedWords {
			if x == m {
				return true
			}
		}
	}
	for i := 0; i+1 < len(w); i++ {
		for _, p := range minimisedPairs {
			if w[i] == p[0] && w[i+1] == p[1] {
				return true
			}
		}
	}
	return false
}

// Fyniq holds no NRIC, FIN, card number or card security code (CMP-PD-07, SEC-14; p1-pdpa-retention D11): no database
// column, no field of the API's schemas and no audit event's field is named for one, except a true-or-false flag about
// one, such as evidence.nric_flagged (a file that shows an NRIC). It sits beside FY285 (a holder record refuses an NRIC
// in full), the evidence scan (FR-DOC-09) and CTL-DP-04, and the inventory says both are not held.
// req: CMP-PD-07
// req: SEC-14
// asvs: V14.2.6
func TestNoNRICOrCardDataIsHeld(t *testing.T) {
	root := repoRoot(t)
	if namesMinimised("financial_year") || namesMinimised("finance") || !namesMinimised("payee_nric") || !namesMinimised("card_number") || !namesMinimised("cvv") {
		t.Fatal("the name rule is wrong")
	}
	var found []string

	// Columns: the schema snapshot's, by table, column and type.
	b, err := os.ReadFile(filepath.Join(root, "internal/adapters/postgres/testdata/schema.golden"))
	if err != nil {
		t.Fatal(err)
	}
	in := false
	for _, l := range strings.Split(string(b), "\n") {
		switch {
		case strings.HasPrefix(l, "## columns"):
			in = true
		case strings.HasPrefix(l, "## "):
			in = false
		case in:
			if f := strings.Split(l, " | "); len(f) > 2 && namesMinimised(f[1]) && f[2] != "boolean" {
				found = append(found, "column "+f[0]+"."+f[1]+" ("+f[2]+")")
			}
		}
	}

	// The API's fields: every property of every schema.
	var walk func(file string, o any)
	walk = func(file string, o any) {
		switch x := o.(type) {
		case map[string]any:
			if props, ok := x["properties"].(map[string]any); ok {
				for name, p := range props {
					if typ, _ := p.(map[string]any)["type"].(string); namesMinimised(name) && typ != "boolean" {
						found = append(found, "API field "+file+": "+name)
					}
				}
			}
			for _, v := range x {
				walk(file, v)
			}
		case []any:
			for _, v := range x {
				walk(file, v)
			}
		}
	}
	schemas, err := filepath.Glob(filepath.Join(root, "contracts/schemas/*.schema.json"))
	if err != nil || len(schemas) < 100 {
		t.Fatalf("the API's schemas: %d %v", len(schemas), err)
	}
	for _, p := range schemas {
		var o any
		raw, err := os.ReadFile(p)
		if err == nil {
			err = json.Unmarshal(raw, &o)
		}
		if err != nil {
			t.Fatal(err)
		}
		walk(filepath.Base(p), o)
	}

	// The audit events' fields: the change sets the code writes, "field": {before, after}.
	key := regexp.MustCompile(`"([a-z0-9_]+)":\s*\{`)
	if err := filepath.WalkDir(filepath.Join(root, "internal"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || strings.Contains(p, "/sqlcdb/") {
			return err
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, m := range key.FindAllStringSubmatch(string(src), -1) {
			if namesMinimised(m[1]) {
				rel, _ := filepath.Rel(root, p)
				found = append(found, "event field "+rel+": "+m[1])
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(found) > 0 {
		t.Errorf("Fyniq holds no NRIC, FIN, card number or security code (CMP-PD-07, SEC-14); a true-or-false flag about one is fine:\n%s",
			strings.Join(found, "\n"))
	}
}
