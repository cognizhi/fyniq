package differential

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// dimensionScenario is a Department in use, with ENG in use and OPS out of use, and a Project out of use; 4010 requires
// a Department and 6010 a Project.
const dimensionScenario = `{"schema": "fyniq-scenario@1", "name": "dimensions-unit", "base_currency": "SGD",
  "currencies": {"SGD": 2, "USD": 2}, "as_of": "2026-02-28", "settings": {"books_start": "2026-01-01"},
  "dimensions": [
    {"name": "Department", "active": true, "values": [{"code": "ENG", "active": true}, {"code": "OPS", "active": false}]},
    {"name": "Project", "active": false, "values": [{"code": "P1", "active": true}]}],
  "accounts": [
    {"code": "1010", "type": "asset", "group": false, "active": true},
    {"code": "3100", "type": "equity", "group": false, "active": true},
    {"code": "4010", "type": "revenue", "group": false, "active": true, "requires": ["Department"]},
    {"code": "6010", "type": "expense", "group": false, "active": true, "requires": ["Project"]}],
  "documents": [
    {"type": "opening_balances", "id": "OB", "version": 1, "date": "2026-01-01", "memo": "Opening", "lines": [
      {"account": "1010", "currency": "SGD", "amount": "300.00", "dimensions": {"Department": "ENG"}},
      {"account": "1010", "currency": "SGD", "amount": "200.00"},
      {"account": "3100", "currency": "SGD", "amount": "-500.00"}]},
    {"type": "manual_journal", "id": "J1", "version": 1, "date": "2026-01-05", "memo": "Tagged", "lines": [
      {"account": "1010", "currency": "SGD", "amount": "100.00"},
      {"account": "4010", "currency": "SGD", "amount": "-100.00", "dimensions": {"Department": "ENG"}}]},
    {"type": "manual_journal", "id": "J2", "version": 1, "date": "2026-01-06", "memo": "No department", "lines": [
      {"account": "1010", "currency": "SGD", "amount": "50.00"},
      {"account": "4010", "currency": "SGD", "amount": "-50.00"}]},
    {"type": "manual_journal", "id": "J3", "version": 1, "date": "2026-01-07", "memo": "OPS is out of use", "lines": [
      {"account": "1010", "currency": "SGD", "amount": "50.00"},
      {"account": "4010", "currency": "SGD", "amount": "-50.00", "dimensions": {"Department": "OPS"}}]},
    {"type": "manual_journal", "id": "J4", "version": 1, "date": "2026-01-08", "memo": "Moves 40 to ENG", "lines": [
      {"account": "4010", "currency": "SGD", "amount": "40.00"},
      {"account": "4010", "currency": "SGD", "amount": "-40.00", "dimensions": {"Department": "ENG"}}]},
    {"type": "manual_journal", "id": "J5", "version": 1, "date": "2026-01-09", "memo": "Project is out of use", "lines": [
      {"account": "6010", "currency": "SGD", "amount": "10.00"},
      {"account": "1010", "currency": "SGD", "amount": "-10.00"}]},
    {"type": "manual_journal", "id": "J6", "version": 1, "date": "2026-01-10", "memo": "Names P1", "lines": [
      {"account": "6010", "currency": "SGD", "amount": "10.00", "dimensions": {"Project": "P1"}},
      {"account": "1010", "currency": "SGD", "amount": "-10.00"}]},
    {"type": "manual_journal", "id": "J7", "version": 1, "date": "2026-01-11", "memo": "In USD", "lines": [
      {"account": "4010", "currency": "USD", "amount": "-10.00", "fx_rate": "1.35", "fx_rate_source": "document", "dimensions": {"Department": "ENG"}},
      {"account": "1010", "currency": "SGD", "amount": "13.50"}]}],
  "reversals": [{"document": "J1", "date": "2026-02-10"}]}`

// The engine checks a journal's dimension values as the journal service does: a value out of use, or of a dimension
// out of use, rejects it (DIM-06); a line without a value its account requires rejects it (DIM-05) unless the journal's
// lines on that account net to zero (rev 0.172); a dimension out of use requires nothing. The opening balances keep
// one line per dimension set, a reversal keeps the values, and the result totals each month's accounts by set.
// req: FR-DIM-01
// req: FR-DIM-02
// req: QA-ENG-02
func TestDimensionsOnJournals(t *testing.T) {
	s, err := ParseScenario([]byte(dimensionScenario))
	if err != nil {
		t.Fatal(err)
	}
	res, err := RunEngine(s)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"J2", "J3", "J6"}; !reflect.DeepEqual(res.Rejected, want) {
		t.Errorf("rejected %v, want %v", res.Rejected, want)
	}
	eng := map[string]string{"Department": "ENG"}
	lines := func(doc, kind string) []EntryLine {
		for _, e := range res.Entries {
			if e.Document == doc && e.Kind == kind {
				return e.Lines
			}
		}
		t.Fatalf("no %s of %s", kind, doc)
		return nil
	}
	if got, want := lines("OB", "post"), []EntryLine{el("1010", "SGD", "200.00", "200.00"),
		{Account: "1010", Currency: "SGD", Amount: "300.00", BaseAmount: "300.00", Dimensions: eng},
		el("3100", "SGD", "-500.00", "-500.00")}; !reflect.DeepEqual(got, want) {
		t.Errorf("the opening balances, one line per set:\n got %+v\nwant %+v", got, want)
	}
	if got, want := lines("J1", "reversal"), []EntryLine{el("1010", "SGD", "-100.00", "-100.00"),
		{Account: "4010", Currency: "SGD", Amount: "100.00", BaseAmount: "100.00", Dimensions: eng}}; !reflect.DeepEqual(got, want) {
		t.Errorf("the reversal keeps the values:\n got %+v\nwant %+v", got, want)
	}
	want := map[string]map[string]map[string]string{
		"2026-01": {
			"1010": {"": "303.50", "Department=ENG": "300.00"},
			"3100": {"": "-500.00"},
			"4010": {"": "40.00", "Department=ENG": "-153.50"},
			"6010": {"": "10.00"},
		},
		"2026-02": {"1010": {"": "-100.00"}, "4010": {"Department=ENG": "100.00"}},
	}
	if !reflect.DeepEqual(res.DimensionBalances, want) {
		t.Errorf("dimension balances:\n got %v\nwant %v", res.DimensionBalances, want)
	}
	out, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckResult(out); err != nil {
		t.Errorf("the result breaks result.schema.json: %v", err)
	}

	// The comparator finds a line's other value and a set's other total.
	other := res
	other.Entries = append([]Entry(nil), res.Entries...)
	other.Entries[1].Lines = append([]EntryLine(nil), res.Entries[1].Lines...)
	other.Entries[1].Lines[1].Dimensions = nil
	if err := Compare(res, other); err == nil || !strings.Contains(err.Error(), "dimensions") {
		t.Errorf("a line without its value: %v", err)
	}
	other = res
	other.DimensionBalances = map[string]map[string]map[string]string{"2026-01": {"4010": {"Department=ENG": "-153.49"}}}
	if err := Compare(res, other); err == nil || !strings.Contains(err.Error(), "dimension balance 2026-01 account 1010") {
		t.Errorf("a set's other total: %v", err)
	}
	if err := Compare(res, res); err != nil {
		t.Errorf("a result against itself: %v", err)
	}
}

// A scenario whose dimensions break the format is invalid (README, "Invalid scenario or rejected document").
// req: FR-DIM-01
// req: QA-ENG-02
func TestDimensionFormatRules(t *testing.T) {
	change := func(f func(s map[string]any)) []byte {
		var s map[string]any
		if err := json.Unmarshal([]byte(dimensionScenario), &s); err != nil {
			t.Fatal(err)
		}
		f(s)
		b, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	dims := func(s map[string]any) []any { return s["dimensions"].([]any) }
	line := func(s map[string]any, doc, i int) map[string]any {
		return s["documents"].([]any)[doc].(map[string]any)["lines"].([]any)[i].(map[string]any)
	}
	for name, f := range map[string]func(s map[string]any){
		"two dimensions of one name": func(s map[string]any) { dims(s)[1].(map[string]any)["name"] = "Department" },
		"two values of one code": func(s map[string]any) {
			dims(s)[0].(map[string]any)["values"].([]any)[1].(map[string]any)["code"] = "ENG"
		},
		"an account requiring an undeclared dimension": func(s map[string]any) {
			s["accounts"].([]any)[2].(map[string]any)["requires"] = []any{"Region"}
		},
		"a line naming an undeclared dimension": func(s map[string]any) { line(s, 1, 1)["dimensions"] = map[string]any{"Region": "EAST"} },
		"a line naming an undeclared code":      func(s map[string]any) { line(s, 1, 1)["dimensions"] = map[string]any{"Department": "SALES"} },
		"an edit's line naming an undeclared code": func(s map[string]any) {
			s["reversals"] = []any{map[string]any{"document": "J1", "edit": map[string]any{"version": 2, "date": "2026-01-05", "memo": "Edited",
				"lines": []any{
					map[string]any{"account": "1010", "currency": "SGD", "amount": "100.00"},
					map[string]any{"account": "4010", "currency": "SGD", "amount": "-100.00", "dimensions": map[string]any{"Department": "SALES"}},
				}}}}
		},
	} {
		var invalid *InvalidError
		if _, err := ParseScenario(change(f)); !errors.As(err, &invalid) {
			t.Errorf("%s: %v, want an invalid scenario", name, err)
		}
	}
}
