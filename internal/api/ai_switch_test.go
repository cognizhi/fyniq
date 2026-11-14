//go:build local

package api

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/ai"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/airoute"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// holdingModel streams one event, says it started, and then holds the stream open until it is stopped.
type holdingModel struct {
	*stepModel
	started chan struct{}
}

func (m holdingModel) Stream(ctx context.Context, _ ports.ModelRequest, each func(ports.ModelEvent) error) (ports.ModelResponse, error) {
	if err := each(ports.ModelEvent{Text: "Revenue"}); err != nil {
		return ports.ModelResponse{}, err
	}
	m.started <- struct{}{}
	select {
	case <-ctx.Done():
		return ports.ModelResponse{}, ctx.Err()
	case <-time.After(10 * time.Second):
		return ports.ModelResponse{}, errors.New("the stream was never stopped")
	}
}

// The AI switch (AI-13, FR-ORG-06): off by default, so nothing runs; an Owner asks to switch it on and a second
// approver approves it; switching it off needs no one else and refuses new calls at once, and a stream already running
// stops at its next check, well inside a minute, kept whole in the budgets as its outcome is unknown.
// req: AI-13
// req: FR-ORG-06
// asvs: V8.1.4
func TestTheAISwitchStopsTheAI(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	ahkow, weiling := f.as(f.ahkow), f.as(f.weiling) // the Owner and the Controller, as the founders are
	ownerOnly := f.as(f.ahkow, authz.Owner)
	sg := routable(e, "Switch Singapore", "SG", []string{"gpt-6"})
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AIRoutes().Save(ctx, storage.AIRoute{Agent: "chat", Version: 1, Classes: []string{"business-figures"}, MadeBy: "user:switch",
			MadeAt: e.clock.Now(), Steps: []storage.AIRouteStep{{Position: 1, ConnectionID: sg, Model: "gpt-6"}}})
		return err
	})
	model := holdingModel{stepModel: &stepModel{caps: full}, started: make(chan struct{}, 1)}
	router := ai.Router{Store: e.api.deps.Store, Clock: e.clock, Outbox: e.api.deps.Outbox, SwitchEvery: 20 * time.Millisecond,
		Provider: func(context.Context, storage.UnitOfWork, storage.AIConnection) (ports.ModelProvider, error) {
			return model, nil
		}}
	ask := ai.Call{Agent: "chat", Classes: []string{"business-figures"}, Actor: "user:" + f.ahkow, Request: ports.ModelRequest{MaxTokens: 64,
		Messages: []ports.ModelMessage{{Role: ports.RoleUser, Parts: []ports.ModelPart{{Kind: ports.PartText, Text: "What was revenue?"}}}}}}
	ctx := context.Background()
	codeOf := func(err error) string {
		var e *airoute.Error
		if errors.As(err, &e) {
			return e.Code
		}
		return ""
	}

	// Off by default: nothing is sent.
	if res, err := router.Run(ctx, e.books.Org, ask); codeOf(err) != airoute.CodeSwitchOff || len(res.Attempts) != 0 || model.calls != 0 {
		t.Fatalf("while off: %v %+v", err, res.Attempts)
	}
	setting := func(as, match, value string) response {
		return e.call(http.MethodPatch, "/v1/organisation/settings/"+ai.Switch, steppedUp(e, as), map[string]any{"value": value}, "If-Match", match)
	}
	if g := e.call(http.MethodGet, "/v1/organisation/settings/"+ai.Switch, ahkow, nil); g.Body["off_at_once"] != true || g.str("value") != "false" {
		t.Fatalf("the switch: %s", g.Raw)
	}

	// On only with a second approver.
	asked := setting(ahkow, `"0"`, "true")
	if asked.Status != http.StatusAccepted {
		t.Fatalf("asking to switch it on: %d %s", asked.Status, asked.Raw)
	}
	if _, err := router.Run(ctx, e.books.Org, ask); codeOf(err) != airoute.CodeSwitchOff {
		t.Fatalf("before the approval: %v", err)
	}
	if d := f.decide(weiling, asked.str("id"), "approve", hashOf(asked)); d.Status != http.StatusOK {
		t.Fatalf("approving it: %d %s", d.Status, d.Raw)
	}
	if res, err := router.Run(ctx, e.books.Org, ask); err != nil || res.Response.Model != "gpt-6" {
		t.Fatalf("once on: %v %+v", err, res.Attempts)
	}

	// A stream running when it is switched off stops at its next check; off needs no one else.
	streamed := ask
	streamed.Stream = func(ports.ModelEvent) error { return nil }
	type outcome struct {
		res ai.Result
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := router.Run(ctx, e.books.Org, streamed)
		done <- outcome{res, err}
	}()
	<-model.started
	off := e.call(http.MethodPost, "/v1/ai/settings/switch-off", ownerOnly, nil)
	if sw, _ := off.Body["switch"].(map[string]any); off.Status != http.StatusOK || sw["value"] != "false" {
		t.Fatalf("switching it off: %d %s", off.Status, off.Raw)
	}
	select {
	case o := <-done:
		if codeOf(o.err) != airoute.CodeSwitchOff || len(o.res.Attempts) != 1 || o.res.Attempts[0].Outcome != airoute.Stopped {
			t.Errorf("the running stream: %v %+v", o.err, o.res.Attempts)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the stream did not stop")
	}
	if _, err := router.Run(ctx, e.books.Org, ask); codeOf(err) != airoute.CodeSwitchOff {
		t.Errorf("a new call once off: %v", err)
	}
	if again := e.call(http.MethodPost, "/v1/ai/settings/switch-off", ownerOnly, nil); again.Status != http.StatusOK {
		t.Errorf("switching it off again: %d %s", again.Status, again.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/ai/settings", ahkow, nil); g.Status != http.StatusOK || g.Body["switch"].(map[string]any)["value"] != "false" ||
		g.Body["budgets"].(map[string]any)["currency"] != "SGD" {
		t.Errorf("getAISettings: %d %s", g.Status, g.Raw)
	}
	// On again only with a second approver; then any member who may change settings switches it off at once, through
	// the setting itself.
	again := setting(ahkow, `"2"`, "true")
	if again.Status != http.StatusAccepted {
		t.Fatalf("asking again: %d %s", again.Status, again.Raw)
	}
	if d := f.decide(weiling, again.str("id"), "approve", hashOf(again)); d.Status != http.StatusOK {
		t.Fatalf("approving it again: %d %s", d.Status, d.Raw)
	}
	if r := setting(f.as(f.weiling, authz.Controller), `"3"`, "false"); r.Status != http.StatusOK || r.str("value") != "false" {
		t.Errorf("a Controller switching it off: %d %s", r.Status, r.Raw)
	}
	if _, err := router.Run(ctx, e.books.Org, ask); codeOf(err) != airoute.CodeSwitchOff {
		t.Errorf("after the Controller switched it off: %v", err)
	}
	// The stopped stream's reservation stays whole until the provider's usage reconciles it (AI-86).
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		calls, err := u.AICalls().Page(ctx, storage.AICallQuery{Agent: "chat", From: time.Unix(0, 0), To: time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC), Limit: 10})
		if err == nil && (len(calls) != 2 || calls[0].Outcome != airoute.Stopped) {
			t.Errorf("the log: %+v", calls)
		}
		return err
	})
}
