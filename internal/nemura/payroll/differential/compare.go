package differential

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/cockroachdb/apd/v3"
)

// Difference is the first way two fyniq-payroll-run-result@1 results differ, or "" when they agree to the cent. Every figure is a decimal string and
// is compared as a decimal; the engine's raw entry ("lines") is for a reader and the refund's total is not compared, since no named policy rounds it; each
// person's broken deduction rules are reduced to the deductions they are broken by (the key of each, "" for the month's 50% limit), since the two sides name the rules in their own words (test/differential/README.md,
// "Payroll months and runs").
func Difference(engine, oracle []byte) string {
	var e, o any
	if err := json.Unmarshal(engine, &e); err != nil {
		return "the engine's result is not JSON: " + err.Error()
	}
	if err := json.Unmarshal(oracle, &o); err != nil {
		return "the oracle's result is not JSON: " + err.Error()
	}
	return tree(normalise(e), normalise(o), "")
}

func normalise(v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	delete(m, "lines")
	if people, ok := m["people"].([]any); ok {
		for _, p := range people {
			if pm, ok := p.(map[string]any); ok {
				delete(pm, "refund_total")
				if vs, ok := pm["violations"].([]any); ok {
					keys := []string{}
					for _, v := range vs {
						if vm, ok := v.(map[string]any); ok {
							key, _ := vm["key"].(string)
							keys = append(keys, key)
						}
					}
					sort.Strings(keys)
					list := make([]any, len(keys))
					for i, k := range keys {
						list[i] = k
					}
					pm["violations"] = list
				}
			}
		}
	}
	return m
}

func tree(e, o any, where string) string {
	switch ev := e.(type) {
	case map[string]any:
		om, ok := o.(map[string]any)
		if !ok {
			return fmt.Sprintf("%s: engine has an object, oracle %v", where, o)
		}
		keys := map[string]bool{}
		for k := range ev {
			keys[k] = true
		}
		for k := range om {
			keys[k] = true
		}
		names := make([]string, 0, len(keys))
		for k := range keys {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			a, aok := ev[k]
			b, bok := om[k]
			if aok != bok {
				return fmt.Sprintf("%s.%s: present in engine %t, in oracle %t", where, k, aok, bok)
			}
			if d := tree(a, b, where+"."+k); d != "" {
				return d
			}
		}
		return ""
	case []any:
		ol, ok := o.([]any)
		if !ok || len(ol) != len(ev) {
			return fmt.Sprintf("%s: engine %v, oracle %v", where, e, o)
		}
		for i := range ev {
			if d := tree(ev[i], ol[i], fmt.Sprintf("%s[%d]", where, i)); d != "" {
				return d
			}
		}
		return ""
	case string:
		os, ok := o.(string)
		if !ok {
			return fmt.Sprintf("%s: engine %q, oracle %v", where, ev, o)
		}
		a, _, errA := apd.NewFromString(ev)
		b, _, errB := apd.NewFromString(os)
		if errA == nil && errB == nil {
			if a.Cmp(b) != 0 {
				return fmt.Sprintf("%s: engine %s, oracle %s", where, ev, os)
			}
			return ""
		}
		if ev != os {
			return fmt.Sprintf("%s: engine %q, oracle %q", where, ev, os)
		}
		return ""
	}
	if e != o {
		return fmt.Sprintf("%s: engine %v, oracle %v", where, e, o)
	}
	return ""
}
