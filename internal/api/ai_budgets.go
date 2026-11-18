package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/aibudget"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ AI budgets (AI-77, AI-86, AI-13)
//
// Monthly budgets in the base currency for the organisation, an agent, a connection or a person, and in tokens for the
// organisation and a person (AI-13), set by the Owner.
// Before an attempt is sent the router reserves its most against every budget that applies; at 80% the Owner is
// alerted once a month, and at 100% nothing a budget cannot cover is admitted.

// AIBudget is a monthly AI budget and what this month used of it.
type AIBudget struct {
	Key     string     `json:"key" doc:"The budget: organisation, agent:<agent>, connection:<id> or person:<user id> in the base currency; tokens:organisation or tokens:person:<user id> in tokens (AI-13)." example:"agent:chat"`
	Scope   string     `json:"scope" enum:"organisation,agent,connection,person,tokens_organisation,tokens_person" example:"agent"`
	Unit    string     `json:"unit" enum:"base,tokens" doc:"base: the base currency; tokens: a month's tokens, whatever the connections cost, so it bounds a subscription too (AI-13)." example:"base"`
	Subject string     `json:"subject,omitempty" doc:"The agent, the connection's ID or user:<id>; absent for the organisation." example:"chat"`
	Amount  string     `json:"amount,omitempty" doc:"Per calendar month in Singapore: in the base currency as an exact decimal, or a whole number of tokens; absent when removed." example:"50"`
	Used    string     `json:"used" doc:"What this month's reservations hold or settled against it, in its unit: a reservation whose outcome is unknown counts whole until it is reconciled." example:"12.5"`
	Version int        `json:"version" example:"1"`
	Alerted bool       `json:"alerted" doc:"Its 80% alert was raised this month." example:"false"`
	SetBy   string     `json:"set_by" example:"user:0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2d"`
	SetAt   *time.Time `json:"set_at,omitempty" example:"2026-11-14T09:00:00Z"`
}

// AIBudgetList is the organisation's AI budgets this month.
type AIBudgetList struct {
	Month    string     `json:"month" doc:"The calendar month in Singapore the use is of." example:"2026-11"`
	Currency string     `json:"currency" doc:"The base currency the budgets are in." example:"SGD"`
	Budgets  []AIBudget `json:"budgets" nullable:"false"`
}

// AIBudgetSet is a budget's new amount.
type AIBudgetSet struct {
	Amount *string `json:"amount" required:"false" doc:"Per calendar month: in the base currency, above zero with at most four decimal places, or for a token budget a whole number of tokens above zero; null removes the budget." example:"50.00"`
}

type (
	aiBudgetListOutput struct{ Body AIBudgetList }
	// AIBudgetPath names a budget in a path: exported, as huma fills only an exported embedded path struct.
	AIBudgetPath struct {
		Budget string `path:"budget" minLength:"3" maxLength:"100" doc:"organisation, agent:<agent>, connection:<id> or person:<user id>; tokens:organisation or tokens:person:<user id> for a token budget." example:"agent:chat"`
	}
	aiBudgetSetInput struct {
		AIBudgetPath
		KeyHeader
		PreferHeader
		IfMatchHeader
		Body AIBudgetSet
	}
	aiBudgetSetOutput struct {
		ETag     string `header:"ETag"`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     AIBudget
	}
)

// aiBudgetKey splits a budget's key into its scope and subject: tokens:organisation and tokens:person:<id> are the token
// budgets.
func aiBudgetKey(key string) (string, string) {
	prefix := ""
	if rest, ok := strings.CutPrefix(key, "tokens:"); ok {
		prefix, key = "tokens_", rest
	}
	scope, subject, _ := strings.Cut(key, ":")
	if scope == "person" && subject != "" {
		subject = "user:" + subject
	}
	if prefix != "" && scope != "organisation" && scope != "person" {
		return "tokens:" + scope, subject // no such token budget: the domain refuses it
	}
	return prefix + scope, subject
}

func aiBudgetOut(b storage.AIBudget, month string) AIBudget {
	key, unit := b.Scope, "base"
	switch b.Scope {
	case "person":
		key += ":" + strings.TrimPrefix(b.Subject, "user:")
	case "organisation":
	case "tokens_organisation":
		key, unit = "tokens:organisation", "tokens"
	case "tokens_person":
		key, unit = "tokens:person:"+strings.TrimPrefix(b.Subject, "user:"), "tokens"
	default:
		key += ":" + b.Subject
	}
	out := AIBudget{Key: key, Scope: b.Scope, Unit: unit, Subject: b.Subject, Amount: b.Amount, Used: b.Used, Version: b.Version,
		Alerted: b.Alerted == month, SetBy: b.SetBy}
	if out.Used == "" {
		out.Used = "0"
	}
	if !b.SetAt.IsZero() {
		at := b.SetAt.UTC()
		out.SetAt = &at
	}
	return out
}

// budgetProblem is a refusal of the budget rules, with the field it is about.
func aiBudgetProblem(err error) error {
	var e *aibudget.Error
	if errors.As(err, &e) {
		return problem(e.Code, e.Field+" "+e.Message+".", e.Fix, FieldProblem{Code: e.Code, Field: "body." + e.Field, Message: e.Message, Fix: e.Fix})
	}
	return err
}

// budgetSubjectExists refuses a budget for an agent, connection or person the organisation does not have.
func (a *API) aiBudgetSubjectExists(ctx context.Context, u storage.UnitOfWork, scope, subject string) error {
	switch scope {
	case "agent":
		_, err := agentOf(ctx, u, subject)
		return err
	case "connection":
		id, _ := uuid.Parse(subject)
		_, err := u.AIConnections().Get(ctx, id)
		return err
	case "person", "tokens_person":
		id, _ := uuid.Parse(strings.TrimPrefix(subject, "user:"))
		members, err := u.Members().List(ctx, a.deps.Clock.Now())
		if err != nil {
			return err
		}
		for _, m := range members {
			if m.UserID == id {
				return nil
			}
		}
		return problem(CodeNotFound, "The person is not a member of the organisation.", "Name a member as person:<user id>")
	}
	return nil
}

// aiBudgetList are the organisation's AI budgets and what this month used of each.
func (a *API) aiBudgetList(ctx context.Context, u storage.UnitOfWork) (AIBudgetList, error) {
	month := aibudget.Month(a.deps.Clock.Now())
	out := AIBudgetList{Month: month, Budgets: []AIBudget{}}
	base, err := baseOf(ctx, u)
	if err != nil {
		return out, err
	}
	out.Currency = base.Code
	all, err := u.AIBudgets().List(ctx, month)
	for _, b := range all {
		out.Budgets = append(out.Budgets, aiBudgetOut(b, month))
	}
	return out, err
}

func registerAIBudgets(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.GetAIBudgets, huma.Operation{
		OperationID: "getAIBudgets", Method: http.MethodGet, Path: "/v1/ai/budgets", Tags: []string{"AI"},
		Summary: "Get the AI budgets",
		Description: "The organisation's monthly AI budgets (AI-77, AI-86, AI-13), in its base currency and in tokens: the organisation's, each agent's, connection's " +
			"and person's, with what this calendar month in Singapore used of each, a reservation whose outcome is unknown counting whole until " +
			"it is reconciled, and whether its 80% alert was raised. Read-only; no side effects.",
	}, locks, func(ctx context.Context, _ *struct{}) (*aiBudgetListOutput, error) {
		p, _ := authn.From(ctx)
		var out AIBudgetList
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = a.aiBudgetList(ctx, u)
			return err
		})
		return &aiBudgetListOutput{Body: out}, err
	})

	register(a, authz.SetAIBudget, huma.Operation{
		OperationID: "setAIBudget", Method: http.MethodPut, Path: "/v1/ai/budgets/{budget}", Tags: []string{"AI"},
		Summary: "Set an AI budget",
		Description: "The Owner sets a monthly AI budget (AI-77, AI-86, AI-13): in the base currency, the organisation's, an agent's, a " +
			"connection's or a person's; in tokens, the organisation's (tokens:organisation) or a person's (tokens:person:<user id>), which " +
			"bound every connection, a subscription's too; a null amount removes it. From the next call the router reserves each attempt's " +
			"most cost and most tokens against it: at 80% the Owner is " +
			"alerted once a month, and at 100% nothing it cannot cover is admitted, while accounting, checks and deadlines keep working. Needs " +
			"If-Match with the budget's version (0 before it has one); idempotent with Idempotency-Key; Prefer: return=preview checks it and saves " +
			"nothing. Side effects: the audit event ai_budget.set. On the never-list for the AI and MCP (§9.6).",
	}, append([]string{CodeNotFound, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeIfMatchRequired, CodeStale, storage.CodeVersion,
		aibudget.CodeBudget}, locks...), func(ctx context.Context, in *aiBudgetSetInput) (*aiBudgetSetOutput, error) {
		month := aibudget.Month(a.deps.Clock.Now())
		resp, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (AIBudget, error) {
			scope, subject := aiBudgetKey(in.Budget)
			amount := ""
			if in.Body.Amount != nil {
				amount = *in.Body.Amount
			}
			if err := aibudget.Check(scope, subject, amount); err != nil {
				return AIBudget{}, aiBudgetProblem(err)
			}
			if err := a.aiBudgetSubjectExists(ctx, u, scope, subject); err != nil {
				return AIBudget{}, err
			}
			all, err := u.AIBudgets().List(ctx, month)
			if err != nil {
				return AIBudget{}, err
			}
			version := 0
			for _, b := range all {
				if b.Scope == scope && b.Subject == subject {
					version = b.Version
				}
			}
			if err := checkIfMatch(in.IfMatch, etag(version)); err != nil {
				return AIBudget{}, err
			}
			saved, err := u.AIBudgets().Set(ctx, storage.AIBudget{Scope: scope, Subject: subject, Amount: amount, Version: version + 1,
				SetBy: uploader(ctx), SetAt: a.deps.Clock.Now()})
			if err != nil {
				return AIBudget{}, err
			}
			for _, b := range all {
				if b.Scope == scope && b.Subject == subject {
					saved.Used = b.Used
				}
			}
			return aiBudgetOut(saved, month), nil
		})
		if err != nil {
			return nil, err
		}
		out := &aiBudgetSetOutput{Replayed: replayedHeader(resp.Replayed), Applied: applied(resp), Body: resp.Body}
		if !resp.Previewed {
			out.ETag = etag(resp.Body.Version)
		}
		return out, nil
	})
}
