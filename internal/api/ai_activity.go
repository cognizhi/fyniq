package api

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

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
