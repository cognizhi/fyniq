//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The schedules report through the API (FR-GL-03, INV-07): a bill of 1,200.00 + 108.00 GST, not claimable, spread over
// three months from September, is one prepayment schedule of 1,308.00 at 436.00 a month. At 31 October September's and
// October's months are posted, 872.00, November's is projected, and 1210 holds the 436.00 left, so it ties. At 30
// November, with the job not run, November's waits. Before the bill there is no schedule, and 1210 holds nothing.
// req: FR-GL-03
// req: INV-07
// req: ENG-05
func TestSchedulesThroughTheAPI(t *testing.T) {
	e := newEnv(t)
	e.supplier()
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "1210", Name: "Prepayments", Type: storage.Asset, Role: storage.AccountRole(ledger.RolePrepayments)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	d := e.billDraft("1200.00", "108.00")
	d["lines"].([]map[string]any)[0]["spread_months"], d["lines"].([]map[string]any)[0]["spread_from"] = 3, "2026-09-20"
	r := e.call(http.MethodPost, "/v1/bills", e.as(authz.Accountant), d)
	if r.Status != 201 {
		t.Fatalf("saving a spread bill: %d %s", r.Status, r.Raw)
	}
	id := r.str("id")
	if p := e.call(http.MethodPost, "/v1/bills/"+id+"/post", e.as(authz.Accountant), nil, "If-Match", r.Header.Get("ETag")); p.Status != 200 {
		t.Fatalf("posting: %d %s", p.Status, p.Raw)
	}
	months := func(r response) string {
		var out []string
		for _, x := range r.Body["schedules"].([]any)[0].(map[string]any)["months"].([]any) {
			m := x.(map[string]any)
			out = append(out, m["month"].(string)+" "+m["amount"].(map[string]any)["amount"].(string)+" "+m["status"].(string))
		}
		return strings.Join(out, "; ")
	}
	amount := func(m map[string]any, field string) string { return m[field].(map[string]any)["amount"].(string) }

	g := e.call(http.MethodGet, "/v1/reports/schedules?as_at=2026-10-31", e.as(authz.Auditor), nil)
	if g.Status != 200 || len(g.Body["schedules"].([]any)) != 1 || len(g.Body["holdings"].([]any)) != 1 {
		t.Fatalf("31 October: %d %s", g.Status, g.Raw)
	}
	sc := g.Body["schedules"].([]any)[0].(map[string]any)
	if sc["kind"] != "prepayment" || sc["source_type"] != "bill" || sc["source_id"] != id || sc["line"] != float64(1) || amount(sc, "total") != "1308.00" ||
		amount(sc, "recognised") != "872.00" || amount(sc, "remaining") != "436.00" || sc["months_posted"] != float64(2) {
		t.Errorf("the schedule at 31 October: %s", g.Raw)
	}
	if got := months(g); got != "2026-09 436.00 posted; 2026-10 436.00 posted; 2026-11 436.00 projected" {
		t.Errorf("its months at 31 October: %s", got)
	}
	h := g.Body["holdings"].([]any)[0].(map[string]any)
	if h["kind"] != "prepayment" || h["account_code"] != "1210" || h["status"] != "agrees" || amount(h, "balance") != "436.00" || amount(h, "schedules") != "436.00" ||
		amount(h, "direct") != "0.00" || amount(h, "unexplained") != "0.00" || h["open"] != float64(1) {
		t.Errorf("1210 at 31 October: %s", g.Raw)
	}

	if g = e.call(http.MethodGet, "/v1/reports/schedules?as_at=2026-11-30&status=all", e.as(authz.Accountant), nil); months(g) !=
		"2026-09 436.00 posted; 2026-10 436.00 posted; 2026-11 436.00 waiting" {
		t.Errorf("its months at 30 November: %s", months(g))
	}
	g = e.call(http.MethodGet, "/v1/reports/schedules?as_at=2026-09-30", e.as(authz.Accountant), nil)
	if len(g.Body["schedules"].([]any)) != 0 || g.Body["holdings"].([]any)[0].(map[string]any)["open"] != float64(0) {
		t.Errorf("30 September, before the bill: %s", g.Raw)
	}
	if bad := e.call(http.MethodGet, "/v1/reports/schedules?as_at=31-10-2026", e.as(authz.Accountant), nil); bad.Status == 200 || !strings.Contains(bad.Raw, "as_at") {
		t.Errorf("a day that is not a date: %d %s", bad.Status, bad.Raw)
	}
}
