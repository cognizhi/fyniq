// Package authztest reads the authorisation rules as PRD-001 states them, for the tests that check
// the policy table against the PRD (QA-SEC-03): Appendix D's cells and §7.1's roles.
package authztest

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
)

// PRD returns the text of PRD-001.
func PRD(t testing.TB) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	b, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "docs", "prd", "prd-001-fyniq-platform.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// section returns the lines of the PRD from the heading that starts with start to the next heading
// of the same or a higher level.
func section(t testing.TB, prd, start string) []string {
	t.Helper()
	lines := strings.Split(prd, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, start) {
			level := strings.Index(l, " ")
			for j := i + 1; j < len(lines); j++ {
				if h := strings.Index(lines[j], " "); strings.HasPrefix(lines[j], "#") && h > 0 && h <= level &&
					strings.Trim(lines[j][:h], "#") == "" {
					return lines[i:j]
				}
			}
			return lines[i:]
		}
	}
	t.Fatalf("the PRD has no section %q", start)
	return nil
}

func cells(row string) []string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(row), "|"), "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// AppendixD returns Appendix D's column names (after "Capability") and its cells by capability and
// column, as the PRD writes them.
func AppendixD(t testing.TB) (columns []string, rows map[string]map[string]string) {
	t.Helper()
	rows = map[string]map[string]string{}
	for _, l := range section(t, PRD(t), "## Appendix D") {
		if !strings.HasPrefix(l, "|") || strings.HasPrefix(l, "|---") {
			continue
		}
		c := cells(l)
		if c[0] == "Capability" {
			columns = c[1:]
			continue
		}
		if columns == nil {
			continue
		}
		if len(c) != len(columns)+1 {
			t.Fatalf("Appendix D row %q has %d cells, the header %d", c[0], len(c)-1, len(columns))
		}
		row := map[string]string{}
		for i, col := range columns {
			row[col] = c[i+1]
		}
		rows[c[0]] = row
	}
	if len(columns) == 0 || len(rows) == 0 {
		t.Fatal("Appendix D's table was not found")
	}
	return columns, rows
}

// RoleLabels returns the roles of §7.1's "What each role sees" table, in order.
func RoleLabels(t testing.TB) []string {
	t.Helper()
	var out []string
	inTable := false
	for _, l := range section(t, PRD(t), "### 7.1 ") {
		if !strings.HasPrefix(l, "|") {
			if inTable {
				break
			}
			continue
		}
		c := cells(l)
		if c[0] == "Role" {
			inTable = true
			continue
		}
		if inTable && !strings.HasPrefix(c[0], "---") {
			out = append(out, c[0])
		}
	}
	if len(out) == 0 {
		t.Fatal("§7.1's role table was not found")
	}
	return out
}

var cellPattern = regexp.MustCompile(`^([CAPRVM](?:/[CAPRVM])*)(\s.*|\(.*)?$`)

// ParseCell reads a cell: its right letters and the words that qualify them.
func ParseCell(s string) authz.Cell {
	s = strings.TrimSpace(s)
	if s == "—" || s == "" {
		return authz.Cell{}
	}
	m := cellPattern.FindStringSubmatch(s)
	if m == nil {
		return authz.Cell{Qualifier: s}
	}
	q := strings.TrimSpace(m[2])
	if strings.HasPrefix(q, "(") && strings.HasSuffix(q, ")") {
		q = q[1 : len(q)-1]
	}
	return authz.Cell{Rights: strings.ReplaceAll(m[1], "/", ""), Qualifier: q}
}

// Allows is the PRD's reading of a cell, written independently of the policy code: whether it
// grants need, and whether only as a masked view. Any right lets its holder view the capability.
func Allows(cell authz.Cell, need authz.Right) (allowed, masked bool) {
	if need == authz.View {
		if strings.ContainsAny(cell.Rights, "CAPRV") {
			return true, false
		}
		return strings.Contains(cell.Rights, "M"), true
	}
	return strings.ContainsRune(cell.Rights, rune(need)), false
}
