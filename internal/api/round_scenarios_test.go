//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Round modelling (FR-EQ-21, design D8; p1-equity-register slice 6), G.3's round: 1,000,000 founder shares, the SAFE of
// S$125,000 with a S$2,500,000 pre-money cap and a 20% discount, a round at S$3.00 with Seedling's S$600,000. The sandbox
// answers with the outcome and saves nothing: the SAFE's 52,083 shares at S$2.40, Seedling's 200,000, the founder 79.87%
// after, diluted by 20.13 points, S$3,756,249.00 post-money. Saved, it is modelled again on read, by price or by a
// pre-money valuation of S$3,000,000; approving it, with its resolutions named and a top-up of 100,000 after the money,
// drafts Seedling's issue for cash and the pool's resolution. Nothing posts. An Owner, who may not draft, tries the round
// in the sandbox of modelRound: the founder Ah Kow signs in as is marked you, the round raises S$600,000.00, and a top-up of
// 200,000 before the money leaves the pool 13.89% of every share fully diluted after it. An Employee reads none of it.
// req: FR-EQ-21
// req: FM-1
// asvs: V8.2.1
func TestARoundIsModelledAndApproved(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	weiling := c.as(c.weiling, authz.Owner, authz.Controller)
	prep := c.as(c.weiling, authz.Accountant)
	mei := c.as(c.mei, authz.Owner)
	approve := func(req response, token string) response {
		return e.call(http.MethodPost, "/v1/approvals/"+req.str("id")+"/approve", token, hashOf(req), "If-Match", `"1"`)
	}
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
	ordinary := e.shareCapitalIn(e.books.Org)
	if r := e.call(http.MethodPost, "/v1/share-issues", weiling, map[string]any{"holder_id": founder, "class_account_id": ordinary, "shares": "1000000",
		"price": "0.01", "consideration": "cash", "resolution_refs": "Allotment resolution", "effective_on": "2025-01-02"}); r.Status != http.StatusCreated {
		t.Fatalf("the founder's shares: %d %s", r.Status, r.Raw)
	}
	hatch := e.call(http.MethodPost, "/v1/contacts", weiling, map[string]any{"legal_name": "Hatch Capital Pte. Ltd.", "investor": true}).str("id")
	fund := e.call(http.MethodPost, "/v1/contacts", weiling, map[string]any{"legal_name": "Seedling Ventures Pte. Ltd.", "investor": true}).str("id")
	customer := e.call(http.MethodPost, "/v1/contacts", weiling, map[string]any{"legal_name": "Northwind Analytics", "customer": true}).str("id")
	safe := e.call(http.MethodPost, "/v1/instruments", prep, map[string]any{"form": "pre_money_safe", "holder_id": hatch, "amount": "125000.00",
		"signed_on": "2026-06-18", "valuation_cap": "2500000", "discount": "20", "trigger": "priced_round", "class_account_id": ordinary,
		"denominator": "pre_money_shares", "price_decimals": 4})
	if f := e.upload("/v1/instruments/"+safe.str("id")+"/evidence", prep, map[string]string{"evidence_type": "Signed SAFE"}, "safe.pdf", []byte("%PDF-1.7 safe"),
		"If-Match", safe.Header.Get("ETag")); f.Status != http.StatusCreated {
		t.Fatalf("the SAFE's copy: %d %s", f.Status, f.Raw)
	}
	if a := approve(e.call(http.MethodPost, "/v1/instruments/"+safe.str("id")+"/approval", prep, nil, "If-Match", safe.Header.Get("ETag")), mei); a.Status != http.StatusOK {
		t.Fatalf("recording the SAFE: %d %s", a.Status, a.Raw)
	}
	model := map[string]any{"name": "G.3 at S$3.00", "on": "2026-10-15", "price": "3.00", "price_decimals": 4, "class_account_id": ordinary,
		"investors": []map[string]any{{"holder_id": fund, "amount": "600000.00"}}, "instruments": []string{safe.str("id")}}

	sandbox := e.call(http.MethodPost, "/v1/round-scenarios", prep, model, "Prefer", "return=preview")
	if sandbox.Status != http.StatusOK || sandbox.Header.Get("Preference-Applied") != "return=preview" {
		t.Fatalf("the sandbox: %d %s", sandbox.Status, sandbox.Raw)
	}
	for _, want := range []string{`"price":"3.00"`, `"shares":"52083"`, `"price":"2.40"`, `"shares":"200000"`, `"post_money":{"amount":"3756249.00","currency":"SGD"}`,
		`"pre_money":{"amount":"3000000.00","currency":"SGD"}`, `"holder_name":"Ah Kow","issued_before":"1000000","issued_after":"1000000"`, `"ownership_after":"79.87"`,
		`"dilution":"20.13"`, `"ownership_after":"15.97"`, `"ownership_after":"4.16"`, `"new_shares":"252083"`} {
		if !strings.Contains(sandbox.Raw, want) {
			t.Errorf("the sandbox has no %s: %s", want, sandbox.Raw)
		}
	}
	if l := e.call(http.MethodGet, "/v1/round-scenarios", weiling, nil); len(l.Body["items"].([]any)) != 0 {
		t.Errorf("the sandbox saved a scenario: %s", l.Raw)
	}

	kow := c.as(c.ahkow, authz.Owner)
	ct := e.call(http.MethodGet, "/v1/contacts/"+founder, weiling, nil)
	if r := approve(e.call(http.MethodPut, "/v1/contacts/"+founder+"/member", weiling, map[string]any{"member_user_id": c.ahkow}, "If-Match", ct.Header.Get("ETag")), mei); r.Status != http.StatusOK {
		t.Fatalf("linking the founder to Ah Kow's sign-in: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/round-scenarios", kow, model, "Prefer", "return=preview"), "createRoundScenario", 403, CodeForbidden)
	tried := e.call(http.MethodPost, "/v1/round-models", kow, model)
	if tried.Status != http.StatusOK || !strings.Contains(tried.Raw, `"holder_name":"Ah Kow"`) || !strings.Contains(tried.Raw, `"pro_rata":"252083","you":true`) ||
		!strings.Contains(tried.Raw, `"raised":{"amount":"600000.00","currency":"SGD"}`) || strings.Contains(tried.Raw, `"pool_diluted_after"`) ||
		strings.Count(tried.Raw, `"you":true`) != 1 {
		t.Errorf("an Owner trying the round: %d %s", tried.Status, tried.Raw)
	}
	topped := map[string]any{}
	for k, v := range model {
		topped[k] = v
	}
	delete(topped, "price")
	topped["pre_money"], topped["top_up"], topped["top_up_before"], topped["instruments"] = "3000000", "200000", true, []string{}
	if r := e.call(http.MethodPost, "/v1/round-models", kow, topped); r.Status != http.StatusOK || !strings.Contains(r.Raw, `"price":"2.50"`) ||
		!strings.Contains(r.Raw, `"pool_diluted_after":"13.89","pool_dilution":"-13.89"`) || !strings.Contains(r.Raw, `"unallocated_after":"200000"`) {
		t.Errorf("trying a top-up before the money: %d %s", r.Status, r.Raw)
	}
	bad0 := map[string]any{}
	for k, v := range model {
		bad0[k] = v
	}
	bad0["investors"] = []map[string]any{{"holder_id": customer, "amount": "600000.00"}}
	if r := e.call(http.MethodPost, "/v1/round-models", kow, bad0); r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Raw, `"field":"body.investors[0].holder_id"`) {
		t.Errorf("trying a round with a customer as its investor: %d %s", r.Status, r.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/round-scenarios", weiling, nil); len(l.Body["items"].([]any)) != 0 {
		t.Errorf("trying a round saved a scenario: %s", l.Raw)
	}

	bad := map[string]any{}
	for k, v := range model {
		bad[k] = v
	}
	bad["pre_money"], bad["investors"] = "3000000", []map[string]any{{"holder_id": customer, "amount": "0"}}
	if r := e.call(http.MethodPost, "/v1/round-scenarios", prep, bad); r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Raw, equity.CodeRound) ||
		!strings.Contains(r.Raw, `"field":"body.price"`) || !strings.Contains(r.Raw, `"field":"body.investors[0].amount"`) ||
		!strings.Contains(r.Raw, `"field":"body.investors[0].holder_id"`) {
		t.Errorf("a scenario that does not fit: %d %s", r.Status, r.Raw)
	}

	saved := e.call(http.MethodPost, "/v1/round-scenarios", prep, model)
	if saved.Status != http.StatusCreated || saved.str("status") != "draft" || saved.Body["outcome"] == nil {
		t.Fatalf("the scenario saved: %d %s", saved.Status, saved.Raw)
	}
	path := "/v1/round-scenarios/" + saved.str("id")
	byValue := map[string]any{}
	for k, v := range model {
		byValue[k] = v
	}
	delete(byValue, "price")
	byValue["pre_money"], byValue["top_up"] = "3000000", "100000"
	changed := e.call(http.MethodPut, path, prep, byValue, "If-Match", saved.Header.Get("ETag"))
	if changed.Status != http.StatusOK || !strings.Contains(changed.Raw, `"price":"3.00"`) || !strings.Contains(changed.Raw, `"unallocated_after":"100000"`) ||
		!strings.Contains(changed.Raw, `"fully_diluted_after":"1352083"`) {
		t.Fatalf("modelled by a pre-money valuation with a top-up after the money: %d %s", changed.Status, changed.Raw)
	}
	if r := e.call(http.MethodPost, path+"/approval", prep, nil, "If-Match", changed.Header.Get("ETag")); r.Status != http.StatusUnprocessableEntity ||
		!strings.Contains(r.Raw, `"field":"body.resolution_refs"`) {
		t.Errorf("asking with no resolutions named: %d %s", r.Status, r.Raw)
	}
	byValue["resolution_refs"] = "Board resolution 15 Oct 2026; shareholders' resolution 15 Oct 2026"
	ready := e.call(http.MethodPut, path, prep, byValue, "If-Match", changed.Header.Get("ETag"))
	req := e.call(http.MethodPost, path+"/approval", prep, nil, "If-Match", ready.Header.Get("ETag"))
	if req.Status != http.StatusAccepted || req.str("object_type") != approval.ObjectRoundScenario || !strings.Contains(req.Raw, "1 issues for cash and a pool resolution") {
		t.Fatalf("asking for approval: %d %s", req.Status, req.Raw)
	}
	if a := approve(req, mei); a.Status != http.StatusOK {
		t.Fatalf("approving the scenario: %d %s", a.Status, a.Raw)
	}
	if got := e.call(http.MethodGet, path, weiling, nil); got.str("status") != "approved" || got.Body["approved_by"] == nil {
		t.Errorf("the scenario once approved: %s", got.Raw)
	}
	e.problemIs(e.call(http.MethodPut, path, prep, byValue, "If-Match", `"2-posted"`), "replaceRoundScenario", 409, equity.CodeRecorded)
	txs := e.call(http.MethodGet, "/v1/equity-transactions?kind=issue_cash", weiling, nil)
	if items := txs.Body["items"].([]any); len(items) != 1 || !strings.Contains(txs.Raw, `"shares":"200000"`) || !strings.Contains(txs.Raw, `"price":"3.00"`) ||
		!strings.Contains(txs.Raw, "From the round scenario G.3 at S$3.00") || !strings.Contains(txs.Raw, `"status":"draft"`) {
		t.Errorf("the issue drafted: %s", txs.Raw)
	}
	if p := e.call(http.MethodGet, "/v1/option-pool", weiling, nil); !strings.Contains(p.Raw, `"size":"100000"`) || !strings.Contains(p.Raw, `"status":"draft"`) {
		t.Errorf("the pool's resolution drafted: %s", p.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/round-scenarios", e.as(authz.Employee), nil), "listRoundScenarios", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodPost, "/v1/round-models", e.as(authz.Employee), model), "modelRound", 403, CodeForbidden)
}

// scenarioFixture is the matrix's round scenario: a draft at S$1.00 a share for one investor.
var matrixScenarioOf = map[*env]string{}

func matrixScenario(e *env) string {
	e.t.Helper()
	if id, ok := matrixScenarioOf[e]; ok {
		return id
	}
	acc := e.as(authz.Accountant)
	var capital string
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.Accounts().List(ctx)
		for _, a := range all {
			if a.Role == storage.RoleShareCapital && a.Active && !a.Group {
				capital = a.ID.String()
			}
		}
		return err
	})
	if capital == "" {
		capital = e.shareCapitalIn(e.books.Org)
	}
	investor := e.call(http.MethodPost, "/v1/contacts", acc, map[string]any{"legal_name": "Matrix round investor", "investor": true}).str("id")
	r := e.call(http.MethodPost, "/v1/round-scenarios", acc, scenarioBody(capital, investor))
	if r.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's round scenario: %d %s", r.Status, r.Raw)
	}
	matrixScenarioOf[e] = r.str("id")
	return r.str("id")
}

func scenarioBody(capital, investor string) map[string]any {
	return map[string]any{"name": "Matrix round", "on": "2026-10-15", "price": "1.00", "price_decimals": 4, "class_account_id": capital,
		"investors": []map[string]any{{"holder_id": investor, "amount": "100.00"}}, "instruments": []string{},
		"resolution_refs": "Matrix resolution"}
}

func matrixScenarioState(e *env, id string) string {
	acc := e.as(authz.Accountant)
	var out []string
	for _, p := range []string{"/v1/round-scenarios/" + id, "/v1/round-scenarios", "/v1/equity-transactions"} {
		r := e.call(http.MethodGet, p, acc, nil)
		out = append(out, r.Header.Get("ETag")+r.Raw)
	}
	return strings.Join(out, "|")
}

// matrixRoundScenarios are the authorisation matrix's rows for round scenarios (FR-EQ-21).
var matrixRoundScenarios = map[string]func(e *env, tok string) (response, func() bool){
	"listRoundScenarios": func(e *env, tok string) (response, func() bool) {
		matrixScenario(e)
		return e.call(http.MethodGet, "/v1/round-scenarios", tok, nil), nil
	},
	"getRoundScenario": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/round-scenarios/"+matrixScenario(e), tok, nil), nil
	},
	"createRoundScenario": func(e *env, tok string) (response, func() bool) {
		id := matrixScenario(e)
		was := matrixScenarioState(e, id)
		cur := e.call(http.MethodGet, "/v1/round-scenarios/"+id, e.as(authz.Accountant), nil)
		body := scenarioBody(cur.str("class_account_id"), cur.Body["investors"].([]any)[0].(map[string]any)["holder_id"].(string))
		r := e.call(http.MethodPost, "/v1/round-scenarios", tok, body, "Prefer", "return=preview")
		return r, func() bool { return matrixScenarioState(e, id) == was }
	},
	"modelRound": func(e *env, tok string) (response, func() bool) {
		id := matrixScenario(e)
		was := matrixScenarioState(e, id)
		cur := e.call(http.MethodGet, "/v1/round-scenarios/"+id, e.as(authz.Accountant), nil)
		body := scenarioBody(cur.str("class_account_id"), cur.Body["investors"].([]any)[0].(map[string]any)["holder_id"].(string))
		r := e.call(http.MethodPost, "/v1/round-models", tok, body)
		return r, func() bool { return matrixScenarioState(e, id) == was }
	},
	"replaceRoundScenario": func(e *env, tok string) (response, func() bool) {
		id := matrixScenario(e)
		was := matrixScenarioState(e, id)
		cur := e.call(http.MethodGet, "/v1/round-scenarios/"+id, e.as(authz.Accountant), nil)
		body := scenarioBody(cur.str("class_account_id"), cur.Body["investors"].([]any)[0].(map[string]any)["holder_id"].(string))
		r := e.call(http.MethodPut, "/v1/round-scenarios/"+id, tok, body, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixScenarioState(e, id) == was }
	},
	"requestRoundScenarioApproval": func(e *env, tok string) (response, func() bool) {
		id := matrixScenario(e)
		was := matrixScenarioState(e, id)
		r := e.call(http.MethodPost, "/v1/round-scenarios/"+id+"/approval", steppedUp(e, tok), nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixScenarioState(e, id) == was }
	},
}
