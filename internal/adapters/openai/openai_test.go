package openai

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

	"github.com/cognizhi/fyniq/internal/adapters/modelwire"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
)

func modelwireNames() modelwire.ToolNames { return modelwire.NewToolNames(nil) }

// testKey is built from parts, so no scanner mistakes the fixture for a credential.
var testKey = "test-openai-" + "fixture-0001"

// fakeResponses plays porttest.Scripted behind the Responses API as OpenAI documents it, and keeps each request.
type fakeResponses struct {
	mu       sync.Mutex
	requests []map[string]any
	headers  []http.Header
	paths    []string
	status   int
	message  string
}

func (f *fakeResponses) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paths = append(f.paths, r.URL.String())
	f.headers = append(f.headers, r.Header.Clone())
	if f.status != 0 {
		w.WriteHeader(f.status)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"type": "requests", "code": "rate_limit_exceeded", "message": f.message}})
		return
	}
	if r.Method == http.MethodGet {
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": []map[string]any{
			{"id": "gpt-6", "object": "model", "created": 1790000000, "owned_by": "openai", "shutdown_date": nil},
			{"id": "gpt-5", "object": "model", "created": 1750000000, "owned_by": "openai", "shutdown_date": "2027-03-31"}}})
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
	var output []map[string]any
	if req.Effort == "high" {
		output = append(output, map[string]any{"type": "reasoning", "id": "rs_1", "summary": []any{}, "encrypted_content": "enc-abc"})
	}
	for _, p := range resp.Parts {
		switch p.Kind {
		case ports.PartText:
			output = append(output, map[string]any{"type": "message", "role": "assistant", "status": "completed",
				"content": []map[string]any{{"type": "output_text", "text": p.Text}}})
		case ports.PartToolCall:
			output = append(output, map[string]any{"type": "function_call", "call_id": p.CallID, "name": p.Tool, "arguments": string(p.Input)})
		}
	}
	cached := resp.Usage.CacheRead
	whole := map[string]any{"object": "response", "model": req.Model, "status": "completed", "output": output, "incomplete_details": nil,
		"usage": map[string]any{"input_tokens": resp.Usage.Input + cached, "input_tokens_details": map[string]int64{"cached_tokens": cached},
			"output_tokens": resp.Usage.Output, "output_tokens_details": map[string]int64{"reasoning_tokens": 4}}}
	if stream, _ := body["stream"].(bool); !stream {
		_ = json.NewEncoder(w).Encode(whole)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	send := func(data map[string]any) {
		b, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", data["type"], b)
	}
	send(map[string]any{"type": "response.created", "response": map[string]any{"model": req.Model, "status": "in_progress"}})
	for i, it := range output {
		send(map[string]any{"type": "response.output_item.added", "output_index": i, "item": it})
		if it["type"] == "message" {
			text := it["content"].([]map[string]any)[0]["text"].(string)
			for _, piece := range []string{text[:len(text)/2], text[len(text)/2:]} {
				send(map[string]any{"type": "response.output_text.delta", "output_index": i, "content_index": 0, "delta": piece})
			}
		}
		send(map[string]any{"type": "response.output_item.done", "output_index": i, "item": it})
	}
	send(map[string]any{"type": "response.completed", "response": whole})
}

// portRequest decodes a Responses API request as the port's, so Scripted can answer it.
func portRequest(body map[string]any) ports.ModelRequest {
	raw, _ := json.Marshal(body)
	var b request
	_ = json.Unmarshal(raw, &b)
	r := ports.ModelRequest{Model: b.Model, System: b.Instructions, MaxTokens: b.MaxOutputTokens, Cache: b.PromptCacheKey != ""}
	for _, t := range b.Tools {
		r.Tools = append(r.Tools, ports.ModelTool{Name: t.Name, Description: t.Description, Schema: t.Parameters})
	}
	if b.Text != nil {
		r.Output = &ports.ModelSchema{Name: b.Text.Format.Name, Schema: b.Text.Format.Schema}
	}
	if b.Reasoning != nil {
		r.Effort = b.Reasoning.Effort
	}
	for _, in := range b.Input {
		var it struct {
			Type    string          `json:"type"`
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
			CallID  string          `json:"call_id"`
			Name    string          `json:"name"`
			Args    string          `json:"arguments"`
			Output  string          `json:"output"`
		}
		_ = json.Unmarshal(in, &it)
		switch {
		case it.Type == "function_call":
			r.Messages = append(r.Messages, ports.ModelMessage{Role: ports.RoleAssistant, Parts: []ports.ModelPart{{Kind: ports.PartToolCall, CallID: it.CallID, Tool: it.Name, Input: json.RawMessage(it.Args)}}})
		case it.Type == "function_call_output":
			r.Messages = append(r.Messages, ports.ModelMessage{Role: ports.RoleUser, Parts: []ports.ModelPart{{Kind: ports.PartToolResult, CallID: it.CallID, Text: it.Output}}})
		case it.Role != "":
			m := ports.ModelMessage{Role: it.Role}
			var parts []content
			if json.Unmarshal(it.Content, &parts) != nil {
				var s string
				_ = json.Unmarshal(it.Content, &s)
				parts = []content{{Type: "input_text", Text: s}}
			}
			for _, p := range parts {
				switch p.Type {
				case "input_text":
					m.Parts = append(m.Parts, ports.ModelPart{Kind: ports.PartText, Text: p.Text})
				case "input_image":
					m.Parts = append(m.Parts, ports.ModelPart{Kind: ports.PartImage})
				case "input_file":
					m.Parts = append(m.Parts, ports.ModelPart{Kind: ports.PartDocument})
				}
			}
			r.Messages = append(r.Messages, m)
		}
	}
	return r
}

// The OpenAI adapter passes the model provider's conformance suite (AI-71).
// req: AI-71
func TestOpenAIConformance(t *testing.T) {
	srv := httptest.NewServer(&fakeResponses{})
	defer srv.Close()
	porttest.ModelProvider(t, New(srv.URL+"/v1", testKey, srv.Client()), "gpt-6")
}

// What goes on the wire (AI-27, AI-28, AI-79): the key as a bearer header, never in the address; nothing stored at
// OpenAI, so reasoning returns encrypted; every tool strict under a name the API accepts; a strict json_schema output;
// the effort; a cache key per stable prefix; images and PDFs as data URLs; and a reasoning item passed back unchanged
// in the next turn of a tool loop. Usage counts cached tokens apart from fresh input.
// req: AI-27
// req: AI-28
// req: AI-79
func TestOpenAIRequestsOnTheWire(t *testing.T) {
	fake := &fakeResponses{}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	c := New(srv.URL+"/v1", testKey, srv.Client())
	ctx := context.Background()
	r := ports.ModelRequest{Model: "gpt-6", System: "You answer from the tools' figures only.", MaxTokens: 2048, Effort: "high", Cache: true,
		Tools:  []ports.ModelTool{{Name: "reports.figure", Description: "A figure of a report.", Schema: json.RawMessage(porttest.ModelSchema)}},
		Output: &ports.ModelSchema{Name: "answer.v1", Schema: json.RawMessage(porttest.ModelSchema)},
		Messages: []ports.ModelMessage{{Role: ports.RoleUser, Parts: []ports.ModelPart{
			{Kind: ports.PartText, Text: "Please use the tool."},
			{Kind: ports.PartImage, MediaType: "image/png", Data: []byte("img")},
			{Kind: ports.PartDocument, MediaType: "application/pdf", Data: []byte("pdf")},
		}}}}
	resp, err := c.Call(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if h := fake.headers[0]; h.Get("Authorization") != "Bearer "+testKey || strings.Contains(fake.paths[0], testKey) || fake.paths[0] != "/v1/responses" {
		t.Errorf("headers %v, path %s", h, fake.paths[0])
	}
	wire, _ := json.Marshal(fake.requests[0])
	for _, want := range []string{`"store":false`, `"include":["reasoning.encrypted_content"]`, `"strict":true`, `"name":"reports_figure"`,
		`"format":{"name":"answer_v1"`, `"type":"json_schema"`, `"reasoning":{"effort":"high"}`, `"prompt_cache_key":"fyniq-`,
		`"image_url":"data:image/png;base64,aW1n"`, `"file_data":"data:application/pdf;base64,cGRm"`, `"max_output_tokens":2048`,
		`"instructions":"You answer from the tools' figures only."`} {
		if !strings.Contains(string(wire), want) {
			t.Errorf("the request lacks %s: %s", want, wire)
		}
	}
	if resp.StopReason != ports.StopToolCall || len(resp.Parts) != 2 || resp.Parts[0].Kind != ports.PartReasoning || resp.Parts[1].Tool != "reports.figure" {
		t.Fatalf("the answer: %+v", resp)
	}
	if resp.Usage != (ports.ModelUsage{Input: 100, Output: 10, CacheRead: 80, Reasoning: 4}) {
		t.Errorf("the usage: %+v", resp.Usage)
	}
	r.Messages = append(r.Messages, ports.ModelMessage{Role: ports.RoleAssistant, Parts: resp.Parts},
		ports.ModelMessage{Role: ports.RoleUser, Parts: []ports.ModelPart{{Kind: ports.PartToolResult, CallID: resp.Parts[1].CallID, Text: "5000.00"}}})
	if _, err := c.Call(ctx, r); err != nil {
		t.Fatal(err)
	}
	next, _ := json.Marshal(fake.requests[1]["input"])
	if !strings.Contains(string(next), `"encrypted_content":"enc-abc"`) || !strings.Contains(string(next), `{"call_id":"call-1","output":"5000.00","type":"function_call_output"}`) ||
		!strings.Contains(string(next), `"arguments":"{\"report\":\"pl\"}"`) {
		t.Errorf("the next turn: %s", next)
	}
}

// A refusal is the port's error, scrubbed of the key (AI-79); an answer cut short or declined says so; the model list
// carries each model's shutdown date (AI-84).
// req: AI-79
// req: AI-80
// req: AI-84
func TestOpenAIErrorsStopsAndModelList(t *testing.T) {
	fake := &fakeResponses{}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	c := New(srv.URL+"/v1", testKey, srv.Client())
	ctx := context.Background()
	hello := ports.ModelRequest{Model: "gpt-6", MaxTokens: 64, Messages: []ports.ModelMessage{{Role: ports.RoleUser, Parts: []ports.ModelPart{{Kind: ports.PartText, Text: "Hello."}}}}}
	for status, want := range map[int]error{429: ports.ErrModelBusy, 503: ports.ErrModelBusy, 404: ports.ErrModelNotFound, 401: ports.ErrModelAuth, 400: ports.ErrModelRequest} {
		fake.status, fake.message = status, "refused for key "+testKey
		if _, err := c.Call(ctx, hello); !errors.Is(err, want) || strings.Contains(err.Error(), testKey) {
			t.Errorf("%d: %v", status, err)
		}
	}
	fake.status = 0

	names := c.port
	for _, tc := range []struct {
		r    response
		want string
	}{
		{response{Status: "incomplete", IncompleteDetails: &struct {
			Reason string `json:"reason"`
		}{"max_output_tokens"}}, ports.StopMaxTokens},
		{response{Status: "incomplete", IncompleteDetails: &struct {
			Reason string `json:"reason"`
		}{"content_filter"}}, ports.StopRefusal},
		{response{Status: "completed", Output: []json.RawMessage{json.RawMessage(`{"type":"message","content":[{"type":"refusal","refusal":"I cannot help with that."}]}`)}}, ports.StopRefusal},
	} {
		out, err := names(tc.r, modelwireNames())
		if err != nil || out.StopReason != tc.want {
			t.Errorf("%+v: %v %s", tc.r, err, out.StopReason)
		}
	}
	if _, err := names(response{Status: "failed", Error: &struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{"server_error", "try again"}}, modelwireNames()); !errors.Is(err, ports.ErrModelBusy) {
		t.Errorf("a failed response: %v", err)
	}

	page, err := c.ListModels(ctx, "")
	if err != nil || len(page.Models) != 2 || page.Next != "" || page.Models[0].Shutdown != nil || page.Models[1].Shutdown == nil ||
		page.Models[1].Shutdown.Format("2006-01-02") != "2027-03-31" || page.Models[0].Created == nil {
		t.Errorf("the model list: %v %+v", err, page)
	}
	if _, err := New(srv.URL+"/v1", "", srv.Client()).Call(ctx, hello); !errors.Is(err, ports.ErrModelAuth) {
		t.Errorf("no key: %v", err)
	}
}
