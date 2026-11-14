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

// The AI activity log (AI-12, AUD-05; Appendix D rev 0.276): every attempt, newest first, a page at a time. Each member
// reads their own entries, and asking for another's is refused; the Owner and the DPO read everyone's. Someone else's
// entry that carried payroll shows masked, its detail withheld, to the DPO, who may not see payroll unmasked; the Owner
// sees it, and that read is audited. Filters narrow it by agent, person, run and days.
// req: AI-12
// req: AUD-05
// asvs: V8.2.2, V16.2.1
func TestTheAIActivityLog(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	conn := routable(e, "Activity Singapore", "SG", []string{"gpt-6"})
	base := time.Date(2099, 3, 4, 2, 0, 0, 0, time.UTC)
	record := func(actor, agent string, classes []string, outcome, detail string, at time.Time) uuid.UUID {
		var saved storage.AICall
		c := storage.AICall{Run: uuid.Must(uuid.NewV7()), Agent: agent, RouteVersion: 1, Step: 1, ConnectionID: conn, ModelRequested: "gpt-6",
			Actor: "user:" + actor, Classes: classes, Outcome: outcome, Detail: detail, StartedAt: at}
		if outcome == "ok" {
			c.ModelReported, c.Input, c.Output = "gpt-6", 100, 20
		}
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			saved, err = u.AICalls().Record(ctx, c)
			return err
		})
		return saved.ID
	}
	mine := record(f.ahkow, "chat", []string{"business-figures"}, "ok", "", base)
	pay := record(f.weiling, "chat", []string{"payroll"}, "skipped", "the payroll detail", base.Add(time.Minute))
	report := record(f.weiling, "report_commentary", []string{"business-figures"}, "ok", "", base.Add(2*time.Minute))
	list := func(as, query string) response {
		return e.call(http.MethodGet, "/v1/ai/activity"+query, as, nil)
	}
	ids := func(r response) []string {
		var out []string
		for _, x := range r.Body["items"].([]any) {
			out = append(out, x.(map[string]any)["id"].(string))
		}
		return out
	}
	entry := func(r response, id uuid.UUID) map[string]any {
		for _, x := range r.Body["items"].([]any) {
			if x.(map[string]any)["id"] == id.String() {
				return x.(map[string]any)
			}
		}
		t.Fatalf("no entry %s: %s", id, r.Raw)
		return nil
	}
	days := "?from=2099-03-04&to=2099-03-04"

	// A Bookkeeper reads their own.
	bookkeeper := f.as(f.ahkow, authz.Bookkeeper)
	if r := list(bookkeeper, days); r.Status != http.StatusOK || r.str("scope") != "own" || len(ids(r)) != 1 || ids(r)[0] != mine.String() ||
		entry(r, mine)["connection"] != "Activity Singapore" {
		t.Fatalf("a Bookkeeper's own: %d %s", r.Status, r.Raw)
	}
	e.problemIs(list(bookkeeper, "?person="+f.weiling), "listAIActivity", 403, CodeForbidden)

	// The DPO reads everyone's, with someone else's payroll entry masked, and no read of payroll is audited.
	audits := func() int {
		r := e.call(http.MethodGet, "/v1/audit/events?action=ai_activity.read", f.as(f.weiling, authz.Owner), nil)
		return len(r.Body["items"].([]any))
	}
	dpo := f.as(f.ahkow, authz.DPO)
	r := list(dpo, days)
	if r.Status != http.StatusOK || r.str("scope") != "all" || len(ids(r)) != 3 || ids(r)[0] != report.String() || ids(r)[2] != mine.String() {
		t.Fatalf("the DPO's: %d %s", r.Status, r.Raw)
	}
	if p := entry(r, pay); p["masked"] != true || p["detail"] != nil || p["outcome"] != "skipped" {
		t.Errorf("payroll to the DPO: %v", p)
	}
	if n := audits(); n != 0 {
		t.Errorf("%d reads audited before any payroll was shown", n)
	}

	// The Owner sees it, and that is audited; their own payroll entry is theirs to see, unaudited.
	r = list(f.as(f.ahkow, authz.Owner), days)
	if p := entry(r, pay); p["masked"] != false || p["detail"] != "the payroll detail" {
		t.Errorf("payroll to another Owner: %v", p)
	}
	if n := audits(); n != 1 {
		t.Errorf("the Owner's read of payroll: %d audited", n)
	}
	if p := entry(list(f.as(f.weiling, authz.DPO), days), pay); p["masked"] != false {
		t.Errorf("one's own payroll entry: %v", p)
	}
	if n := audits(); n != 1 {
		t.Errorf("reading one's own entry was audited: %d", n)
	}

	// Filters and pages.
	owner := f.as(f.weiling, authz.Owner)
	if r := list(owner, days+"&agent=report_commentary"); len(ids(r)) != 1 || ids(r)[0] != report.String() {
		t.Errorf("by agent: %s", r.Raw)
	}
	if r := list(owner, days+"&person="+f.ahkow); len(ids(r)) != 1 || ids(r)[0] != mine.String() {
		t.Errorf("by person: %s", r.Raw)
	}
	if r := list(owner, "?run="+entry(list(owner, days), pay)["run"].(string)); len(ids(r)) != 1 || ids(r)[0] != pay.String() {
		t.Errorf("by run: %s", r.Raw)
	}
	if r := list(owner, "?from=2099-03-05"); len(ids(r)) != 0 {
		t.Errorf("a later day: %s", r.Raw)
	}
	first := list(owner, days+"&page_size=2")
	next := first.str("next_cursor")
	second := list(owner, days+"&page_size=2&cursor="+next)
	if len(ids(first)) != 2 || next == "" || len(ids(second)) != 1 || ids(second)[0] != mine.String() || second.str("next_cursor") != "" {
		t.Errorf("pages: %s then %s", first.Raw, second.Raw)
	}
	e.problemIs(list(owner, "?person=someone"), "listAIActivity", 422, CodeInvalid)
	e.problemIs(list(owner, "?from=14-11-2099"), "listAIActivity", 422, CodeInvalid)
	e.problemIs(list(owner, "?cursor=bm90IGEgY3Vyc29y"), "listAIActivity", 422, CodeCursor)
}
