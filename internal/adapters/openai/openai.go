// Package openai is the model provider on OpenAI's API (PRD-001 AI-71): the Responses API with a project API key, over
// plain HTTP through the egress guard, with no SDK (AI-70, NFR-IMG-04).
//
// The request and answer follow OpenAI's API reference for POST /v1/responses and GET /v1/models (retrieved 14 Nov
// 2026): model, instructions, input items (messages with input_text, input_image and input_file parts; function_call
// and function_call_output; reasoning items with their encrypted_content), function tools with parameters and strict,
// text.format json_schema with strict, max_output_tokens, reasoning.effort, stream, store, include and
// prompt_cache_key; the answer's output items, status, incomplete_details, usage (input and output tokens, cached and
// reasoning tokens) and model; the model list, unpaged, with shutdown_date. Nothing is stored at OpenAI (store false),
// so reasoning travels back to it encrypted (include reasoning.encrypted_content). The streamed events are read by
// their type: output text deltas, each output item once done, and the whole response when it completes
// [verify: the streaming events' payloads].
package openai

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/modelwire"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/ports"
)

// DefaultBase is the OpenAI API's base.
const DefaultBase = "https://api.openai.com/v1"

// Client is a connection to OpenAI's API.
type Client struct {
	Base string       // the API's base, or a data-residency base the organisation's project uses
	Key  string       // a project API key
	HTTP *http.Client // through the egress guard, to the connection's host only (SEC-36)
}

var _ ports.ModelProvider = (*Client)(nil)

// New returns a client of the API at base with key.
func New(base, key string, client *http.Client) *Client {
	return &Client{Base: strings.TrimSuffix(base, "/"), Key: key, HTTP: client}
}

// Capabilities implements ports.ModelProvider: what this adapter carries, each proved by its conformance suite.
func (c *Client) Capabilities() ports.ModelCapabilities {
	return ports.ModelCapabilities{Tools: true, StrictOutput: true, Streaming: true, Images: true, PDF: true, Caching: true, Reasoning: true}
}

type tool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
	Strict      bool            `json:"strict"`
}

type format struct {
	Type   string          `json:"type"`
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema"`
	Strict bool            `json:"strict"`
}

type request struct {
	Model        string            `json:"model"`
	Instructions string            `json:"instructions,omitempty"`
	Input        []json.RawMessage `json:"input"`
	Tools        []tool            `json:"tools,omitempty"`
	Text         *struct {
		Format format `json:"format"`
	} `json:"text,omitempty"`
	MaxOutputTokens int `json:"max_output_tokens,omitempty"`
	Reasoning       *struct {
		Effort string `json:"effort"`
	} `json:"reasoning,omitempty"`
	Stream         bool     `json:"stream,omitempty"`
	Store          bool     `json:"store"`
	Include        []string `json:"include,omitempty"`
	PromptCacheKey string   `json:"prompt_cache_key,omitempty"`
}

type content struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
	Filename string `json:"filename,omitempty"`
	FileData string `json:"file_data,omitempty"`
}

func dataURL(mediaType string, data []byte) string {
	return "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// wire is r as the Responses API takes it, and the map of its tools' names.
func wire(r ports.ModelRequest, stream bool) (request, modelwire.ToolNames, error) {
	names := modelwire.NewToolNames(r.Tools)
	out := request{Model: r.Model, Instructions: r.System, MaxOutputTokens: r.MaxTokens, Stream: stream, Input: []json.RawMessage{},
		Include: []string{"reasoning.encrypted_content"}}
	for _, t := range r.Tools {
		schema := t.Schema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{}}`)
		}
		out.Tools = append(out.Tools, tool{Type: "function", Name: names.Wire(t.Name), Description: t.Description, Parameters: schema, Strict: true})
	}
	if r.Output != nil {
		name := modelwire.NewToolNames([]ports.ModelTool{{Name: r.Output.Name}}).Wire(r.Output.Name)
		if name == "" {
			name = "answer"
		}
		out.Text = &struct {
			Format format `json:"format"`
		}{format{Type: "json_schema", Name: name, Schema: r.Output.Schema, Strict: true}}
	}
	if r.Effort != "" {
		out.Reasoning = &struct {
			Effort string `json:"effort"`
		}{r.Effort}
	}
	// OpenAI caches long prefixes by itself; a key per stable prefix (the instructions and the tools) keeps similar
	// requests on the same cache (AI-27).
	if r.Cache {
		h := sha256.New()
		h.Write([]byte(r.System))
		for _, t := range r.Tools {
			h.Write([]byte("\x00" + t.Name + "\x00"))
			h.Write(t.Schema)
		}
		out.PromptCacheKey = "fyniq-" + hex.EncodeToString(h.Sum(nil))[:32]
	}
	for _, m := range r.Messages {
		var parts []content
		flush := func() error {
			if len(parts) == 0 {
				return nil
			}
			msg := map[string]any{"role": m.Role, "content": parts}
			if m.Role == ports.RoleAssistant {
				var text strings.Builder
				for _, p := range parts {
					text.WriteString(p.Text)
				}
				msg["content"] = text.String()
			}
			b, err := json.Marshal(msg)
			out.Input = append(out.Input, b)
			parts = nil
			return err
		}
		for _, p := range m.Parts {
			var item any
			switch p.Kind {
			case ports.PartText:
				parts = append(parts, content{Type: "input_text", Text: p.Text})
				continue
			case ports.PartImage:
				parts = append(parts, content{Type: "input_image", ImageURL: dataURL(p.MediaType, p.Data)})
				continue
			case ports.PartDocument:
				parts = append(parts, content{Type: "input_file", Filename: "document.pdf", FileData: dataURL(p.MediaType, p.Data)})
				continue
			case ports.PartToolCall:
				args := string(p.Input)
				if strings.TrimSpace(args) == "" {
					args = "{}"
				}
				item = map[string]string{"type": "function_call", "call_id": p.CallID, "name": names.Wire(p.Tool), "arguments": args}
			case ports.PartToolResult:
				item = map[string]string{"type": "function_call_output", "call_id": p.CallID, "output": p.Text}
			case ports.PartReasoning:
				if len(p.Raw) == 0 {
					return request{}, names, errors.New("openai: a reasoning part without its item")
				}
				if err := flush(); err != nil {
					return request{}, names, err
				}
				out.Input = append(out.Input, p.Raw) // the provider's own item, unchanged
				continue
			default:
				return request{}, names, fmt.Errorf("openai: a part of kind %q", p.Kind)
			}
			if err := flush(); err != nil {
				return request{}, names, err
			}
			b, err := json.Marshal(item)
			if err != nil {
				return request{}, names, err
			}
			out.Input = append(out.Input, b)
		}
		if err := flush(); err != nil {
			return request{}, names, err
		}
	}
	return out, names, nil
}

type usage struct {
	InputTokens        int64 `json:"input_tokens"`
	InputTokensDetails struct {
		CachedTokens     int64 `json:"cached_tokens"`
		CacheWriteTokens int64 `json:"cache_write_tokens"`
	} `json:"input_tokens_details"`
	OutputTokens        int64 `json:"output_tokens"`
	OutputTokensDetails struct {
		ReasoningTokens int64 `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
}

// port is the usage by kind: OpenAI counts cached tokens within its input tokens, the port apart (AI-73).
func (u usage) port() ports.ModelUsage {
	return ports.ModelUsage{Input: u.InputTokens - u.InputTokensDetails.CachedTokens - u.InputTokensDetails.CacheWriteTokens,
		Output: u.OutputTokens, CacheRead: u.InputTokensDetails.CachedTokens, CacheWrite: u.InputTokensDetails.CacheWriteTokens,
		Reasoning: u.OutputTokensDetails.ReasoningTokens}
}

type response struct {
	Model             string            `json:"model"`
	Status            string            `json:"status"`
	Output            []json.RawMessage `json:"output"`
	IncompleteDetails *struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Usage usage `json:"usage"`
}

// item is an output item as the port's parts; a refusal is reported as one.
func item(raw json.RawMessage, names modelwire.ToolNames) (parts []ports.ModelPart, refused bool, err error) {
	var it struct {
		Type    string `json:"type"`
		Content []struct {
			Type    string `json:"type"`
			Text    string `json:"text"`
			Refusal string `json:"refusal"`
		} `json:"content"`
		CallID    string `json:"call_id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &it); err != nil {
		return nil, false, fmt.Errorf("openai: an output item cannot be read: %w", err)
	}
	switch it.Type {
	case "message":
		for _, c := range it.Content {
			switch c.Type {
			case "output_text":
				parts = append(parts, ports.ModelPart{Kind: ports.PartText, Text: c.Text})
			case "refusal":
				parts, refused = append(parts, ports.ModelPart{Kind: ports.PartText, Text: c.Refusal}), true
			}
		}
	case "function_call":
		args := it.Arguments
		if strings.TrimSpace(args) == "" {
			args = "{}"
		}
		if !json.Valid([]byte(args)) {
			return nil, false, errors.New("openai: a tool call's arguments are not JSON")
		}
		parts = append(parts, ports.ModelPart{Kind: ports.PartToolCall, CallID: it.CallID, Tool: names.Back(it.Name), Input: json.RawMessage(args)})
	case "reasoning":
		parts = append(parts, ports.ModelPart{Kind: ports.PartReasoning, Raw: append(json.RawMessage(nil), raw...)})
	}
	return parts, refused, nil
}

func (c *Client) port(r response, names modelwire.ToolNames) (ports.ModelResponse, error) {
	if r.Status == "failed" {
		code, msg := "", ""
		if r.Error != nil {
			code, msg = r.Error.Code, r.Error.Message
		}
		return ports.ModelResponse{}, modelwire.Error("openai", codeStatus(code), code, msg, c.Key)
	}
	out := ports.ModelResponse{Model: r.Model, Usage: r.Usage.port(), StopReason: ports.StopEnd}
	refused := false
	for _, raw := range r.Output {
		parts, ref, err := item(raw, names)
		if err != nil {
			return ports.ModelResponse{}, err
		}
		refused = refused || ref
		out.Parts = append(out.Parts, parts...)
	}
	for _, p := range out.Parts {
		if p.Kind == ports.PartToolCall {
			out.StopReason = ports.StopToolCall
		}
	}
	switch {
	case refused:
		out.StopReason = ports.StopRefusal
	case r.Status == "incomplete" && r.IncompleteDetails != nil && r.IncompleteDetails.Reason == "content_filter":
		out.StopReason = ports.StopRefusal
	case r.Status == "incomplete":
		out.StopReason = ports.StopMaxTokens
	}
	return out, nil
}

// codeStatus is the HTTP status an error code stands for, when it comes inside a response or an event.
func codeStatus(code string) int {
	switch code {
	case "rate_limit_exceeded", "insufficient_quota":
		return 429
	case "server_error", "vector_store_timeout":
		return 500
	case "model_not_found":
		return 404
	case "invalid_api_key":
		return 401
	}
	return 400
}

func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return egress.Public()
}

// do sends one request with the key in its header, never in the address (AI-79), and returns the open answer.
func (c *Client) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	if c.Key == "" {
		return nil, fmt.Errorf("openai: no API key: %w", ports.ErrModelAuth)
	}
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
	req.Header.Set("Authorization", "Bearer "+c.Key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai: the provider did not answer: %w", ports.ErrModelBusy)
	}
	if resp.StatusCode/100 != 2 {
		defer resp.Body.Close()
		var e struct {
			Error struct {
				Code    any    `json:"code"`
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&e)
		kind := e.Error.Type
		if code, ok := e.Error.Code.(string); ok && code != "" {
			kind = code
		}
		return nil, modelwire.Error("openai", resp.StatusCode, kind, e.Error.Message, c.Key)
	}
	return resp, nil
}

// Call implements ports.ModelProvider.
func (c *Client) Call(ctx context.Context, r ports.ModelRequest) (ports.ModelResponse, error) {
	body, names, err := wire(r, false)
	if err != nil {
		return ports.ModelResponse{}, err
	}
	resp, err := c.do(ctx, http.MethodPost, "/responses", body)
	if err != nil {
		return ports.ModelResponse{}, err
	}
	defer resp.Body.Close()
	var out response
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&out); err != nil {
		return ports.ModelResponse{}, fmt.Errorf("openai: the answer cannot be read: %w", err)
	}
	return c.port(out, names)
}

// Stream implements ports.ModelProvider: each piece of text as it arrives, each tool call once whole, and the whole
// answer from the completed response.
func (c *Client) Stream(ctx context.Context, r ports.ModelRequest, each func(ports.ModelEvent) error) (ports.ModelResponse, error) {
	body, names, err := wire(r, true)
	if err != nil {
		return ports.ModelResponse{}, err
	}
	resp, err := c.do(ctx, http.MethodPost, "/responses", body)
	if err != nil {
		return ports.ModelResponse{}, err
	}
	defer resp.Body.Close()
	var final *ports.ModelResponse
	err = modelwire.Events(resp.Body, func(_ string, data []byte) error {
		var e struct {
			Type     string          `json:"type"`
			Delta    string          `json:"delta"`
			Item     json.RawMessage `json:"item"`
			Response response        `json:"response"`
			Code     string          `json:"code"`
			Message  string          `json:"message"`
		}
		if err := json.Unmarshal(data, &e); err != nil {
			return fmt.Errorf("openai: an event cannot be read: %w", err)
		}
		switch e.Type {
		case "response.output_text.delta":
			return each(ports.ModelEvent{Text: e.Delta})
		case "response.output_item.done":
			parts, _, err := item(e.Item, names)
			if err != nil {
				return err
			}
			for i := range parts {
				if parts[i].Kind == ports.PartToolCall {
					if err := each(ports.ModelEvent{Part: &parts[i]}); err != nil {
						return err
					}
				}
			}
		case "response.completed", "response.incomplete", "response.failed":
			out, err := c.port(e.Response, names)
			if err != nil {
				return err
			}
			final = &out
		case "error":
			return modelwire.Error("openai", codeStatus(e.Code), e.Code, e.Message, c.Key)
		}
		return nil
	})
	if err != nil {
		return ports.ModelResponse{}, err
	}
	if final == nil {
		return ports.ModelResponse{}, fmt.Errorf("openai: %w", modelwire.ErrStream)
	}
	return *final, nil
}

// ListModels implements ports.ModelProvider: the API's model list, whole on one page, with each model's shutdown date.
func (c *Client) ListModels(ctx context.Context, _ string) (ports.ModelPage, error) {
	resp, err := c.do(ctx, http.MethodGet, "/models", nil)
	if err != nil {
		return ports.ModelPage{}, err
	}
	defer resp.Body.Close()
	var list struct {
		Data []struct {
			ID           string `json:"id"`
			Created      int64  `json:"created"`
			OwnedBy      string `json:"owned_by"`
			ShutdownDate string `json:"shutdown_date"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&list); err != nil {
		return ports.ModelPage{}, fmt.Errorf("openai: the model list cannot be read: %w", err)
	}
	out := ports.ModelPage{Models: []ports.ListedModel{}}
	for _, m := range list.Data {
		lm := ports.ListedModel{ID: m.ID}
		if m.Created > 0 {
			created := time.Unix(m.Created, 0).UTC()
			lm.Created = &created
		}
		if d, err := time.Parse(time.DateOnly, m.ShutdownDate); err == nil {
			lm.Shutdown = &d
		}
		out.Models = append(out.Models, lm)
	}
	return out, nil
}
