package aitool_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/cognizhi/fyniq/internal/aitool"
)

// calc works in exact decimals on the conversation's facts and on constants (AI-P6): sums, differences, products,
// quotients, percentage changes and shares, each rounded once by RND-DISPLAY, half away from zero, to its places. Its
// result is a fact naming its input facts (AI-68), stable for what it calculated. Amounts combine only in one currency,
// a product holds one amount at most, and a fact it was not given, a fact without a value, a division by zero and an
// operand count the operation does not take are refused with a code the model can act on.
// req: AI-P6
// req: AI-68
func TestCalcIsExactAndNamesItsInputs(t *testing.T) {
	facts := map[string]aitool.Fact{
		"F0000000000000001": {ID: "F0000000000000001", Value: "125000.00", Unit: aitool.UnitAmount, Currency: "SGD", From: "2026-07-01", To: "2026-09-30", Revision: 41},
		"F0000000000000002": {ID: "F0000000000000002", Value: "100000.00", Unit: aitool.UnitAmount, Currency: "SGD", From: "2026-07-01", To: "2026-09-30", Revision: 42},
		"F0000000000000003": {ID: "F0000000000000003", Value: "0.1", Unit: aitool.UnitNumber},
		"F0000000000000004": {ID: "F0000000000000004", Value: "0.2", Unit: aitool.UnitNumber},
		"F0000000000000005": {ID: "F0000000000000005", Value: "900.00", Unit: aitool.UnitAmount, Currency: "USD"},
		"F0000000000000006": {ID: "F0000000000000006", Undefined: "no revenue in the month", Unit: "percent"},
		"F0000000000000007": {ID: "F0000000000000007", Value: "4", Unit: "months"},
	}
	known := func(id string) (aitool.Fact, bool) { f, ok := facts[id]; return f, ok }
	fact := func(id string) aitool.Operand { return aitool.Operand{Fact: id} }
	constant := func(c string) aitool.Operand { return aitool.Operand{Constant: c} }
	places := func(n int) *int { return &n }

	for _, c := range []struct {
		what        string
		in          aitool.CalcInput
		value, unit string
		currency    string
		inputs      []string
		from, to    string
		revision    int64
	}{
		{"revenue less costs", aitool.CalcInput{Op: "subtract", Operands: []aitool.Operand{fact("F0000000000000001"), fact("F0000000000000002")}},
			"25000.00", "amount", "SGD", []string{"F0000000000000001", "F0000000000000002"}, "2026-07-01", "2026-09-30", 42},
		{"0.1 and 0.2, exactly", aitool.CalcInput{Op: "add", Operands: []aitool.Operand{fact("F0000000000000003"), fact("F0000000000000004")}, Places: places(10)},
			"0.3000000000", "number", "", []string{"F0000000000000003", "F0000000000000004"}, "", "", 0},
		{"GST on revenue", aitool.CalcInput{Op: "multiply", Operands: []aitool.Operand{fact("F0000000000000001"), constant("0.09")}},
			"11250.00", "amount", "SGD", []string{"F0000000000000001"}, "2026-07-01", "2026-09-30", 41},
		{"a month's revenue", aitool.CalcInput{Op: "divide", Operands: []aitool.Operand{fact("F0000000000000001"), constant("3")}},
			"41666.67", "amount", "SGD", []string{"F0000000000000001"}, "2026-07-01", "2026-09-30", 41},
		{"revenue to costs", aitool.CalcInput{Op: "divide", Operands: []aitool.Operand{fact("F0000000000000001"), fact("F0000000000000002")}, Places: places(4)},
			"1.2500", "ratio", "", []string{"F0000000000000001", "F0000000000000002"}, "2026-07-01", "2026-09-30", 42},
		{"the change from costs to revenue", aitool.CalcInput{Op: "percent_change", Operands: []aitool.Operand{fact("F0000000000000002"), fact("F0000000000000001")}},
			"25.00", "percent", "", []string{"F0000000000000002", "F0000000000000001"}, "2026-07-01", "2026-09-30", 42},
		{"costs as a share of revenue", aitool.CalcInput{Op: "share", Operands: []aitool.Operand{fact("F0000000000000002"), fact("F0000000000000001")}},
			"80.00", "percent", "", []string{"F0000000000000002", "F0000000000000001"}, "2026-07-01", "2026-09-30", 42},
		{"half away from zero", aitool.CalcInput{Op: "add", Operands: []aitool.Operand{constant("-2.344"), constant("-0.001")}},
			"-2.35", "number", "", nil, "", "", 0},
		{"a number of months scaled", aitool.CalcInput{Op: "multiply", Operands: []aitool.Operand{fact("F0000000000000007"), constant("2")}, Places: places(0)},
			"8", "months", "", []string{"F0000000000000007"}, "", "", 0},
	} {
		f, err := aitool.Calc(c.in, known)
		if err != nil {
			t.Errorf("%s: %v", c.what, err)
			continue
		}
		if f.Value != c.value || f.Unit != c.unit || f.Currency != c.currency || !reflect.DeepEqual(f.Inputs, c.inputs) || f.From != c.from || f.To != c.to ||
			f.Revision != c.revision || f.Measure != "calc."+c.in.Op || len(f.ID) != 17 || f.Source == "" {
			t.Errorf("%s: %+v", c.what, f)
		}
		if again, _ := aitool.Calc(c.in, known); again.ID != f.ID {
			t.Errorf("%s: its ID is not stable", c.what)
		}
	}

	for _, c := range []struct {
		what string
		in   aitool.CalcInput
		code string
	}{
		{"SGD and USD", aitool.CalcInput{Op: "add", Operands: []aitool.Operand{fact("F0000000000000001"), fact("F0000000000000005")}}, aitool.CodeCalcUnits},
		{"an amount times an amount", aitool.CalcInput{Op: "multiply", Operands: []aitool.Operand{fact("F0000000000000001"), fact("F0000000000000002")}}, aitool.CodeCalcUnits},
		{"a constant over an amount", aitool.CalcInput{Op: "divide", Operands: []aitool.Operand{constant("1"), fact("F0000000000000001")}}, aitool.CodeCalcUnits},
		{"months and an amount", aitool.CalcInput{Op: "share", Operands: []aitool.Operand{fact("F0000000000000007"), fact("F0000000000000001")}}, aitool.CodeCalcUnits},
		{"a fact not read", aitool.CalcInput{Op: "add", Operands: []aitool.Operand{fact("F00000000000000ff"), constant("1")}}, aitool.CodeCalcFact},
		{"a fact without a value", aitool.CalcInput{Op: "add", Operands: []aitool.Operand{fact("F0000000000000006"), constant("1")}}, aitool.CodeCalcFact},
		{"a division by zero", aitool.CalcInput{Op: "divide", Operands: []aitool.Operand{fact("F0000000000000001"), constant("0.00")}}, aitool.CodeCalcZero},
		{"a change from zero", aitool.CalcInput{Op: "percent_change", Operands: []aitool.Operand{constant("0"), fact("F0000000000000001")}}, aitool.CodeCalcZero},
		{"three operands to subtract", aitool.CalcInput{Op: "subtract", Operands: []aitool.Operand{constant("3"), constant("2"), constant("1")}}, aitool.CodeCalcOperands},
		{"one operand to add", aitool.CalcInput{Op: "add", Operands: []aitool.Operand{constant("3")}}, aitool.CodeCalcOperands},
		{"a fact and a constant in one operand", aitool.CalcInput{Op: "add", Operands: []aitool.Operand{{Fact: "F0000000000000003", Constant: "1"}, constant("1")}}, aitool.CodeCalcOperands},
		{"a constant with an exponent", aitool.CalcInput{Op: "add", Operands: []aitool.Operand{constant("1e9"), constant("1")}}, aitool.CodeCalcOperands},
		{"a constant that is not a number", aitool.CalcInput{Op: "add", Operands: []aitool.Operand{constant("1"), constant("NaN")}}, aitool.CodeCalcOperands},
		{"an operation calc has not", aitool.CalcInput{Op: "power", Operands: []aitool.Operand{constant("2"), constant("3")}}, aitool.CodeCalcOperands},
	} {
		_, err := aitool.Calc(c.in, known)
		var ce *aitool.CalcError
		if !errors.As(err, &ce) || ce.Code != c.code || ce.Fix == "" {
			t.Errorf("%s: %v, want %s", c.what, err, c.code)
		}
	}
}

// calc's definition takes what Calc takes (design D5): its operations, its operands' fields and its places.
// req: AI-P6
func TestCalcsDefinitionIsCalcs(t *testing.T) {
	tool, _ := aitool.Default().Get("calc")
	var s struct {
		Properties struct {
			Op struct {
				Enum []string `json:"enum"`
			} `json:"op"`
			Operands struct {
				Items struct {
					Properties map[string]any `json:"properties"`
				} `json:"items"`
			} `json:"operands"`
			Places map[string]any `json:"places"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(tool.InputSchema, &s); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.Properties.Op.Enum, aitool.CalcOps) || len(s.Properties.Operands.Items.Properties) != 2 ||
		s.Properties.Operands.Items.Properties["fact"] == nil || s.Properties.Operands.Items.Properties["constant"] == nil || s.Properties.Places == nil {
		t.Errorf("calc's definition: %s", tool.InputSchema)
	}
}
