// Package assistant is the AI service's orchestrator (PRD-001 §9.3, AI-P2, AI-28; p1-ai-assistant design D3): the
// tool loop that answers a turn. It is stateless and holds no credential of its own to the books: no database, no
// provider key. Everything it reads and every model call it makes goes through the API's internal operations with the
// turn's delegated token, as the person the turn is for (design D4), so the router, the budgets, the exclusions and the
// screening are the API's. It runs calc itself (AI-P6), keeps every fact the turn read, filters what it sends a model
// and what it answers (AI-20), and records the prompt's hash on the answer (AI-24).
package assistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/aitool"
	"github.com/cognizhi/fyniq/internal/domain/aiexclusion"
	"github.com/cognizhi/fyniq/internal/domain/aipolicy"
	"github.com/cognizhi/fyniq/internal/ports"
)

// Class is the class of the service tokens the API calls the AI service with.
const Class = "assistant"

// Agent is the agent the assistant's model calls are routed for (AI-74).
const Agent = "chat"

// The limits of a turn, until the settings of design D9 (task 5.3).
const (
	DefaultToolCalls = 8    // tool calls a turn
	DefaultMaxTokens = 2048 // an answer's output
)

// ModelCall is a model call the AI service asks the API for (callAIModel): the agent, the classes of data it carries,
// and the request, whose model the route chooses.
type ModelCall struct {
	Agent   string       `json:"agent"`
	Classes []string     `json:"classes"`
	Request ModelRequest `json:"request"`
}

// ModelRequest is a model call's request: the system prompt, the conversation, the registry's tools by name and the
// output's limit.
type ModelRequest struct {
	System    string               `json:"system,omitempty"`
	Messages  []ports.ModelMessage `json:"messages"`
	Tools     []string             `json:"tools,omitempty"`
	Output    *ports.ModelSchema   `json:"output,omitempty"`
	MaxTokens int                  `json:"max_tokens"`
}

// ModelResult is a served model call: its run in the activity log, the step and connection that served it, the classes
// it was routed for, and the answer.
type ModelResult struct {
	Run        string              `json:"run"`
	Step       int                 `json:"step"`
	Connection string              `json:"connection_id"`
	Classes    []string            `json:"classes"`
	Response   ports.ModelResponse `json:"response"`
}

// ToolResult is what a read tool read through the API (callAITool): its facts, the rest of what it read with each
// figure replaced by its fact, what its limits left out, and what screening withheld.
type ToolResult struct {
	Tool     string            `json:"tool"`
	Version  int               `json:"version"`
	Facts    []aitool.Fact     `json:"facts"`
	Content  json.RawMessage   `json:"content,omitempty"`
	Omitted  []aitool.Omission `json:"omitted"`
	Revision int64             `json:"ledger_revision"`
	Withheld string            `json:"withheld,omitempty"`
}

// Problem is the API's refusal, in its words: its status, code, what went wrong and how to fix it.
type Problem struct {
	Status int    `json:"status"`
	Code   string `json:"code"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
}

func (p *Problem) Error() string { return p.Code + ": " + p.Detail }

// Backend is the API's internal operations, as the AI service calls them for a turn with its delegated token.
type Backend interface {
	CallModel(ctx context.Context, token string, call ModelCall) (ModelResult, error)
	CallTool(ctx context.Context, token string, tool string, input json.RawMessage) (ToolResult, error)
}

// Question is a turn to answer: the turn's delegated token, the person's words, what they asked about (FR-UX-17), the
// conversation's earlier turns as the API sends them, the facts those turns cited, which this turn may cite again
// (AI-08: a figure from an earlier turn keeps its fact ID), and the most reads it may make (design D9).
type Question struct {
	Token    string               `json:"token"`
	Text     string               `json:"question"`
	Context  string               `json:"context,omitempty"`
	History  []ports.ModelMessage `json:"history,omitempty"`
	Earlier  []aitool.Fact        `json:"earlier_facts,omitempty"`
	MaxReads int                  `json:"max_reads,omitempty"`
}

// The kinds of an event the assistant streams while it answers.
const (
	EventStatus = "status" // what it is doing, such as "Reading the books"
	EventTool   = "tool"   // a tool it ran, with the facts it read
	EventText   = "text"   // the answer's text
	EventDone   = "done"   // the whole answer
)

// Event is one step of an answer as it streams.
type Event struct {
	Kind   string        `json:"kind"`
	Text   string        `json:"text,omitempty"`
	Tool   string        `json:"tool,omitempty"`
	Facts  []aitool.Fact `json:"facts,omitempty"`
	Answer *Answer       `json:"answer,omitempty"`
}

// The outcomes of a tool call.
const (
	OutcomeRead    = "read"
	OutcomeRefused = "refused"
)

// ToolCall is one tool call of the turn, as the turn records it (design D11).
type ToolCall struct {
	Tool     string          `json:"tool"`
	Version  int             `json:"version"`
	Input    json.RawMessage `json:"input"`
	Outcome  string          `json:"outcome"`
	Code     string          `json:"code,omitempty"`
	Facts    int             `json:"facts"`
	Omitted  int             `json:"omitted,omitempty"`
	Withheld bool            `json:"withheld,omitempty"`
	Duration time.Duration   `json:"duration_ns"`
	// OutputSHA256 is the hash of what the model read of it.
	OutputSHA256 string `json:"output_sha256,omitempty"`
}

// How a turn ends.
const (
	StopAnswered  = "answered"
	StopToolLimit = "tool_limit"
)

// Grounding is how the answer's figures were checked (AI-08): whether it was asked for again, what failed the first
// time, and how many figures or words were withheld after.
type Grounding struct {
	Regenerated bool     `json:"regenerated"`
	Failures    []string `json:"failures,omitempty"`
	Withheld    int      `json:"withheld"`
}

// Answer is a turn's answer: its text, every fact it read, the prompt it was told (AI-24), the model calls' runs in the
// activity log, its tool calls and how it ended.
type Answer struct {
	Text         string           `json:"text"`
	Segments     []Segment        `json:"segments"`
	Cited        []string         `json:"cited"`
	Grounding    Grounding        `json:"grounding"`
	Facts        []aitool.Fact    `json:"facts"`
	Prompt       string           `json:"prompt"`
	PromptSHA256 string           `json:"prompt_sha256"`
	Runs         []string         `json:"runs"`
	ToolCalls    []ToolCall       `json:"tool_calls"`
	Usage        ports.ModelUsage `json:"usage"`
	Stop         string           `json:"stop"`
}

// Assistant answers turns.
type Assistant struct {
	Backend      Backend
	Prompt       Prompt
	Tools        *aitool.Registry // nil: the release's
	MaxToolCalls int              // 0: DefaultToolCalls
	MaxTokens    int              // 0: DefaultMaxTokens
	Clock        ports.Clock
}

// Answer answers q, calling emit with each step as it goes. A refusal of the API, such as the AI switched off, a spent
// budget or a screening finding, is its *Problem.
func (a *Assistant) Answer(ctx context.Context, q Question, emit func(Event) error) (Answer, error) {
	tools := a.Tools
	if tools == nil {
		tools = aitool.Default()
	}
	limit, maxTokens := cmpOr(q.MaxReads, cmpOr(a.MaxToolCalls, DefaultToolCalls)), cmpOr(a.MaxTokens, DefaultMaxTokens)
	var names []string
	for _, t := range tools.Tools() {
		names = append(names, t.Name)
	}
	out := Answer{Prompt: a.Prompt.Name(), PromptSHA256: a.Prompt.SHA256, Facts: []aitool.Fact{}, Runs: []string{}, ToolCalls: []ToolCall{},
		Segments: []Segment{}, Cited: []string{}}
	facts, earlier := map[string]aitool.Fact{}, map[string]aitool.Fact{}
	for _, f := range q.Earlier {
		earlier[f.ID] = f
	}
	known := func(id string) (aitool.Fact, bool) {
		if f, ok := facts[id]; ok {
			return f, true
		}
		f, ok := earlier[id]
		return f, ok
	}
	keep := func(fs []aitool.Fact) {
		for _, f := range fs {
			if _, ok := facts[f.ID]; !ok {
				facts[f.ID] = f
				out.Facts = append(out.Facts, f)
			}
		}
	}
	question := q.Text
	if q.Context != "" {
		question = "About " + q.Context + ":\n\n" + q.Text
	}
	messages := append(filtered(q.History), ports.ModelMessage{Role: ports.RoleUser, Parts: []ports.ModelPart{{Kind: ports.PartText,
		Text: aiexclusion.Filter(question)}}})
	if err := emit(Event{Kind: EventStatus, Text: "Reading the books"}); err != nil {
		return out, err
	}
	for calls := 0; ; {
		res, err := a.Backend.CallModel(ctx, q.Token, ModelCall{Agent: Agent, Classes: []string{aipolicy.Business},
			Request: ModelRequest{System: a.Prompt.Text, Messages: messages, Tools: names, Output: answerOutput, MaxTokens: maxTokens}})
		if err != nil {
			return out, err
		}
		out.Runs = append(out.Runs, res.Run)
		out.Usage = addUsage(out.Usage, res.Response.Usage)
		messages = append(messages, ports.ModelMessage{Role: ports.RoleAssistant, Parts: res.Response.Parts})
		var asked []ports.ModelPart
		for _, p := range res.Response.Parts {
			if p.Kind == ports.PartToolCall {
				asked = append(asked, p)
			}
		}
		if len(asked) == 0 {
			s, fails := parse(textOf(res.Response), known)
			if len(fails) > 0 && !out.Grounding.Regenerated {
				// Asked for once more, with what failed (AI-08).
				out.Grounding.Regenerated, out.Grounding.Failures = true, fails
				messages = append(messages, ports.ModelMessage{Role: ports.RoleUser, Parts: []ports.ModelPart{{Kind: ports.PartText,
					Text: "Your answer did not pass the figures' check: " + strings.Join(fails, "; ") + ". Answer again in the same shape, " +
						"each figure a fact segment that says what the fact is, or say what could not be established."}}})
				continue
			}
			if len(fails) > 0 {
				s, out.Grounding.Withheld = withhold(s, known)
			}
			text, cited := Render(s, known)
			out.Segments, out.Cited, out.Text, out.Stop = s.Segments, cited, aiexclusion.Filter(text), StopAnswered
			for _, id := range cited { // an earlier turn's fact cited again is kept with this turn too
				if _, read := facts[id]; !read {
					if f, ok := earlier[id]; ok {
						facts[id], out.Facts = f, append(out.Facts, f)
					}
				}
			}
			break
		}
		if calls+len(asked) > limit {
			out.Text, out.Stop = fmt.Sprintf("I could not finish: answering needed more than %d reads of the books. Ask about one thing at a time.",
				limit), StopToolLimit
			break
		}
		results := make([]ports.ModelPart, 0, len(asked))
		for _, call := range asked {
			calls++
			part, record, read := a.runTool(ctx, q.Token, tools, call, known)
			keep(read)
			out.ToolCalls = append(out.ToolCalls, record)
			results = append(results, part)
			if err := emit(Event{Kind: EventTool, Tool: record.Tool, Facts: read}); err != nil {
				return out, err
			}
		}
		messages = append(messages, ports.ModelMessage{Role: ports.RoleUser, Parts: results})
	}
	if err := emit(Event{Kind: EventText, Text: out.Text}); err != nil {
		return out, err
	}
	return out, emit(Event{Kind: EventDone, Answer: &out})
}

// runTool runs one tool call the model made: calc here, every other tool through the API. A refusal goes back to the
// model as an error in the API's words, for it to say why or to try another way.
func (a *Assistant) runTool(ctx context.Context, token string, tools *aitool.Registry, call ports.ModelPart,
	known func(string) (aitool.Fact, bool)) (ports.ModelPart, ToolCall, []aitool.Fact) {
	start := a.now()
	record := ToolCall{Tool: call.Tool, Input: call.Input, Outcome: OutcomeRead}
	failed := func(code, words string) (ports.ModelPart, ToolCall, []aitool.Fact) {
		record.Outcome, record.Code, record.Duration = OutcomeRefused, code, a.now().Sub(start)
		return ports.ModelPart{Kind: ports.PartToolResult, CallID: call.CallID, Tool: call.Tool, IsError: true, Text: aiexclusion.Filter(words)}, record, nil
	}
	tool, ok := tools.ByModelName(call.Tool)
	if !ok {
		return failed("AI-TOOL-01", call.Tool+" is not a tool you have. Use one of the tools you were given.")
	}
	record.Tool, record.Version = tool.Name, tool.Version
	var result ToolResult
	switch tool.Runs {
	case aitool.RunsAI:
		var in aitool.CalcInput
		if err := json.Unmarshal(call.Input, &in); err != nil {
			return failed(aitool.CodeCalcOperands, "calc's input is not what it takes: "+err.Error())
		}
		f, err := aitool.Calc(in, known)
		var ce *aitool.CalcError
		if errors.As(err, &ce) {
			return failed(ce.Code, ce.Message+". "+ce.Fix+".")
		}
		if err != nil {
			return failed("CALC", err.Error())
		}
		result = ToolResult{Tool: tool.Name, Version: tool.Version, Facts: []aitool.Fact{f}, Omitted: []aitool.Omission{}}
	default:
		var err error
		result, err = a.Backend.CallTool(ctx, token, tool.Name, call.Input)
		var p *Problem
		if errors.As(err, &p) {
			return failed(p.Code, strings.TrimSpace(p.Detail+" "+p.Fix))
		}
		if err != nil {
			return failed("AI-TOOL", "The read did not finish. Say the books could not be read just now.")
		}
	}
	record.Facts, record.Omitted, record.Withheld, record.Duration = len(result.Facts), len(result.Omitted), result.Withheld != "", a.now().Sub(start)
	text, err := resultText(result)
	if err != nil {
		return failed("AI-TOOL", "The read's result could not be read.")
	}
	sum := sha256.Sum256([]byte(text))
	record.OutputSHA256 = hex.EncodeToString(sum[:])
	return ports.ModelPart{Kind: ports.PartToolResult, CallID: call.CallID, Tool: call.Tool, Text: text}, record, result.Facts
}

// resultText is a tool's result as the model reads it: its facts, its content and what was left out or withheld, each
// text passed through the output filter again (AI-20).
func resultText(r ToolResult) (string, error) {
	var content any
	if len(r.Content) > 0 {
		var err error
		if content, err = aitool.Decode(r.Content); err != nil {
			return "", err
		}
	}
	b, err := json.Marshal(map[string]any{"facts": r.Facts, "content": content, "omitted": r.Omitted, "withheld": r.Withheld})
	if err != nil {
		return "", err
	}
	tree, err := aitool.Decode(b)
	if err != nil {
		return "", err
	}
	b, err = json.Marshal(aiexclusion.Apply(tree, aiexclusion.Policy{Payroll: true}))
	return string(b), err
}

// parse reads the model's structured answer and checks it (AI-08): an answer not in its shape fails as a whole.
func parse(text string, known func(string) (aitool.Fact, bool)) (Structured, []string) {
	var s Structured
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &s); err != nil {
		return Structured{Segments: []Segment{{Missing: "an answer to the question"}}},
			[]string{"the answer is not in its shape (an object with segments): " + err.Error()}
	}
	return s, failures(s, known)
}

// textOf is an answer's words.
func textOf(r ports.ModelResponse) string {
	var parts []string
	for _, p := range r.Parts {
		if p.Kind == ports.PartText && p.Text != "" {
			parts = append(parts, p.Text)
		}
	}
	return strings.Join(parts, "\n\n")
}

func addUsage(a, b ports.ModelUsage) ports.ModelUsage {
	a.Input, a.Output, a.CacheRead, a.CacheWrite, a.Reasoning = a.Input+b.Input, a.Output+b.Output, a.CacheRead+b.CacheRead,
		a.CacheWrite+b.CacheWrite, a.Reasoning+b.Reasoning
	return a
}

func (a *Assistant) now() time.Time {
	if a.Clock == nil {
		return time.Time{}
	}
	return a.Clock.Now()
}

func cmpOr(v, d int) int {
	if v > 0 {
		return v
	}
	return d
}

// filtered are the conversation's earlier messages as the model reads them: every text through the output filter, as the
// question is (AI-20, CTL-AI-06). The API keeps each question as its person typed it, so an NRIC or account number in
// an earlier question never reaches the model with a later one.
func filtered(history []ports.ModelMessage) []ports.ModelMessage {
	out := make([]ports.ModelMessage, len(history))
	for i, m := range history {
		m.Parts = slices.Clone(m.Parts)
		for j := range m.Parts {
			m.Parts[j].Text = aiexclusion.Filter(m.Parts[j].Text)
		}
		out[i] = m
	}
	return out
}
