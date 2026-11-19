package peoplelink

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"unicode/utf8"
)

// Canonical returns the canonical bytes of a JSON document (PRD-001 FR-PPL-15; RFC 8785's subset): UTF-8, object keys sorted by
// their UTF-8 bytes (code point order), no insignificant whitespace, only strings, integers, booleans, null, arrays and
// objects. A number with a fraction or an exponent is refused: money and quantities travel as decimal strings (golden rule 1),
// so no float can be signed. Nemura's copy of this rule (internal/nemura/link) passes the same fixtures in contracts/peoplelink.
func Canonical(raw []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, errors.Join(ErrMalformed, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.Join(ErrMalformed, errors.New("there is more than one JSON document"))
	}
	var b bytes.Buffer
	if err := writeValue(&b, v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func writeValue(b *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case string:
		return writeString(b, x)
	case json.Number:
		n, err := strconv.ParseInt(x.String(), 10, 64)
		if err != nil || n > 1<<53 || n < -(1<<53) {
			return errors.Join(ErrMalformed, fmt.Errorf("the number %s is not an integer within 2^53: send money and quantities as decimal strings", x))
		}
		b.WriteString(strconv.FormatInt(n, 10))
	case []any:
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writeValue(b, e); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys) // Go compares strings byte by byte: UTF-8 order is code point order
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writeString(b, k); err != nil {
				return err
			}
			b.WriteByte(':')
			if err := writeValue(b, x[k]); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	default:
		return errors.Join(ErrMalformed, fmt.Errorf("a value of type %T cannot be signed", v))
	}
	return nil
}

// writeString writes a string as RFC 8785 does: only the quote, the backslash and the control characters are escaped
// (\b \t \n \f \r by name, the rest as \u00xx in lower case); every other character, U+2028 and U+2029 and HTML's <, > and &
// included, is written as it is.
func writeString(b *bytes.Buffer, s string) error {
	if !utf8.ValidString(s) {
		return errors.Join(ErrMalformed, errors.New("a string is not valid UTF-8"))
	}
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\b':
			b.WriteString(`\b`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\f':
			b.WriteString(`\f`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20:
			fmt.Fprintf(b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return nil
}
