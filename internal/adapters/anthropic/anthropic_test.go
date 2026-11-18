package anthropic

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"golang.org/x/oauth2"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
)

// testKey is built from parts, so no scanner mistakes the fixture for a credential.
var testKey = "test-anthropic-" + "fixture-0001"

// fakeMessages plays porttest.Scripted behind the Messages API as Anthropic documents it, the Anthropic API's and
// Agent Platform's alike, and keeps what each request carried for the checks that follow.
type fakeMessages struct {
	mu       sync.Mutex
	requests []map[string]any
	headers  []http.Header
	paths    []string
	status   int    // when set, every Messages call is refused with it
	message  string // and this message
}

func (f *fakeMessages) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paths = append(f.paths, r.URL.Path+"?"+r.URL.RawQuery)
	f.headers = append(f.headers, r.Header.Clone())
	if r.Method == http.MethodGet {
		f.models(w, r)
		return
	}
	if f.status != 0 {
		w.WriteHeader(f.status)
		_ = json.NewEncoder(w).Encode(map[string]any{"type": "error", "error": map[string]string{"type": "rate_limit_error", "message": f.message}})
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	f.requests = append(f.requests, body)
	model, _ := body["model"].(string)
	if i := strings.Index(r.URL.Path, "/models/"); i >= 0 && strings.Contains(r.URL.Path, "/publishers/anthropic/") {
		model = strings.Split(r.URL.Path[i+len("/models/"):], ":")[0]
	}
	req := portRequest(model, body)
	resp := porttest.Scripted(req)
	var blocks []map[string]any
	if req.Effort == "high" {
		blocks = append(blocks, map[string]any{"type": "thinking", "thinking": "", "signature": "sig-abc"})
	}
	for _, p := range resp.Parts {
		switch p.Kind {
		case ports.PartText:
			blocks = append(blocks, map[string]any{"type": "text", "text": p.Text})
		case ports.PartToolCall:
			blocks = append(blocks, map[string]any{"type": "tool_use", "id": p.CallID, "name": p.Tool, "input": json.RawMessage(p.Input)})
		}
	}
	reason := map[string]string{ports.StopEnd: "end_turn", ports.StopToolCall: "tool_use"}[resp.StopReason]
	u := map[string]int64{"input_tokens": resp.Usage.Input, "output_tokens": resp.Usage.Output, "cache_read_input_tokens": resp.Usage.CacheRead}
	if stream, _ := body["stream"].(bool); stream || strings.HasSuffix(r.URL.Path, ":streamRawPredict") {
		f.stream(w, model, blocks, reason, u)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "message", "model": model, "stop_reason": reason, "content": blocks, "usage": u})
}

func (f *fakeMessages) stream(w http.ResponseWriter, model string, blocks []map[string]any, reason string, u map[string]int64) {
	w.Header().Set("Content-Type", "text/event-stream")
	send := func(event string, data any) {
		b, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
	}
	send("message_start", map[string]any{"type": "message_start", "message": map[string]any{"model": model,
		"usage": map[string]int64{"input_tokens": u["input_tokens"], "cache_read_input_tokens": u["cache_read_input_tokens"], "output_tokens": 1}}})
	fmt.Fprint(w, ": ping\n\n")
	for i, b := range blocks {
		switch b["type"] {
		case "text":
			text := b["text"].(string)
			send("content_block_start", map[string]any{"type": "content_block_start", "index": i, "content_block": map[string]any{"type": "text", "text": ""}})
			for _, piece := range []string{text[:len(text)/2], text[len(text)/2:]} {
				send("content_block_delta", map[string]any{"type": "content_block_delta", "index": i, "delta": map[string]any{"type": "text_delta", "text": piece}})
			}
		case "tool_use":
			input := string(b["input"].(json.RawMessage))
			send("content_block_start", map[string]any{"type": "content_block_start", "index": i, "content_block": map[string]any{"type": "tool_use", "id": b["id"], "name": b["name"], "input": map[string]any{}}})
			for _, piece := range []string{input[:len(input)/2], input[len(input)/2:]} {
				send("content_block_delta", map[string]any{"type": "content_block_delta", "index": i, "delta": map[string]any{"type": "input_json_delta", "partial_json": piece}})
			}
		case "thinking":
			send("content_block_start", map[string]any{"type": "content_block_start", "index": i, "content_block": map[string]any{"type": "thinking", "thinking": ""}})
			send("content_block_delta", map[string]any{"type": "content_block_delta", "index": i, "delta": map[string]any{"type": "signature_delta", "signature": b["signature"]}})
		}
		send("content_block_stop", map[string]any{"type": "content_block_stop", "index": i})
	}
	send("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": reason}, "usage": map[string]int64{"output_tokens": u["output_tokens"]}})
	send("message_stop", map[string]any{"type": "message_stop"})
}

func (f *fakeMessages) models(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.URL.Path, "/publishers/anthropic/models") {
		if r.URL.Query().Get("pageToken") == "" {
			_ = json.NewEncoder(w).Encode(map[string]any{"publisherModels": []map[string]string{{"name": "publishers/anthropic/models/claude-opus-5-5"}}, "nextPageToken": "p2"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"publisherModels": []map[string]string{{"name": "publishers/anthropic/models/claude-sonnet-5-5"}}})
		return
	}
	model := func(id, name string) map[string]any {
		return map[string]any{"type": "model", "id": id, "display_name": name, "created_at": "2026-10-01T00:00:00Z",
			"max_input_tokens": 1000000, "max_tokens": 128000, "capabilities": map[string]any{
				"image_input": map[string]bool{"supported": true}, "structured_outputs": map[string]bool{"supported": true},
				"batch": map[string]bool{"supported": false}}}
	}
	if r.URL.Query().Get("after_id") == "" {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{model("claude-opus-5-5", "Claude Opus 5.5")}, "has_more": true, "last_id": "claude-opus-5-5"})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{model("claude-sonnet-5-5", "Claude Sonnet 5.5")}, "has_more": false, "last_id": "claude-sonnet-5-5"})
}

// portRequest decodes a Messages API request as the port's, so Scripted can answer it.
func portRequest(model string, body map[string]any) ports.ModelRequest {
	raw, _ := json.Marshal(body)
	var b request
	_ = json.Unmarshal(raw, &b)
	r := ports.ModelRequest{Model: model, MaxTokens: b.MaxTokens}
	for _, s := range b.System {
		r.System += s.Text
		r.Cache = r.Cache || s.CacheControl != nil
	}
	for _, t := range b.Tools {
		r.Tools = append(r.Tools, ports.ModelTool{Name: t.Name, Description: t.Description, Schema: t.InputSchema})
		r.Cache = r.Cache || t.CacheControl != nil
	}
	if b.OutputConfig != nil {
		r.Effort = b.OutputConfig.Effort
		if b.OutputConfig.Format != nil {
			r.Output = &ports.ModelSchema{Schema: b.OutputConfig.Format.Schema}
		}
	}
	for _, m := range b.Messages {
		pm := ports.ModelMessage{Role: m.Role}
		for _, c := range m.Content {
			var blk block
			_ = json.Unmarshal(c, &blk)
			switch blk.Type {
			case "text":
				pm.Parts = append(pm.Parts, ports.ModelPart{Kind: ports.PartText, Text: blk.Text})
			case "image":
				pm.Parts = append(pm.Parts, ports.ModelPart{Kind: ports.PartImage})
			case "document":
				pm.Parts = append(pm.Parts, ports.ModelPart{Kind: ports.PartDocument})
			case "tool_use":
				pm.Parts = append(pm.Parts, ports.ModelPart{Kind: ports.PartToolCall, CallID: blk.ID, Tool: blk.Name, Input: blk.Input})
			case "tool_result":
				var text string
				for _, c := range blk.Content {
					text += c.Text
				}
				pm.Parts = append(pm.Parts, ports.ModelPart{Kind: ports.PartToolResult, CallID: blk.ToolUseID, Text: text})
			}
		}
		r.Messages = append(r.Messages, pm)
	}
	return r
}

// The Anthropic API adapter passes the model provider's conformance suite (AI-71): whole and streamed answers with
// their usage and reported model, a dotted tool name's call and result, a strict output, image and PDF input, caching
// and the model list.
// req: AI-71
// req: AI-27
// req: AI-28
func TestAnthropicAPIConformance(t *testing.T) {
	fake := &fakeMessages{}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	porttest.ModelProvider(t, New(srv.URL, testKey, srv.Client()), "claude-opus-5-5")
}

// Claude on Agent Platform passes the same suite, with the model in the path, the version in the body and the
// service's own account's token [live-verify].
// req: AI-71
func TestAgentPlatformConformance(t *testing.T) {
	fake := &fakeMessages{}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	c := NewAgentPlatform(srv.URL, "fyniq-dev", "asia-southeast1", oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "platform-token"}), srv.Client())
	porttest.ModelProvider(t, c, "claude-opus-5-5")
	body := fake.requests[0]
	if body["anthropic_version"] != Version || body["model"] != nil || fake.headers[0].Get("Authorization") != "Bearer platform-token" ||
		fake.headers[0].Get("x-api-key") != "" || fake.paths[0] != "/v1/projects/fyniq-dev/locations/asia-southeast1/publishers/anthropic/models/claude-opus-5-5:rawPredict?" {
		t.Errorf("an Agent Platform request: %s %v %v", fake.paths[0], body, fake.headers[0])
	}
	if !strings.Contains(strings.Join(fake.paths, " "), ":streamRawPredict") {
		t.Errorf("no streamed request on Agent Platform: %v", fake.paths)
	}
	if got := c.Capabilities().Regions; len(got) != 1 || got[0] != "asia-southeast1" {
		t.Errorf("Agent Platform's region: %v", got)
	}
	page, err := c.ListModels(context.Background(), "")
	if err != nil || page.Models[0].ID != "claude-opus-5-5" || page.Next != "p2" {
		t.Errorf("Agent Platform's models: %v %+v", err, page)
	}
	if got := AgentPlatformBase("global"); got != "https://aiplatform.googleapis.com" {
		t.Errorf("the global endpoint: %s", got)
	}
}

// What goes on the wire (AI-27, AI-28, AI-79): the key and version in headers, never in the address; every tool
// strict, under a name the API accepts; caching on the system prompt, the end of the stable prefix; the effort and a
// json_schema output under output_config; images and PDFs as base64 sources; a tool's result as text; no sampling
// parameter; and a provider's reasoning block passed back unchanged in the next turn of a tool loop.
// req: AI-27
// req: AI-28
// req: AI-79
func TestAnthropicRequestsOnTheWire(t *testing.T) {
	fake := &fakeMessages{}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	c := New(srv.URL, testKey, srv.Client())
	ctx := context.Background()
	r := ports.ModelRequest{Model: "claude-opus-5-5", System: "You answer from the tools' figures only.", MaxTokens: 2048, Effort: "high", Cache: true,
		Tools:  []ports.ModelTool{{Name: "reports.figure", Description: "A figure of a report.", Schema: json.RawMessage(porttest.ModelSchema)}},
		Output: &ports.ModelSchema{Name: "answer", Schema: json.RawMessage(porttest.ModelSchema)},
		Messages: []ports.ModelMessage{{Role: ports.RoleUser, Parts: []ports.ModelPart{
			{Kind: ports.PartText, Text: "Please use the tool."},
			{Kind: ports.PartImage, MediaType: "image/png", Data: []byte("img")},
			{Kind: ports.PartDocument, MediaType: "application/pdf", Data: []byte("pdf")},
		}}}}
	resp, err := c.Call(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	h, body := fake.headers[0], fake.requests[0]
	if h.Get("x-api-key") != testKey || h.Get("anthropic-version") != Version || strings.Contains(fake.paths[0], testKey) {
		t.Errorf("headers %v, path %s", h, fake.paths[0])
	}
	wire, _ := json.Marshal(body)
	for _, want := range []string{`"strict":true`, `"name":"reports_figure"`, `"cache_control":{"type":"ephemeral"}`, `"effort":"high"`,
		`"format":{"schema":`, `"type":"json_schema"`, `"media_type":"image/png"`, `"data":"` + base64.StdEncoding.EncodeToString([]byte("pdf")) + `"`,
		`"type":"document"`, `"max_tokens":2048`} {
		if !strings.Contains(string(wire), want) {
			t.Errorf("the request lacks %s: %s", want, wire)
		}
	}
	for _, never := range []string{"temperature", "top_p", "top_k"} {
		if strings.Contains(string(wire), never) {
			t.Errorf("the request sends %s", never)
		}
	}
	if resp.StopReason != ports.StopToolCall || len(resp.Parts) != 2 || resp.Parts[0].Kind != ports.PartReasoning || resp.Parts[1].Tool != "reports.figure" {
		t.Fatalf("the answer: %+v", resp)
	}
	r.Messages = append(r.Messages, ports.ModelMessage{Role: ports.RoleAssistant, Parts: resp.Parts},
		ports.ModelMessage{Role: ports.RoleUser, Parts: []ports.ModelPart{{Kind: ports.PartToolResult, CallID: resp.Parts[1].CallID, Text: "5000.00"}}})
	if _, err := c.Call(ctx, r); err != nil {
		t.Fatal(err)
	}
	next, _ := json.Marshal(fake.requests[1]["messages"])
	if !strings.Contains(string(next), `{"signature":"sig-abc","thinking":"","type":"thinking"}`) || !strings.Contains(string(next), `"tool_use_id":"call-1"`) {
		t.Errorf("the next turn: %s", next)
	}

	// The same, streamed: the reasoning block is rebuilt from its events, whole.
	streamed, err := c.Stream(ctx, ports.ModelRequest{Model: "claude-opus-5-5", MaxTokens: 64, Effort: "high", Tools: r.Tools,
		Messages: []ports.ModelMessage{{Role: ports.RoleUser, Parts: []ports.ModelPart{{Kind: ports.PartText, Text: "Please use the tool."}}}}},
		func(ports.ModelEvent) error { return nil })
	if err != nil || streamed.Parts[0].Kind != ports.PartReasoning || !strings.Contains(string(streamed.Parts[0].Raw), `"signature":"sig-abc"`) {
		t.Errorf("a streamed reasoning block: %v %+v", err, streamed.Parts)
	}
}

// A refusal is the port's error, which the router acts on (AI-76, AI-81), with the provider's words scrubbed of the
// key (AI-79); the model list is read page by page with what each model reports.
// req: AI-79
// req: AI-80
func TestAnthropicErrorsAndModelList(t *testing.T) {
	fake := &fakeMessages{}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	c := New(srv.URL, testKey, srv.Client())
	ctx := context.Background()
	for status, want := range map[int]error{429: ports.ErrModelBusy, 529: ports.ErrModelBusy, 404: ports.ErrModelNotFound, 401: ports.ErrModelAuth, 400: ports.ErrModelRequest} {
		fake.status, fake.message = status, "refused for key "+testKey
		_, err := c.Call(ctx, porttestRequest())
		if !errors.Is(err, want) || strings.Contains(err.Error(), testKey) {
			t.Errorf("%d: %v", status, err)
		}
		_, err = c.Stream(ctx, porttestRequest(), func(ports.ModelEvent) error { return nil })
		if !errors.Is(err, want) {
			t.Errorf("%d streamed: %v", status, err)
		}
	}
	fake.status = 0

	page, err := c.ListModels(ctx, "")
	if err != nil || len(page.Models) != 1 || page.Next != "claude-opus-5-5" {
		t.Fatalf("the first page: %v %+v", err, page)
	}
	m := page.Models[0]
	if m.Name != "Claude Opus 5.5" || m.ContextWindow != 1000000 || m.MaxOutput != 128000 || m.Created == nil ||
		strings.Join(m.Reports, ",") != "image_input,structured_outputs" {
		t.Errorf("a listed model: %+v", m)
	}
	page, err = c.ListModels(ctx, page.Next)
	if err != nil || page.Models[0].ID != "claude-sonnet-5-5" || page.Next != "" {
		t.Errorf("the last page: %v %+v", err, page)
	}
	if got := fake.paths[len(fake.paths)-1]; !strings.Contains(got, "after_id=claude-opus-5-5") {
		t.Errorf("the last page's address: %s", got)
	}
	if _, err := New(srv.URL, "", srv.Client()).Call(ctx, porttestRequest()); !errors.Is(err, ports.ErrModelAuth) {
		t.Errorf("no key: %v", err)
	}
}

func porttestRequest() ports.ModelRequest {
	return ports.ModelRequest{Model: "claude-opus-5-5", MaxTokens: 64,
		Messages: []ports.ModelMessage{{Role: ports.RoleUser, Parts: []ports.ModelPart{{Kind: ports.PartText, Text: "Hello."}}}}}
}
