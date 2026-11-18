package aitool

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// The operations of calc (contracts/ai/tools/calc.json).
const (
	OpAdd           = "add"
	OpSubtract      = "subtract"
	OpMultiply      = "multiply"
	OpDivide        = "divide"
	OpPercentChange = "percent_change"
	OpShare         = "share"
)

// CalcOps are calc's operations, in the order its definition lists them.
var CalcOps = []string{OpAdd, OpSubtract, OpMultiply, OpDivide, OpPercentChange, OpShare}

// The codes of calc's refusals.
const (
	CodeCalcOperands = "CALC-01" // the operands are not what the operation takes
	CodeCalcFact     = "CALC-02" // an operand names a fact the conversation has not read, or one without a value
	CodeCalcUnits    = "CALC-03" // the operands' units or currencies do not combine so
	CodeCalcZero     = "CALC-04" // a division by zero
)

// CalcError is a calculation calc refuses, in words the model can act on.
type CalcError struct{ Code, Message, Fix string }

func (e *CalcError) Error() string { return e.Code + ": " + e.Message }

func refuseCalc(code, fix, format string, args ...any) *CalcError {
	return &CalcError{Code: code, Message: fmt.Sprintf(format, args...), Fix: fix}
}

// Operand is one of calc's operands: a fact by its ID, or a constant.
type Operand struct {
	Fact     string `json:"fact,omitempty"`
	Constant string `json:"constant,omitempty"`
}

// CalcInput is calc's input, as its definition takes it.
type CalcInput struct {
	Op       string    `json:"op"`
	Operands []Operand `json:"operands"`
	Places   *int      `json:"places,omitempty"`
}

// displayPolicy is the rounding calc's result takes: RND-DISPLAY, a figure shown and never posted (PRD-001 §10.4).
const displayPolicy = rounding.OwnershipName

// constantText is a constant as calc's definition takes one: plain decimal text, no exponent, no NaN.
var constantText = regexp.MustCompile(`^-?[0-9]{1,20}(\.[0-9]{1,10})?$`)

// calcWork is the exact context calc computes in, before its one rounding.
var calcWork = apd.BaseContext.WithPrecision(60)

// value is an operand read: its decimal, and the unit and currency of its fact ("" for a constant).
type value struct {
	d              *apd.Decimal
	unit, currency string
	fact           *Fact
}

// Calc calculates in exact decimals on facts the conversation has read and on constants (AI-P6), so a figure no tool
// gave is never worked out by the model: a sum, a difference, a product, a quotient, a percentage change (from the
// first to the second, of the first's size) or a share (the first as a percentage of the second). Amounts combine only
// in one currency, and a product holds at most one amount. The result is rounded once, by RND-DISPLAY (half away from
// zero, to places, 2 by default: it is shown, never posted), and is a fact naming its input facts (AI-68). known finds
// the conversation's facts by ID.
func Calc(in CalcInput, known func(id string) (Fact, bool)) (Fact, error) {
	if !slices.Contains(CalcOps, in.Op) {
		return Fact{}, refuseCalc(CodeCalcOperands, "Use one of "+strings.Join(CalcOps, ", "), "%q is not an operation of calc", in.Op)
	}
	two := in.Op != OpAdd && in.Op != OpMultiply
	switch {
	case two && len(in.Operands) != 2:
		return Fact{}, refuseCalc(CodeCalcOperands, "Give it exactly two operands, in order", "%s takes two operands, not %d", in.Op, len(in.Operands))
	case len(in.Operands) < 2 || len(in.Operands) > 20:
		return Fact{}, refuseCalc(CodeCalcOperands, "Give it two to 20 operands", "%s takes two to 20 operands, not %d", in.Op, len(in.Operands))
	}
	places := 2
	if in.Places != nil {
		if *in.Places < 0 || *in.Places > 10 {
			return Fact{}, refuseCalc(CodeCalcOperands, "Ask for 0 to 10 decimal places", "places is %d", *in.Places)
		}
		places = *in.Places
	}
	vs := make([]value, len(in.Operands))
	for i, o := range in.Operands {
		switch {
		case (o.Fact == "") == (o.Constant == ""):
			return Fact{}, refuseCalc(CodeCalcOperands, "Give each operand a fact's ID or a constant, not both", "operand %d is not one fact or one constant", i+1)
		case o.Fact != "":
			f, ok := known(o.Fact)
			if !ok {
				return Fact{}, refuseCalc(CodeCalcFact, "Use a fact a tool returned in this conversation, by its ID", "no fact %s was read in this conversation", o.Fact)
			}
			d, _, err := apd.NewFromString(f.Value)
			if f.Value == "" || err != nil {
				return Fact{}, refuseCalc(CodeCalcFact, "Say why it has no value instead of calculating with it", "the fact %s has no value: %s", o.Fact, f.Undefined)
			}
			vs[i] = value{d: d, unit: f.Unit, currency: f.Currency, fact: &f}
		default:
			d, _, err := apd.NewFromString(o.Constant)
			if err != nil || !constantText.MatchString(o.Constant) {
				return Fact{}, refuseCalc(CodeCalcOperands, "Write a constant as decimal text, such as 12 or 0.09", "operand %d, %q, is not a decimal", i+1, o.Constant)
			}
			vs[i] = value{d: d}
		}
	}
	result, unit, currency, err := compute(in.Op, vs)
	if err != nil {
		return Fact{}, err
	}
	return calcFact(in, vs, rounding.Display(result, places), unit, currency), nil
}

// measured says whether a value carries a unit that does not scale by a constant: an amount, months, days, a percentage
// or a ratio. A count, a number and a constant scale.
func measured(v value) bool { return v.unit != "" && v.unit != UnitCount && v.unit != UnitNumber }

// same says whether two values are of one unit and currency, a constant matching anything.
func same(a, b value) bool {
	return a.fact == nil || b.fact == nil || (a.unit == b.unit && a.currency == b.currency)
}

func compute(op string, vs []value) (*apd.Decimal, string, string, error) {
	c := calcWork
	out := new(apd.Decimal)
	unitOf := func() (string, string) { // the unit of the first fact operand, or a number
		for _, v := range vs {
			if v.fact != nil {
				return v.unit, v.currency
			}
		}
		return UnitNumber, ""
	}
	mismatch := func() error {
		var units []string
		for _, v := range vs {
			if v.fact != nil {
				units = append(units, strings.TrimSpace(v.unit+" "+v.currency))
			}
		}
		return refuseCalc(CodeCalcUnits, "Combine figures of one unit and currency; convert first, or divide by a constant",
			"%s cannot combine %s", op, strings.Join(units, " and "))
	}
	switch op {
	case OpAdd, OpSubtract:
		for _, v := range vs {
			if !sameAsAll(vs, v) {
				return nil, "", "", mismatch()
			}
		}
		out.Set(vs[0].d)
		for _, v := range vs[1:] {
			var err error
			if op == OpAdd {
				_, err = c.Add(out, out, v.d)
			} else {
				_, err = c.Sub(out, out, v.d)
			}
			if err != nil {
				return nil, "", "", err
			}
		}
		u, cur := unitOf()
		return out, u, cur, nil
	case OpMultiply:
		var unit value
		n := 0
		for _, v := range vs {
			if measured(v) {
				unit, n = v, n+1
			}
		}
		if n > 1 {
			return nil, "", "", mismatch()
		}
		out.SetInt64(1)
		for _, v := range vs {
			if _, err := c.Mul(out, out, v.d); err != nil {
				return nil, "", "", err
			}
		}
		if n == 1 {
			return out, unit.unit, unit.currency, nil
		}
		return out, UnitNumber, "", nil
	}
	a, b := vs[0], vs[1]
	if b.d.IsZero() || (op == OpPercentChange && a.d.IsZero()) {
		return nil, "", "", refuseCalc(CodeCalcZero, "Say the figure cannot be calculated, and why", "%s divides by zero", op)
	}
	switch op {
	case OpDivide:
		unit, currency := UnitNumber, ""
		switch {
		case measured(a) && measured(b) && same(a, b):
			unit = "ratio"
		case measured(a) && !measured(b):
			unit, currency = a.unit, a.currency
		case measured(b):
			return nil, "", "", mismatch()
		}
		if _, err := c.Quo(out, a.d, b.d); err != nil {
			return nil, "", "", err
		}
		return out, unit, currency, nil
	case OpPercentChange:
		if !same(a, b) {
			return nil, "", "", mismatch()
		}
		size := new(apd.Decimal).Abs(a.d)
		if _, err := c.Sub(out, b.d, a.d); err != nil {
			return nil, "", "", err
		}
		if _, err := c.Quo(out, out, size); err != nil {
			return nil, "", "", err
		}
	case OpShare:
		if !same(a, b) {
			return nil, "", "", mismatch()
		}
		if _, err := c.Quo(out, a.d, b.d); err != nil {
			return nil, "", "", err
		}
	}
	if _, err := c.Mul(out, out, apd.New(100, 0)); err != nil {
		return nil, "", "", err
	}
	return out, "percent", "", nil
}

// sameAsAll says whether v is of one unit and currency with every fact operand.
func sameAsAll(vs []value, v value) bool {
	for _, w := range vs {
		if !same(v, w) {
			return false
		}
	}
	return true
}

// calcFact is calc's result as a fact: what it calculated, from which facts, at which revision and over which period
// when its facts share them.
func calcFact(in CalcInput, vs []value, result, unit, currency string) Fact {
	symbols := map[string]string{OpAdd: " + ", OpSubtract: " − ", OpMultiply: " × ", OpDivide: " ÷ "}
	terms := make([]string, len(vs))
	var inputs []string
	from, to, periods := "", "", true
	var revision int64
	for i, v := range vs {
		if v.fact == nil {
			terms[i] = in.Operands[i].Constant
			continue
		}
		terms[i] = v.fact.ID
		inputs = append(inputs, v.fact.ID)
		revision = max(revision, v.fact.Revision)
		if len(inputs) == 1 {
			from, to = v.fact.From, v.fact.To
		} else if v.fact.From != from || v.fact.To != to {
			periods = false
		}
	}
	expression := strings.Join(terms, symbols[in.Op])
	switch in.Op {
	case OpPercentChange:
		expression = "the change from " + terms[0] + " to " + terms[1] + ", in percent"
	case OpShare:
		expression = terms[0] + " as a percentage of " + terms[1]
	}
	if !periods {
		from, to = "", ""
	}
	places := "2"
	if in.Places != nil {
		places = strconv.Itoa(*in.Places)
	}
	sum := sha256.Sum256([]byte(strings.Join(append([]string{"calc", in.Op, places}, terms...), "\x00")))
	return Fact{ID: "F" + hex.EncodeToString(sum[:8]), Measure: "calc." + in.Op, Name: expression, Value: result, Unit: unit, Currency: currency,
		From: from, To: to, Sign: "as calculated from its inputs, rounded once by " + displayPolicy, Revision: revision,
		Source: "calc: " + expression, Inputs: inputs}
}
