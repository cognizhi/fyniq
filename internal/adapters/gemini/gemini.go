// Package gemini is the model provider on Google's Gemini models on Agent Platform (PRD-001 AI-71), called with the
// service's own account over plain HTTP through the egress guard, with no SDK (AI-70, NFR-IMG-04).
//
// The request and answer follow Agent Platform's generateContent API as Google documents it (formerly Vertex AI):
// POST …/publishers/google/models/<model>:generateContent, and :streamGenerateContent?alt=sse for server-sent events;
// contents of parts (text, inlineData, functionCall, functionResponse) by role user or model; systemInstruction; tools
// of functionDeclarations with parametersJsonSchema; generationConfig with maxOutputTokens, a JSON responseMimeType and
// responseJsonSchema, and thinkingConfig; the answer's candidates, finishReason, usageMetadata (prompt, candidates,
// cached content and thoughts tokens) and modelVersion. A model part carrying a thought signature goes back unchanged
// in the next turn. The model list is the publisher's models. [live-verify]: the endpoint, the field names and the
// list, against the dev cloud's Agent Platform.
package gemini

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
	"strings"

	"golang.org/x/oauth2"

	"github.com/cognizhi/fyniq/internal/adapters/modelwire"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/ports"
)

// Base is Agent Platform's endpoint for location: the global endpoint, or the location's own.
func Base(location string) string {
	if location == "global" {
		return "https://aiplatform.googleapis.com"
	}
	return "https://" + location + "-aiplatform.googleapis.com"
}

// Client is a connection to Gemini on Agent Platform in a project and location.
type Client struct {
	Base     string
	Project  string
	Location string
	Tokens   oauth2.TokenSource // the service's own account
	HTTP     *http.Client       // through the egress guard, to the endpoint's host only (SEC-36)
}

var _ ports.ModelProvider = (*Client)(nil)

// New returns a client of Gemini in project and location; base "" is the location's own endpoint.
func New(base, project, location string, tokens oauth2.TokenSource, client *http.Client) *Client {
	if base == "" {
		base = Base(location)
	}
	return &Client{Base: strings.TrimSuffix(base, "/"), Project: project, Location: location, Tokens: tokens, HTTP: client}
}

// Capabilities implements ports.ModelProvider: what this adapter carries, each proved by its conformance suite; the
// region is the connection's location (AI-88). Caching is not among them: Gemini caches implicitly, and a request
// cannot ask for it, though the usage reports what it read from the cache.
func (c *Client) Capabilities() ports.ModelCapabilities {
	return ports.ModelCapabilities{Tools: true, StrictOutput: true, Streaming: true, Images: true, PDF: true, Reasoning: true,
		Regions: []string{c.Location}}
}

type inlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type functionCall struct {
	ID   string          `json:"id,omitempty"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type functionResponse struct {
	ID       string         `json:"id,omitempty"`
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type part struct {
	Text             string            `json:"text,omitempty"`
	InlineData       *inlineData       `json:"inlineData,omitempty"`
	FunctionCall     *functionCall     `json:"functionCall,omitempty"`
	FunctionResponse *functionResponse `json:"functionResponse,omitempty"`
	Thought          bool              `json:"thought,omitempty"`
	ThoughtSignature string            `json:"thoughtSignature,omitempty"`
}

type content struct {
	Role  string            `json:"role,omitempty"`
	Parts []json.RawMessage `json:"parts"`
}

type declaration struct {
	Name                 string          `json:"name"`
	Description          string          `json:"description"`
	ParametersJSONSchema json.RawMessage `json:"parametersJsonSchema"`
}

type generationConfig struct {
	MaxOutputTokens    int             `json:"maxOutputTokens,omitempty"`
	ResponseMimeType   string          `json:"responseMimeType,omitempty"`
	ResponseJSONSchema json.RawMessage `json:"responseJsonSchema,omitempty"`
	ThinkingConfig     *struct {
		ThinkingLevel string `json:"thinkingLevel"`
	} `json:"thinkingConfig,omitempty"`
}

type request struct {
	Contents          []content `json:"contents"`
	SystemInstruction *content  `json:"systemInstruction,omitempty"`
	Tools             []struct {
		FunctionDeclarations []declaration `json:"functionDeclarations"`
	} `json:"tools,omitempty"`
	GenerationConfig generationConfig `json:"generationConfig"`
}

// role is Gemini's role for the port's: the model's turns are "model".
func role(r string) string {
	if r == ports.RoleAssistant {
		return "model"
	}
	return "user"
}

func wire(r ports.ModelRequest) (request, modelwire.ToolNames, error) {
	names := modelwire.NewToolNames(r.Tools)
	out := request{Contents: []content{}, GenerationConfig: generationConfig{MaxOutputTokens: r.MaxTokens}}
	if r.System != "" {
		raw, _ := json.Marshal(part{Text: r.System})
		out.SystemInstruction = &content{Parts: []json.RawMessage{raw}}
	}
	if len(r.Tools) > 0 {
		var ds []declaration
		for _, t := range r.Tools {
			schema := t.Schema
			if len(schema) == 0 {
				schema = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			ds = append(ds, declaration{Name: names.Wire(t.Name), Description: t.Description, ParametersJSONSchema: schema})
		}
		out.Tools = []struct {
			FunctionDeclarations []declaration `json:"functionDeclarations"`
		}{{ds}}
	}
	if r.Output != nil {
		out.GenerationConfig.ResponseMimeType, out.GenerationConfig.ResponseJSONSchema = "application/json", r.Output.Schema
	}
	if r.Effort != "" {
		out.GenerationConfig.ThinkingConfig = &struct {
			ThinkingLevel string `json:"thinkingLevel"`
		}{r.Effort}
	}
	for _, m := range r.Messages {
		wc := content{Role: role(m.Role)}
		for _, p := range m.Parts {
			if len(p.Raw) > 0 {
				wc.Parts = append(wc.Parts, p.Raw) // the model's own part, its thought signature unchanged
				continue
			}
			var wp part
			switch p.Kind {
			case ports.PartText:
				wp = part{Text: p.Text}
			case ports.PartImage, ports.PartDocument:
				wp = part{InlineData: &inlineData{MimeType: p.MediaType, Data: base64.StdEncoding.EncodeToString(p.Data)}}
			case ports.PartToolCall:
				args := p.Input
				if len(args) == 0 {
					args = json.RawMessage(`{}`)
				}
				wp = part{FunctionCall: &functionCall{ID: p.CallID, Name: names.Wire(p.Tool), Args: args}}
			case ports.PartToolResult:
				key := "output"
				if p.IsError {
					key = "error"
				}
				wp = part{FunctionResponse: &functionResponse{ID: p.CallID, Name: names.Wire(p.Tool), Response: map[string]any{key: p.Text}}}
			case ports.PartReasoning:
				return request{}, names, errors.New("gemini: a reasoning part without its part")
			default:
				return request{}, names, fmt.Errorf("gemini: a part of kind %q", p.Kind)
			}
			raw, err := json.Marshal(wp)
			if err != nil {
				return request{}, names, err
			}
			wc.Parts = append(wc.Parts, raw)
		}
		out.Contents = append(out.Contents, wc)
	}
	return out, names, nil
}

type usage struct {
	PromptTokenCount        int64 `json:"promptTokenCount"`
	CandidatesTokenCount    int64 `json:"candidatesTokenCount"`
	CachedContentTokenCount int64 `json:"cachedContentTokenCount"`
	ThoughtsTokenCount      int64 `json:"thoughtsTokenCount"`
}

// port is the usage by kind: the prompt count includes cached content, the port's input does not; thoughts are output
// (AI-73).
func (u usage) port() ports.ModelUsage {
	return ports.ModelUsage{Input: u.PromptTokenCount - u.CachedContentTokenCount, Output: u.CandidatesTokenCount + u.ThoughtsTokenCount,
		CacheRead: u.CachedContentTokenCount, Reasoning: u.ThoughtsTokenCount}
}

type answer struct {
	Candidates []struct {
		Content struct {
			Parts []json.RawMessage `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	PromptFeedback *struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
	UsageMetadata usage  `json:"usageMetadata"`
	ModelVersion  string `json:"modelVersion"`
}

// stop is the port's stop reason for a finish reason.
func stop(reason string) string {
	switch reason {
	case "MAX_TOKENS":
		return ports.StopMaxTokens
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII", "IMAGE_SAFETY":
		return ports.StopRefusal
	}
	return ports.StopEnd
}

// parts reads a candidate's parts as the port's; a part with a thought signature keeps itself whole, to go back
// unchanged.
func parts(raws []json.RawMessage, names modelwire.ToolNames) ([]ports.ModelPart, error) {
	var out []ports.ModelPart
	for _, raw := range raws {
		var p part
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("gemini: a part cannot be read: %w", err)
		}
		signed := p.ThoughtSignature != ""
		switch {
		case p.FunctionCall != nil:
			args := p.FunctionCall.Args
			if len(args) == 0 {
				args = json.RawMessage(`{}`)
			}
			id := p.FunctionCall.ID
			if id == "" {
				id = p.FunctionCall.Name
			}
			mp := ports.ModelPart{Kind: ports.PartToolCall, CallID: id, Tool: names.Back(p.FunctionCall.Name), Input: args}
			if signed {
				mp.Raw = append(json.RawMessage(nil), raw...)
			}
			out = append(out, mp)
		case p.Thought || (signed && p.Text == ""):
			out = append(out, ports.ModelPart{Kind: ports.PartReasoning, Raw: append(json.RawMessage(nil), raw...)})
		case p.Text != "":
			mp := ports.ModelPart{Kind: ports.PartText, Text: p.Text}
			if signed {
				mp.Raw = append(json.RawMessage(nil), raw...)
			}
			out = append(out, mp)
		}
	}
	return out, nil
}

func (a answer) port(names modelwire.ToolNames) (ports.ModelResponse, error) {
	out := ports.ModelResponse{Model: a.ModelVersion, Usage: a.UsageMetadata.port(), StopReason: ports.StopEnd}
	if a.PromptFeedback != nil && a.PromptFeedback.BlockReason != "" {
		out.StopReason = ports.StopRefusal
		return out, nil
	}
	if len(a.Candidates) == 0 {
		return out, nil
	}
	c := a.Candidates[0]
	ps, err := parts(c.Content.Parts, names)
	if err != nil {
		return ports.ModelResponse{}, err
	}
	out.Parts, out.StopReason = ps, stop(c.FinishReason)
	for _, p := range ps {
		if p.Kind == ports.PartToolCall && out.StopReason == ports.StopEnd {
			out.StopReason = ports.StopToolCall
		}
	}
	return out, nil
}

func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return egress.Public()
}

// do sends one request with the service's own account's token in its header, and returns the open answer.
func (c *Client) do(ctx context.Context, method, endpoint string, body any) (*http.Response, error) {
	if c.Tokens == nil {
		return nil, fmt.Errorf("gemini: no account for Agent Platform: %w", ports.ErrModelAuth)
	}
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, rd)
	if err != nil {
		return nil, err
	}
	t, err := c.Tokens.Token()
	if err != nil {
		return nil, fmt.Errorf("gemini: no access token for Agent Platform: %w", ports.ErrModelAuth)
	}
	t.SetAuthHeader(req)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini: the provider did not answer: %w", ports.ErrModelBusy)
	}
	if resp.StatusCode/100 != 2 {
		defer resp.Body.Close()
		var e struct {
			Error struct {
				Status  string `json:"status"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&e)
		return nil, modelwire.Error("gemini", resp.StatusCode, e.Error.Status, e.Error.Message)
	}
	return resp, nil
}

func (c *Client) model(model, method string) string {
	return fmt.Sprintf("%s/v1/projects/%s/locations/%s/publishers/google/models/%s:%s", c.Base, url.PathEscape(c.Project),
		url.PathEscape(c.Location), url.PathEscape(model), method)
}

// Call implements ports.ModelProvider.
func (c *Client) Call(ctx context.Context, r ports.ModelRequest) (ports.ModelResponse, error) {
	body, names, err := wire(r)
	if err != nil {
		return ports.ModelResponse{}, err
	}
	resp, err := c.do(ctx, http.MethodPost, c.model(r.Model, "generateContent"), body)
	if err != nil {
		return ports.ModelResponse{}, err
	}
	defer resp.Body.Close()
	var a answer
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&a); err != nil {
		return ports.ModelResponse{}, fmt.Errorf("gemini: the answer cannot be read: %w", err)
	}
	return a.port(names)
}

// Stream implements ports.ModelProvider: each chunk's text as it arrives and each tool call once whole; the chunks'
// parts make the answer, the last chunk's usage and finish reason close it.
func (c *Client) Stream(ctx context.Context, r ports.ModelRequest, each func(ports.ModelEvent) error) (ports.ModelResponse, error) {
	body, names, err := wire(r)
	if err != nil {
		return ports.ModelResponse{}, err
	}
	resp, err := c.do(ctx, http.MethodPost, c.model(r.Model, "streamGenerateContent")+"?alt=sse", body)
	if err != nil {
		return ports.ModelResponse{}, err
	}
	defer resp.Body.Close()
	out := ports.ModelResponse{StopReason: ports.StopEnd}
	var text strings.Builder
	finished := false
	flushText := func() {
		if text.Len() > 0 {
			out.Parts = append(out.Parts, ports.ModelPart{Kind: ports.PartText, Text: text.String()})
			text.Reset()
		}
	}
	err = modelwire.Events(resp.Body, func(_ string, data []byte) error {
		var a answer
		if err := json.Unmarshal(data, &a); err != nil {
			return fmt.Errorf("gemini: a chunk cannot be read: %w", err)
		}
		chunk, err := a.port(names)
		if err != nil {
			return err
		}
		if a.ModelVersion != "" {
			out.Model = a.ModelVersion
		}
		if a.UsageMetadata != (usage{}) {
			out.Usage = chunk.Usage
		}
		for i := range chunk.Parts {
			p := chunk.Parts[i]
			if p.Kind == ports.PartText && len(p.Raw) == 0 {
				text.WriteString(p.Text)
				if err := each(ports.ModelEvent{Text: p.Text}); err != nil {
					return err
				}
				continue
			}
			flushText()
			out.Parts = append(out.Parts, p)
			if p.Kind == ports.PartToolCall {
				if err := each(ports.ModelEvent{Part: &out.Parts[len(out.Parts)-1]}); err != nil {
					return err
				}
			}
		}
		if len(a.Candidates) > 0 && a.Candidates[0].FinishReason != "" || chunk.StopReason == ports.StopRefusal {
			finished = true
			if chunk.StopReason != ports.StopEnd {
				out.StopReason = chunk.StopReason
			}
		}
		return nil
	})
	if err != nil {
		return ports.ModelResponse{}, err
	}
	if !finished {
		return ports.ModelResponse{}, fmt.Errorf("gemini: %w", modelwire.ErrStream)
	}
	flushText()
	for _, p := range out.Parts {
		if p.Kind == ports.PartToolCall && out.StopReason == ports.StopEnd {
			out.StopReason = ports.StopToolCall
		}
	}
	return out, nil
}

// ListModels implements ports.ModelProvider: a page of Google's publisher models on Agent Platform [live-verify].
func (c *Client) ListModels(ctx context.Context, after string) (ports.ModelPage, error) {
	q := url.Values{"pageSize": {"100"}}
	if after != "" {
		q.Set("pageToken", after)
	}
	resp, err := c.do(ctx, http.MethodGet, c.Base+"/v1beta1/publishers/google/models?"+q.Encode(), nil)
	if err != nil {
		return ports.ModelPage{}, err
	}
	defer resp.Body.Close()
	var page struct {
		PublisherModels []struct {
			Name string `json:"name"`
		} `json:"publisherModels"`
		NextPageToken string `json:"nextPageToken"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&page); err != nil {
		return ports.ModelPage{}, fmt.Errorf("gemini: the model list cannot be read: %w", err)
	}
	out := ports.ModelPage{Models: []ports.ListedModel{}, Next: page.NextPageToken}
	for _, m := range page.PublisherModels {
		if id := m.Name[strings.LastIndex(m.Name, "/")+1:]; id != "" {
			out.Models = append(out.Models, ports.ListedModel{ID: id})
		}
	}
	return out, nil
}
