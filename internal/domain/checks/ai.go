package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/aibudget"
	"github.com/cognizhi/fyniq/internal/domain/aiconnection"
	"github.com/cognizhi/fyniq/internal/domain/aimodel"
	"github.com/cognizhi/fyniq/internal/domain/aiqual"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The AI's model routes (PRD-001 C.2 #55; AI-76…AI-78, AI-80, AI-81, AI-86; p1-ai-providers task 7.1).

// viewAI is the Appendix D row whose view opens the AI's items.
const viewAI = "AI providers and model routes: connections, credentials, billing, data-class policy, per-agent routes (rev 0.12, AI-72…AI-75)"

// FallbacksSetting is how many times a day a route may fall back before it needs attention (AI-76).
const FallbacksSetting = "ai.fallbacks_per_day"

// The limits of C.2 #55: a budget or quota within 20% of its amount, a model retiring within 30 days, and a model list
// with no successful read for 3 days.
const (
	aiNearPercent = 80
	aiRetiringIn  = 30 * 24 * time.Hour
	aiListStale   = 3 * 24 * time.Hour
)

var aiChecks = []Definition{
	{Key: "ai.model-routes", Ref: "C.2 #55", Area: "AI", Worst: Warning, Title: "The AI's routes can keep serving",
		Explain: "Each AI agent runs on a route: a model on a connection, then its fallbacks. This flags what needs a person before it " +
			"stops an agent: a route that fell back more often than the day's threshold, a moving model alias not re-qualified this " +
			"week, a budget or a subscription's quota within 20% of its limit, a model on a route retiring within 30 days or no " +
			"longer listed, and a connection with a stored key whose model list has not been read for 3 days.",
		Rules: "AI-76, AI-77, AI-78, AI-80, AI-81, AI-86", Owner: "owner", View: viewAI, Run: aiModelRoutes},
}

// C.2 #55: what about the AI's routes, budgets and connections needs a person (AI-76…AI-78, AI-80, AI-81).
func aiModelRoutes(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	connections, err := u.AIConnections().List(ctx)
	if err != nil {
		return Result{}, err
	}
	if len(connections) == 0 {
		return Fine("No AI connection is set up"), nil
	}
	names := map[string]string{}
	models := map[string]map[string]storage.AIModel{}
	quals := map[string][]storage.AIQualification{}
	for _, c := range connections {
		names[c.ID.String()] = c.Name
		all, err := u.AIModels().Of(ctx, c.ID)
		if err != nil {
			return Result{}, err
		}
		models[c.ID.String()] = map[string]storage.AIModel{}
		for _, m := range all {
			models[c.ID.String()][m.Model] = m
		}
		if quals[c.ID.String()], err = u.AIQualifications().Latest(ctx, c.ID); err != nil {
			return Result{}, err
		}
	}
	var items []Item

	// A route that fell back more often than the day's threshold (AI-76).
	threshold, err := fallbacksPerDay(ctx, u, e)
	if err != nil {
		return Result{}, err
	}
	usage, err := u.AICalls().Usage(ctx, e.Now.Add(-24*time.Hour), e.Now)
	if err != nil {
		return Result{}, err
	}
	fallbacks := map[string]int64{}
	for _, r := range usage {
		fallbacks[r.Agent] += r.Fallbacks
	}
	agents := make([]string, 0, len(fallbacks))
	for a := range fallbacks {
		agents = append(agents, a)
	}
	sort.Strings(agents)
	for _, a := range agents {
		if fallbacks[a] > int64(threshold) {
			items = append(items, Item{Kind: "ai_route", ID: a, Label: "The route of " + a,
				Detail: fmt.Sprintf("It fell back %d times in the last 24 hours, more than %d: look at its first step", fallbacks[a], threshold)})
		}
	}

	// Each route's models: retiring, gone, or a moving alias not re-qualified this week (AI-78, AI-81, AI-87).
	routes, err := u.AIRoutes().Current(ctx)
	if err != nil {
		return Result{}, err
	}
	for _, r := range routes {
		for _, st := range r.Steps {
			conn := st.ConnectionID.String()
			label := fmt.Sprintf("Step %d of the route of %s: %s on %s", st.Position, r.Agent, st.Model, names[conn])
			m, ok := models[conn][st.Model]
			state := aimodel.NoLongerListed
			if ok {
				state = aimodel.State(aimodel.Model{ID: m.Model, Shutdown: m.Shutdown, FirstListed: m.FirstListed, Listed: m.Listed,
					NotCallableAt: m.NotCallableAt}, e.Now)
			}
			switch {
			case state == aimodel.NoLongerListed || state == aimodel.NotCallable:
				items = append(items, Item{Kind: "ai_route", ID: r.Agent, Label: label,
					Detail: "Retired, no longer listed by its provider, or refused by it: the route runs on its next step; choose another model"})
				continue
			case m.Shutdown != nil && m.Shutdown.Sub(e.Now) <= aiRetiringIn:
				items = append(items, Item{Kind: "ai_route", ID: r.Agent, Label: label,
					Detail: "Retires on " + day(*m.Shutdown) + ", within 30 days: choose its successor"})
			}
			if m.AliasOf != "" && aiStanding(quals[conn], st.Model, r.Agent, e.Now) != aiqual.Qualified {
				items = append(items, Item{Kind: "ai_route", ID: r.Agent, Label: label,
					Detail: "A moving alias not re-qualified this week: qualify it again, or pin a version"})
			}
		}
	}

	// Budgets within 20% of their amount this month (AI-77, AI-86).
	budgets, err := u.AIBudgets().List(ctx, aibudget.Month(e.Now))
	if err != nil {
		return Result{}, err
	}
	for _, b := range budgets {
		if b.Amount == "" || !near(b.Used, b.Amount) {
			continue
		}
		unit := e.Base.Code + " "
		if aibudget.Tokens(b.Scope) {
			unit = ""
		}
		items = append(items, Item{Kind: "ai_budget", ID: b.Scope + ":" + b.Subject, Label: "The AI budget of the " + budgetLabel(b, names),
			Detail: fmt.Sprintf("%s%s of %s%s used this month", unit, b.Used, unit, b.Amount)})
	}

	// Subscriptions within 20% of their quota, and connections whose model list was not read for 3 days (AI-73, AI-80,
	// AI-84).
	for _, c := range connections {
		if c.Billing == "subscription" && len(c.Quota) > 0 {
			var q aiconnection.Quota
			if err := json.Unmarshal(c.Quota, &q); err != nil {
				return Result{}, err
			}
			start, resets := q.Period(e.Now, singapore)
			rows, err := u.AICalls().Usage(ctx, start, resets)
			if err != nil {
				return Result{}, err
			}
			units := map[string]int64{}
			for _, r := range rows {
				if r.ConnectionID == c.ID {
					units[r.Model] += q.Units(r.Requests, r.Input+r.Output+r.CacheRead+r.CacheWrite)
				}
			}
			used := q.Drawn(units)
			if q.Amount > 0 && used*100 >= q.Amount*aiNearPercent {
				items = append(items, Item{Kind: "ai_connection", ID: c.ID.String(), Label: "The quota of " + c.Name,
					Detail: fmt.Sprintf("%d of %d %s used this %s; it resets on %s", used, q.Amount, q.Unit, q.Window, day(resets))})
			}
		}
		if c.CredentialRef == "" {
			continue
		}
		reads, err := u.AIModels().Reads(ctx, c.ID, 50)
		if err != nil {
			return Result{}, err
		}
		var last *storage.AIModelListRead
		for i := range reads {
			if reads[i].Outcome != aimodel.Failed && (last == nil || reads[i].ReadAt.After(last.ReadAt)) {
				last = &reads[i]
			}
		}
		switch {
		case last == nil && e.Now.Sub(c.CreatedAt) > aiListStale:
			items = append(items, Item{Kind: "ai_connection", ID: c.ID.String(), Label: "The model list of " + c.Name,
				Detail: "Never read successfully: check its key, then refresh its models"})
		case last != nil && e.Now.Sub(last.ReadAt) > aiListStale:
			items = append(items, Item{Kind: "ai_connection", ID: c.ID.String(), Label: "The model list of " + c.Name,
				Detail: "Last read successfully on " + day(last.ReadAt) + ": check its key, then refresh its models"})
		}
	}
	if len(items) == 0 {
		return Fine("Every route serves without frequent fallbacks, on current models, with room in its budgets"), nil
	}
	return Found(Warning, items, fmt.Sprintf("%d thing(s) about the AI's routes need a person", len(items)),
		"Look at each on Settings › AI: the route, the model, the budget or the connection it names"), nil
}

// fallbacksPerDay is the organisation's FallbacksSetting today.
func fallbacksPerDay(ctx context.Context, u storage.UnitOfWork, e *Env) (int, error) {
	d, ok := settings.Lookup(FallbacksSetting)
	if !ok {
		return 0, fmt.Errorf("no setting %s", FallbacksSetting)
	}
	changes, err := u.Settings().Changes(ctx, FallbacksSetting)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(settings.On(d, changes, e.Today).Value)
}

// aiStanding is a model's standing for agent on a connection, from its runs there.
func aiStanding(quals []storage.AIQualification, model, agent string, now time.Time) string {
	for _, q := range quals {
		if q.Model == model && q.Agent == agent {
			return aiqual.Standing(&aiqual.Run{Passed: q.Result == aiqual.Pass, RanAt: q.RanAt, OutOfDate: q.OutOfDateAt != nil}, true, now)
		}
	}
	return aiqual.Unqualified
}

// near is whether used is within 20% of amount: at least 80% of it, exactly.
func near(used, amount string) bool {
	u, _, err1 := apd.NewFromString(used)
	a, _, err2 := apd.NewFromString(amount)
	if err1 != nil || err2 != nil || a.Sign() <= 0 {
		return false
	}
	var lhs, rhs apd.Decimal
	ctx := apd.BaseContext.WithPrecision(60)
	if _, err := ctx.Mul(&lhs, u, apd.New(100, 0)); err != nil {
		return false
	}
	if _, err := ctx.Mul(&rhs, a, apd.New(aiNearPercent, 0)); err != nil {
		return false
	}
	return lhs.Cmp(&rhs) >= 0
}

// budgetLabel names a budget's scope in words.
func budgetLabel(b storage.AIBudget, connections map[string]string) string {
	switch b.Scope {
	case storage.AIBudgetOrganisation:
		return "organisation"
	case storage.AIBudgetTokensOrganisation:
		return "organisation, in tokens"
	case storage.AIBudgetAgent:
		return "agent " + b.Subject
	case storage.AIBudgetConnection:
		return "connection " + connections[b.Subject]
	case storage.AIBudgetTokensPerson:
		return "person " + b.Subject + ", in tokens"
	}
	return "person " + b.Subject
}

// singapore is the calendar a subscription's quota window follows (AI-73).
var singapore = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Singapore")
	if err != nil {
		return time.FixedZone("SGT", 8*60*60)
	}
	return loc
}()
