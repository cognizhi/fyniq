package porttest

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/ports"
)

// Scripted is the model every model provider's fake plays, so one conformance suite proves each adapter (AI-71): the
// fake decodes its provider's request into a ports.ModelRequest, asks Scripted for the answer, and encodes that in
// its provider's form. A tool's result is answered "The tool said: …"; a request with tools whose question says "use
// the tool" calls the first tool with {"report":"pl"}; a request with an output schema answers {"text":"ok"};
// anything else is echoed with the count of images and PDFs it carried. Usage counts cache reads when caching was
// asked.
func Scripted(r ports.ModelRequest) ports.ModelResponse {
	resp := ports.ModelResponse{Model: r.Model, StopReason: ports.StopEnd, Usage: ports.ModelUsage{Input: 100, Output: 10}}
	if r.Cache {
		resp.Usage.CacheRead = 80
	}
	var last ports.ModelMessage
	if n := len(r.Messages); n > 0 {
		last = r.Messages[n-1]
	}
	var text []string
	images, documents := 0, 0
	for _, p := range last.Parts {
		switch p.Kind {
		case ports.PartToolResult:
			resp.Parts = []ports.ModelPart{{Kind: ports.PartText, Text: "The tool said: " + p.Text}}
			return resp
		case ports.PartText:
			text = append(text, p.Text)
		case ports.PartImage:
			images++
		case ports.PartDocument:
			documents++
		}
	}
	question := strings.Join(text, " ")
	switch {
	case len(r.Tools) > 0 && strings.Contains(question, "use the tool"):
		resp.StopReason = ports.StopToolCall
		resp.Parts = []ports.ModelPart{{Kind: ports.PartToolCall, CallID: "call-1", Tool: r.Tools[0].Name, Input: json.RawMessage(`{"report":"pl"}`)}}
	case r.Output != nil:
		resp.Parts = []ports.ModelPart{{Kind: ports.PartText, Text: `{"text":"ok"}`}}
	default:
		resp.Parts = []ports.ModelPart{{Kind: ports.PartText, Text: fmt.Sprintf("You said: %s; images: %d; documents: %d", question, images, documents)}}
	}
	return resp
}

// ModelSchema is the strict schema the suite's tool and output use: every object closed, every property required.
const ModelSchema = `{"type":"object","additionalProperties":false,"required":["report"],"properties":{"report":{"type":"string"}}}`

func ask(model, question string, parts ...ports.ModelPart) ports.ModelRequest {
	return ports.ModelRequest{Model: model, System: "You answer from the tools' figures only.", MaxTokens: 1024,
		Messages: []ports.ModelMessage{{Role: ports.RoleUser, Parts: append([]ports.ModelPart{{Kind: ports.PartText, Text: question}}, parts...)}}}
}

func text(r ports.ModelResponse) string {
	var out []string
	for _, p := range r.Parts {
		if p.Kind == ports.PartText {
			out = append(out, p.Text)
		}
	}
	return strings.Join(out, "")
}

// ModelProvider checks a model provider's adapter against its provider's fake playing Scripted (PRD-001 AI-70, AI-71):
// a whole answer and a streamed one alike, with its usage and the model it reports; and each capability the adapter
// declares: tool calling with a dotted tool name, the call and its result round trip; a strict output schema; image
// and PDF input; prompt caching; and the provider's model list. A capability it declares and fails here does not
// count (AI-71).
func ModelProvider(t *testing.T, p ports.ModelProvider, model string) {
	t.Helper()
	ctx := context.Background()
	caps := p.Capabilities()

	whole, err := p.Call(ctx, ask(model, "What was revenue in October?"))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if got := text(whole); got != "You said: What was revenue in October?; images: 0; documents: 0" || whole.StopReason != ports.StopEnd {
		t.Errorf("Call answered %q (%s)", got, whole.StopReason)
	}
	if whole.Model == "" || whole.Usage.Input == 0 || whole.Usage.Output == 0 {
		t.Errorf("Call's model and usage: %q %+v", whole.Model, whole.Usage)
	}

	if caps.Streaming {
		var pieces strings.Builder
		streamed, err := p.Stream(ctx, ask(model, "What was revenue in October?"), func(e ports.ModelEvent) error {
			pieces.WriteString(e.Text)
			return nil
		})
		if err != nil {
			t.Fatalf("Stream: %v", err)
		}
		if text(streamed) != text(whole) || pieces.String() != text(whole) || streamed.StopReason != ports.StopEnd || streamed.Usage.Output == 0 {
			t.Errorf("Stream answered %q in pieces %q (%s, %+v), Call %q", text(streamed), pieces.String(), streamed.StopReason, streamed.Usage, text(whole))
		}
	}

	if caps.Tools {
		tool := ports.ModelTool{Name: "reports.figure", Description: "A figure of a report.", Schema: json.RawMessage(ModelSchema)}
		r := ask(model, "Please use the tool for October's revenue.")
		r.Tools = []ports.ModelTool{tool}
		called, err := p.Call(ctx, r)
		if err != nil {
			t.Fatalf("a tool call: %v", err)
		}
		var call ports.ModelPart
		for _, part := range called.Parts {
			if part.Kind == ports.PartToolCall {
				call = part
			}
		}
		if called.StopReason != ports.StopToolCall || call.Tool != "reports.figure" || call.CallID == "" || !sameJSON(call.Input, `{"report":"pl"}`) {
			t.Fatalf("a tool call: %s %+v", called.StopReason, called.Parts)
		}
		r.Messages = append(r.Messages,
			ports.ModelMessage{Role: ports.RoleAssistant, Parts: called.Parts},
			ports.ModelMessage{Role: ports.RoleUser, Parts: []ports.ModelPart{{Kind: ports.PartToolResult, CallID: call.CallID, Tool: call.Tool, Text: "5000.00"}}})
		answered, err := p.Call(ctx, r)
		if err != nil || text(answered) != "The tool said: 5000.00" {
			t.Errorf("the tool's result: %v %q", err, text(answered))
		}
		if caps.Streaming {
			var whole bool
			r := ask(model, "Please use the tool for October's revenue.")
			r.Tools = []ports.ModelTool{tool}
			streamed, err := p.Stream(ctx, r, func(e ports.ModelEvent) error {
				whole = whole || (e.Part != nil && e.Part.Kind == ports.PartToolCall && e.Part.Tool == "reports.figure")
				return nil
			})
			if err != nil || streamed.StopReason != ports.StopToolCall || !whole {
				t.Errorf("a streamed tool call: %v %s %+v, event %v", err, streamed.StopReason, streamed.Parts, whole)
			}
		}
	}

	if caps.StrictOutput {
		r := ask(model, "Answer in the schema.")
		r.Output = &ports.ModelSchema{Name: "answer", Schema: json.RawMessage(ModelSchema)}
		out, err := p.Call(ctx, r)
		if err != nil || !sameJSON(json.RawMessage(text(out)), `{"text":"ok"}`) {
			t.Errorf("a strict output: %v %q", err, text(out))
		}
	}

	var media []ports.ModelPart
	if caps.Images {
		media = append(media, ports.ModelPart{Kind: ports.PartImage, MediaType: "image/png", Data: []byte("\x89PNG\r\n")})
	}
	if caps.PDF {
		media = append(media, ports.ModelPart{Kind: ports.PartDocument, MediaType: "application/pdf", Data: []byte("%PDF-1.7\n")})
	}
	if len(media) > 0 {
		out, err := p.Call(ctx, ask(model, "Read these.", media...))
		want := fmt.Sprintf("You said: Read these.; images: %d; documents: %d", btoi(caps.Images), btoi(caps.PDF))
		if err != nil || text(out) != want {
			t.Errorf("image and PDF input: %v %q, want %q", err, text(out), want)
		}
	}

	if caps.Caching {
		r := ask(model, "What was revenue in October?")
		r.Cache = true
		out, err := p.Call(ctx, r)
		if err != nil || out.Usage.CacheRead == 0 {
			t.Errorf("caching: %v %+v", err, out.Usage)
		}
	}

	page, err := p.ListModels(ctx, "")
	if err != nil || len(page.Models) == 0 || page.Models[0].ID == "" {
		t.Errorf("ListModels: %v %+v", err, page)
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func sameJSON(got json.RawMessage, want string) bool {
	var a, b any
	return json.Unmarshal(got, &a) == nil && json.Unmarshal([]byte(want), &b) == nil && fmt.Sprint(a) == fmt.Sprint(b)
}
