package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/agents"
	"github.com/cognizhi/fyniq/internal/assistant"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A scheduled agent's run's model step (PRD-001 AI-64, AI-63, AI-14, rev 0.281; p1-scheduled-agents task 5.3, design D5
// step 5, D6): the worker's one call to the API, with its own service token. The API checks the run is running and the
// agent may still run, issues the run's delegated token, asks the AI service's /runs with the skill, its tools and the
// findings, screens the model's words, and ends the token. The worker holds no delegated token and never reaches the AI
// service; the AI service reads and proposes only with the run's token, through the API.

// ServiceAgentRuns is the class of the worker's service tokens for this operation alone.
const ServiceAgentRuns = agents.ModelClass

// The codes of a model step's refusal.
const (
	CodeAgentRunStep = "AI-AGENT-01" // the run is not running, its skill changed, or the agent may not run now
)

// AgentRunPath names a scheduled agent's run.
type AgentRunPath struct {
	RunID string `path:"run_id" format:"uuid" doc:"The run." example:"0193a3c2-6d0e-7a1b-9c4d-5e6f7a8b9c0d"`
}

// AgentRunFinding is one of a run's deterministic findings (AI-64), as the worker made it.
type AgentRunFinding struct {
	Key      string   `json:"key" maxLength:"200" doc:"The finding's stable key." example:"calendar:gst_f5:2027-01-31"`
	Status   string   `json:"status" enum:"ok,review,blocked,due,not_assessed" doc:"Its status: ✔, ⚠, ✖, ⏰ or Not assessed." example:"due"`
	Title    string   `json:"title" maxLength:"300" doc:"In plain words." example:"File the GST return"`
	Detail   string   `json:"detail,omitempty" maxLength:"2000" doc:"What, why and what to do next." example:"The GST return for Oct to Dec 2026 is due on 31 Jan 2027 and is not filed."`
	Rule     string   `json:"rule,omitempty" maxLength:"200" doc:"The rule it applies." example:"gst.return-due"`
	Source   string   `json:"source,omitempty" maxLength:"500" doc:"The rule's source." example:"https://www.iras.gov.sg/taxes/goods-services-tax-(gst)/filing-gst/filing-gst-returns"`
	Pack     string   `json:"pack,omitempty" maxLength:"100" doc:"The statutory pack read." example:"sg-gst@2026.1"`
	Evidence []string `json:"evidence,omitempty" maxItems:"50" doc:"The records and documents behind it, as links in the web app."`
	Next     string   `json:"next,omitempty" maxLength:"500" doc:"The next step." example:"Record it in Compliance › Statutory calendar once it is filed"`
	DueOn    string   `json:"due_on,omitempty" format:"date" doc:"When it is due." example:"2027-01-31"`
	Window   string   `json:"window,omitempty" maxLength:"20" doc:"The deadline window it is in: 30, 14, 7 or 1 days ahead, or overdue." example:"14"`
	Task     string   `json:"task,omitempty" maxLength:"64" doc:"Its compliance task." example:"0193a3c2-6d0e-7a1b-9c4d-5e6f7a8b9c0d"`
}

// TransformSchema implements huma.SchemaTransformer: an example for each item of evidence (NFR-API-01).
func (AgentRunFinding) TransformSchema(_ huma.Registry, s *huma.Schema) *huma.Schema {
	itemExamples(s, map[string]any{"evidence": "/compliance/calendar"})
	return s
}

// itemExamples gives the items of s's array properties an example each, which a field tag cannot.
func itemExamples(s *huma.Schema, examples map[string]any) {
	for name, example := range examples {
		if p := s.Properties[name]; p != nil && p.Items != nil && len(p.Items.Examples) == 0 {
			p.Items.Examples = []any{example}
		}
	}
}

// AgentRunModelStep is the worker's request: the run's organisation and its findings.
type AgentRunModelStep struct {
	OrganisationID string            `json:"organisation_id" format:"uuid" doc:"The run's organisation." example:"0193a3c2-6d0e-7a1b-9c4d-5e6f7a8b9c0d"`
	Findings       []AgentRunFinding `json:"findings" nullable:"false" maxItems:"500" doc:"The run's deterministic findings."`
}

// AgentRunModelOutcome is what the model step gave: the model's words, grounded and screened, the model, what it used,
// how many proposals it made and how it ended.
type AgentRunModelOutcome struct {
	Model     string `json:"model" doc:"The model the step ran on, as its provider reported it." example:"claude-opus-5-5"`
	Text      string `json:"text" doc:"The model's words for the run report, as written: labelled AI wherever they are shown."`
	Tokens    int    `json:"tokens" doc:"The tokens the step used." example:"12000"`
	ToolCalls int    `json:"tool_calls" doc:"The reads it made." example:"4"`
	Proposals int    `json:"proposals" doc:"The proposals it made, each waiting for a person or invalid." example:"1"`
	Stop      string `json:"stop" enum:"answered,tool_limit,budget" doc:"How it ended." example:"answered"`
}

type (
	agentRunModelInput struct {
		AgentRunPath
		Body AgentRunModelStep
	}
	agentRunModelOutput struct{ Body AgentRunModelOutcome }
)

// skills are the release's agents' skills: Deps.Skills, else the release's (agents.Default).
func (a *API) skills() *agents.Skills {
	if a.deps.Skills != nil {
		return a.deps.Skills
	}
	return agents.Default()
}

func registerAgentRunModel(a *API) {
	internalOp(a, huma.Operation{
		OperationID: "runAgentModel", Method: http.MethodPost, Path: internalBase + "/agent-runs/{run_id}/model", Tags: []string{"Assistant"},
		Summary: "Ask the model for a scheduled agent's run",
		Description: "The worker's one call to the API (PRD-001 rev 0.281), with its own service token and no other: a scheduled agent's " +
			"run's model step (AI-64). The API checks the run is running, its skill unchanged and the agent still on, unpaused, with the " +
			"AI on and the person it acts for still an Owner. It then issues the run's delegated token, reading as that Owner with the " +
			"skill's tools alone and proposing only its outputs that are on (AI-63), and asks the AI service with the skill, the findings " +
			"and the reasons people rejected the agent's earlier proposals (AI-14), within the run's caps (AI-67). The model's words come " +
			"back grounded and screened; the token ends as the step does. Side effects: the run's grant, and whatever the AI service's " +
			"model calls and proposals record through the API.",
	}, []string{CodeAgentRunStep, CodeNotFound, CodeAIService, CodeScreenIncomplete}, func(ctx context.Context, in *agentRunModelInput) (*agentRunModelOutput, error) {
		org, err := uuid.Parse(in.Body.OrganisationID)
		if err != nil {
			return nil, problem(CodeInvalid, "organisation_id is not an organisation's ID.", "Send the run's organisation")
		}
		id, err := uuid.Parse(in.RunID)
		if err != nil {
			return nil, problem(CodeNotFound, "No run has that ID.", "Ask for a run of the organisation")
		}
		return a.runAgentModel(ctx, org, id, in.Body.Findings)
	})
}

// runAgentModel is the model step of run id in org.
func (a *API) runAgentModel(ctx context.Context, org, id uuid.UUID, findings []AgentRunFinding) (*agentRunModelOutput, error) {
	var req assistant.RunRequest
	var run storage.AgentRun
	err := a.deps.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		if run, err = u.AgentRuns().Get(ctx, id); err != nil {
			return err
		}
		if run.Status != storage.AgentRunRunning {
			return problem(CodeAgentRunStep, "The run is "+run.Status+", not running.", "Ask only for a run the worker started")
		}
		skill, ok := a.skills().Get(run.Agent)
		if !ok || skill.Version != run.SkillVersion || skill.SHA256 != run.SkillSHA256 {
			return problem(CodeAgentRunStep, "The agent's skill changed since the run was queued.", "Its next slot runs the new one")
		}
		now := a.deps.Clock.Now()
		st, err := agents.Enabled(ctx, u, skill, now)
		if err != nil {
			return err
		}
		if why, err := agents.Skipped(ctx, u, st, run, now); err != nil {
			return err
		} else if why != "" {
			return problem(CodeAgentRunStep, "The run may not ask the model: "+why+".", "The run fails; nothing more is read")
		}
		rejected, err := rejectedForAgent(ctx, u, run.ActsFor, skill.Agent())
		if err != nil {
			return err
		}
		today, err := orgToday(ctx, a, u)
		if err != nil {
			return err
		}
		token, grant, err := a.issueRunGrant(ctx, u, org, run, skill)
		if err != nil {
			return err
		}
		body, err := json.Marshal(findings)
		if err != nil {
			return err
		}
		calls, _ := strconv.Atoi(st.Value("tool_calls"))
		tokens, _ := strconv.Atoi(st.Value("run_tokens"))
		req = assistant.RunRequest{Token: token, Agent: skill.Agent(), Skill: skill.Agent() + "@" + strconv.Itoa(skill.Version), Instructions: skill.Instructions,
			Tools: grant.Tools, Propose: grant.TaskTypes, Today: today.Format("2006-01-02"), Trigger: run.Trigger, Findings: body, Rejected: rejected,
			MaxToolCalls: calls, MaxTokens: tokens}
		return nil
	})
	if errors.Is(err, ports.ErrNotFound) {
		return nil, problem(CodeNotFound, "No run of the organisation has that ID.", "Ask for a run of the organisation")
	}
	if err != nil {
		return nil, err
	}
	// The token ends as the step does, whatever became of it.
	defer func() {
		if err := a.deps.Store.Do(context.WithoutCancel(ctx), org, func(ctx context.Context, u storage.UnitOfWork) error {
			return a.endAIGrant(ctx, u, run.ID)
		}); err != nil && !errors.Is(err, ports.ErrNotFound) {
			logFailure(ctx, err)
		}
	}()
	if err := a.screenRejections(ctx, req.Rejected); err != nil {
		return nil, err
	}
	ans, err := a.askRun(ctx, req)
	if err != nil {
		return nil, err
	}
	text := ans.Text
	if err := a.screen(ctx, true, text); err != nil {
		var p *Problem
		if !errors.As(err, &p) || p.Code != CodeScreened {
			return nil, err
		}
		text = "Withheld: screening flagged the model's words. The findings above stand as Fyniq's checks made them."
	}
	return &agentRunModelOutput{Body: AgentRunModelOutcome{Model: ans.Model, Text: text, Tokens: int(ans.Usage.Input + ans.Usage.Output),
		ToolCalls: len(ans.ToolCalls), Proposals: len(ans.Proposals), Stop: ans.Stop}}, nil
}

// rejectedForAgent are the agent's latest proposals for user that people rejected, with their reasons (AI-14): the
// newest five, the latest last, for its next run to read as data.
func rejectedForAgent(ctx context.Context, u storage.UnitOfWork, user uuid.UUID, agent string) ([]assistant.Rejected, error) {
	ps, err := u.AIProposals().List(ctx, storage.AIProposalQuery{Statuses: []string{storage.ProposalRejected}, RequestedBy: &user, Limit: 100})
	if err != nil {
		return nil, err
	}
	var out []assistant.Rejected
	for _, p := range ps { // newest first
		if p.Run == nil || p.Agent != agent {
			continue
		}
		var impact struct {
			Summary string `json:"summary"`
		}
		_ = json.Unmarshal(p.Impact, &impact)
		out = append(out, assistant.Rejected{TaskType: p.TaskType, Summary: impact.Summary, Reason: p.Reason})
		if len(out) == 5 {
			break
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// askRun asks the AI service's /runs for a run's model step; the AI service's refusal in its words is a *Problem.
func (a *API) askRun(ctx context.Context, r assistant.RunRequest) (assistant.Answer, error) {
	s := a.deps.Assistant
	if s == nil || s.URL == "" {
		return assistant.Answer{}, problem(CodeAIService, "The AI service is not set up.", "Ask the platform's operator to set up the AI service")
	}
	token, err := s.Token(ctx)
	if err != nil {
		return assistant.Answer{}, fmt.Errorf("a token for the AI service: %w", err)
	}
	body, err := json.Marshal(r)
	if err != nil {
		return assistant.Answer{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(s.URL, "/")+"/runs", bytes.NewReader(body))
	if err != nil {
		return assistant.Answer{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	h := s.HTTP
	if h == nil {
		h = egress.Public()
	}
	resp, err := h.Do(req)
	if err != nil {
		return assistant.Answer{}, fmt.Errorf("the AI service did not answer: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return assistant.Answer{}, err
	}
	if resp.StatusCode != http.StatusOK {
		var p assistant.Problem
		if json.Unmarshal(data, &p) == nil && p.Code != "" {
			return assistant.Answer{}, problem(p.Code, p.Detail, p.Fix)
		}
		return assistant.Answer{}, fmt.Errorf("the AI service answered %d", resp.StatusCode)
	}
	var out assistant.Answer
	if err := json.Unmarshal(data, &out); err != nil {
		return assistant.Answer{}, fmt.Errorf("the AI service's answer could not be read: %w", err)
	}
	return out, nil
}
