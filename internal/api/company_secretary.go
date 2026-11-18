package api

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/agents"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The AI company secretary assistant's workspace (PRD-001 FR-CS-04 in part, AI-62, AI-63, AI-65, AI-67; p1-scheduled-agents
// tasks 7.1 and 7.2, design D9): the agent's state and schedule, what its latest run found, its history, one run's
// report, and Run now. It reads; Run now queues a run, which the worker runs as any other.

// CodeAgentOff is Run now for an agent that is off or paused.
const CodeAgentOff = "AI-AGENT-02"

// secretary is the release's company secretary skill.
const secretary = "company-secretary"

// AgentSchedule is the agent's schedule and caps, as its settings hold them today (AI-62, AI-67).
type AgentSchedule struct {
	Monitor      string   `json:"monitor" doc:"The daily deadline and lodgement monitor, cron-style in Singapore time." example:"0 7 * * *"`
	CrossCheck   string   `json:"cross_check" doc:"The weekly full cross-check, cron-style in Singapore time." example:"0 8 * * 1"`
	AfterClose   bool     `json:"after_close" doc:"Whether it runs after each month's close." example:"true"`
	FYEndPack    bool     `json:"fy_end_pack" doc:"Whether it runs one month after the financial year ends." example:"true"`
	Events       []string `json:"events" nullable:"false" doc:"The events that start a run." example:"[\"period_closed\",\"filing_recorded\"]"`
	QuietHours   string   `json:"quiet_hours" doc:"When its emails wait, in Singapore time." example:"22:00-07:00"`
	Recipients   []string `json:"recipients" nullable:"false" doc:"The roles it tells; its tasks belong to the first." example:"[\"owner\",\"company_secretary\"]"`
	Channels     []string `json:"channels" nullable:"false" doc:"How it tells them." example:"[\"inbox\",\"email\"]"`
	EscalateDays int      `json:"escalate_days" doc:"How many days before a deadline the Owner is told too." example:"7"`
	ToolCalls    int      `json:"tool_calls" doc:"The most reads one run may make." example:"20"`
	RunTokens    int      `json:"run_tokens" doc:"The most model tokens one run may use." example:"200000"`
}

// TransformSchema implements huma.SchemaTransformer: an example for each item of its lists (NFR-API-01).
func (AgentSchedule) TransformSchema(_ huma.Registry, s *huma.Schema) *huma.Schema {
	itemExamples(s, map[string]any{"events": "period_closed", "recipients": "owner", "channels": "inbox"})
	return s
}

// AgentNextRun is when a schedule entry runs next.
type AgentNextRun struct {
	Entry string `json:"entry" enum:"monitor,cross_check" doc:"The schedule's entry." example:"monitor"`
	At    string `json:"at" format:"date-time" example:"2026-11-16T23:00:00Z"`
}

// AgentFindingCounts are a run's findings by status.
type AgentFindingCounts struct {
	OK          int `json:"ok" example:"12"`
	Review      int `json:"review" doc:"⚠: please review." example:"1"`
	Blocked     int `json:"blocked" doc:"✖: cannot go on until fixed." example:"0"`
	Due         int `json:"due" doc:"⏰: due or overdue." example:"2"`
	NotAssessed int `json:"not_assessed" doc:"⚠ Not assessed: an input it needs is missing or out of date." example:"3"`
}

// AgentRunSummary is a run as the history lists it.
type AgentRunSummary struct {
	ID         string             `json:"id" format:"uuid" example:"0193a3c2-6d0e-7a1b-9c4d-5e6f7a8b9c12"`
	Trigger    string             `json:"trigger" doc:"What started it: schedule:<entry>, event:<kind>:<key> or run_now." example:"schedule:monitor"`
	SlotAt     string             `json:"slot_at" format:"date-time" doc:"When it was due." example:"2026-11-15T23:00:00Z"`
	Status     string             `json:"status" enum:"queued,running,done,failed,skipped" example:"done"`
	Reason     string             `json:"reason,omitempty" doc:"Why it failed or was skipped." example:"the agent is paused"`
	StartedAt  string             `json:"started_at,omitempty" format:"date-time" example:"2026-11-15T23:00:05Z"`
	FinishedAt string             `json:"finished_at,omitempty" format:"date-time" example:"2026-11-15T23:01:10Z"`
	Summary    string             `json:"summary,omitempty" doc:"Fyniq's summary, never \"all clear\" while anything is open (AI-65)." example:"2 things need you this week."`
	Counts     AgentFindingCounts `json:"counts" doc:"Its findings by status, once done."`
	ModelUsed  bool               `json:"model_used" doc:"Whether it asked the model: only when an input changed, for the monitor and events." example:"true"`
	// (AI-64, rev 0.283)
	ModelMissed string `json:"model_missed,omitempty" doc:"Why its AI step did not run when it was due to: the model unavailable, the AI off, no route or the budget spent. Its findings and tasks stand; C.2 #36 shows it." example:"the AI is switched off"`
	New         bool   `json:"new" doc:"Whether what it found differs from the run done before it: new since the last run." example:"true"`
}

// CompanySecretary is the assistant's workspace: its state, who it acts for, its schedule, when it runs next, its latest
// run and the open findings of its latest done run.
type CompanySecretary struct {
	State       string            `json:"state" enum:"off,on,paused" doc:"off until an Owner turns it on with a second approver; paused by an Owner at once." example:"on"`
	ActsFor     string            `json:"acts_for,omitempty" format:"uuid" doc:"The Owner who turned it on: its runs read and propose as them (AI-63)." example:"0193a3c2-6d0e-7a1b-9c4d-5e6f7a8b9c0e"`
	ActsForName string            `json:"acts_for_name,omitempty" doc:"Their name." example:"Tan Ah Kow"`
	Since       string            `json:"since,omitempty" format:"date-time" doc:"When turning it on was approved." example:"2026-11-15T02:00:00Z"`
	Skill       string            `json:"skill" doc:"Its skill and version." example:"company-secretary@1"`
	Schedule    AgentSchedule     `json:"schedule"`
	NextRuns    []AgentNextRun    `json:"next_runs" nullable:"false" doc:"When each schedule entry runs next, while it is on and not paused."`
	Latest      *AgentRunSummary  `json:"latest,omitempty" doc:"Its latest run, whatever became of it."`
	LastDone    *AgentRunSummary  `json:"last_done,omitempty" doc:"Its latest run that finished."`
	Findings    []AgentRunFinding `json:"findings" nullable:"false" doc:"The open findings of its latest finished run: Not assessed first, then ✖, ⏰ and ⚠ (AI-65)."`
	AISummary   *AIText           `json:"ai_summary,omitempty" doc:"What the model wrote in its latest finished run, as written: the AI's words, shown with the AI label, apart from Fyniq's own summary and findings."`
}

// AgentPackVersion is a statutory pack a run read, at its version.
type AgentPackVersion struct {
	Pack    string `json:"pack" example:"sg-gst"`
	Version string `json:"version" example:"sg-gst@2026.1"`
}

// AgentRunModel is what a run's model step gave: its words as written, labelled AI, and what it used.
type AgentRunModel struct {
	Model     string `json:"model" example:"claude-opus-5-5"`
	Text      AIText `json:"text"`
	Tokens    int    `json:"tokens" example:"12000"`
	ToolCalls int    `json:"tool_calls" example:"4"`
}

// AgentRunReport is one run as its report shows it (design D9): what it read, every finding with its rule and source, the
// tasks they raised, the proposals it made that you may see, and the model's words apart.
type AgentRunReport struct {
	AgentRunSummary
	SkillVersion   int                 `json:"skill_version" example:"1"`
	LedgerRevision int64               `json:"ledger_revision" doc:"The books' revision it read (ENG-46)." example:"42"`
	Packs          []AgentPackVersion  `json:"packs" nullable:"false" doc:"The statutory packs it read."`
	KBSnapshot     string              `json:"kb_snapshot,omitempty" doc:"The knowledge base it read." example:"none"`
	Findings       []AgentRunFinding   `json:"findings" nullable:"false" doc:"Every finding, Not assessed first."`
	Tasks          []ComplianceTask    `json:"tasks" nullable:"false" doc:"The tasks its findings have."`
	Proposals      []AIProposalSummary `json:"proposals" nullable:"false" doc:"The proposals it made that you may see."`
	Model          *AgentRunModel      `json:"model,omitempty" doc:"The model step, when it asked the model."`
}

// AgentRunPage is a page of runs, newest first.
type AgentRunPage struct {
	Items      []AgentRunSummary `json:"items" nullable:"false"`
	NextCursor string            `json:"next_cursor,omitempty" doc:"The cursor of the next page; absent on the last." example:"MDE5M2EzYzItNmQwZS03YTFiLTljNGQtNWU2ZjdhOGI5YzEy"`
}

type (
	companySecretaryOutput struct{ Body CompanySecretary }
	agentRunListInput      struct {
		PageQuery
		Status string `query:"status" enum:"queued,running,done,failed,skipped" doc:"Only runs in this state; leave it out for all." example:"done"`
	}
	agentRunListOutput struct{ Body AgentRunPage }
	agentRunGetInput   struct{ AgentRunPath }
	agentRunGetOutput  struct{ Body AgentRunReport }
	agentRunNowInput   struct {
		KeyHeader
		PreferHeader
	}
	agentRunNowOutput struct {
		Status   int
		Location string `header:"Location" doc:"The run's report."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was queued (FR-INT-14)."`
		Body     AgentRunSummary
	}
)

func registerCompanySecretary(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := []string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}
	register(a, authz.GetCompanySecretary, huma.Operation{
		OperationID: "getCompanySecretary", Method: http.MethodGet, Path: "/v1/compliance/secretary", Tags: []string{"Compliance"},
		Summary: "Get the AI company secretary assistant",
		Description: "The company secretary assistant's workspace (FR-CS-04, AI-62, AI-63): whether it is on or paused, the Owner it acts for, " +
			"its schedule and caps, when it runs next, its latest run and the open findings of its latest finished run, Not assessed " +
			"first. It never files, signs, approves or marks an obligation met. Read-only; no side effects.",
	}, locks, func(ctx context.Context, _ *struct{}) (*companySecretaryOutput, error) {
		p, _ := authn.From(ctx)
		var out CompanySecretary
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = a.companySecretary(ctx, u)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &companySecretaryOutput{Body: out}, nil
	})

	register(a, authz.ListAgentRuns, huma.Operation{
		OperationID: "listAgentRuns", Method: http.MethodGet, Path: "/v1/compliance/secretary/runs", Tags: []string{"Compliance"},
		Summary: "List the assistant's runs",
		Description: "The company secretary assistant's runs, newest first, a page at a time, filtered by state (AI-65, AI-67): each with what " +
			"started it, what became of it, Fyniq's summary, its findings by status, and whether what it found is new since the run done " +
			"before it. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *agentRunListInput) (*agentRunListOutput, error) {
		p, _ := authn.From(ctx)
		after := uuid.Nil
		if in.Cursor != "" {
			c, err := decodeCursor(in.Cursor)
			if err != nil {
				return nil, err
			}
			if after, err = uuid.Parse(c); err != nil {
				return nil, badCursor()
			}
		}
		var statuses []string
		if in.Status != "" {
			statuses = []string{in.Status}
		}
		out := AgentRunPage{Items: []AgentRunSummary{}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			list, err := u.AgentRuns().List(ctx, storage.AgentRunQuery{Agent: secretary, Statuses: statuses, After: after, Limit: in.PageSize + 1})
			if err != nil {
				return err
			}
			more := len(list) > in.PageSize
			if more {
				list = list[:in.PageSize]
				out.NextCursor = encodeCursor(list[len(list)-1].ID.String())
			}
			for i, r := range list {
				prev, err := doneBefore(ctx, u, list[i+1:], r)
				if err != nil {
					return err
				}
				out.Items = append(out.Items, runSummary(r, prev))
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return &agentRunListOutput{Body: out}, nil
	})

	register(a, authz.GetAgentRun, huma.Operation{
		OperationID: "getAgentRun", Method: http.MethodGet, Path: "/v1/compliance/secretary/runs/{run_id}", Tags: []string{"Compliance"},
		Summary: "Get a run's report",
		Description: "One run of the company secretary assistant as its report shows it (AI-64, AI-65, FR-CS-03): the ledger revision, the " +
			"packs and the knowledge base it read; every finding with its status, rule, source, evidence and next step, Not assessed first; " +
			"the tasks they have; the proposals it made that you may see; and the model's words, as written and labelled AI. Read-only; no " +
			"side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *agentRunGetInput) (*agentRunGetOutput, error) {
		p, _ := authn.From(ctx)
		id, err := uuid.Parse(in.RunID)
		if err != nil {
			return nil, runNotFound()
		}
		var out AgentRunReport
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			r, err := u.AgentRuns().Get(ctx, id)
			if err != nil {
				return err
			}
			if r.Agent != secretary {
				return ports.ErrNotFound
			}
			out, err = a.runReport(ctx, u, p, r)
			return err
		})
		if errors.Is(err, ports.ErrNotFound) {
			return nil, runNotFound()
		}
		if err != nil {
			return nil, err
		}
		return &agentRunGetOutput{Body: out}, nil
	})

	register(a, authz.RunCompanySecretary, huma.Operation{
		OperationID: "runCompanySecretary", Method: http.MethodPost, Path: "/v1/compliance/secretary/runs", Tags: []string{"Compliance"},
		Summary: "Run the assistant now", DefaultStatus: http.StatusAccepted,
		Description: "Run now (AI-62): queues a run of the company secretary assistant at once, requested by you and acting for the Owner who " +
			"turned it on (AI-63). The worker runs it as any other: its deterministic checks first, the model only if an input changed. " +
			"Refused while the assistant is off or paused. Side effects: the queued run and its audit event. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview checks it and queues nothing.",
	}, append(append([]string{CodeSessionRequired, CodeAgentOff}, writes...), locks...), func(ctx context.Context, in *agentRunNowInput) (*agentRunNowOutput, error) {
		p, _ := authn.From(ctx)
		if p.AI() || p.UserID == uuid.Nil {
			return nil, problem(CodeSessionRequired, "Run now needs a person with a Fyniq user, who asks for it by name.", "Sign in to Fyniq in the browser and do it there")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (AgentRunSummary, error) {
			skill, ok := a.skills().Get(secretary)
			if !ok {
				return AgentRunSummary{}, problem(CodeAgentOff, "The company secretary assistant is not in this release.", "Nothing to run")
			}
			st, err := agents.Enabled(ctx, u, skill, a.deps.Clock.Now())
			if err != nil {
				return AgentRunSummary{}, err
			}
			if !st.Running() {
				why := "off"
				if st.On {
					why = "paused"
				}
				return AgentRunSummary{}, problem(CodeAgentOff, "The company secretary assistant is "+why+".",
					"Ask an Owner to turn it on or end its pause in Settings → Organisation")
			}
			if a.deps.Outbox == nil {
				return AgentRunSummary{}, problem(CodeAgentOff, "This deployment runs no background work, so no run can be queued.",
					"Ask the platform's operator to set up the worker")
			}
			s := agents.Scheduler{Store: a.deps.Store, Dispatcher: *a.deps.Outbox, Clock: a.deps.Clock, Skills: a.skills()}
			r, err := s.RunNow(ctx, u, p.Organisation, skill, st.ActsFor, p.UserID)
			if err != nil {
				return AgentRunSummary{}, err
			}
			return runSummary(r, nil), nil
		})
		if err != nil {
			return nil, err
		}
		if w.Previewed {
			return &agentRunNowOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
		}
		return &agentRunNowOutput{Status: w.Status, Location: "/v1/compliance/secretary/runs/" + w.Body.ID, Replayed: replayedHeader(w.Replayed),
			Body: w.Body}, nil
	})
}

func runNotFound() *Problem {
	return problem(CodeNotFound, "No run of the company secretary assistant has that ID.", "List its runs to find their IDs")
}

// companySecretary is the workspace as u reads it now.
func (a *API) companySecretary(ctx context.Context, u storage.UnitOfWork) (CompanySecretary, error) {
	skill, ok := a.skills().Get(secretary)
	if !ok {
		return CompanySecretary{}, problem(CodeNotFound, "The company secretary assistant is not in this release.", "Nothing to show")
	}
	now := a.deps.Clock.Now()
	st, err := agents.Enabled(ctx, u, skill, now)
	if err != nil {
		return CompanySecretary{}, err
	}
	out := CompanySecretary{State: "off", Skill: skill.Name + "@" + strconv.Itoa(skill.Version), NextRuns: []AgentNextRun{}, Findings: []AgentRunFinding{},
		Schedule: scheduleOf(st)}
	switch {
	case st.On && st.Paused:
		out.State = "paused"
	case st.On:
		out.State = "on"
	}
	if st.ActsFor != uuid.Nil {
		names, err := memberNames(ctx, u, now)
		if err != nil {
			return CompanySecretary{}, err
		}
		out.ActsFor, out.ActsForName = st.ActsFor.String(), names[st.ActsFor]
		if !st.Since.IsZero() {
			out.Since = st.Since.UTC().Format(time.RFC3339)
		}
	}
	if st.Running() {
		for _, entry := range agents.Entries {
			if spec, err := st.Entry(entry); err == nil {
				out.NextRuns = append(out.NextRuns, AgentNextRun{Entry: entry, At: spec.Next(now).UTC().Format(time.RFC3339)})
			}
		}
	}
	latest, err := u.AgentRuns().List(ctx, storage.AgentRunQuery{Agent: secretary, Limit: 1})
	if err != nil {
		return CompanySecretary{}, err
	}
	if len(latest) == 1 {
		s := runSummary(latest[0], nil)
		out.Latest = &s
	}
	done, err := u.AgentRuns().LastDone(ctx, secretary)
	if errors.Is(err, ports.ErrNotFound) {
		return out, nil
	}
	if err != nil {
		return CompanySecretary{}, err
	}
	prev, err := doneBefore(ctx, u, nil, done)
	if err != nil {
		return CompanySecretary{}, err
	}
	s := runSummary(done, prev)
	out.LastDone = &s
	if rep := done.Report; rep != nil && rep.ModelUsed && rep.ModelText != "" {
		out.AISummary = &AIText{Text: rep.ModelText, Origin: "ai"}
	}
	for _, f := range runFindings(done) {
		if f.Status != agents.FindingOK {
			out.Findings = append(out.Findings, f)
		}
	}
	return out, nil
}

// runReport is r's report for p.
func (a *API) runReport(ctx context.Context, u storage.UnitOfWork, p authn.Principal, r storage.AgentRun) (AgentRunReport, error) {
	prev, err := doneBefore(ctx, u, nil, r)
	if err != nil {
		return AgentRunReport{}, err
	}
	out := AgentRunReport{AgentRunSummary: runSummary(r, prev), SkillVersion: r.SkillVersion, Packs: []AgentPackVersion{}, Findings: runFindings(r),
		Tasks: []ComplianceTask{}, Proposals: []AIProposalSummary{}}
	if rep := r.Report; rep != nil {
		out.LedgerRevision, out.KBSnapshot = rep.LedgerRevision, rep.KBSnapshot
		for _, k := range slices.Sorted(maps.Keys(rep.PackVersions)) {
			out.Packs = append(out.Packs, AgentPackVersion{Pack: k, Version: rep.PackVersions[k]})
		}
		if rep.ModelUsed {
			out.Model = &AgentRunModel{Model: rep.Model, Text: AIText{Text: rep.ModelText, Origin: "ai"}, Tokens: rep.Tokens, ToolCalls: rep.ToolCalls}
		}
	}
	for _, f := range out.Findings {
		id, err := uuid.Parse(f.Task)
		if err != nil {
			continue
		}
		t, err := u.ComplianceTasks().Get(ctx, id)
		if errors.Is(err, ports.ErrNotFound) {
			continue
		}
		if err != nil {
			return AgentRunReport{}, err
		}
		out.Tasks = append(out.Tasks, complianceTaskOf(t))
	}
	if !p.AI() && p.UserID != uuid.Nil {
		ps, err := u.AIProposals().List(ctx, storage.AIProposalQuery{Run: &r.ID, Limit: 100})
		if err != nil {
			return AgentRunReport{}, err
		}
		for _, pr := range ps {
			ok, err := a.seesAIProposal(ctx, u, p, pr)
			if err != nil {
				return AgentRunReport{}, err
			}
			if ok {
				out.Proposals = append(out.Proposals, a.proposalSummary(pr))
			}
		}
	}
	return out, nil
}

// doneBefore is the agent's run done before r, from the runs older than it in older when one is done, else the store's.
func doneBefore(ctx context.Context, u storage.UnitOfWork, older []storage.AgentRun, r storage.AgentRun) (*storage.AgentRun, error) {
	if r.Status != storage.AgentRunDone {
		return nil, nil
	}
	for _, o := range older {
		if o.Status == storage.AgentRunDone {
			return &o, nil
		}
	}
	list, err := u.AgentRuns().List(ctx, storage.AgentRunQuery{Agent: r.Agent, Statuses: []string{storage.AgentRunDone}, After: r.ID, Limit: 1})
	if err != nil || len(list) == 0 {
		return nil, err
	}
	return &list[0], nil
}

// runSummary is r as the history lists it; new against prev, the run done before it.
func runSummary(r storage.AgentRun, prev *storage.AgentRun) AgentRunSummary {
	out := AgentRunSummary{ID: r.ID.String(), Trigger: r.Trigger, SlotAt: r.SlotAt.UTC().Format(time.RFC3339), Status: r.Status, Reason: r.Reason}
	if r.StartedAt != nil {
		out.StartedAt = r.StartedAt.UTC().Format(time.RFC3339)
	}
	if r.FinishedAt != nil {
		out.FinishedAt = r.FinishedAt.UTC().Format(time.RFC3339)
	}
	if rep := r.Report; rep != nil {
		out.Summary, out.ModelUsed, out.ModelMissed = rep.Summary, rep.ModelUsed, rep.ModelMissed
		out.New = prev == nil || prev.Report == nil || prev.Report.Fingerprint != rep.Fingerprint
		for _, f := range runFindings(r) {
			switch f.Status {
			case agents.FindingOK:
				out.Counts.OK++
			case agents.FindingReview:
				out.Counts.Review++
			case agents.FindingBlocked:
				out.Counts.Blocked++
			case agents.FindingDue:
				out.Counts.Due++
			case agents.FindingNotAssessed:
				out.Counts.NotAssessed++
			}
		}
	}
	return out
}

// findingOrder lists Not assessed first, then ✖, ⏰, ⚠ and ✔.
var findingOrder = []string{agents.FindingNotAssessed, agents.FindingBlocked, agents.FindingDue, agents.FindingReview, agents.FindingOK}

// runFindings are r's findings as its report keeps them, Not assessed first.
func runFindings(r storage.AgentRun) []AgentRunFinding {
	out := []AgentRunFinding{}
	if r.Report == nil || len(r.Report.Findings) == 0 {
		return out
	}
	var fs []agents.Finding
	if json.Unmarshal(r.Report.Findings, &fs) != nil {
		return out
	}
	slices.SortStableFunc(fs, func(a, b agents.Finding) int {
		return slices.Index(findingOrder, a.Status) - slices.Index(findingOrder, b.Status)
	})
	for _, f := range fs {
		out = append(out, AgentRunFinding{Key: f.Key, Status: f.Status, Title: f.Title, Detail: f.Detail, Rule: f.Rule, Source: f.Source, Pack: f.Pack,
			Evidence: f.Evidence, Next: f.Next, DueOn: f.DueOn, Window: f.Window, Task: f.Task})
	}
	return out
}

// scheduleOf is the agent's schedule as st holds it.
func scheduleOf(st agents.State) AgentSchedule {
	list := func(name string) []string {
		out := []string{}
		for _, v := range strings.Split(st.Value(name), ",") {
			if v = strings.TrimSpace(v); v != "" {
				out = append(out, v)
			}
		}
		return out
	}
	number := func(name string) int {
		n, _ := strconv.Atoi(st.Value(name))
		return n
	}
	return AgentSchedule{Monitor: st.Value("monitor"), CrossCheck: st.Value("cross_check"), AfterClose: st.Value("after_close") == "true",
		FYEndPack: st.Value("fy_end_pack") == "true", Events: list("events"), QuietHours: st.Value("quiet_hours"), Recipients: list("recipients"),
		Channels: list("channels"), EscalateDays: number("escalate_days"), ToolCalls: number("tool_calls"), RunTokens: number("run_tokens")}
}
