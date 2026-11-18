package aitool

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The units of a fact.
const (
	UnitAmount = "amount"
	UnitCount  = "count"
	UnitNumber = "number"
)

// Fact is a figure a read tool returned (AI-68): what it is, its value as exact decimal text, its unit and currency,
// the period or date it is of, the record or member it belongs to, how it is signed, the ledger revision it was read at
// and where it comes from. An answer cites a figure by its fact's ID, and platform code renders it from the fact (AI-08).
type Fact struct {
	ID          string   `json:"id" doc:"The fact's ID, which an answer cites (AI-08)." example:"F3a9c0d1e2b4f5a6"`
	Measure     string   `json:"measure" doc:"What it is: a measure of the semantic layer, or a record's field, such as invoice.total." example:"invoice.total"`
	Name        string   `json:"name,omitempty" doc:"The measure's name; for a record's field, where it is in the record." example:"lines[2].net"`
	Value       string   `json:"value,omitempty" doc:"Exact decimal text; absent when the fact has no value." example:"1250.00"`
	Words       string   `json:"words,omitempty" doc:"What stands for the value, such as Not burning." example:"Not burning"`
	Undefined   string   `json:"undefined,omitempty" doc:"Why the fact has no value: never 0." example:"no revenue in the month"`
	Unit        string   `json:"unit" enum:"amount,count,number,percent,months,ratio,days" example:"amount"`
	Currency    string   `json:"currency,omitempty" doc:"An amount's currency." example:"SGD"`
	From        string   `json:"from,omitempty" format:"date" doc:"The period's first day; a stock's date." example:"2026-07-01"`
	To          string   `json:"to,omitempty" format:"date" doc:"The period's last day; a stock's date." example:"2026-09-30"`
	AsAt        string   `json:"as_at,omitempty" format:"date" doc:"The day a record's figure was read." example:"2026-10-15"`
	Dimension   string   `json:"dimension,omitempty" doc:"What the member is: a semantic dimension, or the kind of record." example:"invoice"`
	Member      string   `json:"member,omitempty" doc:"The member or record it belongs to." example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b"`
	MemberName  string   `json:"member_name,omitempty" example:"Acme Pte Ltd"`
	Sign        string   `json:"sign" doc:"How the value is signed." example:"as the record shows it"`
	Revision    int64    `json:"ledger_revision" doc:"The ledger revision it was read at (ENG-46)." example:"1204"`
	Source      string   `json:"source" doc:"What it is, in words." example:"the invoice's total"`
	Lineage     string   `json:"lineage" doc:"The API path it was read from." example:"/v1/invoices/0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b"`
	Inputs      []string `json:"inputs,omitempty" doc:"A calculated fact's input facts, by ID (AI-68, AI-P6)." example:"[\"F3a9c0d1e2b4f5a6\"]"`
	Pack        string   `json:"pack,omitempty" doc:"A statutory value's pack (AI-53)." example:"sg-gst"`
	PackVersion string   `json:"pack_version,omitempty" doc:"A statutory value's pack version (AI-53)." example:"sg-gst@2026.3"`
}

// Subject is what a read's figures are of: its measures' prefix (the kind of record, or the read), the record it is,
// if it is one, and where and when it was read.
type Subject struct {
	Measure  string // each figure's measure is Measure and the field's name, such as invoice.total
	Kind     string // the kind of record, for one record
	Member   string // the record's ID, for one record
	Source   string // what was read, in words
	Lineage  string // the API path read
	Revision int64
	AsAt     string
}

// notFigures are the fields whose numbers identify or order something rather than measure it.
var notFigures = []string{"id", "version", "revision", "ledger_revision", "line", "position", "step", "sequence", "number",
	"year", "month", "day", "page", "code", "reference", "ref", "status", "http_status", "suite_version"}

// decimalText is a decimal string with a point, such as a rate or a quantity; a whole decimal string is an ID or a
// number, not a figure.
var decimalText = regexp.MustCompile(`^-?(0|[1-9][0-9]{0,19})\.[0-9]{1,10}$`)

// Decode reads b as JSON keeping every number's exact text.
func Decode(b []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// Figures replaces every figure in v, a JSON value from Decode, by {"fact": its ID}, and gives the facts in the order
// they appear: a money object ({"amount", "currency"}) is an amount, a whole number a count, another number or decimal
// text a number. A figure's ID is a hash of what it is, where it is and the revision it was read at.
func Figures(v any, s Subject) (any, []Fact) {
	var facts []Fact
	out := figures(v, nil, s, &facts)
	return out, facts
}

func figures(v any, at []string, s Subject, facts *[]Fact) any {
	fact := func(value, unit, currency string) any {
		name := nameOf(at)
		f := Fact{Measure: s.Measure + "." + measureOf(at), Name: name, Value: value, Unit: unit, Currency: currency, Dimension: s.Kind,
			Member: s.Member, Sign: "as the record shows it", Revision: s.Revision, Source: s.Source, Lineage: s.Lineage, AsAt: s.AsAt}
		sum := sha256.Sum256([]byte(strings.Join([]string{s.Lineage, name, value, currency, strconv.FormatInt(s.Revision, 10)}, "\x00")))
		f.ID = "F" + hex.EncodeToString(sum[:8])
		*facts = append(*facts, f)
		return map[string]any{"fact": f.ID}
	}
	key := ""
	if len(at) > 0 {
		key = at[len(at)-1]
	}
	switch x := v.(type) {
	case map[string]any:
		if amount, ok := x["amount"].(string); ok && len(x) == 2 {
			if currency, ok := x["currency"].(string); ok {
				return fact(amount, UnitAmount, currency)
			}
		}
		out := make(map[string]any, len(x))
		for _, k := range sortedKeys(x) {
			out[k] = figures(x[k], append(slices.Clone(at), k), s, facts)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = figures(e, append(slices.Clone(at), "["+strconv.Itoa(i)+"]"), s, facts)
		}
		return out
	case json.Number:
		if identifies(key) {
			return x
		}
		if _, err := x.Int64(); err == nil {
			return fact(x.String(), UnitCount, "")
		}
		return fact(x.String(), UnitNumber, "")
	case string:
		if decimalText.MatchString(x) && !identifies(key) {
			return fact(x, UnitNumber, "")
		}
	}
	return v
}

// identifies says whether a field's number names or orders something rather than measures it.
func identifies(key string) bool {
	return slices.Contains(notFigures, key) || strings.HasSuffix(key, "_id") || strings.HasSuffix(key, "_number") ||
		strings.HasSuffix(key, "_version")
}

// nameOf is where a field is: lines[2].net.
func nameOf(at []string) string {
	var b strings.Builder
	for _, a := range at {
		if b.Len() > 0 && !strings.HasPrefix(a, "[") {
			b.WriteString(".")
		}
		b.WriteString(a)
	}
	return b.String()
}

// measureOf is a field's name without its places in lists: lines[2].net is lines.net.
func measureOf(at []string) string {
	var out []string
	for _, a := range at {
		if !strings.HasPrefix(a, "[") {
			out = append(out, a)
		}
	}
	return strings.Join(out, ".")
}

// Omission is what a tool's limits left out of a result (AI-04): of a list, how many it shows of how many there are.
type Omission struct {
	Path  string `json:"path" doc:"The list, as a path in the result." example:"lines"`
	Shown int    `json:"shown" example:"50"`
	Of    int    `json:"of" example:"120"`
}

// Cut keeps at most rows items of every list in v, the first ones, and says what it left out.
func Cut(v any, rows int) (any, []Omission) {
	var left []Omission
	out := cut(v, "", rows, &left)
	return out, left
}

func cut(v any, at string, rows int, left *[]Omission) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for _, k := range sortedKeys(x) {
			out[k] = cut(x[k], strings.TrimPrefix(at+"."+k, "."), rows, left)
		}
		return out
	case []any:
		n := min(len(x), rows)
		if n < len(x) {
			*left = append(*left, Omission{Path: at, Shown: n, Of: len(x)})
		}
		out := make([]any, n)
		for i := range n {
			out[i] = cut(x[i], fmt.Sprintf("%s[%d]", at, i), rows, left)
		}
		return out
	}
	return v
}

// Tokens is about how many tokens v takes: four characters a token, as the providers' guides reckon English and JSON.
func Tokens(v any) int {
	b, _ := json.Marshal(v)
	return (len(b) + 3) / 4
}

// Fit is a result within a tool's limits (AI-04, design D6): at most its rows of each list, and within its token
// budget, the lists shortened further as needed, so a larger result is summarised with what was left out counted.
// The figures of what it shows are facts. It is nil, with only the omissions, when not even one row of each fits.
func Fit(v any, l Limits, s Subject) (content any, facts []Fact, left []Omission) {
	for rows := l.Rows; rows >= 1; rows /= 2 {
		shown, omitted := Cut(v, rows)
		withFacts, f := Figures(shown, s)
		if Tokens(withFacts)+Tokens(f) <= l.ResultTokens {
			return withFacts, f, omitted
		}
		if rows == 1 {
			return nil, nil, append(omitted, Omission{Path: "", Shown: 0, Of: 1})
		}
	}
	return nil, nil, nil
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
