//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A forecast's funding plan is taken through the round model over the register's cap table (FR-PLN-07; p2-reporting-planning task 6.2), worked by hand. Ah Kow holds
// 700,000 shares and Lion Rock 300,000: 1,000,000 fully diluted. The forecast plans S$1,000,000 of equity in June 2027 at a pre-money valuation of S$4,000,000
// (S$4.00 a share, 250,000 shares), then S$2,000,000 in March 2028 at S$10,000,000 (S$8.00 over 1,250,000, 250,000 more), a SAFE with no price, a loan and an equity row with
// no valuation. Ah Kow goes from 70.00% to 56.00% and then 46.67%, losing 14.00 points in the first; the new investors reach 20.00% and then 500,000 shares, 33.33%; the SAFE,
// the loan and the unpriced round are listed with why. Only a reader of the whole cap table sees it: the Accountant and the Board viewer are refused.
// req: FR-PLN-07
// req: FR-EQ-21
func TestAForecastsFundingPlanIsTakenThroughTheRoundModelOverTheCapTable(t *testing.T) {
	f := newFounders(t)
	e := f.e
	owner := f.as(f.weiling, authz.Owner, authz.Controller)
	start, end := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		pr := o.Profile
		pr.FYStartMonth, pr.BooksStart, pr.FirstYearEnd = 1, &start, &end
		_, err = u.UpdateOrganisation(ctx, o.Version, pr)
		return err
	})
	founder := e.directorContact("Ah Kow")
	lion := e.call(http.MethodPost, "/v1/contacts", owner, map[string]any{"legal_name": "Lion Rock Ventures Pte. Ltd.", "investor": true, "shareholder": true}).str("id")
	ordinary := e.shareCapitalIn(e.books.Org)
	for _, x := range []struct{ holder, shares string }{{founder, "700000"}, {lion, "300000"}} {
		if r := e.call(http.MethodPost, "/v1/share-issues", owner, map[string]any{"holder_id": x.holder, "class_account_id": ordinary, "shares": x.shares,
			"price": "0.01", "consideration": "cash", "resolution_refs": "Allotment resolution", "effective_on": "2025-01-02"}); r.Status != http.StatusCreated {
			t.Fatalf("the shares of %s: %d %s", x.holder, r.Status, r.Raw)
		}
	}
	plan := forecastPlan()
	plan["opex"] = []any{}
	plan["funding"] = []any{
		map[string]any{"type": "Equity", "month": "2028-03", "amount": "2000000.00", "pre_money": "10000000.00"},
		map[string]any{"type": "Equity", "month": "2027-06", "amount": "1000000.00", "pre_money": "4000000.00"},
		map[string]any{"type": "SAFE", "month": "2027-01", "amount": "100000.00"},
		map[string]any{"type": "Loan", "month": "2027-02", "amount": "50000.00", "rate": "0.05", "tenor": 24},
		map[string]any{"type": "Equity", "month": "2027-09", "amount": "300000.00"},
	}
	made := e.call(http.MethodPost, "/v1/forecasts", e.as(authz.Accountant), plan)
	if made.Status != http.StatusCreated {
		t.Fatalf("the forecast: %d %s", made.Status, made.Raw)
	}
	id := made.str("id")
	r := e.call(http.MethodGet, "/v1/forecasts/"+id+"/funding-plan", owner, nil)
	if r.Status != http.StatusOK || r.str("shares_fully_diluted") != "1000000" {
		t.Fatalf("the funding plan: %d %s", r.Status, r.Raw)
	}
	rounds := r.Body["rounds"].([]any)
	if len(rounds) != 3 {
		t.Fatalf("the rounds: %s", r.Raw)
	}
	holder := func(round any, name string) map[string]any {
		for _, h := range round.(map[string]any)["holders"].([]any) {
			if m := h.(map[string]any); m["holder_name"] == name {
				return m
			}
		}
		t.Fatalf("no %s in %v", name, round)
		return nil
	}
	first, unpriced, second := rounds[0].(map[string]any), rounds[1].(map[string]any), rounds[2].(map[string]any)
	if first["month"] != "2027-06" || first["modelled"] != true || first["new_shares"] != "250000" || first["price_per_share"].(map[string]any)["amount"] != "4.00" ||
		first["raised"].(map[string]any)["amount"] != "1000000.00" || first["post_money"].(map[string]any)["amount"] != "5000000.00" {
		t.Errorf("the first round: %v", first)
	}
	if k := holder(first, "Ah Kow"); k["before"] != "70.00" || k["after"] != "56.00" || k["points_lost"] != "14.00" || k["shares_after"] != "700000" {
		t.Errorf("Ah Kow after the first round: %v", k)
	}
	if n := holder(first, "New investors"); n["after"] != "20.00" {
		t.Errorf("the new investors after the first round: %v", n)
	}
	if unpriced["month"] != "2027-09" || unpriced["modelled"] != false || !strings.Contains(unpriced["why"].(string), "No pre-money valuation") {
		t.Errorf("the unpriced round: %v", unpriced)
	}
	if second["new_shares"] != "250000" || second["price_per_share"].(map[string]any)["amount"] != "8.00" {
		t.Errorf("the second round: %v", second)
	}
	if k := holder(second, "Ah Kow"); k["before"] != "56.00" || k["after"] != "46.67" {
		t.Errorf("Ah Kow after the second round: %v", k)
	}
	if n := holder(second, "New investors"); n["shares_after"] != "500000" || n["after"] != "33.33" {
		t.Errorf("the new investors after the second round: %v", n)
	}
	other := r.Body["other"].([]any)
	if len(other) != 2 || other[0].(map[string]any)["type"] != "SAFE" || other[1].(map[string]any)["type"] != "Loan" {
		t.Errorf("the rows that issue no shares: %v", other)
	}
	// Only a reader of the whole cap table; a forecast that is not one is not found.
	e.problemIs(e.call(http.MethodGet, "/v1/forecasts/"+id+"/funding-plan", e.as(authz.BoardViewer), nil), "getForecastFundingPlan", 403, "API-AUTHZ-01")
	e.problemIs(e.call(http.MethodGet, "/v1/forecasts/"+founder+"/funding-plan", owner, nil), "getForecastFundingPlan", 404, CodeNotFound)
	// A valuation belongs to an equity row only, and is above nothing.
	bad := forecastPlan()
	bad["opex"] = []any{}
	bad["funding"] = []any{map[string]any{"type": "Loan", "month": "2027-02", "amount": "50000.00", "rate": "0.05", "tenor": 24, "pre_money": "1000000.00"}}
	if x := e.call(http.MethodPost, "/v1/forecasts", e.as(authz.Accountant), bad); x.Status != http.StatusUnprocessableEntity || !strings.Contains(x.Raw, "pre_money") {
		t.Errorf("a loan with a valuation: %d %s", x.Status, x.Raw)
	}
	bad["funding"] = []any{map[string]any{"type": "Equity", "month": "2027-02", "amount": "50000.00", "pre_money": "0.00"}}
	if x := e.call(http.MethodPost, "/v1/forecasts", e.as(authz.Accountant), bad); x.Status != http.StatusUnprocessableEntity || !strings.Contains(x.Raw, "pre_money") {
		t.Errorf("a valuation of nothing: %d %s", x.Status, x.Raw)
	}
}
