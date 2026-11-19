package knowledge

import (
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"
)

// The rules of the deterministic lint, as codes both lints give (scripts/kb.py's test maps its messages to them over the
// shared fixtures), so that one finding in one is one finding in the other.
const (
	RuleFileType     = "KB-FILE-TYPE"     // a file under kb/pages that is not Markdown
	RuleFront        = "KB-FRONT"         // no front matter, or a line that cannot be read
	RuleDuplicateKey = "KB-DUPLICATE-KEY" // two pages with one key
	RuleField        = "KB-FIELD"         // a required front-matter field is missing
	RuleType         = "KB-TYPE"          // not one of the page types
	RuleTypeDir      = "KB-TYPE-DIR"      // a page outside the directory of its type
	RuleSummary      = "KB-SUMMARY"       // a summary over 160 characters
	RuleSchema       = "KB-SCHEMA"        // another schema than this release's
	RuleClass        = "KB-CLASS"         // not a data class
	RuleTaint        = "KB-TAINT"         // not trusted or untrusted
	RuleStatus       = "KB-STATUS"        // not draft or published
	RuleHumanEdited  = "KB-HUMAN-EDITED"  // not true or false
	RuleDate         = "KB-DATE"          // compiled_at is not a date
	RuleAliases      = "KB-ALIASES"       // aliases that are empty or too long
	RuleUntrusted    = "KB-UNTRUSTED"     // an untrusted page that is published (AI-54)
	RuleSourceLine   = "KB-SOURCE-LINE"   // a sources line that is not "<path> sha256:<hex>"
	RuleSourceBanned = "KB-SOURCE-BANNED" // an internal document as a source
	RuleSourceGone   = "KB-SOURCE-GONE"   // a source that does not exist
	RuleStale        = "KB-STALE"         // a source changed since the page was compiled
	RulePublishDraft = "KB-PUBLISH-DRAFT" // a published page compiled from a draft source
	RuleTitle        = "KB-TITLE"         // warning: a body that does not start with its title
	RuleSectionCite  = "KB-SECTION-CITE"  // a section that cites no source
	RuleCiteSource   = "KB-CITE-SOURCE"   // a citation of a source the page does not list
	RuleCiteAnchor   = "KB-CITE-ANCHOR"   // a citation of a heading the source lacks
	RuleLink         = "KB-LINK"          // a [[link]] to no page
	RuleNumber       = "KB-NUMBER"        // a number in none of the page's sources (AI-53)
	RuleOrphan       = "KB-ORPHAN"        // warning: a page no other page links to
	RuleUncovered    = "KB-UNCOVERED"     // a source no page covers
	RuleIndex        = "KB-INDEX"         // an index that is not what the pages generate
)

// forbiddenSources are internal documents that may never be a source (kb/schema.md).
var forbiddenSources = []string{"docs/prd/"}

var required = []string{"key", "type", "title", "summary", "sources", "data_class", "taint", "status", "human_edited",
	"compiled_by", "schema", "compiled_at"}

// Finding is one result of the lint.
type Finding struct {
	Rule    string
	Where   string // the page's file, or the source's path
	Detail  string
	Warning bool
}

func (f Finding) String() string { return fmt.Sprintf("%s: %s [%s]", f.Where, f.Detail, f.Rule) }

// Lint checks a corpus against kb/schema.md: front matter, source hashes, citations and their anchors, links, the
// numbers' grounding in the cited sources (AI-53), the coverage of every guide page, and that the shipped index is what
// the pages generate. A published release must have none of the errors.
func Lint(c Corpus) []Finding {
	var out []Finding
	add := func(rule, where, detail string) { out = append(out, Finding{Rule: rule, Where: where, Detail: detail}) }
	warn := func(rule, where, detail string) {
		out = append(out, Finding{Rule: rule, Where: where, Detail: detail, Warning: true})
	}
	keys := map[string]string{}
	for _, p := range c.Pages {
		if p.Key == "" {
			continue
		}
		if first, dup := keys[p.Key]; dup {
			add(RuleDuplicateKey, p.File, "duplicate key "+p.Key+" (also "+first+")")
		}
		keys[p.Key] = p.File
	}
	covered, inbound := map[string]bool{}, map[string]int{}
	for _, p := range c.Pages {
		for _, e := range p.err {
			rule := RuleFront
			if strings.HasPrefix(e, "only Markdown") {
				rule = RuleFileType
			}
			add(rule, p.File, e)
		}
		if p.front.empty() {
			continue
		}
		for _, f := range required {
			if !p.front.has(f) {
				add(RuleField, p.File, "front matter is missing '"+f+"'")
			}
		}
		switch {
		case !slices.Contains(Types, p.Type):
			add(RuleType, p.File, "type must be one of "+strings.Join(Types, ", "))
		case path.Base(path.Dir(p.File)) != p.Type:
			add(RuleTypeDir, p.File, "a '"+p.Type+"' page belongs in kb/pages/"+p.Type+"/")
		}
		if len([]rune(p.Summary)) > 160 {
			add(RuleSummary, p.File, "summary is longer than 160 characters")
		}
		if p.Schema != Schema {
			add(RuleSchema, p.File, "schema must be "+Schema)
		}
		if !slices.Contains(DataClasses, p.DataClass) {
			add(RuleClass, p.File, "data_class must be one of "+strings.Join(DataClasses, ", "))
		}
		if p.Taint != "trusted" && p.Taint != "untrusted" {
			add(RuleTaint, p.File, "taint must be trusted or untrusted")
		}
		if p.Status != "draft" && p.Status != "published" {
			add(RuleStatus, p.File, "status must be draft or published")
		}
		if h := p.front.scalars["human_edited"]; h != "true" && h != "false" {
			add(RuleHumanEdited, p.File, "human_edited must be true or false")
		}
		if !dateRe.MatchString(p.CompiledAt) {
			add(RuleDate, p.File, "compiled_at must be a date (YYYY-MM-DD)")
		}
		if _, has := p.front.lists["aliases"]; has {
			bad := len(p.Aliases) == 0
			for _, a := range p.Aliases {
				bad = bad || len([]rune(a)) > 60
			}
			if bad {
				add(RuleAliases, p.File, "aliases must be a non-empty list of phrases of at most 60 characters")
			}
		}
		if p.Status == "published" && p.Taint == "untrusted" {
			add(RuleUntrusted, p.File, "an untrusted page stays draft until approved (AI-54)")
		}
		texts := map[string]string{}
		var corpus []string
		for _, s := range p.Sources {
			if s.SHA256 == "" {
				add(RuleSourceLine, p.File, "source line must be '<path> sha256:<64 hex>': "+s.Path)
				continue
			}
			for _, f := range forbiddenSources {
				if strings.HasPrefix(s.Path, f) {
					add(RuleSourceBanned, p.File, s.Path+" is internal and may not be a source (kb/schema.md)")
				}
			}
			g, ok := c.GuideAt(s.Path)
			if !ok {
				add(RuleSourceGone, p.File, "source not found: "+s.Path)
				continue
			}
			covered[s.Path] = true
			texts[s.Path] = g.Full
			corpus = append(corpus, g.Full)
			if g.SHA256 != s.SHA256 {
				add(RuleStale, p.File, "STALE, "+s.Path+" changed since compile; recompile (fyniq-knowledge-base skill)")
			}
			if p.Status == "published" && g.Status == "draft" {
				add(RulePublishDraft, p.File, "published, but its source "+s.Path+" is a draft")
			}
		}
		if !strings.HasPrefix(strings.TrimLeft(p.Body, " \t\r\n"), "# ") {
			warn(RuleTitle, p.File, "the body should start with '# <title>'")
		}
		for _, s := range Sections(p.Body) {
			if !citeRe.MatchString(s.Text) {
				add(RuleSectionCite, p.File, "section '"+s.Heading+"' cites no source")
			}
		}
		for _, ct := range Citations(p.Body) {
			text, ok := texts[ct.Path]
			switch {
			case !ok:
				add(RuleCiteSource, p.File, "cites "+ct.Path+", which is not in its sources")
			case !Anchors(text)[ct.Anchor]:
				add(RuleCiteAnchor, p.File, "cites "+ct.Path+"#"+ct.Anchor+", but the source has no such heading")
			}
		}
		for _, t := range Links(p.Body) {
			switch {
			case keys[t] == "":
				add(RuleLink, p.File, "link [["+t+"]] matches no page key")
			case t != p.Key:
				inbound[t]++
			}
		}
		joined := strings.Join(corpus, "\n")
		seen := map[string]bool{}
		var nums []string
		for _, n := range NumbersOutsideIDs(p.Body) {
			if !seen[n] {
				seen[n], nums = true, append(nums, n)
			}
		}
		sort.Strings(nums)
		for _, n := range nums {
			if !strings.Contains(joined, n) {
				add(RuleNumber, p.File, "the number '"+n+"' appears in none of its sources (AI-53)")
			}
		}
	}
	for k, where := range keys {
		if len(keys) > 1 && inbound[k] == 0 {
			warn(RuleOrphan, where, "orphan page, no other page links to [["+k+"]]")
		}
	}
	for _, g := range c.Guide {
		if !covered[g.Path] {
			add(RuleUncovered, g.Path, "source not compiled into any page")
		}
	}
	if c.Index != RenderIndex(c.Pages) {
		add(RuleIndex, "kb/index.md", "kb/index.md is out of date; run make kb-index")
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Where < out[j].Where })
	return out
}

// Errors are the findings that are not warnings.
func Errors(fs []Finding) []Finding {
	var out []Finding
	for _, f := range fs {
		if !f.Warning {
			out = append(out, f)
		}
	}
	return out
}
