package ai

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/aibudget"
	"github.com/cognizhi/fyniq/internal/domain/aimodel"
	"github.com/cognizhi/fyniq/internal/domain/aipolicy"
	"github.com/cognizhi/fyniq/internal/domain/aiprice"
	"github.com/cognizhi/fyniq/internal/domain/airoute"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/notify"
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
	rate       *apd.Decimal // the day's rate from the price's currency into the base currency; nil when there is none
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
			if err := r.priceStep(ctx, u, &st); err != nil {
				return err
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
		reservation, err := r.reserve(ctx, org, s, call, attempt)
		if err != nil {
			var se *storage.Error
			switch {
			case errors.As(err, &se) && se.Code == storage.CodeAIConnectionSpent: // the next step may still serve (AI-86)
				attempt.Outcome, attempt.Detail = airoute.Skipped, se.Message
				if err := r.record(ctx, org, &res, attempt); err != nil {
					return res, err
				}
				reasons, previous = append(reasons, fmt.Sprintf("step %d: %s", s.position, se.Message)), s.position
				continue
			case errors.As(err, &se) && se.Code == storage.CodeAIBudgetSpent:
				attempt.Outcome, attempt.Detail = airoute.Stopped, se.Message
				return res, errors.Join(aibudget.Spent(se.Message), r.record(ctx, org, &res, attempt))
			}
			return res, err
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
			if err := r.settle(ctx, org, reservation, s, &attempt, true); err != nil {
				return res, err
			}
			if err := r.record(ctx, org, &res, attempt); err != nil {
				return res, err
			}
			res.Response, res.Step, res.Connection = resp, s.position, s.connection.ID
			return res, nil
		}
		outcome, next := airoute.Failover(err, errors.Is(err, errDelivered))
		attempt.Outcome, attempt.Detail = outcome, cut(err.Error())
		// A busy, missing or refused answer is not billed; a timeout, a broken stream or a failure may have been, so its
		// reservation stays whole until the provider's usage is reconciled (AI-86).
		billed := outcome == airoute.TimedOut || outcome == airoute.Failed || outcome == airoute.Stopped
		if err := r.settle(ctx, org, reservation, s, &attempt, !billed); err != nil {
			return res, err
		}
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

// reserve reserves an attempt's most against every budget that applies (AI-86): its most usage at the step's price, in
// the base currency at the day's rate; nothing for a connection with no per-token price; unconverted when there is no
// rate, which a budget that applies refuses. A budget it takes to 80% this month alerts the Owner (AI-77).
func (r Router) reserve(ctx context.Context, org uuid.UUID, s step, call Call, a storage.AICall) (uuid.UUID, error) {
	most := "0"
	if s.price != nil {
		most = ""
		cost, missing, err := domainPrice(*s.price).Cost(aibudget.MostUsage(call.Request), 0, 1)
		if err == nil && missing == "" && s.rate != nil {
			if most, err = aibudget.InBase(cost.Text('f'), s.rate); err != nil {
				return uuid.Nil, err
			}
		}
	}
	var id uuid.UUID
	err := r.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		var alerts []storage.AIBudgetAlert
		var err error
		id, alerts, err = u.AIBudgets().Reserve(ctx, storage.AIReservation{Run: a.Run, Step: a.Step, Agent: a.Agent, Connection: a.ConnectionID,
			Person: a.Actor, Month: aibudget.Month(a.StartedAt), Amount: most})
		if err != nil {
			return err
		}
		d := outbox.Dispatcher{Store: r.Store, Clock: r.Clock}
		if r.Outbox != nil {
			d = *r.Outbox
		}
		for _, al := range alerts {
			key := uuid.NewSHA1(uuid.NameSpaceOID, []byte(org.String()+" ai-budget "+al.Scope+" "+al.Subject+" "+aibudget.Month(a.StartedAt)))
			title := fmt.Sprintf("The AI budget of the %s reached %d%%: %s of %s used this month", budgetName(al), aibudget.AlertAt, al.Used, al.Amount)
			if _, err := notify.Emit(ctx, d, u, org, notify.Event{Type: notify.AIBudgetType, RecordType: "ai_budget", RecordID: &key, Title: title,
				Link: "/settings/ai/usage", At: a.StartedAt, Audience: notify.Audience{Capability: string(authz.AIProviders), Need: "A"}}); err != nil {
				return err
			}
		}
		return nil
	})
	return id, err
}

func budgetName(a storage.AIBudgetAlert) string {
	if a.Subject == "" {
		return a.Scope
	}
	return a.Scope + " " + a.Subject
}

// settle settles an attempt's reservation to what it cost in the base currency, or keeps it whole when the outcome or
// the cost is not known (AI-86), and puts the cost in the base currency on the attempt.
func (r Router) settle(ctx context.Context, org, reservation uuid.UUID, s step, a *storage.AICall, known bool) error {
	actual := "0"
	if a.Outcome == airoute.Served {
		switch {
		case s.price == nil:
		case a.Cost != "" && s.rate != nil:
			base, err := aibudget.InBase(a.Cost, s.rate)
			if err != nil {
				return err
			}
			a.CostBase, actual = base, base
		default:
			known = false // priced, but this call's cost could not be put in the base currency: the reservation stays
		}
	}
	return r.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		return u.AIBudgets().Settle(ctx, reservation, actual, known)
	})
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

// ProbeAgent is the agent a probe runs as (AI-80): its calls are outside any route, recorded as route version 0.
const ProbeAgent = "probe"

// Probe asks a model a few tokens of synthetic text on its connection, outside any route (AI-80): like any call it
// reserves against the budgets, settles and is recorded, as the probe agent's (AI-86).
func (r Router) Probe(ctx context.Context, org uuid.UUID, c storage.AIConnection, provider ports.ModelProvider, req ports.ModelRequest,
	actor string) (ports.ModelResponse, error) {
	run, err := uuid.NewV7()
	if err != nil {
		return ports.ModelResponse{}, err
	}
	s := step{position: 1, connection: c, model: req.Model, provider: provider, timeout: time.Minute, cache: false}
	s.state.Caps = provider.Capabilities()
	if err := r.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		return r.priceStep(ctx, u, &s)
	}); err != nil {
		return ports.ModelResponse{}, err
	}
	res := Result{Run: run}
	call := Call{Agent: ProbeAgent, Classes: []string{aipolicy.KnowHow}, Request: req, Actor: actor}
	attempt := storage.AICall{Run: run, Agent: ProbeAgent, Step: 1, ConnectionID: c.ID, ModelRequested: req.Model, Actor: actor,
		Classes: call.Classes, StartedAt: r.Clock.Now()}
	reservation, err := r.reserve(ctx, org, s, call, attempt)
	if err != nil {
		var se *storage.Error
		if errors.As(err, &se) && (se.Code == storage.CodeAIBudgetSpent || se.Code == storage.CodeAIConnectionSpent) {
			attempt.Outcome, attempt.Detail = airoute.Stopped, se.Message
			return ports.ModelResponse{}, errors.Join(aibudget.Spent(se.Message), r.record(ctx, org, &res, attempt))
		}
		return ports.ModelResponse{}, err
	}
	resp, callErr := r.attempt(ctx, s, call)
	attempt.LatencyMS = int(r.Clock.Now().Sub(attempt.StartedAt).Milliseconds())
	known := true
	if callErr == nil {
		attempt.Outcome, attempt.ModelReported, attempt.VersionReported = airoute.Served, cmp.Or(resp.Model, req.Model), resp.Version
		usage(&attempt, resp.Usage)
		if why := r.cost(&attempt, s, req, resp.Usage); why != "" {
			attempt.Detail = why
		}
	} else {
		outcome, _ := airoute.Failover(callErr, false)
		attempt.Outcome, attempt.Detail = outcome, cut(callErr.Error())
		known = !(outcome == airoute.TimedOut || outcome == airoute.Failed || outcome == airoute.Stopped)
	}
	if err := r.settle(ctx, org, reservation, s, &attempt, known); err != nil {
		return resp, err
	}
	if err := r.record(ctx, org, &res, attempt); err != nil {
		return resp, err
	}
	return resp, callErr
}

// priceStep finds a step's price in force and the day's rate from its currency into the base currency.
func (r Router) priceStep(ctx context.Context, u storage.UnitOfWork, s *step) error {
	organisation, err := u.Organisation(ctx)
	if err != nil {
		return err
	}
	base, err := money.Lookup(organisation.BaseCurrency)
	if err != nil {
		return err
	}
	prices, err := u.AIPrices().Current(ctx, s.connection.ID)
	if err != nil {
		return err
	}
	for i := range prices {
		if prices[i].Model == s.model && prices[i].Status == aiprice.Active {
			s.price, s.state.Priced = &prices[i], true
		}
	}
	if s.price != nil {
		if cur, err := money.Lookup(s.price.Currency); err == nil {
			if found, err := fx.Lookup(ctx, u, base, cur, r.Clock.Now()); err == nil {
				s.rate = found.Rate.Decimal()
			}
		}
	}
	return nil
}
