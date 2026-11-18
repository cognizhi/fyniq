//go:build local

package local

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cognizhi/fyniq/internal/aitool"
	"github.com/cognizhi/fyniq/internal/assistant"
	"github.com/cognizhi/fyniq/internal/ports"
)

// Scripted is local mode's scripted model (p1-launch-readiness design D6, the owner's decision of 17 Nov 2026): it
// answers the browser journeys' questions as a right model would, from scripts, so the AI journeys run the real path
// (the panel, the API, the AI service, the tools, numeric grounding, a proposal, its approval and the executor) offline
// and deterministically. For a question it knows, it makes the script's reads, then cites the facts they returned for
// the measures the script names, or makes the script's proposal; for any other question it says it has no script.
// Answer quality is the eval suites' (QA-AI-01…03), never this. Not in a release build (DEV-07).
type Scripted struct {
	Scripts []Script
}

// ScriptedModel is the one model the scripted connection lists.
const ScriptedModel = "scripted-journeys"

// Script is one question a journey asks and how a right model answers it.
type Script struct {
	Question string       `json:"question"`
	Reads    []ScriptRead `json:"reads,omitempty"`
	Cite     []ScriptFact `json:"cite,omitempty"`
	Proposal *ScriptMade  `json:"proposal,omitempty"`
}

// ScriptRead is a read tool's call, by the tool's name as the catalogue gives it.
type ScriptRead struct {
	Tool  string          `json:"tool"`
	Input json.RawMessage `json:"input"`
}

// ScriptFact names a fact to cite by its measure and period.
type ScriptFact struct {
	Measure string `json:"measure"`
	From    string `json:"from"`
	To      string `json:"to"`
}

// ScriptMade is a proposal of a task type with its payload.
type ScriptMade struct {
	TaskType string          `json:"task_type"`
	Payload  json.RawMessage `json:"payload"`
}

//go:embed scripted.json
var scriptedJSON []byte

// ScriptedManifest is the scripted model's connector manifest, which local mode's catalogue adds (internal/wiring).
//
//go:embed scripted-manifest.json
var ScriptedManifest []byte

// JourneyScripts are the browser journeys' scripts (apps/web/journeys/ai.spec.ts).
func JourneyScripts() ([]Script, error) {
	var out []Script
	if err := json.Unmarshal(scriptedJSON, &out); err != nil {
		return nil, fmt.Errorf("the scripted model's scripts: %w", err)
	}
	return out, nil
}

var _ ports.ModelProvider = Scripted{}

// proposeCall is the scripted proposal's call ID, by which a later request shows it was made.
const proposeCall = "scripted-proposal"

// Capabilities implements ports.ModelProvider: what the assistant needs, in Singapore.
func (Scripted) Capabilities() ports.ModelCapabilities {
	return ports.ModelCapabilities{Tools: true, StrictOutput: true, Streaming: true, ContextWindow: 200000, Regions: []string{"SG"}}
}

// ListModels implements ports.ModelProvider: the one scripted model.
func (Scripted) ListModels(context.Context, string) (ports.ModelPage, error) {
	return ports.ModelPage{Models: []ports.ListedModel{{ID: ScriptedModel, Name: "Scripted (local journeys)", ContextWindow: 200000}}}, nil
}

// Stream implements ports.ModelProvider: the answer's parts, one event each.
func (s Scripted) Stream(ctx context.Context, req ports.ModelRequest, each func(ports.ModelEvent) error) (ports.ModelResponse, error) {
	resp, err := s.Call(ctx, req)
	if err != nil {
		return resp, err
	}
	for i := range resp.Parts {
		if err := each(ports.ModelEvent{Part: &resp.Parts[i]}); err != nil {
			return ports.ModelResponse{}, err
		}
	}
	return resp, nil
}

// Call implements ports.ModelProvider.
func (s Scripted) Call(_ context.Context, req ports.ModelRequest) (ports.ModelResponse, error) {
	script, ok := s.script(req)
	if !ok {
		return s.answer(req, []assistant.Segment{{Text: "I have no script for that question: the local scripted model answers only the journeys' questions."}}), nil
	}
	last := req.Messages[len(req.Messages)-1]
	read := len(last.Parts) > 0 && last.Parts[0].Kind == ports.PartToolResult
	if !read && len(script.Reads) > 0 && !proposed(req) {
		parts := make([]ports.ModelPart, 0, len(script.Reads))
		for i, r := range script.Reads {
			parts = append(parts, ports.ModelPart{Kind: ports.PartToolCall, CallID: fmt.Sprintf("scripted-read-%d", i+1),
				Tool: strings.ReplaceAll(r.Tool, ".", "_"), Input: r.Input})
		}
		return ports.ModelResponse{Parts: parts, StopReason: ports.StopToolCall, Model: req.Model}, nil
	}
	if p := script.Proposal; p != nil && !proposed(req) {
		in, err := json.Marshal(map[string]any{"payload": p.Payload, "rationale": "The person asked for it."})
		if err != nil {
			return ports.ModelResponse{}, err
		}
		return ports.ModelResponse{Parts: []ports.ModelPart{{Kind: ports.PartToolCall, CallID: proposeCall,
			Tool: "propose_" + strings.ReplaceAll(p.TaskType, ".", "_"), Input: in}}, StopReason: ports.StopToolCall, Model: req.Model}, nil
	}
	if script.Proposal != nil {
		return s.answer(req, []assistant.Segment{{Text: "I proposed it. It waits for your approval in Fyniq."}}), nil
	}
	facts := factsIn(req)
	var segments []assistant.Segment
	for _, w := range script.Cite {
		var found *aitool.Fact
		for i := range facts {
			if facts[i].Measure == w.Measure && facts[i].From == w.From && facts[i].To == w.To {
				found = &facts[i]
				break
			}
		}
		if found == nil {
			segments = append(segments, assistant.Segment{Missing: w.Measure + " from " + w.From + " to " + w.To})
			continue
		}
		segments = append(segments, assistant.Segment{Text: strings.ReplaceAll(found.Measure, "_", " ") + " from " + found.From + " to " + found.To + " was "},
			assistant.Segment{Fact: found.ID, Measure: found.Measure, Member: found.Member, From: found.From, To: found.To, Currency: found.Currency},
			assistant.Segment{Text: "."})
	}
	return s.answer(req, segments), nil
}

// script is the script whose question is the conversation's last question, ignoring case and spaces at its ends.
func (s Scripted) script(req ports.ModelRequest) (Script, bool) {
	var question string
	for _, m := range req.Messages {
		if m.Role != ports.RoleUser {
			continue
		}
		for _, p := range m.Parts {
			if p.Kind == ports.PartText && strings.TrimSpace(p.Text) != "" {
				question = p.Text
			}
		}
	}
	for _, sc := range s.Scripts {
		if strings.Contains(strings.ToLower(question), strings.ToLower(strings.TrimSpace(sc.Question))) {
			return sc, true
		}
	}
	return Script{}, false
}

// answer is a structured answer of segments, as the assistant asks for it.
func (Scripted) answer(req ports.ModelRequest, segments []assistant.Segment) ports.ModelResponse {
	b, _ := json.Marshal(assistant.Structured{Segments: segments})
	return ports.ModelResponse{Parts: []ports.ModelPart{{Kind: ports.PartText, Text: string(b)}}, StopReason: ports.StopEnd, Model: req.Model,
		Usage: ports.ModelUsage{Input: 1, Output: 1}}
}

// proposed reports whether the conversation holds the result of the scripted proposal.
func proposed(req ports.ModelRequest) bool {
	for _, m := range req.Messages {
		for _, p := range m.Parts {
			if p.Kind == ports.PartToolResult && p.CallID == proposeCall {
				return true
			}
		}
	}
	return false
}

// factsIn are the facts the tools returned in the conversation.
func factsIn(req ports.ModelRequest) []aitool.Fact {
	var out []aitool.Fact
	for _, m := range req.Messages {
		for _, p := range m.Parts {
			if p.Kind != ports.PartToolResult {
				continue
			}
			var r struct {
				Facts []aitool.Fact `json:"facts"`
			}
			if json.Unmarshal([]byte(p.Text), &r) == nil {
				out = append(out, r.Facts...)
			}
		}
	}
	return out
}
