//go:build local

package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/ai"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/aibudget"
	"github.com/cognizhi/fyniq/internal/notify/reminders"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The AI pauses alone when its budget is spent (G4-085; NFR-COST-03, AI-13, AI-67): mid-month, with October's GST F5
// return due in three days and the organisation's AI budget spent, a question's model call is not sent and stops with
// AI-BUDGET-02, which the assistant's panel says as "The AI budget is spent for this month"; meanwhile the deadline's
// reminder goes out, a journal is prepared and posted, and an approval is asked for and decided, as on any day.
// req: G4-085
// req: NFR-COST-03
// req: AI-13
func TestTheAIPausesAloneWhenItsBudgetIsSpent(t *testing.T) {
	f := newFounders(t)
	e := f.e
	start, end := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		pr := o.Profile
		pr.FYStartMonth, pr.BooksStart, pr.FirstYearEnd = 1, &start, &end
		if _, err := u.UpdateOrganisation(ctx, o.Version, pr); err != nil {
			return err
		}
		_, err = u.GSTRegistrations().Create(ctx, storage.NewGSTRegistration{RegisteredFrom: porttest.Day(10, 1), GSTNumber: "M90000001X",
			ReturnFrequency: storage.ReturnMonthly, QuarterStartMonth: 1, AccountingBasis: storage.BasisInvoice, RecordedAt: e.clock.Now()})
		return err
	})
	aiSwitchedOn(e)
	sg := routable(e, "Paused primary", "SG", nil, "gpt-6")
	pricedInBase(e, sg, "gpt-6")
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AIRoutes().Save(ctx, storage.AIRoute{Agent: "chat", Version: 1, Classes: []string{"business-figures"}, MadeBy: "user:paused",
			MadeAt: e.clock.Now(), Steps: []storage.AIRouteStep{{ConnectionID: sg, Model: "gpt-6"}}})
		return err
	})
	model := &stepModel{caps: full}
	router := ai.Router{Store: e.api.deps.Store, Clock: e.clock, Outbox: e.api.deps.Outbox,
		Provider: func(context.Context, storage.UnitOfWork, storage.AIConnection) (ports.ModelProvider, error) {
			return model, nil
		}}

	// 27 November, three days before October's return is due; the organisation's budget all but spent.
	e.clock.Set(porttest.Day(11, 27).Add(9 * time.Hour))
	ahkow, weiling := f.as(f.ahkow, authz.Owner, authz.Controller, authz.Accountant), f.as(f.weiling, authz.Owner, authz.Controller, authz.Accountant)
	if r := e.call(http.MethodPut, "/v1/ai/budgets/organisation", weiling, map[string]any{"amount": "0.0001"}, "If-Match", `"0"`); r.Status != http.StatusOK {
		t.Fatalf("the organisation's budget: %d %s", r.Status, r.Raw)
	}
	_, err := router.Run(context.Background(), e.books.Org, ai.Call{Agent: "chat", Classes: []string{"business-figures"}, Actor: "user:" + f.ahkow,
		Request: ports.ModelRequest{MaxTokens: 1024, Messages: []ports.ModelMessage{{Role: ports.RoleUser, Parts: []ports.ModelPart{{Kind: ports.PartText,
			Text: "What was revenue?"}}}}}})
	var spent *aibudget.Error
	if !errors.As(err, &spent) || spent.Code != aibudget.CodeSpent || model.calls != 0 {
		t.Fatalf("a question with the budget spent: %v, %d calls sent", err, model.calls)
	}

	// The deadline's alert goes on: Checks says the return is due in three days, and its last reminder goes out the day
	// before.
	c := e.checked("gst.return-due")
	wantStatus(t, c, "due")
	if items := itemsOf(c); len(items) != 1 || !strings.Contains(items[0]["detail"].(string), "Due on 30 Nov 2026, in 3 day(s)") {
		t.Errorf("C.2 #13 three days before: %s", c.Raw)
	}
	worker, err := postgres.Open(context.Background(), db.URL("fyniq_worker"))
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	d := outbox.Dispatcher{Store: worker, Queue: local.NewMemory(), Clock: clock.NewFrozen(porttest.Day(11, 29).Add(time.Hour))}
	if n, err := reminders.DeadlinesDueIn(context.Background(), worker, d, e.books.Org); err != nil || n == 0 {
		t.Errorf("the reminder the day before October's return is due: %d, %v", n, err)
	}

	// A journal is prepared and posted.
	j := e.call(http.MethodPost, "/v1/journals", ahkow, e.draft("100.00"))
	if j.Status != http.StatusCreated {
		t.Fatalf("preparing a journal: %d %s", j.Status, j.Raw)
	}
	if p := e.call(http.MethodPost, "/v1/journals/"+j.str("id")+"/post", weiling, nil, "If-Match", j.Header.Get("ETag")); p.Status != http.StatusOK ||
		p.str("journal", "posted_at") == "" {
		t.Errorf("posting it: %d %s", p.Status, p.Raw)
	}

	// An approval is asked for and decided.
	r := e.call(http.MethodPatch, "/v1/organisation/settings/bank.tolerance_base", ahkow, map[string]any{"value": "5.00"}, "If-Match", `"0"`)
	if r.Status != http.StatusAccepted {
		t.Fatalf("asking for a change: %d %s", r.Status, r.Raw)
	}
	if a := f.decide(weiling, r.str("id"), "approve", hashOf(r)); a.Status != http.StatusOK || a.str("status") != "approved" {
		t.Errorf("approving it: %d %s", a.Status, a.Raw)
	}
}
