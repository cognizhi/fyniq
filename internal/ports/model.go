package ports

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// ModelProvider is one connection to a model provider (PRD-001 AI-70; p1-ai-providers design D1): every model call of
// every agent goes through it, whichever provider answers, so the orchestrator, the tools, the proposal service and the
// guardrails stay the same. Production: the Anthropic, OpenAI, Gemini and OpenAI-compatible adapters, each a connector
// of kind model-provider (AI-71). Local: recorded answers by default, a developer's live key on opt-in (§12.8).
type ModelProvider interface {
	// Capabilities are what this provider proved in its conformance suite; one it could not prove counts as absent
	// (AI-71).
	Capabilities() ModelCapabilities
	// Call sends a request and returns the whole answer.
	Call(ctx context.Context, r ModelRequest) (ModelResponse, error)
	// Stream sends a request, calls each with the answer's events as they arrive, and returns the whole answer. An error
	// from each stops the stream.
	Stream(ctx context.Context, r ModelRequest, each func(ModelEvent) error) (ModelResponse, error)
	// ListModels reads one page of the provider's own model list (AI-80): after is the cursor the previous page gave, ""
	// for the first.
	ListModels(ctx context.Context, after string) (ModelPage, error)
}

// ModelRequest is one call: the model, the stable system prompt, the conversation, the tools and an optional strict
// output schema, with its limits (AI-27).
type ModelRequest struct {
	// Model is the provider's model ID, as the connection's allow-list holds it (AI-80): never typed by a person.
	Model     string         `json:"model"`
	System    string         `json:"system,omitempty"`
	Messages  []ModelMessage `json:"messages"`
	Tools     []ModelTool    `json:"tools,omitempty"`
	Output    *ModelSchema   `json:"output,omitempty"`
	MaxTokens int            `json:"max_tokens"`
	// Effort tunes adaptive thinking for the route: "", "low", "medium" or "high" (AI-27).
	Effort string `json:"effort,omitempty"`
	// Cache asks the provider to cache the system prompt and the tool definitions (AI-27).
	Cache bool `json:"cache,omitempty"`
}

// The roles of a message.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// ModelMessage is one message of the conversation, in parts.
type ModelMessage struct {
	Role  string      `json:"role"`
	Parts []ModelPart `json:"parts"`
}

// The kinds of a message's part.
const (
	PartText       = "text"
	PartImage      = "image"
	PartDocument   = "document" // a PDF, read in the request (AI-28)
	PartToolCall   = "tool_call"
	PartToolResult = "tool_result"
)

// ModelPart is one part of a message: text, an image or a PDF, a tool call the model made, or a tool's result.
type ModelPart struct {
	Kind      string          `json:"kind"`
	Text      string          `json:"text,omitempty"`
	MediaType string          `json:"media_type,omitempty"`
	Data      []byte          `json:"data,omitempty"`
	CallID    string          `json:"call_id,omitempty"`
	Tool      string          `json:"tool,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	// Raw is a reasoning part's block as its provider gave it, passed back to it unchanged.
	Raw json.RawMessage `json:"raw,omitempty"`
}

// ModelTool is a tool the model may call: its name, its description for people and models alike, and its input as a
// JSON Schema, applied strictly where the provider proves it can (AI-27).
type ModelTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
}

// ModelSchema is a strict structured output: the answer must be JSON matching Schema (AI-27).
type ModelSchema struct {
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema"`
}

// The reasons an answer stops.
const (
	StopEnd       = "end"
	StopToolCall  = "tool_call"
	StopMaxTokens = "max_tokens"
	StopRefusal   = "refusal"
)

// ModelResponse is a whole answer: its parts, why it stopped, its usage, and the model and version the provider says
// answered (AI-87).
type ModelResponse struct {
	Parts      []ModelPart `json:"parts"`
	StopReason string      `json:"stop_reason"`
	Usage      ModelUsage  `json:"usage"`
	// Model is the model the provider reports; Version its version, "" when the provider reports none (AI-87).
	Model   string `json:"model"`
	Version string `json:"version,omitempty"`
}

// ModelUsage is an answer's tokens by kind, as the provider's usage fields give them (AI-73, AI-77).
type ModelUsage struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cache_read,omitempty"`
	CacheWrite int64 `json:"cache_write,omitempty"`
	Reasoning  int64 `json:"reasoning,omitempty"`
}

// ModelEvent is one event of a streamed answer: a piece of text, or a part once it is whole.
type ModelEvent struct {
	Text string     `json:"text,omitempty"`
	Part *ModelPart `json:"part,omitempty"`
}

// ModelPage is one page of a provider's model list: Next is the cursor of the next page, "" on the last.
type ModelPage struct {
	Models []ListedModel `json:"models"`
	Next   string        `json:"next,omitempty"`
}

// ListedModel is a model as the provider lists it (AI-80): its ID, and what the provider reports about it, as hints the
// conformance suite still proves (AI-71).
type ListedModel struct {
	ID string `json:"id"`
	// AliasOf names the model an alias points at, when the list says so, such as OpenRouter's alias_target (AI-78).
	AliasOf       string     `json:"alias_of,omitempty"`
	Name          string     `json:"name,omitempty"`
	Created       *time.Time `json:"created,omitempty"`
	Shutdown      *time.Time `json:"shutdown,omitempty"` // the provider's retirement or shutdown date (AI-84)
	ContextWindow int        `json:"context_window,omitempty"`
	MaxOutput     int        `json:"max_output,omitempty"`
	// Reports are the capabilities, modalities and methods the list reports, as the provider names them.
	Reports []string `json:"reports,omitempty"`
}

// ModelCapabilities are a provider's proved capabilities (AI-71).
type ModelCapabilities struct {
	Tools         bool     `json:"tools"`
	StrictOutput  bool     `json:"strict_output"`
	Streaming     bool     `json:"streaming"`
	Images        bool     `json:"images"`
	PDF           bool     `json:"pdf"`
	Caching       bool     `json:"caching"`
	Reasoning     bool     `json:"reasoning"`
	Batch         bool     `json:"batch"`
	ContextWindow int      `json:"context_window,omitempty"`
	Regions       []string `json:"regions,omitempty"` // where it processes, as its connection declares and evidences (AI-88)
}

// PartReasoning is a provider's own reasoning block (its thinking and signature), kept opaque in Raw and passed back
// unchanged in later turns to the same provider, which needs it in a tool loop; never shown, never sent elsewhere.
const PartReasoning = "reasoning"

// The errors a model provider's call ends in, so the router knows what to do (AI-76, AI-81).
var (
	// ErrModelNotFound is a model the provider does not know (any more): read its list again (AI-81).
	ErrModelNotFound = errors.New("the provider does not know this model")
	// ErrModelBusy is a rate limit, an overload or a server error: retry, or fail over to the route's next step (AI-76).
	ErrModelBusy = errors.New("the provider is busy or rate limited")
	// ErrModelAuth is a credential the provider refused: the connection needs a new one.
	ErrModelAuth = errors.New("the provider refused the credential")
	// ErrModelRequest is a request the provider refused as invalid.
	ErrModelRequest = errors.New("the provider refused the request")
)

// ErrNoCassette is a replayed request no recorded answer matches: record it in live mode on synthetic data (§12.8).
var ErrNoCassette = errors.New("no recorded answer for this request")
