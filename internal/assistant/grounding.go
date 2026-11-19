package assistant

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/aitool"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/ports"
)

// Grounding and rendering (PRD-001 AI-08, AI-21, AI-69; p1-ai-assistant design D7): the model answers in segments of
// words and references to facts, and writes no figure itself. Platform code checks each reference against the fact it
// names (the measure, the period, the currency and whose it is: G4-089) and renders the figure from the fact; words
// that hold a figure fail. A failing answer is asked for once more with the failures named; what still fails is not
// shown, and the answer says it could not be established (G4-090).

// Segment is one piece of a structured answer: words, a figure as the fact it is with what the sentence says it is, or
// what could not be established.
type Segment struct {
	Text     string `json:"text,omitempty"`
	Fact     string `json:"fact,omitempty"`
	Measure  string `json:"measure,omitempty"`
	Member   string `json:"member,omitempty"`
	From     string `json:"from,omitempty"`
	To       string `json:"to,omitempty"`
	Currency string `json:"currency,omitempty"`
	Missing  string `json:"missing,omitempty"`
	// A know-how sentence (AI-51, AI-53): Text is its words, Page the key of a knowledge-base page read this turn and Section
	// the section's anchor. Sources are the guide pages the section cites, set by Fyniq and never by the model.
	Page    string   `json:"page,omitempty"`
	Section string   `json:"section,omitempty"`
	Sources []Source `json:"sources,omitempty"`
}

// Structured is the answer as the model gives it.
type Structured struct {
	Segments []Segment `json:"segments"`
}

// answerSchema is the structured output the model answers in.
var answerSchema = json.RawMessage(`{"type":"object","additionalProperties":false,"required":["segments"],"properties":{"segments":{` +
	`"type":"array","description":"The answer, in order: words in text segments, never a figure; each figure as a fact segment; what could not be established as a missing segment.",` +
	`"items":{"type":"object","additionalProperties":false,"properties":{` +
	`"text":{"type":"string","description":"Words, in Markdown: no figures, except dates and ordinals."},` +
	`"fact":{"type":"string","description":"A figure: the ID of the fact a tool returned in this conversation, or calc's."},` +
	`"measure":{"type":"string","description":"With fact: what the sentence says the figure is, the fact's measure."},` +
	`"member":{"type":"string","description":"With fact: whose figure it is, the fact's member, when it has one."},` +
	`"from":{"type":"string","description":"With fact: the period's first day the sentence gives it, YYYY-MM-DD."},` +
	`"to":{"type":"string","description":"With fact: the period's last day, YYYY-MM-DD."},` +
	`"currency":{"type":"string","description":"With fact: the currency the sentence gives an amount."},` +
	`"missing":{"type":"string","description":"What could not be established, because no tool gave it or a tool failed."},` +
	`"page":{"type":"string","description":"A know-how sentence, in a text segment: the key of the knowledge-base page you read with kb_read in this conversation. Its numbers must be the section's own."},` +
	`"section":{"type":"string","description":"With page: the anchor of the section of that page the sentence comes from, as kb_read returned it."}}}}}}`)

// answerOutput is the strict output the model calls ask for (AI-27).
var answerOutput = &ports.ModelSchema{Name: "answer", Schema: answerSchema}

// failures are the reasons a structured answer is not grounded, each in words the model can act on.
func failures(s Structured, known func(string) (aitool.Fact, bool), pg pages) []string {
	var out []string
	if len(s.Segments) == 0 {
		return []string{"the answer has no segments"}
	}
	for _, g := range s.Segments {
		switch {
		case g.Fact != "":
			out = append(out, attribution(g, known)...)
		case g.Page != "":
			out = append(out, pg.knowHow(g)...)
		case g.Text != "":
			if n := stray(g.Text); n != "" {
				out = append(out, fmt.Sprintf("the words %q hold the figure %s: give every figure as a fact segment", clip(g.Text), n))
			}
		}
	}
	return out
}

// attribution checks what a sentence says a figure is against the fact it cites (G4-089).
func attribution(g Segment, known func(string) (aitool.Fact, bool)) []string {
	f, ok := known(g.Fact)
	if !ok {
		return []string{g.Fact + " is not a fact a tool returned in this conversation: read the figure, or say it could not be established"}
	}
	var out []string
	bad := func(what, said, is string) {
		out = append(out, fmt.Sprintf("%s's %s is %q, not %q", g.Fact, what, is, said))
	}
	if g.Measure != f.Measure {
		bad("measure", g.Measure, f.Measure)
	}
	if f.Member != "" && g.Member != f.Member && g.Member != f.MemberName {
		bad("member", g.Member, f.Member)
	}
	if f.From != "" && g.From != f.From {
		bad("first day", g.From, f.From)
	}
	if f.To != "" && g.To != f.To {
		bad("last day", g.To, f.To)
	}
	if f.Currency != "" && g.Currency != f.Currency {
		bad("currency", g.Currency, f.Currency)
	}
	return out
}

var (
	// allowedNumbers are the numbers words may hold: dates and ordinals.
	allowedNumbers = regexp.MustCompile(`(?i)\b\d{4}-\d{2}-\d{2}\b|\b\d{1,2}\s+(?:jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*\.?\s+\d{4}\b|` +
		`\b(?:jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*\.?\s+\d{4}\b|\b(?:q[1-4]|fy|ya)\s?\d{2,4}\b|\b\d{1,3}(?:st|nd|rd|th)\b`)
	digits = regexp.MustCompile(`[0-9][0-9.,]*`)
)

// stray is the first figure in words that is not a date or an ordinal, or "".
func stray(s string) string {
	return digits.FindString(allowedNumbers.ReplaceAllString(s, ""))
}

func clip(s string) string {
	if len(s) > 60 {
		return s[:57] + "..."
	}
	return s
}

// withhold replaces what still fails with what could not be established: an unknown or misattributed figure, and words
// holding a figure.
func withhold(s Structured, known func(string) (aitool.Fact, bool), pg pages) (Structured, int) {
	out := Structured{Segments: make([]Segment, 0, len(s.Segments))}
	n := 0
	for _, g := range s.Segments {
		switch {
		case g.Fact != "" && len(attribution(g, known)) > 0:
			out.Segments, n = append(out.Segments, Segment{Missing: "this figure"}), n+1
		case g.Page != "" && len(pg.knowHow(g)) > 0:
			out.Segments, n = append(out.Segments, Segment{Missing: "this part of the guide"}), n+1
		case g.Text != "" && g.Page == "" && stray(g.Text) != "":
			out.Segments, n = append(out.Segments, Segment{Text: restoreAllowed(g.Text)}), n+1
		default:
			out.Segments = append(out.Segments, g)
		}
	}
	return out, n
}

// restoreAllowed is words with every figure but a date or an ordinal replaced by what could not be established.
func restoreAllowed(s string) string {
	var b strings.Builder
	last := 0
	for _, m := range allowedNumbers.FindAllStringIndex(s, -1) {
		b.WriteString(digits.ReplaceAllString(s[last:m[0]], "(a figure not established)"))
		b.WriteString(s[m[0]:m[1]])
		last = m[1]
	}
	b.WriteString(digits.ReplaceAllString(s[last:], "(a figure not established)"))
	return b.String()
}

// Render is a structured answer as the person reads it (AI-69): words in Fyniq's Markdown subset, each figure from its
// fact, and what could not be established said so. It gives the facts it cited, in order.
func Render(s Structured, known func(string) (aitool.Fact, bool)) (string, []string) {
	var b strings.Builder
	var cited []string
	for _, g := range s.Segments {
		switch {
		case g.Fact != "":
			if f, ok := known(g.Fact); ok {
				b.WriteString(Figure(f))
				cited = append(cited, f.ID)
			}
		case g.Missing != "":
			b.WriteString("(I could not establish " + strings.TrimSuffix(strings.TrimSpace(g.Missing), ".") + ")")
		default:
			b.WriteString(Markdown(g.Text))
		}
	}
	return strings.TrimSpace(b.String()), cited
}

// Figure is a fact's value as the answer shows it, rounded once by RND-DISPLAY: an amount in its currency's decimals
// with thousands separated and a true minus, a percentage to one decimal, months to one, a ratio to two, days and
// counts whole. A fact with no value shows what stands for it, or why it has none.
func Figure(f aitool.Fact) string {
	if f.Value == "" {
		if f.Words != "" {
			return f.Words
		}
		return "no figure (" + f.Undefined + ")"
	}
	d, _, err := apd.NewFromString(f.Value)
	if err != nil {
		return f.Value
	}
	places := map[string]int{aitool.UnitAmount: 2, "percent": 1, "months": 1, "ratio": 2, "days": 0, aitool.UnitCount: 0}
	p, ok := places[f.Unit]
	if f.Unit == aitool.UnitAmount {
		if c, err := money.Lookup(f.Currency); err == nil {
			p = c.MinorUnits
		}
	}
	text := f.Value
	if ok {
		text = rounding.Display(d, p)
	}
	text = grouped(text)
	switch f.Unit {
	case aitool.UnitAmount:
		return strings.TrimSpace(f.Currency + " " + text)
	case "percent":
		return text + "%"
	case "months", "days":
		return text + " " + f.Unit
	case "ratio":
		return text + "×"
	}
	return text
}

// grouped is decimal text with its thousands separated by commas and a true minus (U+2212).
func grouped(s string) string {
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	whole, frac, hasFrac := strings.Cut(s, ".")
	var b strings.Builder
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	out := b.String()
	if hasFrac {
		out += "." + frac
	}
	if neg && strings.Trim(out, "0.,") != "" {
		out = "−" + out
	}
	return out
}

var (
	mdImage = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	mdLink  = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)[^)]*\)`)
	html    = regexp.MustCompile(`</?[A-Za-z][^>]*>`)
	bareURL = regexp.MustCompile(`<(https?://[^>\s]+)>`)
)

// Markdown is words in Fyniq's Markdown subset (AI-69): paragraphs, lists, emphasis and tables stay; images and raw
// HTML go; a link to Fyniq's own pages stays a link, and any other shows its address in full.
func Markdown(s string) string {
	s = mdImage.ReplaceAllString(s, "")
	s = bareURL.ReplaceAllString(s, "$1")
	s = mdLink.ReplaceAllStringFunc(s, func(m string) string {
		sub := mdLink.FindStringSubmatch(m)
		label, target := sub[1], sub[2]
		if strings.HasPrefix(target, "/") && !strings.HasPrefix(target, "//") {
			return "[" + label + "](" + target + ")"
		}
		return label + " (" + target + ")"
	})
	return html.ReplaceAllString(s, "")
}
