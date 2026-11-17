//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The statutory values in force on a day (ENG-30, ENG-31): the organisation's version then, each table's rows whose
// dates cover the day, with the sources they cite; one table or one row by name; none for a pack with no version then.
// req: ENG-31
// req: AI-53
func TestTheStatutoryValuesInForceOnADay(t *testing.T) {
	e := newEnv(t)
	reader := e.as(authz.Accountant)
	get := func(as, query string) response {
		t.Helper()
		return e.call(http.MethodGet, "/v1/statutory-values?"+query, as, nil)
	}
	r := get(reader, "pack=sg-gst&on=2026-11-15")
	if r.Status != http.StatusOK || !strings.HasPrefix(r.str("version"), "sg-gst@") || r.str("on") != "2026-11-15" {
		t.Fatalf("getStatutoryValues: %d %s", r.Status, r.Raw)
	}
	tables := map[string][]any{}
	for _, x := range r.Body["tables"].([]any) {
		m := x.(map[string]any)
		tables[m["name"].(string)] = m["rows"].([]any)
	}
	if rates := tables["rates"]; len(rates) != 1 || rates[0].(map[string]any)["values"].(map[string]any)["rate"] != "0.09" {
		t.Errorf("the rate in force: %v", rates)
	}
	if len(tables["codes"]) < 10 {
		t.Errorf("the codes, a table without dates, whole: %d", len(tables["codes"]))
	}
	if sources := r.Body["sources"].([]any); len(sources) == 0 || sources[0].(map[string]any)["url"] == "" {
		t.Errorf("the sources: %v", sources)
	}
	if old := get(reader, "pack=sg-gst&on=2005-06-30"); !strings.Contains(old.Raw, `"rate":"0.05"`) || strings.Contains(old.Raw, `"rate":"0.09"`) {
		t.Errorf("the rate in 2005: %s", old.Raw)
	}
	one := get(reader, "pack=sg-gst&on=2026-11-15&name=registration_threshold")
	if ts := one.Body["tables"].([]any); len(ts) != 1 || len(ts[0].(map[string]any)["rows"].([]any)) != 1 || !strings.Contains(one.Raw, `"amount":"1000000.00"`) {
		t.Errorf("one row by name: %s", one.Raw)
	}
	e.problemIs(get(reader, "pack=sg-nothing"), "getStatutoryValues", 404, CodeNotFound)
	if r := get(e.as(authz.Employee), "pack=sg-gst"); r.Status != http.StatusForbidden {
		t.Errorf("an Employee: %d", r.Status)
	}
}

// packs.get and lineage.explain as the turn's person (AI-53, AI-68, AI-49): a statutory value is a fact with its pack's
// key and version and its source; a fact's lineage is its report, read at its revision, the fact first, and a fact the
// model altered is refused.
// req: AI-53
// req: AI-68
// req: AI-P1
// asvs: V8.2.1
func TestPacksAndLineageReadAsThePerson(t *testing.T) {
	e := newEnv(t)
	acc := kpiBooks(e)
	f := foundersIn(e)
	service, err := e.services.Token(ServiceAI)
	if err != nil {
		t.Fatal(err)
	}
	var token string
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		token, err = e.api.issueAIGrant(ctx, u, e.books.Org, uuid.MustParse(f.ahkow), uuid.New())
		return err
	})
	tool := func(name string, input map[string]any) response {
		t.Helper()
		return e.call(http.MethodPost, "/internal/v1/tool-calls", token, map[string]any{"tool": name, "input": input}, ServiceTokenHeader, service)
	}
	factsOf := func(r response) []map[string]any {
		t.Helper()
		if r.Status != http.StatusOK {
			t.Fatalf("callAITool: %d %s", r.Status, r.Raw)
		}
		var out []map[string]any
		for _, x := range r.Body["facts"].([]any) {
			out = append(out, x.(map[string]any))
		}
		return out
	}

	threshold := factsOf(tool("packs.get", map[string]any{"pack": "sg-gst", "on": "2026-11-15", "name": "registration_threshold"}))
	if len(threshold) != 1 || threshold[0]["measure"] != "sg-gst.limits.registration_threshold.amount" || threshold[0]["value"] != "1000000.00" ||
		threshold[0]["unit"] != "amount" || threshold[0]["currency"] != "SGD" || threshold[0]["pack"] != "sg-gst" ||
		!strings.HasPrefix(threshold[0]["pack_version"].(string), "sg-gst@") || threshold[0]["source"] == "" {
		t.Errorf("the registration threshold: %v", threshold)
	}
	rate := factsOf(tool("packs.get", map[string]any{"pack": "sg-gst", "on": "2026-11-15", "name": "rates"}))
	if len(rate) != 1 || rate[0]["measure"] != "sg-gst.rates.rate" || rate[0]["value"] != "0.09" || rate[0]["from"] != "2024-01-01" {
		t.Errorf("the rate: %v", rate)
	}
	if r := tool("packs.get", map[string]any{"pack": "sg-cpf", "on": "2026-11-15"}); r.Status != http.StatusOK || len(r.Body["facts"].([]any)) == 0 {
		t.Errorf("CPF: %d %s", r.Status, r.Raw)
	}

	// A fact's lineage: the profit and loss it comes from, the fact first.
	q := e.call(http.MethodPost, "/v1/metrics:query", e.as(authz.Controller), map[string]any{"measure": "revenue", "from": "2026-10", "to": "2026-11"})
	revenue := q.Body["facts"].([]any)[0].(map[string]any)
	explain := map[string]any{"fact": revenue["id"], "measure": "revenue", "from": "2026-10", "to": "2026-11", "revision": revenue["ledger_revision"]}
	l := tool("lineage.explain", explain)
	lf := factsOf(l)
	if lf[0]["id"] != revenue["id"] || l.Body["content"] == nil || !strings.Contains(l.Raw, "Subscriptions") {
		t.Errorf("revenue's lineage: %s", l.Raw)
	}
	explain["from"] = "2026-11"
	if r := tool("lineage.explain", explain); r.Status != http.StatusUnprocessableEntity || r.str("code") != CodeToolRefused {
		t.Errorf("a fact with its months altered: %d %s", r.Status, r.Raw)
	}

	// An account's fact: the general ledger's lines.
	q = e.call(http.MethodPost, "/v1/metrics:query", e.as(authz.Controller), map[string]any{"measure": "account_movement", "by": "account",
		"member": acc["4010"].ID.String(), "from": "2026-11", "to": "2026-11"})
	movement := q.Body["facts"].([]any)[0].(map[string]any)
	g := tool("lineage.explain", map[string]any{"fact": movement["id"], "measure": "account_movement", "by": "account", "member": "4010",
		"from": "2026-11", "to": "2026-11", "revision": movement["ledger_revision"]})
	if gf := factsOf(g); gf[0]["id"] != movement["id"] || len(gf) < 2 || !strings.Contains(g.Raw, `"lines"`) {
		t.Errorf("an account's lineage: %s", g.Raw)
	}
}
