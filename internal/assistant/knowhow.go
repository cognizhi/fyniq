package assistant

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Know-how answers (PRD-001 AI-51, AI-53, AI-08; p1-knowledge-base design D4): how Fyniq works, what a status means, what
// the organisation decided. The model gives such a sentence as a segment naming the page it read this turn (kb.read) and the
// section of it, and platform code checks it before the person sees it: the page was read in this turn, the section is one
// the read returned, and every number in the sentence is one the section holds as a whole token. The citation shown is the
// guide page the section was compiled from, which the reader can open, resolved here from the section's own citations: the
// model never names a source, and no citation ever resolves to a compiled page.

// Source is a guide page a know-how sentence cites: what the reader can open, with the heading it comes from.
type Source struct {
	Guide  string `json:"guide,omitempty"` // the guide page's key; empty for what an organisation page comes from
	Title  string `json:"title"`
	Anchor string `json:"anchor"`
	Draft  bool   `json:"draft,omitempty"`
	Href   string `json:"href,omitempty"` // where it opens, when it is not the guide: a note or a setting; none when the reader may not open it
}

// pageSection is a section of a page the turn read: its words, and the guide pages it cites.
type pageSection struct {
	Text    string
	Sources []Source
}

// pageRead is a page the turn read, by the sections the reads returned.
type pageRead struct {
	Version  string
	Draft    bool
	Sections map[string]pageSection
}

// pages are the knowledge-base pages a turn read, by key.
type pages map[string]*pageRead

// readContent is a kb.read result's content, as the tool shapes it (internal/api/ai_kb.go).
type readContent struct {
	Key      string `json:"key"`
	Version  string `json:"version"`
	Draft    bool   `json:"draft"`
	Intro    string `json:"intro"`
	Sections []struct {
		Section string   `json:"section"`
		Text    string   `json:"text"`
		Cites   []Source `json:"cites"`
	} `json:"sections"`
}

// note keeps what a kb.read returned, so a later sentence can cite it. Anything that is not a page is ignored.
func (p pages) note(content json.RawMessage) {
	var c readContent
	if len(content) == 0 || json.Unmarshal(content, &c) != nil || c.Key == "" || len(c.Sections) == 0 {
		return
	}
	pg := p[c.Key]
	if pg == nil {
		pg = &pageRead{Sections: map[string]pageSection{}}
		p[c.Key] = pg
	}
	pg.Version, pg.Draft = c.Version, c.Draft
	for _, s := range c.Sections {
		pg.Sections[s.Section] = pageSection{Text: s.Text, Sources: s.Cites}
	}
}

var numberToken = regexp.MustCompile(`[0-9][0-9,]*(?:\.[0-9]+)?`)

// tokens are the numbers in text, each as a whole token: a trailing comma or point is not part of it.
func tokens(text string) map[string]bool {
	out := map[string]bool{}
	for _, t := range numberToken.FindAllString(text, -1) {
		out[strings.TrimRight(t, ",.")] = true
	}
	return out
}

// knowHow checks one know-how segment against what the turn read, in words the model can act on.
func (p pages) knowHow(g Segment) []string {
	pg, ok := p[g.Page]
	if !ok {
		return []string{fmt.Sprintf("the know-how page %q was not read in this conversation: read it with kb_read first, or answer without it", clip(g.Page))}
	}
	sec, ok := pg.Sections[g.Section]
	if !ok {
		return []string{fmt.Sprintf("%q has no section %q among those you read: cite a section kb_read returned", g.Page, clip(g.Section))}
	}
	if strings.TrimSpace(g.Text) == "" {
		return []string{fmt.Sprintf("the know-how segment citing %s has no words", g.Page)}
	}
	var out []string
	held := tokens(sec.Text)
	for t := range tokens(allowedNumbers.ReplaceAllString(g.Text, "")) {
		if !held[t] {
			out = append(out, fmt.Sprintf("the words %q hold the number %s, which section %s of %s does not: use only the section's own numbers", clip(g.Text), t, g.Section, g.Page))
		}
	}
	return out
}

// sources are the guide pages a turn's know-how sentences cite, each once, in order: resolved from the sections the model
// cited, never named by it.
func (p pages) sources(segments []Segment) []Source {
	var out []Source
	seen := map[Source]bool{}
	for _, g := range segments {
		if g.Page == "" {
			continue
		}
		pg := p[g.Page]
		if pg == nil {
			continue
		}
		for _, s := range pg.Sections[g.Section].Sources {
			if !seen[s] {
				seen[s], out = true, append(out, s)
			}
		}
	}
	return out
}
