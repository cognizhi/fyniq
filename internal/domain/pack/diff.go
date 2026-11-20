package pack

import (
	"slices"
	"strings"
)

// Change is one difference between two versions of a pack (PRD-001 ENG-33): a row added, removed or changed in a table, so an organisation sees what an update would do before it
// accepts it. A table has no declared key, so a row is matched by its first column: rows that share it and differ are a change, listing each row that went and each that came.
type Change struct {
	Table string
	Kind  string // added, removed or changed
	Key   string // the first column
	Was   [][]string
	Now   [][]string
}

// Differences between the kinds of change.
const (
	ChangeAdded   = "added"
	ChangeRemoved = "removed"
	ChangeChanged = "changed"
)

func rowsByKey(rows [][]string) map[string][][]string {
	out := map[string][][]string{}
	for _, r := range rows {
		if len(r) > 0 {
			out[r[0]] = append(out[r[0]], r)
		}
	}
	return out
}

func sameRows(a, b [][]string) bool {
	if len(a) != len(b) {
		return false
	}
	key := func(r []string) string { return strings.Join(r, "\x1f") }
	ak, bk := make([]string, len(a)), make([]string, len(b))
	for i := range a {
		ak[i], bk[i] = key(a[i]), key(b[i])
	}
	slices.Sort(ak)
	slices.Sort(bk)
	return slices.Equal(ak, bk)
}

// Diff lists what changed from the table rows of from to those of to, tables and keys in order, pinned cases left out (they are tests, not values). A table that only one
// version holds is all added or all removed.
func Diff(from, to Version) []Change {
	var out []Change
	names := map[string]bool{}
	for n := range from.Tables {
		names[n] = true
	}
	for n := range to.Tables {
		names[n] = true
	}
	for _, name := range slices.Sorted(mapKeys(names)) {
		a, b := rowsByKey(from.Tables[name].Rows), rowsByKey(to.Tables[name].Rows)
		keys := map[string]bool{}
		for k := range a {
			keys[k] = true
		}
		for k := range b {
			keys[k] = true
		}
		for _, k := range slices.Sorted(mapKeys(keys)) {
			switch {
			case len(a[k]) == 0:
				out = append(out, Change{Table: name, Kind: ChangeAdded, Key: k, Now: b[k]})
			case len(b[k]) == 0:
				out = append(out, Change{Table: name, Kind: ChangeRemoved, Key: k, Was: a[k]})
			case !sameRows(a[k], b[k]):
				out = append(out, Change{Table: name, Kind: ChangeChanged, Key: k, Was: a[k], Now: b[k]})
			}
		}
	}
	return out
}

func mapKeys(m map[string]bool) func(yield func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}
