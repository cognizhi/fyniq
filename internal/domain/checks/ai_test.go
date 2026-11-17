//go:build local

package checks_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/aibudget"
	"github.com/cognizhi/fyniq/internal/domain/checks"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// C.2 #55: each of the six conditions on the AI's routes is flagged (AI-76…AI-78, AI-80, AI-81, AI-86): a route that
// fell back more often than the day's threshold; a moving alias not re-qualified this week; a budget, and a
// subscription's quota, within 20% of its limit; a model on a route retiring within 30 days; one the provider no longer
// serves; and a connection with a stored key whose model list was not read for 3 days. Raising the threshold and
// reading the list again clear theirs.
// req: C.2 #55
// req: AI-76
// req: AI-81
// req: controls/platform-checks Frequent fallbacks
// req: controls/platform-checks Moving alias not re-qualified
// req: controls/platform-checks Close to budget or quota
// req: controls/platform-checks Model retiring soon
// req: controls/platform-checks Model retired or no longer listed
// req: controls/platform-checks Stale model list
func TestTheAIsRoutesAreWatched(t *testing.T) {
	b := newBooks(t, false)
	const key = "ai.model-routes"
	if s := want(t, b.run(t), key, checks.OK); s.Detail != "No AI connection is set up" {
		t.Errorf("with none: %q", s.Detail)
	}
	approval := uuid.Must(uuid.NewV7())
	retires, keyed := now.Add(10*24*time.Hour), now.Add(-10*24*time.Hour)
	var openai, kimi storage.AIConnection
	do := func(f func(ctx context.Context, u storage.UnitOfWork) error) {
		t.Helper()
		if err := b.api.Do(ctx, b.org, f); err != nil {
			t.Fatal(err)
		}
	}
	read := func(c uuid.UUID, at time.Time, models []storage.AIModel) {
		t.Helper()
		do(func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.AIModels().Record(ctx, storage.AIModelListRead{ConnectionID: c, Trigger: "daily", Outcome: "complete", Pages: 1,
				Added: len(models), ReadBy: "system:ai.models.read", ReadAt: at}, models)
			return err
		})
	}
	do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if openai, err = u.AIConnections().Create(ctx, storage.AIConnection{Name: "OpenAI", Connector: "openai", Auth: "api-key",
			Configuration: json.RawMessage(`{}`), Host: "api.openai.com", Billing: "usage", CredentialRef: "checks/openai", CredentialSetAt: &keyed,
			CreatedBy: "user:founder", CreatedAt: keyed}, approval); err != nil {
			return err
		}
		kimi, err = u.AIConnections().Create(ctx, storage.AIConnection{Name: "Kimi plan", Connector: "openai-compatible", Preset: "kimi",
			Auth: "api-key", Configuration: json.RawMessage(`{}`), Host: "api.moonshot.ai", Billing: "subscription",
			Quota:     json.RawMessage(`{"amount":1000,"unit":"tokens","window":"month","fee":"19.00","currency":"USD"}`),
			CreatedBy: "user:founder", CreatedAt: now.Add(-10 * 24 * time.Hour)}, uuid.Must(uuid.NewV7()))
		return err
	})
	// The list was last read 5 days ago: an alias, a model retiring in 10 days, and one the provider will refuse.
	listedAt := now.Add(-5 * 24 * time.Hour)
	read(openai.ID, listedAt, []storage.AIModel{
		{ConnectionID: openai.ID, Model: "gpt-6", FirstListed: listedAt, LastListed: listedAt, Listed: true},
		{ConnectionID: openai.ID, Model: "gpt-old", FirstListed: listedAt, LastListed: listedAt, Listed: true, Shutdown: &retires},
		{ConnectionID: openai.ID, Model: "gpt-latest", AliasOf: "gpt-6", FirstListed: listedAt, LastListed: listedAt, Listed: true},
		{ConnectionID: openai.ID, Model: "gpt-gone", FirstListed: listedAt, LastListed: listedAt, Listed: true}})
	do(func(ctx context.Context, u storage.UnitOfWork) error {
		for _, m := range []string{"gpt-6", "gpt-old", "gpt-latest", "gpt-gone"} {
			if _, err := u.AIModels().Probe(ctx, openai.ID, m, true, false, "answered", listedAt); err != nil {
				return err
			}
			if _, err := u.AIModels().Allow(ctx, openai.ID, m, true, &approval); err != nil {
				return err
			}
		}
		// The alias passed chat's evals 8 days ago, so its week ended; after the route was set the provider refused gpt-gone.
		if _, err := u.AIQualifications().Record(ctx, storage.AIQualification{Agent: "chat", ConnectionID: openai.ID, Model: "gpt-latest",
			Suite: "chat", SuiteVersion: 1, Threshold: "1", Cases: 1, Passed: 1, Result: "pass", Run: uuid.Must(uuid.NewV7()), RanBy: "user:founder",
			RanAt: now.Add(-8 * 24 * time.Hour)}); err != nil {
			return err
		}
		var steps []storage.AIRouteStep
		for i, m := range []string{"gpt-6", "gpt-old", "gpt-latest", "gpt-gone"} {
			steps = append(steps, storage.AIRouteStep{Position: i + 1, ConnectionID: openai.ID, Model: m})
		}
		if _, err := u.AIRoutes().Save(ctx, storage.AIRoute{Agent: "chat", Version: 1, Classes: []string{"business-figures"}, MadeBy: "user:founder",
			MadeAt: now.Add(-6 * 24 * time.Hour), Steps: steps}); err != nil {
			return err
		}
		if _, err := u.AIModels().Probe(ctx, openai.ID, "gpt-gone", false, true, "the provider answered 404", now.Add(-time.Hour)); err != nil {
			return err
		}
		// Six fallbacks in the last day, more than the 5 recommended.
		for i := range 6 {
			if _, err := u.AICalls().Record(ctx, storage.AICall{Run: uuid.Must(uuid.NewV7()), Agent: "chat", RouteVersion: 1, Step: 2, FellBackFrom: 1,
				ConnectionID: openai.ID, ModelRequested: "gpt-old", ModelReported: "gpt-old", Actor: "user:founder", Classes: []string{"business-figures"},
				Outcome: "ok", Input: 10, Output: 5, StartedAt: now.Add(-time.Duration(i+1) * time.Hour)}); err != nil {
				return err
			}
		}
		// The subscription's month used 900 of its 1,000 tokens.
		if _, err := u.AICalls().Record(ctx, storage.AICall{Run: uuid.Must(uuid.NewV7()), Agent: "probe", Step: 1, ConnectionID: kimi.ID,
			ModelRequested: "kimi-k3", ModelReported: "kimi-k3", Actor: "user:founder", Classes: []string{"know-how"}, Outcome: "ok", Input: 800,
			Output: 100, StartedAt: now.Add(-2 * time.Hour)}); err != nil {
			return err
		}
		// The agent's budget of 10 has 8.5 reserved this month.
		if _, err := u.AIBudgets().Set(ctx, storage.AIBudget{Scope: "agent", Subject: "chat", Amount: "10", Version: 1, SetBy: "user:founder",
			SetAt: now}); err != nil {
			return err
		}
		_, _, err := u.AIBudgets().Reserve(ctx, storage.AIReservation{Run: uuid.Must(uuid.NewV7()), Step: 1, Agent: "chat", Connection: openai.ID,
			Person: "user:founder", Month: aibudget.Month(now), Amount: "8.5"})
		return err
	})

	s := want(t, b.run(t), key, checks.Warning)
	got := map[string]string{}
	for _, it := range itemsOf(t, s) {
		got[it.Kind+" "+it.Label] = it.Detail
	}
	for label, detail := range map[string]string{
		"ai_route The route of chat":                                 "fell back 6 times in the last 24 hours, more than 5",
		"ai_route Step 2 of the route of chat: gpt-old on OpenAI":    "Retires on 20 Nov 2026, within 30 days",
		"ai_route Step 3 of the route of chat: gpt-latest on OpenAI": "A moving alias not re-qualified this week",
		"ai_route Step 4 of the route of chat: gpt-gone on OpenAI":   "the route runs on its next step",
		"ai_budget The AI budget of the agent chat":                  "8.5 of SGD 10 used this month",
		"ai_connection The quota of Kimi plan":                       "900 of 1000 tokens used this month",
		"ai_connection The model list of OpenAI":                     "Last read successfully on 5 Nov 2026",
	} {
		if !strings.Contains(got[label], detail) {
			t.Errorf("%s: %q, want %q", label, got[label], detail)
		}
	}
	if len(got) != 7 {
		t.Errorf("%d items: %v", len(got), got)
	}

	// A higher threshold and a fresh read of the list clear theirs.
	do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Settings().Record(ctx, checks.FallbacksSetting, "5", "10", time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC), 1)
		return err
	})
	read(openai.ID, now.Add(-time.Hour), nil)
	s = want(t, b.run(t), key, checks.Warning)
	if items := itemsOf(t, s); len(items) != 5 {
		t.Errorf("after the fixes: %+v", items)
	}
}
