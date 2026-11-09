//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The control dashboard (CMP-01, FR-CTL-05; p1-compliance-center D9): every control of the registry with its status,
// each check with what it last said and its evidence, its owner role and frameworks; before compliance.run's first run
// each is not assessed. A control of each status shows as compliance.run saved it; the framework and status filters keep
// only theirs; one control reads alone; an unknown one is COMPLIANCE-01; a Bookkeeper may not read it.
// req: CMP-01
// req: FR-CTL-05
// req: compliance/compliance-center The Compliance Center shows controls and raises tasks
func TestTheControlDashboard(t *testing.T) {
	e := newEnv(t)
	r := e.call(http.MethodGet, "/v1/compliance/controls", e.as(authz.Auditor), nil)
	items, _ := r.Body["items"].([]any)
	if r.Status != 200 || len(items) < 67 || r.Body["release"] != nil {
		t.Fatalf("the dashboard: %d %d %s", r.Status, len(items), r.Raw[:min(len(r.Raw), 300)])
	}
	first := items[0].(map[string]any)
	if first["status"] != "not_assessed" || !strings.Contains(first["detail"].(string), "Not worked out yet") || first["checked_at"] != nil ||
		len(first["checks"].([]any)) == 0 || len(first["frameworks"].([]any)) == 0 {
		t.Errorf("a control before the first run: %v", first)
	}
	frameworks, _ := r.Body["frameworks"].([]any)
	if len(frameworks) != 10 || frameworks[0].(map[string]any)["key"] != "sg-statute" {
		t.Errorf("the frameworks: %v", frameworks)
	}

	// compliance.run saved four controls' states, one of each status.
	now := time.Date(2027, 1, 3, 20, 35, 0, 0, time.UTC)
	since := now.Add(-24 * time.Hour)
	checks := func(status, detail string) []byte {
		b, _ := json.Marshal([]map[string]string{{"frequency": "N", "how": "verify payments.dual-control", "status": status, "detail": detail},
			{"frequency": "CI", "how": "test TestAttackPaymentBatchesKeepTheirRelease", "status": "pass", "detail": "Passed in CI",
				"evidence": "https://github.com/cognizhi/fyniq/releases/download/v1.2.0/test-report.zip"}})
		return b
	}
	for _, s := range []storage.ControlState{
		{Control: "CTL-FIN-08", Status: "fail", Detail: "1 found in 2 approval(s)", Checks: checks("fail", "1 found in 2 approval(s)"), FailingSince: &since},
		{Control: "CTL-FIN-07", Status: "overdue", Detail: "2 actions wait", FailingSince: &since},
		{Control: "CTL-FIN-05", Status: "pass", Detail: "Each of its 1 check(s) passed."},
		{Control: "CTL-AI-02", Status: "not_assessed", Detail: "Not built yet: it arrives with p1-ai-proposals."},
	} {
		s.RunAt, s.ChangedAt = now, now
		if err := workerStore(t).Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Compliance().SaveState(ctx, s, 0)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	r = e.call(http.MethodGet, "/v1/compliance/controls/CTL-FIN-08", e.as(authz.Controller), nil)
	if r.Status != 200 || r.Body["status"] != "fail" || r.Body["failing_since"] != since.Format(time.RFC3339) ||
		r.Body["checked_at"] != now.Format(time.RFC3339) || r.Body["owner"] != "controller" || r.Body["section"] != "F.1" {
		t.Fatalf("a failing control: %d %s", r.Status, r.Raw)
	}
	byHow := map[string]map[string]any{}
	for _, c := range r.Body["checks"].([]any) {
		byHow[c.(map[string]any)["how"].(string)] = c.(map[string]any)
	}
	if v := byHow["verify payments.dual-control"]; v["status"] != "fail" || v["kind"] != "verify" || v["state"] != "running" || v["detail"] != "1 found in 2 approval(s)" {
		t.Errorf("its verification: %v", v)
	}
	if c := byHow["test TestAttackPaymentBatchesKeepTheirRelease"]; c["kind"] != "ci" || c["evidence"] != "https://github.com/cognizhi/fyniq/releases/download/v1.2.0/test-report.zip" {
		t.Errorf("its CI check: %v", c)
	}
	if c := byHow["check controls.break-glass-unreviewed"]; c["status"] != "not_assessed" || c["kind"] != "check" {
		t.Errorf("a check the run did not save: %v", c)
	}
	for query, want := range map[string][]string{
		"status=fail":                {"CTL-FIN-08"},
		"status=overdue":             {"CTL-FIN-07"},
		"status=pass":                {"CTL-FIN-05"},
		"framework=coso&status=fail": {"CTL-FIN-08"},
		"framework=pdpa&status=fail": nil,
	} {
		r := e.call(http.MethodGet, "/v1/compliance/controls?"+query, e.as(authz.DPO), nil)
		var got []string
		for _, it := range r.Body["items"].([]any) {
			got = append(got, it.(map[string]any)["id"].(string))
		}
		if r.Status != 200 || strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: %d %v, want %v", query, r.Status, got, want)
		}
	}
	if r := e.call(http.MethodGet, "/v1/compliance/controls?framework=nist", e.as(authz.Owner), nil); r.Status != 422 {
		t.Errorf("an unknown framework: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/compliance/controls/CTL-XYZ-99", e.as(authz.Owner), nil), "getControl", 404, CodeUnknownControl)
	e.problemIs(e.call(http.MethodGet, "/v1/compliance/controls", e.as(authz.Bookkeeper), nil), "listControls", 403, CodeForbidden)
}

// matrixControls is the authorisation matrix's row for the control dashboard (CMP-01, Appendix D "Compliance Center").
var matrixControls = map[string]func(e *env, tok string) (response, func() bool){
	"listControls": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/compliance/controls", tok, nil), nil
	},
	"getControl": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/compliance/controls/CTL-FIN-01", tok, nil), nil
	},
}
