package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/aimodel"
	"github.com/cognizhi/fyniq/internal/domain/aipolicy"
	"github.com/cognizhi/fyniq/internal/domain/aiprice"
	"github.com/cognizhi/fyniq/internal/domain/airoute"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Router runs an agent's model calls along its route (PRD-001 AI-74, AI-76, AI-83, AI-87, AI-27; p1-ai-providers
// design D7). Before the first attempt and every fallback it checks the step against the union of the data classes the
// call carries and the capabilities it needs, and skips a step that does not meet them without sending it anything; it
// fails over on a busy, timed-out, missing or failing step only to an eligible one, and otherwise stops with a plain
// message and a code. Every attempt, skipped ones too, is recorded with the model requested and the model and version
// the provider reports, its tokens and its exact cost (AI-12, AI-77). It calls providers only through the model port.
type Router struct {
	Store storage.Store
	// Provider builds a connection's provider in the unit of work: the connector runtime's Model.
	Provider func(ctx context.Context, u storage.UnitOfWork, c storage.AIConnection) (ports.ModelProvider, error)
	Clock    ports.Clock
	// Outbox queues a read of a connection's model list after a not-found answer (AI-81); nil: the tick sends it.
	Outbox *outbox.Dispatcher
	// Admit admits each attempt before it is sent: the switch and the budgets (tasks 5.3 and 6.2); nil admits it.
	Admit func(ctx context.Context, org uuid.UUID, a storage.AICall) error
}

// Call is one model call an agent makes: its request without a model, the union of the data classes of everything it
// sends (conversation, cached prefixes and attachments included, AI-83), who it runs for, and, to stream it, where its
// events go.
type Call struct {
	Agent   string
	Classes []string
	Request ports.ModelRequest
	Actor   string
	Stream  func(ports.ModelEvent) error
}

// Result is a served call: the answer, the step and connection that gave it, and the run's attempts.
type Result struct {
	Run        uuid.UUID
	Response   ports.ModelResponse
	Step       int
	Connection uuid.UUID
	Attempts   []storage.AICall
}

// stepTimeout is how long an attempt may take when its connection names no timeout.
const stepTimeout = 120 * time.Second

// step is a route step as the router found it before the run.
type step struct {
	position   int
	connection storage.AIConnection
	model      string
	effort     string
	cache      bool
	state      airoute.StepState
	price      *storage.AIPrice
	provider   ports.ModelProvider
	timeout    time.Duration
}

// load reads the agent's route and what each step needs to be judged and called.
func (r Router) load(ctx context.Context, org uuid.UUID, call Call) (storage.AIRoute, []step, error) {
	var route storage.AIRoute
	var steps []step
	err := r.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		versions, err := u.AIRoutes().Versions(ctx, call.Agent, 1)
		if err != nil {
			return err
		}
		if len(versions) == 0 {
			return airoute.Stop(airoute.CodeNoRoute, call.Agent+" has no route, so it does not run",
				"Ask the Owner or an Administrator to set its route under Settings › AI")
		}
		route = versions[0]
		if err := airoute.Unhandled(call.Classes, route.Classes); err != nil {
			return err
		}
		stored, err := u.AIDataPolicies().Latest(ctx)
		if err != nil {
			return err
		}
		var policy aipolicy.Policy
		if len(stored.Policy) > 0 {
			if err := json.Unmarshal(stored.Policy, &policy); err != nil {
				return err
			}
		}
		now := r.Clock.Now()
		for _, s := range route.Steps {
			c, err := u.AIConnections().Get(ctx, s.ConnectionID)
			if err != nil {
				return err
			}
			st := step{position: s.Position, connection: c, model: s.Model, effort: s.Effort, cache: s.Cache, timeout: timeoutOf(c)}
			st.state = airoute.StepState{Enabled: c.State == "enabled", Billing: c.Billing, SelfHosted: c.SelfHosted}
			models, err := u.AIModels().Of(ctx, c.ID)
			if err != nil {
				return err
			}
			for _, m := range models {
				if m.Model == s.Model {
					st.state.Allowed, st.state.Alias = m.Allowed, m.AliasOf != ""
					st.state.Callable = aimodel.Callable(aimodel.Model{ID: m.Model, Shutdown: m.Shutdown, Listed: m.Listed, NotCallableAt: m.NotCallableAt}, now)
				}
			}
			prices, err := u.AIPrices().Current(ctx, c.ID)
			if err != nil {
				return err
			}
			for i := range prices {
				if prices[i].Model == s.Model && prices[i].Status == aiprice.Active {
					st.price, st.state.Priced = &prices[i], true
				}
			}
			st.state.Policy = aipolicy.Allowed(policy, aipolicy.Connection{ID: c.ID.String(), Enabled: c.State == "enabled", Declaration: declaration(c)},
				s.Model, airoute.Feature, call.Classes)
			if st.state.Enabled {
				st.provider, st.state.Built = r.Provider(ctx, u, c)
				if st.state.Built == nil {
					st.state.Caps = st.provider.Capabilities()
				}
			}
			steps = append(steps, st)
		}
		return nil
	})
	return route, steps, err
}

// declaration is a connection's data-handling declaration.
func declaration(c storage.AIConnection) aipolicy.Declaration {
	var d aipolicy.Declaration
	_ = json.Unmarshal(c.DataHandling, &d)
	return d
}

// timeoutOf is how long an attempt on c may take: its configured timeout, or stepTimeout.
func timeoutOf(c storage.AIConnection) time.Duration {
	var config struct {
		Seconds int `json:"timeout_seconds"`
	}
	if json.Unmarshal(c.Configuration, &config) != nil || config.Seconds <= 0 {
		return stepTimeout
	}
	return time.Duration(config.Seconds) * time.Second
}

// Run makes call along its agent's route and returns the first answer an eligible step gives.
func (r Router) Run(ctx context.Context, org uuid.UUID, call Call) (Result, error) {
	run, err := uuid.NewV7()
	if err != nil {
		return Result{}, err
	}
	res := Result{Run: run}
	route, steps, err := r.load(ctx, org, call)
	if err != nil {
		return res, err
	}
	needs := airoute.NeedsOf(call.Request, call.Stream != nil)
	var reasons []string
	previous := 0
	for _, s := range steps {
		attempt := storage.AICall{Run: run, Agent: call.Agent, RouteVersion: route.Version, Step: s.position, FellBackFrom: previous,
			ConnectionID: s.connection.ID, ModelRequested: s.model, Actor: call.Actor, Classes: append([]string{}, call.Classes...),
			StartedAt: r.Clock.Now()}
		if why := airoute.Why(call.Classes, needs, s.state, s.model); why != "" {
			attempt.Outcome, attempt.Detail = airoute.Skipped, why
			if err := r.record(ctx, org, &res, attempt); err != nil {
				return res, err
			}
			reasons, previous = append(reasons, fmt.Sprintf("step %d: %s", s.position, why)), s.position
			continue
		}
		if r.Admit != nil {
			if err := r.Admit(ctx, org, attempt); err != nil {
				attempt.Outcome, attempt.Detail = airoute.Stopped, err.Error()
				return res, errors.Join(err, r.record(ctx, org, &res, attempt))
			}
		}
		resp, err := r.attempt(ctx, s, call)
		attempt.LatencyMS = int(r.Clock.Now().Sub(attempt.StartedAt).Milliseconds())
		if err == nil {
			attempt.Outcome, attempt.ModelReported, attempt.VersionReported = airoute.Served, resp.Model, resp.Version
			if attempt.ModelReported == "" {
				attempt.ModelReported = s.model
			}
			usage(&attempt, resp.Usage)
			if why := r.cost(&attempt, s, call.Request, resp.Usage); why != "" {
				attempt.Detail = why
			}
			if err := r.record(ctx, org, &res, attempt); err != nil {
				return res, err
			}
			res.Response, res.Step, res.Connection = resp, s.position, s.connection.ID
			return res, nil
		}
		outcome, next := airoute.Failover(err, errors.Is(err, errDelivered))
		attempt.Outcome, attempt.Detail = outcome, cut(err.Error())
		if err := r.record(ctx, org, &res, attempt); err != nil {
			return res, err
		}
		if outcome == airoute.NotFound {
			if err := r.readList(ctx, org, s.connection.ID, run); err != nil {
				return res, err
			}
		}
		if !next {
			if outcome == airoute.Stopped {
				return res, err
			}
			return res, airoute.Stop(airoute.CodeRefused, "the provider did not answer the call, and it is not sent elsewhere: "+attempt.Detail,
				"Check the request, or try again")
		}
		reasons, previous = append(reasons, fmt.Sprintf("step %d: %s", s.position, outcome)), s.position
	}
	return res, airoute.Stop(airoute.CodeNoStep, "no step of the route could take the call, and nothing was sent where it may not go ("+
		strings.Join(reasons, "; ")+")", "Wait for the provider, or ask the Owner to add a fallback that may carry this data")
}

// errDelivered marks a stream that broke after its first event: it cannot be taken back, so it is not sent elsewhere.
var errDelivered = errors.New("the answer had begun")

// attempt sends the call to one step, with the step's model, effort and caching where the provider proved them (AI-27).
func (r Router) attempt(ctx context.Context, s step, call Call) (ports.ModelResponse, error) {
	req := call.Request
	req.Model = s.model
	if req.Effort == "" {
		req.Effort = s.effort
	}
	if !s.state.Caps.Reasoning {
		req.Effort = ""
	}
	req.Cache = s.cache && s.state.Caps.Caching
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	if call.Stream == nil {
		return s.provider.Call(ctx, req)
	}
	delivered := false
	resp, err := s.provider.Stream(ctx, req, func(e ports.ModelEvent) error {
		delivered = true
		return call.Stream(e)
	})
	if err != nil && delivered {
		err = errors.Join(errDelivered, err)
	}
	return resp, err
}

func usage(a *storage.AICall, u ports.ModelUsage) {
	a.Input, a.Output, a.CacheRead, a.CacheWrite, a.Reasoning = u.Input, u.Output, u.CacheRead, u.CacheWrite, min(u.Reasoning, u.Output)
}

// cost prices an answer exactly with the step's price in force (AI-77, AI-85): "" when priced, or why it is not.
func (r Router) cost(a *storage.AICall, s step, req ports.ModelRequest, u ports.ModelUsage) string {
	if s.price == nil {
		return "no price in force for " + s.model
	}
	var images int64
	for _, m := range req.Messages {
		for _, p := range m.Parts {
			if p.Kind == ports.PartImage {
				images++
			}
		}
	}
	cost, missing, err := domainPrice(*s.price).Cost(u, images, 1)
	switch {
	case err != nil:
		return "the cost could not be computed: " + err.Error()
	case missing != "":
		return "unpriced for this call: its price gives no rate for " + missing
	}
	a.Cost, a.Currency, a.Price = cost.Text('f'), s.price.Currency, &s.price.ID
	return ""
}

// record keeps an attempt in the organisation's activity log.
func (r Router) record(ctx context.Context, org uuid.UUID, res *Result, a storage.AICall) error {
	return r.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		saved, err := u.AICalls().Record(ctx, a)
		if err == nil {
			res.Attempts = append(res.Attempts, saved)
		}
		return err
	})
}

// readList queues a read of the connection's model list after the provider said it does not know a model (AI-81).
func (r Router) readList(ctx context.Context, org, connection, run uuid.UUID) error {
	item, err := ReadItem(connection, aimodel.ByNotFound, run.String())
	if err != nil {
		return err
	}
	return r.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		if r.Outbox != nil {
			_, err := r.Outbox.Enqueue(ctx, u, org, item)
			return err
		}
		_, err := u.Outbox().Enqueue(ctx, item)
		return err
	})
}
