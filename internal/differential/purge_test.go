package differential

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

// ParsePurge refuses what the oracle refuses (README, "Purges"), each with its reason: a document either side found
// invalid while the other purged would be a disagreement in itself.
// req: AUD-10
// req: QA-ENG-02
func TestParsePurgeRefusesWhatTheOracleRefuses(t *testing.T) {
	raw, err := os.ReadFile("../../test/differential/purges/hand-worked-year.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParsePurge(raw); err != nil {
		t.Fatalf("the golden ledger: %v", err)
	}
	type doc = map[string]any
	entry := func(d doc, i int) doc { return d["entries"].([]any)[i].(doc) }
	line := func(d doc, i, j int) doc { return entry(d, i)["lines"].([]any)[j].(doc) }
	for reason, edit := range map[string]func(doc){
		"two accounts":           func(d doc) { d["accounts"] = append(d["accounts"].([]any), d["accounts"].([]any)[0]) },
		"two entries":            func(d doc) { d["entries"] = append(d["entries"].([]any), entry(d, 0)) },
		"no such account":        func(d doc) { line(d, 0, 0)["account"] = "9999" },
		"not in currencies":      func(d doc) { line(d, 4, 0)["currency"] = "EUR" },
		"decimal places":         func(d doc) { line(d, 0, 0)["amount"], line(d, 0, 0)["base_amount"] = "1090.001", "1090.001" },
		"is not the base amount": func(d doc) { line(d, 0, 0)["amount"] = "1000.00" },
		"does not balance":       func(d doc) { line(d, 0, 0)["amount"], line(d, 0, 0)["base_amount"] = "1000.00", "1000.00" },
		"not an entry":           func(d doc) { entry(d, 9)["reverses"] = "E99" },
		"not the first day":      func(d doc) { d["boundary"] = "2019-02-01" },
		"base currency":          func(d doc) { d["base_currency"] = "EUR" },
		"revenue or expense":     func(d doc) { d["retained_earnings"] = "4010" },
		"is not an account":      func(d doc) { d["retained_earnings"] = "9999" },
		"date":                   func(d doc) { entry(d, 0)["posting_date"] = "2018-02-30" },
		"additional properties":  func(d doc) { d["extra"] = 1 },
	} {
		var d doc
		if err := json.Unmarshal(raw, &d); err != nil {
			t.Fatal(err)
		}
		edit(d)
		b, _ := json.Marshal(d)
		_, err := ParsePurge(b)
		var invalid *InvalidError
		if !errors.As(err, &invalid) || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(reason)) {
			t.Errorf("%s: %v", reason, err)
		}
	}
}
