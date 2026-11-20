//go:build local

package api

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/groupconsol"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// matrixConsolidation are the authorisation matrix's rows of the consolidation operations.
var matrixConsolidation = map[string]func(e *env, tok string) (response, func() bool){
	"listGroupMappings": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/group/mappings", tok, nil), nil
	},
	"mapGroupAccount": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/group/mappings", steppedUp(e, tok), map[string]any{"account_code": "1000", "group_account_id": uuid.NewString()}, "Idempotency-Key", uuid.NewString()), nil
	},
	"listGroupRates": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/group/rates", tok, nil), nil
	},
	"addGroupRate": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/group/rates", steppedUp(e, tok), map[string]any{"currency": "MYR", "month": "2026-09", "closing": "0.3056", "average": "0.3058", "note": "Matrix"}, "Idempotency-Key", uuid.NewString()), nil
	},
	"getGroupPairs": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/group/pairs", tok, nil), nil
	},
	"setGroupPairs": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPut, "/v1/group/pairs", steppedUp(e, tok), map[string]any{"pairs": []any{}}, "If-Match", `"0"`, "Idempotency-Key", uuid.NewString()), nil
	},
	"listGroupEntityAccounts": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/group/entities/"+uuid.NewString()+"/accounts", steppedUp(e, tok), nil), nil
	},
	"runConsolidation": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/group/consolidations", steppedUp(e, tok), map[string]any{"from": "2026-10", "to": "2026-10"}, "Idempotency-Key", uuid.NewString(), "Prefer", "return=preview"), nil
	},
	"listConsolidations": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/group/consolidations", tok, nil), nil
	},
	"getConsolidation": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/group/consolidations/"+uuid.NewString(), tok, nil), nil
	},
}

func init() {
	// The matrix's organisation has no group, no account to map and no run: an allowed role is answered by that, past the policy.
	pastPolicy["mapGroupAccount"] = CodeNotFound
	pastPolicy["listGroupEntityAccounts"] = CodeNotFound
	pastPolicy["getConsolidation"] = CodeNotFound
	pastPolicy["setGroupPairs"] = CodeStale // the first allowed role sets the pairs; the next one holds a stale version
}

// linkedGroup makes a parent and an entity, links them through the API and returns each side's founders.
func linkedGroup(t *testing.T) (parent, entity founders, owner, entityOwner, row string) {
	t.Helper()
	parent, entity = newFounders(t), newFounders(t)
	owner, entityOwner = parent.as(parent.ahkow), entity.as(entity.ahkow)
	made := parent.e.call(http.MethodPost, "/v1/group/entities", owner, map[string]any{"label": "Subsidiary"})
	if made.Status != http.StatusCreated {
		t.Fatalf("inviting: %d %s", made.Status, made.Raw)
	}
	if acc := entity.e.call(http.MethodPost, "/v1/group/accept", entityOwner, map[string]any{"code": made.str("code")}); acc.Status != http.StatusCreated {
		t.Fatalf("accepting: %d %s", acc.Status, acc.Raw)
	}
	return parent, entity, owner, entityOwner, made.str("id")
}

// A parent consolidates its entity (FR-ENT-05, FR-ENT-06, FR-RPT-12): the run refuses an account with figures and no mapping and names it; the entity's accounts are listed through the link
// with the parent's account of the same code suggested; once mapped, the run translates (none here: one currency), adds the entity's figures to the parent's and freezes them as a provisional run
// (the months are open); a role that may only view reads the run but does not run one; another organisation reads none of it; and after the entity ends the link the run stays, marked as ended.
// req: FR-ENT-05
// req: FR-ENT-06
// req: FR-RPT-12
// asvs: V8.2.1, V8.3.1, V4.2.1
func TestAParentConsolidatesItsEntityAndTheRunStaysFrozen(t *testing.T) {
	parent, entity, owner, entityOwner, row := linkedGroup(t)
	pe, ee := parent.e, entity.e
	pe.postedJournal("100.00")
	ee.postedJournal("250.00")
	// Nothing is mapped: the run names the entity's accounts.
	r := pe.call(http.MethodPost, "/v1/group/consolidations", owner, map[string]any{"from": "2026-10", "to": "2026-10"})
	if r.Status != 422 || r.problemCode() != groupconsol.CodeUnmapped {
		t.Fatalf("a run with nothing mapped: %d %s", r.Status, r.Raw)
	}
	// The accountant of the parent runs and maps; the entity's accounts are read through the link.
	accountant := parent.as(parent.ahkow, authz.Accountant)
	accounts := pe.call(http.MethodGet, "/v1/group/entities/"+row+"/accounts", accountant, nil)
	items, _ := accounts.Body["items"].([]any)
	if accounts.Status != 200 || len(items) != 2 {
		t.Fatalf("the entity's accounts: %d %s", accounts.Status, accounts.Raw)
	}
	for _, it := range items {
		x := it.(map[string]any)
		if x["suggested_group_account_id"] == nil || x["has_figures"] != true {
			t.Errorf("an account of the entity: %v", x)
		}
		if m := pe.call(http.MethodPost, "/v1/group/mappings", accountant, map[string]any{"entity_id": row, "account_code": x["code"], "group_account_id": x["suggested_group_account_id"]}, "Idempotency-Key", uuid.NewString()); m.Status != 200 {
			t.Fatalf("mapping %v: %d %s", x["code"], m.Status, m.Raw)
		}
	}
	// A role with a view alone reads and runs nothing.
	if r := pe.call(http.MethodPost, "/v1/group/consolidations", parent.as(parent.ahkow, authz.Administrator), map[string]any{"from": "2026-10", "to": "2026-10"}); r.Status != http.StatusForbidden {
		t.Errorf("an Administrator runs: %d %s", r.Status, r.Raw)
	}
	run := pe.call(http.MethodPost, "/v1/group/consolidations", accountant, map[string]any{"from": "2026-10", "to": "2026-10"}, "Idempotency-Key", uuid.NewString())
	if run.Status != http.StatusCreated || run.Body["provisional"] != true || run.str("currency") != "SGD" {
		t.Fatalf("the run: %d %s", run.Status, run.Raw)
	}
	got := map[string]string{}
	for _, c := range run.Body["consolidated"].([]any) {
		x := c.(map[string]any)
		got[x["account"].(string)+"|"+x["section"].(string)] = x["amount"].(string)
	}
	if got["1000|balance"] != "-350.00" || got["6100|result"] != "350.00" || run.str("translation_reserve") != "0.00" || len(run.Body["sources"].([]any)) != 2 {
		t.Errorf("the figures: %v reserve %s", got, run.str("translation_reserve"))
	}
	// Read back, listed, and the entity's side of the link has two events: the read of the accounts and of the run.
	again := pe.call(http.MethodGet, "/v1/group/consolidations/"+run.str("id"), parent.as(parent.ahkow, authz.Auditor), nil)
	if again.Status != 200 || again.str("id") != run.str("id") {
		t.Errorf("reading the run: %d %s", again.Status, again.Raw)
	}
	if list := pe.call(http.MethodGet, "/v1/group/consolidations", owner, nil); list.Status != 200 || len(list.Body["items"].([]any)) != 1 {
		t.Errorf("the runs: %s", list.Raw)
	}
	if other := ee.call(http.MethodGet, "/v1/group/consolidations/"+run.str("id"), entityOwner, nil); other.Status != http.StatusNotFound {
		t.Errorf("the entity reads the parent's run: %d %s", other.Status, other.Raw)
	}
	if other := ee.call(http.MethodGet, "/v1/group/consolidations", entityOwner, nil); other.Status != 200 || len(other.Body["items"].([]any)) != 0 {
		t.Errorf("the entity lists the parent's runs: %s", other.Raw)
	}
	// Rates: a currency other than the group's, once and corrected; none for the group's own.
	if bad := pe.call(http.MethodPost, "/v1/group/rates", accountant, map[string]any{"currency": "SGD", "month": "2026-09", "closing": "1", "average": "1", "note": "x"}); bad.Status != 422 {
		t.Errorf("a rate for the group's own currency: %d %s", bad.Status, bad.Raw)
	}
	for _, closing := range []string{"0.3056", "0.3060"} {
		if r := pe.call(http.MethodPost, "/v1/group/rates", accountant, map[string]any{"currency": "MYR", "month": "2026-09", "closing": closing, "average": "0.3058", "note": "MAS daily rates"}, "Idempotency-Key", uuid.NewString()); r.Status != http.StatusCreated {
			t.Fatalf("a rate: %d %s", r.Status, r.Raw)
		}
	}
	if rates := pe.call(http.MethodGet, "/v1/group/rates", owner, nil); len(rates.Body["items"].([]any)) != 2 {
		t.Errorf("both entries are kept: %s", rates.Raw)
	}
	// Pairs: set over the version read; a stale version is refused.
	if p := pe.call(http.MethodPut, "/v1/group/pairs", accountant, map[string]any{"pairs": []any{}}, "If-Match", `"0"`); p.Status != 200 {
		t.Errorf("the first pairs: %d %s", p.Status, p.Raw)
	}
	if p := pe.call(http.MethodPut, "/v1/group/pairs", accountant, map[string]any{"pairs": []any{}}, "If-Match", `"0"`); p.Status != http.StatusPreconditionFailed && p.Status != 409 {
		t.Errorf("a stale pairs version: %d %s", p.Status, p.Raw)
	}
	// The entity leaves: the run stays and says so; a new run cannot read it.
	if e := ee.call(http.MethodPost, "/v1/group/end", entityOwner, map[string]any{"organisation_id": pe.books.Org.String(), "reason": "Left"}); e.Status != 200 {
		t.Fatal(e.Raw)
	}
	frozen := pe.call(http.MethodGet, "/v1/group/consolidations/"+run.str("id"), owner, nil)
	ended := false
	for _, s := range frozen.Body["sources"].([]any) {
		if x := s.(map[string]any); x["key"] == row && x["link_ended"] == true {
			ended = true
		}
	}
	if !ended || len(frozen.Body["consolidated"].([]any)) != len(run.Body["consolidated"].([]any)) {
		t.Errorf("the run after the link ended: %s", frozen.Raw)
	}
	if acc := pe.call(http.MethodGet, "/v1/group/entities/"+row+"/accounts", accountant, nil); acc.Status != http.StatusNotFound {
		t.Errorf("an ended entity's accounts: %d %s", acc.Status, acc.Raw)
	}
	_ = storage.CodeGroupLink
}
