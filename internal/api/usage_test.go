//go:build local

package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The usage view (FR-ADM-05): the Owner, the Administrator and the DPO read what the organisation used by month and what is stored now; nobody else does, and the AI never; a month with
// no use is still listed, with zeros; and the view only reads.
// req: FR-ADM-05
// asvs: V8.2.2, V4.2.1
func TestTheUsageViewShowsEveryMonthAndOnlyToTheOnesWhoMaySeeIt(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	conn := routable(e, "Usage Singapore", "SG", []string{"gpt-6"})
	for _, at := range []time.Time{time.Date(2026, 10, 5, 2, 0, 0, 0, time.UTC), time.Date(2026, 10, 6, 2, 0, 0, 0, time.UTC), time.Date(2026, 9, 20, 2, 0, 0, 0, time.UTC)} {
		c := storage.AICall{Run: uuid.Must(uuid.NewV7()), Agent: "chat", RouteVersion: 1, Step: 1, ConnectionID: conn, ModelRequested: "gpt-6", Actor: "user:" + f.ahkow,
			Classes: []string{"business-figures"}, Outcome: "ok", ModelReported: "gpt-6", Input: 100, Output: 20, StartedAt: at}
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			_, err = u.AICalls().Record(ctx, c)
			return
		})
	}
	for _, role := range []authz.Role{authz.Owner, authz.Administrator, authz.DPO} {
		r := e.call(http.MethodGet, "/v1/usage?months=3", e.as(role), nil)
		months, _ := r.Body["months"].([]any)
		if r.Status != 200 || len(months) != 3 {
			t.Fatalf("%s reads the usage: %d %s", role, r.Status, r.Raw)
		}
		last := months[2].(map[string]any)
		if last["month"] != "2026-10" || last["ai_cost"] == nil {
			t.Fatalf("the current month is listed, with an empty cost list: %v", last)
		}
		if first := months[0].(map[string]any); first["ai_calls"] != float64(0) || first["month"] != "2026-08" {
			t.Fatalf("a month with no use is listed with zeros: %v", first)
		}
		if st, _ := r.Body["stored"].(map[string]any); st["total_bytes"] == nil {
			t.Fatalf("what is stored: %s", r.Raw)
		}
	}
	// Two calls in October and one in September are summed by the month they started in.
	r := e.call(http.MethodGet, "/v1/usage?months=3", e.as(authz.Owner), nil)
	months := r.Body["months"].([]any)
	oct, sep := months[2].(map[string]any), months[1].(map[string]any)
	if oct["ai_calls"] != float64(2) || oct["input_tokens"] != float64(200) || oct["output_tokens"] != float64(40) || sep["ai_calls"] != float64(1) || sep["input_tokens"] != float64(100) {
		t.Fatalf("the months: %v %v", sep, oct)
	}
	for _, role := range []authz.Role{authz.Accountant, authz.Controller, authz.Employee, authz.Bookkeeper} {
		e.problemIs(e.call(http.MethodGet, "/v1/usage", e.as(role), nil), "getUsage", 403, CodeForbidden)
	}
	if r := e.call(http.MethodGet, "/v1/usage?months=25", e.as(authz.Owner), nil); r.Status != 422 {
		t.Fatalf("25 months: %d", r.Status)
	}
}

// matrixUsage is the authorisation matrix's row of the usage view.
var matrixUsage = map[string]func(e *env, tok string) (response, func() bool){
	"getUsage": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/usage", tok, nil), nil
	},
}
