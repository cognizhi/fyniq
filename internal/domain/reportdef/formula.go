package reportdef

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/cockroachdb/apd/v3"
)

// ctx works the formulas: wide enough that a sum or product of stored amounts is exact, and a quotient is taken to 60 more digits than
// either side, then rounded once by the row's format. No operation traps inexact: only a division can be.
var ctx = func() *apd.Context {
	c := apd.BaseContext.WithPrecision(120)
	c.MaxExponent, c.MinExponent = 1000, -1000
	return c
}()

// expr is a parsed formula.
type expr interface {
	refs() []string
	eval(env func(id string) *apd.Decimal) *apd.Decimal // nil: not available
}

type (
	num struct{ v apd.Decimal }
	ref struct{ id string }
	neg struct{ x expr }
	bin struct {
		op   byte
		l, r expr
	}
)

func (n num) refs() []string                              { return nil }
func (n num) eval(func(string) *apd.Decimal) *apd.Decimal { v := n.v; return &v }
func (r ref) refs() []string                              { return []string{r.id} }
func (r ref) eval(env func(string) *apd.Decimal) *apd.Decimal {
	if v := env(r.id); v != nil {
		c := *v
		return &c
	}
	return nil
}
func (n neg) refs() []string { return n.x.refs() }
func (n neg) eval(env func(string) *apd.Decimal) *apd.Decimal {
	v := n.x.eval(env)
	if v == nil {
		return nil
	}
	var o apd.Decimal
	o.Neg(v)
	return &o
}
func (b bin) refs() []string { return append(b.l.refs(), b.r.refs()...) }
func (b bin) eval(env func(string) *apd.Decimal) *apd.Decimal {
	l, r := b.l.eval(env), b.r.eval(env)
	if l == nil || r == nil {
		return nil
	}
	var o apd.Decimal
	var err error
	switch b.op {
	case '+':
		_, err = ctx.Add(&o, l, r)
	case '-':
		_, err = ctx.Sub(&o, l, r)
	case '*':
		_, err = ctx.Mul(&o, l, r)
	case '/':
		if r.IsZero() {
			return nil
		}
		// A quotient is worked to 60 digits beyond its operands before the one rounding the format makes.
		digits := min(l.NumDigits()+int64(max(0, l.Exponent))+r.NumDigits()+60, 2000)
		_, err = apd.BaseContext.WithPrecision(uint32(digits)).Quo(&o, l, r) // #nosec G115 -- bounded to 2,000 above
	}
	if err != nil {
		return nil
	}
	return &o
}

// parse reads a formula: numbers, row IDs, + - * /, unary minus and brackets.
func parse(s string) (expr, error) {
	p := &parser{s: []rune(strings.TrimSpace(s))}
	if len(p.s) == 0 {
		return nil, fmt.Errorf("the formula is empty")
	}
	e, err := p.sum()
	if err != nil {
		return nil, err
	}
	p.skip()
	if p.i < len(p.s) {
		return nil, fmt.Errorf("unexpected %q at position %d", string(p.s[p.i]), p.i+1)
	}
	return e, nil
}

type parser struct {
	s     []rune
	i     int
	depth int
}

func (p *parser) skip() {
	for p.i < len(p.s) && p.s[p.i] == ' ' {
		p.i++
	}
}

func (p *parser) sum() (expr, error) {
	l, err := p.product()
	if err != nil {
		return nil, err
	}
	for {
		p.skip()
		if p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-') {
			op := byte(p.s[p.i])
			p.i++
			r, err := p.product()
			if err != nil {
				return nil, err
			}
			l = bin{op, l, r}
			continue
		}
		return l, nil
	}
}

func (p *parser) product() (expr, error) {
	l, err := p.unary()
	if err != nil {
		return nil, err
	}
	for {
		p.skip()
		if p.i < len(p.s) && (p.s[p.i] == '*' || p.s[p.i] == '/') {
			op := byte(p.s[p.i])
			p.i++
			r, err := p.unary()
			if err != nil {
				return nil, err
			}
			l = bin{op, l, r}
			continue
		}
		return l, nil
	}
}

func (p *parser) unary() (expr, error) {
	p.skip()
	if p.i < len(p.s) && p.s[p.i] == '-' {
		p.i++
		x, err := p.unary()
		if err != nil {
			return nil, err
		}
		return neg{x}, nil
	}
	return p.atom()
}

func (p *parser) atom() (expr, error) {
	p.skip()
	if p.i >= len(p.s) {
		return nil, fmt.Errorf("the formula ends where a row ID, a number or a bracket is expected")
	}
	c := p.s[p.i]
	switch {
	case c == '(':
		if p.depth++; p.depth > 8 {
			return nil, fmt.Errorf("brackets go deeper than 8 levels")
		}
		p.i++
		e, err := p.sum()
		if err != nil {
			return nil, err
		}
		p.skip()
		if p.i >= len(p.s) || p.s[p.i] != ')' {
			return nil, fmt.Errorf("a bracket opened at position %d is not closed", p.i+1)
		}
		p.i++
		p.depth--
		return e, nil
	case unicode.IsDigit(c):
		j := p.i
		for j < len(p.s) && unicode.IsDigit(p.s[j]) {
			j++
		}
		if j < len(p.s) && p.s[j] == '.' {
			j++
			k := j
			for j < len(p.s) && unicode.IsDigit(p.s[j]) {
				j++
			}
			if j == k {
				return nil, fmt.Errorf("a number at position %d has no digits after its point", p.i+1)
			}
		}
		var n num
		if _, _, err := apd.NewFromString(string(p.s[p.i:j])); err != nil {
			return nil, fmt.Errorf("%q is not a number", string(p.s[p.i:j]))
		}
		d, _, _ := apd.NewFromString(string(p.s[p.i:j]))
		n.v = *d
		p.i = j
		return n, nil
	case c >= 'a' && c <= 'z':
		j := p.i
		for j < len(p.s) && (p.s[j] >= 'a' && p.s[j] <= 'z' || p.s[j] >= '0' && p.s[j] <= '9' || p.s[j] == '_') {
			j++
		}
		id := string(p.s[p.i:j])
		p.i = j
		return ref{id}, nil
	}
	return nil, fmt.Errorf("unexpected %q at position %d", string(c), p.i+1)
}
