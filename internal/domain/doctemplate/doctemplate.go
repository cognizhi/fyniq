// Package doctemplate holds document and email templates as data (PRD-001 §8.25; p1-templates-email task 1.1): the
// content a template version is, the typed field catalogue its texts draw on (FR-TPL-02), the IRAS particulars a tax
// invoice's or credit note's template keeps (FR-TPL-03), and the rules every version meets before it is saved. The
// render service maps a version to Typst sources it owns, so a template holds no code (design: "the template is data,
// the renderer is fixed"). Saving makes a draft; publishing makes an immutable numbered version under approval
// (FR-TPL-04), which the API and the approval engine carry out.
package doctemplate

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/cognizhi/fyniq/internal/domain/approval"
)

// Schema names the content's format; a change to the format changes it.
const Schema = "fyniq.template/1"

// The stable error codes of a template's rules (ENG-23).
const (
	CodeKind     = "TPL-01" // not a kind or document type templates serve
	CodeName     = "TPL-02" // the name is blank or too long
	CodeSchema   = "TPL-03" // the content is not this format, or has parts its kind does not take
	CodePage     = "TPL-04" // a page size or margin outside the choices
	CodeTheme    = "TPL-05" // a font the render service does not ship, or an accent colour too pale to read
	CodeBlock    = "TPL-06" // a block the document type does not take, or one repeated
	CodeLocked   = "TPL-07" // an IRAS particulars block removed or hidden (FR-TPL-03)
	CodeText     = "TPL-08" // a text the block does not take, or too long
	CodeField    = "TPL-09" // a field not in the catalogue, a format not for its type, or loose braces
	CodeSubject  = "TPL-10" // an email's subject blank, on several lines or too long
	CodeBody     = "TPL-11" // an email's body blank or too long
	CodeUnchange = "TPL-12" // the version to publish is the one in force already
	CodeVersion  = "TPL-13" // no such version of the template to publish again
	CodeApproval = "TPL-14" // publishing needs another person's approval (FR-TPL-04, FR-APR-01)
	CodeOwnDraft = "TPL-15" // whoever saved the draft never approves its publication (SOD-3)
)

// The limits of a template.
const (
	MaxName    = 100
	MaxSubject = 200
	MaxEmail   = 10_000
)

// Content is a template version: a document's page, theme and blocks, or an email's subject and body.
type Content struct {
	Schema  string  `json:"schema"`
	Page    *Page   `json:"page,omitempty"`
	Theme   *Theme  `json:"theme,omitempty"`
	Blocks  []Block `json:"blocks,omitempty"`
	Subject string  `json:"subject,omitempty"`
	Body    string  `json:"body,omitempty"`
}

// Page is a document's paper and margins, in millimetres.
type Page struct {
	Size    string  `json:"size"`
	Margins Margins `json:"margins_mm"`
}

// Margins are a page's margins in whole millimetres.
type Margins struct {
	Top    int `json:"top"`
	Right  int `json:"right"`
	Bottom int `json:"bottom"`
	Left   int `json:"left"`
}

// Theme is a document's look: whether it shows the organisation's logo (FR-ORG-09), its accent colour and font.
type Theme struct {
	Logo   bool   `json:"logo"`
	Accent string `json:"accent"`
	Font   string `json:"font"`
}

// Block is one block of a document: shown or hidden, with its texts.
type Block struct {
	Kind  string            `json:"kind"`
	Shown bool              `json:"shown"`
	Texts map[string]string `json:"texts,omitempty"`
}

// Problem is one rule a template breaks: its field, what is wrong and how to fix it.
type Problem struct {
	Code, Field, Message, Fix string
}

// Known reports whether a kind and document type are ones templates serve.
func Known(kind, documentType string) bool {
	return slices.Contains(Kinds, kind) && slices.Contains(Types, documentType)
}

// CheckName returns the problem with a template's name, or none.
func CheckName(name string) []Problem {
	if n := utf8.RuneCountInString(strings.TrimSpace(name)); n < 1 || n > MaxName {
		return []Problem{{CodeName, "name", fmt.Sprintf("the name is blank or longer than %d characters", MaxName), "Name the template, such as \"Standard invoice\""}}
	}
	return nil
}

// Check returns every rule c breaks as a template of kind for documentType, or none.
func Check(kind, documentType string, c Content) []Problem {
	if !Known(kind, documentType) {
		return []Problem{{CodeKind, "document_type", fmt.Sprintf("%q %q is not a template Fyniq serves", kind, documentType),
			fmt.Sprintf("Use a kind of %s and a document type of %s", strings.Join(Kinds, " or "), strings.Join(Types, ", "))}}
	}
	if c.Schema != Schema {
		return []Problem{{CodeSchema, "content.schema", fmt.Sprintf("%q is not this template format", c.Schema), "Set it to " + Schema}}
	}
	fields := map[string]string{}
	for _, f := range Fields(documentType) {
		fields[f.Path] = f.Type
	}
	if kind == KindEmail {
		return checkEmail(c, fields)
	}
	return checkDocument(documentType, c, fields)
}

func checkEmail(c Content, fields map[string]string) []Problem {
	var out []Problem
	if c.Page != nil || c.Theme != nil || len(c.Blocks) > 0 {
		out = append(out, Problem{CodeSchema, "content", "an email template has no page, theme or blocks", "Give it a subject and a body only"})
	}
	switch s := strings.TrimSpace(c.Subject); {
	case s == "" || utf8.RuneCountInString(c.Subject) > MaxSubject:
		out = append(out, Problem{CodeSubject, "content.subject", fmt.Sprintf("the subject is blank or longer than %d characters", MaxSubject), "Write a subject of one line"})
	case strings.ContainsAny(c.Subject, "\r\n"):
		out = append(out, Problem{CodeSubject, "content.subject", "the subject is on several lines", "Write it on one line"})
	}
	if strings.TrimSpace(c.Body) == "" || utf8.RuneCountInString(c.Body) > MaxEmail {
		out = append(out, Problem{CodeBody, "content.body", fmt.Sprintf("the body is blank or longer than %d characters", MaxEmail), "Write the email's text"})
	}
	out = append(out, checkFields("content.subject", c.Subject, fields)...)
	return append(out, checkFields("content.body", c.Body, fields)...)
}

func checkDocument(documentType string, c Content, fields map[string]string) []Problem {
	var out []Problem
	add := func(code, field, message, fix string) { out = append(out, Problem{code, field, message, fix}) }
	if c.Subject != "" || c.Body != "" {
		add(CodeSchema, "content", "a document template has no subject or body", "Put the texts in the blocks; the email template has the subject and body")
	}
	if c.Page == nil {
		add(CodeSchema, "content.page", "the page is missing", "Give the paper size and margins")
	} else {
		if !slices.Contains(PageSizes, c.Page.Size) {
			add(CodePage, "content.page.size", fmt.Sprintf("%q is not a paper size", c.Page.Size), "Use "+strings.Join(PageSizes, " or "))
		}
		m := c.Page.Margins
		for i, mm := range []int{m.Top, m.Right, m.Bottom, m.Left} {
			if mm < MinMargin || mm > MaxMargin {
				add(CodePage, "content.page.margins_mm."+[]string{"top", "right", "bottom", "left"}[i], fmt.Sprintf("%d mm is outside %d to %d mm", mm, MinMargin, MaxMargin),
					fmt.Sprintf("Set a margin of %d to %d mm", MinMargin, MaxMargin))
			}
		}
	}
	if c.Theme == nil {
		add(CodeSchema, "content.theme", "the theme is missing", "Give the accent colour and font")
	} else {
		if !slices.Contains(Fonts, c.Theme.Font) {
			add(CodeTheme, "content.theme.font", fmt.Sprintf("%q is not a font Fyniq ships", c.Theme.Font), "Use "+strings.Join(Fonts, ", "))
		}
		switch ratio, err := ContrastOnWhite(c.Theme.Accent); {
		case err != nil:
			add(CodeTheme, "content.theme.accent", fmt.Sprintf("%q is not a colour", c.Theme.Accent), "Give it as #RRGGBB, such as #0F766E")
		case ratio < int64(MinOnWhite):
			add(CodeTheme, "content.theme.accent", fmt.Sprintf("%s has a contrast of %d.%d:1 against white, below %d.%d:1, so text in it is hard to read",
				c.Theme.Accent, ratio/10, ratio%10, MinOnWhite/10, MinOnWhite%10), "Choose a darker colour")
		}
	}
	allowed := Blocks(documentType)
	seen := map[string]int{}
	for i, b := range c.Blocks {
		at := fmt.Sprintf("content.blocks[%d]", i)
		if !slices.Contains(allowed, b.Kind) {
			add(CodeBlock, at+".kind", fmt.Sprintf("%q is not a block a %s takes", b.Kind, label(documentType)), "Use one of "+strings.Join(allowed, ", "))
			continue
		}
		seen[b.Kind]++
		switch {
		case b.Kind == BlockText && seen[b.Kind] == MaxTextBlocks+1:
			add(CodeBlock, at, fmt.Sprintf("a template has at most %d free text blocks", MaxTextBlocks), "Put the texts together, or remove one")
		case b.Kind != BlockText && seen[b.Kind] == 2:
			add(CodeBlock, at, fmt.Sprintf("the %s block appears twice", b.Kind), "Keep one")
		}
		if !b.Shown && slices.Contains(Locked[documentType], b.Kind) {
			add(CodeLocked, at+".shown", fmt.Sprintf("the %s block carries particulars IRAS requires on a %s, so it cannot be hidden", b.Kind, label(documentType)), "Show it")
		}
		for _, name := range sortedKeys(b.Texts) {
			text := b.Texts[name]
			tat := at + ".texts." + name
			switch {
			case !slices.Contains(blockTexts[b.Kind], name):
				add(CodeText, tat, fmt.Sprintf("the %s block takes no %s text", b.Kind, name), textsFix(b.Kind))
			case name == TextTitle && LockedTitle(documentType):
				add(CodeLocked, tat, "a tax invoice's title is \"Tax Invoice\", as IRAS requires, so it is not a text", "Remove the title")
			case name == TextBody && utf8.RuneCountInString(text) > MaxBody:
				add(CodeText, tat, fmt.Sprintf("the text is longer than %d characters", MaxBody), "Shorten it")
			case name != TextBody && (utf8.RuneCountInString(text) > MaxLabel || strings.ContainsAny(text, "\r\n")):
				add(CodeText, tat, fmt.Sprintf("the %s is on several lines or longer than %d characters", name, MaxLabel), "Write it on one short line")
			default:
				out = append(out, checkFields(tat, text, fields)...)
			}
		}
	}
	for _, kind := range Locked[documentType] {
		if seen[kind] == 0 {
			add(CodeLocked, "content.blocks", fmt.Sprintf("the %s block carries particulars IRAS requires on a %s, so it cannot be removed", kind, label(documentType)),
				"Add it back, shown")
		}
	}
	return out
}

// reference finds a field reference: {{path}} or {{path|format}}, with spaces allowed inside the braces.
var reference = regexp.MustCompile(`\{\{\s*([a-z_]+\.[a-z_]+)\s*(?:\|\s*([a-z_]+)\s*)?\}\}`)

// checkFields returns the problems with the field references in text: each names a field of the catalogue, with a
// format its type takes; braces that make no reference are refused, so a text never holds anything but words and
// fields (FR-TPL-02's "no free code").
func checkFields(at, text string, fields map[string]string) []Problem {
	var out []Problem
	for _, m := range reference.FindAllStringSubmatch(text, -1) {
		typ, ok := fields[m[1]]
		switch {
		case !ok:
			out = append(out, Problem{CodeField, at, fmt.Sprintf("{{%s}} is not a field of this document", m[1]), "Choose a field from the catalogue"})
		case m[2] != "" && !slices.Contains(Formats[typ], m[2]):
			fix := "Leave out the format: a " + typ + " takes none"
			if len(Formats[typ]) > 0 {
				fix = "Use one of " + strings.Join(Formats[typ], ", ")
			}
			out = append(out, Problem{CodeField, at, fmt.Sprintf("%q is not a format of the %s %s", m[2], typ, m[1]), fix})
		}
	}
	if rest := reference.ReplaceAllString(text, ""); strings.Contains(rest, "{{") || strings.Contains(rest, "}}") {
		out = append(out, Problem{CodeField, at, "the text has braces that name no field", "Write a field as {{customer.name}} or {{document.date|long}}, or remove the braces"})
	}
	return out
}

func textsFix(kind string) string {
	if len(blockTexts[kind]) == 0 {
		return "Remove the text: the block's content comes from the document"
	}
	return "Use " + strings.Join(blockTexts[kind], " or ")
}

func label(documentType string) string { return strings.ReplaceAll(documentType, "_", " ") }

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// Canonical is c as RFC 8785 JSON: the bytes a version is stored and hashed as, so the same content always has the same
// hash.
func Canonical(c Content) ([]byte, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	return approval.Canonical(v)
}

// Hash is the SHA-256 of c's canonical JSON.
func Hash(c Content) ([]byte, error) {
	b, err := Canonical(c)
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(b)
	return h[:], nil
}

// Read reads stored content, refusing a member the format does not have.
func Read(b []byte) (Content, error) {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	var c Content
	if err := d.Decode(&c); err != nil {
		return Content{}, fmt.Errorf("doctemplate: %w", err)
	}
	return c, nil
}
