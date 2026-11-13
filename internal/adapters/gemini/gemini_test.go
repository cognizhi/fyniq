package gemini

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

	"golang.org/x/oauth2"

	"github.com/cognizhi/fyniq/internal/adapters/modelwire"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
)

func modelwireNames() modelwire.ToolNames { return modelwire.NewToolNames(nil) }

// fakeGemini plays porttest.Scripted behind Agent Platform's generateContent API, and keeps each request.
type fakeGemini struct {
	mu       sync.Mutex
	requests []map[string]any
	headers  []http.Header
	paths    []string
	status   int
}

func (f *fakeGemini) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paths = append(f.paths, r.URL.String())
	f.headers = append(f.headers, r.Header.Clone())
	if f.status != 0 {
		w.WriteHeader(f.status)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": f.status, "status": "RESOURCE_EXHAUSTED", "message": "quota"}})
		return
	}
	if r.Method == http.MethodGet {
		_ = json.NewEncoder(w).Encode(map[string]any{"publisherModels": []map[string]string{{"name": "publishers/google/models/gemini-3-pro"}}})
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	f.requests = append(f.requests, body)
	i := strings.Index(r.URL.Path, "/models/")
	model := strings.Split(r.URL.Path[i+len("/models/"):], ":")[0]
	req := portRequest(model, body)
	resp := porttest.Scripted(req)
	var parts []map[string]any
	for _, p := range resp.Parts {
		switch p.Kind {
		case ports.PartText:
			parts = append(parts, map[string]any{"text": p.Text})
		case ports.PartToolCall:
			parts = append(parts, map[string]any{"functionCall": map[string]any{"id": p.CallID, "name": p.Tool, "args": json.RawMessage(p.Input)},
				"thoughtSignature": "sig-abc"})
		}
	}
	u := map[string]int64{"promptTokenCount": resp.Usage.Input, "candidatesTokenCount": resp.Usage.Output, "thoughtsTokenCount": 3}
	whole := map[string]any{"candidates": []map[string]any{{"content": map[string]any{"role": "model", "parts": parts}, "finishReason": "STOP"}},
		"usageMetadata": u, "modelVersion": model + "-001"}
	if !strings.HasSuffix(r.URL.Path, ":streamGenerateContent") {
		_ = json.NewEncoder(w).Encode(whole)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	send := func(data any) {
		b, _ := json.Marshal(data)
		fmt.Fprintf(w, "data: %s\n\n", b)
	}
	for _, p := range parts {
		if text, ok := p["text"].(string); ok {
			for _, piece := range []string{text[:len(text)/2], text[len(text)/2:]} {
				send(map[string]any{"candidates": []map[string]any{{"content": map[string]any{"role": "model", "parts": []any{map[string]any{"text": piece}}}}},
					"modelVersion": model + "-001"})
			}
			continue
		}
		send(map[string]any{"candidates": []map[string]any{{"content": map[string]any{"role": "model", "parts": []any{p}}}}})
	}
	send(map[string]any{"candidates": []map[string]any{{"content": map[string]any{"role": "model", "parts": []any{}}, "finishReason": "STOP"}},
		"usageMetadata": u, "modelVersion": model + "-001"})
}

// portRequest decodes a generateContent request as the port's, so Scripted can answer it.
func portRequest(model string, body map[string]any) ports.ModelRequest {
	raw, _ := json.Marshal(body)
	var b request
	_ = json.Unmarshal(raw, &b)
	r := ports.ModelRequest{Model: model, MaxTokens: b.GenerationConfig.MaxOutputTokens}
	for _, t := range b.Tools {
		for _, d := range t.FunctionDeclarations {
			r.Tools = append(r.Tools, ports.ModelTool{Name: d.Name, Description: d.Description, Schema: d.ParametersJSONSchema})
		}
	}
	if len(b.GenerationConfig.ResponseJSONSchema) > 0 {
		r.Output = &ports.ModelSchema{Schema: b.GenerationConfig.ResponseJSONSchema}
	}
	for _, c := range b.Contents {
		m := ports.ModelMessage{Role: ports.RoleUser}
		if c.Role == "model" {
			m.Role = ports.RoleAssistant
		}
		for _, raw := range c.Parts {
			var p part
			_ = json.Unmarshal(raw, &p)
			switch {
			case p.FunctionCall != nil:
				m.Parts = append(m.Parts, ports.ModelPart{Kind: ports.PartToolCall, CallID: p.FunctionCall.ID, Tool: p.FunctionCall.Name, Input: p.FunctionCall.Args})
			case p.FunctionResponse != nil:
				out, _ := p.FunctionResponse.Response["output"].(string)
				m.Parts = append(m.Parts, ports.ModelPart{Kind: ports.PartToolResult, CallID: p.FunctionResponse.ID, Text: out})
			case p.InlineData != nil && p.InlineData.MimeType == "application/pdf":
				m.Parts = append(m.Parts, ports.ModelPart{Kind: ports.PartDocument})
			case p.InlineData != nil:
				m.Parts = append(m.Parts, ports.ModelPart{Kind: ports.PartImage})
			case p.Text != "":
				m.Parts = append(m.Parts, ports.ModelPart{Kind: ports.PartText, Text: p.Text})
			}
		}
		r.Messages = append(r.Messages, m)
	}
	return r
}

func client(srv *httptest.Server) *Client {
	return New(srv.URL, "fyniq-dev", "asia-southeast1", oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "platform-token"}), srv.Client())
}

// Gemini on Agent Platform passes the model provider's conformance suite (AI-71) against a fake of its API
// [live-verify].
// req: AI-71
func TestGeminiConformance(t *testing.T) {
	srv := httptest.NewServer(&fakeGemini{})
	defer srv.Close()
	porttest.ModelProvider(t, client(srv), "gemini-3-pro")
}

// What goes on the wire (AI-27, AI-28): the service's own account's token; the model in the path, in the project and
// location; tools as function declarations under names the API accepts; a JSON output with its schema; thinking at
// the effort; images and PDFs inline; and a tool call's thought signature passed back unchanged with its result.
// Usage counts thoughts as output.
// req: AI-27
// req: AI-28
func TestGeminiRequestsOnTheWire(t *testing.T) {
	fake := &fakeGemini{}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	c := client(srv)
	ctx := context.Background()
	r := ports.ModelRequest{Model: "gemini-3-pro", System: "You answer from the tools' figures only.", MaxTokens: 2048, Effort: "high",
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
	if fake.headers[0].Get("Authorization") != "Bearer platform-token" ||
		fake.paths[0] != "/v1/projects/fyniq-dev/locations/asia-southeast1/publishers/google/models/gemini-3-pro:generateContent" {
		t.Errorf("the request: %s %v", fake.paths[0], fake.headers[0])
	}
	wire, _ := json.Marshal(fake.requests[0])
	for _, want := range []string{`"systemInstruction":{"parts":[{"text":"You answer from the tools' figures only."}]}`, `"name":"reports_figure"`,
		`"parametersJsonSchema":`, `"responseMimeType":"application/json"`, `"responseJsonSchema":`, `"thinkingConfig":{"thinkingLevel":"high"}`,
		`"inlineData":{"data":"aW1n","mimeType":"image/png"}`, `"inlineData":{"data":"cGRm","mimeType":"application/pdf"}`, `"maxOutputTokens":2048`} {
		if !strings.Contains(string(wire), want) {
			t.Errorf("the request lacks %s: %s", want, wire)
		}
	}
	if resp.StopReason != ports.StopToolCall || resp.Parts[0].Tool != "reports.figure" || len(resp.Parts[0].Raw) == 0 ||
		resp.Model != "gemini-3-pro-001" || resp.Usage != (ports.ModelUsage{Input: 100, Output: 13, Reasoning: 3}) {
		t.Fatalf("the answer: %+v", resp)
	}
	r.Messages = append(r.Messages, ports.ModelMessage{Role: ports.RoleAssistant, Parts: resp.Parts},
		ports.ModelMessage{Role: ports.RoleUser, Parts: []ports.ModelPart{{Kind: ports.PartToolResult, CallID: resp.Parts[0].CallID, Tool: "reports.figure", Text: "5000.00"}}})
	if _, err := c.Call(ctx, r); err != nil {
		t.Fatal(err)
	}
	next, _ := json.Marshal(fake.requests[1]["contents"])
	if !strings.Contains(string(next), `"thoughtSignature":"sig-abc"`) || !strings.Contains(string(next), `"functionResponse":{"id":"call-1","name":"reports_figure","response":{"output":"5000.00"}}`) {
		t.Errorf("the next turn: %s", next)
	}
}

// Refusals map to the port's errors, finish reasons to its stop reasons, and the model list is the publisher's.
// req: AI-80
func TestGeminiErrorsStopsAndModelList(t *testing.T) {
	fake := &fakeGemini{}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	c := client(srv)
	ctx := context.Background()
	hello := ports.ModelRequest{Model: "gemini-3-pro", MaxTokens: 64, Messages: []ports.ModelMessage{{Role: ports.RoleUser, Parts: []ports.ModelPart{{Kind: ports.PartText, Text: "Hello."}}}}}
	for status, want := range map[int]error{429: ports.ErrModelBusy, 503: ports.ErrModelBusy, 404: ports.ErrModelNotFound, 403: ports.ErrModelAuth, 400: ports.ErrModelRequest} {
		fake.status = status
		if _, err := c.Call(ctx, hello); !errors.Is(err, want) {
			t.Errorf("%d: %v", status, err)
		}
	}
	fake.status = 0
	for reason, want := range map[string]string{"STOP": ports.StopEnd, "MAX_TOKENS": ports.StopMaxTokens, "SAFETY": ports.StopRefusal, "PROHIBITED_CONTENT": ports.StopRefusal} {
		if got := stop(reason); got != want {
			t.Errorf("%s: %s", reason, got)
		}
	}
	blocked := answer{PromptFeedback: &struct {
		BlockReason string `json:"blockReason"`
	}{"SAFETY"}}
	if out, _ := blocked.port(modelwireNames()); out.StopReason != ports.StopRefusal {
		t.Errorf("a blocked prompt: %s", out.StopReason)
	}
	page, err := c.ListModels(ctx, "")
	if err != nil || page.Models[0].ID != "gemini-3-pro" || !strings.Contains(fake.paths[len(fake.paths)-1], "/v1beta1/publishers/google/models?pageSize=100") {
		t.Errorf("the model list: %v %+v %s", err, page, fake.paths[len(fake.paths)-1])
	}
	if _, err := New(srv.URL, "fyniq-dev", "asia-southeast1", nil, srv.Client()).Call(ctx, hello); !errors.Is(err, ports.ErrModelAuth) {
		t.Errorf("no account: %v", err)
	}
	if Base("global") != "https://aiplatform.googleapis.com" || Base("asia-southeast1") != "https://asia-southeast1-aiplatform.googleapis.com" {
		t.Error("the endpoints")
	}
}
