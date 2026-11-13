package compatible

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
)

// fakeChat plays porttest.Scripted behind the Chat Completions API, and keeps each request.
type fakeChat struct {
	mu       sync.Mutex
	requests []map[string]any
	headers  []http.Header
	status   int
}

func (f *fakeChat) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.headers = append(f.headers, r.Header.Clone())
	if f.status != 0 {
		w.WriteHeader(f.status)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"type": "server_error", "message": "busy"}})
		return
	}
	if r.Method == http.MethodGet {
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": []map[string]any{
			{"id": "moonshotai/kimi-k3", "name": "MoonshotAI: Kimi K3", "created": 1790000000, "context_length": 262144,
				"supported_parameters": []string{"tools", "response_format"}, "pricing": map[string]any{"prompt": "0.0000006", "completion": "0.0000025",
					"input_cache_read": "0.00000015", "input_cache_write": "0", "request": "0", "image": "0.0012", "web_search": "0.004"}},
			{"id": "openrouter/auto", "alias_target": "anthropic/claude-sonnet-5-5"},
			{"id": "vendor/discounted", "pricing": map[string]any{"prompt": "0.000001", "completion": "0.000002", "discount": 0.25}}}})
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	f.requests = append(f.requests, body)
	req := portRequest(body)
	resp := porttest.Scripted(req)
	msg := map[string]any{"role": "assistant", "content": nil}
	var calls []map[string]any
	finish := "stop"
	for _, p := range resp.Parts {
		switch p.Kind {
		case ports.PartText:
			msg["content"] = p.Text
		case ports.PartToolCall:
			calls = append(calls, map[string]any{"id": p.CallID, "type": "function", "function": map[string]any{"name": p.Tool, "arguments": string(p.Input)}})
			finish = "tool_calls"
		}
	}
	if len(calls) > 0 {
		msg["tool_calls"] = calls
	}
	u := map[string]any{"prompt_tokens": resp.Usage.Input + 20, "completion_tokens": resp.Usage.Output, "prompt_tokens_details": map[string]int64{"cached_tokens": 20}}
	if stream, _ := body["stream"].(bool); !stream {
		_ = json.NewEncoder(w).Encode(map[string]any{"model": req.Model, "choices": []map[string]any{{"index": 0, "message": msg, "finish_reason": finish}}, "usage": u})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	send := func(data any) {
		b, _ := json.Marshal(data)
		fmt.Fprintf(w, "data: %s\n\n", b)
	}
	if text, ok := msg["content"].(string); ok {
		for _, piece := range []string{text[:len(text)/2], text[len(text)/2:]} {
			send(map[string]any{"model": req.Model, "choices": []map[string]any{{"index": 0, "delta": map[string]any{"content": piece}}}})
		}
	}
	for i, c := range calls {
		args := c["function"].(map[string]any)["arguments"].(string)
		send(map[string]any{"choices": []map[string]any{{"index": 0, "delta": map[string]any{"tool_calls": []map[string]any{{"index": i, "id": c["id"], "type": "function",
			"function": map[string]any{"name": c["function"].(map[string]any)["name"], "arguments": args[:len(args)/2]}}}}}}})
		send(map[string]any{"choices": []map[string]any{{"index": 0, "delta": map[string]any{"tool_calls": []map[string]any{{"index": i,
			"function": map[string]any{"arguments": args[len(args)/2:]}}}}}}})
	}
	send(map[string]any{"choices": []map[string]any{{"index": 0, "delta": map[string]any{}, "finish_reason": finish}}})
	send(map[string]any{"choices": []any{}, "usage": u})
	fmt.Fprint(w, "data: [DONE]\n\n")
}

// portRequest decodes a Chat Completions request as the port's, so Scripted can answer it.
func portRequest(body map[string]any) ports.ModelRequest {
	raw, _ := json.Marshal(body)
	var b request
	_ = json.Unmarshal(raw, &b)
	r := ports.ModelRequest{Model: b.Model, MaxTokens: b.MaxTokens, Effort: b.ReasoningEffort}
	for _, t := range b.Tools {
		r.Tools = append(r.Tools, ports.ModelTool{Name: t.Function.Name, Description: t.Function.Description, Schema: t.Function.Parameters})
	}
	if b.ResponseFormat != nil {
		r.Output = &ports.ModelSchema{Name: b.ResponseFormat.JSONSchema.Name, Schema: b.ResponseFormat.JSONSchema.Schema}
	}
	for _, m := range b.Messages {
		switch m.Role {
		case "system":
			r.System, _ = m.Content.(string)
		case "tool":
			text, _ := m.Content.(string)
			r.Messages = append(r.Messages, ports.ModelMessage{Role: ports.RoleUser, Parts: []ports.ModelPart{{Kind: ports.PartToolResult, CallID: m.ToolCallID, Text: text}}})
		case "assistant":
			pm := ports.ModelMessage{Role: ports.RoleAssistant}
			for _, c := range m.ToolCalls {
				pm.Parts = append(pm.Parts, ports.ModelPart{Kind: ports.PartToolCall, CallID: c.ID, Tool: c.Function.Name, Input: json.RawMessage(c.Function.Arguments)})
			}
			r.Messages = append(r.Messages, pm)
		case "user":
			pm := ports.ModelMessage{Role: ports.RoleUser}
			parts, _ := json.Marshal(m.Content)
			var cps []contentPart
			_ = json.Unmarshal(parts, &cps)
			for _, p := range cps {
				switch p.Type {
				case "text":
					pm.Parts = append(pm.Parts, ports.ModelPart{Kind: ports.PartText, Text: p.Text})
				case "image_url":
					pm.Parts = append(pm.Parts, ports.ModelPart{Kind: ports.PartImage})
				}
			}
			r.Messages = append(r.Messages, pm)
		}
	}
	return r
}

// An OpenAI-compatible endpoint that proved everything the adapter carries passes the model provider's conformance
// suite (AI-71); PDF input and caching are not among what it can carry.
// req: AI-71
func TestCompatibleConformance(t *testing.T) {
	srv := httptest.NewServer(&fakeChat{})
	defer srv.Close()
	porttest.ModelProvider(t, New(srv.URL+"/v1", "test-key", Carried, srv.Client()), "moonshotai/kimi-k3")
}

// A connection claims only what its probe proved (AI-71): a request needing anything else is refused before it is
// sent, saying what is missing; PDF input is never carried. What goes on the wire otherwise: the system message, strict
// tools under names the endpoint accepts, a json_schema response format, the reasoning effort, images as data URLs,
// usage with the stream; and no Authorization header for a self-hosted endpoint without a key.
// req: AI-71
// req: AI-27
func TestCompatibleClaimsOnlyWhatItProved(t *testing.T) {
	fake := &fakeChat{}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	ctx := context.Background()
	textOnly := New(srv.URL+"/v1", "", ports.ModelCapabilities{}, srv.Client())
	if caps := textOnly.Capabilities(); caps.Tools || caps.Streaming {
		t.Errorf("an unproved connection's capabilities: %+v", caps)
	}
	r := ports.ModelRequest{Model: "llama-4", MaxTokens: 64, Effort: "low",
		Tools:  []ports.ModelTool{{Name: "reports.figure", Schema: json.RawMessage(porttest.ModelSchema)}},
		Output: &ports.ModelSchema{Name: "answer", Schema: json.RawMessage(porttest.ModelSchema)},
		Messages: []ports.ModelMessage{{Role: ports.RoleUser, Parts: []ports.ModelPart{{Kind: ports.PartText, Text: "Please use the tool."},
			{Kind: ports.PartImage, MediaType: "image/png", Data: []byte("img")}, {Kind: ports.PartDocument, MediaType: "application/pdf", Data: []byte("pdf")}}}}}
	_, err := textOnly.Call(ctx, r)
	if !errors.Is(err, ports.ErrModelRequest) || len(fake.requests) != 0 {
		t.Fatalf("an unproved request: %v, %d sent", err, len(fake.requests))
	}
	for _, want := range []string{"tool calling", "a strict output schema", "reasoning controls", "image input", "PDF input"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %s: %v", want, err)
		}
	}
	if _, err := textOnly.Stream(ctx, porttestHello(), func(ports.ModelEvent) error { return nil }); !errors.Is(err, ports.ErrModelRequest) || !strings.Contains(err.Error(), "streaming") {
		t.Errorf("an unproved stream: %v", err)
	}

	// A self-hosted endpoint without a key sends none; text alone needs nothing proved.
	if out, err := textOnly.Call(ctx, porttestHello()); err != nil || out.Parts[0].Text != "You said: Hello.; images: 0; documents: 0" {
		t.Fatalf("a text request: %v %+v", err, out)
	}
	if fake.headers[0].Get("Authorization") != "" {
		t.Errorf("a key was sent without one: %v", fake.headers[0])
	}

	full := New(srv.URL+"/v1", "test-key", Carried, srv.Client())
	r.Messages[0].Parts = r.Messages[0].Parts[:2]
	resp, err := full.Call(ctx, r)
	if err != nil || resp.StopReason != ports.StopToolCall || resp.Parts[0].Tool != "reports.figure" || resp.Usage.CacheRead != 20 || resp.Usage.Input != 100 {
		t.Fatalf("a proved request: %v %+v", err, resp)
	}
	wire, _ := json.Marshal(fake.requests[len(fake.requests)-1])
	for _, want := range []string{`"name":"reports_figure"`, `"strict":true`, `"response_format":{"json_schema":{"name":"answer"`, `"type":"json_schema"`,
		`"reasoning_effort":"low"`, `"url":"data:image/png;base64,aW1n"`} {
		if !strings.Contains(string(wire), want) {
			t.Errorf("the request lacks %s: %s", want, wire)
		}
	}
	if fake.headers[len(fake.headers)-1].Get("Authorization") != "Bearer test-key" {
		t.Errorf("the key: %v", fake.headers[len(fake.headers)-1])
	}
}

// Refusals map to the port's errors; finish reasons to its stop reasons; the model list carries the hints an endpoint
// such as OpenRouter adds, an alias's target and its prices among them (AI-78, AI-80, AI-82).
// req: AI-80
// req: AI-78
// req: AI-82
func TestCompatibleErrorsStopsAndModelList(t *testing.T) {
	fake := &fakeChat{}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	c := New(srv.URL+"/v1", "test-key", Carried, srv.Client())
	ctx := context.Background()
	for status, want := range map[int]error{429: ports.ErrModelBusy, 502: ports.ErrModelBusy, 404: ports.ErrModelNotFound, 401: ports.ErrModelAuth, 400: ports.ErrModelRequest} {
		fake.status = status
		if _, err := c.Call(ctx, porttestHello()); !errors.Is(err, want) {
			t.Errorf("%d: %v", status, err)
		}
	}
	fake.status = 0
	for reason, want := range map[string]string{"stop": ports.StopEnd, "length": ports.StopMaxTokens, "tool_calls": ports.StopToolCall, "content_filter": ports.StopRefusal} {
		if got := stop(reason, false); got != want {
			t.Errorf("%s: %s", reason, got)
		}
	}
	if stop("stop", true) != ports.StopRefusal {
		t.Error("a refusal")
	}
	page, err := c.ListModels(ctx, "")
	if err != nil || len(page.Models) != 3 || page.Models[0].ContextWindow != 262144 || page.Models[0].Name != "MoonshotAI: Kimi K3" ||
		strings.Join(page.Models[0].Reports, ",") != "tools,response_format" || page.Models[1].AliasOf != "anthropic/claude-sonnet-5-5" {
		t.Errorf("the model list: %v %+v", err, page)
	}
	// The list's prices, as OpenRouter gives them (AI-82 (a)): decimal text per token; an optional rate of zero left out;
	// a discount makes the price conditional; no pricing, no price.
	if p := page.Models[0].Price; p == nil || p.Currency != "USD" || p.PerToken["input"] != "0.0000006" || p.PerToken["output"] != "0.0000025" ||
		p.PerToken["cache_read"] != "0.00000015" || len(p.PerToken) != 3 || p.PerImage != "0.0012" || p.PerRequest != "" || p.Conditional {
		t.Errorf("a listed price: %+v", p)
	}
	if page.Models[1].Price != nil || page.Models[2].Price == nil || !page.Models[2].Price.Conditional {
		t.Errorf("no price, and a discounted one: %+v %+v", page.Models[1].Price, page.Models[2].Price)
	}
}

func porttestHello() ports.ModelRequest {
	return ports.ModelRequest{Model: "llama-4", MaxTokens: 64, Messages: []ports.ModelMessage{{Role: ports.RoleUser, Parts: []ports.ModelPart{{Kind: ports.PartText, Text: "Hello."}}}}}
}
