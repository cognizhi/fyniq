// Package anthropic is the model provider on Claude (PRD-001 AI-71): the Anthropic Messages API with an API key, or
// Claude on Google Cloud's Agent Platform with the service's own account, over plain HTTP through the egress guard,
// with no SDK (AI-70, NFR-IMG-04).
//
// The request and answer follow the Messages API at API version 2026-10-21, as Anthropic's reference for it gives them
// (retrieved 14 Nov 2026): POST /v1/messages with x-api-key and anthropic-version; system, messages of content blocks
// (text, image and document with base64 sources, tool_use, tool_result), tools with input_schema and strict, max_tokens,
// output_config with effort and a json_schema format, cache_control; the answer's content blocks, stop_reason, usage
// (input, output, cache read and cache creation tokens) and model; server-sent events when streamed; GET /v1/models
// paged by after_id. No sampling parameter is sent: version 2026-10-21 refuses them. On Agent Platform the model is in
// the path (publishers/anthropic/models/<model>:rawPredict, :streamRawPredict) and the version in the body
// (anthropic_version) [live-verify]; Agent Platform has no Models API, so its list is the publisher's models
// [live-verify].
package anthropic

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/cognizhi/fyniq/internal/adapters/modelwire"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/ports"
)

// The Messages API's base and the API version every request names.
const (
	DefaultBase = "https://api.anthropic.com"
	Version     = "2026-10-21"
)

// Client is a connection to Claude: the Anthropic API when Key is set, Agent Platform when Project is.
type Client struct {
	Base string // the Anthropic API's base, or Agent Platform's endpoint
	Key  string // the Anthropic API key

	Project  string             // Agent Platform's project
	Location string             // Agent Platform's location, such as asia-southeast1 or global
	Tokens   oauth2.TokenSource // the service's own account on Agent Platform

	HTTP *http.Client // through the egress guard, to the connection's host only (SEC-36)
}

var _ ports.ModelProvider = (*Client)(nil)

// New returns a client of the Anthropic API at base with key.
func New(base, key string, client *http.Client) *Client {
	return &Client{Base: strings.TrimSuffix(base, "/"), Key: key, HTTP: client}
}

// AgentPlatformBase is Agent Platform's endpoint for location: the global endpoint, or the location's own.
func AgentPlatformBase(location string) string {
	if location == "global" {
		return "https://aiplatform.googleapis.com"
	}
	return "https://" + location + "-aiplatform.googleapis.com"
}

// NewAgentPlatform returns a client of Claude on Agent Platform in project and location, with the service's own
// account's tokens; base "" is the location's own endpoint.
func NewAgentPlatform(base, project, location string, tokens oauth2.TokenSource, client *http.Client) *Client {
	if base == "" {
		base = AgentPlatformBase(location)
	}
	return &Client{Base: strings.TrimSuffix(base, "/"), Project: project, Location: location, Tokens: tokens, HTTP: client}
}

func (c *Client) agentPlatform() bool { return c.Project != "" }

// Capabilities implements ports.ModelProvider: what this adapter carries, each proved by its conformance suite;
// a model's own limits are proved by its probe (AI-78).
func (c *Client) Capabilities() ports.ModelCapabilities {
	caps := ports.ModelCapabilities{Tools: true, StrictOutput: true, Streaming: true, Images: true, PDF: true, Caching: true, Reasoning: true}
	if c.agentPlatform() {
		caps.Regions = []string{c.Location}
	}
	return caps
}

type cacheControl struct {
	Type string `json:"type"`
}

type source struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type block struct {
	Type         string          `json:"type"`
	Text         string          `json:"text,omitempty"`
	Source       *source         `json:"source,omitempty"`
	ID           string          `json:"id,omitempty"`
	Name         string          `json:"name,omitempty"`
	Input        json.RawMessage `json:"input,omitempty"`
	ToolUseID    string          `json:"tool_use_id,omitempty"`
	Content      []block         `json:"content,omitempty"`
	IsError      bool            `json:"is_error,omitempty"`
	CacheControl *cacheControl   `json:"cache_control,omitempty"`
}

type message struct {
	Role    string            `json:"role"`
	Content []json.RawMessage `json:"content"`
}

type tool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"input_schema"`
	Strict       bool            `json:"strict"`
	CacheControl *cacheControl   `json:"cache_control,omitempty"`
}

type format struct {
	Type   string          `json:"type"`
	Schema json.RawMessage `json:"schema"`
}

type outputConfig struct {
	Effort string  `json:"effort,omitempty"`
	Format *format `json:"format,omitempty"`
}

type request struct {
	AnthropicVersion string        `json:"anthropic_version,omitempty"` // Agent Platform only
	Model            string        `json:"model,omitempty"`             // the Anthropic API only: Agent Platform has it in the path
	MaxTokens        int           `json:"max_tokens"`
	System           []block       `json:"system,omitempty"`
	Messages         []message     `json:"messages"`
	Tools            []tool        `json:"tools,omitempty"`
	OutputConfig     *outputConfig `json:"output_config,omitempty"`
	Stream           bool          `json:"stream,omitempty"`
}

// wire is r as the Messages API takes it, and the map of its tools' names.
func (c *Client) wire(r ports.ModelRequest, stream bool) (request, modelwire.ToolNames, error) {
	names := modelwire.NewToolNames(r.Tools)
	out := request{MaxTokens: r.MaxTokens, Stream: stream, Messages: []message{}}
	if c.agentPlatform() {
		out.AnthropicVersion = Version
	} else {
		out.Model = r.Model
	}
	// Caching marks the end of the stable prefix, which renders tools, then system: the system prompt when there is
	// one, else the last tool (AI-27).
	if r.System != "" {
		out.System = []block{{Type: "text", Text: r.System}}
		if r.Cache {
			out.System[0].CacheControl = &cacheControl{Type: "ephemeral"}
		}
	}
	for i, t := range r.Tools {
		schema := t.Schema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{}}`)
		}
		out.Tools = append(out.Tools, tool{Name: names.Wire(t.Name), Description: t.Description, InputSchema: schema, Strict: true})
		if r.Cache && r.System == "" && i == len(r.Tools)-1 {
			out.Tools[i].CacheControl = &cacheControl{Type: "ephemeral"}
		}
	}
	if r.Effort != "" || r.Output != nil {
		out.OutputConfig = &outputConfig{Effort: r.Effort}
		if r.Output != nil {
			out.OutputConfig.Format = &format{Type: "json_schema", Schema: r.Output.Schema}
		}
	}
	for _, m := range r.Messages {
		wm := message{Role: m.Role}
		for _, p := range m.Parts {
			b, err := partBlock(p, names)
			if err != nil {
				return request{}, names, err
			}
			wm.Content = append(wm.Content, b)
		}
		out.Messages = append(out.Messages, wm)
	}
	return out, names, nil
}

func partBlock(p ports.ModelPart, names modelwire.ToolNames) (json.RawMessage, error) {
	var b block
	switch p.Kind {
	case ports.PartText:
		b = block{Type: "text", Text: p.Text}
	case ports.PartImage:
		b = block{Type: "image", Source: &source{Type: "base64", MediaType: p.MediaType, Data: base64.StdEncoding.EncodeToString(p.Data)}}
	case ports.PartDocument:
		b = block{Type: "document", Source: &source{Type: "base64", MediaType: p.MediaType, Data: base64.StdEncoding.EncodeToString(p.Data)}}
	case ports.PartToolCall:
		input := p.Input
		if len(input) == 0 {
			input = json.RawMessage(`{}`)
		}
		b = block{Type: "tool_use", ID: p.CallID, Name: names.Wire(p.Tool), Input: input}
	case ports.PartToolResult:
		b = block{Type: "tool_result", ToolUseID: p.CallID, Content: []block{{Type: "text", Text: p.Text}}, IsError: p.IsError}
	case ports.PartReasoning:
		if len(p.Raw) == 0 {
			return nil, errors.New("anthropic: a reasoning part without its block")
		}
		return p.Raw, nil // the provider's own block, unchanged
	default:
		return nil, fmt.Errorf("anthropic: a part of kind %q", p.Kind)
	}
	return json.Marshal(b)
}

type usage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

func (u usage) port() ports.ModelUsage {
	return ports.ModelUsage{Input: u.InputTokens, Output: u.OutputTokens, CacheRead: u.CacheReadInputTokens, CacheWrite: u.CacheCreationInputTokens}
}

type answer struct {
	Model      string            `json:"model"`
	StopReason string            `json:"stop_reason"`
	Content    []json.RawMessage `json:"content"`
	Usage      usage             `json:"usage"`
}

// stop is the port's stop reason for the Messages API's.
func stop(reason string) string {
	switch reason {
	case "tool_use":
		return ports.StopToolCall
	case "max_tokens", "model_context_window_exceeded":
		return ports.StopMaxTokens
	case "refusal":
		return ports.StopRefusal
	}
	return ports.StopEnd // end_turn, stop_sequence, pause_turn
}

// part is a content block as the port's part; ok false for a block the port does not carry, such as a server tool's.
func part(raw json.RawMessage, names modelwire.ToolNames) (ports.ModelPart, bool, error) {
	var b struct {
		Type  string          `json:"type"`
		Text  string          `json:"text"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(raw, &b); err != nil {
		return ports.ModelPart{}, false, err
	}
	switch b.Type {
	case "text":
		return ports.ModelPart{Kind: ports.PartText, Text: b.Text}, true, nil
	case "tool_use":
		return ports.ModelPart{Kind: ports.PartToolCall, CallID: b.ID, Tool: names.Back(b.Name), Input: b.Input}, true, nil
	case "thinking", "redacted_thinking":
		return ports.ModelPart{Kind: ports.PartReasoning, Raw: append(json.RawMessage(nil), raw...)}, true, nil
	}
	return ports.ModelPart{}, false, nil
}

func (a answer) port(names modelwire.ToolNames) (ports.ModelResponse, error) {
	out := ports.ModelResponse{StopReason: stop(a.StopReason), Usage: a.Usage.port(), Model: a.Model}
	for _, raw := range a.Content {
		p, ok, err := part(raw, names)
		if err != nil {
			return ports.ModelResponse{}, fmt.Errorf("anthropic: a content block cannot be read: %w", err)
		}
		if ok {
			out.Parts = append(out.Parts, p)
		}
	}
	return out, nil
}

// post sends body to the Messages endpoint for model, streamed or not, and returns the open answer.
func (c *Client) post(ctx context.Context, model string, body request) (*http.Response, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	endpoint := c.Base + "/v1/messages"
	if c.agentPlatform() {
		method := "rawPredict"
		if body.Stream {
			method = "streamRawPredict"
		}
		endpoint = fmt.Sprintf("%s/v1/projects/%s/locations/%s/publishers/anthropic/models/%s:%s", c.Base,
			url.PathEscape(c.Project), url.PathEscape(c.Location), url.PathEscape(model), method)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if err := c.authorise(req); err != nil {
		return nil, err
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic: the provider did not answer: %w", ports.ErrModelBusy)
	}
	if resp.StatusCode/100 != 2 {
		defer resp.Body.Close()
		return nil, c.refusal(resp)
	}
	return resp, nil
}

func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return egress.Public()
}

// authorise adds the credential, in a header and never in the address (AI-79).
func (c *Client) authorise(req *http.Request) error {
	if c.agentPlatform() {
		if c.Tokens == nil {
			return errors.New("anthropic: Agent Platform needs the service's own account")
		}
		t, err := c.Tokens.Token()
		if err != nil {
			return fmt.Errorf("anthropic: no access token for Agent Platform: %w", ports.ErrModelAuth)
		}
		t.SetAuthHeader(req)
		return nil
	}
	if c.Key == "" {
		return fmt.Errorf("anthropic: no API key: %w", ports.ErrModelAuth)
	}
	req.Header.Set("x-api-key", c.Key)
	req.Header.Set("anthropic-version", Version)
	return nil
}

// refusal reads a refused request's error and maps it to the port's, scrubbed of the credential.
func (c *Client) refusal(resp *http.Response) error {
	var e struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&e)
	return modelwire.Error("anthropic", resp.StatusCode, e.Error.Type, e.Error.Message, c.Key)
}

// Call implements ports.ModelProvider.
func (c *Client) Call(ctx context.Context, r ports.ModelRequest) (ports.ModelResponse, error) {
	body, names, err := c.wire(r, false)
	if err != nil {
		return ports.ModelResponse{}, err
	}
	resp, err := c.post(ctx, r.Model, body)
	if err != nil {
		return ports.ModelResponse{}, err
	}
	defer resp.Body.Close()
	var a answer
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&a); err != nil {
		return ports.ModelResponse{}, fmt.Errorf("anthropic: the answer cannot be read: %w", err)
	}
	return a.port(names)
}

// streamBlock is a content block as its events build it.
type streamBlock struct {
	raw       map[string]any
	text      strings.Builder
	input     strings.Builder
	thinking  strings.Builder
	signature strings.Builder
}

// Stream implements ports.ModelProvider: each piece of text as it arrives, and each other part once it is whole.
func (c *Client) Stream(ctx context.Context, r ports.ModelRequest, each func(ports.ModelEvent) error) (ports.ModelResponse, error) {
	body, names, err := c.wire(r, true)
	if err != nil {
		return ports.ModelResponse{}, err
	}
	resp, err := c.post(ctx, r.Model, body)
	if err != nil {
		return ports.ModelResponse{}, err
	}
	defer resp.Body.Close()
	var out ports.ModelResponse
	var u usage
	blocks := map[int]*streamBlock{}
	done := false
	err = modelwire.Events(resp.Body, func(_ string, data []byte) error {
		var e struct {
			Type    string          `json:"type"`
			Index   int             `json:"index"`
			Message answer          `json:"message"`
			Block   json.RawMessage `json:"content_block"`
			Delta   struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
				Thinking    string `json:"thinking"`
				Signature   string `json:"signature"`
				StopReason  string `json:"stop_reason"`
			} `json:"delta"`
			Usage usage `json:"usage"`
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(data, &e); err != nil {
			return fmt.Errorf("anthropic: an event cannot be read: %w", err)
		}
		switch e.Type {
		case "message_start":
			out.Model, u = e.Message.Model, e.Message.Usage
		case "content_block_start":
			b := &streamBlock{raw: map[string]any{}}
			if err := json.Unmarshal(e.Block, &b.raw); err != nil {
				return err
			}
			if t, _ := b.raw["text"].(string); t != "" {
				b.text.WriteString(t)
			}
			blocks[e.Index] = b
		case "content_block_delta":
			b := blocks[e.Index]
			if b == nil {
				return fmt.Errorf("anthropic: a delta for block %d before its start", e.Index)
			}
			switch e.Delta.Type {
			case "text_delta":
				b.text.WriteString(e.Delta.Text)
				return each(ports.ModelEvent{Text: e.Delta.Text})
			case "input_json_delta":
				b.input.WriteString(e.Delta.PartialJSON)
			case "thinking_delta":
				b.thinking.WriteString(e.Delta.Thinking)
			case "signature_delta":
				b.signature.WriteString(e.Delta.Signature)
			}
		case "content_block_stop":
			b := blocks[e.Index]
			if b == nil {
				return fmt.Errorf("anthropic: block %d stopped before it started", e.Index)
			}
			p, ok, err := b.part(names)
			if err != nil {
				return err
			}
			if !ok {
				return nil
			}
			out.Parts = append(out.Parts, p)
			if p.Kind != ports.PartText && p.Kind != ports.PartReasoning {
				return each(ports.ModelEvent{Part: &out.Parts[len(out.Parts)-1]})
			}
		case "message_delta":
			out.StopReason = stop(e.Delta.StopReason)
			if e.Usage.OutputTokens > 0 {
				u.OutputTokens = e.Usage.OutputTokens
			}
		case "message_stop":
			done = true
		case "error":
			return modelwire.Error("anthropic", errorStatus(e.Error.Type), e.Error.Type, e.Error.Message, c.Key)
		}
		return nil
	})
	if err != nil {
		return ports.ModelResponse{}, err
	}
	if !done {
		return ports.ModelResponse{}, fmt.Errorf("anthropic: %w", modelwire.ErrStream)
	}
	out.Usage = u.port()
	return out, nil
}

// errorStatus is the HTTP status an error event's type stands for, mid-stream.
func errorStatus(kind string) int {
	switch kind {
	case "overloaded_error":
		return 529
	case "rate_limit_error":
		return 429
	case "api_error":
		return 500
	case "not_found_error":
		return 404
	case "authentication_error", "permission_error":
		return 401
	}
	return 400
}

// part is the whole block as the port's part.
func (b *streamBlock) part(names modelwire.ToolNames) (ports.ModelPart, bool, error) {
	switch b.raw["type"] {
	case "text":
		b.raw["text"] = b.text.String()
	case "tool_use":
		input := b.input.String()
		if strings.TrimSpace(input) == "" {
			input = "{}"
		}
		if !json.Valid([]byte(input)) {
			return ports.ModelPart{}, false, errors.New("anthropic: a streamed tool call's input is not whole JSON")
		}
		b.raw["input"] = json.RawMessage(input)
	case "thinking":
		b.raw["thinking"], b.raw["signature"] = b.thinking.String(), b.signature.String()
	}
	raw, err := json.Marshal(b.raw)
	if err != nil {
		return ports.ModelPart{}, false, err
	}
	return part(raw, names)
}

// ListModels implements ports.ModelProvider: a page of the Anthropic API's models, or of the publisher's on Agent
// Platform [live-verify].
func (c *Client) ListModels(ctx context.Context, after string) (ports.ModelPage, error) {
	q := url.Values{}
	endpoint := c.Base + "/v1/models"
	if c.agentPlatform() {
		endpoint = c.Base + "/v1beta1/publishers/anthropic/models"
		q.Set("pageSize", "100")
		if after != "" {
			q.Set("pageToken", after)
		}
	} else {
		q.Set("limit", "100")
		if after != "" {
			q.Set("after_id", after)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return ports.ModelPage{}, err
	}
	if err := c.authorise(req); err != nil {
		return ports.ModelPage{}, err
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return ports.ModelPage{}, fmt.Errorf("anthropic: the provider did not answer: %w", ports.ErrModelBusy)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return ports.ModelPage{}, c.refusal(resp)
	}
	if c.agentPlatform() {
		return publisherModels(resp.Body)
	}
	var page struct {
		Data []struct {
			ID             string                     `json:"id"`
			DisplayName    string                     `json:"display_name"`
			CreatedAt      time.Time                  `json:"created_at"`
			MaxInputTokens int                        `json:"max_input_tokens"`
			MaxTokens      int                        `json:"max_tokens"`
			Capabilities   map[string]json.RawMessage `json:"capabilities"`
		} `json:"data"`
		HasMore bool   `json:"has_more"`
		LastID  string `json:"last_id"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&page); err != nil {
		return ports.ModelPage{}, fmt.Errorf("anthropic: the model list cannot be read: %w", err)
	}
	out := ports.ModelPage{Models: []ports.ListedModel{}}
	for _, m := range page.Data {
		lm := ports.ListedModel{ID: m.ID, Name: m.DisplayName, ContextWindow: m.MaxInputTokens, MaxOutput: m.MaxTokens}
		if !m.CreatedAt.IsZero() {
			created := m.CreatedAt
			lm.Created = &created
		}
		lm.Reports = supported(m.Capabilities)
		out.Models = append(out.Models, lm)
	}
	if page.HasMore {
		out.Next = page.LastID
	}
	return out, nil
}

// supported names the capabilities a model's entry reports as supported, as hints its probe still proves (AI-71).
func supported(caps map[string]json.RawMessage) []string {
	var out []string
	for name, raw := range caps {
		var leaf struct {
			Supported bool `json:"supported"`
		}
		if json.Unmarshal(raw, &leaf) == nil && leaf.Supported {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// publisherModels reads a page of Agent Platform's publisher models [live-verify].
func publisherModels(r io.Reader) (ports.ModelPage, error) {
	var page struct {
		PublisherModels []struct {
			Name      string `json:"name"` // publishers/anthropic/models/<id>
			VersionID string `json:"versionId"`
		} `json:"publisherModels"`
		NextPageToken string `json:"nextPageToken"`
	}
	if err := json.NewDecoder(io.LimitReader(r, 8<<20)).Decode(&page); err != nil {
		return ports.ModelPage{}, fmt.Errorf("anthropic: Agent Platform's model list cannot be read: %w", err)
	}
	out := ports.ModelPage{Models: []ports.ListedModel{}, Next: page.NextPageToken}
	for _, m := range page.PublisherModels {
		id := m.Name[strings.LastIndex(m.Name, "/")+1:]
		if id != "" {
			out.Models = append(out.Models, ports.ListedModel{ID: id})
		}
	}
	return out, nil
}
