package knowledge_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/domain/knowledge"
)

func renderInput() knowledge.RenderInput {
	return knowledge.RenderInput{TeamMode: "founder",
		Settings: []knowledge.RenderSetting{
			{Key: "approvals.self_approval_limit", Group: "Approvals", Label: "Founder self-approval limit (SGD)", Value: "2500", Recommended: "2000", Reason: "A founder may approve small items alone.", Changed: true},
			{Key: "approvals.review_weekly", Group: "Approvals", Label: "Weekly review", Value: "on", Recommended: "on", Reason: "Self-approved actions are reviewed every week."},
			{Key: "features.ai_assistant", Group: "Features", Label: "AI assistant", Value: "off", Recommended: "off", Reason: "Off until an Owner turns it on."},
		},
		Policies: []knowledge.RenderPolicy{
			{ObjectType: "journal", Label: "Posting a journal", Approvers: []int{1}},
			{ObjectType: "payment_batch", Label: "Releasing a payment batch", Approvers: []int{1, 2}, Changed: true},
		}}
}

// What an organisation's settings say is rendered into pages with no model and no clock (AI-50; design D6): the same input gives the
// same bytes, a changed setting says so and gives the recommended value and why, a page cannot hold a number its sources lack, and
// every page passes the lint a publication is checked by.
func TestSettingsAreRenderedIntoPagesDeterministically(t *testing.T) {
	a, b := knowledge.Render(renderInput()), knowledge.Render(renderInput())
	if !reflect.DeepEqual(a, b) {
		t.Fatal("the same settings gave two renderings")
	}
	keys := map[string]knowledge.RenderedPage{}
	for _, p := range a {
		keys[p.Key] = p
	}
	for _, want := range []string{"org.team-mode", "org.approval-policies", "org.settings.approvals", "org.settings.features"} {
		if _, ok := keys[want]; !ok {
			t.Fatalf("no page %s among %d", want, len(a))
		}
	}
	appr := keys["org.settings.approvals"]
	for _, w := range []string{"is 2500 (changed from the recommended value 2000)", "is on (the recommended value)", "small items alone"} {
		if !strings.Contains(appr.Body, w) {
			t.Errorf("the approvals page lacks %q:\n%s", w, appr.Body)
		}
	}
	if !strings.Contains(keys["org.approval-policies"].Body, "Releasing a payment batch: 1 approver, then 2 approvers, the organisation's own policy") ||
		!strings.Contains(keys["org.team-mode"].Body, "works in Founder mode") {
		t.Errorf("the policy and team-mode pages:\n%s\n%s", keys["org.approval-policies"].Body, keys["org.team-mode"].Body)
	}
	// Every page is what the lint accepts: each section cites a source it lists, each number is in a source, the summary is a line.
	for _, p := range a {
		texts := map[string]string{}
		var refs []string
		for _, s := range p.Sources {
			texts[s.Cite()], refs = s.Text, append(refs, s.Cite())
		}
		f := knowledge.LintOrgPage(knowledge.OrgPage{Key: p.Key, Summary: p.Summary, Body: p.Body, Sources: refs},
			func(r string) (string, bool) { t, ok := texts[r]; return t, ok }, func(string) bool { return true })
		if len(f) != 0 {
			t.Errorf("%s fails its lint: %v", p.Key, f)
		}
		if len([]rune(p.Title)) > 200 || len(p.Body) > 60000 {
			t.Errorf("%s is too large: %d, %d", p.Key, len(p.Title), len(p.Body))
		}
	}
	// A change to one setting changes only the page of its group.
	in := renderInput()
	in.Settings[2].Value = "on"
	changed := 0
	for _, p := range knowledge.Render(in) {
		if p.Body != keys[p.Key].Body {
			changed++
			if p.Key != "org.settings.features" {
				t.Errorf("%s changed with a features setting", p.Key)
			}
		}
	}
	if changed != 1 {
		t.Errorf("%d pages changed", changed)
	}
	if len(knowledge.Render(knowledge.RenderInput{})) != 0 {
		t.Error("an organisation with no settings, policies or mode rendered pages")
	}
}
