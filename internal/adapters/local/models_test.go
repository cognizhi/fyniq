//go:build local

package local

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/ports"
)

// liveModel stands in for a provider a developer's key reaches: it answers each request once and counts the calls.
type liveModel struct{ calls int }

func (l *liveModel) Capabilities() ports.ModelCapabilities {
	return ports.ModelCapabilities{Tools: true, StrictOutput: true, Streaming: true, Images: true, PDF: true}
}

func (l *liveModel) answer(r ports.ModelRequest) ports.ModelResponse {
	l.calls++
	return ports.ModelResponse{Parts: []ports.ModelPart{{Kind: ports.PartText, Text: "Revenue in October was 5000.00."}}, StopReason: ports.StopEnd,
		Usage: ports.ModelUsage{Input: 120, Output: 9, CacheRead: 100}, Model: r.Model, Version: r.Model + "-20261001"}
}

func (l *liveModel) Call(_ context.Context, r ports.ModelRequest) (ports.ModelResponse, error) {
	return l.answer(r), nil
}

func (l *liveModel) Stream(_ context.Context, r ports.ModelRequest, each func(ports.ModelEvent) error) (ports.ModelResponse, error) {
	resp := l.answer(r)
	for _, piece := range []string{"Revenue in October ", "was 5000.00."} {
		if err := each(ports.ModelEvent{Text: piece}); err != nil {
			return ports.ModelResponse{}, err
		}
	}
	return resp, nil
}

func (l *liveModel) ListModels(_ context.Context, _ string) (ports.ModelPage, error) {
	return ports.ModelPage{Models: []ports.ListedModel{{ID: "model-a"}, {ID: "model-b", AliasOf: "model-a"}}}, nil
}

// A request as every agent sends one through the port (AI-70): messages with text, an image and a PDF, a JSON Schema
// tool, a strict output schema, its limits, the thinking effort and caching.
func request(schema string) ports.ModelRequest {
	return ports.ModelRequest{
		Model:  "model-a",
		System: "You answer from the tools' figures only.",
		Messages: []ports.ModelMessage{{Role: ports.RoleUser, Parts: []ports.ModelPart{
			{Kind: ports.PartText, Text: "What was revenue in October?"},
			{Kind: ports.PartImage, MediaType: "image/png", Data: []byte("\x89PNG")},
			{Kind: ports.PartDocument, MediaType: "application/pdf", Data: []byte("%PDF-1.7")},
		}}},
		Tools:     []ports.ModelTool{{Name: "reports.figure", Description: "A figure of a report.", Schema: json.RawMessage(schema)}},
		Output:    &ports.ModelSchema{Name: "answer", Schema: json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"text":{"type":"string"}}}`)},
		MaxTokens: 1024,
		Effort:    "medium",
		Cache:     true,
	}
}

// Local AI (PRD-001 §12.8): with FYNIQ_AI_LIVE=1 the developer's live provider answers and each answer is kept as a
// cassette; by default the same requests replay offline without it, streamed or whole; a request no cassette matches is
// refused naming its key; the key ignores the tools' formatting but not their content; live mode without a provider
// says what to set. Every request goes through the one port with its parts, tools, schema and limits (AI-70).
// req: AI-70
// req: platform/local-mode Local AI in replay and live mode
func TestLocalAIReplaysWhatLiveModeRecorded(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	live := &liveModel{}
	schema := `{"type":"object","additionalProperties":false,"properties":{"report":{"type":"string"}}}`

	t.Setenv(LiveEnv, "1")
	p, err := Models(dir, func() (ports.ModelProvider, error) { return live, nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.(Recorder); !ok {
		t.Fatalf("live mode: %T", p)
	}
	whole, err := p.Call(ctx, request(schema))
	if err != nil {
		t.Fatal(err)
	}
	streamed := request(schema)
	streamed.Effort = "high"
	var pieces []string
	if _, err := p.Stream(ctx, streamed, func(e ports.ModelEvent) error { pieces = append(pieces, e.Text); return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ListModels(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if live.calls != 2 || !p.Capabilities().PDF {
		t.Fatalf("live: %d calls, %+v", live.calls, p.Capabilities())
	}

	t.Setenv(LiveEnv, "0")
	r, err := Models(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The same request, its tool schema spaced differently: the same cassette.
	spaced := `{ "type": "object", "additionalProperties": false, "properties": { "report": { "type": "string" } } }`
	again, err := r.Call(ctx, request(spaced))
	if err != nil || !reflect.DeepEqual(again, whole) || again.Version != "model-a-20261001" || again.Usage.CacheRead != 100 {
		t.Fatalf("the replayed answer: %v %+v", err, again)
	}
	var replayed []string
	if _, err := r.Stream(ctx, streamed, func(e ports.ModelEvent) error { replayed = append(replayed, e.Text); return nil }); err != nil ||
		!reflect.DeepEqual(replayed, pieces) {
		t.Fatalf("the replayed stream: %v %v, live %v", err, replayed, pieces)
	}
	if page, err := r.ListModels(ctx, ""); err != nil || len(page.Models) != 2 || page.Models[1].AliasOf != "model-a" {
		t.Fatalf("the replayed list: %v %+v", err, page)
	}
	if live.calls != 2 {
		t.Errorf("replay called the live provider: %d calls", live.calls)
	}

	// Another tool, or another model: no cassette, and the error names the key to record.
	other := request(`{"type":"object","properties":{"account":{"type":"string"}}}`)
	key, _ := CassetteKey(other)
	if _, err := r.Call(ctx, other); !errors.Is(err, ports.ErrNoCassette) || !strings.Contains(err.Error(), key) {
		t.Errorf("an unrecorded request: %v", err)
	}
	moved := request(schema)
	moved.Model = "model-b"
	if _, err := r.Call(ctx, moved); !errors.Is(err, ports.ErrNoCassette) {
		t.Errorf("another model: %v", err)
	}

	t.Setenv(LiveEnv, "1")
	if _, err := Models(dir, nil); err == nil || !strings.Contains(err.Error(), "developer's key") {
		t.Errorf("live mode with no provider: %v", err)
	}
}
