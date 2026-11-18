package api

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	appclock "github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/aipolicy"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the AI activity log (AI-12, AUD-05)
//
// Every attempt of a model call, as the router recorded it: the agent, the route's step and what it fell back from,
// the connection, the model requested and the model and version that answered, who it ran for, the classes it carried,
// the outcome, the tokens and the cost. Each member reads their own; the Owner and the DPO read everyone's (Appendix D,
// rev 0.276). Someone else's entry that carried payroll shows masked to a reader who may not see payroll unmasked, and
// showing one unmasked is audited. Turns, tool calls, proposals and knowledge pages join it as the changes that make
// them arrive.

// AIActivityTokens are an attempt's tokens by kind (AI-77).
type AIActivityTokens struct {
	Input      int64 `json:"input" doc:"Fresh input, without what was read from or written to the cache." example:"1200"`
	Output     int64 `json:"output" doc:"Output, reasoning included." example:"340"`
	CacheRead  int64 `json:"cache_read" example:"0"`
	CacheWrite int64 `json:"cache_write" example:"0"`
	Reasoning  int64 `json:"reasoning" example:"120"`
}

// AIActivityEntry is one attempt of a model call (AI-12, AI-76, AI-87).
type AIActivityEntry struct {
	ID              string           `json:"id" format:"uuid" example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a41"`
	Run             string           `json:"run" format:"uuid" doc:"The run its attempts share: a call and its fallbacks, or an eval run." example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a42"`
	Agent           string           `json:"agent" example:"chat"`
	RouteVersion    int              `json:"route_version" doc:"The route's version it followed; 0 for a probe or an eval run, outside any route." example:"2"`
	Step            int              `json:"step" example:"1"`
	FellBackFrom    int              `json:"fell_back_from,omitempty" doc:"The step before it, which failed or was skipped (AI-76)." example:"1"`
	ConnectionID    string           `json:"connection_id" format:"uuid" example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b"`
	Connection      string           `json:"connection" doc:"The connection's name." example:"Claude in Singapore"`
	ModelRequested  string           `json:"model_requested" example:"claude-opus-4-6"`
	ModelReported   string           `json:"model_reported,omitempty" doc:"The model the provider says answered (AI-87)." example:"claude-opus-4-6"`
	VersionReported string           `json:"version_reported,omitempty" doc:"Its version, where the provider reports one (AI-87)." example:"20260801"`
	Person          string           `json:"person" doc:"Who it ran for: user:<id>, or the system for a job." example:"user:0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2d"`
	Classes         []RouteClass     `json:"classes" nullable:"false" doc:"The classes of data it carried (AI-83)."`
	Outcome         string           `json:"outcome" enum:"ok,skipped,busy,timeout,not_found,refused,failed,stopped" doc:"ok: served; skipped: not sent, as the detail says; stopped: refused before sending, or cancelled." example:"ok"`
	Detail          string           `json:"detail,omitempty" doc:"Why it was skipped, stopped or failed, or why it has no cost; absent when masked." example:"the connection's AI budget is spent this month"`
	Tokens          AIActivityTokens `json:"tokens"`
	Cost            string           `json:"cost,omitempty" doc:"Its exact cost in the price's currency; absent when unpriced." example:"0.006"`
	Currency        string           `json:"currency,omitempty" example:"USD"`
	CostBase        string           `json:"cost_base,omitempty" doc:"Its cost in the base currency at the day's rate, exact." example:"0.0081"`
	LatencyMS       int              `json:"latency_ms" example:"1840"`
	StartedAt       time.Time        `json:"started_at" example:"2026-11-14T09:00:00Z"`
	Masked          bool             `json:"masked" doc:"It carried payroll and you may not see payroll unmasked: its detail is withheld (FR-PAY-13)." example:"false"`
}

// AIActivityPage is a page of the AI activity log, newest first.
type AIActivityPage struct {
	Scope      string            `json:"scope" enum:"own,all" doc:"own: your own entries only; all: every member's, for the Owner and the DPO (Appendix D)." example:"own"`
	Items      []AIActivityEntry `json:"items" nullable:"false"`
	NextCursor string            `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjYtMTEtMTRUMDk6MDA6MDBafDAxOTMifQ"`
}

type (
	aiActivityInput struct {
		PageQuery
		Agent  string `query:"agent" maxLength:"60" doc:"Only this agent's entries." example:"chat"`
		Person string `query:"person" maxLength:"60" doc:"Only this member's entries, by user ID: for the Owner and the DPO; anyone else reads only their own." example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2d"`
		Run    string `query:"run" maxLength:"40" doc:"Only one run's attempts." example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a42"`
		From   string `query:"from" format:"date" doc:"Only entries on or after this day, in Singapore time." example:"2026-11-01"`
		To     string `query:"to" format:"date" doc:"Only entries on or before this day." example:"2026-11-30"`
	}
	aiActivityOutput struct{ Body AIActivityPage }
)

// readsAllAIActivity is whether the caller reads every member's entries: the row's V without "own" (Appendix D).
func readsAllAIActivity(p authn.Principal) bool {
	return authz.Decide(authz.Policy{Operation: "listAIActivity", Capability: authz.AIActivity, Need: authz.View, Unqualified: true}, p.Roles,
		p.AI()).Allowed
}

// seesPayroll is whether the caller may see payroll unmasked (FR-PAY-13): a right on the payroll row, neither masked
// (M) nor narrowed to their own (V (own)).
func seesPayroll(p authn.Principal) bool {
	d := authz.Decide(authz.Policy{Operation: "listAIActivity", Capability: authz.PayRuns, Need: authz.View, Unqualified: true}, p.Roles, p.AI())
	return d.Allowed && d.Right != authz.Masked
}

func registerAIActivity(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.ListAIActivity, huma.Operation{
		OperationID: "listAIActivity", Method: http.MethodGet, Path: "/v1/ai/activity", Tags: []string{"AI"},
		Summary: "List the AI activity log",
		Description: "The AI activity log (AI-12), newest first, a page at a time: every attempt of a model call, with the agent, the route's " +
			"step and what it fell back from, the connection, the model requested and the model and version that answered, who it ran for, " +
			"the classes it carried, the outcome, the tokens and the cost; filtered by agent, person, run and days. Each member reads their " +
			"own entries; the Owner and the DPO read everyone's (Appendix D, rev 0.276). Someone else's entry that carried payroll shows " +
			"masked to a reader who may not see payroll unmasked (FR-PAY-13). Side effects: showing such an entry unmasked writes the audit " +
			"event ai_activity.read (AUD-05); otherwise none. On the never-list for the AI and MCP (§9.6).",
	}, append([]string{CodeInvalid, CodeCursor, CodeForbidden}, locks...), func(ctx context.Context, in *aiActivityInput) (*aiActivityOutput, error) {
		p, _ := authn.From(ctx)
		me := "user:" + p.UserID.String()
		all := readsAllAIActivity(p)
		q := storage.AICallQuery{Agent: in.Agent, From: time.Unix(0, 0).UTC(), To: time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC), Limit: in.PageSize + 1}
		var bad []FieldProblem
		switch {
		case !all:
			if in.Person != "" && in.Person != p.UserID.String() {
				return nil, problem(CodeForbidden, "You read only your own entries of the AI activity log.",
					"Leave person out, or ask the Owner or the DPO, who read everyone's")
			}
			q.Actor = me
		case in.Person != "":
			if _, err := uuid.Parse(in.Person); err != nil {
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "query.person", Message: "is not a user ID", Fix: "Send the member's user ID"})
			}
			q.Actor = "user:" + in.Person
		}
		if in.Run != "" {
			run, err := uuid.Parse(in.Run)
			if err != nil {
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "query.run", Message: "is not a run's ID", Fix: "Send a run an entry shows"})
			}
			q.Run = &run
		}
		for _, d := range []struct {
			raw, field string
			to         *time.Time
			next       bool
		}{{in.From, "query.from", &q.From, false}, {in.To, "query.to", &q.To, true}} {
			if d.raw == "" {
				continue
			}
			day, err := time.ParseInLocation(time.DateOnly, d.raw, appclock.Singapore)
			if err != nil {
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: d.field, Message: "is not a date", Fix: "Send the day as YYYY-MM-DD"})
				continue
			}
			if d.next {
				day = day.AddDate(0, 0, 1)
			}
			*d.to = day.UTC()
		}
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		if after != "" {
			at, id, ok := strings.Cut(after, "|")
			t, terr := time.Parse(time.RFC3339Nano, at)
			uid, ierr := uuid.Parse(id)
			if !ok || terr != nil || ierr != nil {
				return nil, badCursor()
			}
			q.BeforeAt, q.BeforeID = &t, uid
		}
		if len(bad) > 0 {
			return nil, invalid(bad)
		}
		out := AIActivityPage{Scope: "own", Items: []AIActivityEntry{}}
		if all {
			out.Scope = "all"
		}
		payroll := seesPayroll(p)
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			calls, err := u.AICalls().Page(ctx, q)
			if err != nil {
				return err
			}
			if len(calls) > in.PageSize {
				calls = calls[:in.PageSize]
				last := calls[len(calls)-1]
				out.NextCursor = encodeCursor(last.StartedAt.UTC().Format(time.RFC3339Nano) + "|" + last.ID.String())
			}
			names := map[uuid.UUID]string{}
			connections, err := u.AIConnections().List(ctx)
			if err != nil {
				return err
			}
			for _, c := range connections {
				names[c.ID] = c.Name
			}
			var read []string
			for _, c := range calls {
				e := aiActivityOut(c, names[c.ConnectionID])
				if c.Actor != me && slices.Contains(c.Classes, aipolicy.Payroll) {
					if payroll {
						read = append(read, c.ID.String())
					} else {
						e.Masked, e.Detail = true, ""
					}
				}
				out.Items = append(out.Items, e)
			}
			if len(read) == 0 {
				return nil
			}
			// Showing someone else's payroll entries unmasked is a read of payroll (AUD-05).
			first, _ := uuid.Parse(read[0])
			return u.Audit().Record(ctx, storage.AuditRecord{Action: "ai_activity.read", ObjectType: "ai_call", ObjectID: first,
				Changes: map[string][2]any{"entries": {nil, read}}})
		})
		return &aiActivityOutput{Body: out}, err
	})
}

func aiActivityOut(c storage.AICall, connection string) AIActivityEntry {
	e := AIActivityEntry{ID: c.ID.String(), Run: c.Run.String(), Agent: c.Agent, RouteVersion: c.RouteVersion, Step: c.Step, FellBackFrom: c.FellBackFrom,
		ConnectionID: c.ConnectionID.String(), Connection: connection, ModelRequested: c.ModelRequested, ModelReported: c.ModelReported,
		VersionReported: c.VersionReported, Person: c.Actor, Classes: []RouteClass{}, Outcome: c.Outcome, Detail: c.Detail,
		Tokens: AIActivityTokens{Input: c.Input, Output: c.Output, CacheRead: c.CacheRead, CacheWrite: c.CacheWrite, Reasoning: c.Reasoning},
		Cost:   c.Cost, Currency: c.Currency, CostBase: c.CostBase, LatencyMS: c.LatencyMS, StartedAt: c.StartedAt.UTC()}
	for _, k := range c.Classes {
		e.Classes = append(e.Classes, RouteClass(k))
	}
	return e
}

// AITurnToolCall is a read a turn made, as it ran (design D11): never its input or what it read, only a hash of that.
type AITurnToolCall struct {
	Tool         string `json:"tool" example:"metrics.query"`
	Version      int    `json:"version" example:"1"`
	Outcome      string `json:"outcome" enum:"read,refused" example:"read"`
	Code         string `json:"code,omitempty" doc:"Why it was refused." example:"AI-TOOL-03"`
	Facts        int    `json:"facts" doc:"How many facts it read." example:"3"`
	Omitted      int    `json:"omitted" doc:"How many lists its limits cut." example:"0"`
	Withheld     bool   `json:"withheld" doc:"Screening withheld its free text (AI-18)." example:"false"`
	DurationMS   int    `json:"duration_ms" example:"120"`
	OutputSHA256 string `json:"output_sha256,omitempty" doc:"The hash of what the model read of it." example:"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"`
}

// AITurnActivity is one turn of the assistant in the AI activity log (AI-12): who asked, what, how it ended, the
// prompt it was told, the model calls' runs, the classes it read and the reads it made.
type AITurnActivity struct {
	ID           string           `json:"id" format:"uuid" example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2c"`
	Conversation string           `json:"conversation_id" format:"uuid" example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b"`
	Person       string           `json:"person" doc:"Who asked: user:<id>." example:"user:0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2d"`
	Question     string           `json:"question,omitempty" doc:"What was asked; absent when masked." example:"What is our cash runway?"`
	Status       string           `json:"status" enum:"answering,answered,refused,failed" example:"answered"`
	Code         string           `json:"code,omitempty" example:"AI-LIMIT-01"`
	Prompt       string           `json:"prompt,omitempty" doc:"The system prompt it was told, and its hash (AI-24)." example:"chat@2"`
	PromptSHA256 string           `json:"prompt_sha256,omitempty" example:"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"`
	Runs         []AIRunID        `json:"runs" nullable:"false" doc:"Its model calls' runs, whose attempts listAIActivity shows."`
	InputTokens  int64            `json:"input_tokens" example:"2400"`
	OutputTokens int64            `json:"output_tokens" example:"310"`
	Classes      []RouteClass     `json:"classes" nullable:"false" doc:"The classes of data it read (AI-83)."`
	Cited        int              `json:"cited" doc:"How many facts its answer cites." example:"2"`
	Regenerated  bool             `json:"regenerated" doc:"Its answer failed the figures' check once (AI-08)." example:"false"`
	Withheld     int              `json:"withheld" doc:"Figures or words withheld after the check." example:"0"`
	ToolErrors   int              `json:"tool_errors" doc:"Its reads refused or failed (QA-AI-04)." example:"0"`
	Screening    int              `json:"screening_flags" doc:"Screening stopped it, or withheld the text of so many of its reads (AI-P7, QA-AI-04)." example:"0"`
	Cost         string           `json:"cost" doc:"Its model calls' cost in the base currency, exact; 0 when none was priced (QA-AI-04)." example:"0.0213"`
	ToolCalls    []AITurnToolCall `json:"tool_calls" nullable:"false"`
	AskedAt      time.Time        `json:"asked_at" example:"2026-11-15T08:00:00Z"`
	AnsweredAt   *time.Time       `json:"answered_at,omitempty" example:"2026-11-15T08:00:04Z"`
	Masked       bool             `json:"masked" doc:"It read payroll and you may not see payroll unmasked: its question is withheld (FR-PAY-13)." example:"false"`
}

// AIRunID names a model call's run in the AI activity log.
type AIRunID string

// Schema implements huma.SchemaProvider.
func (AIRunID) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Format: "uuid", Description: "A model call's run.", Examples: []any{"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a42"}}
}

// AITurnActivityPage is a page of the assistant's turns in the AI activity log, newest first.
type AITurnActivityPage struct {
	Scope      string           `json:"scope" enum:"own,all" doc:"own: your own turns only; all: every member's, for the Owner and the DPO (Appendix D)." example:"own"`
	Items      []AITurnActivity `json:"items" nullable:"false"`
	NextCursor string           `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjYtMTEtMTRUMDk6MDA6MDBafDAxOTMifQ"`
}

type (
	aiTurnActivityInput struct {
		PageQuery
		Person string `query:"person" maxLength:"60" doc:"Only this member's turns, by user ID: for the Owner and the DPO; anyone else reads only their own." example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2d"`
		From   string `query:"from" format:"date" doc:"Only turns asked on or after this day, in Singapore time." example:"2026-11-01"`
		To     string `query:"to" format:"date" doc:"Only turns asked on or before this day." example:"2026-11-30"`
	}
	aiTurnActivityOutput struct{ Body AITurnActivityPage }
)

func registerAITurnActivity(a *API) {
	register(a, authz.ListAITurnActivity, huma.Operation{
		OperationID: "listAITurnActivity", Method: http.MethodGet, Path: "/v1/ai/activity/turns", Tags: []string{"AI"},
		Summary: "List the assistant's turns in the AI activity log",
		Description: "The assistant's turns (AI-12), newest first, a page at a time: who asked, what, how it ended and why, the prompt it was " +
			"told and its hash, its model calls' runs, its tokens, the classes of data it read, what grounding did, and each read it made " +
			"with its outcome and counts, never its input or what it read; filtered by person and days. Each member reads their own; the " +
			"Owner and the DPO read everyone's (Appendix D, rev 0.276). Someone else's turn that read payroll shows masked to a reader who " +
			"may not see payroll unmasked (FR-PAY-13). Side effects: showing such a turn unmasked writes the audit event ai_activity.read " +
			"(AUD-05); otherwise none. On the never-list for the AI and MCP (§9.6).",
	}, []string{CodeInvalid, CodeCursor, CodeForbidden, storage.CodeRetry, storage.CodeLockTimeout}, func(ctx context.Context, in *aiTurnActivityInput) (*aiTurnActivityOutput, error) {
		p, _ := authn.From(ctx)
		all := readsAllAIActivity(p)
		q := storage.AITurnQuery{From: time.Unix(0, 0).UTC(), To: time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC), Limit: in.PageSize + 1}
		var bad []FieldProblem
		switch {
		case !all:
			if in.Person != "" && in.Person != p.UserID.String() {
				return nil, problem(CodeForbidden, "You read only your own turns in the AI activity log.", "Leave person out, or ask the Owner or the DPO, who read everyone's")
			}
			q.User = p.UserID
		case in.Person != "":
			id, err := uuid.Parse(in.Person)
			if err != nil {
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "query.person", Message: "is not a user ID", Fix: "Send the member's user ID"})
			}
			q.User = id
		}
		for _, d := range []struct {
			raw, field string
			to         *time.Time
			next       bool
		}{{in.From, "query.from", &q.From, false}, {in.To, "query.to", &q.To, true}} {
			if d.raw == "" {
				continue
			}
			day, err := time.ParseInLocation(time.DateOnly, d.raw, appclock.Singapore)
			if err != nil {
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: d.field, Message: "is not a date", Fix: "Send the day as YYYY-MM-DD"})
				continue
			}
			if d.next {
				day = day.AddDate(0, 0, 1)
			}
			*d.to = day.UTC()
		}
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		if after != "" {
			at, id, ok := strings.Cut(after, "|")
			t, terr := time.Parse(time.RFC3339Nano, at)
			uid, ierr := uuid.Parse(id)
			if !ok || terr != nil || ierr != nil {
				return nil, badCursor()
			}
			q.BeforeAt, q.BeforeID = &t, uid
		}
		if len(bad) > 0 {
			return nil, invalid(bad)
		}
		out := AITurnActivityPage{Scope: "own", Items: []AITurnActivity{}}
		if all {
			out.Scope = "all"
		}
		payroll := seesPayroll(p)
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			turns, calls, err := u.AIConversations().Page(ctx, q)
			if err != nil {
				return err
			}
			if len(turns) > in.PageSize {
				turns = turns[:in.PageSize]
				last := turns[len(turns)-1]
				out.NextCursor = encodeCursor(last.AskedAt.UTC().Format(time.RFC3339Nano) + "|" + last.ID.String())
			}
			var runs []uuid.UUID
			for _, t := range turns {
				runs = append(runs, t.Runs...)
			}
			costs, err := u.AICalls().CostOfRuns(ctx, runs)
			if err != nil {
				return err
			}
			var read []string
			for _, t := range turns {
				e := turnActivityOf(t, calls[t.ID])
				e.Cost = costOf(t.Runs, costs)
				if t.UserID != p.UserID && slices.Contains(t.Classes, aipolicy.Payroll) {
					if payroll {
						read = append(read, t.ID.String())
					} else {
						e.Masked, e.Question = true, ""
					}
				}
				out.Items = append(out.Items, e)
			}
			if len(read) == 0 {
				return nil
			}
			// Showing someone else's turns that read payroll is a read of payroll (AUD-05).
			first, _ := uuid.Parse(read[0])
			return u.Audit().Record(ctx, storage.AuditRecord{Action: "ai_activity.read", ObjectType: "ai_turn", ObjectID: first,
				Changes: map[string][2]any{"turns": {nil, read}}})
		})
		return &aiTurnActivityOutput{Body: out}, err
	})
}

// costOf is the turn's runs' cost, summed exactly.
func costOf(runs []uuid.UUID, costs map[uuid.UUID]string) string {
	sum := apd.New(0, 0)
	for _, r := range runs {
		if c, _, err := apd.NewFromString(costs[r]); err == nil {
			_, _ = apd.BaseContext.WithPrecision(34).Add(sum, sum, c)
		}
	}
	return sum.Text('f')
}

func turnActivityOf(t storage.AITurn, calls []storage.AIToolCall) AITurnActivity {
	e := AITurnActivity{ID: t.ID.String(), Conversation: t.Conversation.String(), Person: "user:" + t.UserID.String(), Question: t.Question,
		Status: t.Status, Code: t.Code, Prompt: t.Prompt, PromptSHA256: t.PromptSHA256, Runs: []AIRunID{}, InputTokens: t.InputTokens,
		OutputTokens: t.OutputTokens, Classes: []RouteClass{}, Cited: len(t.Cited), Regenerated: t.Regenerated, Withheld: t.Withheld,
		ToolCalls: []AITurnToolCall{}, AskedAt: t.AskedAt.UTC(), AnsweredAt: t.AnsweredAt}
	for _, r := range t.Runs {
		e.Runs = append(e.Runs, AIRunID(r.String()))
	}
	for _, c := range t.Classes {
		e.Classes = append(e.Classes, RouteClass(c))
	}
	if strings.HasPrefix(t.Code, "AI-SCREEN-") {
		e.Screening++
	}
	for _, c := range calls {
		e.ToolCalls = append(e.ToolCalls, AITurnToolCall{Tool: c.Tool, Version: c.Version, Outcome: c.Outcome, Code: c.Code, Facts: c.Facts,
			Omitted: c.Omitted, Withheld: c.Withheld, DurationMS: c.DurationMS, OutputSHA256: c.OutputSHA256})
		if c.Outcome != "read" {
			e.ToolErrors++
		}
		if c.Withheld {
			e.Screening++
		}
	}
	e.Cost = "0"
	return e
}
