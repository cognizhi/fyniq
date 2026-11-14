package api

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	appclock "github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/aibudget"
	"github.com/cognizhi/fyniq/internal/domain/aiconnection"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ AI usage (AI-77)
//
// Requests, tokens by kind, cost in the base currency, fallbacks and errors per connection, model, agent, person and
// organisation for a calendar month in Singapore, with each subscription's quota used and when it resets. The
// provider's invoice stays the record: it is entered as a bill, and this view helps check it.

// AIUsageTotals are the use of some calls.
type AIUsageTotals struct {
	Requests         int64  `json:"requests" doc:"Attempts sent and answered or failed: not those skipped, or stopped before sending, such as by a budget." example:"120"`
	Served           int64  `json:"served" example:"117"`
	Fallbacks        int64  `json:"fallbacks" doc:"Served by a step after the route's first (AI-76)." example:"3"`
	Errors           int64  `json:"errors" doc:"Busy, timed out, missing, refused or failed." example:"3"`
	Skipped          int64  `json:"skipped" doc:"Steps the router passed over without sending them anything." example:"5"`
	InputTokens      int64  `json:"input_tokens" example:"812000"`
	OutputTokens     int64  `json:"output_tokens" doc:"Reasoning included." example:"96000"`
	CacheReadTokens  int64  `json:"cache_read_tokens" example:"640000"`
	CacheWriteTokens int64  `json:"cache_write_tokens" example:"12000"`
	ReasoningTokens  int64  `json:"reasoning_tokens" example:"30000"`
	Cost             string `json:"cost" doc:"In the base currency at each day's rate, exact; the screens round it for display." example:"4.3125"`
	Unpriced         int64  `json:"unpriced" doc:"Served attempts whose cost could not be put in the base currency." example:"0"`
}

func (t *AIUsageTotals) add(r storage.AIUsageRow) {
	t.Requests += r.Requests
	t.Served += r.Served
	t.Fallbacks += r.Fallbacks
	t.Errors += r.Errors
	t.Skipped += r.Skipped
	t.InputTokens += r.Input
	t.OutputTokens += r.Output
	t.CacheReadTokens += r.CacheRead
	t.CacheWriteTokens += r.CacheWrite
	t.ReasoningTokens += r.Reasoning
	t.Unpriced += r.Unpriced
	t.Cost = sumDecimal(t.Cost, r.CostBase)
}

var usageSum = apd.BaseContext.WithPrecision(80)

// sumDecimal is a + b exactly, as decimal text.
func sumDecimal(a, b string) string {
	x, _, err := apd.NewFromString(cmpOr(a, "0"))
	if err != nil {
		return a
	}
	y, _, err := apd.NewFromString(cmpOr(b, "0"))
	if err != nil {
		return a
	}
	var out apd.Decimal
	if _, err := usageSum.Add(&out, x, y); err != nil {
		return a
	}
	_, _, _ = usageSum.Reduce(&out, &out)
	return out.Text('f')
}

// AIQuotaUse is a subscription's quota, what its window used, and when it resets (AI-73, AI-77).
type AIQuotaUse struct {
	Amount int64     `json:"amount" example:"5000000"`
	Unit   string    `json:"unit" enum:"tokens,requests" example:"tokens"`
	Window string    `json:"window" enum:"day,month" example:"month"`
	Used   int64     `json:"used" example:"908000"`
	Resets time.Time `json:"resets" doc:"When the window starts again, in Singapore." example:"2026-11-30T16:00:00Z"`
}

// AIUsageByConnection is a connection's use.
type AIUsageByConnection struct {
	ConnectionID string `json:"connection_id" format:"uuid" example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b"`
	Name         string `json:"name" example:"Claude on Agent Platform"`
	AIUsageTotals
	Quota *AIQuotaUse `json:"quota,omitempty" doc:"A subscription's quota."`
}

// AIUsageByModel is a model's use on a connection.
type AIUsageByModel struct {
	ConnectionID string `json:"connection_id" format:"uuid" example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b"`
	Model        string `json:"model" example:"claude-opus-4-6"`
	AIUsageTotals
}

// AIUsageByAgent is an agent's use.
type AIUsageByAgent struct {
	Agent string `json:"agent" example:"chat"`
	AIUsageTotals
}

// AIUsageByPerson is the use for one person, or for the system's jobs.
type AIUsageByPerson struct {
	Person string `json:"person" doc:"user:<id>, or system:<job> for a job." example:"user:0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2d"`
	AIUsageTotals
}

// AIUsage is a month's AI use.
type AIUsage struct {
	Month        string                `json:"month" example:"2026-11"`
	Currency     string                `json:"currency" doc:"The base currency the costs are in." example:"SGD"`
	Total        AIUsageTotals         `json:"total"`
	ByConnection []AIUsageByConnection `json:"by_connection" nullable:"false"`
	ByModel      []AIUsageByModel      `json:"by_model" nullable:"false"`
	ByAgent      []AIUsageByAgent      `json:"by_agent" nullable:"false"`
	ByPerson     []AIUsageByPerson     `json:"by_person" nullable:"false"`
}

type (
	aiUsageInput struct {
		Month string `query:"month" pattern:"^[0-9]{4}-(0[1-9]|1[0-2])$" doc:"The calendar month in Singapore; this month when left out." example:"2026-11"`
	}
	aiUsageOutput struct{ Body AIUsage }
)

var usageMonth = regexp.MustCompile(monthPattern)

// monthWindow is a calendar month in Singapore as an interval.
func monthWindow(month string) (time.Time, time.Time, error) {
	start, err := time.ParseInLocation("2006-01", month, appclock.Singapore)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return start, start.AddDate(0, 1, 0), nil
}

// quotaUse is a subscription's quota used in its current window (AI-73).
func quotaUse(ctx context.Context, u storage.UnitOfWork, c storage.AIConnection, now time.Time) (*AIQuotaUse, error) {
	if c.Billing != "subscription" || len(c.Quota) == 0 {
		return nil, nil
	}
	var q aiconnection.Quota
	if err := json.Unmarshal(c.Quota, &q); err != nil {
		return nil, err
	}
	start, resets := q.Period(now, appclock.Singapore)
	rows, err := u.AICalls().Usage(ctx, start, resets)
	if err != nil {
		return nil, err
	}
	out := &AIQuotaUse{Amount: q.Amount, Unit: q.Unit, Window: q.Window, Resets: resets.UTC()}
	for _, r := range rows {
		if r.ConnectionID != c.ID {
			continue
		}
		if q.Unit == "requests" {
			out.Used += r.Requests
		} else {
			out.Used += r.Input + r.Output + r.CacheRead + r.CacheWrite
		}
	}
	return out, nil
}

func registerAIUsage(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.GetAIUsage, huma.Operation{
		OperationID: "getAIUsage", Method: http.MethodGet, Path: "/v1/ai/usage", Tags: []string{"AI"},
		Summary: "Get the AI usage of a month",
		Description: "Requests, tokens by kind, cost in the base currency, fallbacks and errors per connection, model, agent, person and the " +
			"organisation, for a calendar month in Singapore (AI-77), from the AI activity log; and each subscription's quota used in its " +
			"current window and when it resets. The provider's invoice stays the record: it is entered as a bill, and this view helps check it. " +
			"Read-only; no side effects.",
	}, append([]string{CodeInvalid}, locks...), func(ctx context.Context, in *aiUsageInput) (*aiUsageOutput, error) {
		p, _ := authn.From(ctx)
		now := a.deps.Clock.Now()
		month := in.Month
		if month == "" {
			month = aibudget.Month(now)
		}
		from, to, err := monthWindow(month)
		if err != nil || !usageMonth.MatchString(month) {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "query.month", Message: "is not a month as YYYY-MM", Fix: "Give a month such as 2026-11"}})
		}
		out := AIUsage{Month: month, Total: AIUsageTotals{Cost: "0"}, ByConnection: []AIUsageByConnection{}, ByModel: []AIUsageByModel{},
			ByAgent: []AIUsageByAgent{}, ByPerson: []AIUsageByPerson{}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			base, err := baseOf(ctx, u)
			if err != nil {
				return err
			}
			out.Currency = base.Code
			rows, err := u.AICalls().Usage(ctx, from, to)
			if err != nil {
				return err
			}
			connections := map[uuid.UUID]*AIUsageByConnection{}
			models := map[string]*AIUsageByModel{}
			agents := map[string]*AIUsageByAgent{}
			people := map[string]*AIUsageByPerson{}
			for _, r := range rows {
				out.Total.add(r)
				if connections[r.ConnectionID] == nil {
					connections[r.ConnectionID] = &AIUsageByConnection{ConnectionID: r.ConnectionID.String(), AIUsageTotals: AIUsageTotals{Cost: "0"}}
				}
				connections[r.ConnectionID].add(r)
				mk := r.ConnectionID.String() + " " + r.Model
				if models[mk] == nil {
					models[mk] = &AIUsageByModel{ConnectionID: r.ConnectionID.String(), Model: r.Model, AIUsageTotals: AIUsageTotals{Cost: "0"}}
				}
				models[mk].add(r)
				if agents[r.Agent] == nil {
					agents[r.Agent] = &AIUsageByAgent{Agent: r.Agent, AIUsageTotals: AIUsageTotals{Cost: "0"}}
				}
				agents[r.Agent].add(r)
				if people[r.Actor] == nil {
					people[r.Actor] = &AIUsageByPerson{Person: r.Actor, AIUsageTotals: AIUsageTotals{Cost: "0"}}
				}
				people[r.Actor].add(r)
			}
			all, err := u.AIConnections().List(ctx)
			if err != nil {
				return err
			}
			for _, c := range all {
				quota, err := quotaUse(ctx, u, c, now)
				if err != nil {
					return err
				}
				if connections[c.ID] == nil && quota == nil {
					continue
				}
				if connections[c.ID] == nil {
					connections[c.ID] = &AIUsageByConnection{ConnectionID: c.ID.String(), AIUsageTotals: AIUsageTotals{Cost: "0"}}
				}
				connections[c.ID].Name, connections[c.ID].Quota = c.Name, quota
			}
			for _, v := range connections {
				out.ByConnection = append(out.ByConnection, *v)
			}
			for _, v := range models {
				out.ByModel = append(out.ByModel, *v)
			}
			for _, v := range agents {
				out.ByAgent = append(out.ByAgent, *v)
			}
			for _, v := range people {
				out.ByPerson = append(out.ByPerson, *v)
			}
			sort.Slice(out.ByConnection, func(i, j int) bool { return out.ByConnection[i].Name < out.ByConnection[j].Name })
			sort.Slice(out.ByModel, func(i, j int) bool {
				return out.ByModel[i].ConnectionID+out.ByModel[i].Model < out.ByModel[j].ConnectionID+out.ByModel[j].Model
			})
			sort.Slice(out.ByAgent, func(i, j int) bool { return out.ByAgent[i].Agent < out.ByAgent[j].Agent })
			sort.Slice(out.ByPerson, func(i, j int) bool { return out.ByPerson[i].Person < out.ByPerson[j].Person })
			return nil
		})
		return &aiUsageOutput{Body: out}, err
	})
}
