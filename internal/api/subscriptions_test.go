//go:build local

package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/subscription"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-AR-15, FR-KPI-02 through the API: a contract of an active customer with its dated amendments, and the schedule and contracted MRR derived from them.
// An upgrade raises and a downgrade lowers the value in force; a cancellation ends it and a renewal starts it again; amendments only go forward in
// time; the contract's ETag guards each; a month that is closed takes no amendment; the contracted MRR sums the active contracts by currency.
// req: FR-AR-15
// req: FR-KPI-02
// asvs: V2.3.1
func TestASubscriptionContractIsAmendedFromDatedDaysAndItsMRRIsDerived(t *testing.T) {
	e := newEnv(t)
	acc := e.as(authz.Accountant)
	customer := e.customer()
	post := func(body map[string]any) response {
		return e.call(http.MethodPost, "/v1/subscription-contracts", acc, body)
	}
	e.problemIs(post(map[string]any{"contact_id": uuid.NewString(), "name": "Growth", "currency": "SGD", "starts_on": "2026-01-15", "mrr": "100.00"}),
		"createSubscriptionContract", 422, subscription.CodeContact)
	e.problemIs(post(map[string]any{"contact_id": customer, "name": "Growth", "currency": "SGD", "starts_on": "2026-01-15", "ends_on": "2026-01-01", "mrr": "100.00"}),
		"createSubscriptionContract", 422, subscription.CodeTerms)
	made := post(map[string]any{"contact_id": customer, "name": "Growth plan", "currency": "SGD", "starts_on": "2026-01-15", "ends_on": "2026-12-31", "mrr": "100.00"})
	if made.Status != http.StatusCreated || made.str("status") != "active" || made.str("mrr", "amount") != "100.00" || made.Header.Get("ETag") != `"1"` {
		t.Fatalf("createSubscriptionContract = %d: %s", made.Status, made.Raw)
	}
	id := made.str("id")
	amend := func(tag string, body map[string]any) response {
		return e.call(http.MethodPost, "/v1/subscription-contracts/"+id+"/amendments", acc, body, "If-Match", tag)
	}
	if r := e.call(http.MethodPost, "/v1/subscription-contracts/"+id+"/amendments", acc, map[string]any{"kind": "upgrade", "effective_on": "2026-04-01", "mrr": "150.00", "reason": "x"}); r.Status != 428 {
		t.Errorf("an amendment without If-Match: %d %s", r.Status, r.Raw)
	}
	// A downgrade that raises, an upgrade that lowers, and an amendment before the start are refused.
	e.problemIs(amend(`"1"`, map[string]any{"kind": "downgrade", "effective_on": "2026-04-01", "mrr": "150.00", "reason": "Wrong way"}), "amendSubscriptionContract", 422, subscription.CodeAmount)
	e.problemIs(amend(`"1"`, map[string]any{"kind": "upgrade", "effective_on": "2026-01-01", "mrr": "150.00", "reason": "Too early"}), "amendSubscriptionContract", 422, subscription.CodeDate)
	e.problemIs(amend(`"1"`, map[string]any{"kind": "renew", "effective_on": "2026-04-01", "mrr": "150.00", "reason": "Not ended"}), "amendSubscriptionContract", 422, subscription.CodeKind)
	up := amend(`"1"`, map[string]any{"kind": "upgrade", "effective_on": "2026-04-01", "mrr": "150.00", "reason": "The customer moved to the larger plan"})
	if up.Status != http.StatusCreated || up.Header.Get("ETag") != `"2"` {
		t.Fatalf("the upgrade: %d %s", up.Status, up.Raw)
	}
	e.problemIs(amend(`"1"`, map[string]any{"kind": "downgrade", "effective_on": "2026-07-01", "mrr": "120.00", "reason": "Stale"}), "amendSubscriptionContract", 412, CodeStale)
	e.problemIs(amend(`"2"`, map[string]any{"kind": "downgrade", "effective_on": "2026-03-01", "mrr": "120.00", "reason": "Back in time"}), "amendSubscriptionContract", 422, subscription.CodeDate)
	if r := amend(`"2"`, map[string]any{"kind": "downgrade", "effective_on": "2026-07-01", "mrr": "120.00", "reason": "Smaller team"}); r.Status != http.StatusCreated {
		t.Fatalf("the downgrade: %d %s", r.Status, r.Raw)
	}
	if r := amend(`"3"`, map[string]any{"kind": "cancel", "effective_on": "2026-09-10", "reason": "The customer left"}); r.Status != http.StatusCreated {
		t.Fatalf("the cancellation: %d %s", r.Status, r.Raw)
	}
	e.problemIs(amend(`"4"`, map[string]any{"kind": "upgrade", "effective_on": "2026-10-01", "mrr": "200.00", "reason": "It ended"}), "amendSubscriptionContract", 422, subscription.CodeKind)
	ren := amend(`"4"`, map[string]any{"kind": "renew", "effective_on": "2026-11-01", "mrr": "130.00", "reason": "The customer came back"})
	if ren.Status != http.StatusCreated {
		t.Fatalf("the renewal: %d %s", ren.Status, ren.Raw)
	}
	// The schedule is the value in force on each month's last day (worked by hand: 100 x3, 150 x3, 120 x2, ended x2, 130 x2).
	got := e.call(http.MethodGet, "/v1/subscription-contracts/"+id, acc, nil)
	sched := got.Body["schedule"].([]any)
	want := []string{"100.00", "100.00", "100.00", "150.00", "150.00", "150.00", "120.00", "120.00", "0.00", "0.00", "130.00"}
	if got.Status != 200 || len(sched) < len(want) {
		t.Fatalf("getSubscriptionContract = %d: %s", got.Status, got.Raw)
	}
	for i, w := range want {
		if m := sched[i].(map[string]any)["mrr"].(map[string]any)["amount"]; m != w {
			t.Errorf("month %d: %v, want %s", i+1, m, w)
		}
	}
	if len(got.Body["amendments"].([]any)) != 4 {
		t.Errorf("its amendments: %s", got.Raw)
	}
	// Contracted MRR on a day sums the active contracts by currency.
	other := post(map[string]any{"contact_id": customer, "name": "Seats", "currency": "SGD", "starts_on": "2026-06-01", "mrr": "40.50"})
	if other.Status != http.StatusCreated {
		t.Fatalf("a second contract: %d %s", other.Status, other.Raw)
	}
	for _, x := range []struct{ on, mrr string }{{"2026-05-31", "150.00"}, {"2026-08-31", "160.50"}, {"2026-10-15", "40.50"}, {"2026-12-01", "170.50"}} {
		r := e.call(http.MethodGet, "/v1/subscription-mrr?as_at="+x.on, acc, nil)
		tot, _ := r.Body["totals"].([]any)
		if r.Status != 200 || len(tot) != 1 || tot[0].(map[string]any)["mrr"].(map[string]any)["amount"] != x.mrr {
			t.Errorf("contracted MRR on %s: %d %s, want %s", x.on, r.Status, r.Raw, x.mrr)
		}
	}
	list := e.call(http.MethodGet, "/v1/subscription-contracts?contact_id="+customer, acc, nil)
	if list.Status != 200 || len(list.Body["items"].([]any)) != 2 {
		t.Errorf("listSubscriptionContracts: %d %s", list.Status, list.Raw)
	}
	// A closed month takes no amendment.
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		periods, err := u.Periods().List(ctx)
		for _, p := range periods {
			if err == nil && p.StartsOn.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
				_, err = u.Periods().Close(ctx, p.ID)
			}
		}
		return err
	})
	e.problemIs(e.call(http.MethodPost, "/v1/subscription-contracts/"+other.str("id")+"/amendments", acc, map[string]any{"kind": "upgrade", "effective_on": "2026-10-20", "mrr": "99.00", "reason": "Closed"},
		"If-Match", `"1"`), "amendSubscriptionContract", 422, subscription.CodeDate)
}

// matrixSubscriptions are the authorisation matrix's rows for subscription contracts.
var matrixSubscriptions = map[string]func(e *env, tok string) (response, func() bool){
	"createSubscriptionContract": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/subscription-contracts", tok, map[string]any{"contact_id": uuid.NewString(), "name": "Plan", "currency": "SGD", "starts_on": "2026-01-15", "mrr": "10.00"}), nil
	},
	"listSubscriptionContracts": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/subscription-contracts", tok, nil), nil
	},
	"getSubscriptionContract": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/subscription-contracts/"+uuid.NewString(), tok, nil), nil
	},
	"amendSubscriptionContract": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/subscription-contracts/"+uuid.NewString()+"/amendments", tok, map[string]any{"kind": "cancel", "effective_on": "2026-10-30", "reason": "x"}, "If-Match", `"1"`), nil
	},
	"getContractedMRR": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/subscription-mrr", tok, nil), nil
	},
}
