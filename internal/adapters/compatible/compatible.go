// Package compatible is the model provider on any endpoint that speaks OpenAI's Chat Completions API (PRD-001 AI-71):
// Moonshot Kimi, xAI Grok, OpenRouter, a LiteLLM gateway, or a self-hosted vLLM, Ollama or LM Studio server, over
// plain HTTP through the egress guard, with no SDK (AI-70, NFR-IMG-04).
//
// The request and answer are Chat Completions' as these endpoints implement it: POST <base>/chat/completions with
// messages (system, user with text and image_url parts, assistant with tool_calls, tool with tool_call_id), function
// tools with parameters and strict, a json_schema response_format, max_tokens, reasoning_effort, stream with
// stream_options.include_usage; the answer's choices, finish_reason, message (content, tool_calls, refusal), usage and
// model; streamed chunks of deltas ending in [DONE]; GET <base>/models. Endpoints differ, so the adapter claims nothing
// a connection's probe has not proved (AI-71): Caps holds what it proved, and a request needing anything else is
// refused before it is sent.
package compatible

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/modelwire"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/ports"
)

// Client is a connection to an OpenAI-compatible endpoint.
type Client struct {
	Base string                  // the endpoint's base, ending before /chat/completions
	Key  string                  // the API key, or "" for a self-hosted endpoint that needs none
	Caps ports.ModelCapabilities // what the connection's probe proved; nothing more is claimed
	HTTP *http.Client            // through the egress guard, to the base's host or a registered private endpoint (SEC-36, SEC-37)
}

var _ ports.ModelProvider = (*Client)(nil)

// New returns a client of the endpoint at base with key, claiming caps.
func New(base, key string, caps ports.ModelCapabilities, client *http.Client) *Client {
	return &Client{Base: strings.TrimSuffix(base, "/"), Key: key, Caps: caps, HTTP: client}
}

// Carried is everything this adapter can carry; a connection proves which of it its endpoint serves.
var Carried = ports.ModelCapabilities{Tools: true, StrictOutput: true, Streaming: true, Images: true, Reasoning: true}

// Capabilities implements ports.ModelProvider: what the connection's probe proved.
func (c *Client) Capabilities() ports.ModelCapabilities { return c.Caps }

// lacks names what r needs that the connection has not proved (AI-71).
func (c *Client) lacks(r ports.ModelRequest, stream bool) error {
	var missing []string
	if len(r.Tools) > 0 && !c.Caps.Tools {
		missing = append(missing, "tool calling")
	}
	if r.Output != nil && !c.Caps.StrictOutput {
		missing = append(missing, "a strict output schema")
	}
	if stream && !c.Caps.Streaming {
		missing = append(missing, "streaming")
	}
	if r.Effort != "" && !c.Caps.Reasoning {
		missing = append(missing, "reasoning controls")
	}
	for _, m := range r.Messages {
		for _, p := range m.Parts {
			switch {
			case p.Kind == ports.PartImage && !c.Caps.Images:
				missing = append(missing, "image input")
			case p.Kind == ports.PartDocument:
				missing = append(missing, "PDF input")
			case p.Kind == ports.PartReasoning:
				missing = append(missing, "reasoning blocks")
			}
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return fmt.Errorf("this connection has not proved %s (AI-71): pick a route whose model has: %w", strings.Join(compact(missing), ", "), ports.ErrModelRequest)
}

func compact(s []string) []string {
	out := s[:0]
	for i, v := range s {
		if i == 0 || v != s[i-1] {
			out = append(out, v)
		}
	}
	return out
}

type toolCall struct {
	Index    *int   `json:"index,omitempty"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type message struct {
	Role       string     `json:"role"`
	Content    any        `json:"content"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type contentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL *struct {
		URL string `json:"url"`
	} `json:"image_url,omitempty"`
}

type function struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
	Strict      bool            `json:"strict"`
}

type request struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
	Tools    []struct {
		Type     string   `json:"type"`
		Function function `json:"function"`
	} `json:"tools,omitempty"`
	ResponseFormat *struct {
		Type       string `json:"type"`
		JSONSchema struct {
			Name   string          `json:"name"`
			Schema json.RawMessage `json:"schema"`
			Strict bool            `json:"strict"`
		} `json:"json_schema"`
	} `json:"response_format,omitempty"`
	MaxTokens       int    `json:"max_tokens,omitempty"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	Stream          bool   `json:"stream,omitempty"`
	StreamOptions   *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options,omitempty"`
}

func wire(r ports.ModelRequest, stream bool) (request, modelwire.ToolNames, error) {
	names := modelwire.NewToolNames(r.Tools)
	out := request{Model: r.Model, MaxTokens: r.MaxTokens, ReasoningEffort: r.Effort, Stream: stream}
	if stream {
		out.StreamOptions = &struct {
			IncludeUsage bool `json:"include_usage"`
		}{true}
	}
	if r.System != "" {
		out.Messages = append(out.Messages, message{Role: "system", Content: r.System})
	}
	for _, t := range r.Tools {
		schema := t.Schema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{}}`)
		}
		out.Tools = append(out.Tools, struct {
			Type     string   `json:"type"`
			Function function `json:"function"`
		}{"function", function{Name: names.Wire(t.Name), Description: t.Description, Parameters: schema, Strict: true}})
	}
	if r.Output != nil {
		name := modelwire.NewToolNames([]ports.ModelTool{{Name: r.Output.Name}}).Wire(r.Output.Name)
		if name == "" {
			name = "answer"
		}
		out.ResponseFormat = &struct {
			Type       string `json:"type"`
			JSONSchema struct {
				Name   string          `json:"name"`
				Schema json.RawMessage `json:"schema"`
				Strict bool            `json:"strict"`
			} `json:"json_schema"`
		}{Type: "json_schema"}
		out.ResponseFormat.JSONSchema.Name, out.ResponseFormat.JSONSchema.Schema, out.ResponseFormat.JSONSchema.Strict = name, r.Output.Schema, true
	}
	for _, m := range r.Messages {
		var parts []contentPart
		var calls []toolCall
		for _, p := range m.Parts {
			switch p.Kind {
			case ports.PartText:
				parts = append(parts, contentPart{Type: "text", Text: p.Text})
			case ports.PartImage:
				parts = append(parts, contentPart{Type: "image_url", ImageURL: &struct {
					URL string `json:"url"`
				}{"data:" + p.MediaType + ";base64," + base64.StdEncoding.EncodeToString(p.Data)}})
			case ports.PartToolCall:
				tc := toolCall{ID: p.CallID, Type: "function"}
				tc.Function.Name, tc.Function.Arguments = names.Wire(p.Tool), string(p.Input)
				if strings.TrimSpace(tc.Function.Arguments) == "" {
					tc.Function.Arguments = "{}"
				}
				calls = append(calls, tc)
			case ports.PartToolResult:
				out.Messages = append(out.Messages, message{Role: "tool", ToolCallID: p.CallID, Content: p.Text})
			default:
				return request{}, names, fmt.Errorf("compatible: a part of kind %q", p.Kind)
			}
		}
		switch {
		case m.Role == ports.RoleAssistant && (len(parts) > 0 || len(calls) > 0):
			var text strings.Builder
			for _, p := range parts {
				text.WriteString(p.Text)
			}
			var content any
			if text.Len() > 0 {
				content = text.String()
			}
			out.Messages = append(out.Messages, message{Role: "assistant", Content: content, ToolCalls: calls})
		case len(parts) > 0:
			out.Messages = append(out.Messages, message{Role: "user", Content: parts})
		}
	}
	return out, names, nil
}

type usage struct {
	PromptTokens        int64 `json:"prompt_tokens"`
	CompletionTokens    int64 `json:"completion_tokens"`
	PromptTokensDetails struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails struct {
		ReasoningTokens int64 `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

// port is the usage by kind: cached tokens are counted within prompt tokens, the port's input apart (AI-73).
func (u usage) port() ports.ModelUsage {
	return ports.ModelUsage{Input: u.PromptTokens - u.PromptTokensDetails.CachedTokens, Output: u.CompletionTokens,
		CacheRead: u.PromptTokensDetails.CachedTokens, Reasoning: u.CompletionTokensDetails.ReasoningTokens}
}

func stop(reason string, refused bool) string {
	switch {
	case refused || reason == "content_filter":
		return ports.StopRefusal
	case reason == "length":
		return ports.StopMaxTokens
	case reason == "tool_calls" || reason == "function_call":
		return ports.StopToolCall
	}
	return ports.StopEnd
}

func calls(tcs []toolCall, names modelwire.ToolNames) ([]ports.ModelPart, error) {
	var out []ports.ModelPart
	for _, tc := range tcs {
		args := tc.Function.Arguments
		if strings.TrimSpace(args) == "" {
			args = "{}"
		}
		if !json.Valid([]byte(args)) {
			return nil, errors.New("compatible: a tool call's arguments are not JSON")
		}
		out = append(out, ports.ModelPart{Kind: ports.PartToolCall, CallID: tc.ID, Tool: names.Back(tc.Function.Name), Input: json.RawMessage(args)})
	}
	return out, nil
}

func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return egress.Public()
}

func (c *Client) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, rd)
	if err != nil {
		return nil, err
	}
	if c.Key != "" {
		req.Header.Set("Authorization", "Bearer "+c.Key) // in a header, never the address (AI-79)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("compatible: the endpoint did not answer: %w", ports.ErrModelBusy)
	}
	if resp.StatusCode/100 != 2 {
		defer resp.Body.Close()
		var e struct {
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&e)
		return nil, modelwire.Error("the endpoint", resp.StatusCode, e.Error.Type, e.Error.Message, c.Key)
	}
	return resp, nil
}

// Call implements ports.ModelProvider.
func (c *Client) Call(ctx context.Context, r ports.ModelRequest) (ports.ModelResponse, error) {
	if err := c.lacks(r, false); err != nil {
		return ports.ModelResponse{}, err
	}
	body, names, err := wire(r, false)
	if err != nil {
		return ports.ModelResponse{}, err
	}
	resp, err := c.do(ctx, http.MethodPost, "/chat/completions", body)
	if err != nil {
		return ports.ModelResponse{}, err
	}
	defer resp.Body.Close()
	var a struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content   *string    `json:"content"`
				Refusal   *string    `json:"refusal"`
				ToolCalls []toolCall `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage usage `json:"usage"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&a); err != nil {
		return ports.ModelResponse{}, fmt.Errorf("compatible: the answer cannot be read: %w", err)
	}
	out := ports.ModelResponse{Model: a.Model, Usage: a.Usage.port(), StopReason: ports.StopEnd}
	if len(a.Choices) == 0 {
		return out, nil
	}
	ch := a.Choices[0]
	refused := ch.Message.Refusal != nil && *ch.Message.Refusal != ""
	switch {
	case refused:
		out.Parts = append(out.Parts, ports.ModelPart{Kind: ports.PartText, Text: *ch.Message.Refusal})
	case ch.Message.Content != nil && *ch.Message.Content != "":
		out.Parts = append(out.Parts, ports.ModelPart{Kind: ports.PartText, Text: *ch.Message.Content})
	}
	tcs, err := calls(ch.Message.ToolCalls, names)
	if err != nil {
		return ports.ModelResponse{}, err
	}
	out.Parts = append(out.Parts, tcs...)
	out.StopReason = stop(ch.FinishReason, refused)
	if len(tcs) > 0 && out.StopReason == ports.StopEnd {
		out.StopReason = ports.StopToolCall
	}
	return out, nil
}

// Stream implements ports.ModelProvider: each piece of text as it arrives, and each tool call, built from its deltas,
// once the stream finishes.
func (c *Client) Stream(ctx context.Context, r ports.ModelRequest, each func(ports.ModelEvent) error) (ports.ModelResponse, error) {
	if err := c.lacks(r, true); err != nil {
		return ports.ModelResponse{}, err
	}
	body, names, err := wire(r, true)
	if err != nil {
		return ports.ModelResponse{}, err
	}
	resp, err := c.do(ctx, http.MethodPost, "/chat/completions", body)
	if err != nil {
		return ports.ModelResponse{}, err
	}
	defer resp.Body.Close()
	out := ports.ModelResponse{StopReason: ports.StopEnd}
	var text, refusal strings.Builder
	var pending []toolCall
	finish, done := "", false
	err = modelwire.Events(resp.Body, func(_ string, data []byte) error {
		if string(data) == "[DONE]" {
			done = true
			return nil
		}
		var chunk struct {
			Model   string `json:"model"`
			Choices []struct {
				Delta struct {
					Content   string     `json:"content"`
					Refusal   string     `json:"refusal"`
					ToolCalls []toolCall `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage *usage `json:"usage"`
		}
		if err := json.Unmarshal(data, &chunk); err != nil {
			return fmt.Errorf("compatible: a chunk cannot be read: %w", err)
		}
		if chunk.Model != "" {
			out.Model = chunk.Model
		}
		if chunk.Usage != nil {
			out.Usage = chunk.Usage.port()
		}
		for _, ch := range chunk.Choices {
			if ch.FinishReason != nil && *ch.FinishReason != "" {
				finish = *ch.FinishReason
			}
			refusal.WriteString(ch.Delta.Refusal)
			if ch.Delta.Content != "" {
				text.WriteString(ch.Delta.Content)
				if err := each(ports.ModelEvent{Text: ch.Delta.Content}); err != nil {
					return err
				}
			}
			for _, d := range ch.Delta.ToolCalls {
				i := len(pending)
				if d.Index != nil {
					i = *d.Index
				}
				for len(pending) <= i {
					pending = append(pending, toolCall{Type: "function"})
				}
				if d.ID != "" {
					pending[i].ID = d.ID
				}
				pending[i].Function.Name += d.Function.Name
				pending[i].Function.Arguments += d.Function.Arguments
			}
		}
		return nil
	})
	if err != nil {
		return ports.ModelResponse{}, err
	}
	if !done && finish == "" {
		return ports.ModelResponse{}, fmt.Errorf("compatible: %w", modelwire.ErrStream)
	}
	refused := refusal.Len() > 0
	switch {
	case refused:
		out.Parts = append(out.Parts, ports.ModelPart{Kind: ports.PartText, Text: refusal.String()})
	case text.Len() > 0:
		out.Parts = append(out.Parts, ports.ModelPart{Kind: ports.PartText, Text: text.String()})
	}
	tcs, err := calls(pending, names)
	if err != nil {
		return ports.ModelResponse{}, err
	}
	for i := range tcs {
		out.Parts = append(out.Parts, tcs[i])
		if err := each(ports.ModelEvent{Part: &out.Parts[len(out.Parts)-1]}); err != nil {
			return ports.ModelResponse{}, err
		}
	}
	out.StopReason = stop(finish, refused)
	if len(tcs) > 0 && out.StopReason == ports.StopEnd {
		out.StopReason = ports.StopToolCall
	}
	return out, nil
}

// ListModels implements ports.ModelProvider: the endpoint's model list, whole on one page, with the hints some
// endpoints add (OpenRouter's name, context length, supported parameters and alias target).
// pricingKinds are the per-token rates of OpenRouter's pricing object, by the kind Fyniq keeps (checked 14 Nov 2026):
// strings in US dollars per token. Its audio, image-output, image-token and web-search rates are uses Fyniq never makes.
var pricingKinds = map[string]string{"prompt": "input", "completion": "output", "input_cache_read": "cache_read",
	"input_cache_write": "cache_write", "input_cache_write_1h": "cache_write_1h", "internal_reasoning": "reasoning"}

// listedPrice is the price a model list gives (AI-82 (a)), such as OpenRouter's pricing object: each rate as the
// provider's decimal text; an optional rate of zero is left out, and a discount or overrides make it conditional. Nil
// when the list gives none.
func listedPrice(pricing map[string]json.RawMessage) *ports.ListedPrice {
	if len(pricing) == 0 {
		return nil
	}
	text := func(raw json.RawMessage) string {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return s
		}
		return strings.TrimSpace(string(raw)) // a number, kept as its text
	}
	p := &ports.ListedPrice{Currency: "USD", PerToken: map[string]string{}}
	for key, raw := range pricing {
		v := text(raw)
		switch kind, ok := pricingKinds[key]; {
		case ok && (kind == "input" || kind == "output" || !zero(v)):
			p.PerToken[kind] = v
		case key == "image" && !zero(v):
			p.PerImage = v
		case key == "request" && !zero(v):
			p.PerRequest = v
		case key == "discount" && !zero(v):
			p.Conditional = true
		case key == "overrides" && v != "[]" && v != "null":
			p.Conditional = true
		}
	}
	return p
}

// zero is whether a price's text is zero, or empty.
func zero(v string) bool { return strings.Trim(v, "0.") == "" }

func (c *Client) ListModels(ctx context.Context, _ string) (ports.ModelPage, error) {
	resp, err := c.do(ctx, http.MethodGet, "/models", nil)
	if err != nil {
		return ports.ModelPage{}, err
	}
	defer resp.Body.Close()
	var list struct {
		Data []struct {
			ID                  string                     `json:"id"`
			Name                string                     `json:"name"`
			Created             int64                      `json:"created"`
			ContextLength       int                        `json:"context_length"`
			SupportedParameters []string                   `json:"supported_parameters"`
			AliasTarget         string                     `json:"alias_target"`
			Pricing             map[string]json.RawMessage `json:"pricing"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&list); err != nil {
		return ports.ModelPage{}, fmt.Errorf("compatible: the model list cannot be read: %w", err)
	}
	out := ports.ModelPage{Models: []ports.ListedModel{}}
	for _, m := range list.Data {
		lm := ports.ListedModel{ID: m.ID, Name: m.Name, ContextWindow: m.ContextLength, Reports: m.SupportedParameters, AliasOf: m.AliasTarget,
			Price: listedPrice(m.Pricing)}
		if m.Created > 0 {
			created := time.Unix(m.Created, 0).UTC()
			lm.Created = &created
		}
		out.Models = append(out.Models, lm)
	}
	return out, nil
}
