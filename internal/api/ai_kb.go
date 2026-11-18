package api

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/cognizhi/fyniq/internal/aitool"
	"github.com/cognizhi/fyniq/internal/assistant"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/knowledge"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The knowledge base's read tools (PRD-001 AI-49, AI-51, AI-54, AI-58; p1-knowledge-base design D2, D3): kb.index, kb.read
// and kb.search run through callAITool as the turn's person, like every read tool. Each reads through the reader filter of
// migration 000560, which applies the person's live roles and the data classes their rights let them read in the query, so
// a page the person may not open is not in the index, the search, a page read or a citation at all. What comes back is
// know-how: no figures, which come only from the other tools.

// AIKBPage is the version of a knowledge-base page an answer used (AI-12, AI-51): the page and the release that shipped it
// with the page file's digest.
type AIKBPage struct {
	Key     string `json:"key" example:"claims.approve-team-claims"`
	Version string `json:"version" doc:"The platform release and the page file's SHA-256 (first 12 characters)." example:"v0.1.0#7c1ea9b4f2d3"`
}

// kbReader is who is asking: the person's roles as the user guide names them, and the data classes their rights let them
// read (AI-54). The AI never reads the identity class (AI-02), and payroll only with AI payroll access on and the person's
// own payroll right, as for every other read.
func (a *API) kbReader(ctx context.Context) (storage.KBReader, error) {
	p, _ := authn.From(ctx)
	roles := make([]string, 0, len(p.Roles))
	for _, r := range p.Roles {
		roles = append(roles, string(r))
	}
	may := func(c authz.Capability) bool {
		return authz.Decide(authz.Policy{Operation: "kb.class", Capability: c, Need: authz.View}, p.Roles, true).Allowed
	}
	classes := []string{"general"}
	if may(authz.ContactsAndItems) || may(authz.UsersAndRoles) {
		classes = append(classes, "personal")
	}
	if may(authz.CapTable) {
		classes = append(classes, "equity")
	}
	policy, err := a.aiExclusionPolicy(ctx, p)
	if err != nil {
		return storage.KBReader{}, err
	}
	if policy.Payroll {
		classes = append(classes, "payroll")
	}
	return storage.KBReader{Roles: roles, Classes: classes}, nil
}

// kbSection is a page key's section: the first part of its key.
func kbSection(key string) string { return strings.SplitN(key, ".", 2)[0] }

// runKBTool runs one of the knowledge base's tools. The result's content is shaped for the model; KBPages names the page
// versions it read, for the activity log.
func (a *API) runKBTool(ctx context.Context, tool aitool.Tool, in map[string]any) (AIToolResult, error) {
	reader, err := a.kbReader(ctx)
	if err != nil {
		return AIToolResult{}, err
	}
	str := func(k string) string { s, _ := in[k].(string); return s }
	res := AIToolResult{Facts: []aitool.Fact{}, Omitted: []aitool.Omission{}}
	p, _ := authn.From(ctx)
	mayReadNotes := authz.Decide(authz.Policy{Operation: "kb.notes", Capability: authz.KnowledgeBase, Need: authz.View}, p.Roles, false).Allowed
	err = a.deps.Store.Do(ctx, authnOrg(ctx), func(ctx context.Context, u storage.UnitOfWork) error {
		kb, org := u.KnowledgeBase(), u.OrganisationKnowledge()
		info, ok, err := kb.PlatformInfo(ctx)
		if err != nil {
			return err
		}
		if !ok {
			return problem(CodeToolRefused, "The knowledge base is not loaded in this installation.", "Answer from the tools that read the books, and say the guide is not available")
		}
		switch tool.Name {
		case "kb.index":
			entries, err := kb.PlatformIndex(ctx, reader)
			if err != nil {
				return err
			}
			live, err := org.LiveIndex(ctx, reader.Classes)
			if err != nil {
				return err
			}
			res.Content = kbIndexContent(info.Version, mergeIndex(entries, live), str("section"), tool.Limits.ResultTokens)
		case "kb.read":
			if mine, ok, err := org.LivePage(ctx, str("key"), reader.Classes); err != nil {
				return err
			} else if ok { // the organisation's own page, or its overlay of a platform page, wins
				res.Content = kbPageContent(orgView(mine, mayReadNotes), str("section"), tool.Limits.ResultTokens)
				res.KBPages = []AIKBPage{{Key: mine.Key, Version: fmt.Sprintf("organisation snapshot %d, version %d", mine.Snapshot, mine.Number)}}
				return nil
			}
			page, ok, err := kb.PlatformPage(ctx, str("key"), reader)
			if err != nil {
				return err
			}
			if !ok { // a page the person may not open looks like a page that is not there
				return problem(CodeToolRefused, "No page has the key "+str("key")+" that you may read.", "Look for it with kb.index or kb.search")
			}
			guides, err := kb.PlatformGuides(ctx, page.Key, reader)
			if err != nil {
				return err
			}
			res.Content = kbPageContent(platformView(page, guides), str("section"), tool.Limits.ResultTokens)
			res.KBPages = []AIKBPage{{Key: page.Key, Version: kbVersion(page)}}
		case "kb.search":
			limit := 5
			if n, ok := in["limit"].(float64); ok {
				limit = int(n)
			}
			scope := str("scope")
			if scope == "" {
				scope = "both"
			}
			terms := knowledge.QueryTerms(str("query"))
			var hits []storage.KBHit
			if len(terms) > 0 {
				if hits, err = kb.PlatformSearch(ctx, storage.KBSearch{Terms: terms, Scope: scope, Limit: limit, Reader: reader}); err != nil {
					return err
				}
				if scope != "sources" {
					mine, err := org.Search(ctx, terms, reader.Classes, limit)
					if err != nil {
						return err
					}
					hits = mergeHits(hits, mine, limit)
				}
			}
			res.Content = kbSearchContent(hits, terms)
		default:
			return problem(CodeToolElse, tool.Name+" is not a knowledge-base tool.", "Use another read tool")
		}
		return nil
	})
	if err != nil {
		return AIToolResult{}, err
	}
	return res, nil
}

func kbVersion(p storage.KBPlatformPage) string {
	d := p.Digest
	if len(d) > 12 {
		d = d[:12]
	}
	return p.Release + "#" + d
}

// kbIndexContent is the index the reader may read: the whole of it while it fits the tool's result, otherwise its sections
// with their page counts, and a section's pages when one is asked for (AI-58).
func kbIndexContent(release string, entries []storage.KBEntry, section string, budget int) map[string]any {
	type row struct {
		Key     string `json:"key"`
		Type    string `json:"type"`
		Title   string `json:"title"`
		Summary string `json:"summary"`
		Status  string `json:"status,omitempty"`
	}
	rows := func(es []storage.KBEntry) []row {
		out := make([]row, 0, len(es))
		for _, e := range es {
			r := row{Key: e.Key, Type: e.Type, Title: e.Title, Summary: e.Summary}
			if e.Status == "draft" {
				r.Status = "draft"
			}
			out = append(out, r)
		}
		return out
	}
	content := map[string]any{"release": release}
	if section != "" {
		var in []storage.KBEntry
		for _, e := range entries {
			if kbSection(e.Key) == section {
				in = append(in, e)
			}
		}
		content["section"], content["pages"] = section, rows(in)
		if len(in) == 0 {
			content["note"] = "No page you may read is in this section: list the sections by calling kb.index with no section."
		}
		return content
	}
	if all := rows(entries); aitool.Tokens(all) <= budget {
		content["pages"] = all
		return content
	}
	counts := map[string]int{}
	var order []string
	for _, e := range entries {
		s := kbSection(e.Key)
		if counts[s] == 0 {
			order = append(order, s)
		}
		counts[s]++
	}
	slices.Sort(order)
	sections := make([]map[string]any, 0, len(order))
	for _, s := range order {
		sections = append(sections, map[string]any{"section": s, "pages": counts[s]})
	}
	content["sections"] = sections
	content["note"] = "The index is long: call kb.index with a section to list its pages."
	return content
}

// kbCite is a source a section cites, as the model reads it and as Fyniq resolves it: a guide page, or what an organisation
// page comes from (a note or a setting), never a compiled page.
type kbCite struct {
	Guide  string `json:"guide,omitempty"`
	Title  string `json:"title"`
	Anchor string `json:"anchor"`
	Draft  bool   `json:"draft,omitempty"`
	Href   string `json:"href,omitempty"`
}

// kbPageView is what a page's content is made from, whoever's page it is.
type kbPageView struct {
	Key, Type, Title, Summary, Body string
	Aliases                         []string
	Version, Release                string
	Draft                           bool
	Note                            string // the page's own hint to the model
	Resolve                         func(path, anchor string) (kbCite, bool)
}

// kbPageContent is a page as the model reads it: its sections with their words and the sources each cites, in full while it
// fits the tool's result, otherwise its outline and the section asked for (design D3). The page's own citation markers are
// left out of the words; each section's citations come beside them, resolved to what a person can open (a guide page, a note
// or a setting), never to this page.
func kbPageContent(p kbPageView, section string, budget int) map[string]any {
	type sec struct {
		Section string   `json:"section"`
		Heading string   `json:"heading"`
		Text    string   `json:"text,omitempty"`
		Cites   []kbCite `json:"cites"`
	}
	var secs []sec
	for _, s := range knowledge.Sections(p.Body) {
		x := sec{Section: s.Anchor, Heading: s.Heading, Text: knowledge.Cited(s.Words()), Cites: []kbCite{}}
		for _, c := range s.Cites() {
			if r, ok := p.Resolve(c.Path, c.Anchor); ok {
				x.Cites = append(x.Cites, r)
			}
		}
		secs = append(secs, x)
	}
	content := map[string]any{
		"key": p.Key, "type": p.Type, "title": p.Title, "summary": p.Summary, "version": p.Version, "release": p.Release,
		"draft": p.Draft,
	}
	if len(p.Aliases) > 0 {
		content["aliases"] = p.Aliases
	}
	if p.Note != "" {
		content["note"] = p.Note
	}
	if intro := knowledge.Intro(p.Body); intro != "" {
		content["intro"] = knowledge.Cited(intro)
	}
	shown := secs
	if section != "" {
		shown = nil
		for _, s := range secs {
			if s.Section == section {
				shown = append(shown, s)
			}
		}
		if len(shown) == 0 {
			content["note"] = fmt.Sprintf("The page has no section %q: its sections are listed in the outline.", section)
		}
	}
	content["sections"] = shown
	if aitool.Tokens(content) > budget {
		outline := make([]map[string]string, 0, len(secs))
		for _, s := range secs {
			outline = append(outline, map[string]string{"section": s.Section, "heading": s.Heading})
		}
		content["outline"] = outline
		if section == "" {
			content["sections"] = []sec{}
			content["note"] = "The page is long: read it a section at a time, calling kb.read with a section from the outline."
			return content
		}
		for aitool.Tokens(content) > budget && len(shown) > 0 {
			last := &shown[len(shown)-1]
			cut := len(last.Text) / 2
			for cut > 0 && !isRuneStart(last.Text, cut) {
				cut--
			}
			last.Text = last.Text[:cut] + " [cut: the section is longer than a tool result allows]"
			if cut == 0 {
				break
			}
			content["sections"] = shown
		}
	}
	return content
}

// platformView is a platform page as kbPageContent reads it: its citations are the guide pages it was compiled from.
func platformView(p storage.KBPlatformPage, guides []storage.KBGuideRef) kbPageView {
	byFile := map[string]storage.KBGuideRef{}
	for _, g := range guides {
		byFile[g.File] = g
	}
	v := kbPageView{Key: p.Key, Type: p.Type, Title: p.Title, Summary: p.Summary, Body: p.Body, Aliases: p.Aliases, Version: kbVersion(p),
		Release: p.Release, Draft: p.Status == "draft", Resolve: func(path, anchor string) (kbCite, bool) {
			g, ok := byFile[path]
			return kbCite{Guide: g.Key, Title: g.Title, Anchor: anchor, Draft: g.Draft}, ok
		}}
	if v.Draft {
		v.Note = "Compiled from a guide page still awaiting review: say so when you rely on it."
	}
	return v
}

// orgSource is one source of an organisation page, as its version records it: a note, a setting, or the platform page an
// overlay replaces.
type orgSource struct {
	Kind  string `json:"kind"`
	Ref   string `json:"ref"`
	Title string `json:"title"`
}

// orgView is an organisation page as kbPageContent reads it: a section cites "note:<key>#anchor", "setting:<key>#anchor" or
// "platform:<key>#anchor", resolved to what the reader can open. A note is the organisation's own and is shown to those who
// may read the knowledge base's notes; anyone else is told it is the organisation's policy, with no link.
func orgView(p storage.KBLivePage, mayReadNotes bool) kbPageView {
	var srcs []orgSource
	_ = json.Unmarshal(p.Sources, &srcs)
	return kbPageView{Key: p.Key, Type: p.Type, Title: p.Title, Summary: p.Summary, Body: p.Body, Aliases: p.Aliases,
		Version: fmt.Sprintf("organisation snapshot %d, version %d", p.Snapshot, p.Number), Release: "organisation",
		Note: "This is the organisation's own page, written or approved by its people: its policies and decisions, not Fyniq's guide.",
		Resolve: func(path, anchor string) (kbCite, bool) {
			kind, ref, _ := strings.Cut(path, ":")
			title := ref
			for _, s := range srcs {
				if s.Kind == kind && s.Ref == ref && s.Title != "" {
					title = s.Title
				}
			}
			switch kind {
			case "note":
				if !mayReadNotes {
					return kbCite{Title: "The organisation's policy", Anchor: anchor}, true
				}
				return kbCite{Title: "Note: " + title, Anchor: anchor, Href: "/settings/ai/knowledge-base#note-" + ref}, true
			case "setting":
				return kbCite{Title: "Setting: " + title, Anchor: anchor, Href: "/settings#" + ref}, true
			case "platform":
				return kbCite{Guide: ref, Title: title, Anchor: anchor, Href: "/help/" + ref + "#" + anchor}, true
			}
			return kbCite{}, false
		}}
}

func isRuneStart(s string, i int) bool { return i >= len(s) || s[i]&0xC0 != 0x80 }

var highlight = regexp.MustCompile(`</?b>`)

// kbSearchContent is the hits of a search: pages and guide pages the reader may open, ranked. A snippet is plain words.
func kbSearchContent(hits []storage.KBHit, terms []string) map[string]any {
	type hit struct {
		Kind    string `json:"kind"`
		Key     string `json:"key"`
		Title   string `json:"title"`
		Summary string `json:"summary,omitempty"`
		Snippet string `json:"snippet"`
		Draft   bool   `json:"draft,omitempty"`
	}
	out := make([]hit, 0, len(hits))
	for _, h := range hits {
		out = append(out, hit{Kind: h.Kind, Key: h.Key, Title: h.Title, Summary: h.Summary,
			Snippet: strings.Join(strings.Fields(highlight.ReplaceAllString(h.Snippet, "")), " "), Draft: h.Draft})
	}
	c := map[string]any{"searched": terms, "hits": out}
	if len(out) == 0 {
		c["note"] = "Nothing you may read matches. Try other words a person would use, or say the guide does not cover it."
	}
	return c
}

// kbPagesOf are the page versions the AI service reports a call read, as the turn's record keeps them.
func kbPagesOf(in []assistant.KBPage) []storage.KBPageVersion {
	var out []storage.KBPageVersion
	for _, p := range in {
		out = append(out, storage.KBPageVersion{Key: p.Key, Version: p.Version})
	}
	return out
}

// kbPagesOut are a call's recorded page versions as the activity log lists them: an empty list for most calls.
func kbPagesOut(in []storage.KBPageVersion) []AIKBPage {
	out := make([]AIKBPage, 0, len(in))
	for _, p := range in {
		out = append(out, AIKBPage{Key: p.Key, Version: p.Version})
	}
	return out
}

// mergeIndex is the platform's index with the organisation's published pages: an organisation page takes the place of a
// platform page of the same key (its overlay), and is listed with the rest by type and key.
func mergeIndex(platform []storage.KBEntry, mine []storage.KBLivePage) []storage.KBEntry {
	byKey := map[string]storage.KBEntry{}
	for _, e := range platform {
		byKey[e.Key] = e
	}
	for _, p := range mine {
		byKey[p.Key] = p.KBEntry
	}
	out := make([]storage.KBEntry, 0, len(byKey))
	for _, e := range byKey {
		out = append(out, e)
	}
	order := func(t string) int {
		return slices.Index([]string{"concept", "task", "reference", "policy", "decision"}, t)
	}
	slices.SortFunc(out, func(a, b storage.KBEntry) int {
		if c := order(a.Type) - order(b.Type); c != 0 {
			return c
		}
		return strings.Compare(a.Key, b.Key)
	})
	return out
}

// mergeHits are the organisation's hits, then the platform's, each best first, cut to limit: the organisation's own page
// that matches answers a question about "our" rule before the guide does, and the two ranks are of different searches, so they
// are not mixed. An organisation page replaces a platform hit of the same key (its overlay).
func mergeHits(platform, mine []storage.KBHit, limit int) []storage.KBHit {
	own := map[string]bool{}
	for _, h := range mine {
		own[h.Key] = true
	}
	byRank := func(a, b storage.KBHit) int {
		switch {
		case a.Rank > b.Rank:
			return -1
		case a.Rank < b.Rank:
			return 1
		}
		return 0
	}
	out := slices.Clone(mine)
	slices.SortStableFunc(out, byRank)
	rest := make([]storage.KBHit, 0, len(platform))
	for _, h := range platform {
		if h.Kind == "page" && own[h.Key] {
			continue
		}
		rest = append(rest, h)
	}
	slices.SortStableFunc(rest, byRank)
	out = append(out, rest...)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
