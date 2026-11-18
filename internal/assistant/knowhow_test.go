package assistant_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/aitool"
	"github.com/cognizhi/fyniq/internal/assistant"
	"github.com/cognizhi/fyniq/internal/ports"
)

// claimsPage is what kb.read returns for a page: its sections with their words and the guide pages each cites.
const claimsPage = `{"key":"claims.approve-team-claims","version":"v0.1.0#7c1ea9b4f2d3","draft":false,"sections":[
 {"section":"who-approves","heading":"Who approves","text":"A team member's claim goes to the approver named in its policy. Claims are submitted within 30 days of the receipt date.",
  "cites":[{"guide":"claims.approve-team","title":"Approving your team's claims","anchor":"who-approves"}]},
 {"section":"receipts","heading":"Receipts","text":"Every claim over 50 dollars needs a receipt.","cites":[{"guide":"claims.approve-team","title":"Approving your team's claims","anchor":"receipts","draft":true}]}]}`

func knowHowRun(t *testing.T, segments ...assistant.Segment) (assistant.Answer, *api) {
	t.Helper()
	b := &api{
		answers: []ports.ModelResponse{
			{Parts: []ports.ModelPart{toolCall("c1", "kb_read", `{"key":"claims.approve-team-claims"}`)}},
			answer(segments...),
			answer(segments...), // asked again when it fails: the same words again
		},
		results: map[string]assistant.ToolResult{"kb.read": {Tool: "kb.read", Version: 1, Facts: []aitool.Fact{}, Omitted: []aitool.Omission{},
			Content: json.RawMessage(claimsPage), KBPages: []assistant.KBPage{{Key: "claims.approve-team-claims", Version: "v0.1.0#7c1ea9b4f2d3"}}}},
	}
	a := &assistant.Assistant{Backend: b, Prompt: prompt(t)}
	out, err := a.Answer(context.Background(), assistant.Question{Token: "fyai_turn", Text: "How soon must a claim be submitted?"}, func(assistant.Event) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	return out, b
}

func knowHow(text, page, section string) assistant.Segment {
	return assistant.Segment{Text: text, Page: page, Section: section}
}

// A know-how sentence names the page it read this turn and the section it comes from, and may hold a number only if that
// section holds it as a whole token (AI-51, AI-53, AI-08): the answer shows its words and the guide page the section was
// compiled from, resolved by Fyniq, never the compiled page and never a source the model named.
// req: AI-51
// req: AI-53
// req: AI-08
func TestAKnowHowSentenceCitesTheGuidePageItsSectionCameFrom(t *testing.T) {
	out, _ := knowHowRun(t, knowHow("A claim is submitted within 30 days of the receipt date.", "claims.approve-team-claims", "who-approves"),
		knowHow("Ask the approver named in the policy.", "claims.approve-team-claims", "who-approves"))
	if out.Stop != assistant.StopAnswered || out.Grounding.Regenerated || out.Grounding.Withheld != 0 ||
		!strings.Contains(out.Text, "within 30 days") {
		t.Fatalf("the answer: %+v", out)
	}
	want := assistant.Source{Guide: "claims.approve-team", Title: "Approving your team's claims", Anchor: "who-approves"}
	if len(out.Sources) != 1 || out.Sources[0] != want {
		t.Errorf("the sources: %+v", out.Sources)
	}
	if got := out.Segments[0].Sources; len(got) != 1 || got[0] != want {
		t.Errorf("the segment's sources: %+v", got)
	}
	// A section that cites a draft guide page says so.
	out, _ = knowHowRun(t, knowHow("Every claim over 50 dollars needs a receipt.", "claims.approve-team-claims", "receipts"))
	if len(out.Sources) != 1 || !out.Sources[0].Draft {
		t.Errorf("a draft guide page's source: %+v", out.Sources)
	}
}

// A know-how sentence that cites a page it did not read, a section the read did not return, or a number the section does not
// hold is asked for again once and then withheld: what the person reads says the guide part could not be established, and
// the model's number never reaches them.
// req: AI-53
// req: AI-08
func TestAKnowHowSentenceThatFailsTheCheckIsWithheld(t *testing.T) {
	for name, seg := range map[string]assistant.Segment{
		"a number the section does not hold": knowHow("A claim is submitted within 45 days.", "claims.approve-team-claims", "who-approves"),
		"a number from another section":      knowHow("Every claim over 50 dollars needs a receipt.", "claims.approve-team-claims", "who-approves"),
		"a page not read":                    knowHow("Claims are paid on Friday.", "payments.batches", "when"),
		"a section not returned":             knowHow("Claims are paid on Friday.", "claims.approve-team-claims", "payment"),
		"no words":                           {Page: "claims.approve-team-claims", Section: "who-approves"},
	} {
		out, b := knowHowRun(t, seg)
		if out.Stop != assistant.StopAnswered || !out.Grounding.Regenerated || out.Grounding.Withheld != 1 || len(out.Sources) != 0 ||
			strings.Contains(out.Text, "45") || strings.Contains(out.Text, "Friday") || !strings.Contains(out.Text, "could not establish") {
			t.Errorf("%s: %+v", name, out)
		}
		last := b.calls[len(b.calls)-1].Request.Messages
		if !strings.Contains(last[len(last)-1].Parts[0].Text, "know-how") && !strings.Contains(last[len(last)-1].Parts[0].Text, "section") {
			t.Errorf("%s: the model was not told what failed: %v", name, last[len(last)-1].Parts[0].Text)
		}
	}
	// Words with a number and no page are the old rule: a figure in plain words is refused.
	out, _ := knowHowRun(t, assistant.Segment{Text: "A claim is submitted within 30 days."})
	if out.Grounding.Withheld != 1 || strings.Contains(out.Text, "30 days") {
		t.Errorf("a number in plain words: %+v", out)
	}
}

// The knowledge base's answers hold no figure of the books: a fact segment is still how a figure of the organisation is
// shown, and a know-how sentence beside it does not change that.
// req: AI-51
func TestKnowHowAndFiguresShareAnAnswer(t *testing.T) {
	b := &api{
		answers: []ports.ModelResponse{
			{Parts: []ports.ModelPart{toolCall("c1", "kb_read", `{"key":"claims.approve-team-claims"}`), toolCall("c2", "metrics_query", `{"measure":"revenue","from":"2026-07","to":"2026-09"}`)}},
			answer(knowHow("A claim is submitted within 30 days of the receipt date.", "claims.approve-team-claims", "who-approves"), words(" Revenue was "), cite(revenue), words(".")),
		},
		results: map[string]assistant.ToolResult{
			"kb.read":       {Tool: "kb.read", Version: 1, Facts: []aitool.Fact{}, Omitted: []aitool.Omission{}, Content: json.RawMessage(claimsPage)},
			"metrics.query": {Tool: "metrics.query", Version: 1, Facts: []aitool.Fact{revenue}},
		},
	}
	a := &assistant.Assistant{Backend: b, Prompt: prompt(t)}
	out, err := a.Answer(context.Background(), assistant.Question{Token: "fyai_turn", Text: "q"}, func(assistant.Event) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if out.Grounding.Withheld != 0 || !strings.Contains(out.Text, "within 30 days") || !strings.Contains(out.Text, "SGD 125,000.00") || len(out.Sources) != 1 {
		t.Errorf("the answer: %+v", out)
	}
}
