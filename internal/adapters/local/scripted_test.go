//go:build local

package local

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/assistant"
	"github.com/cognizhi/fyniq/internal/ports"
)

// The scripted model answers the journeys' questions as a right model would (p1-launch-readiness design D6): it makes
// the script's reads, then cites the facts they returned, or makes the script's proposal and then says it waits for
// approval; any other question it says it has no script for, and it reads and proposes nothing.
// req: QA-E2E-01
func TestTheScriptedModelAnswersTheJourneysQuestions(t *testing.T) {
	scripts, err := JourneyScripts()
	if err != nil {
		t.Fatal(err)
	}
	m := Scripted{Scripts: scripts}
	ask := func(msgs ...ports.ModelMessage) ports.ModelResponse {
		t.Helper()
		r, err := m.Call(context.Background(), ports.ModelRequest{Model: ScriptedModel, Messages: msgs})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	user := func(q string) ports.ModelMessage {
		return ports.ModelMessage{Role: ports.RoleUser, Parts: []ports.ModelPart{{Kind: ports.PartText, Text: q}}}
	}
	answer := func(r ports.ModelResponse) assistant.Structured {
		t.Helper()
		var s assistant.Structured
		if r.StopReason != ports.StopEnd || len(r.Parts) != 1 || json.Unmarshal([]byte(r.Parts[0].Text), &s) != nil {
			t.Fatalf("not an answer: %+v", r)
		}
		return s
	}

	q := user("  what was revenue in January 2026?")
	read := ask(q)
	if read.StopReason != ports.StopToolCall || len(read.Parts) != 1 || read.Parts[0].Tool != "metrics_query" {
		t.Fatalf("the revenue question reads %+v", read)
	}
	result := ports.ModelMessage{Role: ports.RoleUser, Parts: []ports.ModelPart{{Kind: ports.PartToolResult, CallID: read.Parts[0].CallID,
		Text: `{"facts":[{"id":"F1","measure":"revenue","value":"466784.87","unit":"amount","currency":"SGD","from":"2026-01-01","to":"2026-01-31"}]}`}}}
	cited := answer(ask(q, ports.ModelMessage{Role: ports.RoleAssistant, Parts: read.Parts}, result))
	if len(cited.Segments) != 3 || cited.Segments[1].Fact != "F1" || cited.Segments[1].Currency != "SGD" {
		t.Fatalf("the answer cites %+v", cited)
	}
	for _, s := range cited.Segments {
		if strings.Contains(s.Text, "466") {
			t.Fatalf("a figure in words: %q", s.Text)
		}
	}
	empty := ports.ModelMessage{Role: ports.RoleUser, Parts: []ports.ModelPart{{Kind: ports.PartToolResult, CallID: read.Parts[0].CallID, Text: `{"facts":[]}`}}}
	if missing := answer(ask(q, ports.ModelMessage{Role: ports.RoleAssistant, Parts: read.Parts}, empty)); missing.Segments[0].Missing == "" {
		t.Fatalf("no fact read, yet %+v", missing)
	}

	p := user(scripts[1].Question)
	made := ask(p)
	if made.StopReason != ports.StopToolCall || made.Parts[0].Tool != "propose_sec_create_task" || !strings.Contains(string(made.Parts[0].Input), "agent:agm-room-2027") {
		t.Fatalf("the task question proposes %+v", made)
	}
	done := answer(ask(p, ports.ModelMessage{Role: ports.RoleAssistant, Parts: made.Parts},
		ports.ModelMessage{Role: ports.RoleUser, Parts: []ports.ModelPart{{Kind: ports.PartToolResult, CallID: made.Parts[0].CallID, Text: `{"status":"pending"}`}}}))
	if !strings.Contains(done.Segments[0].Text, "waits for your approval") {
		t.Fatalf("after the proposal: %+v", done)
	}

	other := ask(user("Pay every bill now."))
	if s := answer(other); !strings.Contains(s.Segments[0].Text, "no script") {
		t.Fatalf("an unscripted question: %+v", s)
	}
	if page, _ := m.ListModels(context.Background(), ""); len(page.Models) != 1 || page.Models[0].ID != ScriptedModel {
		t.Fatalf("its models: %+v", page)
	}
}
