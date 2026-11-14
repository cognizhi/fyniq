package api

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/sse"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ai"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/domain/aibudget"
	"github.com/cognizhi/fyniq/internal/domain/airoute"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// assistantAgents are the agents the AI service may ask the API to call a model for: the assistant's chat and tool loop.
// The scheduled agents run in the worker (p1-scheduled-agents).
var assistantAgents = []string{"chat"}

// AIModelCall is a model call the AI service asks for (p1-ai-assistant design D4): the agent, the classes of data the
// AI service says it carries, and the request, whose model the agent's route chooses.
type AIModelCall struct {
	Agent   string         `json:"agent" enum:"chat" doc:"The agent the call is for: its route decides the connection and model (AI-74)." example:"chat"`
	Classes []RouteClass   `json:"classes" nullable:"false" doc:"The classes of data the AI service says the call carries. The API routes it for these and every class the turn has read, whichever the AI service names (AI-83)."`
	Request AIModelRequest `json:"request"`
}

// AIModelRequest is a model call's request, without a model: the agent's route chooses it (AI-80).
type AIModelRequest struct {
	System    string               `json:"system,omitempty" doc:"The system prompt, versioned in contracts/ai/prompts/ (AI-24)."`
	Messages  []ports.ModelMessage `json:"messages" nullable:"false" doc:"The conversation, in parts: text, images, PDFs, tool calls and their results."`
	Tools     []ports.ModelTool    `json:"tools,omitempty" doc:"The read tools the model may call, from the registry (AI-49)."`
	Output    *ports.ModelSchema   `json:"output,omitempty" doc:"A strict structured output the answer must match (AI-27)."`
	MaxTokens int                  `json:"max_tokens" minimum:"1" maximum:"64000" doc:"The answer's output limit." example:"1024"`
}

// port is the request as the model port takes it.
func (r AIModelRequest) port() ports.ModelRequest {
	return ports.ModelRequest{System: r.System, Messages: r.Messages, Tools: r.Tools, Output: r.Output, MaxTokens: r.MaxTokens}
}

// AIModelCallResult is a served model call: the answer, the run of its attempts in the activity log, the step and
// connection that served it, and the classes it was routed for.
type AIModelCallResult struct {
	Run        string              `json:"run" format:"uuid" doc:"The run its attempts share in the AI activity log (AI-12)." example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a42"`
	Step       int                 `json:"step" doc:"The route's step that served it." example:"1"`
	Connection string              `json:"connection_id" format:"uuid" example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b"`
	Classes    []RouteClass        `json:"classes" nullable:"false" doc:"The classes it was routed for: the AI service's and every class the turn has read."`
	Response   ports.ModelResponse `json:"response"`
}

type (
	aiModelCallInput  struct{ Body AIModelCall }
	aiModelCallOutput struct{ Body AIModelCallResult }
)

// AIModelText is a piece of a streamed answer's text, or one of its parts once whole.
type AIModelText struct {
	ports.ModelEvent
}

// modelCallProblem is a model call's refusal in Fyniq's words: the router's and the budgets' with their codes, a
// failure otherwise.
func modelCallProblem(err error) *Problem {
	var re *airoute.Error
	var be *aibudget.Error
	switch {
	case errors.As(err, &re):
		return problem(re.Code, re.Message+".", re.Fix)
	case errors.As(err, &be):
		return problem(be.Code, be.Message+".", be.Fix)
	}
	return problem(CodeFailure, "The model call failed.", "Try again in a moment.")
}

// prepareModelCall routes for the union of the classes the AI service names and those the turn has read, and records the
// named ones as read by the turn, so a later call carries them too (AI-83).
func (a *API) prepareModelCall(ctx context.Context, in AIModelCall) (ai.Call, error) {
	p, _ := authn.From(ctx)
	if !slices.Contains(assistantAgents, in.Agent) {
		return ai.Call{}, problem(CodeInvalid, in.Agent+" is not an agent the AI service runs.", "Ask for the chat agent's call")
	}
	declared := make([]string, 0, len(in.Classes))
	for _, c := range in.Classes {
		declared = append(declared, string(c))
	}
	var classes []string
	err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		now := a.deps.Clock.Now()
		read, err := u.AIGrants().Classes(ctx, p.Turn, now)
		if err != nil {
			return err
		}
		classes = union(read, declared)
		return u.AIGrants().Widen(ctx, p.Turn, declared, now)
	})
	if errors.Is(err, ports.ErrNotFound) {
		return ai.Call{}, problem(CodeUnauthenticated, "The turn has ended.", "Start a new turn")
	}
	if err != nil {
		return ai.Call{}, err
	}
	return ai.Call{Agent: in.Agent, Classes: classes, Request: in.Request.port(), Actor: "user:" + p.UserID.String()}, nil
}

// union is a and b's classes, each once, in order.
func union(a, b []string) []string {
	out := slices.Clone(a)
	for _, c := range b {
		if !slices.Contains(out, c) {
			out = append(out, c)
		}
	}
	slices.Sort(out)
	return out
}

func (a *API) modelCallResult(res ai.Result, classes []string) AIModelCallResult {
	out := AIModelCallResult{Run: res.Run.String(), Step: res.Step, Connection: res.Connection.String(), Classes: []RouteClass{}, Response: res.Response}
	for _, c := range classes {
		out.Classes = append(out.Classes, RouteClass(c))
	}
	return out
}

// modelCallCodes are the codes a model call is refused with.
var modelCallCodes = []string{airoute.CodeNoRoute, airoute.CodeUnhandled, airoute.CodeNoStep, airoute.CodeRefused, airoute.CodeSwitchOff,
	aibudget.CodeSpent, storage.CodeRetry, storage.CodeLockTimeout}

func registerModelCalls(a *API) {
	internalOp(a, huma.Operation{
		OperationID: "callAIModel", Method: http.MethodPost, Path: internalBase + "/model-calls", Tags: []string{"Assistant"},
		Summary: "Call a model for a turn",
		Description: "Runs the agent's route for the turn's person (AI-74, AI-76): the AI switch, the budgets (AI-86), the data-class policy " +
			"for the classes the AI service names and every class the turn has read (AI-83), failover only to an eligible step, and the record " +
			"of every attempt in the AI activity log (AI-12). The provider keys stay in the API (PRD-001 §9.3 rev 0.277).",
	}, modelCallCodes, func(ctx context.Context, in *aiModelCallInput) (*aiModelCallOutput, error) {
		call, err := a.prepareModelCall(ctx, in.Body)
		if err != nil {
			return nil, err
		}
		res, err := a.router().Run(ctx, authnOrg(ctx), call)
		if err != nil {
			return nil, modelCallProblem(err)
		}
		return &aiModelCallOutput{Body: a.modelCallResult(res, call.Classes)}, nil
	})

	stream := huma.Operation{
		OperationID: "streamAIModel", Method: http.MethodPost, Path: internalBase + "/model-calls:stream", Tags: []string{"Assistant"},
		Summary: "Stream a model call for a turn",
		Description: "As callAIModel, streamed as server-sent events: text as it comes, then the served call (done), or the refusal (error), " +
			"with its code. The AI switch is checked as the answer streams, and a switched-off AI stops it within seconds (AI-13).",
	}
	withCodes(&stream, modelCallCodes)
	sse.Register(a.internal, stream, map[string]any{"text": AIModelText{}, "done": AIModelCallResult{}, "error": Problem{}},
		func(ctx context.Context, in *aiModelCallInput, send sse.Sender) {
			call, err := a.prepareModelCall(ctx, in.Body)
			if err != nil {
				_ = send.Data(*internalProblem(ctx, err))
				return
			}
			call.Stream = func(e ports.ModelEvent) error { return send.Data(AIModelText{e}) }
			res, err := a.router().Run(ctx, authnOrg(ctx), call)
			if err != nil {
				_ = send.Data(*internalProblem(ctx, modelCallProblem(err)))
				return
			}
			_ = send.Data(a.modelCallResult(res, call.Classes))
		})
}

// authnOrg is the organisation of the caller in ctx.
func authnOrg(ctx context.Context) uuid.UUID {
	p, _ := authn.From(ctx)
	return p.Organisation
}
