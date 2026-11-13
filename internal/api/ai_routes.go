package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/aimodel"
	"github.com/cognizhi/fyniq/internal/domain/aipolicy"
	"github.com/cognizhi/fyniq/internal/domain/aiprice"
	"github.com/cognizhi/fyniq/internal/domain/airoute"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ each agent's route (AI-74, AI-26)
//
// Every agent runs on a route: the data classes it handles and its ordered steps, a primary and its fallbacks, each a
// connection and a model on that connection's allow-list. The Owner or an Administrator sets a route at once when it
// only narrows where data goes; a version that names a connection or model, or a class, the one in force did not, waits
// for the Owner or a second approver, as does an agent's first route.

// RouteClass is a class of data a route's agent handles (AI-75): never restricted data.
type RouteClass string

// Schema implements huma.SchemaProvider.
func (RouteClass) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Enum: []any{aipolicy.KnowHow, aipolicy.Business, aipolicy.Personal, aipolicy.Payroll},
		Description: "A class of data: restricted data never goes to an AI provider (AI-75).", Examples: []any{aipolicy.Business}}
}

// AIRouteStepInput is a step as it is set.
type AIRouteStepInput struct {
	ConnectionID string `json:"connection_id" format:"uuid" example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b"`
	Model        string `json:"model" minLength:"1" maxLength:"200" doc:"A model on the connection's allow-list." example:"claude-opus-4-6"`
	Effort       string `json:"effort,omitempty" enum:"low,medium,high" doc:"The thinking effort; absent for the model's default (AI-27)." example:"high"`
	Cache        bool   `json:"cache" doc:"Whether the stable prompt and tools are cached, where the connection proves caching (AI-27)." example:"true"`
}

// AIRouteStep is a step of a route, with whether it can serve now.
type AIRouteStep struct {
	ConnectionID string `json:"connection_id" format:"uuid" example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b"`
	Connection   string `json:"connection" doc:"The connection's name." example:"Claude on Agent Platform"`
	Model        string `json:"model" example:"claude-opus-4-6"`
	Effort       string `json:"effort,omitempty" enum:"low,medium,high" example:"high"`
	Cache        bool   `json:"cache" example:"true"`
	Serves       bool   `json:"serves" doc:"It can serve now: its connection on, its model on the allow-list and callable, and priced where the connection bills by usage." example:"true"`
	Why          string `json:"why,omitempty" doc:"Why it cannot serve now: the router skips it to the next step (AI-76)." example:"the model is no longer on the allow-list"`
}

// AIRoute is a version of an agent's route.
type AIRoute struct {
	Version  int           `json:"version" example:"2"`
	Classes  []RouteClass  `json:"classes" nullable:"false"`
	Preset   string        `json:"preset,omitempty" enum:"A,B,C" doc:"The ADR-T2 preset it was set from (AI-26)." example:"A"`
	Steps    []AIRouteStep `json:"steps" nullable:"false" doc:"The primary first, then the fallbacks in order."`
	Approval string        `json:"approval,omitempty" format:"uuid" doc:"The second approver's approval, for a version that widened where data goes." example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2e"`
	MadeBy   string        `json:"made_by" example:"user:0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2d"`
	MadeAt   time.Time     `json:"made_at" example:"2026-11-14T09:00:00Z"`
}

// AIAgentRoute is an agent and its route in force, if it has one.
type AIAgentRoute struct {
	Agent       string       `json:"agent" example:"chat"`
	Name        string       `json:"name" example:"Assistant"`
	Description string       `json:"description" example:"The assistant's chat and tool loop: questions, answers and the tools it reads with."`
	Handles     []RouteClass `json:"handles" nullable:"false" doc:"The classes it may ever handle: a route names some of them."`
	Line        string       `json:"line" enum:"main,classification" doc:"The model line its presets pin: classification for high-volume work (AI-26)." example:"main"`
	Route       *AIRoute     `json:"route,omitempty" doc:"Its route in force; absent until one is set, and then the agent does not run."`
}

// AIRouteList is every agent and its route.
type AIRouteList struct {
	Agents []AIAgentRoute `json:"agents" nullable:"false"`
}

// AIAgentRouteHistory is an agent, its route in force and its versions.
type AIAgentRouteHistory struct {
	AIAgentRoute
	Versions []AIRoute `json:"versions" nullable:"false" doc:"Its versions, newest first, at most 20."`
}

// AIRouteSet is a route as it is set.
type AIRouteSet struct {
	Classes []RouteClass       `json:"classes" minItems:"1" maxItems:"4" doc:"The classes the agent handles on this route."`
	Steps   []AIRouteStepInput `json:"steps" minItems:"1" maxItems:"5" doc:"The primary first, then at most four fallbacks."`
	Preset  string             `json:"preset,omitempty" enum:"A,B,C" doc:"The ADR-T2 preset it comes from, if any (AI-26)." example:"A"`
}

// AIRouteChange is a route set at once, or the approval request it waits for.
type AIRouteChange struct {
	Route    *AIRoute  `json:"route,omitempty" doc:"The route's new version, when it only narrows where data goes."`
	Approval *Approval `json:"approval,omitempty" doc:"The approval request, when it widens where data goes: the Owner or a second approver approves it."`
}

func (c AIRouteChange) waitsForApproval() bool { return c.Approval != nil }

type (
	aiRouteListOutput struct{ Body AIRouteList }
	// AIRouteAgentPath names an agent in a path: exported, as huma fills only an exported embedded path struct.
	AIRouteAgentPath struct {
		Agent string `path:"agent" minLength:"2" maxLength:"60" doc:"The agent, as GET /v1/ai/routes lists it." example:"chat"`
	}
	aiRouteGetOutput struct {
		ETag string `header:"ETag" doc:"The route's version, 0 before it has one: If-Match for setAIRoute."`
		Body AIAgentRouteHistory
	}
	aiRouteSetInput struct {
		AIRouteAgentPath
		KeyHeader
		PreferHeader
		IfMatchHeader
		Body AIRouteSet
	}
	aiRouteSetOutput struct {
		Status   int
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     AIRouteChange
	}
)

// routeScene is what a route is checked and shown against: the organisation's connections, their models and prices, and
// the data-class policy.
type routeScene struct {
	connections map[uuid.UUID]storage.AIConnection
	models      map[uuid.UUID]map[string]storage.AIModel
	priced      map[uuid.UUID]map[string]bool
	policy      aipolicy.Policy
	now         time.Time
}

func (a *API) routeScene(ctx context.Context, u storage.UnitOfWork) (routeScene, error) {
	s := routeScene{connections: map[uuid.UUID]storage.AIConnection{}, models: map[uuid.UUID]map[string]storage.AIModel{},
		priced: map[uuid.UUID]map[string]bool{}, now: a.deps.Clock.Now()}
	all, err := u.AIConnections().List(ctx)
	if err != nil {
		return s, err
	}
	for _, c := range all {
		s.connections[c.ID] = c
		models, err := u.AIModels().Of(ctx, c.ID)
		if err != nil {
			return s, err
		}
		s.models[c.ID] = map[string]storage.AIModel{}
		for _, m := range models {
			s.models[c.ID][m.Model] = m
		}
		prices, err := u.AIPrices().Current(ctx, c.ID)
		if err != nil {
			return s, err
		}
		s.priced[c.ID] = map[string]bool{}
		for _, p := range prices {
			s.priced[c.ID][p.Model] = s.priced[c.ID][p.Model] || p.Status == aiprice.Active
		}
	}
	policy, err := u.AIDataPolicies().Latest(ctx)
	if err != nil {
		return s, err
	}
	if len(policy.Policy) > 0 {
		if err := json.Unmarshal(policy.Policy, &s.policy); err != nil {
			return s, err
		}
	}
	return s, nil
}

// serves is why a step cannot serve its route's classes now, nil when it can: its connection on, its model allowed and
// callable, the policy letting every class go there (AI-72, AI-75, AI-81), and a price where the connection bills by
// usage (AI-82).
func (s routeScene) serves(i int, step airoute.Step, classes []string) error {
	c, ok := s.connections[step.Connection]
	m, listed := s.models[step.Connection][step.Model]
	allowed := listed && m.Allowed && aimodel.Callable(aimodel.Model{ID: m.Model, Shutdown: m.Shutdown, Listed: m.Listed, NotCallableAt: m.NotCallableAt}, s.now)
	var policy error
	if ok {
		policy = aipolicy.Allowed(s.policy, aipolicy.Connection{ID: c.ID.String(), Enabled: c.State == "enabled", Declaration: declared(c)}, step.Model,
			airoute.Feature, classes)
	}
	if err := airoute.StepFailure(i, step, ok && c.State == "enabled", allowed, policy); err != nil {
		return err
	}
	return aiprice.Serves(c.Billing, c.SelfHosted, s.priced[c.ID][step.Model], step.Model)
}

func routeOut(r storage.AIRoute, s routeScene) AIRoute {
	out := AIRoute{Version: r.Version, Classes: []RouteClass{}, Preset: r.Preset, Steps: []AIRouteStep{}, MadeBy: r.MadeBy, MadeAt: r.MadeAt.UTC()}
	for _, c := range r.Classes {
		out.Classes = append(out.Classes, RouteClass(c))
	}
	if r.Approval != nil {
		out.Approval = r.Approval.String()
	}
	for i, step := range r.Steps {
		st := AIRouteStep{ConnectionID: step.ConnectionID.String(), Connection: s.connections[step.ConnectionID].Name, Model: step.Model,
			Effort: step.Effort, Cache: step.Cache, Serves: true}
		if err := s.serves(i, airoute.Step{Connection: step.ConnectionID, Model: step.Model}, r.Classes); err != nil {
			st.Serves, st.Why = false, problemMessage(err)
		}
		out.Steps = append(out.Steps, st)
	}
	return out
}

// problemMessage is a refusal's message in Fyniq's words.
func problemMessage(err error) string {
	var re *airoute.Error
	var pe *aiprice.Error
	switch {
	case errors.As(err, &re):
		return re.Message
	case errors.As(err, &pe):
		return pe.Message
	}
	return err.Error()
}

func agentOut(a storage.AIAgent) AIAgentRoute {
	out := AIAgentRoute{Agent: a.ID, Name: a.Name, Description: a.Description, Handles: []RouteClass{}, Line: a.Line}
	for _, c := range a.Classes {
		out.Handles = append(out.Handles, RouteClass(c))
	}
	return out
}

// routeProblem is a refusal of the route rules, with the field it is about.
func routeProblem(err error) error {
	var e *airoute.Error
	if errors.As(err, &e) {
		return problem(e.Code, e.Field+" "+e.Message+".", e.Fix, FieldProblem{Code: e.Code, Field: "body." + e.Field, Message: e.Message, Fix: e.Fix})
	}
	return priceProblem(err)
}

// agentOf is the release's agent named id.
func agentOf(ctx context.Context, u storage.UnitOfWork, id string) (storage.AIAgent, error) {
	all, err := u.AIRoutes().Agents(ctx)
	if err != nil {
		return storage.AIAgent{}, err
	}
	for _, a := range all {
		if a.ID == id {
			return a, nil
		}
	}
	return storage.AIAgent{}, problem(CodeNotFound, id+" is not an agent of this release.", "Choose one of the agents GET /v1/ai/routes lists")
}

// routeInForce is an agent's route in force, nil when it has none.
func routeInForce(ctx context.Context, u storage.UnitOfWork, agent string) (*storage.AIRoute, error) {
	all, err := u.AIRoutes().Versions(ctx, agent, 1)
	if err != nil || len(all) == 0 {
		return nil, err
	}
	return &all[0], nil
}

func domainRoute(r storage.AIRoute) airoute.Route {
	out := airoute.Route{Classes: r.Classes}
	for _, s := range r.Steps {
		out.Steps = append(out.Steps, airoute.Step{Connection: s.ConnectionID, Model: s.Model, Effort: s.Effort, Cache: s.Cache})
	}
	return out
}

// checkRoute checks a route for its agent, and each step against the organisation as it is now.
func (a *API) checkRoute(ctx context.Context, u storage.UnitOfWork, agent storage.AIAgent, r airoute.Route) error {
	if err := airoute.Check(airoute.Agent{ID: agent.ID, Name: agent.Name, Classes: agent.Classes, Line: agent.Line}, r); err != nil {
		return routeProblem(err)
	}
	scene, err := a.routeScene(ctx, u)
	if err != nil {
		return err
	}
	for i, s := range r.Steps {
		if err := scene.serves(i, s, r.Classes); err != nil {
			return routeProblem(err)
		}
	}
	return nil
}

// aiRouteDependencies are the agent's route version (AI-74).
func aiRouteDependencies(ctx context.Context, u storage.UnitOfWork, agent string) (int, map[string]any, error) {
	r, err := routeInForce(ctx, u, agent)
	if err != nil {
		return 0, nil, err
	}
	version := 0
	if r != nil {
		version = r.Version
	}
	return version, map[string]any{"version": version}, nil
}

func registerAIRoutes(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.GetAIRoutes, huma.Operation{
		OperationID: "getAIRoutes", Method: http.MethodGet, Path: "/v1/ai/routes", Tags: []string{"AI"},
		Summary: "Get every agent's route",
		Description: "Every agent of this release and its route in force (AI-74): the data classes it handles and its steps, the primary first, " +
			"each with whether it can serve now and why not. An agent without a route does not run. Read-only; no side effects.",
	}, locks, func(ctx context.Context, _ *struct{}) (*aiRouteListOutput, error) {
		p, _ := authn.From(ctx)
		out := AIRouteList{Agents: []AIAgentRoute{}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			agents, err := u.AIRoutes().Agents(ctx)
			if err != nil {
				return err
			}
			routes, err := u.AIRoutes().Current(ctx)
			if err != nil {
				return err
			}
			scene, err := a.routeScene(ctx, u)
			if err != nil {
				return err
			}
			for _, ag := range agents {
				item := agentOut(ag)
				if i := slices.IndexFunc(routes, func(r storage.AIRoute) bool { return r.Agent == ag.ID }); i >= 0 {
					r := routeOut(routes[i], scene)
					item.Route = &r
				}
				out.Agents = append(out.Agents, item)
			}
			return nil
		})
		return &aiRouteListOutput{Body: out}, err
	})

	register(a, authz.GetAIRoute, huma.Operation{
		OperationID: "getAIRoute", Method: http.MethodGet, Path: "/v1/ai/routes/{agent}", Tags: []string{"AI"},
		Summary: "Get an agent's route and its versions",
		Description: "An agent, its route in force with its ETag (0 before it has one), and its versions, newest first (AI-74). Read-only; no " +
			"side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *AIRouteAgentPath) (*aiRouteGetOutput, error) {
		p, _ := authn.From(ctx)
		out := AIAgentRouteHistory{Versions: []AIRoute{}}
		version := 0
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			ag, err := agentOf(ctx, u, in.Agent)
			if err != nil {
				return err
			}
			out.AIAgentRoute = agentOut(ag)
			versions, err := u.AIRoutes().Versions(ctx, ag.ID, 20)
			if err != nil {
				return err
			}
			scene, err := a.routeScene(ctx, u)
			if err != nil {
				return err
			}
			for i, v := range versions {
				r := routeOut(v, scene)
				if i == 0 {
					out.Route, version = &r, v.Version
				}
				out.Versions = append(out.Versions, r)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return &aiRouteGetOutput{ETag: etag(version), Body: out}, nil
	})

	register(a, authz.SetAIRoute, huma.Operation{
		OperationID: "setAIRoute", Method: http.MethodPut, Path: "/v1/ai/routes/{agent}", Tags: []string{"AI"},
		Summary: "Set an agent's route",
		Description: "Sets an agent's route (AI-74, AI-26): the data classes it handles and one to five steps, each a connection that is on and a " +
			"model on its allow-list that the data-class policy lets every class go to, priced where the connection bills by usage (AI-82). " +
			"A version that only narrows where data goes (reordering, dropping a step or a class, the effort or caching) is saved at once (200, " +
			"the route). One that names a connection or model, or a class, the one in force did not, and an agent's first route, waits for the " +
			"Owner or a second approver (202, the approval request): model IDs change only so. Needs If-Match with the route's ETag (0 before " +
			"it has one); idempotent with Idempotency-Key; Prefer: return=preview checks it and saves or asks nothing. On the never-list for the " +
			"AI and MCP (§9.6).",
	}, append([]string{CodeNotFound, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeSessionRequired, CodeStepUpRequired,
		CodeIfMatchRequired, CodeStale, approval.CodePending, airoute.CodeRoute, airoute.CodeStep, aiprice.CodeUnpriced, storage.CodeAIRoute}, locks...),
		func(ctx context.Context, in *aiRouteSetInput) (*aiRouteSetOutput, error) {
			resp, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (AIRouteChange, error) {
				ag, err := agentOf(ctx, u, in.Agent)
				if err != nil {
					return AIRouteChange{}, err
				}
				was, err := routeInForce(ctx, u, ag.ID)
				if err != nil {
					return AIRouteChange{}, err
				}
				version := 0
				var prev *airoute.Route
				if was != nil {
					version = was.Version
					r := domainRoute(*was)
					prev = &r
				}
				if err := checkIfMatch(in.IfMatch, etag(version)); err != nil {
					return AIRouteChange{}, err
				}
				next, err := in.Body.domain()
				if err != nil {
					return AIRouteChange{}, err
				}
				if err := a.checkRoute(ctx, u, ag, next); err != nil {
					return AIRouteChange{}, err
				}
				if !airoute.Widens(prev, next) {
					saved, err := u.AIRoutes().Save(ctx, storedRoute(ag.ID, version+1, in.Body.Preset, next, nil, uploader(ctx), a.deps.Clock.Now()))
					if err != nil {
						return AIRouteChange{}, err
					}
					scene, err := a.routeScene(ctx, u)
					if err != nil {
						return AIRouteChange{}, err
					}
					r := routeOut(saved, scene)
					return AIRouteChange{Route: &r}, nil
				}
				steps, err := json.Marshal(in.Body.Steps)
				if err != nil {
					return AIRouteChange{}, err
				}
				classes, err := json.Marshal(in.Body.Classes)
				if err != nil {
					return AIRouteChange{}, err
				}
				diff := []ApprovalDiff{{Field: "classes", After: string(classes)}}
				for i, s := range next.Steps {
					diff = append(diff, ApprovalDiff{Field: fmt.Sprintf("step %d", i+1), After: s.Model + " on " + s.Connection.String()})
				}
				asked, err := a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectAIRoute, ObjectKey: ag.ID, Operation: approval.OpAIRouteChange,
					Summary: "Change the route of " + ag.Name + ", which sends its data somewhere new",
					Execute: map[string]any{"agent": ag.ID, "version": version + 1, "classes": string(classes), "steps": string(steps), "preset": in.Body.Preset},
					Diff:    diff})
				if err != nil {
					return AIRouteChange{}, err
				}
				return AIRouteChange{Approval: &asked}, nil
			})
			if err != nil {
				return nil, err
			}
			return &aiRouteSetOutput{Status: resp.Status, Replayed: replayedHeader(resp.Replayed), Applied: applied(resp), Body: resp.Body}, nil
		})
}

// domain is a route as it is set, as the domain checks it.
func (s AIRouteSet) domain() (airoute.Route, error) {
	out := airoute.Route{}
	for _, c := range s.Classes {
		out.Classes = append(out.Classes, string(c))
	}
	for i, st := range s.Steps {
		id, err := uuid.Parse(st.ConnectionID)
		if err != nil {
			return out, invalid([]FieldProblem{{Code: CodeInvalid, Field: fmt.Sprintf("body.steps[%d].connection_id", i), Message: "is not a connection's ID",
				Fix: "Name a connection GET /v1/ai/connections lists"}})
		}
		out.Steps = append(out.Steps, airoute.Step{Connection: id, Model: st.Model, Effort: st.Effort, Cache: st.Cache})
	}
	return out, nil
}

func storedRoute(agent string, version int, preset string, r airoute.Route, approvalID *uuid.UUID, by string, at time.Time) storage.AIRoute {
	out := storage.AIRoute{Agent: agent, Version: version, Classes: r.Classes, Preset: preset, Approval: approvalID, MadeBy: by, MadeAt: at}
	for i, s := range r.Steps {
		out.Steps = append(out.Steps, storage.AIRouteStep{Position: i + 1, ConnectionID: s.Connection, Model: s.Model, Effort: s.Effort, Cache: s.Cache})
	}
	return out
}

// executeAIRoute makes an approved route its agent's next version, after checking it once more against the
// organisation as it is now (AI-74).
func (a *API) executeAIRoute(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, s stored) error {
	str := func(k string) string { v, _ := s.Execute[k].(string); return v }
	ag, err := agentOf(ctx, u, str("agent"))
	if err != nil {
		return err
	}
	set := AIRouteSet{Preset: str("preset")}
	if err := json.Unmarshal([]byte(str("classes")), &set.Classes); err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(str("steps")), &set.Steps); err != nil {
		return err
	}
	next, err := set.domain()
	if err != nil {
		return err
	}
	if err := a.checkRoute(ctx, u, ag, next); err != nil {
		return err
	}
	version, err := strconv.Atoi(numberText(s.Execute["version"]))
	if err != nil {
		return err
	}
	_, err = u.AIRoutes().Save(ctx, storedRoute(ag.ID, version, set.Preset, next, &r.ID, "user:"+r.RequestedBy.String(), a.deps.Clock.Now()))
	return err
}
