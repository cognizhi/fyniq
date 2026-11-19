package knowledge

import (
	"fmt"
	"sort"
	"strings"
)

// The rules of the lint of an organisation page's version before it is published (AI-52, AI-53, AI-56; p1-knowledge-base design
// D8). They are the platform lint's rules for what an organisation page is made of: its sources are notes, settings and, for an
// overlay, the platform page it replaces.
const (
	OrgRuleSummary    = "KB-ORG-SUMMARY"     // a summary over 160 characters, or none
	OrgRuleSection    = "KB-ORG-SECTION"     // a section that cites no source
	OrgRuleCiteSource = "KB-ORG-CITE-SOURCE" // a citation of a source the page does not list
	OrgRuleSourceGone = "KB-ORG-SOURCE-GONE" // a listed source that does not exist
	OrgRuleNumber     = "KB-ORG-NUMBER"      // a number in none of the cited sources
	OrgRuleLink       = "KB-ORG-LINK"        // a [[link]] to no page
)

// OrgPage is the part of a page's version the lint reads.
type OrgPage struct {
	Key     string
	Summary string
	Body    string
	// Sources are the sources the version lists, as "kind:ref" (note:expense-policy, setting:approvals.self_approval_limit,
	// platform:founders.balance).
	Sources []string
}

// LintOrgPage checks a version before it is published: the page's summary, that every section cites a source it lists, that each
// listed source exists (source returns its text), that every number in the page appears in a source it cites, and that its links
// match a page (pageExists). A page with any finding is not published until its text or its sources are fixed.
func LintOrgPage(p OrgPage, source func(ref string) (string, bool), pageExists func(key string) bool) []Finding {
	var out []Finding
	add := func(rule, detail string) { out = append(out, Finding{Rule: rule, Where: p.Key, Detail: detail}) }
	if n := len([]rune(p.Summary)); n == 0 || n > 160 {
		add(OrgRuleSummary, "the summary must be one line of at most 160 characters")
	}
	listed := map[string]bool{}
	texts := map[string]string{}
	for _, s := range p.Sources {
		listed[s] = true
		t, ok := source(s)
		if !ok {
			add(OrgRuleSourceGone, "the source "+s+" does not exist")
			continue
		}
		texts[s] = t
	}
	for _, s := range Sections(p.Body) {
		if !citeRe.MatchString(s.Text) {
			add(OrgRuleSection, "section '"+s.Heading+"' cites no source")
		}
	}
	for _, c := range Citations(p.Body) {
		if !listed[c.Path] {
			add(OrgRuleCiteSource, "cites "+c.Path+", which the page does not list as a source")
		}
	}
	var cited []string
	for _, c := range Citations(p.Body) {
		if t, ok := texts[c.Path]; ok {
			cited = append(cited, t)
		}
	}
	joined := strings.Join(cited, "\n")
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
			add(OrgRuleNumber, fmt.Sprintf("the number '%s' appears in none of the sources it cites (AI-53)", n))
		}
	}
	for _, l := range Links(p.Body) {
		if l != p.Key && !pageExists(l) {
			add(OrgRuleLink, "link [["+l+"]] matches no page")
		}
	}
	return out
}
