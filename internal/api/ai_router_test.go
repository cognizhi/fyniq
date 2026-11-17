//go:build local

package api

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ai"
	appclock "github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/airoute"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// stepModel is one connection's provider as the router's tests script it: what it proves, and how it answers.
type stepModel struct {
	caps    ports.ModelCapabilities
	fail    error // the error each call answers with, if any
	partial bool  // stream one event before failing
	calls   int
}

func (s *stepModel) Capabilities() ports.ModelCapabilities { return s.caps }
func (s *stepModel) Call(_ context.Context, r ports.ModelRequest) (ports.ModelResponse, error) {
	s.calls++
	if s.fail != nil {
		return ports.ModelResponse{}, s.fail
	}
	return ports.ModelResponse{Parts: []ports.ModelPart{{Kind: ports.PartText, Text: "Revenue was 5000.00."}}, StopReason: ports.StopEnd,
		Usage: ports.ModelUsage{Input: 1000, Output: 500, Reasoning: 100}, Model: r.Model, Version: r.Model + "-20261001"}, nil
}
func (s *stepModel) Stream(ctx context.Context, r ports.ModelRequest, each func(ports.ModelEvent) error) (ports.ModelResponse, error) {
	if s.partial {
		s.calls++
		if err := each(ports.ModelEvent{Text: "Revenue"}); err != nil {
			return ports.ModelResponse{}, err
		}
		return ports.ModelResponse{}, s.fail
	}
	resp, err := s.Call(ctx, r)
	if err == nil {
		err = each(ports.ModelEvent{Text: "Revenue was 5000.00."})
	}
	return resp, err
}
func (s *stepModel) ListModels(context.Context, string) (ports.ModelPage, error) {
	return ports.ModelPage{}, nil
}

var full = ports.ModelCapabilities{Tools: true, StrictOutput: true, Streaming: true, Caching: true, Reasoning: true}

// aiSwitchedOn turns the organisation's AI switch on from today, as an approved change would (AI-13).
func aiSwitchedOn(e *env) {
	e.t.Helper()
	y, m, d := e.clock.Now().In(appclock.Singapore).Date()
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Settings().Record(ctx, ai.Switch, "false", "true", time.Date(y, m, d, 0, 0, 0, 0, time.UTC), 1)
		return err
	})
}

// The router follows an agent's route (AI-76, AI-83, AI-87, AI-27): before each attempt it checks the step against the
// classes the call carries and the capabilities it needs, skipping one that does not meet them without sending it
// anything; it fails over on a busy, missing or failing step only to an eligible one, and stops with a plain message and
// a code when none is left, when the provider refuses the request, or when a stream breaks after it began. Every
// attempt is recorded with the model requested and the model and version reported, its tokens and its exact cost.
// req: AI-76
// req: AI-83
// req: AI-87
// req: AI-27
func TestTheRouterFailsOverOnlyToEligibleSteps(t *testing.T) {
	e := newEnv(t)
	aiSwitchedOn(e)
	sg := routable(e, "Router Singapore", "SG", []string{"gpt-6"})
	us := routable(e, "Router overseas", "US", []string{"gpt-6"})
	models := map[uuid.UUID]*stepModel{sg: {caps: full}, us: {caps: full}}
	setRoute := func(agent string, classes []string, steps ...uuid.UUID) {
		e.do(func(ctx context.Context, u storage.UnitOfWork) error {
			versions, err := u.AIRoutes().Versions(ctx, agent, 1)
			if err != nil {
				return err
			}
			r := storage.AIRoute{Agent: agent, Version: len(versions) + 1, Classes: classes, MadeBy: "user:router", MadeAt: e.clock.Now()}
			if len(versions) > 0 {
				r.Version = versions[0].Version + 1
			}
			for _, c := range steps {
				r.Steps = append(r.Steps, storage.AIRouteStep{ConnectionID: c, Model: "gpt-6", Effort: "high", Cache: true})
			}
			_, err = u.AIRoutes().Save(ctx, r)
			return err
		})
	}
	// The route as storage keeps it: the overseas fallback stands in for a route set before the policy changed.
	setRoute("chat", []string{"business-figures", "personal-data"}, sg, us)
	router := ai.Router{Store: e.api.deps.Store, Clock: e.clock, Provider: func(_ context.Context, _ storage.UnitOfWork, c storage.AIConnection) (ports.ModelProvider, error) {
		return models[c.ID], nil
	}}
	ask := func(classes ...string) ai.Call {
		return ai.Call{Agent: "chat", Classes: classes, Actor: "user:router", Request: ports.ModelRequest{MaxTokens: 1024,
			Messages: []ports.ModelMessage{{Role: ports.RoleUser, Parts: []ports.ModelPart{{Kind: ports.PartText, Text: "What was revenue?"}}}}}}
	}
	outcomes := func(res ai.Result) string {
		var out []string
		for _, a := range res.Attempts {
			out = append(out, a.Outcome)
		}
		return strings.Join(out, ",")
	}
	codeOf := func(err error) string {
		var re *airoute.Error
		if errors.As(err, &re) {
			return re.Code
		}
		return ""
	}
	ctx := context.Background()

	// Business figures: the busy primary falls over to the overseas step, which serves; the cost is exact.
	models[sg].fail = ports.ErrModelBusy
	res, err := router.Run(ctx, e.books.Org, ask("business-figures"))
	if err != nil || res.Step != 2 || res.Connection != us || outcomes(res) != "busy,ok" || res.Attempts[1].FellBackFrom != 1 {
		t.Fatalf("a busy primary: %v %+v", err, res.Attempts)
	}
	served := res.Attempts[1]
	// 1,000 tokens at 2 and 500 at 8 per million.
	if served.ModelReported != "gpt-6" || served.VersionReported != "gpt-6-20261001" || served.Cost != "0.006" || served.Currency != "USD" ||
		served.Price == nil || served.Input != 1000 || served.Reasoning != 100 {
		t.Errorf("the served attempt: %+v", served)
	}

	// Personal data: the overseas step may not take it, so nothing is sent there and the run stops.
	us0 := models[us].calls
	res, err = router.Run(ctx, e.books.Org, ask("business-figures", "personal-data"))
	if codeOf(err) != airoute.CodeNoStep || outcomes(res) != "busy,skipped" || models[us].calls != us0 ||
		!strings.Contains(res.Attempts[1].Detail, "data-class policy") {
		t.Fatalf("a fallback that may not carry personal data: %v %+v", err, res.Attempts)
	}

	// A class the route does not handle, and an agent with no route.
	if _, err := router.Run(ctx, e.books.Org, ask("payroll")); codeOf(err) != airoute.CodeUnhandled {
		t.Errorf("payroll on a route without it: %v", err)
	}
	report := ask("business-figures")
	report.Agent = "report_commentary"
	if _, err := router.Run(ctx, e.books.Org, report); codeOf(err) != airoute.CodeNoRoute {
		t.Errorf("an agent with no route: %v", err)
	}

	// A refused request is not sent elsewhere; a missing model falls over and queues a read of its list.
	models[sg].fail = ports.ErrModelRequest
	if res, err := router.Run(ctx, e.books.Org, ask("business-figures")); codeOf(err) != airoute.CodeRefused || outcomes(res) != "refused" {
		t.Errorf("a refused request: %v %+v", err, res.Attempts)
	}
	models[sg].fail = ports.ErrModelNotFound
	missing, err := router.Run(ctx, e.books.Org, ask("business-figures"))
	if err != nil || outcomes(missing) != "not_found,ok" {
		t.Errorf("a missing model: %v %+v", err, missing.Attempts)
	}
	item, _ := ai.ReadItem(sg, "not_found", missing.Run.String())
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		row, err := u.Outbox().ByKey(ctx, ai.ReadKind, item.BusinessKey)
		if err == nil && row == nil {
			t.Error("no read queued after a missing model")
		}
		return err
	})

	// A step that has not proved what the call needs is skipped; a stream that broke after it began is not resent.
	models[sg].fail = nil
	models[sg].caps.Tools = false
	tools := ask("business-figures")
	tools.Request.Tools = []ports.ModelTool{{Name: "reports.figure", Description: "A figure.", Schema: []byte(`{"type":"object"}`)}}
	if res, err := router.Run(ctx, e.books.Org, tools); err != nil || outcomes(res) != "skipped,ok" ||
		!strings.Contains(res.Attempts[0].Detail, "tool calling") {
		t.Errorf("a step without tools: %v %+v", err, res.Attempts)
	}
	models[sg].caps.Tools = true
	models[sg].fail, models[sg].partial = errors.New("the stream broke"), true
	streamed := ask("business-figures")
	var pieces []string
	streamed.Stream = func(ev ports.ModelEvent) error { pieces = append(pieces, ev.Text); return nil }
	if res, err := router.Run(ctx, e.books.Org, streamed); codeOf(err) != airoute.CodeRefused || outcomes(res) != "failed" || len(pieces) != 1 {
		t.Errorf("a broken stream: %v %+v %v", err, res.Attempts, pieces)
	}

	// A moving alias carries no personal data (AI-87).
	models[sg].fail, models[sg].partial = nil, false
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AIModels().Record(ctx, storage.AIModelListRead{ConnectionID: sg, Trigger: "refresh", Outcome: "partial", Pages: 1,
			Failure: "the page cap", ReadBy: "user:router", ReadAt: e.clock.Now()}, []storage.AIModel{{ConnectionID: sg, Model: "gpt-6",
			AliasOf: "gpt-6-20261001", FirstListed: e.clock.Now(), LastListed: e.clock.Now(), Listed: true, Allowed: true}})
		return err
	})
	if res, err := router.Run(ctx, e.books.Org, ask("personal-data")); codeOf(err) != airoute.CodeNoStep ||
		!strings.Contains(res.Attempts[0].Detail, "moving alias") {
		t.Errorf("a moving alias with personal data: %v %+v", err, res.Attempts)
	}

	// Every attempt is in the log, in its run.
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		calls, err := u.AICalls().OfRun(ctx, res.Run)
		if err == nil && len(calls) != len(res.Attempts) {
			t.Errorf("the log has %d of the run's %d attempts", len(calls), len(res.Attempts))
		}
		return err
	})
}

// AI-84's stale limit: a connection whose model list has not been read whole for 14 days carries no personal data or
// payroll, so a call carrying them skips it without sending it anything, while know-how and business figures still
// go; a partial read does not make it current, and a complete read does.
// req: AI-84
func TestAStaleModelListCarriesNoPersonalData(t *testing.T) {
	e := newEnv(t)
	aiSwitchedOn(e)
	sg := routable(e, "Stale Singapore", "SG", []string{"gpt-6"})
	model := &stepModel{caps: full}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AIRoutes().Save(ctx, storage.AIRoute{Agent: "chat", Version: 1, Classes: []string{"business-figures", "personal-data"}, MadeBy: "user:router",
			MadeAt: e.clock.Now(), Steps: []storage.AIRouteStep{{ConnectionID: sg, Model: "gpt-6"}}})
		return err
	})
	router := ai.Router{Store: e.api.deps.Store, Clock: e.clock, Provider: func(context.Context, storage.UnitOfWork, storage.AIConnection) (ports.ModelProvider, error) {
		return model, nil
	}}
	ask := func(classes ...string) ai.Call {
		return ai.Call{Agent: "chat", Classes: classes, Actor: "user:router", Request: ports.ModelRequest{MaxTokens: 256,
			Messages: []ports.ModelMessage{{Role: ports.RoleUser, Parts: []ports.ModelPart{{Kind: ports.PartText, Text: "Who is on leave?"}}}}}}
	}
	read := func(outcome string) {
		e.do(func(ctx context.Context, u storage.UnitOfWork) error {
			r := storage.AIModelListRead{ConnectionID: sg, Trigger: "refresh", Outcome: outcome, Pages: 1, ReadBy: "user:router", ReadAt: e.clock.Now()}
			if outcome != "complete" {
				r.Failure = "page 2 did not answer"
			}
			_, err := u.AIModels().Record(ctx, r, []storage.AIModel{{ConnectionID: sg, Model: "gpt-6", FirstListed: e.clock.Now(), LastListed: e.clock.Now(), Listed: true, Allowed: true}})
			return err
		})
	}
	ctx := context.Background()
	if _, err := router.Run(ctx, e.books.Org, ask("personal-data")); err != nil {
		t.Fatalf("a list read today: %v", err)
	}

	e.clock.Advance(airoute.ListStale + time.Hour)
	sent := model.calls
	res, err := router.Run(ctx, e.books.Org, ask("business-figures", "personal-data"))
	var re *airoute.Error
	if !errors.As(err, &re) || re.Code != airoute.CodeNoStep || len(res.Attempts) != 1 || res.Attempts[0].Outcome != "skipped" ||
		!strings.Contains(res.Attempts[0].Detail, "AI-84") || model.calls != sent {
		t.Fatalf("personal data on a stale list: %v %+v", err, res.Attempts)
	}
	if _, err := router.Run(ctx, e.books.Org, ask("business-figures")); err != nil {
		t.Errorf("business figures on a stale list: %v", err)
	}
	read("partial")
	if res, err := router.Run(ctx, e.books.Org, ask("personal-data")); !errors.As(err, &re) || re.Code != airoute.CodeNoStep ||
		len(res.Attempts) != 1 || !strings.Contains(res.Attempts[0].Detail, "AI-84") {
		t.Errorf("personal data after a partial read: %v %+v", err, res.Attempts)
	}
	read("complete")
	if _, err := router.Run(ctx, e.books.Org, ask("personal-data")); err != nil {
		t.Errorf("personal data after a complete read: %v", err)
	}
}
