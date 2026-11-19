// Package knowledge is the knowledge base's own logic (PRD-001 §9.11, AI-50, AI-52, AI-53; p1-knowledge-base design D1):
// the page format of kb/schema.md (kb-schema@1), its front matter, the deterministic lint, the generated index, and the
// reader filter's vocabulary. It reads pages and their raw sources from file systems and touches no database and no
// network. scripts/kb.py does the same for the repository's pages, and the two agree on the shared fixtures in
// test/kb/fixtures/ (a rule changed in one fails the other).
package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

// Schema is the page format this release reads and writes (kb/schema.md).
const Schema = "kb-schema@1"

// The page types, data classes and statuses of kb/schema.md.
var (
	Types       = []string{"concept", "task", "reference", "policy", "decision"}
	DataClasses = []string{"general", "personal", "payroll", "equity", "identity"}
)

// Page is one compiled page: its front matter and its Markdown body.
type Page struct {
	File        string // path under kb/, such as pages/task/sales-invoices.md
	Key         string
	Type        string
	Title       string
	Summary     string
	Aliases     []string
	Sources     []Source
	DataClass   string
	Taint       string
	Status      string
	HumanEdited bool
	CompiledBy  string
	Schema      string
	CompiledAt  string
	Body        string
	Digest      string // the SHA-256 of the file as written: with its release, the version an answer used

	front frontMatter
	err   []string // problems reading the file
}

// Source is one raw source a page cites, with the SHA-256 it was compiled from.
type Source struct {
	Path   string
	SHA256 string // empty when the line is not "<path> sha256:<64 hex>"
}

// Draft is whether the page is a draft.
func (p Page) Draft() bool { return p.Status == "draft" }

type frontMatter struct {
	scalars map[string]string
	lists   map[string][]string
}

func (f frontMatter) has(k string) bool {
	if v, ok := f.scalars[k]; ok {
		return v != ""
	}
	l, ok := f.lists[k]
	return ok && len(l) > 0
}

func (f frontMatter) empty() bool { return len(f.scalars) == 0 && len(f.lists) == 0 }

var (
	commentTail  = regexp.MustCompile(`\s+#.*$`)
	frontLine    = regexp.MustCompile(`^([a-z_]+):\s*(.*)$`)
	sourceLine   = regexp.MustCompile(`^(\S+)\s+sha256:([0-9a-f]{64})$`)
	headingLine  = regexp.MustCompile(`^#{1,6}\s+(.*?)\s*#*\s*$`)
	slugStrip    = regexp.MustCompile(`[^\p{L}\p{N}_\- ]`)
	citeRe       = regexp.MustCompile(`\(src: ([^)#\s]+)#([^)\s]+)\)`)
	wikilinkRe   = regexp.MustCompile(`\[\[([a-z0-9.\-]+)\]\]`)
	idRe         = regexp.MustCompile(`\b[A-Z][A-Z0-9]*(?:-[A-Z0-9]+)*-\d+[a-z]?\b|\bP\d\b|\b[A-Z]{2,}\d+\b`)
	numRe        = regexp.MustCompile(`(?:S\$|US\$)?\d[\d,]*(?:\.\d+)?(?:%|k\b|m\b)?`)
	inlineCode   = regexp.MustCompile("`[^`]*`")
	linkTarget   = regexp.MustCompile(`\]\([^)]*\)`)
	orderedItem  = regexp.MustCompile(`(?m)^\s*\d+\.\s`)
	sectionSplit = regexp.MustCompile(`(?m)^## `)
	dateRe       = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

// ParseFrontMatter reads the restricted front matter of kb/schema.md: "key: value" lines and lists of "  - item"
// lines. It returns ok false when the text has none, and an error for a line it cannot read.
func parseFrontMatter(text string) (fm frontMatter, body string, ok bool, err error) {
	if !strings.HasPrefix(text, "---\n") {
		return frontMatter{}, text, false, nil
	}
	end := strings.Index(text[4:], "\n---\n")
	if end < 0 {
		return frontMatter{}, text, false, nil
	}
	end += 4
	fm = frontMatter{scalars: map[string]string{}, lists: map[string][]string{}}
	current := ""
	for _, line := range strings.Split(text[4:end], "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimLeft(line, " \t"), "#") {
			continue
		}
		if strings.HasPrefix(line, "  - ") && current != "" {
			fm.lists[current] = append(fm.lists[current], strings.TrimSpace(commentTail.ReplaceAllString(line[4:], "")))
			continue
		}
		m := frontLine.FindStringSubmatch(line)
		if m == nil {
			return fm, text[end+5:], true, fmt.Errorf("unreadable front-matter line: %q", line)
		}
		value := strings.TrimSpace(commentTail.ReplaceAllString(m[2], ""))
		if value == "" {
			fm.lists[m[1]], current = []string{}, m[1]
		} else {
			fm.scalars[m[1]], current = strings.Trim(value, `"`), ""
		}
	}
	return fm, text[end+5:], true, nil
}

// ParsePage reads one page file's text. A page that cannot be read still comes back, with the problems Lint reports.
func ParsePage(file, text string) Page {
	sum := sha256.Sum256([]byte(text))
	p := Page{File: file, Digest: hex.EncodeToString(sum[:])}
	fm, body, ok, err := parseFrontMatter(text)
	switch {
	case err != nil:
		p.err = append(p.err, err.Error())
	case !ok:
		p.err = append(p.err, "missing front matter")
	}
	if !ok || err != nil {
		return p
	}
	p.front, p.Body = fm, body
	s := fm.scalars
	p.Key, p.Type, p.Title, p.Summary = s["key"], s["type"], s["title"], s["summary"]
	p.DataClass, p.Taint, p.Status = s["data_class"], s["taint"], s["status"]
	p.HumanEdited = s["human_edited"] == "true"
	p.CompiledBy, p.Schema, p.CompiledAt = s["compiled_by"], s["schema"], s["compiled_at"]
	p.Aliases = fm.lists["aliases"]
	for _, item := range fm.lists["sources"] {
		if m := sourceLine.FindStringSubmatch(item); m != nil {
			p.Sources = append(p.Sources, Source{Path: m[1], SHA256: m[2]})
		} else {
			p.Sources = append(p.Sources, Source{Path: item})
		}
	}
	return p
}

// Slug is a heading's GitHub-style anchor.
func Slug(heading string) string {
	s := strings.ToLower(strings.TrimSpace(heading))
	return strings.ReplaceAll(slugStrip.ReplaceAllString(s, ""), " ", "-")
}

// Anchors are the anchors of a Markdown text's headings, outside fenced code, as GitHub numbers repeats.
func Anchors(text string) map[string]bool {
	seen, out, fenced := map[string]int{}, map[string]bool{}, false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		if m := headingLine.FindStringSubmatch(line); m != nil {
			slug := Slug(m[1])
			n := seen[slug]
			seen[slug] = n + 1
			if n == 0 {
				out[slug] = true
			} else {
				out[fmt.Sprintf("%s-%d", slug, n)] = true
			}
		}
	}
	return out
}

// Section is one H2 section of a page body: its heading, its anchor and its text.
type Section struct {
	Heading string
	Anchor  string
	Text    string
}

// Sections are a body's H2 sections, in order.
func Sections(body string) []Section {
	parts := sectionSplit.Split(body, -1)
	var out []Section
	for _, part := range parts[1:] {
		heading, _, _ := strings.Cut(part, "\n")
		heading = strings.TrimSpace(heading)
		out = append(out, Section{Heading: heading, Anchor: Slug(heading), Text: part})
	}
	return out
}

// Citation is a "(src: path#anchor)" in a page body.
type Citation struct{ Path, Anchor string }

// Citations are the citations in a body, in order.
func Citations(body string) []Citation {
	var out []Citation
	for _, m := range citeRe.FindAllStringSubmatch(body, -1) {
		out = append(out, Citation{Path: m[1], Anchor: m[2]})
	}
	return out
}

// Links are the [[key]] links in a body.
func Links(body string) []string {
	var out []string
	for _, m := range wikilinkRe.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

// NumbersOutsideIDs are the numbers a body states, leaving out code spans, citations, links, list markers and
// requirement and rule IDs (AI-53): each must appear in a cited source.
func NumbersOutsideIDs(body string) []string {
	text := inlineCode.ReplaceAllString(body, " ")
	text = citeRe.ReplaceAllString(text, " ")
	text = wikilinkRe.ReplaceAllString(text, " ")
	text = linkTarget.ReplaceAllString(text, "]")
	text = orderedItem.ReplaceAllString(text, " ")
	text = idRe.ReplaceAllString(text, " ")
	var out []string
	for _, t := range numRe.FindAllString(text, -1) {
		if strings.ContainsAny(t, "0123456789") {
			out = append(out, t)
		}
	}
	return out
}

// SearchText is what a page's body contributes to full-text search: the words, without the citations and links that every
// page carries ("(src: docs/user-guide/x.md#y)", "[[key]]"), which would match every query for "guide" or "docs".
func SearchText(body string) string {
	return wikilinkRe.ReplaceAllString(citeRe.ReplaceAllString(body, " "), " ")
}
