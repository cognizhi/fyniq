package approval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

// maxSafeInteger is the largest integer an RFC 8785 number holds exactly (an IEEE 754 double's 2^53 − 1).
const maxSafeInteger = 1<<53 - 1

// Canonical serialises v as RFC 8785 JSON (the JSON Canonicalization Scheme): no whitespace, object members
// sorted by their names' UTF-16 code units, strings with only the escapes ECMAScript's JSON.stringify makes,
// and numbers in their shortest form. v is built from map[string]any, []any, string, bool, nil and integers (a
// json.Number read back from a stored change counts when it is an integer);
// money and every other decimal travel as strings (ENG-10), so an envelope never holds a binary fraction, and a
// float or any other type is refused rather than serialised in a form two builds might disagree on.
func Canonical(v any) ([]byte, error) {
	var b bytes.Buffer
	if err := canonical(&b, v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func canonical(b *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case string:
		return quote(b, x)
	case int:
		return integer(b, int64(x))
	case int64:
		return integer(b, x)
	case int32:
		return integer(b, int64(x))
	case json.Number:
		n, err := strconv.ParseInt(string(x), 10, 64)
		if err != nil {
			return fmt.Errorf("an envelope holds integers only, not %s: use a string for decimals and text", x)
		}
		return integer(b, n)
	case map[string]any:
		names := make([]string, 0, len(x))
		for k := range x {
			names = append(names, k)
		}
		slices.SortFunc(names, utf16Compare)
		b.WriteByte('{')
		for i, k := range names {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := quote(b, k); err != nil {
				return err
			}
			b.WriteByte(':')
			if err := canonical(b, x[k]); err != nil {
				return fmt.Errorf("%s: %w", k, err)
			}
		}
		b.WriteByte('}')
	case []any:
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := canonical(b, e); err != nil {
				return fmt.Errorf("[%d]: %w", i, err)
			}
		}
		b.WriteByte(']')
	case []string:
		a := make([]any, len(x))
		for i, s := range x {
			a[i] = s
		}
		return canonical(b, a)
	default:
		return fmt.Errorf("an envelope holds no %T: use a string for decimals and text", v)
	}
	return nil
}

func integer(b *bytes.Buffer, n int64) error {
	if n > maxSafeInteger || n < -maxSafeInteger {
		return fmt.Errorf("%d is beyond the integers RFC 8785 holds exactly; send it as a string", n)
	}
	b.WriteString(strconv.FormatInt(n, 10))
	return nil
}

// quote writes s as RFC 8785 §3.2.2.2 does: the two-character escapes for \b \t \n \f \r " and \, \u00xx in
// lower-case hex for the other control characters, and every other character as itself.
func quote(b *bytes.Buffer, s string) error {
	if !utf8.ValidString(s) {
		return fmt.Errorf("%q is not valid UTF-8", s)
	}
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 {
				fmt.Fprintf(b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return nil
}

// utf16Compare orders two names by their UTF-16 code units (RFC 8785 §3.2.3), which differs from Go's byte
// order for characters beyond the Basic Multilingual Plane.
func utf16Compare(a, b string) int {
	x, y := utf16.Encode([]rune(a)), utf16.Encode([]rune(b))
	for i := 0; i < len(x) && i < len(y); i++ {
		if x[i] != y[i] {
			if x[i] < y[i] {
				return -1
			}
			return 1
		}
	}
	return len(x) - len(y)
}
