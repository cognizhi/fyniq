package knowledge_test

import (
	"testing"

	"github.com/cognizhi/fyniq/internal/domain/knowledge"
)

// The lint of an organisation page's version before it is published (AI-52, AI-53; design D8): the platform lint's rules, for
// pages made of notes and settings.
func TestAnOrganisationPageIsLintedBeforeItIsPublished(t *testing.T) {
	sources := map[string]string{"note:expense-policy": "Claims are made within 14 days. Receipts over 50 dollars.", "setting:approvals.limit": "2000"}
	source := func(ref string) (string, bool) { s, ok := sources[ref]; return s, ok }
	exists := func(key string) bool { return key == "org.other" }
	good := knowledge.OrgPage{Key: "org.expense-policy", Summary: "How claims are made.", Sources: []string{"note:expense-policy", "setting:approvals.limit"},
		Body: "# Expense policy\n\n## Claims\n\nClaims are made within 14 days. See [[org.other]]. (src: note:expense-policy#claims)\n\n## Limit\n\nUp to 2000. (src: setting:approvals.limit#value)\n"}
	if f := knowledge.LintOrgPage(good, source, exists); len(f) != 0 {
		t.Fatalf("a good page: %v", f)
	}
	for name, c := range map[string]struct {
		mut  func(p knowledge.OrgPage) knowledge.OrgPage
		rule string
	}{
		"no summary":         {func(p knowledge.OrgPage) knowledge.OrgPage { p.Summary = ""; return p }, knowledge.OrgRuleSummary},
		"a section uncited":  {func(p knowledge.OrgPage) knowledge.OrgPage { p.Body += "\n## More\n\nWords.\n"; return p }, knowledge.OrgRuleSection},
		"an unlisted source": {func(p knowledge.OrgPage) knowledge.OrgPage { p.Body += "\nX. (src: note:secret#a)\n"; return p }, knowledge.OrgRuleCiteSource},
		"a missing source": {func(p knowledge.OrgPage) knowledge.OrgPage {
			p.Sources = append(p.Sources, "note:gone")
			return p
		}, knowledge.OrgRuleSourceGone},
		"an invented number": {func(p knowledge.OrgPage) knowledge.OrgPage {
			p.Body = "# E\n\n## Claims\n\nWithin 30 days. (src: note:expense-policy#claims)\n"
			return p
		}, knowledge.OrgRuleNumber},
		"a broken link": {func(p knowledge.OrgPage) knowledge.OrgPage {
			p.Body += "\nSee [[org.nowhere]]. (src: note:expense-policy#claims)\n"
			return p
		}, knowledge.OrgRuleLink},
	} {
		found := false
		for _, f := range knowledge.LintOrgPage(c.mut(good), source, exists) {
			found = found || f.Rule == c.rule
		}
		if !found {
			t.Errorf("%s: no %s", name, c.rule)
		}
	}
}
