package assistant_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cognizhi/fyniq/internal/aitool"
	"github.com/cognizhi/fyniq/internal/assistant"
	"github.com/cognizhi/fyniq/internal/ports"
)

// api is a fake of the API's internal operations: the model answers in order, and each tool's result by name.
type api struct {
	answers []ports.ModelResponse
	results map[string]assistant.ToolResult
	refuse  map[string]*assistant.Problem
	calls   []assistant.ModelCall
	tools   []string
	tokens  []string
}

func (a *api) CallModel(_ context.Context, token string, call assistant.ModelCall) (assistant.ModelResult, error) {
	a.tokens = append(a.tokens, token)
	b, _ := json.Marshal(call) // a copy, as the request was sent
	var sent assistant.ModelCall
	_ = json.Unmarshal(b, &sent)
	a.calls = append(a.calls, sent)
	if p := a.refuse["model"]; p != nil {
		return assistant.ModelResult{}, p
	}
	r := a.answers[0]
	a.answers = a.answers[1:]
	return assistant.ModelResult{Run: "run-" + string(rune('a'+len(a.calls))), Response: r}, nil
}

func (a *api) CallTool(_ context.Context, token, tool string, _ json.RawMessage) (assistant.ToolResult, error) {
	a.tokens = append(a.tokens, token)
	a.tools = append(a.tools, tool)
	if p := a.refuse[tool]; p != nil {
		return assistant.ToolResult{}, p
	}
	return a.results[tool], nil
}

func toolCall(id, tool, input string) ports.ModelPart {
	return ports.ModelPart{Kind: ports.PartToolCall, CallID: id, Tool: tool, Input: json.RawMessage(input)}
}

func text(s string) ports.ModelResponse {
	return ports.ModelResponse{Parts: []ports.ModelPart{{Kind: ports.PartText, Text: s}}, StopReason: ports.StopEnd}
}

// answer is a structured answer of segments, as the model gives it.
func answer(segments ...assistant.Segment) ports.ModelResponse {
	b, _ := json.Marshal(assistant.Structured{Segments: segments})
	return text(string(b))
}

// words is a segment of words.
func words(s string) assistant.Segment { return assistant.Segment{Text: s} }

// cite is a fact segment saying what f is.
func cite(f aitool.Fact) assistant.Segment {
	return assistant.Segment{Fact: f.ID, Measure: f.Measure, Member: f.Member, From: f.From, To: f.To, Currency: f.Currency}
}

func prompt(t *testing.T) assistant.Prompt {
	t.Helper()
	p, err := assistant.DefaultPrompt(assistant.Agent)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

var revenue = aitool.Fact{ID: "F00000000000000aa", Measure: "revenue", Value: "125000.00", Unit: aitool.UnitAmount, Currency: "SGD", From: "2026-07-01",
	To: "2026-09-30", Revision: 7}
var costs = aitool.Fact{ID: "F00000000000000bb", Measure: "costs", Value: "100000.00", Unit: aitool.UnitAmount, Currency: "SGD", From: "2026-07-01",
	To: "2026-09-30", Revision: 7}

// The tool loop (AI-P2, AI-28, design D3): the assistant asks the model through the API with the release's prompt and
// the registry's tools by name, runs each tool the model calls through the API with the turn's token (calc it runs
// itself, on the facts it read), gives the model the results as tool results, never as instructions, and answers when
// the model does: the answer keeps every fact the turn read, the prompt's name and hash (AI-24), the runs and the tool
// calls, and streams its steps.
// req: AI-P2
// req: AI-28
// req: AI-24
// req: ai/proposals The orchestrator holds no write credential
func TestTheToolLoopReadsThroughTheAPI(t *testing.T) {
	known := func(id string) (aitool.Fact, bool) {
		return map[string]aitool.Fact{revenue.ID: revenue, costs.ID: costs}[id], id == revenue.ID || id == costs.ID
	}
	diff, err := aitool.Calc(aitool.CalcInput{Op: "subtract", Operands: []aitool.Operand{{Fact: revenue.ID}, {Fact: costs.ID}}}, known)
	if err != nil {
		t.Fatal(err)
	}
	b := &api{
		answers: []ports.ModelResponse{
			{Parts: []ports.ModelPart{toolCall("c1", "metrics_query", `{"measure":"revenue","from":"2026-07","to":"2026-09"}`),
				toolCall("c2", "metrics_query", `{"measure":"costs","from":"2026-07","to":"2026-09"}`)}},
			{Parts: []ports.ModelPart{toolCall("c3", "calc", `{"op":"subtract","operands":[{"fact":"F00000000000000aa"},{"fact":"F00000000000000bb"}]}`)}},
			answer(words("Revenue less costs was "), cite(diff), words(" from July to September 2026.")),
		},
		results: map[string]assistant.ToolResult{"metrics.query": {Tool: "metrics.query", Version: 1, Facts: []aitool.Fact{revenue, costs}}},
	}
	p := prompt(t)
	a := &assistant.Assistant{Backend: b, Prompt: p}
	var kinds []string
	out, err := a.Answer(context.Background(), assistant.Question{Token: "fyai_turn", Text: "What did we make after costs last quarter?",
		Context: "the profit and loss for July to September"}, func(e assistant.Event) error {
		kinds = append(kinds, e.Kind)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Text != "Revenue less costs was SGD 25,000.00 from July to September 2026." || out.Stop != assistant.StopAnswered || len(out.Runs) != 3 ||
		out.Grounding.Regenerated || !slices.Equal(out.Cited, []string{diff.ID}) {
		t.Errorf("the answer: %+v", out)
	}
	if !slices.Equal(kinds, []string{"status", "tool", "tool", "tool", "text", "done"}) {
		t.Errorf("the events: %v", kinds)
	}
	if !slices.Equal(b.tools, []string{"metrics.query", "metrics.query"}) || slices.ContainsFunc(b.tokens, func(s string) bool { return s != "fyai_turn" }) {
		t.Errorf("the tools the API ran, and the tokens: %v %v", b.tools, b.tokens)
	}
	var calc *aitool.Fact
	for i, f := range out.Facts {
		if f.Measure == "calc.subtract" {
			calc = &out.Facts[i]
		}
	}
	if len(out.Facts) != 3 || calc == nil || calc.Value != "25000.00" || !slices.Equal(calc.Inputs, []string{revenue.ID, costs.ID}) {
		t.Errorf("the facts: %+v", out.Facts)
	}
	if out.Prompt != "chat@2" || out.PromptSHA256 != p.SHA256 || len(out.ToolCalls) != 3 || out.ToolCalls[2].Tool != "calc" || out.ToolCalls[0].Facts != 2 {
		t.Errorf("the record: %+v", out)
	}

	// What the model was sent: the prompt as the system, the registry's tools by name, the person's words with what they
	// asked about, and the results as tool results under their calls.
	first, last := b.calls[0], b.calls[2]
	var registry []string
	for _, tool := range aitool.Default().Tools() {
		registry = append(registry, tool.Name)
	}
	if first.Agent != "chat" || first.Request.System != p.Text || !slices.Equal(first.Request.Tools, registry) || first.Request.MaxTokens != assistant.DefaultMaxTokens {
		t.Errorf("the first call: %+v", first)
	}
	if q := first.Request.Messages[0]; q.Role != ports.RoleUser || !strings.Contains(q.Parts[0].Text, "the profit and loss for July to September") {
		t.Errorf("the question: %+v", q)
	}
	results := last.Request.Messages[2]
	if results.Role != ports.RoleUser || len(results.Parts) != 2 || results.Parts[0].Kind != ports.PartToolResult || results.Parts[0].CallID != "c1" ||
		!strings.Contains(results.Parts[0].Text, revenue.ID) || strings.Contains(last.Request.System, revenue.ID) {
		t.Errorf("the tool results: %+v", results)
	}
}

// A refused read, a tool the model invents, a calc on a fact the turn did not read and too many reads each go back to
// the model or the person in words; a refusal of the model call itself is the API's problem; an NRIC in an answer is
// masked on its way out, and one in an earlier question on its way back to the model (AI-20, CTL-AI-06).
// req: AI-P2
// req: AI-20
// req: CTL-AI-06
func TestTheToolLoopSaysWhatWentWrong(t *testing.T) {
	refused := &assistant.Problem{Status: 422, Code: "AI-TOOL-03", Detail: "Your roles hold no V right on \"Invoices, credit notes\".", Fix: "Ask an Owner."}
	b := &api{
		answers: []ports.ModelResponse{
			{Parts: []ports.ModelPart{toolCall("c1", "records_get", `{"kind":"invoice","id":"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b"}`),
				toolCall("c2", "records_update", `{}`), toolCall("c3", "calc", `{"op":"add","operands":[{"fact":"F00000000000000ff"},{"constant":"1"}]}`)}},
			answer(words("I could not read it. The customer's NRIC is S1234567D.")),
			answer(words("I could not read it. The customer's NRIC is S1234567D.")),
		},
		refuse: map[string]*assistant.Problem{"records.get": refused},
	}
	out, err := (&assistant.Assistant{Backend: b, Prompt: prompt(t)}).Answer(context.Background(), assistant.Question{Token: "fyai_t", Text: "Show me INV-0012"},
		func(assistant.Event) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	parts := b.calls[1].Request.Messages[2].Parts
	if len(parts) != 3 || !parts[0].IsError || !strings.Contains(parts[0].Text, "no V right") || !parts[1].IsError || !parts[2].IsError ||
		!strings.Contains(parts[2].Text, aitool.CodeCalcFact) && !strings.Contains(parts[2].Text, "no fact") {
		t.Errorf("the errors the model read: %+v", parts)
	}
	if out.ToolCalls[0].Outcome != assistant.OutcomeRefused || out.ToolCalls[0].Code != "AI-TOOL-03" || out.ToolCalls[1].Code != "AI-TOOL-01" {
		t.Errorf("the tool calls: %+v", out.ToolCalls)
	}
	if strings.Contains(out.Text, "1234567") || !out.Grounding.Regenerated || out.Grounding.Withheld != 1 {
		t.Errorf("the answer's NRIC: %q %+v", out.Text, out.Grounding)
	}

	// An NRIC and an account number in an earlier question, kept as typed, reach the model masked with a later one.
	b = &api{answers: []ports.ModelResponse{answer(words("Noted."))}}
	history := []ports.ModelMessage{{Role: ports.RoleUser, Parts: []ports.ModelPart{{Kind: ports.PartText, Text: "Pay S1234567D at 012-345678-9"}}},
		{Role: ports.RoleAssistant, Parts: []ports.ModelPart{{Kind: ports.PartText, Text: "I cannot pay anyone."}}}}
	if _, err := (&assistant.Assistant{Backend: b, Prompt: prompt(t)}).Answer(context.Background(), assistant.Question{Token: "fyai_t", Text: "And now?",
		History: history}, func(assistant.Event) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if sent, _ := json.Marshal(b.calls[0].Request.Messages); strings.Contains(string(sent), "1234567D") || strings.Contains(string(sent), "345678") ||
		!strings.Contains(string(sent), "S****567D") || history[0].Parts[0].Text != "Pay S1234567D at 012-345678-9" {
		t.Errorf("the earlier question as the model read it: %s", sent)
	}

	many := make([]ports.ModelPart, assistant.DefaultToolCalls+1)
	for i := range many {
		many[i] = toolCall("c", "checks_list", `{}`)
	}
	b = &api{answers: []ports.ModelResponse{{Parts: many}}}
	out, err = (&assistant.Assistant{Backend: b, Prompt: prompt(t)}).Answer(context.Background(), assistant.Question{Token: "fyai_t", Text: "Everything"},
		func(assistant.Event) error { return nil })
	if err != nil || out.Stop != assistant.StopToolLimit || len(b.tools) != 0 {
		t.Errorf("too many reads: %v %+v %v", err, out, b.tools)
	}

	off := &assistant.Problem{Status: 409, Code: "AI-ROUTE-05", Detail: "The AI is switched off."}
	b = &api{refuse: map[string]*assistant.Problem{"model": off}}
	_, err = (&assistant.Assistant{Backend: b, Prompt: prompt(t)}).Answer(context.Background(), assistant.Question{Token: "fyai_t", Text: "Revenue?"},
		func(assistant.Event) error { return nil })
	if p := (*assistant.Problem)(nil); !errors.As(err, &p) || p.Code != "AI-ROUTE-05" {
		t.Errorf("the AI switched off: %v", err)
	}
}

// The prompts are versioned files (AI-24, design D10): the latest version of an agent's prompt is the one a turn is
// told, named and hashed as its file is; a file not named <agent>@<version>.md is refused.
// req: AI-24
func TestThePromptIsTheLatestVersionHashed(t *testing.T) {
	b, err := os.ReadFile("../../contracts/ai/prompts/chat@2.md")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	if p := prompt(t); p.Version != 2 || p.Text != string(b) || p.SHA256 != hex.EncodeToString(sum[:]) || p.Name() != "chat@2" {
		t.Errorf("chat's prompt: %s %d", p.Name(), len(p.Text))
	}
	fsys := fstest.MapFS{"prompts/chat@1.md": {Data: []byte("one")}, "prompts/chat@12.md": {Data: []byte("twelve")}, "prompts/other@3.md": {Data: []byte("x")}}
	if p, err := assistant.LoadPrompt(fsys, "chat"); err != nil || p.Version != 12 || p.Text != "twelve" {
		t.Errorf("the latest: %+v %v", p, err)
	}
	if _, err := assistant.LoadPrompt(fstest.MapFS{"prompts/chat-1.md": {Data: []byte("x")}}, "chat"); err == nil {
		t.Error("a prompt with no version loaded")
	}
	if _, err := assistant.LoadPrompt(fsys, "secretary"); err == nil {
		t.Error("an agent with no prompt loaded")
	}
}

// Figures are bound to their facts (AI-08, AI-21; G4-089, G4-090): asked for August revenue from customer A, with B's
// August and A's July read too, an answer citing B's fact as A's, or July's as August's, fails and is asked for once
// more with the failures named; one that still fails withholds the figure and says it could not be established; words
// that hold a figure fail the same way; a figure is rendered from its fact, never from the model's words.
// req: AI-08
// req: AI-21
func TestFiguresAreBoundToTheirFacts(t *testing.T) {
	a := aitool.Fact{ID: "F000000000000000a", Measure: "receivables", Value: "1250.5", Unit: aitool.UnitAmount, Currency: "SGD", From: "2026-08-31",
		To: "2026-08-31", Dimension: "customer", Member: "cust-a", MemberName: "Acme Pte Ltd", Revision: 3}
	bFact := a
	bFact.ID, bFact.Member, bFact.MemberName = "F000000000000000b", "cust-b", "Beta Pte Ltd"
	july := a
	july.ID, july.From, july.To = "F000000000000000c", "2026-07-31", "2026-07-31"
	read := ports.ModelResponse{Parts: []ports.ModelPart{toolCall("c1", "metrics_query", `{"measure":"receivables","by":"customer"}`)}}
	results := map[string]assistant.ToolResult{"metrics.query": {Tool: "metrics.query", Version: 1, Facts: []aitool.Fact{a, bFact, july}}}
	misattributed := cite(bFact)
	misattributed.Member = "cust-a"
	ask := func(answers ...ports.ModelResponse) (assistant.Answer, *api) {
		t.Helper()
		b := &api{answers: append([]ports.ModelResponse{read}, answers...), results: results}
		out, err := (&assistant.Assistant{Backend: b, Prompt: prompt(t)}).Answer(context.Background(), assistant.Question{Token: "fyai_t",
			Text: "What does Acme owe us at the end of August?"}, func(assistant.Event) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		return out, b
	}

	// Right the first time: rendered from the fact, with its currency's decimals.
	out, b := ask(answer(words("Acme owes "), cite(a), words(" at 31 August 2026.")))
	if out.Text != "Acme owes SGD 1,250.50 at 31 August 2026." || out.Grounding.Regenerated || b.calls[1].Request.Output == nil {
		t.Errorf("a grounded answer: %q %+v", out.Text, out.Grounding)
	}

	// B's fact given as A's, then right: asked for again with the failure named.
	out, b = ask(answer(words("Acme owes "), misattributed), answer(words("Acme owes "), cite(a)))
	last := b.calls[len(b.calls)-1].Request.Messages
	if !out.Grounding.Regenerated || len(out.Grounding.Failures) != 1 || !strings.Contains(out.Grounding.Failures[0], "member") ||
		!strings.Contains(last[len(last)-1].Parts[0].Text, "member") || out.Text != "Acme owes SGD 1,250.50" {
		t.Errorf("a misattributed figure, then right: %q %+v", out.Text, out.Grounding)
	}

	// July's given as August's, twice: withheld.
	wrongMonth := cite(july)
	wrongMonth.From, wrongMonth.To = "2026-08-31", "2026-08-31"
	out, _ = ask(answer(words("Acme owes "), wrongMonth), answer(words("Acme owes "), wrongMonth))
	if out.Grounding.Withheld != 1 || strings.Contains(out.Text, "1,250.50") || !strings.Contains(out.Text, "could not establish") {
		t.Errorf("a figure that stays wrong: %q %+v", out.Text, out.Grounding)
	}

	// A figure in the words, an unknown fact, and an answer not in its shape.
	for name, bad := range map[string]ports.ModelResponse{
		"a figure in words": answer(words("Acme owes 1,250.50 at 31 August 2026.")),
		"an unknown fact":   answer(words("Acme owes "), assistant.Segment{Fact: "F00000000000000ff", Measure: "receivables"}),
		"not its shape":     text("Acme owes SGD 1,250.50."),
	} {
		out, _ := ask(bad, bad)
		if !out.Grounding.Regenerated || out.Grounding.Withheld == 0 && name != "not its shape" || strings.Contains(out.Text, "1,250") {
			t.Errorf("%s: %q %+v", name, out.Text, out.Grounding)
		}
	}
}

// A figure as the answer shows it (RND-DISPLAY) and the Markdown subset (AI-69): images and raw HTML go, links to other
// sites show their address in full, Fyniq's own links stay.
// req: AI-69
// req: AI-08
func TestTheAnswerIsRenderedInFyniqsWords(t *testing.T) {
	for _, c := range []struct {
		f    aitool.Fact
		want string
	}{
		{aitool.Fact{Value: "1234567.891", Unit: aitool.UnitAmount, Currency: "SGD"}, "SGD 1,234,567.89"},
		{aitool.Fact{Value: "-500", Unit: aitool.UnitAmount, Currency: "JPY"}, "JPY −500"},
		{aitool.Fact{Value: "45.25", Unit: "percent"}, "45.3%"},
		{aitool.Fact{Value: "18.04", Unit: "months"}, "18.0 months"},
		{aitool.Fact{Value: "1.2", Unit: "ratio"}, "1.20×"},
		{aitool.Fact{Value: "12", Unit: aitool.UnitCount}, "12"},
		{aitool.Fact{Words: "Not burning", Unit: "months"}, "Not burning"},
	} {
		if got := assistant.Figure(c.f); got != c.want {
			t.Errorf("Figure(%+v) = %q, want %q", c.f, got, c.want)
		}
	}
	for in, want := range map[string]string{
		"See ![chart](https://evil.example/c?d=x) here":            "See  here",
		"Read [the guide](https://example.com/guide)":              "Read the guide (https://example.com/guide)",
		"Open [the invoice](/sales/invoices/01928f6e)":             "Open [the invoice](/sales/invoices/01928f6e)",
		"Not [this](//evil.example/x)":                             "Not this (//evil.example/x)",
		"<b>bold</b> and <img src=x onerror=y> **kept**":           "bold and  **kept**",
		"- a list\n- of points\n\n| a | b |\n|---|---|\n| c | d |": "- a list\n- of points\n\n| a | b |\n|---|---|\n| c | d |",
	} {
		if got := assistant.Markdown(in); got != want {
			t.Errorf("Markdown(%q) = %q, want %q", in, got, want)
		}
	}
}
