package archtest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// QA-MCP-01's table names, for every red-team case, the tests that prove it, and every named test exists: a row cannot keep
// naming a test that was renamed or removed, and a case cannot be listed with no test (PRD-001 §16.2, p1-mcp-first-party task 10.1).
// req: QA-MCP-01
// asvs: V13.1.1
func TestEveryQAMCP01RowNamesTestsThatExist(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "compliance/qa-mcp-01.json"))
	if err != nil {
		t.Fatal(err)
	}
	var suite struct {
		ID   string `json:"id"`
		Rows []struct {
			ID           string   `json:"id"`
			Threat       string   `json:"threat"`
			Requirements string   `json:"requirements"`
			Control      string   `json:"control"`
			Tests        []string `json:"tests"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(raw, &suite); err != nil || suite.ID != "QA-MCP-01" || len(suite.Rows) < 15 {
		t.Fatalf("the suite's table: %v %d rows", err, len(suite.Rows))
	}
	have := map[string]bool{}
	decl := regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(t \*testing\.T\)`)
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == ".git" || d.Name() == ".data") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err == nil {
			for _, m := range decl.FindAllStringSubmatch(string(src), -1) {
				have[m[1]] = true
			}
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, r := range suite.Rows {
		if r.ID == "" || r.Threat == "" || r.Requirements == "" || r.Control == "" || len(r.Tests) == 0 || seen[r.ID] {
			t.Errorf("the row %q needs its own ID, a threat, requirements, a control and a test", r.ID)
		}
		seen[r.ID] = true
		for _, name := range r.Tests {
			if !have[name] {
				t.Errorf("row %s names %s, which no test file declares", r.ID, name)
			}
		}
	}
}
