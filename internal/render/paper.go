package render

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// Paper is a paper its caller wrote (FR-CS-05; p1-company-secretary task 4.1): a resolution, a notice or minutes, every
// word already filled in from the records. The render service lays it out with paper.typ as it lays a report out with
// report.typ: the Typst source only places the text, so the same paper and creation time give the same bytes.
type Paper struct {
	Title      string           `json:"title"`                // such as "Directors' resolution in writing": the PDF's title (PDF/UA-1)
	Author     string           `json:"author"`               // the company it is for, its name at the top
	Lines      []string         `json:"lines"`                // under the company's name: its UEN and registered office
	Banner     string           `json:"banner,omitempty"`     // boxed above everything, such as that it is a draft to review
	Sections   []PaperSection   `json:"sections"`             // in order
	Signatures []PaperSignature `json:"signatures,omitempty"` // a line to sign above each name, after the sections
	Footer     string           `json:"footer"`               // each page's foot, beside its number: the template and its version
}

// PaperSection is a part of a paper: a heading, its paragraphs, then its numbered items, such as the resolutions.
type PaperSection struct {
	Heading    string   `json:"heading,omitempty"`
	Paragraphs []string `json:"paragraphs,omitempty"`
	Items      []string `json:"items,omitempty"`
}

// PaperSignature is a person who signs a paper, and as what, such as a director.
type PaperSignature struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

// The bounds of a paper: minutes of a meeting with its resolutions, signed by a board.
const (
	MaxPaperSections   = 40
	MaxPaperParagraphs = 40 // in a section
	MaxPaperItems      = 40 // in a section
	MaxPaperSignatures = 30
	maxPaperText       = 2000
)

// ErrPaper is a paper the render service refuses to lay out.
var ErrPaper = errors.New("render: not a paper")

// Check says what is wrong with p, if anything: a title and a section, each section with a paragraph or an item, at
// most MaxPaperSections sections of at most MaxPaperParagraphs paragraphs and MaxPaperItems items, at most
// MaxPaperSignatures signatures each with a name, and no text longer than 2,000 characters.
func (p Paper) Check() error {
	bad := func(format string, args ...any) error {
		return fmt.Errorf("%w: "+format, append([]any{ErrPaper}, args...)...)
	}
	if strings.TrimSpace(p.Title) == "" {
		return bad("it has no title")
	}
	if len(p.Sections) == 0 || len(p.Sections) > MaxPaperSections {
		return bad("it has %d sections; a paper has 1 to %d", len(p.Sections), MaxPaperSections)
	}
	if len(p.Signatures) > MaxPaperSignatures {
		return bad("it has %d signatures; a paper has at most %d", len(p.Signatures), MaxPaperSignatures)
	}
	texts := slices.Concat([]string{p.Title, p.Author, p.Banner, p.Footer}, p.Lines)
	for i, s := range p.Sections {
		switch {
		case len(s.Paragraphs) == 0 && len(s.Items) == 0:
			return bad("section %d has no paragraph or item", i+1)
		case len(s.Paragraphs) > MaxPaperParagraphs || len(s.Items) > MaxPaperItems:
			return bad("section %d has %d paragraphs and %d items; a section has at most %d of each", i+1, len(s.Paragraphs), len(s.Items), MaxPaperItems)
		}
		texts = slices.Concat(texts, []string{s.Heading}, s.Paragraphs, s.Items)
	}
	for i, s := range p.Signatures {
		if strings.TrimSpace(s.Name) == "" {
			return bad("signature %d has no name", i+1)
		}
		texts = append(texts, s.Name, s.Role)
	}
	for _, s := range texts {
		if utf8.RuneCountInString(s) > maxPaperText {
			return bad("a text is longer than %d characters", maxPaperText)
		}
	}
	return nil
}
