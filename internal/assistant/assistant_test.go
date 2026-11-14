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
	b := &api{
		answers: []ports.ModelResponse{
			{Parts: []ports.ModelPart{toolCall("c1", "metrics_query", `{"measure":"revenue","from":"2026-07","to":"2026-09"}`),
				toolCall("c2", "metrics_query", `{"measure":"costs","from":"2026-07","to":"2026-09"}`)}},
			{Parts: []ports.ModelPart{toolCall("c3", "calc", `{"op":"subtract","operands":[{"fact":"F00000000000000aa"},{"fact":"F00000000000000bb"}]}`)}},
			text("Revenue less costs was 25,000.00 in July to September."),
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
	if out.Text != "Revenue less costs was 25,000.00 in July to September." || out.Stop != assistant.StopAnswered || len(out.Runs) != 3 {
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
	if out.Prompt != "chat@1" || out.PromptSHA256 != p.SHA256 || len(out.ToolCalls) != 3 || out.ToolCalls[2].Tool != "calc" || out.ToolCalls[0].Facts != 2 {
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
// masked on its way out (AI-20).
// req: AI-P2
// req: AI-20
func TestTheToolLoopSaysWhatWentWrong(t *testing.T) {
	refused := &assistant.Problem{Status: 422, Code: "AI-TOOL-03", Detail: "Your roles hold no V right on \"Invoices, credit notes\".", Fix: "Ask an Owner."}
	b := &api{
		answers: []ports.ModelResponse{
			{Parts: []ports.ModelPart{toolCall("c1", "records_get", `{"kind":"invoice","id":"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b"}`),
				toolCall("c2", "records_update", `{}`), toolCall("c3", "calc", `{"op":"add","operands":[{"fact":"F00000000000000ff"},{"constant":"1"}]}`)}},
			text("I could not read it. The customer's NRIC is S1234567D."),
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
	if strings.Contains(out.Text, "S1234567D") || !strings.Contains(out.Text, "S****567D") {
		t.Errorf("the answer's NRIC: %q", out.Text)
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
	b, err := os.ReadFile("../../contracts/ai/prompts/chat@1.md")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	if p := prompt(t); p.Version != 1 || p.Text != string(b) || p.SHA256 != hex.EncodeToString(sum[:]) || p.Name() != "chat@1" {
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
