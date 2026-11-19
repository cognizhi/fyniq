package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/aitool"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/mcpsurface"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/proposal"
)

// An AI app proposes over MCP (PRD-001 AI-16, AI-42, AI-34, AI-89 rev 0.298; p1-mcp-first-party tasks 7.1 to 7.3; the owner's
// decision of 19 Nov 2026, option B). The same gates as a read: the tool is one the manifest exposes, the token holds the scope of
// its task type (fyniq.propose.<type>) or fyniq.read.proposals, and the organisation, the allowlist and the person's opt-in allow
// the app now. A proposal then goes the way a turn's does, through the catalogue's checks, the person's own right to do it, screening
// and a dry run in a rolled-back unit of work; it waits for a person to approve it in Fyniq. What the delegated token cannot do
// is stand for that person: an agent-class token reaches no approval, so another member signs it, or the person themselves on a
// step-up taken after the app proposed it. Nothing here approves, rejects, executes or withdraws.

// mcpProposalInput is what a propose tool takes (the schema the manifest pins): the payload the task type describes, the reasons,
// the records relied on, and a proposal of this app's still waiting that this one replaces.
type mcpProposalInput struct {
	Payload   map[string]any  `json:"payload"`
	Rationale string          `json:"rationale"`
	Evidence  []AIEvidenceRef `json:"evidence"`
	Revises   string          `json:"revises"`
}

// runMCPProposalTool runs a proposal tool or one of the two reads of the person's own proposals, through every gate and limit.
func (a *API) runMCPProposalTool(ctx context.Context, p authn.Principal, in *mcpToolCallInput, manifest mcpsurface.Manifest, scopes mcpsurface.Scopes) (*mcpToolCallOutput, error) {
	name := string(in.Body.Tool)
	need, err := scopes.Needs(name, in.Body.Input)
	if err != nil {
		return nil, problem(CodeMCPScope, "This call asks for something the MCP server does not expose: "+err.Error()+".", "List the tools and call one of them")
	}
	if missing := slices.DeleteFunc(slices.Clone(need), func(s string) bool { return slices.Contains(p.Scopes, s) }); len(missing) > 0 {
		return nil, problem(CodeMCPScope, "This call needs "+strings.Join(missing, " ")+", which the app was not granted.", "Connect the app again and allow that access").
			withHeader(headerNeedScopes, strings.Join(missing, " "))
	}
	if err := a.mcpAdmit(ctx, p, in.Body.App); err != nil {
		return nil, err
	}
	if err := a.mcpLimit(ctx, p, in.Body.App); err != nil {
		return nil, err
	}
	var def mcpsurface.Definition
	for _, d := range manifest.Tools {
		if d.Name == name {
			def = d
		}
	}
	if err := checkSchema(def.InputSchema, in.Body.Input); err != nil {
		return nil, err
	}
	var content any
	switch name {
	case mcpsurface.ToolProposalsList:
		content, err = a.mcpListProposals(ctx, p, in.Body.App, in.Body.Input)
	case mcpsurface.ToolProposalsGet:
		content, err = a.mcpGetProposal(ctx, p, in.Body.App, in.Body.Input)
	default:
		content, err = a.mcpPropose(ctx, p, in.Body.App, in.Body.AppVersion, scopes.TaskTypeOf(name), def, in.Body.Input)
	}
	if err != nil {
		return nil, err
	}
	out := AIToolResult{Tool: name, Version: def.Version, Facts: []aitool.Fact{}, Content: content, Omitted: []aitool.Omission{}}
	if err := a.mcpFit(ctx, p, &out); err != nil {
		return nil, err
	}
	return &mcpToolCallOutput{Body: out}, nil
}

// checkSchema is the input against the tool's pinned schema, in the platform's words.
func checkSchema(schema json.RawMessage, input map[string]any) error {
	if input == nil {
		input = map[string]any{}
	}
	words, err := schemaErrors(schema, input, "input")
	if err != nil {
		return err
	}
	if len(words) > 0 {
		return problem(CodeToolInput, "The input is not what the tool takes: "+strings.Join(words, "; ")+".", "Send the input as the tool's schema describes it")
	}
	return nil
}

// mcpPropose makes a proposal of taskType for the person, from the app: as the assistant's turn does, with the person's own
// rights, and no more.
func (a *API) mcpPropose(ctx context.Context, p authn.Principal, app, appVersion, taskType string, def mcpsurface.Definition, input map[string]any) (any, error) {
	tt, ok := a.catalogue().Get(taskType)
	if !ok {
		return nil, problem(CodeProposalType, taskType+" is not a task type of this release.", "List the tools and call one of them")
	}
	if why := proposal.Refuse(tt.ID, tt.Command); why != "" { // the catalogue and the manifest refused it already; refuse it again by name
		return nil, problem(CodeProposalType, tt.ID+" can never be proposed: it is "+why+" (§9.6).", "Leave it to a person to do")
	}
	cmd, ok := a.commands()[tt.Command]
	if !ok {
		return nil, problem(CodeProposalType, tt.ID+" is not built in this release.", "Propose one of the task types the server lists")
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	var body mcpProposalInput
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		return nil, problem(CodeToolInput, "The input is not what the tool takes.", "Send the input as the tool's schema describes it")
	}
	if err := a.mcpProposalLimit(ctx, p, app); err != nil {
		return nil, err
	}
	payloadRaw, err := json.Marshal(body.Payload)
	if err != nil {
		return nil, err
	}
	payload, sum, err := proposal.Canonical(payloadRaw)
	if err != nil {
		return nil, problem(CodeInvalid, "The payload is not a JSON object.", "Send the payload as the task type's input schema describes it")
	}
	via := &mcpOrigin{App: app, ToolSHA256: def.SHA256}
	revises, err := a.revisable(ctx, p, tt, body.Revises, via)
	if err != nil {
		return nil, err
	}
	in := AIPropose{TaskType: tt.ID, Payload: body.Payload, Rationale: body.Rationale, Evidence: body.Evidence,
		Model: cut(app+" "+appVersion, 200), Prompt: "mcp@1", Revises: body.Revises}
	made, err := a.draftProposal(ctx, p, tt, in, payload, sum, via)
	if err != nil {
		return nil, err
	}
	reason, impact, err := a.checkProposal(ctx, p, tt, cmd, in, payload)
	if err != nil {
		_, _ = a.endProposal(ctx, p, made, "it could not be checked: "+plainError(err), proposal.Impact{}, tt, cmd, nil)
		return nil, err
	}
	if reason, err = a.endProposal(ctx, p, made, reason, impact, tt, cmd, revises); err != nil {
		return nil, err
	}
	out := map[string]any{"id": made.ID.String(), "task_type": tt.ID, "status": storage.ProposalPending, "summary": impact.Summary,
		"payload_sha256": sum, "expires_at": made.ExpiresAt.UTC().Format(time.RFC3339),
		"next": "A person approves it in Fyniq before anything changes. Read its status with proposals.get."}
	if reason != "" {
		out["status"], out["reason"], out["summary"] = storage.ProposalInvalid, reason, ""
		out["next"] = "It was not kept. Correct what the reason says and propose it again."
	}
	return out, nil
}

// mcpProposalLimit refuses a proposal over the organisation's limit of proposals an hour for this person's app (AI-43): a person
// approves each one, so a script that proposes faster than they can read stops here.
func (a *API) mcpProposalLimit(ctx context.Context, p authn.Principal, app string) error {
	var wait time.Duration
	err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		today, err := orgToday(ctx, a, u)
		if err != nil {
			return err
		}
		changes, err := u.Settings().Changes(ctx, "")
		if err != nil {
			return err
		}
		d, _ := settings.Lookup("mcp.proposals_per_hour")
		max, _ := strconv.Atoi(settings.On(d, changes, today).Value)
		now := a.deps.Clock.Now()
		since := now.Add(-time.Hour)
		made, err := u.AIProposals().List(ctx, storage.AIProposalQuery{RequestedBy: &p.UserID, Agent: "mcp", Since: &since, Limit: 500})
		if err != nil {
			return err
		}
		made = slices.DeleteFunc(made, func(m storage.AIProposal) bool { return m.MCPApp != app })
		if max > 0 && len(made) >= max {
			oldest := made[len(made)-1].ProposedAt // newest first
			wait = oldest.Add(time.Hour).Sub(now)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if wait > 0 {
		return problem(CodeMCPLimit, "This app has proposed as many changes as the organisation allows for now.", "Wait a little, and let the person approve what is waiting").
			withHeader("Retry-After", strconv.Itoa(int(wait/time.Second)+1))
	}
	return nil
}

// mcpListProposals are the person's own proposals this app made, newest first.
func (a *API) mcpListProposals(ctx context.Context, p authn.Principal, app string, input map[string]any) (any, error) {
	q := storage.AIProposalQuery{RequestedBy: &p.UserID, Agent: "mcp", Limit: 200}
	limit := 20
	if v, ok := input["status"].(string); ok {
		q.Statuses = []string{v}
	}
	if v, ok := input["limit"].(float64); ok {
		limit = int(v)
	}
	var rows []AIProposalSummary
	err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		got, err := u.AIProposals().List(ctx, q)
		for _, m := range got {
			if m.MCPApp == app && len(rows) < limit {
				rows = append(rows, a.proposalSummary(m))
			}
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []AIProposalSummary{}
	}
	return map[string]any{"proposals": rows}, nil
}

// mcpGetProposal is one of the person's own proposals this app made; another person's, or another app's, is not found.
func (a *API) mcpGetProposal(ctx context.Context, p authn.Principal, app string, input map[string]any) (any, error) {
	id, err := uuid.Parse(input["id"].(string)) // the schema checked it is a UUID
	if err != nil {
		return nil, problem(CodeInvalid, "id is not a proposal's ID.", "Use an ID from proposals.list")
	}
	var out AIProposalSummary
	err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		m, err := u.AIProposals().Get(ctx, id)
		if errors.Is(err, ports.ErrNotFound) || err == nil && (m.RequestedBy != p.UserID || m.MCPApp != app) {
			return notFoundProposal()
		}
		out = a.proposalSummary(m)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
