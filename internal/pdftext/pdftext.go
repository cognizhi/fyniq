// Package pdftext reads back the text of the PDFs Fyniq renders, for QA-RPT-03's export round trip: a statement printed
// as PDF must say what the statement says. It reads the PDFs Typst writes (PDF 1.7 with a cross-reference table, Flate
// streams, Type0 fonts with ToUnicode maps), not every PDF there is; anything it cannot read is an error, never a guess.
// It is for tests and checks: nothing it returns is shown to a person.
package pdftext

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
)

// maxStream bounds a stream once inflated, so a hostile PDF cannot exhaust memory.
const maxStream = 32 << 20

var (
	objStart = regexp.MustCompile(`(?m)(\d+)\s+0\s+obj\b`)
	ref      = regexp.MustCompile(`^(\d+)\s+0\s+R`)
	refs     = regexp.MustCompile(`(\d+)\s+0\s+R`)
)

type object struct {
	dict   string // the object's text before any stream: its dictionary or value
	stream []byte // its stream as stored, or nil
}

type document struct {
	objects map[int]object
	cmaps   map[int]map[uint16]string
}

// Pages is the text of each page in order: one line for each block of text the page shows, in the order it is drawn.
func Pages(pdf []byte) ([]string, error) {
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		return nil, errors.New("pdftext: not a PDF")
	}
	d := &document{objects: map[int]object{}, cmaps: map[int]map[uint16]string{}}
	if err := d.parse(pdf); err != nil {
		return nil, err
	}
	root, ok := d.find("/Type/Catalog")
	if !ok {
		return nil, errors.New("pdftext: no catalogue")
	}
	pages, ok := d.refOf(d.objects[root].dict, "/Pages")
	if !ok {
		return nil, errors.New("pdftext: the catalogue names no pages")
	}
	var order []int
	if err := d.walk(pages, &order, 0); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(order))
	for _, p := range order {
		text, err := d.page(p)
		if err != nil {
			return nil, fmt.Errorf("pdftext: page %d: %w", len(out)+1, err)
		}
		out = append(out, text)
	}
	return out, nil
}

// Text is every page's text, the pages one after another.
func Text(pdf []byte) (string, error) {
	pages, err := Pages(pdf)
	return strings.Join(pages, "\n"), err
}

func (d *document) parse(pdf []byte) error {
	locs := objStart.FindAllSubmatchIndex(pdf, -1)
	for i, l := range locs {
		n, _ := strconv.Atoi(string(pdf[l[2]:l[3]]))
		end := len(pdf)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		body := pdf[l[1]:end]
		o := object{}
		if s := bytes.Index(body, []byte("stream")); s >= 0 && bytes.Contains(body[:s], []byte("<<")) && !bytes.Contains(body[:s], []byte("endobj")) {
			o.dict = string(body[:s])
			start := s + len("stream")
			if bytes.HasPrefix(body[start:], []byte("\r\n")) {
				start += 2
			} else if start < len(body) && body[start] == '\n' {
				start++
			}
			length, ok := d.lengthOf(o.dict)
			if !ok || start+length > len(body) {
				return fmt.Errorf("pdftext: object %d: a stream without a direct length that fits", n)
			}
			o.stream = body[start : start+length]
		} else {
			e := bytes.Index(body, []byte("endobj"))
			if e < 0 {
				return fmt.Errorf("pdftext: object %d does not end", n)
			}
			o.dict = string(body[:e])
		}
		d.objects[n] = o
	}
	if len(d.objects) == 0 {
		return errors.New("pdftext: no objects")
	}
	return nil
}

var (
	lengthKey = regexp.MustCompile(`/Length\s+(\d+)(\s+0\s+R)?`)
	fontRef   = regexp.MustCompile(`/([A-Za-z0-9_.+-]+)\s+(\d+)\s+0\s+R`)
)

func (d *document) lengthOf(dict string) (int, bool) {
	m := lengthKey.FindStringSubmatch(dict)
	if m == nil || m[2] != "" {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	return n, err == nil
}

func (d *document) find(marker string) (int, bool) {
	for n, o := range d.objects {
		if strings.Contains(o.dict, marker) {
			return n, true
		}
	}
	return 0, false
}

// refOf is the object a key of dict refers to.
func (d *document) refOf(dict, key string) (int, bool) {
	i := strings.Index(dict, key)
	if i < 0 {
		return 0, false
	}
	m := ref.FindStringSubmatch(strings.TrimLeft(dict[i+len(key):], " \r\n\t"))
	if m == nil {
		return 0, false
	}
	n, _ := strconv.Atoi(m[1])
	return n, true
}

// value is what follows key in dict: a dictionary or an array, balanced, or the referenced object's text.
func (d *document) value(dict, key string) (string, bool) {
	i := strings.Index(dict, key)
	if i < 0 {
		return "", false
	}
	rest := strings.TrimLeft(dict[i+len(key):], " \r\n\t")
	if m := ref.FindStringSubmatch(rest); m != nil {
		n, _ := strconv.Atoi(m[1])
		o, ok := d.objects[n]
		return o.dict, ok
	}
	open, closing := "", ""
	switch {
	case strings.HasPrefix(rest, "<<"):
		open, closing = "<<", ">>"
	case strings.HasPrefix(rest, "["):
		open, closing = "[", "]"
	default:
		return "", false
	}
	depth := 0
	for j := 0; j < len(rest); {
		switch {
		case strings.HasPrefix(rest[j:], open):
			depth++
			j += len(open)
		case strings.HasPrefix(rest[j:], closing):
			depth--
			j += len(closing)
			if depth == 0 {
				return rest[:j], true
			}
		default:
			j++
		}
	}
	return "", false
}

func (d *document) walk(n int, out *[]int, depth int) error {
	if depth > 32 {
		return errors.New("pdftext: the page tree is too deep")
	}
	o, ok := d.objects[n]
	if !ok {
		return fmt.Errorf("pdftext: no object %d in the page tree", n)
	}
	if strings.Contains(o.dict, "/Type/Page") && !strings.Contains(o.dict, "/Type/Pages") {
		*out = append(*out, n)
		return nil
	}
	kids, ok := d.value(o.dict, "/Kids")
	if !ok {
		return fmt.Errorf("pdftext: page tree node %d has no kids", n)
	}
	for _, m := range refs.FindAllStringSubmatch(kids, -1) {
		k, _ := strconv.Atoi(m[1])
		if err := d.walk(k, out, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func (d *document) inflate(o object) ([]byte, error) {
	if !strings.Contains(o.dict, "/FlateDecode") {
		return o.stream, nil
	}
	r, err := zlib.NewReader(bytes.NewReader(o.stream))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	b, err := io.ReadAll(io.LimitReader(r, maxStream+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxStream {
		return nil, errors.New("a stream inflates beyond the limit")
	}
	return b, nil
}

// fonts are the page's font resource names with their ToUnicode maps.
func (d *document) fonts(page string) (map[string]map[uint16]string, error) {
	res, ok := d.value(page, "/Resources")
	if !ok {
		return nil, errors.New("no resources")
	}
	out := map[string]map[uint16]string{}
	fontDict, ok := d.value(res, "/Font")
	if !ok {
		return out, nil
	}
	for _, m := range fontRef.FindAllStringSubmatch(fontDict, -1) {
		n, _ := strconv.Atoi(m[2])
		cm, ok := d.refOf(d.objects[n].dict, "/ToUnicode")
		if !ok {
			return nil, fmt.Errorf("font %s has no ToUnicode map", m[1])
		}
		mapping, err := d.cmap(cm)
		if err != nil {
			return nil, fmt.Errorf("font %s: %w", m[1], err)
		}
		out[m[1]] = mapping
	}
	return out, nil
}

var (
	bfchar  = regexp.MustCompile(`(?s)beginbfchar(.*?)endbfchar`)
	bfrange = regexp.MustCompile(`(?s)beginbfrange(.*?)endbfrange`)
	hexes   = regexp.MustCompile(`<([0-9A-Fa-f]*)>|\[([^\]]*)\]`)
)

func (d *document) cmap(n int) (map[uint16]string, error) {
	if m, ok := d.cmaps[n]; ok {
		return m, nil
	}
	body, err := d.inflate(d.objects[n])
	if err != nil {
		return nil, err
	}
	m := map[uint16]string{}
	for _, block := range bfchar.FindAllSubmatch(body, -1) {
		hs := hexes.FindAllSubmatch(block[1], -1)
		for i := 0; i+1 < len(hs); i += 2 {
			m[code(hs[i][1])] = unicodeOf(hs[i+1][1])
		}
	}
	for _, block := range bfrange.FindAllSubmatch(body, -1) {
		hs := hexes.FindAllSubmatch(block[1], -1)
		for i := 0; i+2 < len(hs); i += 3 {
			lo, hi := code(hs[i][1]), code(hs[i+1][1])
			if hs[i+2][2] != nil { // an array: one destination per code
				for j, h := range hexes.FindAllSubmatch(hs[i+2][2], -1) {
					m[lo+uint16(j)] = unicodeOf(h[1]) // #nosec G115 -- a CMap range holds at most 256 codes
				}
				continue
			}
			base := []rune(unicodeOf(hs[i+2][1]))
			for c := lo; c >= lo && c <= hi && len(base) > 0; c++ {
				r := append([]rune{}, base...)
				r[len(r)-1] += rune(c - lo)
				m[c] = string(r)
				if c == 0xffff {
					break
				}
			}
		}
	}
	d.cmaps[n] = m
	return m, nil
}

func code(h []byte) uint16 {
	v, _ := strconv.ParseUint(string(h), 16, 32)
	return uint16(v) // #nosec G115 -- Identity-H codes are two bytes
}

// unicodeOf is a ToUnicode destination, UTF-16BE in hex.
func unicodeOf(h []byte) string {
	var units []uint16
	for i := 0; i+4 <= len(h); i += 4 {
		v, _ := strconv.ParseUint(string(h[i:i+4]), 16, 16)
		units = append(units, uint16(v))
	}
	return string(utf16.Decode(units))
}

func (d *document) page(n int) (string, error) {
	p := d.objects[n].dict
	fonts, err := d.fonts(p)
	if err != nil {
		return "", err
	}
	contents, ok := d.value(p, "/Contents")
	var ids []int
	if ok && strings.HasPrefix(contents, "[") {
		for _, m := range refs.FindAllStringSubmatch(contents, -1) {
			k, _ := strconv.Atoi(m[1])
			ids = append(ids, k)
		}
	} else if k, ok := d.refOf(p, "/Contents"); ok {
		ids = []int{k}
	}
	var stream []byte
	for _, k := range ids {
		b, err := d.inflate(d.objects[k])
		if err != nil {
			return "", err
		}
		stream = append(append(stream, b...), '\n')
	}
	return show(stream, fonts)
}

// show runs the content stream's text operators: each BT … ET block is a line, its strings decoded through the font in
// use; a gap in a TJ array wider than a third of the font's space is a space.
func show(stream []byte, fonts map[string]map[uint16]string) (string, error) {
	var lines []string
	var line strings.Builder
	var font map[uint16]string
	var operands []token
	emit := func(s []byte) {
		for i := 0; i+1 < len(s); i += 2 {
			line.WriteString(font[uint16(s[i])<<8|uint16(s[i+1])])
		}
	}
	t := tokenizer{b: stream}
	for {
		tok, err := t.next()
		if err != nil {
			return "", err
		}
		if tok.kind == tEOF {
			break
		}
		if tok.kind != tOperator {
			operands = append(operands, tok)
			continue
		}
		switch tok.text {
		case "BT":
			line.Reset()
		case "ET":
			if s := strings.TrimSpace(line.String()); s != "" {
				lines = append(lines, s)
			}
		case "Tf":
			if len(operands) >= 2 && operands[len(operands)-2].kind == tName {
				font = fonts[operands[len(operands)-2].text]
			}
		case "Tj", "'", `"`:
			if len(operands) > 0 && operands[len(operands)-1].kind == tString {
				emit(operands[len(operands)-1].data)
			}
		case "TJ":
			if len(operands) > 0 && operands[len(operands)-1].kind == tArray {
				for _, el := range operands[len(operands)-1].items {
					switch {
					case el.kind == tString:
						emit(el.data)
					case el.kind == tNumber && el.num < -250:
						line.WriteByte(' ')
					}
				}
			}
		}
		operands = operands[:0]
	}
	return strings.Join(lines, "\n"), nil
}

type kind int

const (
	tEOF kind = iota
	tNumber
	tString
	tName
	tArray
	tOperator
	tOther
)

type token struct {
	kind  kind
	text  string
	data  []byte
	num   float64
	items []token
}

type tokenizer struct {
	b []byte
	i int
}

func (t *tokenizer) next() (token, error) {
	for t.i < len(t.b) && strings.IndexByte(" \t\r\n\f\x00", t.b[t.i]) >= 0 {
		t.i++
	}
	if t.i >= len(t.b) {
		return token{kind: tEOF}, nil
	}
	c := t.b[t.i]
	switch {
	case c == '%':
		for t.i < len(t.b) && t.b[t.i] != '\n' && t.b[t.i] != '\r' {
			t.i++
		}
		return t.next()
	case c == '(':
		return t.literal()
	case c == '<' && t.i+1 < len(t.b) && t.b[t.i+1] == '<':
		t.i += 2
		depth := 1
		for t.i+1 < len(t.b) && depth > 0 {
			switch {
			case t.b[t.i] == '<' && t.b[t.i+1] == '<':
				depth++
				t.i += 2
			case t.b[t.i] == '>' && t.b[t.i+1] == '>':
				depth--
				t.i += 2
			default:
				t.i++
			}
		}
		return token{kind: tOther}, nil
	case c == '<':
		end := bytes.IndexByte(t.b[t.i:], '>')
		if end < 0 {
			return token{}, errors.New("a hex string does not end")
		}
		h := bytes.Map(func(r rune) rune {
			if strings.ContainsRune(" \t\r\n", r) {
				return -1
			}
			return r
		}, t.b[t.i+1:t.i+end])
		if len(h)%2 == 1 {
			h = append(h, '0')
		}
		data := make([]byte, len(h)/2)
		for k := range data {
			v, err := strconv.ParseUint(string(h[2*k:2*k+2]), 16, 8)
			if err != nil {
				return token{}, errors.New("a hex string holds a non-hex digit")
			}
			data[k] = byte(v)
		}
		t.i += end + 1
		return token{kind: tString, data: data}, nil
	case c == '[':
		t.i++
		var items []token
		for {
			el, err := t.next()
			if err != nil {
				return token{}, err
			}
			if el.kind == tEOF {
				return token{}, errors.New("an array does not end")
			}
			if el.kind == tOperator && el.text == "]" {
				return token{kind: tArray, items: items}, nil
			}
			items = append(items, el)
		}
	case c == ']':
		t.i++
		return token{kind: tOperator, text: "]"}, nil
	case c == '/':
		start := t.i + 1
		t.i++
		for t.i < len(t.b) && !delimiter(t.b[t.i]) {
			t.i++
		}
		return token{kind: tName, text: string(t.b[start:t.i])}, nil
	}
	start := t.i
	for t.i < len(t.b) && !delimiter(t.b[t.i]) {
		t.i++
	}
	if t.i == start {
		t.i++
		return token{kind: tOther}, nil
	}
	word := string(t.b[start:t.i])
	if f, err := strconv.ParseFloat(word, 64); err == nil {
		return token{kind: tNumber, num: f}, nil
	}
	return token{kind: tOperator, text: word}, nil
}

func delimiter(c byte) bool { return strings.IndexByte(" \t\r\n\f\x00()<>[]{}/%", c) >= 0 }

// literal reads a (string) with its escapes, nested parentheses included.
func (t *tokenizer) literal() (token, error) {
	t.i++
	var out []byte
	depth := 1
	for t.i < len(t.b) {
		c := t.b[t.i]
		switch {
		case c == '\\' && t.i+1 < len(t.b):
			t.i++
			e := t.b[t.i]
			switch e {
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 't':
				out = append(out, '\t')
			case 'b':
				out = append(out, '\b')
			case 'f':
				out = append(out, '\f')
			case '\r', '\n': // a line continuation
			default:
				if e >= '0' && e <= '7' {
					v := 0
					for k := 0; k < 3 && t.i < len(t.b) && t.b[t.i] >= '0' && t.b[t.i] <= '7'; k++ {
						v = v*8 + int(t.b[t.i]-'0')
						t.i++
					}
					out = append(out, byte(v)) // #nosec G115 -- three octal digits are at most 511; PDF keeps the low byte
					continue
				}
				out = append(out, e)
			}
			t.i++
		case c == '(':
			depth++
			out = append(out, c)
			t.i++
		case c == ')':
			depth--
			t.i++
			if depth == 0 {
				return token{kind: tString, data: out}, nil
			}
			out = append(out, c)
		default:
			out = append(out, c)
			t.i++
		}
	}
	return token{}, errors.New("a string does not end")
}
