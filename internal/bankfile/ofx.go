package bankfile

import (
	"strings"
	"time"
)

// OFX and QFX (OFX 1.x is SGML with unclosed leaf tags; 2.x is XML): a bank statement (STMTRS) or a card statement
// (CCSTMTRS) per account, with the currency (CURDEF), the account (ACCTID), the period (DTSTART, DTEND), the lines
// (STMTTRN) and the ledger balance (LEDGERBAL), which is the closing balance; OFX gives no opening balance. The reader
// tokenises the tags itself, so the SGML and the XML forms read alike and nothing outside the file is resolved.

type ofxNode struct {
	name     string
	value    string
	children []*ofxNode
	parent   *ofxNode
}

func (n *ofxNode) child(name string) *ofxNode {
	for _, c := range n.children {
		if c.name == name {
			return c
		}
	}
	return nil
}

func (n *ofxNode) val(path ...string) string {
	cur := n
	for _, p := range path {
		if cur = cur.child(p); cur == nil {
			return ""
		}
	}
	return cur.value
}

func (n *ofxNode) all(name string, out *[]*ofxNode) {
	for _, c := range n.children {
		if c.name == name {
			*out = append(*out, c)
		}
		c.all(name, out)
	}
}

// ofxTree reads the tags after the header into a tree: a tag followed by text is a leaf (closed or not); a tag
// followed by another tag opens an aggregate, closed by its end tag.
func ofxTree(data []byte, lim Limits) (*ofxNode, error) {
	s := string(data)
	i := strings.Index(asciiUpper(s), "<OFX>")
	if i < 0 {
		return nil, refuse(CodeFormat, 0, "", "Export the statement as OFX or QFX from the bank.", "the file has no <OFX> element")
	}
	s = s[i:]
	root := &ofxNode{name: "#root"}
	cur := root
	tags := 0
	for len(s) > 0 {
		lt := strings.IndexByte(s, '<')
		if lt < 0 {
			break
		}
		gt := strings.IndexByte(s[lt:], '>')
		if gt < 0 {
			return nil, refuse(CodeFormat, 0, "", "Export the statement as OFX or QFX from the bank.", "a tag is not closed")
		}
		tag := strings.TrimSpace(s[lt+1 : lt+gt])
		s = s[lt+gt+1:]
		if tags++; tags > lim.MaxLines*40 {
			return nil, refuse(CodeTooLarge, 0, "", "Split the statement by month.", "the file has too many elements")
		}
		if tag == "" || strings.HasPrefix(tag, "?") || strings.HasPrefix(tag, "!") {
			continue // an empty tag, the XML declaration, processing instructions, comments
		}
		if strings.HasPrefix(tag, "/") {
			name := asciiUpper(strings.TrimSpace(tag[1:]))
			for n := cur; n != nil; n = n.parent {
				if n.name == name {
					cur = n.parent
					break
				}
			}
			if cur == nil {
				cur = root
			}
			continue
		}
		name := asciiUpper(strings.Fields(tag + " ")[0])
		next := strings.IndexByte(s, '<')
		body := s
		if next >= 0 {
			body = s[:next]
		}
		node := &ofxNode{name: name, parent: cur}
		cur.children = append(cur.children, node)
		if v := strings.TrimSpace(body); v != "" {
			node.value = v // a leaf; its end tag, if any, closes nothing
			continue
		}
		cur = node
	}
	return root, nil
}

// asciiUpper upper-cases ASCII letters only, byte for byte, so indexes into it are indexes into s (strings.ToUpper
// changes the length of invalid UTF-8).
func asciiUpper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 'a' + 'A'
		}
	}
	return string(b)
}

// ofxDate reads YYYYMMDD[HHMMSS[.XXX]][[+-]H[:TZ]]: the date part only, as the bank booked it.
func ofxDate(s string, line int, field string) (time.Time, error) {
	v := strings.TrimSpace(s)
	if len(v) >= 8 {
		v = v[:8]
	}
	return date(v, []string{"20060102"}, line, field)
}

func parseOFX(data []byte, lim Limits) ([]Statement, error) {
	root, err := ofxTree(data, lim)
	if err != nil {
		return nil, err
	}
	var rs []*ofxNode
	root.all("STMTRS", &rs)
	root.all("CCSTMTRS", &rs)
	if len(rs) == 0 {
		return nil, refuse(CodeFormat, 0, "STMTRS", "Export a bank or card statement as OFX or QFX.", "the file has no statement")
	}
	var out []Statement
	lines := 0
	for si, r := range rs {
		cur, err := currency(r.val("CURDEF"), si+1, "CURDEF")
		if err != nil {
			return nil, err
		}
		s := Statement{Currency: cur}
		acct := r.val("BANKACCTFROM", "ACCTID")
		if acct == "" {
			acct = r.val("CCACCTFROM", "ACCTID")
		}
		if s.Account, err = text(acct, si+1, "ACCTID", lim); err != nil {
			return nil, err
		}
		list := r.child("BANKTRANLIST")
		if list != nil {
			if v := list.val("DTSTART"); v != "" {
				if s.From, err = ofxDate(v, si+1, "DTSTART"); err != nil {
					return nil, err
				}
			}
			if v := list.val("DTEND"); v != "" {
				if s.To, err = ofxDate(v, si+1, "DTEND"); err != nil {
					return nil, err
				}
			}
			for ti, t := range list.children {
				if t.name != "STMTTRN" {
					continue
				}
				lines++
				if lines > lim.MaxLines {
					return nil, refuse(CodeTooLarge, ti+1, "STMTTRN", "Split the statement by month.", "the file has more than %d lines", lim.MaxLines)
				}
				amt, err := amount(t.val("TRNAMT"), Number{}, cur, ti+1, "TRNAMT")
				if err != nil {
					return nil, err
				}
				booked, err := ofxDate(t.val("DTPOSTED"), ti+1, "DTPOSTED")
				if err != nil {
					return nil, err
				}
				l := Line{Position: len(s.Lines) + 1, Booked: booked, Amount: amt}
				if v := t.val("DTUSER"); v != "" {
					if u, err := ofxDate(v, ti+1, "DTUSER"); err == nil && !u.Equal(booked) {
						l.Value = &u
					}
				}
				if l.Reference, err = text(t.val("FITID"), ti+1, "FITID", lim); err != nil {
					return nil, err
				}
				if l.Counterparty, err = text(t.val("NAME"), ti+1, "NAME", lim); err != nil {
					return nil, err
				}
				if l.Description, err = text(strings.TrimSpace(t.val("MEMO")+" "+t.val("CHECKNUM")), ti+1, "MEMO", lim); err != nil {
					return nil, err
				}
				s.Lines = append(s.Lines, l)
			}
		}
		if bal := r.child("LEDGERBAL"); bal != nil && bal.val("BALAMT") != "" {
			m, err := amount(bal.val("BALAMT"), Number{}, cur, si+1, "LEDGERBAL")
			if err != nil {
				return nil, err
			}
			s.Closing = &m
		}
		if s.From.IsZero() || s.To.IsZero() {
			span(&s)
		}
		out = append(out, s)
	}
	return out, nil
}
